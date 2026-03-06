package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/domain"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
)

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
	registerTaskTools(s)
	registerJobTools(s)
	registerBrowserTools(s)
	registerMemoryTools(s)
	registerAuthTools(s)
	registerServerTools(s)
	registerUsageTools(s)
}

// ── Agent tools ──────────────────────────────────────────────────────────────

type agentRunArgs struct {
	Name     string `json:"name"`
	Message  string `json:"message"`
	Session  string `json:"session,omitempty"`  // session name; defaults to "main"
	File     string `json:"file,omitempty"`
	MediaURL string `json:"media_url,omitempty"` // optional image (data URL or remote URL)
}

type agentNameArgs struct {
	Name string `json:"name"`
}

func registerAgentTools(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "agent_list",
		Description: "List all configured agents and their current state",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Agents == nil {
			return nil, struct{}{}, fmt.Errorf("agent manager not initialized; is the server running?")
		}
		return jsonResult(d.Agents.List())
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "agent_run",
		Description: "Send a message to an agent and stream the response",
	}, func(ctx context.Context, req *sdkmcp.CallToolRequest, args agentRunArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "chat", "tool", "agent_run", "agent", args.Name, "session", args.Session)
		d := GetDeps()
		if d.Agents == nil {
			return nil, struct{}{}, fmt.Errorf("agent manager not initialized; is the server running?")
		}
		runner, ok := d.Agents.Get(args.Name)
		if !ok {
			return nil, struct{}{}, fmt.Errorf("agent %q not found", args.Name)
		}
		// Ensure the session exists (defaults to "main").
		agentID := fmt.Sprintf("agent_%s", args.Name)
		sess, err := agent.NewSessionManager().GetOrCreateNamed(agentID, args.Session)
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("initializing session: %w", err)
		}
		if isStopCommand(args.Message) {
			stopped := agent.StopSession(sess.ID)
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
		runner.PromptMedia(ctx, args.Message, args.MediaURL, func(e agent.StreamEvent) {
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
			return nil, struct{}{}, err
		}
		slog.Info("mcp: tool done", "component", "chat", "tool", "agent_run", "agent", args.Name)
		return text(buf.String())
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "agent_stop",
		Description: "Immediately stop all work in progress for an agent",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args agentNameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Agents == nil {
			return nil, struct{}{}, fmt.Errorf("agent manager not initialized; is the server running?")
		}
		runner, ok := d.Agents.Get(args.Name)
		if !ok {
			return nil, struct{}{}, fmt.Errorf("agent %q not found", args.Name)
		}
		runner.Stop()
		return text(fmt.Sprintf("agent %q stopped", args.Name))
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "agent_get",
		Description: "Get the full configuration for a named agent",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args agentNameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		cfg, err := config.Load("")
		if err != nil {
			return nil, struct{}{}, err
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

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "agent_add",
		Description: "Add a new agent to the configuration",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args agentUpsertArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if args.Name == "" {
			return nil, struct{}{}, fmt.Errorf("name is required")
		}
		cfg, err := config.Load("")
		if err != nil {
			return nil, struct{}{}, err
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
		return text(fmt.Sprintf("agent %q added", args.Name))
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "agent_update",
		Description: "Update an existing agent's configuration fields",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args agentUpsertArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if args.Name == "" {
			return nil, struct{}{}, fmt.Errorf("name is required")
		}
		cfg, err := config.Load("")
		if err != nil {
			return nil, struct{}{}, err
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
		return text(fmt.Sprintf("agent %q updated", args.Name))
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "agent_delete",
		Description: "Remove an agent from the configuration",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args agentNameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
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
		return text(fmt.Sprintf("agent %q deleted", args.Name))
	})
}

// ── Rules tools ──────────────────────────────────────────────────────────────

type agentRulesSetArgs struct {
	Agent   string `json:"agent"`
	Content string `json:"content"`
}

func registerRulesTools(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "agent_rules_get",
		Description: "Read the RULES.md file for an agent (returns empty string if none)",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args agentNameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if args.Name == "" {
			return nil, struct{}{}, fmt.Errorf("agent name is required")
		}
		agentID := fmt.Sprintf("agent_%s", args.Name)
		path := store.AgentRulesPath(agentID)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return text("") // no rules file yet — not an error
			}
			return nil, struct{}{}, fmt.Errorf("reading rules: %w", err)
		}
		return text(string(data))
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "agent_rules_set",
		Description: "Write the RULES.md file for an agent (creates or replaces)",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args agentRulesSetArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if args.Agent == "" {
			return nil, struct{}{}, fmt.Errorf("agent name is required")
		}
		agentID := fmt.Sprintf("agent_%s", args.Agent)
		path := store.AgentRulesPath(agentID)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, struct{}{}, fmt.Errorf("creating agent dir: %w", err)
		}
		if err := os.WriteFile(path, []byte(args.Content), 0o600); err != nil {
			return nil, struct{}{}, fmt.Errorf("writing rules: %w", err)
		}
		return text(fmt.Sprintf("RULES.md written for agent %q", args.Agent))
	})
}

