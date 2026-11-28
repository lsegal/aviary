package transcript

import (
	"encoding/json"
	"strings"
)

// museMapper maps `muse exec --json`. Every line is a runtime record named by
// payload_type: run.output.delta streams the assistant's words, tool.result
// reports a finished tool call with its name, outcome and output text (muse
// never records a call's arguments), the plan snapshot record carries the plan,
// run.usage the run's token use and run.terminal.failed a run that failed.
type museMapper struct {
	// pending holds the deltas of the assistant message being streamed; it
	// ends when its model task does.
	pending strings.Builder
}

type museEnvelope struct {
	PayloadType string `json:"payload_type"`
	Payload     struct {
		Text      string `json:"text"`
		CallID    string `json:"call_id"`
		Terminal  string `json:"terminal"`
		Reason    string `json:"reason"`
		EditFacts struct {
			Path string `json:"path"`
		} `json:"edit_facts"`
		CorrelationFacts struct {
			ToolName string `json:"tool_name"`
			Outcome  string `json:"outcome"`
		} `json:"correlation_facts"`
		Facts struct {
			Items []PlanItem `json:"items"`
		} `json:"facts"`
		Models []struct {
			InputTokens  int `json:"input_tokens"`
			CachedTokens int `json:"cached_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"models"`
	} `json:"payload"`
}

// museBashResult is the JSON document a bash tool result's text holds.
type museBashResult struct {
	Command  string `json:"command"`
	ExitCode *int   `json:"exit_code"`
	Output   string `json:"output"`
}

func (m *museMapper) Map(line []byte) []Event {
	var envelope museEnvelope
	if json.Unmarshal(line, &envelope) != nil {
		return nil
	}
	payload := envelope.Payload
	switch envelope.PayloadType {
	case "run.output.delta":
		m.pending.WriteString(payload.Text)
		return nil
	case "task.lifecycle.proposed", "task.lifecycle.completed", "task.lifecycle.failed", "task.lifecycle.cancelled":
		// A task starting or ending closes the message its model task
		// streamed.
		return m.Flush()
	}
	if strings.HasPrefix(envelope.PayloadType, "task.") {
		return nil
	}
	events := m.Flush()
	switch envelope.PayloadType {
	case "tool.result":
		events = append(events, museToolResult(envelope)...)
	case "todo.snapshot.updated":
		events = append(events, Event{Kind: KindPlan, Items: payload.Facts.Items})
	case "run.usage":
		if len(payload.Models) == 0 {
			break
		}
		usage := Usage{}
		for _, model := range payload.Models {
			// Like codex, muse counts cached tokens inside input_tokens.
			usage.InputTokens += max(model.InputTokens-model.CachedTokens, 0)
			usage.CacheReadTokens += model.CachedTokens
			usage.OutputTokens += model.OutputTokens
		}
		events = append(events, Event{Kind: KindUsage, Usage: &usage})
	case "run.terminal.failed", "run.terminal.cancelled":
		text := payload.Reason
		if text == "" {
			text = "muse run " + payload.Terminal
		}
		events = append(events, Event{Kind: KindSystem, Text: text, IsError: true})
	}
	return events
}

// Flush ends the assistant message being streamed.
func (m *museMapper) Flush() []Event {
	text := m.pending.String()
	m.pending.Reset()
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return []Event{{Kind: KindMessage, Text: text}}
}

// museToolResult maps a finished tool call: bash becomes a command,
// write_file and edit_file become file changes, write_todos only completes the
// plan its snapshot already reported, and everything else stays a tool call.
func museToolResult(envelope museEnvelope) []Event {
	payload := envelope.Payload
	id, name, text := payload.CallID, payload.CorrelationFacts.ToolName, payload.Text
	failed := payload.CorrelationFacts.Outcome == "failure"
	result := Event{Kind: KindToolResult, CallID: id, Name: name, Output: text, IsError: failed}
	switch name {
	case "bash":
		var bash museBashResult
		if json.Unmarshal([]byte(text), &bash) != nil {
			bash.Output = text
		}
		done := Event{Kind: KindCommand, CallID: id, Name: name, Command: bash.Command, Output: bash.Output, ExitCode: bash.ExitCode, IsError: failed, Status: "done"}
		if bash.ExitCode != nil && *bash.ExitCode != 0 {
			done.IsError = true
		}
		return []Event{{Kind: KindCommand, CallID: id, Name: name, Command: bash.Command, Status: "running"}, done}
	case "write_file", "edit_file":
		change := Event{Kind: KindFileChange, CallID: id, Name: name, Path: payload.EditFacts.Path}
		// edit_file reports its change as a unified diff after the
		// "+++ updated" header.
		if _, diff, ok := strings.Cut(text, "+++ updated\n"); ok {
			change.Diff = strings.TrimRight(diff, "\n")
		}
		return []Event{change, result}
	case "write_todos":
		return []Event{result}
	}
	return []Event{{Kind: KindToolCall, CallID: id, Name: name}, result}
}
