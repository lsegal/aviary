package transcript

import (
	"encoding/json"
	"strings"
)

// geminiMapper maps `gemini --output-format stream-json`. Assistant messages
// arrive as delta fragments, tool_use and tool_result events pair by tool_id,
// error events carry warnings and errors, and the result event carries the
// run's token use.
type geminiMapper struct {
	// calls remembers the kind each tool_use became, so its tool_result
	// completes the same event kind.
	calls map[string]Kind
	// pending holds the fragments of the assistant message being streamed;
	// it ends on the next event of any other kind.
	pending strings.Builder
}

type geminiEnvelope struct {
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	Delta      bool           `json:"delta"`
	ToolName   string         `json:"tool_name"`
	ToolID     string         `json:"tool_id"`
	Parameters map[string]any `json:"parameters"`
	Status     string         `json:"status"`
	Output     string         `json:"output"`
	Severity   string         `json:"severity"`
	Message    string         `json:"message"`
	Error      struct {
		Message string `json:"message"`
	} `json:"error"`
	Stats struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		Cached       int `json:"cached"`
		Input        int `json:"input"`
	} `json:"stats"`
}

func (m *geminiMapper) Map(line []byte) []Event {
	var envelope geminiEnvelope
	if json.Unmarshal(line, &envelope) != nil {
		return nil
	}
	if m.calls == nil {
		m.calls = map[string]Kind{}
	}
	if envelope.Type == "message" {
		if envelope.Role != "assistant" {
			return nil
		}
		m.pending.WriteString(envelope.Content)
		if envelope.Delta {
			return nil
		}
		return m.Flush()
	}
	events := m.Flush()
	switch envelope.Type {
	case "tool_use":
		event := geminiToolUse(envelope)
		m.calls[envelope.ToolID] = event.Kind
		events = append(events, event)
	case "tool_result":
		failed := envelope.Status == "error"
		output := envelope.Output
		if failed && output == "" {
			output = envelope.Error.Message
		}
		// Gemini reports a shell command's output but not its exit code, so
		// a command result carries only whether the tool failed.
		if m.calls[envelope.ToolID] == KindCommand {
			events = append(events, Event{Kind: KindCommand, CallID: envelope.ToolID, Output: output, IsError: failed, Status: "done"})
		} else {
			events = append(events, Event{Kind: KindToolResult, CallID: envelope.ToolID, Output: output, IsError: failed})
		}
		delete(m.calls, envelope.ToolID)
	case "error":
		events = append(events, Event{Kind: KindSystem, Text: envelope.Message, IsError: envelope.Severity == "error"})
	case "result":
		s := envelope.Stats
		input := s.Input
		if input == 0 {
			input = s.InputTokens - s.Cached
		}
		events = append(events, Event{Kind: KindUsage, Usage: &Usage{InputTokens: input, OutputTokens: s.OutputTokens, CacheReadTokens: s.Cached}})
		if envelope.Status == "error" && envelope.Error.Message != "" {
			events = append(events, Event{Kind: KindSystem, Text: envelope.Error.Message, IsError: true})
		}
	}
	return events
}

// Flush ends the assistant message being streamed.
func (m *geminiMapper) Flush() []Event {
	text := m.pending.String()
	m.pending.Reset()
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return []Event{{Kind: KindMessage, Text: text}}
}

// geminiToolUse maps one tool_use event: the shell tool becomes a command,
// write_file and replace become file changes, write_todos becomes a plan,
// and everything else stays a tool call.
func geminiToolUse(envelope geminiEnvelope) Event {
	input, id, name := envelope.Parameters, envelope.ToolID, envelope.ToolName
	switch name {
	case "run_shell_command":
		return Event{Kind: KindCommand, CallID: id, Name: name, Command: stringValue(input, "command"), Status: "running"}
	case "write_file":
		return Event{Kind: KindFileChange, CallID: id, Name: name, Path: stringValue(input, "file_path"), Diff: prefixLines("+", stringValue(input, "content"))}
	case "replace":
		return Event{Kind: KindFileChange, CallID: id, Name: name, Path: stringValue(input, "file_path"), Diff: editHunk(stringValue(input, "old_string"), stringValue(input, "new_string"))}
	case "write_todos":
		var items []PlanItem
		todos, _ := input["todos"].([]any)
		for _, raw := range todos {
			todo, _ := raw.(map[string]any)
			items = append(items, PlanItem{Text: stringValue(todo, "description"), Status: stringValue(todo, "status")})
		}
		return Event{Kind: KindPlan, CallID: id, Name: name, Items: items}
	}
	return Event{Kind: KindToolCall, CallID: id, Name: name, Input: input}
}
