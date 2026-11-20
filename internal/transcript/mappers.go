package transcript

import (
	"encoding/json"
	"fmt"
	"strings"
)

// claudeMapper maps `claude -p --output-format stream-json --verbose`.
// Assistant messages carry text, thinking and tool_use blocks; user messages
// carry tool_result blocks; the result envelope carries usage and cost.
type claudeMapper struct {
	// calls remembers the kind each tool_use became, so its tool_result
	// completes the same event kind.
	calls map[string]Kind
}

type claudeEnvelope struct {
	Type    string `json:"type"`
	Message struct {
		Content []claudeBlock `json:"content"`
	} `json:"message"`
	Usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	IsError      bool    `json:"is_error"`
	Result       string  `json:"result"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     map[string]any  `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

func (m *claudeMapper) Map(line []byte) []Event {
	var envelope claudeEnvelope
	if json.Unmarshal(line, &envelope) != nil {
		return nil
	}
	if m.calls == nil {
		m.calls = map[string]Kind{}
	}
	var events []Event
	switch envelope.Type {
	case "assistant":
		for _, block := range envelope.Message.Content {
			switch block.Type {
			case "text":
				if strings.TrimSpace(block.Text) != "" {
					events = append(events, Event{Kind: KindMessage, Text: block.Text})
				}
			case "thinking":
				if strings.TrimSpace(block.Thinking) != "" {
					events = append(events, Event{Kind: KindReasoning, Text: block.Thinking})
				}
			case "tool_use":
				event := claudeToolUse(block)
				m.calls[block.ID] = event.Kind
				events = append(events, event)
			}
		}
	case "user":
		for _, block := range envelope.Message.Content {
			if block.Type != "tool_result" {
				continue
			}
			output, images := claudeToolResultContent(block.Content)
			switch m.calls[block.ToolUseID] {
			case KindCommand:
				code := 0
				if block.IsError {
					code = 1
				}
				events = append(events, Event{Kind: KindCommand, CallID: block.ToolUseID, Output: output, IsError: block.IsError, ExitCode: &code, Status: "done"})
			default:
				events = append(events, Event{Kind: KindToolResult, CallID: block.ToolUseID, Output: output, IsError: block.IsError, Images: images})
			}
			delete(m.calls, block.ToolUseID)
		}
	case "result":
		u := envelope.Usage
		events = append(events, Event{Kind: KindUsage, Usage: &Usage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, CacheReadTokens: u.CacheReadInputTokens, CacheWriteTokens: u.CacheCreationInputTokens, CostUSD: envelope.TotalCostUSD}})
		if envelope.IsError && envelope.Result != "" {
			events = append(events, Event{Kind: KindSystem, Text: envelope.Result, IsError: true})
		}
	}
	return events
}

// claudeToolUse maps one tool_use block: shell tools become commands, edits
// become file changes, todo and task tools become plans, and everything else
// stays a tool call.
func claudeToolUse(block claudeBlock) Event {
	input := block.Input
	switch block.Name {
	case "Bash", "PowerShell":
		return Event{Kind: KindCommand, CallID: block.ID, Name: block.Name, Input: input, Command: stringValue(input, "command"), Status: "running"}
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		path := stringValue(input, "file_path")
		if path == "" {
			path = stringValue(input, "notebook_path")
		}
		return Event{Kind: KindFileChange, CallID: block.ID, Name: block.Name, Path: path, Diff: claudeEditDiff(block.Name, input)}
	case "TodoWrite":
		var items []PlanItem
		if todos, ok := input["todos"].([]any); ok {
			for _, raw := range todos {
				todo, _ := raw.(map[string]any)
				items = append(items, PlanItem{Text: stringValue(todo, "content"), Status: stringValue(todo, "status")})
			}
		}
		return Event{Kind: KindPlan, CallID: block.ID, Name: block.Name, Input: input, Items: items}
	case "TaskCreate":
		return Event{Kind: KindPlan, CallID: block.ID, Name: block.Name, Input: input, Items: []PlanItem{{Text: stringValue(input, "subject"), Status: "pending"}}}
	case "TaskUpdate":
		text := stringValue(input, "subject")
		if text == "" {
			text = "task " + stringValue(input, "taskId")
		}
		return Event{Kind: KindPlan, CallID: block.ID, Name: block.Name, Input: input, Items: []PlanItem{{Text: text, Status: stringValue(input, "status")}}}
	}
	return Event{Kind: KindToolCall, CallID: block.ID, Name: block.Name, Input: input}
}