type sessionAgentArgs struct {
	Agent string `json:"agent"`
}

type sessionMessagesArgs struct {
	SessionID string `json:"session_id"`
}

type sessionStopArgs struct {
	SessionID string `json:"session_id,omitempty"`
	Agent     string `json:"agent,omitempty"`
	Session   string `json:"session,omitempty"`
}

func registerSessionTools(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "session_list",
		Description: "List all sessions for an agent",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args sessionAgentArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "chat_session", "tool", "session_list", "agent", args.Agent)
		if args.Agent == "" {
			return nil, struct{}{}, fmt.Errorf("agent name is required")
		}
		agentID := fmt.Sprintf("agent_%s", args.Agent)
		sm := agent.NewSessionManager()
		// Ensure the main session exists.
		if _, err := sm.GetOrCreateNamed(agentID, "main"); err != nil {
			return nil, struct{}{}, err
		}
		sessions, err := sm.List(agentID)
		if err != nil {
			return nil, struct{}{}, err
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
				IsProcessing: agent.IsSessionProcessing(sess.ID),
			})
		}
		return jsonResult(out)
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "session_create",
		Description: "Create a new session for an agent",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args sessionAgentArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "chat_session", "tool", "session_create", "agent", args.Agent)
		if args.Agent == "" {
			return nil, struct{}{}, fmt.Errorf("agent name is required")
		}
		agentID := fmt.Sprintf("agent_%s", args.Agent)
		sm := agent.NewSessionManager()
		sess, err := sm.Create(agentID)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(sess)
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "session_messages",
		Description: "List persisted messages for a session (user/assistant/system)",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args sessionMessagesArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if args.SessionID == "" {
			return nil, struct{}{}, fmt.Errorf("session_id is required")
		}
		lines, err := store.ReadJSONL[map[string]any](store.FindSessionPath(args.SessionID))
		if err != nil {
			return nil, struct{}{}, err
		}

		type messageDTO struct {
			ID        string `json:"id"`
			SessionID string `json:"session_id"`
			Role      string `json:"role"`
			Content   string `json:"content"`
			MediaURL  string `json:"media_url,omitempty"`
			Timestamp string `json:"timestamp"`
		}

		out := make([]messageDTO, 0, len(lines))
		for _, line := range lines {
			role, _ := line["role"].(string)
			content, _ := line["content"].(string)
			mediaURLVal, _ := line["media_url"].(string)
			if role == "" || (content == "" && mediaURLVal == "") {
				continue
			}
			if role != string(domain.MessageRoleUser) && role != string(domain.MessageRoleAssistant) && role != string(domain.MessageRoleSystem) {
				continue
			}

			id, _ := line["id"].(string)
			sid, _ := line["session_id"].(string)
			ts := ""
			switch v := line["timestamp"].(type) {
			case string:
				ts = v
			}
			if sid == "" {
				sid = args.SessionID
			}

			out = append(out, messageDTO{
				ID:        id,
				SessionID: sid,
				Role:      role,
				Content:   content,
				MediaURL:  mediaURLVal,
				Timestamp: ts,
			})
		}

		return jsonResult(out)
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "session_stop",
		Description: "Stop all in-progress work for a specific session",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args sessionStopArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		sid := strings.TrimSpace(args.SessionID)
		if sid == "" {
			if fromCtx, ok := agent.SessionIDFromContext(ctx); ok {
				sid = fromCtx
			}
		}
		if sid == "" && strings.TrimSpace(args.Agent) != "" {
			sessionName := strings.TrimSpace(args.Session)
			agentID := fmt.Sprintf("agent_%s", strings.TrimSpace(args.Agent))
			sess, err := agent.NewSessionManager().GetOrCreateNamed(agentID, sessionName)
			if err != nil {
				return nil, struct{}{}, err
			}
			sid = sess.ID
		}
		if sid == "" {
			return nil, struct{}{}, fmt.Errorf("session_id is required")
		}
		stopped := agent.StopSession(sid)
		if stopped == 0 {
			return text(fmt.Sprintf("session %q has no active work", sid))
		}
		return text(fmt.Sprintf("stopped %d active run(s) in session %q", stopped, sid))
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

