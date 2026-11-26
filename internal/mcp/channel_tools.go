package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/channels"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/sessiontarget"
	"github.com/lsegal/aviary/internal/store"
)

type channelArgs struct {
	Agent   string `json:"agent"`
	Channel string `json:"channel"`
}

type channelCreateArgs struct {
	Agent string `json:"agent"`
	Name  string `json:"name"`
	Type  string `json:"type" schema:"enum=slack|discord|signal"`
}

type channelRenameArgs struct {
	Agent   string `json:"agent"`
	Channel string `json:"channel"`
	NewName string `json:"new_name"`
}

type channelUpdateArgs struct {
	Agent   string `json:"agent"`
	Channel string `json:"channel"`
	Enabled bool   `json:"enabled"`
}

type channelRemoveArgs struct {
	Agent      string `json:"agent"`
	Channel    string `json:"channel"`
	DeleteData bool   `json:"delete_data,omitempty" jsonschema:"Also delete the channel's sessions with their messages and media. Leave out to keep them as regular chats"`
}

// channelSessionDTO is one of a channel's sessions, in session_list's shape.
type channelSessionDTO struct {
	ID            string `json:"id"`
	AgentID       string `json:"agent_id"`
	Name          string `json:"name,omitempty"`
	Title         string `json:"title,omitempty"`
	CreatedAt     string `json:"created_at"`
	LastMessageAt string `json:"last_message_at,omitempty"`
	IsProcessing  bool   `json:"is_processing"`
	// Own marks the channel's own session, which messages delivered to the
	// channel by name land in.
	Own bool `json:"own"`
}

func registerChannelTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "channel_sessions",
		Description: "List a channel's chat sessions: its own session first (where messages delivered to the channel by name land), then the conversations it received",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args channelArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		_, a, idx, err := loadChannel(args.Agent, args.Channel)
		if err != nil {
			return nil, struct{}{}, err
		}
		if _, err := sessiontarget.EnsureChannel(a.Name, a.Channels[idx], channelManager()); err != nil {
			return nil, struct{}{}, err
		}
		sessions, err := sessiontarget.ChannelSessions(a, idx)
		if err != nil {
			return nil, struct{}{}, err
		}
		own := a.Channels[idx].SessionName()
		out := make([]channelSessionDTO, 0, len(sessions))
		for _, sess := range sessions {
			dto := channelSessionDTO{
				ID:           sess.ID,
				AgentID:      sess.AgentID,
				Name:         sess.Name,
				Title:        sess.Title,
				CreatedAt:    sess.CreatedAt.Format(time.RFC3339Nano),
				IsProcessing: agent.IsSessionProcessing(sess.AgentID, sess.ID),
				Own:          sess.Name == own,
			}
			if sess.LastMessageAt != nil {
				dto.LastMessageAt = sess.LastMessageAt.Format(time.RFC3339Nano)
			}
			out = append(out, dto)
		}
		return jsonResult(out)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "channel_create",
		Description: "Add a channel (slack, discord or signal) to an agent and create its session. The channel starts disabled until its connection settings are filled in.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args channelCreateArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "channels", "tool", "channel_create", "agent", args.Agent, "name", args.Name)
		name := strings.TrimSpace(args.Name)
		channelType := strings.ToLower(strings.TrimSpace(args.Type))
		if name == "" {
			return nil, struct{}{}, fmt.Errorf("name is required")
		}
		if channelType != "slack" && channelType != "discord" && channelType != "signal" {
			return nil, struct{}{}, fmt.Errorf("type must be slack, discord or signal")
		}
		cfg, ai, err := loadAgentConfig(args.Agent)
		if err != nil {
			return nil, struct{}{}, err
		}
		prev := cloneChannels(cfg)
		a := &cfg.Agents[ai]
		if a.ChannelByName(name) >= 0 {
			return nil, struct{}{}, fmt.Errorf("agent %q already has a channel named %q", a.Name, name)
		}
		disabled := false
		ch := config.ChannelConfig{
			Enabled: &disabled,
			Type:    channelType,
			ID:      uniqueChannelID(*a, channelType, name),
			Name:    name,
		}
		if channelType == "signal" {
			on := true
			ch.ShowTyping, ch.ReplyToReplies, ch.ReactToEmoji, ch.SendReadReceipts = &on, &on, &on, &on
		}
		a.Channels = append(a.Channels, ch)
		if err := saveChannelConfig(prev, cfg); err != nil {
			return nil, struct{}{}, err
		}
		sess, err := sessiontarget.EnsureChannel(a.Name, ch, channelManager())
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(map[string]any{"channel": ch, "session_id": sess.ID})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "channel_rename",
		Description: "Rename a channel. Its session and history stay, and delivery targets that pick it by name follow the new name.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args channelRenameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "channels", "tool", "channel_rename", "agent", args.Agent, "channel", args.Channel, "new", args.NewName)
		newName := strings.TrimSpace(args.NewName)
		if newName == "" {
			return nil, struct{}{}, fmt.Errorf("new_name is required")
		}
		cfg, a, idx, err := loadChannel(args.Agent, args.Channel)
		if err != nil {
			return nil, struct{}{}, err
		}
		if other := a.ChannelByName(newName); other >= 0 && other != idx {
			return nil, struct{}{}, fmt.Errorf("agent %q already has a channel named %q", a.Name, newName)
		}
		prev := cloneChannels(cfg)
		ai := agentIndex(cfg, a.Name)
		oldName := cfg.Agents[ai].Channels[idx].EffectiveName()
		cfg.Agents[ai].Channels[idx].Name = newName
		retargetChannel(cfg, a.Name, oldName, newName)
		if err := saveChannelConfig(prev, cfg); err != nil {
			return nil, struct{}{}, err
		}
		return text(fmt.Sprintf("channel %q renamed to %q", oldName, newName))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "channel_update",
		Description: "Enable or disable a channel. Disabled channels are not started and do not receive or send messages until re-enabled.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args channelUpdateArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "channels", "tool", "channel_update", "agent", args.Agent, "channel", args.Channel, "enabled", args.Enabled)
		cfg, a, idx, err := loadChannel(args.Agent, args.Channel)
		if err != nil {
			return nil, struct{}{}, err
		}
		prev := cloneChannels(cfg)
		ai := agentIndex(cfg, a.Name)
		enabled := args.Enabled
		cfg.Agents[ai].Channels[idx].Enabled = &enabled
		if err := saveChannelConfig(prev, cfg); err != nil {
			return nil, struct{}{}, err
		}
		verb := "disabled"
		if enabled {
			verb = "enabled"
		}
		return text(fmt.Sprintf("channel %q %s", cfg.Agents[ai].Channels[idx].EffectiveName(), verb))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "channel_remove",
		Description: "Remove a channel from an agent. delete_data=true deletes its sessions with their messages and media; without it they are kept as regular chats. Delivery targets that pick it by name are cleared.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args channelRemoveArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "channels", "tool", "channel_remove", "agent", args.Agent, "channel", args.Channel, "delete_data", args.DeleteData)
		cfg, a, idx, err := loadChannel(args.Agent, args.Channel)
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := sessiontarget.ReleaseChannel(a, idx, args.DeleteData); err != nil {
			return nil, struct{}{}, err
		}
		prev := cloneChannels(cfg)
		ai := agentIndex(cfg, a.Name)
		name := cfg.Agents[ai].Channels[idx].EffectiveName()
		cfg.Agents[ai].Channels = append(cfg.Agents[ai].Channels[:idx:idx], cfg.Agents[ai].Channels[idx+1:]...)
		retargetChannel(cfg, a.Name, name, "")
		if err := saveChannelConfig(prev, cfg); err != nil {
			return nil, struct{}{}, err
		}
		verb := "archived"
		if args.DeleteData {
			verb = "deleted"
		}
		return text(fmt.Sprintf("channel %q removed; its sessions were %s", name, verb))
	})
}

func channelManager() *channels.Manager {
	if d := GetDeps(); d != nil {
		return d.Channels
	}
	return nil
}

// loadAgentConfig loads the config and finds the agent named agentName.
func loadAgentConfig(agentName string) (*config.Config, int, error) {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return nil, -1, fmt.Errorf("agent is required")
	}
	cfg, err := config.Load("")
	if err != nil {
		return nil, -1, err
	}
	ai := agentIndex(cfg, agentName)
	if ai < 0 {
		return nil, -1, fmt.Errorf("agent %q is not configured", agentName)
	}
	return cfg, ai, nil
}

