package transcript

import (
	"encoding/json"
	"strings"
)

// opencodeMapper maps `opencode run --format json`. Every line is one part of
// the session: text and reasoning parts carry the assistant's words, a
// tool_use part carries a finished tool call with its input, output and
// metadata, and each step_finish carries that step's token use and cost.
type opencodeMapper struct{}

type opencodeEnvelope struct {
	Type string `json:"type"`
	Part struct {
		Text   string `json:"text"`
		Tool   string `json:"tool"`
		CallID string `json:"callID"`
		State  struct {
			Status   string         `json:"status"`
			Input    map[string]any `json:"input"`
			Output   string         `json:"output"`
			Error    string         `json:"error"`
			Metadata map[string]any `json:"metadata"`
		} `json:"state"`
		Tokens struct {
			Input  int `json:"input"`
			Output int `json:"output"`
			Cache  struct {
				Read  int `json:"read"`
				Write int `json:"write"`
			} `json:"cache"`
		} `json:"tokens"`
		Cost float64 `json:"cost"`
	} `json:"part"`
	Error struct {
		Name string `json:"name"`
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	} `json:"error"`
}

func (opencodeMapper) Map(line []byte) []Event {
	var envelope opencodeEnvelope
	if json.Unmarshal(line, &envelope) != nil {
		return nil
	}
	part := envelope.Part
	switch envelope.Type {
	case "text":
		if strings.TrimSpace(part.Text) != "" {
			return []Event{{Kind: KindMessage, Text: part.Text}}
		}
	case "reasoning":
		if strings.TrimSpace(part.Text) != "" {
			return []Event{{Kind: KindReasoning, Text: part.Text}}
		}
	case "tool_use":
		return opencodeTool(envelope)
	case "step_finish":
		t := part.Tokens
		return []Event{{Kind: KindUsage, Usage: &Usage{InputTokens: t.Input, OutputTokens: t.Output, CacheReadTokens: t.Cache.Read, CacheWriteTokens: t.Cache.Write, CostUSD: part.Cost}}}
	case "error":
		text := envelope.Error.Data.Message
		if text == "" {
			text = envelope.Error.Name
		}
		return []Event{{Kind: KindSystem, Text: text, IsError: true}}
	}
	return nil
}

// opencodeTool maps a finished tool part: opencode reports a tool only once it
// has completed or failed, so each part becomes the call and its result
// together.
func opencodeTool(envelope opencodeEnvelope) []Event {
	part := envelope.Part
	state := part.State
	input, metadata := state.Input, state.Metadata
	failed := state.Status == "error"
	output := state.Output
	if failed {
		output = state.Error
	}
	id := part.CallID
	result := Event{Kind: KindToolResult, CallID: id, Name: part.Tool, Output: output, IsError: failed}
	switch part.Tool {
	case "bash":
		command := stringValue(input, "command")
		done := Event{Kind: KindCommand, CallID: id, Name: part.Tool, Command: command, Output: output, IsError: failed, Status: "done"}
		if code, ok := metadata["exit"].(float64); ok {
			exit := int(code)
			done.ExitCode = &exit
			done.IsError = done.IsError || exit != 0
		}
		return []Event{{Kind: KindCommand, CallID: id, Name: part.Tool, Command: command, Status: "running"}, done}
	case "apply_patch", "edit", "write", "multiedit":
		var events []Event
		files, _ := metadata["files"].([]any)
		for _, raw := range files {
			file, _ := raw.(map[string]any)
			events = append(events, Event{Kind: KindFileChange, CallID: id, Name: part.Tool, Path: stringValue(file, "filePath"), Status: stringValue(file, "type"), Diff: stringValue(file, "patch")})
		}
		if len(events) == 0 {
			if part.Tool == "apply_patch" {
				events = patchFileChanges(id, part.Tool, stringValue(input, "patchText"))
			} else {
				diff := stringValue(metadata, "diff")
				if diff == "" && part.Tool == "write" {
					diff = prefixLines("+", stringValue(input, "content"))
				} else if diff == "" {
					diff = editHunk(stringValue(input, "oldString"), stringValue(input, "newString"))
				}
				events = []Event{{Kind: KindFileChange, CallID: id, Name: part.Tool, Path: stringValue(input, "filePath"), Diff: diff}}
			}
		}
		return append(events, result)
	case "todowrite":
		var items []PlanItem
		todos, _ := input["todos"].([]any)
		for _, raw := range todos {
			todo, _ := raw.(map[string]any)
			items = append(items, PlanItem{Text: stringValue(todo, "content"), Status: stringValue(todo, "status")})
		}
		return []Event{{Kind: KindPlan, CallID: id, Name: part.Tool, Items: items}, result}
	}
	return []Event{{Kind: KindToolCall, CallID: id, Name: part.Tool, Input: input}, result}
}