type taskScheduleArgs struct {
	Agent  string `json:"agent"`
	Prompt string `json:"prompt"`
	In     string `json:"in,omitempty"` // duration: "5m", "1h", "30s", "5 minutes", etc.
}

func registerTaskTools(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "task_list",
		Description: "List all tasks, their trigger type, and last run status",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "scheduler", "tool", "task_list")
		d := GetDeps()
		if d.Scheduler == nil {
			return nil, struct{}{}, fmt.Errorf("scheduler not initialized")
		}
		jobs, err := d.Scheduler.Queue().List("")
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(jobs)
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
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

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "task_schedule",
		Description: "Schedule a one-time task (arguments: agent=<your-agent-name>, prompt=<what to do>, in=<optional delay e.g. '5m', '1h30m', '30 seconds'>)",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args taskScheduleArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "scheduler", "tool", "task_schedule", "agent", args.Agent, "in", args.In)
		d := GetDeps()
		if d.Scheduler == nil {
			return nil, struct{}{}, fmt.Errorf("scheduler not initialized")
		}
		if args.Agent == "" {
			return nil, struct{}{}, fmt.Errorf("agent is required")
		}
		if args.Prompt == "" {
			return nil, struct{}{}, fmt.Errorf("prompt is required")
		}
		if d.Agents != nil {
			if _, ok := d.Agents.Get(args.Agent); !ok {
				return nil, struct{}{}, fmt.Errorf("agent %q not found; use agent_list to see available agents", args.Agent)
			}
		}
		agentID := fmt.Sprintf("agent_%s", args.Agent)
		taskID := fmt.Sprintf("oneshot/%s", args.Agent)

		var job *domain.Job
		var err error
		if args.In != "" {
			delay, parseErr := parseDuration(args.In)
			if parseErr != nil {
				return nil, struct{}{}, fmt.Errorf("invalid duration %q: %w", args.In, parseErr)
			}
			job, err = d.Scheduler.Queue().EnqueueAt(taskID, agentID, args.Agent, args.Prompt, 1, time.Now().Add(delay))
		} else {
			job, err = d.Scheduler.Queue().Enqueue(taskID, agentID, args.Agent, args.Prompt, 1)
		}
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(job)
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "task_stop",
		Description: "Stop all currently running scheduled task jobs",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return stub("task_stop")
	})
}

// parseDuration parses a duration string, accepting both Go format ("5m", "1h30m")
// and natural language ("5 minutes", "1 hour", "30 seconds").
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

// ── Job tools ────────────────────────────────────────────────────────────────

type jobListArgs struct {
	Task string `json:"task,omitempty"`
}

type jobIDArgs struct {
	ID string `json:"id"`
}

