package mcp

import (
	"github.com/lsegal/aviary/internal/agent"
)

// AgentRunEvent is one structured event of an agent run: the progress
// message agent_run sends when stream_events is set, and the line
// `aviary agent run --output-format json` prints for it.
type AgentRunEvent struct {
	// Type is "text" (a fragment of the agent's reply, sent only as
	// progress), "message" (a whole reply, printed by the CLI), "tool_call",
	// "tool_result", "file_change", "media", "error", or "done" (the last
	// progress message of a run, sent only as progress).
	Type string `json:"type"`
	// Text is a reply or an error message.
	Text string `json:"text,omitempty"`
	// ID ties a tool call to its result; Name and Input describe the call.
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
	// Output and Error are a tool result's outcome.
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
	// Path and Diff describe a file change; Name is the tool that made it.
	Path string `json:"path,omitempty"`
	Diff string `json:"diff,omitempty"`
	// URL is a media item's image URL.
	URL string `json:"url,omitempty"`
}

// agentRunEvents turns one run's stream into AgentRunEvents, giving tool
// calls IDs when the backend reports none.
type agentRunEvents struct {
	ids agent.ToolCallIDs
}

func (a *agentRunEvents) fromStream(e agent.StreamEvent) (AgentRunEvent, bool) {
	switch e.Type {
	case agent.StreamEventText:
		if e.Text != "" {
			return AgentRunEvent{Type: "text", Text: e.Text}, true
		}
	case agent.StreamEventTool:
		if e.Tool == nil {
			return AgentRunEvent{}, false
		}
		if !e.Tool.Done {
			return AgentRunEvent{Type: "tool_call", ID: a.ids.ID(e.Tool), Name: e.Tool.Name, Input: e.Tool.Args}, true
		}
		return AgentRunEvent{Type: "tool_result", ID: a.ids.ID(e.Tool), Name: e.Tool.Name, Output: e.Tool.Result, Error: e.Tool.Error}, true
	case agent.StreamEventFile:
		if e.File != nil {
			return AgentRunEvent{Type: "file_change", Name: e.File.Tool, Path: e.File.Path, Diff: e.File.Diff}, true
		}
	case agent.StreamEventMedia:
		if e.MediaURL != "" {
			return AgentRunEvent{Type: "media", URL: e.MediaURL}, true
		}
	case agent.StreamEventError:
		if e.Err != nil {
			return AgentRunEvent{Type: "error", Text: e.Err.Error()}, true
		}
	}
	return AgentRunEvent{}, false
}

// toolProgress is the JSON payload of the "[tool]" progress messages
// agent_run sends with include_tool_progress. A call and its result share an
// ID, so the web chat updates one block per call instead of adding one per
// message.
type toolProgress struct {
	ID     string         `json:"id,omitempty"`
	Name   string         `json:"name"`
	Args   map[string]any `json:"args,omitempty"`
	Result string         `json:"result,omitempty"`
	Error  string         `json:"error,omitempty"`
	Done   bool           `json:"done,omitempty"`
}

func (a *agentRunEvents) toolProgress(tool *agent.ToolEvent) toolProgress {
	return toolProgress{
		ID:     a.ids.ID(tool),
		Name:   tool.Name,
		Args:   tool.Args,
		Result: tool.Result,
		Error:  tool.Error,
		Done:   tool.Done,
	}
}
