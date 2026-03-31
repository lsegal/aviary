package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/channels"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/cronutil"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/scriptruntime"
	"github.com/lsegal/aviary/internal/sessiontarget"
	"github.com/lsegal/aviary/internal/store"
	"github.com/lsegal/aviary/internal/update"
	"github.com/lsegal/aviary/skills"
)

// ── Provider connectivity ping cache ─────────────────────────────────────────

// providerPingTTL is how long a cached ping result is considered fresh.
const providerPingTTL = 30 * time.Second

type providerPingEntry struct {
	ok        bool
	errMsg    string
	checkedAt time.Time
}

var (
	providerPingMu     sync.RWMutex
	providerPingCache  = map[string]providerPingEntry{}
	providerPingActive sync.Map // provider → struct{} while in flight
)

// deleteAgentMarkdownFile is an indirection for deleting agent markdown files
// so tests can simulate edge conditions (e.g. file-not-exist races).
var deleteAgentMarkdownFile = store.DeleteAgentMarkdownFile

const maxInlineSessionMediaBytes = 8 << 20

// startProviderPingIfStale fires a background goroutine to ping the provider
// unless a fresh result is already cached or a goroutine is already in flight.
func startProviderPingIfStale(provider, model string, factory *llm.Factory) {
	providerPingMu.RLock()
	entry, ok := providerPingCache[provider]
	providerPingMu.RUnlock()
	if ok && time.Since(entry.checkedAt) < providerPingTTL {
		return
	}
	if _, loaded := providerPingActive.LoadOrStore(provider, struct{}{}); loaded {
		return
	}
	go func() {
		defer providerPingActive.Delete(provider)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := factory.PingModel(ctx, model)
		e := providerPingEntry{ok: err == nil, checkedAt: time.Now()}
		if err != nil {
			e.errMsg = err.Error()
		}
		providerPingMu.Lock()
		providerPingCache[provider] = e
		providerPingMu.Unlock()
	}()
}

func localFileToDataURL(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("file is empty")
	}
	if len(data) > maxInlineSessionMediaBytes {
		return "", fmt.Errorf("file too large to attach inline (%d bytes > %d bytes)", len(data), maxInlineSessionMediaBytes)
	}
	mediaType := http.DetectContentType(data)
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// text returns a CallToolResult with a single text content item.
func text(s string) (*sdkmcp.CallToolResult, struct{}, error) {
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: s}},
	}, struct{}{}, nil
}

// jsonResult marshals v as JSON and returns it as a CallToolResult.
func jsonResult(v any) (*sdkmcp.CallToolResult, struct{}, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, struct{}{}, fmt.Errorf("marshaling result: %w", err)
	}
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(data)}},
	}, struct{}{}, nil
}

// stub returns a "not yet implemented" result for a named tool.
func stub(name string) (*sdkmcp.CallToolResult, struct{}, error) {
	return text(fmt.Sprintf("%s: not yet implemented", name))
}

// Register wires all Aviary MCP tools onto s.
// Tool handlers are stubs; replaced in later phases by real implementations.
func Register(s *sdkmcp.Server) {
	registerAgentTools(s)
	registerRulesTools(s)
	registerSessionTools(s)
	registerSessionSendTool(s)
	registerTaskTools(s)
	registerAgentContextTools(s)
	registerFileTools(s)
	registerExecTools(s)
	registerSessionTools(s)
	registerTaskTools(s)
	registerJobTools(s)
	registerBrowserTools(s)
	registerSearchTools(s)
	registerAuthTools(s)
	registerServerTools(s)
	registerSkillTools(s)
	registerUsageTools(s)
}

// ── Agent tools ──────────────────────────────────────────────────────────────

type agentRunArgs struct {
	Name                string `json:"name,omitempty"`
	Message             string `json:"message"`
	Session             string `json:"session,omitempty"` // session name; defaults to "main"
	SessionID           string `json:"session_id,omitempty"`
	File                string `json:"file,omitempty"`
	MediaURL            string `json:"media_url,omitempty"`             // optional image (data URL or remote URL)
	IncludeToolProgress bool   `json:"include_tool_progress,omitempty"` // opt-in live tool progress for web UI
	Bare                bool   `json:"bare,omitempty"`                  // skip all system prompt, rules, memory, and tool preamble
	History             *bool  `json:"history,omitempty"`               // include prior session messages; defaults to true unless bare=true
}

type agentNameArgs struct {
	Name string `json:"name"`
}

type agentStopArgs struct {
	Name      string `json:"name"`
	SessionID string `json:"session_id,omitempty"`
	Session   string `json:"session,omitempty"`
}