func registerJobTools(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
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

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "job_logs",
		Description: "Show output for a specific job run",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args jobIDArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return stub(fmt.Sprintf("job_logs(%s): job output not yet persisted (Phase 7)", args.ID))
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

func registerBrowserTools(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "browser_open",
		Description: "Navigate to a URL in a new browser tab. Returns the tab_id needed for subsequent operations on that tab.",
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

	sdkmcp.AddTool(s, &sdkmcp.Tool{
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

	sdkmcp.AddTool(s, &sdkmcp.Tool{
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

	sdkmcp.AddTool(s, &sdkmcp.Tool{
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

	sdkmcp.AddTool(s, &sdkmcp.Tool{
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

	sdkmcp.AddTool(s, &sdkmcp.Tool{
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

		screenshotDir := store.ScreenshotDir()
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

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "browser_eval",
		Description: "Evaluate JavaScript in the specified tab and return the result",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args struct {
		TabID string `json:"tab_id"`
		Expr  string `json:"expr"`
	}) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "browser", "tool", "browser_eval", "tab_id", args.TabID)
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		if args.TabID == "" {
			return nil, struct{}{}, fmt.Errorf("tab_id is required")
		}
		result, err := d.Browser.EvalJS(ctx, args.TabID, args.Expr)
		if err != nil {
			slog.Error("mcp: tool failed", "component", "browser", "tool", "browser_eval", "tab_id", args.TabID, "err", err)
			return nil, struct{}{}, err
		}
		slog.Info("javascript evaluated", "component", "browser", "tool", "browser_eval", "tab_id", args.TabID)
		return text(result)
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "browser_close",
		Description: "Close the browser manager (no-op: Chrome and tabs run independently)",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "browser", "tool", "browser_close")
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		d.Browser.Close()
		slog.Info("browser manager closed", "component", "browser", "tool", "browser_close")
		return text("browser closed")
	})
}

// ── Memory tools ─────────────────────────────────────────────────────────────

type memoryAgentQueryArgs struct {
	Agent string `json:"agent"`
	Query string `json:"query"`
}

type memoryAgentArgs struct {
	Agent string `json:"agent"`
}

type memoryStoreArgs struct {
	Agent   string `json:"agent"`
	Content string `json:"content"`
}

type memoryNotesSetArgs struct {
	Agent   string `json:"agent"`
	Content string `json:"content"`
}

func registerMemoryTools(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "memory_search",
		Description: "Search an agent's notes for lines matching a keyword query",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args memoryAgentQueryArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Memory == nil {
			return nil, struct{}{}, fmt.Errorf("memory manager not initialized")
		}
		poolID := "private:" + args.Agent
		notes, err := d.Memory.GetNotes(poolID)
		if err != nil {
			return nil, struct{}{}, err
		}
		if strings.TrimSpace(args.Query) == "" {
			return text(notes)
		}
		terms := strings.Fields(strings.ToLower(args.Query))
		var matched []string
		for _, line := range strings.Split(notes, "\n") {
			lower := strings.ToLower(line)
			ok := true
			for _, t := range terms {
				if !strings.Contains(lower, t) {
					ok = false
					break
				}
			}
			if ok && strings.TrimSpace(line) != "" {
				matched = append(matched, line)
			}
		}
		return text(strings.Join(matched, "\n"))
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "memory_show",
		Description: "Display the full notes memory for an agent",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args memoryAgentArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Memory == nil {
			return nil, struct{}{}, fmt.Errorf("memory manager not initialized")
		}
		poolID := "private:" + args.Agent
		notes, err := d.Memory.GetNotes(poolID)
		if err != nil {
			return nil, struct{}{}, err
		}
		return text(notes)
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "memory_store",
		Description: "Store a fact or note into an agent's persistent notes. Arguments: agent (string, required) - the agent name; content (string, required) - the text to remember.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args memoryStoreArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Memory == nil {
			return nil, struct{}{}, fmt.Errorf("memory manager not initialized")
		}
		poolID := "private:" + args.Agent
		if err := d.Memory.AppendNote(poolID, args.Content); err != nil {
			return nil, struct{}{}, err
		}
		return text(fmt.Sprintf("remembered: %s", args.Content))
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "memory_notes_set",
		Description: "Replace the entire notes file for an agent with new content (markdown text). Use this to edit or correct existing memories.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args memoryNotesSetArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Memory == nil {
			return nil, struct{}{}, fmt.Errorf("memory manager not initialized")
		}
		poolID := "private:" + args.Agent
		if err := d.Memory.SetNotes(poolID, args.Content); err != nil {
			return nil, struct{}{}, err
		}
		return text("notes updated")
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "memory_clear",
		Description: "Wipe all memory (notes and conversation history) for an agent",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args memoryAgentArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Memory == nil {
			return nil, struct{}{}, fmt.Errorf("memory manager not initialized")
		}
		poolID := "private:" + args.Agent
		if err := d.Memory.Clear(poolID); err != nil {
			return nil, struct{}{}, err
		}
		if err := d.Memory.SetNotes(poolID, ""); err != nil {
			return nil, struct{}{}, err
		}
		return text(fmt.Sprintf("memory cleared for agent %q", args.Agent))
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

// authStore returns the auth FileStore from Deps, opening it lazily if needed.
func authStore() (*auth.FileStore, error) {
	d := GetDeps()
	if d.Auth != nil {
		return d.Auth, nil
	}
	// Fallback: open directly (e.g. during tests or before server wires up Deps).
	authPath := store.SubDir(store.DirAuth) + "/credentials.json"
	return auth.NewFileStore(authPath)
}

func registerAuthTools(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "auth_set",
		Description: "Store a credential by name (e.g. name=auth:anthropic:default, value=sk-ant-...)",
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

	sdkmcp.AddTool(s, &sdkmcp.Tool{
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

	sdkmcp.AddTool(s, &sdkmcp.Tool{
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

	sdkmcp.AddTool(s, &sdkmcp.Tool{
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

	// ── OAuth login: Anthropic Claude Pro/Max ────────────────────────────────

	sdkmcp.AddTool(s, &sdkmcp.Tool{
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

		// Try to open the browser automatically (best-effort).
		_ = auth.OpenBrowser(authURL)

		return jsonResult(map[string]any{
			"url":          authURL,
			"instructions": "Open the URL in your browser (opened automatically if possible). After signing in, you will be shown an authorization code. Call auth_login_anthropic_complete with that code.",
		})
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
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

		// Persist the OAuth token as JSON under auth:anthropic:oauth.
		tokenJSON, _ := json.Marshal(token)
		st, err := authStore()
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := st.Set("anthropic:oauth", string(tokenJSON)); err != nil {
			return nil, struct{}{}, err
		}

		return text(fmt.Sprintf("Anthropic OAuth login successful. Access token stored (expires %s).",
			time.UnixMilli(token.ExpiresAt).UTC().Format(time.RFC3339)))
	})

	// ── OAuth login: OpenAI / Codex ──────────────────────────────────────────

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name: "auth_login_openai",
		Description: "Start OpenAI/Codex OAuth login. Opens the browser to the OpenAI consent screen, " +
			"listens on localhost:1455 for the callback, exchanges the code for tokens, and stores them. " +
			"This enables use of ChatGPT Pro/Plus (Codex) models without an API key.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		// Give the user 5 minutes to complete the browser flow.
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

		return text(fmt.Sprintf("OpenAI OAuth login successful. Access token stored (expires %s).",
			time.UnixMilli(token.ExpiresAt).UTC().Format(time.RFC3339)))
	})
}

// ── Server tools ─────────────────────────────────────────────────────────────

func registerServerTools(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "server_status",
		Description: "Get server status, uptime, and connected agents",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return jsonResult(map[string]any{"status": "running"})
	})

	// Replace ping placeholder with a proper tool.
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "ping",
		Description: "Check server connectivity",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return text("pong")
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
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
		Config string `json:"config"` // full JSON-encoded Config
	}

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "config_save",
		Description: "Save an updated server configuration (full JSON-encoded config object)",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args configSaveArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "settings", "tool", "config_save")
		var cfg config.Config
		if err := json.Unmarshal([]byte(args.Config), &cfg); err != nil {
			return nil, struct{}{}, fmt.Errorf("invalid config JSON: %w", err)
		}
		// Structural validation only (no auth store check from MCP context).
		issues := config.Validate(&cfg, nil)
		for _, issue := range issues {
			if issue.Level == config.LevelError {
				return nil, struct{}{}, fmt.Errorf("validation: [%s] %s: %s", issue.Level, issue.Field, issue.Message)
			}
		}
		if err := config.Save("", &cfg); err != nil {
			return nil, struct{}{}, err
		}
		return text("config saved")
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "config_validate",
		Description: "Validate the current configuration and credentials, returning all issues",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "settings", "tool", "config_validate")
		cfg, err := config.Load("")
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("loading config: %w", err)
		}

		// Open the file-backed auth store to check credentials.
		authPath := store.SubDir(store.DirAuth) + "/credentials.json"
		var authGet func(string) (string, error)
		if st, err := auth.NewFileStore(authPath); err == nil {
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

		// Ping each unique provider to verify the API key actually works.
		factory := llm.NewFactory(func(ref string) (string, error) {
			if authGet == nil {
				return "", nil
			}
			return authGet(strings.TrimPrefix(ref, "auth:"))
		})
		for provider, model := range config.UniqueProviderModels(cfg) {
			if err := factory.PingModel(model); err != nil {
				out = append(out, issueDTO{
					Level:   string(config.LevelError),
					Field:   "models." + provider,
					Message: err.Error(),
				})
			}
		}

		return jsonResult(out)
	})
}

// ── Usage tools ──────────────────────────────────────────────────────────────

type usageQueryArgs struct {
	Start string `json:"start,omitempty"` // RFC3339 or date (YYYY-MM-DD); defaults to 30 days ago
	End   string `json:"end,omitempty"`   // RFC3339 or date; defaults to now
}

func registerUsageTools(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
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
		// end is inclusive for full-day queries: extend to end of day
		if len(args.End) == 10 { // "YYYY-MM-DD" — extend to end of day
			end = end.AddDate(0, 0, 1)
		}

		records, err := store.ReadJSONL[domain.UsageRecord](store.UsagePath())
		if err != nil {
			return nil, struct{}{}, fmt.Errorf("reading usage log: %w", err)
		}

		// Filter to requested date range.
		filtered := records[:0]
		for _, r := range records {
			if (r.Timestamp.Equal(start) || r.Timestamp.After(start)) &&
				r.Timestamp.Before(end) {
				filtered = append(filtered, r)
			}
		}
		return jsonResult(filtered)
	})
}
