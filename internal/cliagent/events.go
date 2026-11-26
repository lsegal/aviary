package cliagent

import (
	"strings"

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
	// refusalNote is added to a tool error that is the CLI's own permission
	// refusal, naming that layer; empty adds nothing.
	refusalNote string
}

// cliRefusalPhrases are the CLI's own wording for a call its permission
// check refused, as it reports them in a tool result.
var cliRefusalPhrases = []string{"haven't granted", "requires approval", "has been denied", "denied by your permission settings"}

// isCLIRefusal reports whether a tool error is the CLI's permission refusal.
func isCLIRefusal(output string) bool {
	lower := strings.ToLower(output)
	for _, phrase := range cliRefusalPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
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
		if e.Kind == transcript.KindFileChange && e.Name == "" {
			changeID = e.CallID
			changes = append(changes, map[string]any{"path": e.Path, "kind": e.Status, "diff": e.Diff})
			continue
		}
		t.translateOne(e)
	}
	if len(changes) > 0 {
		t.start(changeID, "file_change", map[string]any{"changes": changes})
		t.finish(changeID, "completed", false)
	}
}

// translateOne emits the Events for one transcript event other than a codex
// file_change, which translate gathers into a single tool.
func (t *toolTranslator) translateOne(e transcript.Event) {
	switch e.Kind {
	case transcript.KindToolCall:
		t.start(e.CallID, e.Name, e.Input)
	case transcript.KindPlan:
		// Codex's todo list is its own running plan, not a tool call.
		if e.Name != "" {
			t.start(e.CallID, e.Name, e.Input)
		}
	case transcript.KindCommand:
		t.command(e)
	case transcript.KindFileChange:
		// Claude's edit tools: the tool_result completes the call.
		t.start(e.CallID, e.Name, map[string]any{"file_path": e.Path, "diff": e.Diff})
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

// command starts a shell command's tool the first time it is seen and
// finishes it once it is done.
func (t *toolTranslator) command(e transcript.Event) {
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
		if t.refusalNote != "" && isCLIRefusal(output) {
			done.Error += "\n\n" + t.refusalNote
		}
	} else {
		done.Result = output
	}
	t.emit(Event{Type: EventToolResult, Tool: &done})
}