type agentRunScriptArgs struct {
	Agent     string `json:"agent,omitempty"`
	Script    string `json:"script"`
	Session   string `json:"session,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

type agentTemplateSyncArgs struct {
	Agent string `json:"agent"`
}

func loadSessionByID(agentName, sessionID string) (*domain.Session, error) {
	agentID, resolvedAgentName, err := resolveAgentIdentity(agentName)
	if err != nil {
		return nil, err
	}
	sessionID = strings.TrimSpace(sessionID)
	path := store.FindSessionPath(agentID, sessionID)
	if path == "" {
		return nil, fmt.Errorf("session %q not found for agent %q", sessionID, resolvedAgentName)
	}
	lines, err := store.ReadJSONL[map[string]any](path)
	if err != nil {
		return nil, fmt.Errorf("reading session %q: %w", sessionID, err)
	}
	var created, updated time.Time
	storedID := sessionID
	storedName := sessionID
	for _, m := range lines {
		if v, ok := m["id"]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				storedID = strings.TrimSpace(s)
			}
		}
		if v, ok := m["name"]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				storedName = strings.TrimSpace(s)
			}
		}
		if v, ok := m["created_at"]; ok {
			if s, ok := v.(string); ok {
				if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
					created = t
				}
			}
		}
		if v, ok := m["updated_at"]; ok {
			if s, ok := v.(string); ok {
				if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
					updated = t
				}
			}
		}
		if !created.IsZero() {
			break
		}
	}
	if created.IsZero() {
		return nil, fmt.Errorf("session %q is missing agent metadata", sessionID)
	}
	if updated.IsZero() {
		updated = created
	}
	return &domain.Session{
		ID:        storedID,
		AgentID:   agentID,
		Name:      storedName,
		CreatedAt: created,
		UpdatedAt: updated,
	}, nil
}

func resolveAgentIdentity(agentName string) (agentID, resolvedName string, err error) {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return "", "", fmt.Errorf("agent is required")
	}
	agentsDir := filepath.Join(store.DataDir(), store.DirAgents)
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return agentName, agentName, nil
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.EqualFold(entry.Name(), agentName) {
			return entry.Name(), entry.Name(), nil
		}
	}
	return agentName, agentName, nil
}

func resolveAgentRunHistory(args agentRunArgs) bool {
	if args.History != nil {
		return *args.History
	}
	return !args.Bare
}

func registerAgentTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "agent_list",
		Description: "List all configured agents and their current state",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Agents == nil {
			return nil, struct{}{}, fmt.Errorf("agent manager not initialized; is the server running?")
		}
		return jsonResult(d.Agents.List())
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "agent_run",
		Description: "Send a message to an agent and stream the response",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, args agentRunArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "chat", "tool", "agent_run", "agent", args.Name, "session", args.Session)
		d := GetDeps()
		if d.Agents == nil {
			return nil, struct{}{}, fmt.Errorf("agent manager not initialized; is the server running?")
		}

		agentName := strings.TrimSpace(args.Name)
		agentID := ""
		var sess *domain.Session
		if strings.TrimSpace(args.SessionID) != "" {
			if agentName == "" {
				return nil, struct{}{}, fmt.Errorf("name is required when session_id is provided")
			}
			loaded, err := loadSessionByID(agentName, args.SessionID)
			if err != nil {
				return nil, struct{}{}, err
			}
			sess = loaded
			agentID = strings.TrimSpace(sess.AgentID)
			if agentID == "" {
				return nil, struct{}{}, fmt.Errorf("session %q is missing agent metadata", args.SessionID)
			}
			agentName = agentID
		} else {
			if agentName == "" {
				return nil, struct{}{}, fmt.Errorf("name is required when session_id is not provided")
			}
			agentID = agentName
			// Ensure the session exists (defaults to "main").
			var err error
			sess, err = agent.NewSessionManager().GetOrCreateNamed(agentID, args.Session)
			if err != nil {
				return nil, struct{}{}, fmt.Errorf("initializing session: %w", err)
			}
		}

		runner, ok := d.Agents.Get(agentName)
		if !ok {
			return nil, struct{}{}, fmt.Errorf("agent %q not found", agentName)
		}
		if isStopCommand(args.Message) {
			stopped := agent.StopSession(sess.AgentID, sess.ID)
			if stopped == 0 {
				return text(fmt.Sprintf("session %q has no active work", sess.ID))
			}
			return text(fmt.Sprintf("stopped session %q", sess.ID))
		}
		ctx = agent.WithSessionID(ctx, sess.ID)

		var buf strings.Builder
		progressToken := req.Params.GetProgressToken()
		progressCount := 0.0
		done := make(chan error, 1)
		history := resolveAgentRunHistory(args)

		runner.PromptMediaWithOverrides(ctx, args.Message, args.MediaURL, agent.RunOverrides{
			Bare:    args.Bare,
			History: &history,
		}, func(e agent.StreamEvent) {
			switch e.Type {
			case agent.StreamEventText:
				buf.WriteString(e.Text)
				if progressToken != nil {
					progressCount++
					_ = req.Session.NotifyProgress(ctx, &sdkmcp.ProgressNotificationParams{
						ProgressToken: progressToken,
						Progress:      progressCount,
						Message:       e.Text,
					})
				}
			case agent.StreamEventTool:
				if progressToken != nil && args.IncludeToolProgress && e.Tool != nil {
					payload, err := json.Marshal(map[string]any{
						"name": e.Tool.Name,
						"args": e.Tool.Args,
					})
					if err == nil {
						progressCount++
						_ = req.Session.NotifyProgress(ctx, &sdkmcp.ProgressNotificationParams{
							ProgressToken: progressToken,
							Progress:      progressCount,
							Message:       "[tool]" + string(payload),
						})
					}
				}
			case agent.StreamEventMedia:
				if e.MediaURL != "" && progressToken != nil {
					progressCount++
					_ = req.Session.NotifyProgress(ctx, &sdkmcp.ProgressNotificationParams{
						ProgressToken: progressToken,
						Progress:      progressCount,
						// Prefix lets the client detect media progress vs text.
						Message: "[media]" + e.MediaURL,
					})
				}
			case agent.StreamEventDone:
				done <- nil
			case agent.StreamEventStop:
				done <- context.Canceled
			case agent.StreamEventError:
				if errors.Is(e.Err, context.Canceled) {
					done <- context.Canceled
					return
				}
				done <- e.Err
			}
		})
		if err := <-done; err != nil {
			if errors.Is(err, context.Canceled) {
				return text(buf.String())
			}
			slog.Error("mcp: tool failed", "component", "chat", "tool", "agent_run", "agent", args.Name, "err", err)
			return &sdkmcp.CallToolResult{
				IsError: true,
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: err.Error()}},
			}, struct{}{}, nil
		}
		slog.Info("mcp: tool done", "component", "chat", "tool", "agent_run", "agent", args.Name)
		return text(buf.String())
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "agent_stop",
		Description: "Immediately stop all work in progress for an agent",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args agentStopArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Agents == nil {
			return nil, struct{}{}, fmt.Errorf("agent manager not initialized; is the server running?")
		}
		runner, ok := d.Agents.Get(args.Name)
		if !ok {
			return nil, struct{}{}, fmt.Errorf("agent %q not found", args.Name)
		}

		// If a session was specified, only stop that session and delete matching
		// checkpoints. Otherwise stop the whole agent and delete all checkpoints.
		sid := strings.TrimSpace(args.SessionID)
		if sid == "" && strings.TrimSpace(args.Session) != "" {
			// Resolve agent ID then get/create named session to obtain session ID.
			agentID, _, err := resolveAgentIdentity(args.Name)
			if err == nil {
				if sess, err2 := agent.NewSessionManager().GetOrCreateNamed(agentID, args.Session); err2 == nil && sess != nil {
					sid = sess.ID
				}
			}
		}

		if sid != "" {
			// Stop only the specified session (does nothing if no active work).
			stopped := agent.StopSession(args.Name, sid)
			if stopped == 0 {
				// Still attempt to delete matching checkpoints even if nothing was running.
				slog.Info("mcp: agent_stop - no active runs for session", "agent", args.Name, "session", sid)
			}
			// Delete checkpoints matching this session ID.
			dir := store.CheckpointDir(args.Name)
			entries, err := os.ReadDir(dir)
			if err == nil {
				for _, e := range entries {
					if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
						continue
					}
					p := filepath.Join(dir, e.Name())
					v, rerr := store.ReadJSON[agent.RunCheckpoint](p)
					if rerr == nil {
						if v.SessionID == sid {
							if derr := store.DeleteJSON(p); derr != nil {
								slog.Warn("mcp: failed to delete checkpoint", "agent", args.Name, "path", p, "err", derr)
							}
						}
					} else {
						// Couldn't read checkpoint — try to delete to avoid leaving corrupt files.
						if derr := store.DeleteJSON(p); derr != nil {
							slog.Warn("mcp: failed to delete unreadable checkpoint", "agent", args.Name, "path", p, "err", derr)
						}
					}
				}
			}
			return text(fmt.Sprintf("agent %q stopped (session %s)", args.Name, sid))
		}

		// No session specified: stop whole agent and delete all checkpoints.
		runner.Stop()
		dir := store.CheckpointDir(args.Name)
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
					continue
				}
				p := filepath.Join(dir, e.Name())
				if err := store.DeleteJSON(p); err != nil {
					slog.Warn("mcp: failed to delete checkpoint", "agent", args.Name, "path", p, "err", err)
				}
			}
		}
		return text(fmt.Sprintf("agent %q stopped", args.Name))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "agent_run_script",
		Description: "Run an embedded Lua script for the current agent/session. Scripts get a sandboxed `tool.<name>({ ... })` table and an `environment` table with agent_id, session_id, task_id, and job_id.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args agentRunScriptArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Agents == nil {
			return nil, struct{}{}, fmt.Errorf("agent manager not initialized; is the server running?")
		}
		agentName := strings.TrimSpace(args.Agent)
		if agentName == "" {
			if fromCtx, ok := agent.SessionAgentIDFromContext(ctx); ok {
				agentName = strings.TrimSpace(fromCtx)
			}
		}
		if agentName == "" {
			return nil, struct{}{}, fmt.Errorf("agent is required")
		}

		var sess *domain.Session
		if strings.TrimSpace(args.SessionID) != "" {
			loaded, err := loadSessionByID(agentName, args.SessionID)
			if err != nil {
				return nil, struct{}{}, err
			}
			sess = loaded
		} else {
			sessionName := strings.TrimSpace(args.Session)
			if sessionName == "" {
				sessionName = "main"
			}
			created, err := agent.NewSessionManager().GetOrCreateNamed(agentName, sessionName)
			if err != nil {
				return nil, struct{}{}, fmt.Errorf("initializing session: %w", err)
			}
			sess = created
		}

		runCtx := agent.WithSessionAgentID(agent.WithSessionID(ctx, sess.ID), sess.AgentID)
		if taskID, ok := agent.TaskIDFromContext(ctx); ok {
			runCtx = agent.WithTaskID(runCtx, taskID)
		}
		if jobID, ok := agent.JobIDFromContext(ctx); ok {
			runCtx = agent.WithJobID(runCtx, jobID)
		}
		toolClient, err := newScriptToolClient(runCtx)
		if err != nil {
			return nil, struct{}{}, err
		}
		defer toolClient.Close() //nolint:errcheck

		output, err := scriptruntime.RunLua(runCtx, args.Script, scriptruntime.Options{
			ToolClient: toolClient,
			Environment: scriptruntime.Environment{
				AgentID:   sess.AgentID,
				SessionID: sess.ID,
			},
		})
		if err != nil {
			return &sdkmcp.CallToolResult{
				IsError: true,
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: err.Error()}},
			}, struct{}{}, nil
		}
		return text(output)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "agent_get",
		Description: "Get the full configuration for a named agent",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args agentNameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		cfg, loadErr := config.Load("")
		if loadErr != nil {
			return nil, struct{}{}, loadErr
		}
		for _, ac := range cfg.Agents {
			if ac.Name == args.Name {
				return jsonResult(ac)
			}
		}
		return nil, struct{}{}, fmt.Errorf("agent %q not found", args.Name)
	})

	type agentUpsertArgs struct {
		Name      string   `json:"name"`
		Model     string   `json:"model,omitempty"`
		Fallbacks []string `json:"fallbacks,omitempty"`
	}

	addTool(s, &sdkmcp.Tool{
		Name:        "agent_add",
		Description: "Add a new agent to the configuration",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args agentUpsertArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if args.Name == "" {
			return nil, struct{}{}, fmt.Errorf("name is required")
		}
		cfg, loadErr := config.Load("")
		if loadErr != nil {
			return nil, struct{}{}, loadErr
		}
		for _, ac := range cfg.Agents {
			if ac.Name == args.Name {
				return nil, struct{}{}, fmt.Errorf("agent %q already exists", args.Name)
			}
		}
		cfg.Agents = append(cfg.Agents, config.AgentConfig{
			Name:      args.Name,
			Model:     args.Model,
			Fallbacks: args.Fallbacks,
		})
		if err := config.Save("", cfg); err != nil {
			return nil, struct{}{}, err
		}
		if err := store.SyncAgentTemplate(args.Name); err != nil {
			return nil, struct{}{}, err
		}
		if d.Agents != nil {
			d.Agents.Reconcile(cfg)
		}
		return text(fmt.Sprintf("agent %q added", args.Name))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "agent_template_sync",
		Description: "Sync the embedded agent template into an agent directory. Missing files are added, existing files are preserved, and markdown files only update the 'Synced by Aviary' section.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args agentTemplateSyncArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if strings.TrimSpace(args.Agent) == "" {
			return nil, struct{}{}, fmt.Errorf("agent is required")
		}
		if err := store.SyncAgentTemplate(args.Agent); err != nil {
			return nil, struct{}{}, err
		}
		return text(fmt.Sprintf("templates synced for agent %q", args.Agent))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "agent_update",
		Description: "Update an existing agent's configuration fields",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args agentUpsertArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if args.Name == "" {
			return nil, struct{}{}, fmt.Errorf("name is required")
		}
		cfg, loadErr := config.Load("")
		if loadErr != nil {
			return nil, struct{}{}, loadErr
		}
		found := false
		for i := range cfg.Agents {
			if cfg.Agents[i].Name == args.Name {
				if args.Model != "" {
					cfg.Agents[i].Model = args.Model
				}
				if args.Fallbacks != nil {
					cfg.Agents[i].Fallbacks = args.Fallbacks
				}
				found = true
				break
			}
		}
		if !found {
			return nil, struct{}{}, fmt.Errorf("agent %q not found", args.Name)
		}
		if err := config.Save("", cfg); err != nil {
			return nil, struct{}{}, err
		}
		if d.Agents != nil {
			d.Agents.Reconcile(cfg)
		}
		return text(fmt.Sprintf("agent %q updated", args.Name))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "agent_delete",
		Description: "Remove an agent from the configuration",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args agentNameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		cfg, err := config.Load("")
		if err != nil {
			return nil, struct{}{}, err
		}
		newAgents := cfg.Agents[:0]
		found := false
		for _, ac := range cfg.Agents {
			if ac.Name == args.Name {
				found = true
				continue
			}
			newAgents = append(newAgents, ac)
		}
		if !found {
			return nil, struct{}{}, fmt.Errorf("agent %q not found", args.Name)
		}
		cfg.Agents = newAgents
		if err := config.Save("", cfg); err != nil {
			return nil, struct{}{}, err
		}
		if d.Agents != nil {
			d.Agents.Reconcile(cfg)
		}
		return text(fmt.Sprintf("agent %q deleted", args.Name))
	})
}

// ── Rules tools ──────────────────────────────────────────────────────────────

type agentRulesSetArgs struct {
	Agent   string `json:"agent"`
	Content string `json:"content"`
}

func registerRulesTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "agent_rules_get",
		Description: "Read the RULES.md file for an agent (returns empty string if none)",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args agentNameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		agentID, _, err := resolveAgentIdentity(args.Name)
		if err != nil {
			return nil, struct{}{}, err
		}
		path := store.AgentRulesPath(agentID)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return text("")
			}
			return nil, struct{}{}, fmt.Errorf("reading rules: %w", err)
		}
		return text(string(data))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "agent_rules_set",
		Description: "Write the RULES.md file for an agent (creates or replaces)",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args agentRulesSetArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		agentID, _, err := resolveAgentIdentity(args.Agent)
		if err != nil {
			return nil, struct{}{}, err
		}
		path := store.AgentRulesPath(agentID)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, struct{}{}, fmt.Errorf("creating agent dir: %w", err)
		}
		if err := os.WriteFile(path, []byte(args.Content), 0o600); err != nil {
			return nil, struct{}{}, fmt.Errorf("writing rules: %w", err)
		}
		return text(fmt.Sprintf("RULES.md written for agent %q", agentID))
	})
}

type sessionAgentArgs struct {
	Agent string `json:"agent,omitempty"`
}

type agentFileReadArgs struct {
	Agent string `json:"agent,omitempty"`
	File  string `json:"file"`
}

type agentFileWriteArgs struct {
	Agent   string `json:"agent,omitempty"`
	File    string `json:"file"`
	Content string `json:"content"`
}

// resolveAgentID returns the agent ID from session context, falling back to the
// explicit agent arg for callers without a session (e.g. the web UI).
func resolveAgentID(ctx context.Context, argAgent string) (string, bool) {
	if id, ok := agent.SessionAgentIDFromContext(ctx); ok && id != "" {
		return id, true
	}
	if argAgent != "" {
		return argAgent, true
	}
	return "", false
}

func registerAgentContextTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "agent_file_list",
		Description: "List all markdown files under the current agent's data directory, including subdirectories and built-in files such as AGENTS.md, RULES.md, and MEMORY.md.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args sessionAgentArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		agentID, ok := resolveAgentID(ctx, args.Agent)
		if !ok {
			return nil, struct{}{}, fmt.Errorf("agent_file_list requires an agent session context")
		}
		files, err := store.ListAgentMarkdownFiles(agentID)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("listing agent files: %w", err)
		}
		return jsonResult(files)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "agent_file_read",
		Description: "Read a markdown file from the current agent's data directory. Use agent_file_list first when you need extra context and are not sure which file is relevant.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args agentFileReadArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		agentID, ok := resolveAgentID(ctx, args.Agent)
		if !ok {
			return nil, struct{}{}, fmt.Errorf("agent_file_read requires an agent session context")
		}
		content, err := store.ReadAgentMarkdownFile(agentID, args.File)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("reading agent file: %w", err)
		}
		return text(content)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "agent_file_write",
		Description: "Create or replace a markdown file in the current agent's data directory. Use paths like notes/foo.md for notes or MEMORY.md for memory. Protected built-in files such as AGENTS.md, SYSTEM.md, MEMORY.md, and RULES.md cannot be deleted but can be written.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args agentFileWriteArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		agentID, ok := resolveAgentID(ctx, args.Agent)
		if !ok {
			return nil, struct{}{}, fmt.Errorf("agent_file_write requires an agent session context")
		}
		if err := store.WriteAgentMarkdownFile(agentID, args.File, args.Content); err != nil {
			return nil, struct{}{}, fmt.Errorf("writing agent file: %w", err)
		}
		return text(fmt.Sprintf("%s written", strings.TrimSpace(args.File)))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "agent_file_delete",
		Description: "Delete a markdown file from the current agent's data directory. Protected built-in files such as AGENTS.md, SYSTEM.md, MEMORY.md, and RULES.md cannot be deleted.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args agentFileReadArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		agentID, ok := resolveAgentID(ctx, args.Agent)
		if !ok {
			return nil, struct{}{}, fmt.Errorf("agent_file_delete requires an agent session context")
		}
		if err := deleteAgentMarkdownFile(agentID, args.File); err != nil {
			return nil, struct{}{}, fmt.Errorf("deleting agent file: %w", err)
		}
		return text(fmt.Sprintf("%s deleted", strings.TrimSpace(args.File)))
	})
}

type sessionMessagesArgs struct {
	SessionID string `json:"session_id"`
	Agent     string `json:"agent,omitempty"`
	ID        string `json:"id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Skip      int    `json:"skip,omitempty"`
	Order     string `json:"order,omitempty"`
}

type sessionStopArgs struct {
	SessionID string `json:"session_id,omitempty"`
	Agent     string `json:"agent,omitempty"`
	Session   string `json:"session,omitempty"`
}

type sessionSetTargetArgs struct {
	Agent       string `json:"agent"`
	SessionID   string `json:"session_id"`
	ChannelType string `json:"channel_type"`
	ChannelID   string `json:"channel_id"`
	Target      string `json:"target"`
}

func resolveSessionTargetIdentity(agentName, sessionID string) (agentID, resolvedAgentName string, err error) {
	agentID, resolvedAgentName, err = resolveAgentIdentity(agentName)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(sessionID) == "" {
		return "", "", fmt.Errorf("session_id is required")
	}
	if store.FindSessionPath(agentID, sessionID) == "" {
		return "", "", fmt.Errorf("session %q not found for agent %q", sessionID, resolvedAgentName)
	}
	return agentID, resolvedAgentName, nil
}