// clineMapper maps `cline --json`. Cline wraps its agent events in an
// envelope: content_end carries a whole text or reasoning block (content_start
// streams the same text token by token), a tool's content_start carries its
// name and input and its content_end its output, and done carries the run's
// total usage.
type clineMapper struct {
	// calls remembers the kind each tool call became, so its result completes
	// the same event kind.
	calls map[string]Kind
}

type clineEnvelope struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Event   struct {
		Type        string          `json:"type"`
		ContentType string          `json:"contentType"`
		Text        string          `json:"text"`
		Reasoning   string          `json:"reasoning"`
		ToolCallID  string          `json:"toolCallId"`
		ToolName    string          `json:"toolName"`
		Input       map[string]any  `json:"input"`
		Output      json.RawMessage `json:"output"`
		Reason      string          `json:"reason"`
		Usage       struct {
			InputTokens      int     `json:"inputTokens"`
			OutputTokens     int     `json:"outputTokens"`
			CacheReadTokens  int     `json:"cacheReadTokens"`
			CacheWriteTokens int     `json:"cacheWriteTokens"`
			TotalCost        float64 `json:"totalCost"`
		} `json:"usage"`
	} `json:"event"`
}

func (m *clineMapper) Map(line []byte) []Event {
	var envelope clineEnvelope
	if json.Unmarshal(line, &envelope) != nil {
		return nil
	}
	if envelope.Type == "error" {
		return []Event{{Kind: KindSystem, Text: envelope.Message, IsError: true}}
	}
	if envelope.Type != "agent_event" {
		return nil
	}
	if m.calls == nil {
		m.calls = map[string]Kind{}
	}
	event := envelope.Event
	switch event.Type {
	case "content_start":
		if event.ContentType == "tool" {
			call := clineToolCall(event.ToolCallID, event.ToolName, event.Input)
			if len(call) > 0 {
				m.calls[event.ToolCallID] = call[0].Kind
			}
			return call
		}
	case "content_end":
		switch event.ContentType {
		case "text":
			if strings.TrimSpace(event.Text) != "" {
				return []Event{{Kind: KindMessage, Text: event.Text}}
			}
		case "reasoning":
			if strings.TrimSpace(event.Reasoning) != "" {
				return []Event{{Kind: KindReasoning, Text: event.Reasoning}}
			}
		case "tool":
			output, failed := clineToolOutput(event.Output)
			kind := m.calls[event.ToolCallID]
			delete(m.calls, event.ToolCallID)
			if kind == KindCommand {
				code := 0
				if failed {
					code = 1
				}
				return []Event{{Kind: KindCommand, CallID: event.ToolCallID, Name: event.ToolName, Output: output, IsError: failed, ExitCode: &code, Status: "done"}}
			}
			return []Event{{Kind: KindToolResult, CallID: event.ToolCallID, Name: event.ToolName, Output: output, IsError: failed}}
		}
	case "done":
		u := event.Usage
		events := []Event{{Kind: KindUsage, Usage: &Usage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens, CostUSD: u.TotalCost}}}
		if event.Reason != "" && event.Reason != "completed" {
			text := event.Text
			if text == "" {
				text = event.Reason
			}
			events = append(events, Event{Kind: KindSystem, Text: text, IsError: true})
		}
		return events
	}
	return nil
}