// claudeEditDiff renders an edit tool's input as a unified-style diff.
func claudeEditDiff(name string, input map[string]any) string {
	switch name {
	case "Write":
		return prefixLines("+", stringValue(input, "content"))
	case "Edit":
		return editHunk(stringValue(input, "old_string"), stringValue(input, "new_string"))
	case "MultiEdit":
		var hunks []string
		if edits, ok := input["edits"].([]any); ok {
			for _, raw := range edits {
				edit, _ := raw.(map[string]any)
				hunks = append(hunks, editHunk(stringValue(edit, "old_string"), stringValue(edit, "new_string")))
			}
		}
		return strings.Join(hunks, "\n")
	}
	return prefixLines("+", stringValue(input, "new_source"))
}

func editHunk(oldText, newText string) string {
	parts := []string{"@@"}
	if oldText != "" {
		parts = append(parts, prefixLines("-", oldText))
	}
	if newText != "" {
		parts = append(parts, prefixLines("+", newText))
	}
	return strings.Join(parts, "\n")
}

func prefixLines(prefix, text string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// claudeToolResultContent flattens a tool_result's content, which is either a
// string or a list of text and image blocks.
func claudeToolResultContent(raw json.RawMessage) (string, []string) {
	if len(raw) == 0 {
		return "", nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	var blocks []struct {
		Type   string `json:"type"`
		Text   string `json:"text"`
		Source struct {
			Type      string `json:"type"`
			MediaType string `json:"media_type"`
			Data      string `json:"data"`
			URL       string `json:"url"`
		} `json:"source"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return string(raw), nil
	}
	var texts, images []string
	for _, block := range blocks {
		switch block.Type {
		case "text":
			texts = append(texts, block.Text)
		case "image":
			if block.Source.URL != "" {
				images = append(images, block.Source.URL)
			} else if block.Source.Data != "" {
				images = append(images, "data:"+block.Source.MediaType+";base64,"+block.Source.Data)
			}
		}
	}
	return strings.Join(texts, "\n"), images
}

// codexMapper maps `codex exec --json`: item.started and item.completed
// events carry agent messages, reasoning, command executions, file changes,
// tool calls and todo lists, and turn.completed carries usage.
type codexMapper struct {
	// started remembers the web searches whose call was already mapped, so a
	// search that never reported item.started still gets one.
	started map[string]bool
}

type codexEnvelope struct {
	Type     string         `json:"type"`
	ThreadID string         `json:"thread_id"`
	Item     map[string]any `json:"item"`
	Usage    struct {
		InputTokens       int `json:"input_tokens"`
		CachedInputTokens int `json:"cached_input_tokens"`
		CacheWriteTokens  int `json:"cache_write_input_tokens"`
		OutputTokens      int `json:"output_tokens"`
	} `json:"usage"`
	Message string `json:"message"`
	Error   struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (m *codexMapper) Map(line []byte) []Event {
	var envelope codexEnvelope
	if json.Unmarshal(line, &envelope) != nil {
		return nil
	}
	switch envelope.Type {
	case "item.started", "item.updated":
		return m.item(envelope.Item, false)
	case "item.completed":
		return m.item(envelope.Item, true)
	case "turn.completed":
		// Codex counts cached tokens inside input_tokens. Usage reports
		// uncached input, as Claude and Gemini do, so cache reads are not
		// counted twice.
		u := envelope.Usage
		return []Event{{Kind: KindUsage, Usage: &Usage{InputTokens: max(u.InputTokens-u.CachedInputTokens, 0), OutputTokens: u.OutputTokens, CacheReadTokens: u.CachedInputTokens, CacheWriteTokens: u.CacheWriteTokens}}}
	case "turn.failed":
		return []Event{{Kind: KindSystem, Text: envelope.Error.Message, IsError: true}}
	case "error":
		return []Event{{Kind: KindSystem, Text: envelope.Message, IsError: true}}
	}
	return nil
}

func (m *codexMapper) item(item map[string]any, completed bool) []Event {
	id := stringValue(item, "id")
	failed := stringValue(item, "status") == "failed"
	switch stringValue(item, "type") {
	case "command_execution":
		event := Event{Kind: KindCommand, CallID: id, Command: stringValue(item, "command"), Status: "running"}
		if completed {
			event.Status = "done"
			event.Output = stringValue(item, "aggregated_output")
			event.IsError = failed
			if code, ok := item["exit_code"].(float64); ok {
				exit := int(code)
				event.ExitCode = &exit
				event.IsError = event.IsError || exit != 0
			}
		}
		return []Event{event}
	case "agent_message":
		if completed {
			return []Event{{Kind: KindMessage, Text: stringValue(item, "text")}}
		}
	case "reasoning":
		if completed && strings.TrimSpace(stringValue(item, "text")) != "" {
			return []Event{{Kind: KindReasoning, Text: stringValue(item, "text")}}
		}
	case "file_change":
		if !completed {
			return nil
		}
		var events []Event
		changes, _ := item["changes"].([]any)
		for _, raw := range changes {
			change, _ := raw.(map[string]any)
			events = append(events, Event{Kind: KindFileChange, CallID: id, Path: stringValue(change, "path"), Status: stringValue(change, "kind"), Diff: stringValue(change, "diff")})
		}
		return events
	case "mcp_tool_call":
		name := stringValue(item, "tool")
		if server := stringValue(item, "server"); server != "" {
			name = server + "." + name
		}
		args, _ := item["arguments"].(map[string]any)
		if !completed {
			return []Event{{Kind: KindToolCall, CallID: id, Name: name, Input: args}}
		}
		result := Event{Kind: KindToolResult, CallID: id, Name: name, Input: args, IsError: failed}
		if errValue, ok := item["error"].(map[string]any); ok {
			result.IsError, result.Output = true, stringValue(errValue, "message")
		} else if value, ok := item["result"].(map[string]any); ok && value["content"] != nil {
			raw, _ := json.Marshal(value["content"])
			result.Output, result.Images = claudeToolResultContent(raw)
		} else if item["result"] != nil {
			data, _ := json.Marshal(item["result"])
			result.Output = string(data)
		}
		return []Event{result}
	case "web_search":
		if m.started == nil {
			m.started = map[string]bool{}
		}
		var events []Event
		if !m.started[id] {
			m.started[id] = true
			events = append(events, Event{Kind: KindToolCall, CallID: id, Name: "web_search", Input: map[string]any{"query": stringValue(item, "query")}})
		}
		if completed {
			delete(m.started, id)
			events = append(events, Event{Kind: KindToolResult, CallID: id, Name: "web_search", Output: stringValue(item, "status"), IsError: failed})
		}
		return events
	case "todo_list":
		var items []PlanItem
		list, _ := item["items"].([]any)
		for _, raw := range list {
			todo, _ := raw.(map[string]any)
			status := "pending"
			if done, _ := todo["completed"].(bool); done {
				status = "completed"
			}
			items = append(items, PlanItem{Text: stringValue(todo, "text"), Status: status})
		}
		return []Event{{Kind: KindPlan, CallID: id, Items: items}}
	case "error":
		if completed {
			return []Event{{Kind: KindSystem, Text: stringValue(item, "message"), IsError: true}}
		}
	}
	return nil
}

func stringValue(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return v
	case nil:
		return ""
	case float64:
		return fmt.Sprint(int64(v))
	default:
		return fmt.Sprint(v)
	}
}