func registerSessionTools(s *sdkmcp.Server) {
	sessionHistoryHandler := func(_ context.Context, _ *sdkmcp.CallToolRequest, args sessionMessagesArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if strings.TrimSpace(args.Agent) == "" {
			return nil, struct{}{}, fmt.Errorf("agent is required")
		}
		if args.SessionID == "" {
			return nil, struct{}{}, fmt.Errorf("session_id is required")
		}
		if args.Limit < 0 {
			return nil, struct{}{}, fmt.Errorf("limit must be >= 0")
		}
		if args.Skip < 0 {
			return nil, struct{}{}, fmt.Errorf("skip must be >= 0")
		}

		order := strings.ToLower(strings.TrimSpace(args.Order))
		switch order {
		case "", "asc", "desc":
		default:
			return nil, struct{}{}, fmt.Errorf("order must be 'asc' or 'desc'")
		}

		agentID, _, err := resolveAgentIdentity(args.Agent)
		if err != nil {
			return nil, struct{}{}, err
		}
		path := store.FindSessionPath(agentID, args.SessionID)
		if path == "" {
			return jsonResult([]any{})
		}
		lines, err := store.ReadJSONL[domain.Message](path)
		if err != nil {
			return nil, struct{}{}, err
		}

		type messageDTO struct {
			ID         string                `json:"id"`
			SessionID  string                `json:"session_id"`
			Role       string                `json:"role"`
			Sender     *domain.MessageSender `json:"sender,omitempty"`
			Content    string                `json:"content"`
			MediaURL   string                `json:"media_url,omitempty"`
			Model      string                `json:"model,omitempty"`
			ResponseID string                `json:"response_id,omitempty"`
			Timestamp  string                `json:"timestamp"`
		}

		responseIDs := make(map[string]string)
		for _, msg := range lines {
			if msg.Role == "" && msg.ID != "" && msg.ResponseID != "" {
				responseIDs[msg.ID] = msg.ResponseID
			}
		}

		out := make([]messageDTO, 0, len(lines))
		for _, msg := range lines {
			if msg.Role == "" {
				continue
			}
			ts := ""
			if !msg.Timestamp.IsZero() {
				ts = msg.Timestamp.Format(time.RFC3339Nano)
			}
			responseID := responseIDs[msg.ID]

			out = append(out, messageDTO{
				ID:         msg.ID,
				SessionID:  args.SessionID,
				Role:       string(msg.Role),
				Sender:     msg.Sender,
				Content:    msg.Content,
				MediaURL:   msg.MediaURL,
				Model:      msg.Model,
				ResponseID: responseID,
				Timestamp:  ts,
			})
		}

		if args.ID != "" {
			filtered := out[:0]
			for _, m := range out {
				if m.ID == args.ID {
					filtered = append(filtered, m)
				}
			}
			out = filtered
		}

		if order == "desc" {
			for left, right := 0, len(out)-1; left < right; left, right = left+1, right-1 {
				out[left], out[right] = out[right], out[left]
			}
		}

		if args.Skip > 0 {
			if args.Skip >= len(out) {
				out = out[:0]
			} else {
				out = out[args.Skip:]
			}
		}
		if args.Limit > 0 && args.Limit < len(out) {
			out = out[:args.Limit]
		}

		return jsonResult(out)
	}

	addTool(s, &sdkmcp.Tool{
		Name:        "session_list",
		Description: "List all sessions for an agent",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args sessionAgentArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "chat_session", "tool", "session_list", "agent", args.Agent)
		if args.Agent == "" {
			return nil, struct{}{}, fmt.Errorf("agent name is required")
		}
		agentsDir := filepath.Join(store.DataDir(), store.DirAgents)
		agentNameDir := strings.TrimSpace(args.Agent)
		if entries, err := os.ReadDir(agentsDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				if strings.EqualFold(e.Name(), args.Agent) {
					agentNameDir = e.Name()
					break
				}
			}
		}
		agentID := agentNameDir
		sm := agent.NewSessionManager()
		slog.Info("mcp: session_list resolving", "agent", args.Agent, "resolved_agent_dir", agentNameDir, "agent_id", agentID)
		if _, err := sm.GetOrCreateNamed(agentID, "main"); err != nil {
			slog.Error("mcp: session_list get/create main failed", "agent", agentID, "err", err)
			return nil, struct{}{}, err
		}
		sessions, err := sm.List(agentID)
		if err != nil {
			return nil, struct{}{}, err
		}
		if len(sessions) == 0 {
			slog.Info("mcp: session_list found no sessions", "agent", agentID)
		} else {
			ids := make([]string, 0, len(sessions))
			for _, ss := range sessions {
				if ss == nil {
					ids = append(ids, "<nil>")
					continue
				}
				ids = append(ids, fmt.Sprintf("%s(%s)", ss.ID, ss.Name))
			}
			slog.Info("mcp: session_list found sessions", "agent", agentID, "sessions", strings.Join(ids, ", "))
		}
		mainIndex := -1
		for i, sess := range sessions {
			if sess == nil {
				continue
			}
			if sess.Name == "main" || strings.EqualFold(sess.ID, "main") {
				mainIndex = i
				break
			}
		}
		if mainIndex > 0 {
			m := sessions[mainIndex]
			sessions = append([]*domain.Session{m}, append(sessions[:mainIndex], sessions[mainIndex+1:]...)...)
		}
		if mainIndex >= 0 && sessions[0] != nil && sessions[0].Name == "" {
			sessions[0].Name = "main"
		}
		type sessionDTO struct {
			ID           string `json:"id"`
			AgentID      string `json:"agent_id"`
			Name         string `json:"name,omitempty"`
			TaskID       string `json:"task_id,omitempty"`
			CreatedAt    string `json:"created_at"`
			UpdatedAt    string `json:"updated_at"`
			IsProcessing bool   `json:"is_processing"`
		}
		out := make([]sessionDTO, 0, len(sessions))
		for _, sess := range sessions {
			if sess == nil {
				continue
			}
			out = append(out, sessionDTO{
				ID:           sess.ID,
				AgentID:      sess.AgentID,
				Name:         sess.Name,
				TaskID:       sess.TaskID,
				CreatedAt:    sess.CreatedAt.Format(time.RFC3339Nano),
				UpdatedAt:    sess.UpdatedAt.Format(time.RFC3339Nano),
				IsProcessing: agent.IsSessionProcessing(sess.AgentID, sess.ID),
			})
		}
		return jsonResult(out)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "session_create",
		Description: "Create a new session for an agent",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args sessionAgentArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "chat_session", "tool", "session_create", "agent", args.Agent)
		if args.Agent == "" {
			return nil, struct{}{}, fmt.Errorf("agent name is required")
		}
		agentID := args.Agent
		sm := agent.NewSessionManager()
		sess, err := sm.Create(agentID)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(sess)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "session_messages",
		Description: "List persisted messages for a session. Supports order=desc with limit/skip for efficient recent-history reads.",
	}, sessionHistoryHandler)

	addTool(s, &sdkmcp.Tool{
		Name:        "session_history",
		Description: "Read session history. Prefer order=desc and limit=20 to recover recent context in group chats or resumed sessions.",
	}, sessionHistoryHandler)

	addTool(s, &sdkmcp.Tool{
		Name:        "session_stop",
		Description: "Stop all in-progress work for a specific session",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args sessionStopArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		sid := strings.TrimSpace(args.SessionID)
		agentName := strings.TrimSpace(args.Agent)
		if sid == "" {
			if fromCtx, ok := agent.SessionIDFromContext(ctx); ok {
				sid = fromCtx
			}
			if fromCtxAgent, ok := agent.SessionAgentIDFromContext(ctx); ok && agentName == "" {
				agentName = fromCtxAgent
			}
		}
		if sid == "" && agentName != "" {
			sessionName := strings.TrimSpace(args.Session)
			agentID, _, err := resolveAgentIdentity(agentName)
			if err != nil {
				return nil, struct{}{}, err
			}
			sess, err := agent.NewSessionManager().GetOrCreateNamed(agentID, sessionName)
			if err != nil {
				return nil, struct{}{}, err
			}
			sid = sess.ID
		}
		if agentName == "" {
			return nil, struct{}{}, fmt.Errorf("agent is required")
		}
		if sid == "" {
			return nil, struct{}{}, fmt.Errorf("session_id is required")
		}
		agentID, _, err := resolveAgentIdentity(agentName)
		if err != nil {
			return nil, struct{}{}, err
		}
		stopped := agent.StopSession(agentID, sid)
		if stopped == 0 {
			return text(fmt.Sprintf("session %q has no active work", sid))
		}
		return text(fmt.Sprintf("stopped %d active run(s) in session %q", stopped, sid))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "session_remove",
		Description: "Permanently delete a session and all its messages",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args sessionStopArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if strings.TrimSpace(args.Agent) == "" {
			return nil, struct{}{}, fmt.Errorf("agent is required")
		}
		sid := strings.TrimSpace(args.SessionID)
		if sid == "" {
			return nil, struct{}{}, fmt.Errorf("session_id is required")
		}
		agentID, _, err := resolveAgentIdentity(args.Agent)
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := agent.NewSessionManager().Delete(agentID, sid); err != nil {
			return nil, struct{}{}, err
		}
		return text(fmt.Sprintf("session %q removed", sid))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "session_set_target",
		Description: "Set the configured channel target for a session and persist it in the session sidecar",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args sessionSetTargetArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		agentNameArg := strings.TrimSpace(args.Agent)
		sessionID := strings.TrimSpace(args.SessionID)
		channelType := strings.TrimSpace(args.ChannelType)
		configuredID := strings.TrimSpace(args.ChannelID)
		targetID := strings.TrimSpace(args.Target)
		if agentNameArg == "" {
			return nil, struct{}{}, fmt.Errorf("agent is required")
		}
		if sessionID == "" {
			return nil, struct{}{}, fmt.Errorf("session_id is required")
		}
		if channelType == "" {
			return nil, struct{}{}, fmt.Errorf("channel_type is required")
		}
		if configuredID == "" {
			return nil, struct{}{}, fmt.Errorf("channel_id is required")
		}
		if targetID == "" {
			return nil, struct{}{}, fmt.Errorf("target is required")
		}

		agentID, agentName, err := resolveSessionTargetIdentity(agentNameArg, sessionID)
		if err != nil {
			return nil, struct{}{}, err
		}

		d := GetDeps()
		var channelMgr *channels.Manager
		if d != nil {
			channelMgr = d.Channels
		}
		target := store.SessionChannel{
			Type:         channelType,
			ConfiguredID: configuredID,
			ID:           targetID,
		}
		if err := sessiontarget.Set(agentID, agentName, sessionID, target, channelMgr); err != nil {
			return nil, struct{}{}, fmt.Errorf("setting session target: %w", err)
		}

		msg := fmt.Sprintf("session %q will deliver output via %s/%s to %s", sessionID, channelType, configuredID, targetID)
		if channelMgr == nil {
			msg += " after the channel manager loads the sidecar"
		}
		return text(msg)
	})
}

func isStopCommand(message string) bool {
	n := strings.TrimSpace(strings.ToLower(message))
	n = strings.TrimPrefix(n, "/")
	switch n {
	case "stop", "halt", "cancel", "abort":
		return true
	default:
		return false
	}
}

// ── Task tools ───────────────────────────────────────────────────────────────

type taskNameArgs struct {
	Name string `json:"name"`
}

type taskStopArgs struct {
	Name  string `json:"name,omitempty"`
	JobID string `json:"job_id,omitempty"`
}

type taskScheduleArgs struct {
	Agent        string   `json:"agent"`
	Name         string   `json:"name,omitempty"`
	Type         string   `json:"type,omitempty" schema:"enum=prompt|script"`
	Content      string   `json:"content"`
	In           string   `json:"in,omitempty"`
	Schedule     string   `json:"schedule,omitempty"`
	Target       string   `json:"target,omitempty"`
	TriggerType  string   `json:"trigger_type,omitempty" schema:"enum=cron|watch"`
	RunDiscovery bool     `json:"run_discovery,omitempty"`
	Schema       struct{} `json:"-" schema:"atmostone=in|schedule"`
}

func registerTaskTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "task_list",
		Description: "List configured tasks and their trigger definitions",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "scheduler", "tool", "task_list")
		d := GetDeps()
		if d.Scheduler == nil {
			return nil, struct{}{}, fmt.Errorf("scheduler not initialized")
		}
		return jsonResult(d.Scheduler.ListTasks())
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "task_run",
		Description: "Immediately trigger a configured task by name (e.g. 'myagent/daily-report' or just 'daily-report')",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args taskNameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "scheduler", "tool", "task_run", "task", args.Name)
		d := GetDeps()
		if d.Scheduler == nil {
			return nil, struct{}{}, fmt.Errorf("scheduler not initialized")
		}
		job, err := d.Scheduler.Trigger(args.Name)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(job)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "task_schedule",
		Description: "Register a new scheduled task definition. This does not run a task. The scheduler is responsible for running tasks automatically based on their schedule. Only call this to define a new task that doesn't yet exist. Use in=<delay> for a one-time task, or schedule=<cron expression> for a recurring configured task. Use the argument name schedule, not cron. Do not add timezone arguments, timezone conversions, or timestamp-formatting logic unless the task explicitly requires them. Use content for the task body. For type=script tasks, content must be Aviary embedded Lua source, not shell/bash/sh or a shebang script. If type is omitted, it defaults to prompt. Aviary accepts standard 5-field cron and 6-field cron with leading seconds. Optional name=<task-name> for recurring tasks. Prompt tasks may be precomputed into script tasks automatically when scheduler.precompute_tasks is enabled.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args taskScheduleArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "scheduler", "tool", "task_schedule", "agent", args.Agent, "in", args.In, "schedule", args.Schedule)
		d := GetDeps()
		if d.Scheduler == nil {
			return nil, struct{}{}, fmt.Errorf("scheduler not initialized")
		}
		if args.Agent == "" {
			return nil, struct{}{}, fmt.Errorf("agent is required")
		}
		if d.Agents != nil {
			if _, ok := d.Agents.Get(args.Agent); !ok {
				return nil, struct{}{}, fmt.Errorf("agent %q not found; use agent_list to see available agents", args.Agent)
			}
		}
		taskType := strings.ToLower(strings.TrimSpace(args.Type))
		if taskType == "" {
			taskType = "prompt"
		}
		requestedTaskType := taskType
		if taskType != "prompt" && taskType != "script" {
			return nil, struct{}{}, fmt.Errorf("invalid task type %q", args.Type)
		}
		contentText := strings.TrimSpace(args.Content)
		if contentText == "" {
			return nil, struct{}{}, fmt.Errorf("content is required")
		}
		promptText := ""
		scriptText := ""
		switch taskType {
		case "prompt":
			promptText = contentText
		case "script":
			scriptText = contentText
			if err := scriptruntime.ValidateLua(scriptText); err != nil {
				return nil, struct{}{}, fmt.Errorf("script tasks require valid Aviary Lua source: %w", err)
			}
			promptText = generatedTaskName(scriptText)
		}
		if strings.TrimSpace(args.In) != "" && strings.TrimSpace(args.Schedule) != "" {
			return nil, struct{}{}, fmt.Errorf("only one of \"in\" or \"schedule\" may be set")
		}
		loadedCfg, loadErr := config.Load("")
		if loadErr != nil {
			return nil, struct{}{}, loadErr
		}
		target := normalizeTaskTarget(args.Target)
		effectiveTarget := target
		if strings.TrimSpace(normalizeTaskSchedule(args.Schedule)) != "" {
			effectiveTarget = firstNonEmpty(target, defaultScheduledTaskRoute(ctx, loadedCfg, args.Agent))
		}
		precomputeEnabled := config.EffectivePrecomputeTasks(loadedCfg.Scheduler)
		if taskType == "prompt" && promptText != "" && scriptText == "" {
			trigger := "immediate"
			if strings.TrimSpace(args.In) != "" {
				trigger = "delay"
			}
			if strings.TrimSpace(args.Schedule) != "" {
				trigger = "schedule"
			}
			tracker := newTaskCompileTracker(args.Agent, strings.TrimSpace(args.Name), taskType, promptText, effectiveTarget, trigger, args.RunDiscovery)
			slog.Info(
				"task_compile: schedule precompute decision",
				"component", "task_compile",
				"agent", args.Agent,
				"target", effectiveTarget,
				"precompute_enabled", precomputeEnabled,
				"run_discovery", args.RunDiscovery,
			)
			if precomputeEnabled {
				compiled, compileErr := resolveTryCompileTaskPrompt()(withTaskCompileTracker(ctx, tracker), args.Agent, promptText, effectiveTarget, args.RunDiscovery)
				if compileErr != nil {
					tracker.record.Status = domain.TaskCompileStatusFailed
					tracker.record.Reason = compileErr.Error()
					slog.Warn("task_compile: precompute errored; continuing with prompt task", "component", "task_compile", "agent", args.Agent, "error", compileErr)
				} else if compiled.Type == "script" && strings.TrimSpace(compiled.Script) != "" && !compiled.NeedsDiscovery {
					taskType = "script"
					scriptText = strings.TrimSpace(compiled.Script)
					tracker.record.Status = domain.TaskCompileStatusSucceeded
					tracker.record.ResultTaskType = "script"
					tracker.record.NeedsDiscovery = compiled.NeedsDiscovery
					tracker.record.Validated = compiled.Validated
					tracker.record.Reason = strings.TrimSpace(compiled.Reason)
					tracker.record.Steps = toDomainCompileSteps(compiled.Steps)
					tracker.record.Script = scriptText
					slog.Info(
						"task_compile: promoted prompt to script",
						"component", "task_compile",
						"agent", args.Agent,
						"validated", compiled.Validated,
						"steps", len(compiled.Steps),
						"reason", strings.TrimSpace(compiled.Reason),
					)
				} else {
					tracker.record.Status = domain.TaskCompileStatusSkipped
					tracker.record.ResultTaskType = compiled.Type
					tracker.record.NeedsDiscovery = compiled.NeedsDiscovery
					tracker.record.Validated = compiled.Validated
					tracker.record.Reason = strings.TrimSpace(compiled.Reason)
					tracker.record.Steps = toDomainCompileSteps(compiled.Steps)
					slog.Info(
						"task_compile: kept prompt after precompute",
						"component", "task_compile",
						"agent", args.Agent,
						"compiled_type", compiled.Type,
						"needs_discovery", compiled.NeedsDiscovery,
						"reason", strings.TrimSpace(compiled.Reason),
					)
				}
				if persistErr := tracker.persist(); persistErr != nil {
					slog.Warn("task_compile: failed to persist compile record", "component", "task_compile", "agent", args.Agent, "error", persistErr)
				}
			} else {
				slog.Info("task_compile: skipped precompute because scheduler setting is disabled", "component", "task_compile", "agent", args.Agent)
			}
		}
		triggerType := strings.ToLower(strings.TrimSpace(args.TriggerType))
		if triggerType != "" && triggerType != "cron" && triggerType != "watch" {
			return nil, struct{}{}, fmt.Errorf("invalid trigger_type %q", args.TriggerType)
		}
		normalizedSchedule := normalizeTaskSchedule(args.Schedule)
		if strings.TrimSpace(normalizedSchedule) != "" {
			if err := validateTaskSchedule(normalizedSchedule); err != nil {
				return nil, struct{}{}, err
			}
			if triggerType == "watch" {
				return nil, struct{}{}, fmt.Errorf("trigger_type %q conflicts with schedule; use cron or omit trigger_type", args.TriggerType)
			}
			agentIdx := -1
			for i := range loadedCfg.Agents {
				if loadedCfg.Agents[i].Name == args.Agent {
					agentIdx = i
					break
				}
			}
			if agentIdx < 0 {
				return nil, struct{}{}, fmt.Errorf("agent %q not found in config", args.Agent)
			}
			taskTarget := nextTaskTarget(ctx, target, loadedCfg, args.Agent)
			taskName := strings.TrimSpace(args.Name)
			if taskName == "" {
				taskName = generatedRecurringTaskName(requestedTaskType, contentText, normalizedSchedule, taskTarget)
				if existingName, ok := findUnnamedRecurringTaskMatch(loadedCfg.Agents[agentIdx].Tasks, requestedTaskType, contentText, normalizedSchedule, taskTarget); ok {
					taskName = existingName
				}
			}
			// For script tasks, store the Lua source in Prompt so that
			// task markdown files contain the script body as the file body.
			body := promptText
			if taskType == "script" {
				body = scriptText
			}
			nextTask := config.TaskConfig{
				Type:     taskType,
				Name:     taskName,
				Prompt:   body,
				Schedule: strings.TrimSpace(normalizedSchedule),
				Target:   taskTarget,
			}
			// Check whether a task with this name already exists (for action message).
			updated := false
			for _, t := range loadedCfg.Agents[agentIdx].Tasks {
				if t.Name == taskName {
					updated = true
					break
				}
			}
			// Write the task definition to the agent's tasks/ directory as a
			// markdown file.  This keeps task definitions out of aviary.yaml.
			tasksDir := config.AgentTasksDir(loadedCfg.Agents[agentIdx])
			if _, saveErr := config.SaveMarkdownTask(tasksDir, nextTask); saveErr != nil {
				return nil, struct{}{}, saveErr
			}
			// Reload config (including newly written task file) then reconcile.
			reloadedCfg, reloadErr := config.Load("")
			if reloadErr != nil {
				return nil, struct{}{}, reloadErr
			}
			deps := GetDeps()
			if deps != nil && deps.Agents != nil {
				deps.Agents.Reconcile(reloadedCfg)
			}
			if deps != nil && deps.Scheduler != nil {
				deps.Scheduler.Reconcile(reloadedCfg)
			}
			action := "created"
			if updated {
				action = "updated"
			}
			return text(fmt.Sprintf("Recurring task %q %s for agent %q with schedule %q.", taskName, action, args.Agent, nextTask.Schedule))
		}
		agentID := args.Agent
		taskID := strings.TrimSpace(args.Name)
		if taskID == "" {
			taskID = generatedTaskName(firstNonEmpty(promptText, scriptText))
		}

		replySessionID, _ := agent.SessionIDFromContext(ctx)
		replyAgentID, _ := agent.SessionAgentIDFromContext(ctx)

		var job *domain.Job
		var err error
		if args.In != "" {
			delay, parseErr := parseDuration(args.In)
			if parseErr != nil {
				return nil, struct{}{}, fmt.Errorf("invalid duration %q: %w", args.In, parseErr)
			}
			job, err = d.Scheduler.Queue().EnqueueAtWithType(taskID, taskType, agentID, promptText, scriptText, "", 1, time.Now().Add(delay), replyAgentID, replySessionID)
		} else {
			job, err = d.Scheduler.Queue().EnqueueWithType(taskID, taskType, agentID, promptText, scriptText, "", 1, replyAgentID, replySessionID)
		}
		if err != nil {
			return nil, struct{}{}, err
		}
		when := "immediately"
		if job.ScheduledFor != nil {
			when = "at " + job.ScheduledFor.Format("15:04:05")
		}
		return text(fmt.Sprintf("Task scheduled %s (job ID: %s). Done — no further action needed.", when, job.ID))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "task_stop",
		Description: "Stop scheduled task jobs. Optional name=<task-name or agent/task> or job_id=<job-id>; omit both to stop all pending and running jobs.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args taskStopArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Scheduler == nil {
			return nil, struct{}{}, fmt.Errorf("scheduler not initialized")
		}
		target := strings.TrimSpace(args.JobID)
		if target == "" {
			target = strings.TrimSpace(args.Name)
		}
		stopped, err := d.Scheduler.StopJobs(target)
		if err != nil {
			return nil, struct{}{}, err
		}
		if stopped == 0 {
			if target == "" {
				return text("no pending or running task jobs to stop")
			}
			return text(fmt.Sprintf("no pending or running task jobs matched %q", target))
		}
		if target == "" {
			return text(fmt.Sprintf("stopped %d pending/running task job(s)", stopped))
		}
		return text(fmt.Sprintf("stopped %d pending/running task job(s) matching %q", stopped, target))
	})
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, " ", "")
	for _, r := range []struct{ from, to string }{
		{"minutes", "m"}, {"minute", "m"},
		{"hours", "h"}, {"hour", "h"},
		{"seconds", "s"}, {"second", "s"},
	} {
		s = strings.ReplaceAll(s, r.from, r.to)
	}
	return time.ParseDuration(s)
}

func validateTaskSchedule(schedule string) error {
	c := cronutil.New()
	if _, err := c.AddFunc(strings.TrimSpace(schedule), func() {}); err != nil {
		return fmt.Errorf("invalid schedule %q: %w", schedule, err)
	}
	return nil
}

var everyMinutesPattern = regexp.MustCompile(`(?i)^every\s+(\d+)\s+minutes?$`)

func normalizeTaskSchedule(schedule string) string {
	trimmed := strings.TrimSpace(schedule)
	if trimmed == "" {
		return ""
	}
	if match := everyMinutesPattern.FindStringSubmatch(trimmed); len(match) == 2 {
		n, err := strconv.Atoi(match[1])
		if err == nil && n > 0 {
			return fmt.Sprintf("0 */%d * * * *", n)
		}
	}
	return trimmed
}

func defaultScheduledTaskRoute(ctx context.Context, cfg *config.Config, agentName string) string {
	channelType, configuredID, channelID, ok := agent.ChannelSessionFromContext(ctx)
	if !ok {
		sessionID, _ := agent.SessionIDFromContext(ctx)
		sessionAgentID, _ := agent.SessionAgentIDFromContext(ctx)
		if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(sessionAgentID) == "" {
			return ""
		}
		if sessionAgentID != agentName {
			return ""
		}
		return "session:" + strings.TrimSpace(sessionID)
	}
	for _, ac := range cfg.Agents {
		if ac.Name != agentName {
			continue
		}
		for _, ch := range ac.Channels {
			if ch.Type == channelType && ch.ID == configuredID {
				return fmt.Sprintf("%s:%s:%s", channelType, configuredID, channelID)
			}
		}
		return ""
	}
	return ""
}

func generatedTaskName(prompt string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(prompt) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && b.String()[b.Len()-1] != '-':
			b.WriteRune('-')
		}
		if b.Len() >= 24 {
			break
		}
	}
	base := strings.Trim(b.String(), "-")
	if base == "" {
		base = "scheduled"
	}
	return fmt.Sprintf("%s-%d", base, time.Now().Unix())
}

func generatedRecurringTaskName(taskType, content, schedule, target string) string {
	base := recurringTaskNameBase(content)
	identity := strings.Join([]string{
		strings.TrimSpace(strings.ToLower(taskType)),
		strings.TrimSpace(content),
		strings.TrimSpace(schedule),
		normalizeTaskTarget(target),
	}, "\x00")
	h := fnv.New32a()
	_, _ = h.Write([]byte(identity))
	return fmt.Sprintf("%s-%08x", base, h.Sum32())
}

func findUnnamedRecurringTaskMatch(tasks []config.TaskConfig, taskType, content, schedule, target string) (string, bool) {
	base := recurringTaskNameBase(content)
	expectedPrefix := base + "-"
	for _, task := range tasks {
		if strings.TrimSpace(task.Schedule) != strings.TrimSpace(schedule) {
			continue
		}
		if strings.TrimSpace(task.Target) != strings.TrimSpace(target) {
			continue
		}
		if !strings.HasPrefix(task.Name, expectedPrefix) {
			continue
		}
		if generatedRecurringTaskName(taskType, content, schedule, target) == task.Name {
			return task.Name, true
		}
		return task.Name, true
	}
	return "", false
}

func recurringTaskNameBase(content string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(content) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && b.String()[b.Len()-1] != '-':
			b.WriteRune('-')
		}
		if b.Len() >= 24 {
			break
		}
	}
	base := strings.Trim(b.String(), "-")
	if base == "" {
		base = "scheduled"
	}
	return base
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// ── Job tools ────────────────────────────────────────────────────────────────

func nextTaskTarget(ctx context.Context, target string, cfg *config.Config, agentName string) string {
	return firstNonEmpty(normalizeTaskTarget(target), defaultScheduledTaskRoute(ctx, cfg, agentName))
}

func normalizeTaskTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" || strings.EqualFold(target, "silent") {
		return ""
	}
	return target
}

type jobListArgs struct {
	Task string `json:"task,omitempty"`
}

type jobIDArgs struct {
	ID string `json:"id"`
}

type jobQueryArgs struct {
	ID     string `json:"id,omitempty"`
	Start  string `json:"start,omitempty"`
	End    string `json:"end,omitempty"`
	Status string `json:"status,omitempty"`
	Agent  string `json:"agent,omitempty"`
}

type taskCompileQueryArgs struct {
	ID     string `json:"id,omitempty"`
	Start  string `json:"start,omitempty"`
	End    string `json:"end,omitempty"`
	Status string `json:"status,omitempty"`
	Agent  string `json:"agent,omitempty"`
}

func listTaskCompiles() ([]domain.TaskCompile, error) {
	var records []domain.TaskCompile
	for _, dir := range store.AllTaskCompileDirs() {
		batch, err := store.ListJSON[domain.TaskCompile](dir, ".json")
		if err != nil {
			return nil, err
		}
		records = append(records, batch...)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[j].CreatedAt.Before(records[i].CreatedAt)
	})
	return records, nil
}

func registerJobTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "task_compile_query",
		Description: "Return task compile attempt records filtered by id, date range, status, and/or agent",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args taskCompileQueryArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "task_compile", "tool", "task_compile_query")
		all, err := listTaskCompiles()
		if err != nil {
			return nil, struct{}{}, err
		}
		var start, end time.Time
		if args.Start != "" {
			start, _ = time.Parse("2006-01-02", args.Start)
		}
		if args.End != "" {
			end, _ = time.Parse("2006-01-02", args.End)
			end = end.Add(24*time.Hour - time.Nanosecond)
		}
		out := all[:0]
		for _, record := range all {
			if args.ID != "" && record.ID != args.ID {
				continue
			}
			if !start.IsZero() && record.CreatedAt.Before(start) {
				continue
			}
			if !end.IsZero() && record.CreatedAt.After(end) {
				continue
			}
			if args.Status != "" && string(record.Status) != args.Status {
				continue
			}
			if args.Agent != "" && record.AgentID != args.Agent {
				continue
			}
			out = append(out, record)
		}
		return jsonResult(out)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "task_compile_get",
		Description: "Show the full stored record for a specific task compile attempt",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args jobIDArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "task_compile", "tool", "task_compile_get", "id", args.ID)
		path := store.FindTaskCompilePath(args.ID)
		if path == "" {
			return nil, struct{}{}, fmt.Errorf("task compile %s not found", args.ID)
		}
		record, err := store.ReadJSON[domain.TaskCompile](path)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("reading task compile %s: %w", args.ID, err)
		}
		return jsonResult(record)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "job_list",
		Description: "Show job history across all tasks",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args jobListArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "scheduler", "tool", "job_list", "task", args.Task)
		d := GetDeps()
		if d.Scheduler == nil {
			return nil, struct{}{}, fmt.Errorf("scheduler not initialized")
		}
		jobs, err := d.Scheduler.Queue().List(args.Task)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(jobs)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "job_query",
		Description: "Return job records filtered by id, date range, status, and/or agent",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args jobQueryArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "scheduler", "tool", "job_query")
		d := GetDeps()
		if d.Scheduler == nil {
			return nil, struct{}{}, fmt.Errorf("scheduler not initialized")
		}
		all, err := d.Scheduler.Queue().List("")
		if err != nil {
			return nil, struct{}{}, err
		}
		var start, end time.Time
		if args.Start != "" {
			start, _ = time.Parse("2006-01-02", args.Start)
		}
		if args.End != "" {
			end, _ = time.Parse("2006-01-02", args.End)
			end = end.Add(24*time.Hour - time.Nanosecond)
		}
		out := all[:0]
		for _, j := range all {
			if args.ID != "" && j.ID != args.ID {
				continue
			}
			if !start.IsZero() && j.CreatedAt.Before(start) {
				continue
			}
			if !end.IsZero() && j.CreatedAt.After(end) {
				continue
			}
			if args.Status != "" && string(j.Status) != args.Status {
				continue
			}
			if args.Agent != "" && j.AgentID != args.Agent {
				continue
			}
			out = append(out, j)
		}
		return jsonResult(out)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "job_logs",
		Description: "Show captured output for a specific job run",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args jobIDArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "scheduler", "tool", "job_logs", "id", args.ID)
		path := store.FindJobPath(args.ID)
		if path == "" {
			return nil, struct{}{}, fmt.Errorf("job %s not found", args.ID)
		}
		job, err := store.ReadJSON[domain.Job](path)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("reading job %s: %w", args.ID, err)
		}
		if strings.TrimSpace(job.Output) == "" {
			// If no explicit job output was captured, attempt to show the
			// session message log for the job's session (includes tool
			// events, secondary inputs, media markers, and assistant text).
			if job.SessionID != "" {
				sessPath := store.FindSessionPath(job.AgentID, job.SessionID)
				if sessPath != "" {
					lines, err := store.ReadJSONL[domain.Message](sessPath)
					if err == nil && len(lines) > 0 {
						var sb strings.Builder
						for _, msg := range lines {
							if msg.Role == "" {
								// response marker or metadata — skip
								continue
							}
							ts := ""
							if !msg.Timestamp.IsZero() {
								ts = msg.Timestamp.Format(time.RFC3339Nano)
							}
							content := strings.TrimSpace(msg.Content)
							if msg.MediaURL != "" {
								if content != "" {
									content += " "
								}
								content += "[media: " + msg.MediaURL + "]"
							}
							// Pretty-print tool JSON payloads when possible.
							if msg.Role == domain.MessageRoleTool && content != "" {
								var pretty any
								if jerr := json.Unmarshal([]byte(content), &pretty); jerr == nil {
									if p, merr := json.MarshalIndent(pretty, "", "  "); merr == nil {
										content = string(p)
									}
								}
							}
							fmt.Fprintf(&sb, "[%s] <%s>: %s\n", ts, string(msg.Role), content)
						}
						out := sb.String()
						if strings.TrimSpace(out) != "" {
							return text(out)
						}
					}
				}
			}
			return text("(no output captured)")
		}
		return text(job.Output)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "job_stop",
		Description: "Stop a pending or running job by ID. Cancels execution and marks the job as canceled.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args jobIDArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "scheduler", "tool", "job_stop", "id", args.ID)
		d := GetDeps()
		if d.Scheduler == nil {
			return nil, struct{}{}, fmt.Errorf("scheduler not initialized")
		}
		stopped, err := d.Scheduler.StopJobs(args.ID)
		if err != nil {
			return nil, struct{}{}, err
		}
		if stopped == 0 {
			return text(fmt.Sprintf("no pending or running job found with ID %q", args.ID))
		}
		return text(fmt.Sprintf("stopped job %s", args.ID))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "job_run_now",
		Description: "Immediately run an existing pending job by ID, ignoring its scheduled time and worker concurrency limits",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args jobIDArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "scheduler", "tool", "job_run_now", "id", args.ID)
		d := GetDeps()
		if d.Scheduler == nil {
			return nil, struct{}{}, fmt.Errorf("scheduler not initialized")
		}
		job, err := d.Scheduler.RunJobNow(args.ID)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(job)
	})
}

// ── Browser tools ────────────────────────────────────────────────────────────

type browserOpenArgs struct {
	URL string `json:"url"`
}

type browserTabArgs struct {
	TabID string `json:"tab_id"`
}

type browserSelectorArgs struct {
	TabID    string `json:"tab_id"`
	Selector string `json:"selector"`
}

type browserTypeArgs struct {
	TabID    string `json:"tab_id"`
	Selector string `json:"selector"`
	Text     string `json:"text"`
}

type browserNavigateArgs struct {
	TabID string `json:"tab_id"`
	URL   string `json:"url"`
}

type browserWaitArgs struct {
	TabID     string `json:"tab_id"`
	Selector  string `json:"selector"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

type browserTextArgs struct {
	TabID     string `json:"tab_id"`
	Selector  string `json:"selector,omitempty"`
	MaxLength int    `json:"max_length,omitempty"`
}

type browserQueryArgs struct {
	TabID         string `json:"tab_id"`
	Selector      string `json:"selector"`
	Count         int    `json:"count,omitempty"`
	MaxTextLength int    `json:"max_text_length,omitempty"`
	IncludeHTML   bool   `json:"include_html,omitempty"`
}

type browserResizeArgs struct {
	TabID  string `json:"tab_id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func registerBrowserTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "browser_open",
		Description: "Navigate to a URL in a browser tab. Returns the tab_id needed for subsequent operations on that tab. When browser.reuse_tabs is enabled, an existing tab with the exact same URL is reused. Always close tabs with browser_close when done to avoid resource leaks",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args browserOpenArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "browser", "tool", "browser_open", "url", args.URL)
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		tabID, err := d.Browser.Open(ctx, args.URL)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(map[string]any{"tab_id": tabID, "url": args.URL})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "browser_tabs",
		Description: "List all currently open browser tabs and their IDs",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "browser", "tool", "browser_tabs")
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		tabs, err := d.Browser.Tabs()
		if err != nil {
			slog.Error("mcp: tool failed", "component", "browser", "tool", "browser_tabs", "err", err)
			return nil, struct{}{}, err
		}
		slog.Info("browser tabs listed", "component", "browser", "count", len(tabs))
		return jsonResult(tabs)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "browser_navigate",
		Description: "Navigate an existing browser tab to a new URL.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args browserNavigateArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "browser", "tool", "browser_navigate", "tab_id", args.TabID, "url", args.URL)
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		if args.TabID == "" {
			return nil, struct{}{}, fmt.Errorf("tab_id is required")
		}
		if args.URL == "" {
			return nil, struct{}{}, fmt.Errorf("url is required")
		}
		if err := d.Browser.Navigate(ctx, args.TabID, args.URL); err != nil {
			slog.Error("mcp: tool failed", "component", "browser", "tool", "browser_navigate", "tab_id", args.TabID, "url", args.URL, "err", err)
			return nil, struct{}{}, err
		}
		return jsonResult(map[string]any{"tab_id": args.TabID, "url": args.URL})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "browser_wait",
		Description: "Wait for a CSS selector to become visible in the specified tab.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args browserWaitArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "browser", "tool", "browser_wait", "tab_id", args.TabID, "selector", args.Selector)
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		if args.TabID == "" {
			return nil, struct{}{}, fmt.Errorf("tab_id is required")
		}
		if args.Selector == "" {
			return nil, struct{}{}, fmt.Errorf("selector is required")
		}
		timeoutMS := args.TimeoutMS
		if timeoutMS <= 0 {
			timeoutMS = 10000
		}
		if timeoutMS > 60000 {
			timeoutMS = 60000
		}
		waitCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
		defer cancel()
		if err := d.Browser.WaitVisible(waitCtx, args.TabID, args.Selector, time.Duration(timeoutMS)*time.Millisecond); err != nil {
			slog.Error("mcp: tool failed", "component", "browser", "tool", "browser_wait", "tab_id", args.TabID, "selector", args.Selector, "err", err)
			return nil, struct{}{}, err
		}
		return jsonResult(map[string]any{"tab_id": args.TabID, "selector": args.Selector, "timeout_ms": timeoutMS, "status": "visible"})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "browser_click",
		Description: "Click an element by CSS selector in the specified tab",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args browserSelectorArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "browser", "tool", "browser_click", "tab_id", args.TabID, "selector", args.Selector)
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		if args.TabID == "" {
			return nil, struct{}{}, fmt.Errorf("tab_id is required")
		}
		if err := d.Browser.Click(ctx, args.TabID, args.Selector); err != nil {
			slog.Error("mcp: tool failed", "component", "browser", "tool", "browser_click", "tab_id", args.TabID, "selector", args.Selector, "err", err)
			return nil, struct{}{}, err
		}
		slog.Info("clicked element", "component", "browser", "tab_id", args.TabID, "selector", args.Selector)
		return text(fmt.Sprintf("clicked %q in tab %s", args.Selector, args.TabID))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "browser_keystroke",
		Description: "Send keystrokes to an element by CSS selector in the specified tab",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args browserTypeArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "browser", "tool", "browser_keystroke", "tab_id", args.TabID, "selector", args.Selector)
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		if args.TabID == "" {
			return nil, struct{}{}, fmt.Errorf("tab_id is required")
		}
		if err := d.Browser.Type(ctx, args.TabID, args.Selector, args.Text); err != nil {
			slog.Error("mcp: tool failed", "component", "browser", "tool", "browser_keystroke", "tab_id", args.TabID, "selector", args.Selector, "err", err)
			return nil, struct{}{}, err
		}
		slog.Info("keystrokes sent", "component", "browser", "tab_id", args.TabID, "selector", args.Selector)
		return text(fmt.Sprintf("keystrokes sent to %q in tab %s", args.Selector, args.TabID))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "browser_fill",
		Description: "Fill (default typing) text in an element by CSS selector in the specified tab",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args browserTypeArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "browser", "tool", "browser_fill", "tab_id", args.TabID, "selector", args.Selector)
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		if args.TabID == "" {
			return nil, struct{}{}, fmt.Errorf("tab_id is required")
		}
		if err := d.Browser.Fill(ctx, args.TabID, args.Selector, args.Text); err != nil {
			slog.Error("mcp: tool failed", "component", "browser", "tool", "browser_fill", "tab_id", args.TabID, "selector", args.Selector, "err", err)
			return nil, struct{}{}, err
		}
		slog.Info("filled element", "component", "browser", "tab_id", args.TabID, "selector", args.Selector)
		return text(fmt.Sprintf("filled %q in tab %s", args.Selector, args.TabID))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "browser_text",
		Description: "Extract normalized visible text from the whole page or from elements matching a CSS selector.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args browserTextArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "browser", "tool", "browser_text", "tab_id", args.TabID, "selector", args.Selector)
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		if args.TabID == "" {
			return nil, struct{}{}, fmt.Errorf("tab_id is required")
		}
		selectorJSON, err := json.Marshal(args.Selector)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("encoding selector: %w", err)
		}
		maxLength := args.MaxLength
		if maxLength <= 0 {
			maxLength = 4000
		}
		if maxLength > 20000 {
			maxLength = 20000
		}
		expr := fmt.Sprintf(`(() => {
			const selector = %s;
			const normalize = (value) => (value || "").replace(/\s+/g, " ").trim();
			let nodes = [];
			if (selector) {
				nodes = Array.from(document.querySelectorAll(selector));
			} else if (document.body) {
				nodes = [document.body];
			}
			const text = normalize(nodes.map((node) => node.innerText || node.textContent || "").join("\n\n")).slice(0, %d);
			return JSON.stringify({
				tab_id: %q,
				url: window.location.href,
				title: document.title || "",
				selector: selector || "",
				match_count: nodes.length,
				text
			});
		})()`, string(selectorJSON), maxLength, args.TabID)
		raw, err := d.Browser.EvalJS(ctx, args.TabID, expr)
		if err != nil {
			slog.Error("mcp: tool failed", "component", "browser", "tool", "browser_text", "tab_id", args.TabID, "selector", args.Selector, "err", err)
			return nil, struct{}{}, err
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil, struct{}{}, fmt.Errorf("parsing browser_text result: %w", err)
		}
		return jsonResult(payload)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "browser_query",
		Description: "Extract structured data from elements matching a CSS selector, including text and common attributes.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args browserQueryArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "browser", "tool", "browser_query", "tab_id", args.TabID, "selector", args.Selector)
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		if args.TabID == "" {
			return nil, struct{}{}, fmt.Errorf("tab_id is required")
		}
		if args.Selector == "" {
			return nil, struct{}{}, fmt.Errorf("selector is required")
		}
		selectorJSON, err := json.Marshal(args.Selector)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("encoding selector: %w", err)
		}
		count := args.Count
		if count <= 0 {
			count = 20
		}
		if count > 100 {
			count = 100
		}
		maxTextLength := args.MaxTextLength
		if maxTextLength <= 0 {
			maxTextLength = 500
		}
		if maxTextLength > 5000 {
			maxTextLength = 5000
		}
		expr := fmt.Sprintf(`(() => {
			const selector = %s;
			const normalize = (value) => (value || "").replace(/\s+/g, " ").trim();
			const items = Array.from(document.querySelectorAll(selector)).slice(0, %d).map((node, index) => ({
				index,
				tag_name: (node.tagName || "").toLowerCase(),
				text: normalize(node.innerText || node.textContent || "").slice(0, %d),
				href: node.href || node.getAttribute("href") || "",
				src: node.src || node.getAttribute("src") || "",
				value: node.value || "",
				aria_label: node.getAttribute("aria-label") || "",
				html: %t ? (node.outerHTML || "") : ""
			}));
			return JSON.stringify({
				tab_id: %q,
				url: window.location.href,
				title: document.title || "",
				selector,
				count: items.length,
				items
			});
		})()`, string(selectorJSON), count, maxTextLength, args.IncludeHTML, args.TabID)
		raw, err := d.Browser.EvalJS(ctx, args.TabID, expr)
		if err != nil {
			slog.Error("mcp: tool failed", "component", "browser", "tool", "browser_query", "tab_id", args.TabID, "selector", args.Selector, "err", err)
			return nil, struct{}{}, err
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil, struct{}{}, fmt.Errorf("parsing browser_query result: %w", err)
		}
		return jsonResult(payload)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "browser_screenshot",
		Description: "Capture a screenshot of the specified tab",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args browserTabArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "browser", "tool", "browser_screenshot", "tab_id", args.TabID)
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		if args.TabID == "" {
			return nil, struct{}{}, fmt.Errorf("tab_id is required")
		}
		png, err := d.Browser.Screenshot(ctx, args.TabID)
		if err != nil {
			slog.Error("mcp: tool failed", "component", "browser", "tool", "browser_screenshot", "tab_id", args.TabID, "err", err)
			return nil, struct{}{}, err
		}

		screenshotDir := store.BrowserMediaDir()
		if err := os.MkdirAll(screenshotDir, 0o700); err != nil {
			return nil, struct{}{}, fmt.Errorf("creating screenshot directory: %w", err)
		}

		filename := fmt.Sprintf("screenshot_%s_%d.png", args.TabID, time.Now().UnixMilli())
		path := filepath.Join(screenshotDir, filename)
		if err := os.WriteFile(path, png, 0o600); err != nil {
			slog.Error("mcp: tool failed", "component", "browser", "tool", "browser_screenshot", "tab_id", args.TabID, "err", err)
			return nil, struct{}{}, fmt.Errorf("writing screenshot: %w", err)
		}

		slog.Info(fmt.Sprintf("screenshot saved: %s", path), "component", "browser", "tool", "browser_screenshot", "tab_id", args.TabID)

		return text(fmt.Sprintf("screenshot saved: %s", path))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "browser_resize",
		Description: "Resize the browser window containing the specified tab (width, height in pixels).",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args browserResizeArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "browser", "tool", "browser_resize", "tab_id", args.TabID, "width", args.Width, "height", args.Height)
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		if args.TabID == "" {
			return nil, struct{}{}, fmt.Errorf("tab_id is required")
		}
		if args.Width <= 0 || args.Height <= 0 {
			return nil, struct{}{}, fmt.Errorf("width and height must be positive")
		}
		if err := d.Browser.Resize(ctx, args.TabID, args.Width, args.Height); err != nil {
			slog.Error("mcp: tool failed", "component", "browser", "tool", "browser_resize", "tab_id", args.TabID, "err", err)
			return nil, struct{}{}, err
		}
		return text(fmt.Sprintf("resized tab %s to %dx%d", args.TabID, args.Width, args.Height))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "browser_eval",
		Description: "Evaluate JavaScript in the specified tab and return the result. Pass the script in the `javascript` field.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args struct {
		TabID      string `json:"tab_id"`
		JavaScript string `json:"javascript"`
	}) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "browser", "tool", "browser_eval", "tab_id", args.TabID)
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		if args.TabID == "" {
			return nil, struct{}{}, fmt.Errorf("tab_id is required")
		}
		result, err := d.Browser.EvalJS(ctx, args.TabID, args.JavaScript)
		if err != nil {
			slog.Error("mcp: tool failed", "component", "browser", "tool", "browser_eval", "tab_id", args.TabID, "err", err)
			return nil, struct{}{}, err
		}
		slog.Info("javascript evaluated", "component", "browser", "tool", "browser_eval", "tab_id", args.TabID)
		return text(result)
	})

	addTool(s, &sdkmcp.Tool{
		Name: "channel_send_file",
		Description: "Send a local file (e.g. a screenshot) to the current conversation channel. " +
			"Use this to share images or files with the user instead of asking them to open a path manually. " +
			"caption is optional text to accompany the file.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args struct {
		FilePath string `json:"file_path"`
		Caption  string `json:"caption,omitempty"`
	}) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "channel", "tool", "channel_send_file", "path", args.FilePath)
		if args.FilePath == "" {
			return nil, struct{}{}, fmt.Errorf("file_path is required")
		}
		sessionID, ok := agent.SessionIDFromContext(ctx)
		if !ok || sessionID == "" {
			return nil, struct{}{}, fmt.Errorf("no active channel session; cannot send file")
		}
		agentID, ok := agent.SessionAgentIDFromContext(ctx)
		if !ok || agentID == "" {
			return nil, struct{}{}, fmt.Errorf("no active session agent; cannot attach file to session")
		}
		if agent.HasSessionMediaDelivery(agentID, sessionID) {
			agent.DeliverMediaToSession(agentID, sessionID, args.Caption, args.FilePath)
			return text(fmt.Sprintf("file sent: %s", args.FilePath))
		}
		mediaURL, err := localFileToDataURL(args.FilePath)
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := agent.AppendMediaMessageToSession(agentID, sessionID, domain.MessageRoleAssistant, args.Caption, mediaURL); err != nil {
			return nil, struct{}{}, fmt.Errorf("persisting session media: %w", err)
		}
		return text(fmt.Sprintf("file sent: %s", args.FilePath))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "browser_close",
		Description: "Close an existing browser tab by tab_id.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args browserTabArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "browser", "tool", "browser_close", "tab_id", args.TabID)
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		if args.TabID == "" {
			return nil, struct{}{}, fmt.Errorf("tab_id is required")
		}
		if err := d.Browser.CloseTab(args.TabID); err != nil {
			slog.Error("mcp: tool failed", "component", "browser", "tool", "browser_close", "tab_id", args.TabID, "err", err)
			return nil, struct{}{}, err
		}
		slog.Info("browser tab closed", "component", "browser", "tool", "browser_close", "tab_id", args.TabID)
		return jsonResult(map[string]any{"tab_id": args.TabID, "closed": true})
	})
}