// clineToolCall maps a tool's content_start: run_commands becomes a command,
// apply_patch and editor become file changes, and everything else stays a
// tool call.
func clineToolCall(id, name string, input map[string]any) []Event {
	switch name {
	case "run_commands":
		return []Event{{Kind: KindCommand, CallID: id, Name: name, Command: clineCommands(input["commands"]), Status: "running"}}
	case "apply_patch":
		if events := patchFileChanges(id, name, stringValue(input, "input")); len(events) > 0 {
			return events
		}
	case "editor":
		diff := editHunk(stringValue(input, "old_text"), stringValue(input, "new_text"))
		if stringValue(input, "insert_line") != "" {
			diff = "@@ +" + stringValue(input, "insert_line") + " @@\n" + prefixLines("+", stringValue(input, "new_text"))
		}
		return []Event{{Kind: KindFileChange, CallID: id, Name: name, Path: stringValue(input, "path"), Diff: diff}}
	}
	return []Event{{Kind: KindToolCall, CallID: id, Name: name, Input: input}}
}

// clineCommands renders run_commands' commands, which are shell strings or
// executable-and-argv objects, one per line.
func clineCommands(raw any) string {
	list, ok := raw.([]any)
	if !ok {
		list = []any{raw}
	}
	var lines []string
	for _, item := range list {
		switch command := item.(type) {
		case string:
			lines = append(lines, command)
		case map[string]any:
			parts := []string{stringValue(command, "command")}
			if parts[0] == "" {
				parts[0] = stringValue(command, "executable")
			}
			args, _ := command["args"].([]any)
			for _, arg := range args {
				if s, ok := arg.(string); ok {
					parts = append(parts, s)
				}
			}
			lines = append(lines, strings.Join(parts, " "))
		}
	}
	return strings.Join(lines, "\n")
}

// clineToolOutput flattens a tool's output, one {query, result, error,
// success} record or a list of them, into its text and whether any failed.
func clineToolOutput(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	type record struct {
		Result  json.RawMessage `json:"result"`
		Error   string          `json:"error"`
		Success *bool           `json:"success"`
	}
	var records []record
	if json.Unmarshal(raw, &records) != nil {
		var one record
		if json.Unmarshal(raw, &one) != nil || (one.Result == nil && one.Error == "" && one.Success == nil) {
			var text string
			if json.Unmarshal(raw, &text) == nil {
				return text, false
			}
			return string(raw), false
		}
		records = []record{one}
	}
	var texts []string
	failed := false
	for _, r := range records {
		var text string
		if json.Unmarshal(r.Result, &text) != nil && len(r.Result) > 0 {
			text = string(r.Result)
		}
		if r.Error != "" {
			text = strings.TrimSpace(strings.Join([]string{text, r.Error}, "\n"))
		}
		texts = append(texts, text)
		if (r.Success != nil && !*r.Success) || r.Error != "" {
			failed = true
		}
	}
	return strings.Join(texts, "\n"), failed
}

// patchFileChanges splits an apply_patch envelope ("*** Begin Patch" ...
// "*** End Patch") into one file change per file it adds, updates or deletes.
func patchFileChanges(id, name, patch string) []Event {
	var events []Event
	var body []string
	flush := func() {
		if len(events) > 0 {
			events[len(events)-1].Diff = strings.Join(body, "\n")
		}
		body = nil
	}
	for _, line := range strings.Split(patch, "\n") {
		status, path := "", ""
		for prefix, kind := range map[string]string{"*** Add File: ": "add", "*** Update File: ": "update", "*** Delete File: ": "delete"} {
			if rest, ok := strings.CutPrefix(line, prefix); ok {
				status, path = kind, strings.TrimSpace(rest)
			}
		}
		switch {
		case path != "":
			flush()
			events = append(events, Event{Kind: KindFileChange, CallID: id, Name: name, Path: path, Status: status})
		case strings.HasPrefix(line, "*** End Patch"), strings.HasPrefix(line, "*** Begin Patch"):
		case strings.HasPrefix(line, "*** Move to: ") && len(events) > 0:
			events[len(events)-1].Path = strings.TrimSpace(strings.TrimPrefix(line, "*** Move to: "))
		case len(events) > 0:
			body = append(body, line)
		}
	}
	flush()
	return events
}
