package cliagent

import (
	"github.com/lsegal/aviary/internal/transcript"
)

// toolTranslator turns the transcript events the shared stream parsers map
// from a CLI's output into tool and usage Events. Messages are left to the
// backend, which knows whether its text already streamed.
type toolTranslator struct {
	emit func(Event)
	// skip reports whether a tool is served by Aviary's MCP boundary, which
	// reports its calls itself.
	skip    func(name string) bool
	pending map[string]*ToolEvent
}

func newToolTranslator(emit func(Event), skip func(string) bool) *toolTranslator {
	return &toolTranslator{emit: emit, skip: skip, pending: map[string]*ToolEvent{}}
}

// translate emits the Events for one line's transcript events. A codex
// file_change item maps to one event per changed file; they are reported as
// a single tool, as Codex ran them.
func (t *toolTranslator) translate(events []transcript.Event) {
	var changes []any
	changeID := ""
	for _, e := range events {
		switch e.Kind {
		case transcript.KindToolCall:
			t.start(e.CallID, e.Name, e.Input)
		case transcript.KindPlan:
			// Codex's todo list is its own running plan, not a tool call.
			if e.Name != "" {
				t.start(e.CallID, e.Name, e.Input)
			}
		case transcript.KindCommand:
			name := e.Name
			if name == "" {
				name = "shell"
			}
			args := e.Input
			if args == nil {
				args = map[string]any{"command": e.Command}
			}
			if _, ok := t.pending[e.CallID]; !ok {
				t.start(e.CallID, name, args)
			}
			if e.Status == "done" {
				t.finish(e.CallID, e.Output, e.IsError)
			}
		case transcript.KindFileChange:
			if e.Name != "" {
				// Claude's edit tools: the tool_result completes the call.
				t.start(e.CallID, e.Name, map[string]any{"file_path": e.Path, "diff": e.Diff})
				continue
			}
			changeID = e.CallID
			changes = append(changes, map[string]any{"path": e.Path, "kind": e.Status, "diff": e.Diff})
		case transcript.KindToolResult:
			if _, ok := t.pending[e.CallID]; !ok && e.Name != "" {
				t.start(e.CallID, e.Name, e.Input)
			}
			t.finish(e.CallID, e.Output, e.IsError)
		case transcript.KindUsage:
			u := e.Usage
			t.emit(Event{Type: EventUsage, Usage: &Usage{
				InputTokens:      u.InputTokens,
				OutputTokens:     u.OutputTokens,
				CacheReadTokens:  u.CacheReadTokens,
				CacheWriteTokens: u.CacheWriteTokens,
				CostUSD:          u.CostUSD,
			}})
		}
	}
	if len(changes) > 0 {
		t.start(changeID, "file_change", map[string]any{"changes": changes})
		t.finish(changeID, "completed", false)
	}
}

func (t *toolTranslator) start(id, name string, args map[string]any) {
	if t.skip(name) {
		return
	}
	tool := &ToolEvent{ID: id, Name: name, Args: args}
	t.pending[id] = tool
	t.emit(Event{Type: EventToolStart, Tool: &ToolEvent{ID: id, Name: name, Args: args}})
}

// finish reports the result of a started tool. A result for a tool that was
// never reported, such as one of Aviary's own, is dropped.
func (t *toolTranslator) finish(id, output string, isError bool) {
	tool, ok := t.pending[id]
	if !ok {
		return
	}
	delete(t.pending, id)
	done := *tool
	if isError {
		done.Error = output
		if done.Error == "" {
			done.Error = "failed"
		}
	} else {
		done.Result = output
	}
	t.emit(Event{Type: EventToolResult, Tool: &done})
}