// ── Auth tools ───────────────────────────────────────────────────────────────

type authSetArgs struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type authNameArgs struct {
	Name string `json:"name"`
}

type authLoginCompleteArgs struct {
	Code string `json:"code"`
}

func authStore() (*auth.FileStore, error) {
	d := GetDeps()
	if d.Auth != nil {
		return d.Auth, nil
	}
	authPath := store.SubDir(store.DirAuth) + "/credentials.json"
	return auth.NewFileStore(authPath)
}

func reconcileAgents() {
	d := GetDeps()
	if d == nil || d.Agents == nil {
		return
	}
	cfg, err := config.Load("")
	if err != nil {
		slog.Warn("mcp: failed to reload config for agent reconcile", "err", err)
		return
	}
	d.Agents.Reconcile(cfg)
}

func registerAuthTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "auth_set",
		Description: "Store a credential by name (e.g. name=anthropic:default, value=sk-ant-...)",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args authSetArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if args.Name == "" {
			return nil, struct{}{}, fmt.Errorf("name is required")
		}
		st, err := authStore()
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := st.Set(args.Name, args.Value); err != nil {
			return nil, struct{}{}, err
		}
		return text(fmt.Sprintf("credential %q stored", args.Name))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "auth_get",
		Description: "Check whether a credential is set (value is masked)",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args authNameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		st, err := authStore()
		if err != nil {
			return nil, struct{}{}, err
		}
		val, err := st.Get(args.Name)
		if err != nil {
			return nil, struct{}{}, err
		}
		masked := "****"
		if len(val) > 4 {
			masked = val[:4] + strings.Repeat("*", len(val)-4)
		}
		return jsonResult(map[string]any{"name": args.Name, "set": true, "preview": masked})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "auth_list",
		Description: "List all stored credential names",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		st, err := authStore()
		if err != nil {
			return nil, struct{}{}, err
		}
		keys, err := st.List()
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(keys)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "auth_delete",
		Description: "Remove a stored credential",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args authNameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		st, err := authStore()
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := st.Delete(args.Name); err != nil {
			return nil, struct{}{}, err
		}
		return text(fmt.Sprintf("credential %q deleted", args.Name))
	})

	addTool(s, &sdkmcp.Tool{
		Name: "auth_login_anthropic",
		Description: "Start Anthropic Claude Pro/Max OAuth login. " +
			"Returns an authorization URL; open it in a browser, complete sign-in, " +
			"then copy the code shown and call auth_login_anthropic_complete with it.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		pkce, err := auth.GeneratePKCE()
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("generating PKCE: %w", err)
		}
		auth.StorePendingPKCE("anthropic", pkce)
		authURL := auth.AnthropicBuildAuthorizeURL(pkce, "max")
		_ = auth.OpenBrowser(authURL)
		return jsonResult(map[string]any{
			"url":          authURL,
			"instructions": "Open the URL in your browser (opened automatically if possible). After signing in, you will be shown an authorization code. Call auth_login_anthropic_complete with that code.",
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "auth_login_anthropic_complete",
		Description: "Complete Anthropic OAuth login by exchanging the authorization code. Call this after auth_login_anthropic with the code shown on the Anthropic page.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args authLoginCompleteArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if args.Code == "" {
			return nil, struct{}{}, fmt.Errorf("code is required")
		}
		pkce, ok := auth.LoadPendingPKCE("anthropic")
		if !ok {
			return nil, struct{}{}, fmt.Errorf("no pending Anthropic login; call auth_login_anthropic first")
		}
		token, err := auth.AnthropicExchange(ctx, args.Code, pkce.Verifier)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("completing Anthropic login: %w", err)
		}
		tokenJSON, _ := json.Marshal(token)
		st, err := authStore()
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := st.Set("anthropic:oauth", string(tokenJSON)); err != nil {
			return nil, struct{}{}, err
		}
		reconcileAgents()
		return text(fmt.Sprintf("Anthropic OAuth login successful. Access token stored (expires %s).", time.UnixMilli(token.ExpiresAt).UTC().Format(time.RFC3339)))
	})

	addTool(s, &sdkmcp.Tool{
		Name: "auth_login_gemini",
		Description: "Start Google Gemini OAuth login (gemini-cli style). Opens the browser to Google's consent screen, " +
			"listens on localhost:45289 for the callback, exchanges the code for tokens, and stores them. " +
			"This enables use of Gemini models via your Google account without an API key.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		loginCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		token, err := auth.GeminiLogin(loginCtx)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("gemini login: %w", err)
		}
		tokenJSON, _ := json.Marshal(token)
		st, err := authStore()
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := st.Set("gemini:oauth", string(tokenJSON)); err != nil {
			return nil, struct{}{}, err
		}
		reconcileAgents()
		return text(fmt.Sprintf("Gemini OAuth login successful. Access token stored (expires %s).", time.UnixMilli(token.ExpiresAt).UTC().Format(time.RFC3339)))
	})

	addTool(s, &sdkmcp.Tool{
		Name: "auth_login_openai",
		Description: "Start OpenAI/Codex OAuth login. Opens the browser to the OpenAI consent screen, " +
			"listens on localhost:1455 for the callback, exchanges the code for tokens, and stores them. " +
			"This enables use of ChatGPT Pro/Plus (Codex) models without an API key.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		loginCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		token, err := auth.OpenAILogin(loginCtx)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("OpenAI login: %w", err)
		}
		tokenJSON, _ := json.Marshal(token)
		st, err := authStore()
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := st.Set("openai:oauth", string(tokenJSON)); err != nil {
			return nil, struct{}{}, err
		}
		reconcileAgents()
		return text(fmt.Sprintf("OpenAI OAuth login successful. Access token stored (expires %s).", time.UnixMilli(token.ExpiresAt).UTC().Format(time.RFC3339)))
	})

	addTool(s, &sdkmcp.Tool{
		Name: "auth_login_github_copilot",
		Description: "Start GitHub Copilot device-flow login. Returns a user_code and verification_uri " +
			"to display to the user; call auth_login_github_copilot_complete to finish.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		state, err := auth.CopilotDeviceCode(ctx)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("copilot device code: %w", err)
		}
		auth.StoreCopilotDeviceState(state)
		return jsonResult(map[string]any{"user_code": state.UserCode, "verification_uri": state.VerificationURI})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "auth_login_github_copilot_complete",
		Description: "Complete GitHub Copilot login after the user has authorized the device code. Polls GitHub until authorization succeeds and stores the token.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		state, ok := auth.LoadCopilotDeviceState()
		if !ok {
			return nil, struct{}{}, fmt.Errorf("no pending Copilot login; call auth_login_github_copilot first")
		}
		pollCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		token, err := auth.CopilotPollDevice(pollCtx, state)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("copilot device poll: %w", err)
		}
		tokenJSON, _ := json.Marshal(token)
		st, err := authStore()
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := st.Set("github-copilot:oauth", string(tokenJSON)); err != nil {
			return nil, struct{}{}, err
		}
		reconcileAgents()
		return text("GitHub Copilot login successful. Token stored as github-copilot:oauth.")
	})
}

