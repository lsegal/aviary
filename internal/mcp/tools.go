package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/agent"
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
	registerTaskTools(s)
	registerJobTools(s)
	registerBrowserTools(s)
	registerMemoryTools(s)
	registerAuthTools(s)
	registerServerTools(s)
}

// ── Agent tools ──────────────────────────────────────────────────────────────

type agentRunArgs struct {
	Name    string `json:"name"`
	Message string `json:"message"`
	File    string `json:"file,omitempty"`
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
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args agentRunArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Agents == nil {
			return nil, struct{}{}, fmt.Errorf("agent manager not initialized; is the server running?")
		}
		runner, ok := d.Agents.Get(args.Name)
		if !ok {
			return nil, struct{}{}, fmt.Errorf("agent %q not found", args.Name)
		}

		var buf strings.Builder
		done := make(chan error, 1)
		runner.Prompt(ctx, args.Message, func(e agent.StreamEvent) {
			switch e.Type {
			case agent.StreamEventText:
				buf.WriteString(e.Text)
			case agent.StreamEventDone:
				done <- nil
			case agent.StreamEventStop:
				done <- fmt.Errorf("agent stopped")
			case agent.StreamEventError:
				done <- e.Err
			}
		})
		if err := <-done; err != nil {
			return nil, struct{}{}, err
		}
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
}

// ── Task tools ───────────────────────────────────────────────────────────────

type taskNameArgs struct {
	Name string `json:"name"`
}

func registerTaskTools(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "task_list",
		Description: "List all tasks, their trigger type, and last run status",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
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
		Description: "Manually trigger a task right now",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args taskNameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return stub(fmt.Sprintf("task_run(%s): use agent_run to run an agent directly", args.Name))
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "task_stop",
		Description: "Stop all currently running scheduled task jobs",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return stub("task_stop")
	})
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

type browserSelectorArgs struct {
	Selector string `json:"selector"`
}

type browserTypeArgs struct {
	Selector string `json:"selector"`
	Text     string `json:"text"`
}

func registerBrowserTools(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "browser_open",
		Description: "Navigate to a URL in the browser",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args browserOpenArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		if err := d.Browser.Open(ctx, args.URL); err != nil {
			return nil, struct{}{}, err
		}
		return text(fmt.Sprintf("navigated to %s", args.URL))
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "browser_click",
		Description: "Click an element by CSS selector",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args browserSelectorArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		if err := d.Browser.Click(args.Selector); err != nil {
			return nil, struct{}{}, err
		}
		return text(fmt.Sprintf("clicked %q", args.Selector))
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "browser_type",
		Description: "Type text into an element by CSS selector",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args browserTypeArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		if err := d.Browser.Type(args.Selector, args.Text); err != nil {
			return nil, struct{}{}, err
		}
		return text(fmt.Sprintf("typed into %q", args.Selector))
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "browser_screenshot",
		Description: "Capture a screenshot of the current browser view",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		png, err := d.Browser.Screenshot()
		if err != nil {
			return nil, struct{}{}, err
		}
		return text(fmt.Sprintf("screenshot captured (%d bytes PNG)", len(png)))
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "browser_close",
		Description: "Close the current browser session",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Browser == nil {
			return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
		}
		d.Browser.Close()
		return text("browser session closed")
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

func registerMemoryTools(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "memory_search",
		Description: "Search an agent's memory",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args memoryAgentQueryArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Memory == nil {
			return nil, struct{}{}, fmt.Errorf("memory manager not initialized")
		}
		poolID := "private:" + args.Agent
		results, err := d.Memory.Search(poolID, args.Query)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(results)
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "memory_show",
		Description: "Display the full memory for an agent",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args memoryAgentArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Memory == nil {
			return nil, struct{}{}, fmt.Errorf("memory manager not initialized")
		}
		poolID := "private:" + args.Agent
		entries, err := d.Memory.All(poolID)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(entries)
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "memory_clear",
		Description: "Wipe all memory for an agent",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args memoryAgentArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d.Memory == nil {
			return nil, struct{}{}, fmt.Errorf("memory manager not initialized")
		}
		poolID := "private:" + args.Agent
		if err := d.Memory.Clear(poolID); err != nil {
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

func registerAuthTools(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "auth_set",
		Description: "Store a credential by name",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args authSetArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return stub(fmt.Sprintf("auth_set(%s)", args.Name))
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "auth_get",
		Description: "Get a credential name (value masked)",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args authNameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return stub(fmt.Sprintf("auth_get(%s)", args.Name))
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "auth_list",
		Description: "List all stored credential names",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return stub("auth_list")
	})

	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "auth_delete",
		Description: "Remove a stored credential",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args authNameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return stub(fmt.Sprintf("auth_delete(%s)", args.Name))
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
}