// loadChannel loads the config and finds agentName's channel named channel.
func loadChannel(agentName, channel string) (*config.Config, config.AgentConfig, int, error) {
	cfg, ai, err := loadAgentConfig(agentName)
	if err != nil {
		return nil, config.AgentConfig{}, -1, err
	}
	a := cfg.Agents[ai]
	idx := a.ChannelByName(channel)
	if idx < 0 {
		return nil, config.AgentConfig{}, -1, fmt.Errorf("agent %q has no channel named %q", a.Name, strings.TrimSpace(channel))
	}
	return cfg, a, idx, nil
}

func agentIndex(cfg *config.Config, name string) int {
	for i, a := range cfg.Agents {
		if strings.EqualFold(a.Name, strings.TrimSpace(name)) {
			return i
		}
	}
	return -1
}

// uniqueChannelID derives a channel ID from its name, unique among the
// agent's channels of that type.
func uniqueChannelID(a config.AgentConfig, channelType, name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteRune('-')
		}
	}
	base := strings.Trim(b.String(), "-")
	if base == "" {
		base = channelType
	}
	id := base
	for n := 2; a.ChannelByKey(channelType, id) >= 0; n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	return id
}

// retargetChannel points delivery targets that pick agentName's channel
// oldName at newName, or clears them when newName is empty.
func retargetChannel(cfg *config.Config, agentName, oldName, newName string) {
	move := func(target string) string {
		name, ok := config.ChannelTargetName(target)
		if !ok || !strings.EqualFold(name, oldName) {
			return target
		}
		if newName == "" {
			return ""
		}
		return config.ChannelTarget(newName)
	}
	for i := range cfg.Agents {
		a := &cfg.Agents[i]
		if a.Name == agentName {
			kept := a.Tasks[:0]
			for _, t := range a.Tasks {
				t.Target = move(t.Target)
				if t.IsWorkflow() && strings.EqualFold(strings.TrimSpace(t.Channel), oldName) {
					if newName == "" {
						// A workflow belongs to its channel and goes with it.
						removeWorkflowFile(*a, t)
						continue
					}
					t.Channel = newName
				}
				kept = append(kept, t)
			}
			a.Tasks = kept
		}
		for j := range a.Projects {
			reports := a.Projects[j].Reports
			if reports == nil || reports.Agent != agentName {
				continue
			}
			var kept []string
			for _, target := range reports.Targets {
				if next := move(target); next != "" {
					kept = append(kept, next)
				}
			}
			reports.Targets = kept
			if len(kept) == 0 {
				a.Projects[j].Reports = nil
			}
		}
	}
}

// removeWorkflowFile deletes the definition file of a workflow whose channel
// is being removed.
func removeWorkflowFile(a config.AgentConfig, t config.TaskConfig) {
	if !t.FromFile {
		return
	}
	if path, err := config.FindMarkdownTask(config.AgentTasksDir(a), t.Name); err == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Warn("channel: removing workflow file failed", "path", path, "err", err)
		}
	}
}

// cloneChannels copies the agents' channel lists, which are all a channel
// save's metadata update compares.
func cloneChannels(cfg *config.Config) *config.Config {
	prev := &config.Config{Agents: make([]config.AgentConfig, len(cfg.Agents))}
	for i, a := range cfg.Agents {
		prev.Agents[i] = config.AgentConfig{Name: a.Name, Channels: append([]config.ChannelConfig(nil), a.Channels...)}
	}
	return prev
}

// saveChannelConfig writes cfg after a channel change, which the channel
// tools check themselves, so unrelated config problems do not block it.
// File-backed tasks stay
// in their files: those whose target changed are rewritten, and the rest are
// left out of aviary.yaml.
func saveChannelConfig(prev, cfg *config.Config) error {
	for i := range cfg.Agents {
		a := &cfg.Agents[i]
		dir := config.AgentTasksDir(*a)
		var inline []config.TaskConfig
		for _, t := range a.Tasks {
			if !t.FromFile {
				inline = append(inline, t)
				continue
			}
			path, err := config.FindMarkdownTask(dir, t.Name)
			if err != nil {
				continue
			}
			onDisk, err := config.LoadMarkdownTask(path)
			if err != nil || (onDisk.Target == t.Target && onDisk.Channel == t.Channel) {
				continue
			}
			written, err := config.SaveMarkdownTask(dir, t)
			if err != nil {
				return fmt.Errorf("writing task file: %w", err)
			}
			if written != path {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
		}
		a.Tasks = inline
	}
	if err := config.Save("", cfg); err != nil {
		return err
	}
	if err := store.UpdateChannelMetadataState(prev, cfg, time.Now().UTC()); err != nil {
		return err
	}
	return reloadTaskConfig()
}