// ── Server tools ─────────────────────────────────────────────────────────────

func registerServerTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "server_status",
		Description: "Get server status, uptime, and connected agents",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return jsonResult(map[string]any{"status": "running"})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "server_version_check",
		Description: "Check the current Aviary version against the latest GitHub release",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		check, err := update.Check(ctx, nil)
		if err != nil && check.LatestVersion == "" {
			return nil, struct{}{}, err
		}
		return jsonResult(check)
	})

	type serverUpgradeArgs struct {
		Version string `json:"version,omitempty"`
	}

	addTool(s, &sdkmcp.Tool{
		Name:        "server_upgrade",
		Description: "Upgrade Aviary to the latest release and restart the server if needed",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args serverUpgradeArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if update.EmulationActive() {
			return jsonResult(map[string]any{"started": true, "emulated": true})
		}
		if d.Upgrade == nil {
			return nil, struct{}{}, fmt.Errorf("server upgrade is only available when the Aviary server is running")
		}
		if err := d.Upgrade(ctx, args.Version); err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(map[string]any{"started": true})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "ping",
		Description: "Check server connectivity",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return text("pong")
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "config_get",
		Description: "Get the current server configuration as JSON",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "settings", "tool", "config_get")
		cfg, err := config.Load("")
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(cfg)
	})

	type configSaveArgs struct {
		Config string `json:"config"`
	}

	addTool(s, &sdkmcp.Tool{
		Name:        "config_save",
		Description: "Save an updated server configuration (full JSON-encoded config object)",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args configSaveArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "settings", "tool", "config_save")
		prevCfg, err := config.Load("")
		if err != nil {
			prevCfg = &config.Config{}
		}
		var cfg config.Config
		if err := json.Unmarshal([]byte(args.Config), &cfg); err != nil {
			return nil, struct{}{}, fmt.Errorf("invalid config JSON: %w", err)
		}
		issues := config.Validate(&cfg, nil)
		for _, issue := range issues {
			if issue.Level == config.LevelError {
				return nil, struct{}{}, fmt.Errorf("validation: [%s] %s: %s", issue.Level, issue.Field, issue.Message)
			}
		}
		// Handle task files: detect tasks that were previously defined in
		// markdown but are now removed from the incoming config and delete
		// their files; then write any tasks marked FromFile in the incoming
		// config to markdown files and remove them from the inline config so
		// aviary.yaml does not duplicate file-backed tasks.
		for i := range cfg.Agents {
			// Build set of previous file-backed task names.
			prevFileTasks := map[string]struct{}{}
			if i < len(prevCfg.Agents) {
				for _, t := range prevCfg.Agents[i].Tasks {
					if t.FromFile {
						prevFileTasks[t.Name] = struct{}{}
					}
				}
			}

			// Build set of current task names for quick lookup.
			currTasks := map[string]struct{}{}
			for _, t := range cfg.Agents[i].Tasks {
				currTasks[t.Name] = struct{}{}
			}

			// Any prev file-backed task missing from currTasks should have
			// its markdown file removed.
			if len(prevFileTasks) > 0 {
				tasksDir := config.AgentTasksDir(cfg.Agents[i])
				pattern := filepath.Join(tasksDir, "*.md")
				files, _ := filepath.Glob(pattern)
				for name := range prevFileTasks {
					if _, ok := currTasks[name]; ok {
						continue
					}
					// Find matching file by parsing each markdown file and
					// comparing the parsed task name.
					for _, f := range files {
						tc, terr := config.LoadMarkdownTask(f)
						if terr != nil {
							continue
						}
						if tc.Name == name {
							// Delete via store helper using agent name and
							// relative filename (preserve subdirectory such as "tasks/").
							// Compute the path relative to the agent directory so the
							// store helper deletes the correct file.
							agentDir := store.AgentDir(cfg.Agents[i].Name)
							rel, rerr := filepath.Rel(agentDir, f)
							if rerr != nil {
								return nil, struct{}{}, fmt.Errorf("computing relative task file path: %w", rerr)
							}
							if derr := deleteAgentMarkdownFile(cfg.Agents[i].Name, rel); derr != nil {
								if os.IsNotExist(derr) {
									// Ignore missing files (race or already-removed).
									continue
								}
								return nil, struct{}{}, fmt.Errorf("deleting task file %s: %w", rel, derr)
							}
							break
						}
					}
				}
			}

			// Now write any tasks in the incoming config that are marked
			// FromFile to actual markdown files and remove them from the
			// inline config to avoid duplication.
			var filtered []config.TaskConfig
			for _, t := range cfg.Agents[i].Tasks {
				if t.FromFile {
					dir := config.AgentTasksDir(cfg.Agents[i])
					if _, err := config.SaveMarkdownTask(dir, t); err != nil {
						return nil, struct{}{}, fmt.Errorf("writing task file: %w", err)
					}
					// skip adding to filtered (remove from inline config)
					continue
				}
				filtered = append(filtered, t)
			}
			cfg.Agents[i].Tasks = filtered
		}
		if err := config.Save("", &cfg); err != nil {
			return nil, struct{}{}, err
		}
		if err := store.RenameMatchingAgentDirs(prevCfg, &cfg); err != nil {
			return nil, struct{}{}, err
		}
		if err := store.EnsureNewAgentTemplates(prevCfg, &cfg); err != nil {
			return nil, struct{}{}, err
		}
		if err := store.UpdateChannelMetadataState(prevCfg, &cfg, time.Now().UTC()); err != nil {
			return nil, struct{}{}, err
		}
		return text("config saved")
	})

	type configTaskRenameArgs struct {
		Agent   string `json:"agent"`
		Task    string `json:"task"`
		NewName string `json:"new_name"`
	}

	addTool(s, &sdkmcp.Tool{
		Name:        "config_task_rename",
		Description: "Rename a task: updates aviary.yaml inline task names or renames file-backed task markdown and frontmatter",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args configTaskRenameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "settings", "tool", "config_task_rename", "agent", args.Agent, "task", args.Task, "new", args.NewName)
		if strings.TrimSpace(args.NewName) == "" {
			return nil, struct{}{}, fmt.Errorf("new name must be provided")
		}

		cfg, err := config.Load("")
		if err != nil {
			return nil, struct{}{}, err
		}
		// Keep a copy of previous config for reconciliation helpers later.
		prevCfg := *cfg

		agentIdx := -1
		for i, a := range cfg.Agents {
			if a.Name == args.Agent {
				agentIdx = i
				break
			}
		}
		if agentIdx < 0 {
			return nil, struct{}{}, fmt.Errorf("agent %q not found", args.Agent)
		}

		// Check for existing task with the target name
		for _, t := range cfg.Agents[agentIdx].Tasks {
			if t.Name == args.NewName {
				return nil, struct{}{}, fmt.Errorf("task %q already defined for agent %q", args.NewName, args.Agent)
			}
		}

		// Locate the task to rename
		taskIdx := -1
		for i, t := range cfg.Agents[agentIdx].Tasks {
			if t.Name == args.Task {
				taskIdx = i
				break
			}
		}
		if taskIdx < 0 {
			// As a fallback, try to locate a file in the tasks dir that parses
			// to the requested task name (covers edge cases where FromFile
			// tracking may be inconsistent).
			tasksDir := config.AgentTasksDir(cfg.Agents[agentIdx])
			pattern := filepath.Join(tasksDir, "*.md")
			files, gerr := filepath.Glob(pattern)
			if gerr != nil {
				return nil, struct{}{}, fmt.Errorf("globbing task files: %w", gerr)
			}
			found := false
			for _, f := range files {
				tc, terr := config.LoadMarkdownTask(f)
				if terr != nil {
					continue
				}
				if tc.Name == args.Task {
					found = true
					break
				}
			}
			if !found {
				return nil, struct{}{}, fmt.Errorf("task %q not found in agent %q", args.Task, args.Agent)
			}
			// If found via file scan, proceed by treating it as a file-backed task
			// below by forcing taskIdx to -2 sentinel.
			taskIdx = -2
		}

		// If task is file-backed
		if taskIdx >= 0 && cfg.Agents[agentIdx].Tasks[taskIdx].FromFile || taskIdx == -2 {
			// Locate the markdown file that defines the task
			tasksDir := config.AgentTasksDir(cfg.Agents[agentIdx])
			pattern := filepath.Join(tasksDir, "*.md")
			files, gerr := filepath.Glob(pattern)
			if gerr != nil {
				return nil, struct{}{}, fmt.Errorf("globbing task files: %w", gerr)
			}
			oldPath := ""
			var taskCfg config.TaskConfig
			for _, f := range files {
				tc, terr := config.LoadMarkdownTask(f)
				if terr != nil {
					continue
				}
				if tc.Name == args.Task {
					oldPath = f
					taskCfg = tc
					break
				}
			}
			if oldPath == "" {
				return nil, struct{}{}, fmt.Errorf("task file for %q not found", args.Task)
			}
			// Update task name and write a new file using SaveMarkdownTask (which
			// derives a safe filename). Then remove the old file.
			taskCfg.Name = args.NewName
			newPath, serr := config.SaveMarkdownTask(tasksDir, taskCfg)
			if serr != nil {
				return nil, struct{}{}, fmt.Errorf("writing renamed task file: %w", serr)
			}
			if filepath.Clean(newPath) != filepath.Clean(oldPath) {
				if rerr := os.Remove(oldPath); rerr != nil {
					// Best effort: warn but continue
					slog.Warn("failed to remove old task file after rename", "old", oldPath, "err", rerr)
				}
			}
			// Reload config (including task files) and reconcile runtime state.
			reloadedCfg, reloadErr := config.Load("")
			if reloadErr != nil {
				return nil, struct{}{}, reloadErr
			}
			deps := GetDeps()
			if deps != nil && deps.Agents != nil {
				deps.Agents.Reconcile(reloadedCfg)
			}
			if deps != nil && deps.Scheduler != nil {
				deps.Scheduler.Reconcile(reloadedCfg)
			}
			return text(fmt.Sprintf("task %q renamed to %q (file: %s)", args.Task, args.NewName, filepath.Base(newPath)))
		}

		// Inline task in aviary.yaml: just rename and save config
		if taskIdx >= 0 {
			cfg.Agents[agentIdx].Tasks[taskIdx].Name = args.NewName
			// Save config and run the same follow-up actions as config_save
			if err := config.Save("", cfg); err != nil {
				return nil, struct{}{}, err
			}
			if err := store.RenameMatchingAgentDirs(&prevCfg, cfg); err != nil {
				return nil, struct{}{}, err
			}
			if err := store.EnsureNewAgentTemplates(&prevCfg, cfg); err != nil {
				return nil, struct{}{}, err
			}
			if err := store.UpdateChannelMetadataState(&prevCfg, cfg, time.Now().UTC()); err != nil {
				return nil, struct{}{}, err
			}
			return text(fmt.Sprintf("task %q renamed to %q", args.Task, args.NewName))
		}

		return nil, struct{}{}, fmt.Errorf("unexpected error locating task %q", args.Task)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "config_restore_latest_backup",
		Description: "Restore aviary.yaml from the most recent rotating backup file",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "settings", "tool", "config_restore_latest_backup")
		if err := config.RestoreLatestBackup(""); err != nil {
			return nil, struct{}{}, err
		}
		if _, err := config.Load(""); err != nil {
			return nil, struct{}{}, fmt.Errorf("loading restored config: %w", err)
		}
		return text("latest config backup restored")
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "config_validate",
		Description: "Validate the current configuration and credentials, returning all issues. Provider connectivity is checked asynchronously; results appear on subsequent calls.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "settings", "tool", "config_validate")
		cfg, err := config.Load("")
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("loading config: %w", err)
		}
		st, err := authStore()
		var authGet func(string) (string, error)
		if err == nil {
			authGet = func(key string) (string, error) { return st.Get(key) }
		}
		type issueDTO struct {
			Level   string `json:"level"`
			Field   string `json:"field"`
			Message string `json:"message"`
		}
		issues := config.Validate(cfg, authGet)
		out := make([]issueDTO, len(issues))
		for i, iss := range issues {
			out[i] = issueDTO{Level: string(iss.Level), Field: iss.Field, Message: iss.Message}
		}
		pingFactory := llm.NewFactory(func(ref string) (string, error) {
			if authGet == nil {
				return "", nil
			}
			return authGet(strings.TrimPrefix(ref, "auth:"))
		})
		for provider, model := range config.UniqueProviderModels(cfg) {
			startProviderPingIfStale(provider, model, pingFactory)
			providerPingMu.RLock()
			entry, cached := providerPingCache[provider]
			providerPingMu.RUnlock()
			if cached && !entry.ok {
				out = append(out, issueDTO{Level: string(config.LevelError), Field: "models." + provider, Message: entry.errMsg})
			}
		}
		return jsonResult(out)
	})

	type configTaskMoveToFileArgs struct {
		Agent string `json:"agent"`
		Task  string `json:"task"`
	}

	addTool(s, &sdkmcp.Tool{
		Name:        "config_task_move_to_file",
		Description: "Move a task defined in aviary.yaml to a markdown file in the agent's tasks/ directory. The task is removed from aviary.yaml and written as <task-name>.md. Only tasks defined directly in aviary.yaml can be moved; file-based tasks are already in files.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args configTaskMoveToFileArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "settings", "tool", "config_task_move_to_file", "agent", args.Agent, "task", args.Task)
		cfg, err := config.Load("")
		if err != nil {
			return nil, struct{}{}, err
		}
		agentIdx := -1
		for i, a := range cfg.Agents {
			if a.Name == args.Agent {
				agentIdx = i
				break
			}
		}
		if agentIdx < 0 {
			return nil, struct{}{}, fmt.Errorf("agent %q not found", args.Agent)
		}
		taskIdx := -1
		for i, t := range cfg.Agents[agentIdx].Tasks {
			if t.Name == args.Task {
				taskIdx = i
				break
			}
		}
		if taskIdx < 0 {
			return nil, struct{}{}, fmt.Errorf("task %q not found in agent %q (only tasks defined in aviary.yaml can be moved to files)", args.Task, args.Agent)
		}

		// If the task was loaded from a file, it is already defined as a task
		// markdown file and should not be moved again.
		if cfg.Agents[agentIdx].Tasks[taskIdx].FromFile {
			return nil, struct{}{}, fmt.Errorf("task %q is already defined as a file", args.Task)
		}
		task := cfg.Agents[agentIdx].Tasks[taskIdx]
		dir := config.AgentTasksDir(cfg.Agents[agentIdx])
		path, err := config.SaveMarkdownTask(dir, task)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("writing task file: %w", err)
		}
		cfg.Agents[agentIdx].Tasks = append(cfg.Agents[agentIdx].Tasks[:taskIdx], cfg.Agents[agentIdx].Tasks[taskIdx+1:]...)
		if err := config.Save("", cfg); err != nil {
			return nil, struct{}{}, fmt.Errorf("saving config: %w", err)
		}
		return text(fmt.Sprintf("task %q moved to %s", args.Task, path))
	})

	type configTaskConvertToScriptArgs struct {
		Agent string `json:"agent"`
		Task  string `json:"task"`
	}

	addTool(s, &sdkmcp.Tool{
		Name:        "config_task_convert_to_script",
		Description: "Attempt to compile a PROMPT-type task to a Lua script asynchronously; returns a compile ID immediately",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args configTaskConvertToScriptArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "settings", "tool", "config_task_convert_to_script", "agent", args.Agent, "task", args.Task)
		cfg, err := config.Load("")
		if err != nil {
			return nil, struct{}{}, err
		}
		agentIdx := -1
		for i, a := range cfg.Agents {
			if a.Name == args.Agent {
				agentIdx = i
				break
			}
		}
		if agentIdx < 0 {
			return nil, struct{}{}, fmt.Errorf("agent %q not found", args.Agent)
		}

		// Locate task and determine prompt/body and whether it's file-backed.
		taskIdx := -1
		for i, t := range cfg.Agents[agentIdx].Tasks {
			if t.Name == args.Task {
				taskIdx = i
				break
			}
		}
		if taskIdx < 0 {
			// Try to find via markdown files as a fallback.
			tasksDir := config.AgentTasksDir(cfg.Agents[agentIdx])
			pattern := filepath.Join(tasksDir, "*.md")
			files, gerr := filepath.Glob(pattern)
			if gerr != nil {
				return nil, struct{}{}, fmt.Errorf("globbing task files: %w", gerr)
			}
			found := false
			var taskCfg config.TaskConfig
			for _, f := range files {
				tc, terr := config.LoadMarkdownTask(f)
				if terr != nil {
					continue
				}
				if tc.Name == args.Task {
					taskCfg = tc
					found = true
					break
				}
			}
			if !found {
				return nil, struct{}{}, fmt.Errorf("task %q not found in agent %q", args.Task, args.Agent)
			}
			// For file-backed tasks found via file, treat as file-backed and use that prompt body.
			prompt := taskCfg.Prompt
			target := taskCfg.Target
			tracker := newTaskCompileTracker(args.Agent, args.Task, "prompt", prompt, target, "manual", false)
			if err := tracker.persist(); err != nil {
				slog.Warn("task_compile: failed to persist initial record", "component", "task_compile", "agent", args.Agent, "err", err)
			}
			go func() {
				ctx2 := withTaskCompileTracker(context.Background(), tracker)
				compiled, cerr := resolveTryCompileTaskPrompt()(ctx2, args.Agent, prompt, target, false)
				if cerr != nil {
					tracker.record.Status = domain.TaskCompileStatusFailed
					tracker.record.Reason = cerr.Error()
					_ = tracker.persist()
					return
				}
				tracker.record.ResultTaskType = compiled.Type
				tracker.record.NeedsDiscovery = compiled.NeedsDiscovery
				tracker.record.Validated = compiled.Validated
				tracker.record.Reason = strings.TrimSpace(compiled.Reason)
				tracker.record.Steps = toDomainCompileSteps(compiled.Steps)
				tracker.record.Script = strings.TrimSpace(compiled.Script)
				if compiled.Type == "script" && strings.TrimSpace(compiled.Script) != "" && !compiled.NeedsDiscovery {
					// Overwrite the existing markdown file with the script body.
					nextTask := taskCfg
					nextTask.Type = "script"
					nextTask.Prompt = strings.TrimSpace(compiled.Script)
					tasksDir := config.AgentTasksDir(cfg.Agents[agentIdx])
					if _, serr := config.SaveMarkdownTask(tasksDir, nextTask); serr != nil {
						tracker.record.Status = domain.TaskCompileStatusFailed
						tracker.record.Reason = serr.Error()
						_ = tracker.persist()
						return
					}
					tracker.record.Status = domain.TaskCompileStatusSucceeded
					_ = tracker.persist()
					// Reload and reconcile runtime state.
					reloadedCfg, reloadErr := config.Load("")
					if reloadErr == nil {
						deps := GetDeps()
						if deps != nil && deps.Agents != nil {
							deps.Agents.Reconcile(reloadedCfg)
						}
						if deps != nil && deps.Scheduler != nil {
							deps.Scheduler.Reconcile(reloadedCfg)
						}
					}
					return
				}
				// If not promoted to script, mark as skipped.
				tracker.record.Status = domain.TaskCompileStatusSkipped
				_ = tracker.persist()
			}()
			return text(tracker.record.ID)
		}

		// Found inline task in aviary.yaml
		task := cfg.Agents[agentIdx].Tasks[taskIdx]
		if task.FromFile {
			// If FromFile is true, treat similar to file-backed case by loading file.
			tasksDir := config.AgentTasksDir(cfg.Agents[agentIdx])
			pattern := filepath.Join(tasksDir, "*.md")
			files, gerr := filepath.Glob(pattern)
			if gerr != nil {
				return nil, struct{}{}, fmt.Errorf("globbing task files: %w", gerr)
			}
			found := false
			var taskCfg config.TaskConfig
			for _, f := range files {
				tc, terr := config.LoadMarkdownTask(f)
				if terr != nil {
					continue
				}
				if tc.Name == args.Task {
					taskCfg = tc
					found = true
					break
				}
			}
			if !found {
				return nil, struct{}{}, fmt.Errorf("task %q not found in agent %q", args.Task, args.Agent)
			}
			prompt := taskCfg.Prompt
			target := taskCfg.Target
			tracker := newTaskCompileTracker(args.Agent, args.Task, "prompt", prompt, target, "manual", false)
			if err := tracker.persist(); err != nil {
				slog.Warn("task_compile: failed to persist initial record", "component", "task_compile", "agent", args.Agent, "err", err)
			}
			go func() {
				ctx2 := withTaskCompileTracker(context.Background(), tracker)
				compiled, cerr := resolveTryCompileTaskPrompt()(ctx2, args.Agent, prompt, target, false)
				if cerr != nil {
					tracker.record.Status = domain.TaskCompileStatusFailed
					tracker.record.Reason = cerr.Error()
					_ = tracker.persist()
					return
				}
				tracker.record.ResultTaskType = compiled.Type
				tracker.record.NeedsDiscovery = compiled.NeedsDiscovery
				tracker.record.Validated = compiled.Validated
				tracker.record.Reason = strings.TrimSpace(compiled.Reason)
				tracker.record.Steps = toDomainCompileSteps(compiled.Steps)
				tracker.record.Script = strings.TrimSpace(compiled.Script)
				if compiled.Type == "script" && strings.TrimSpace(compiled.Script) != "" && !compiled.NeedsDiscovery {
					// Update the markdown file backing this task with the script.
					nextTask := taskCfg
					nextTask.Type = "script"
					nextTask.Prompt = strings.TrimSpace(compiled.Script)
					tasksDir := config.AgentTasksDir(cfg.Agents[agentIdx])
					if _, serr := config.SaveMarkdownTask(tasksDir, nextTask); serr != nil {
						tracker.record.Status = domain.TaskCompileStatusFailed
						tracker.record.Reason = serr.Error()
						_ = tracker.persist()
						return
					}
					tracker.record.Status = domain.TaskCompileStatusSucceeded
					_ = tracker.persist()
					reloadedCfg, reloadErr := config.Load("")
					if reloadErr == nil {
						deps := GetDeps()
						if deps != nil && deps.Agents != nil {
							deps.Agents.Reconcile(reloadedCfg)
						}
						if deps != nil && deps.Scheduler != nil {
							deps.Scheduler.Reconcile(reloadedCfg)
						}
					}
					return
				}
				tracker.record.Status = domain.TaskCompileStatusSkipped
				_ = tracker.persist()
			}()
			return text(tracker.record.ID)
		}

		// Inline prompt task: attempt to compile and, on success, update aviary.yaml
		prompt := task.Prompt
		target := task.Target
		tracker := newTaskCompileTracker(args.Agent, args.Task, "prompt", prompt, target, "manual", false)
		if err := tracker.persist(); err != nil {
			slog.Warn("task_compile: failed to persist initial record", "component", "task_compile", "agent", args.Agent, "err", err)
		}
		go func() {
			ctx2 := withTaskCompileTracker(context.Background(), tracker)
			compiled, cerr := resolveTryCompileTaskPrompt()(ctx2, args.Agent, prompt, target, false)
			if cerr != nil {
				tracker.record.Status = domain.TaskCompileStatusFailed
				tracker.record.Reason = cerr.Error()
				_ = tracker.persist()
				return
			}
			tracker.record.ResultTaskType = compiled.Type
			tracker.record.NeedsDiscovery = compiled.NeedsDiscovery
			tracker.record.Validated = compiled.Validated
			tracker.record.Reason = strings.TrimSpace(compiled.Reason)
			tracker.record.Steps = toDomainCompileSteps(compiled.Steps)
			tracker.record.Script = strings.TrimSpace(compiled.Script)
			if compiled.Type == "script" && strings.TrimSpace(compiled.Script) != "" && !compiled.NeedsDiscovery {
				// Update inline task in aviary.yaml to script type and store Lua in Prompt
				cfg2, lerr := config.Load("")
				if lerr != nil {
					tracker.record.Status = domain.TaskCompileStatusFailed
					tracker.record.Reason = lerr.Error()
					_ = tracker.persist()
					return
				}
				agentIdx2 := -1
				for i, a := range cfg2.Agents {
					if a.Name == args.Agent {
						agentIdx2 = i
						break
					}
				}
				if agentIdx2 < 0 {
					tracker.record.Status = domain.TaskCompileStatusFailed
					tracker.record.Reason = fmt.Sprintf("agent %q not found during save", args.Agent)
					_ = tracker.persist()
					return
				}
				for i := range cfg2.Agents[agentIdx2].Tasks {
					if cfg2.Agents[agentIdx2].Tasks[i].Name == args.Task {
						cfg2.Agents[agentIdx2].Tasks[i].Type = "script"
						cfg2.Agents[agentIdx2].Tasks[i].Prompt = strings.TrimSpace(compiled.Script)
						break
					}
				}
				if serr := config.Save("", cfg2); serr != nil {
					tracker.record.Status = domain.TaskCompileStatusFailed
					tracker.record.Reason = serr.Error()
					_ = tracker.persist()
					return
				}
				tracker.record.Status = domain.TaskCompileStatusSucceeded
				_ = tracker.persist()
				// Reconcile runtime state
				deps := GetDeps()
				if deps != nil && deps.Agents != nil {
					deps.Agents.Reconcile(cfg2)
				}
				if deps != nil && deps.Scheduler != nil {
					deps.Scheduler.Reconcile(cfg2)
				}
				return
			}
			tracker.record.Status = domain.TaskCompileStatusSkipped
			_ = tracker.persist()
		}()
		return text(tracker.record.ID)
	})
}

// ── Usage tools ──────────────────────────────────────────────────────────────

type usageQueryArgs struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

func registerUsageTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "usage_query",
		Description: "Return raw token-usage records within a date range for display in the Usage dashboard",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args usageQueryArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		now := time.Now()
		start := now.AddDate(0, 0, -30)
		end := now
		parse := func(s string, fallback time.Time) time.Time {
			if s == "" {
				return fallback
			}
			for _, layout := range []string{time.RFC3339, "2006-01-02"} {
				if t, err := time.Parse(layout, s); err == nil {
					return t
				}
			}
			return fallback
		}
		start = parse(args.Start, start)
		end = parse(args.End, end)
		if len(args.End) == 10 {
			end = end.AddDate(0, 0, 1)
		}
		records, err := store.ReadJSONL[domain.UsageRecord](store.UsagePath())
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("reading usage log: %w", err)
		}
		filtered := records[:0]
		for _, r := range records {
			if (r.Timestamp.Equal(start) || r.Timestamp.After(start)) && r.Timestamp.Before(end) {
				filtered = append(filtered, r)
			}
		}
		return jsonResult(filtered)
	})
}

// ── Skill tools ──────────────────────────────────────────────────────────────

func registerSkillTools(s *sdkmcp.Server) {
	registerConfiguredSkillTools(s)
	addTool(s, &sdkmcp.Tool{
		Name:        "skills_list",
		Description: "List installed skills and whether they are enabled in configuration",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		cfg, err := config.Load("")
		if err != nil {
			return nil, struct{}{}, err
		}
		list, err := skills.ListInstalled(cfg)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(list)
	})
}
