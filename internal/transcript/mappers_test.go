package transcript

import (
	"encoding/json"
	"testing"
)

// eventsJSON renders events for comparison, so a mismatch prints every field.
func eventsJSON(t *testing.T, events []Event) string {
	t.Helper()
	if len(events) == 0 {
		return "[]"
	}
	data, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertEvents(t *testing.T, got, want []Event) {
	t.Helper()
	if g, w := eventsJSON(t, got), eventsJSON(t, want); g != w {
		t.Errorf("events =\n%s\nwant\n%s", g, w)
	}
}

func intPtr(v int) *int { return &v }

func mapLines(mapper Mapper, lines ...string) []Event {
	var events []Event
	for _, line := range lines {
		events = append(events, mapper.Map([]byte(line))...)
	}
	return events
}

func TestClaudeEditDiffs(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"Write", map[string]any{"content": "a\nb\n"}, "+a\n+b"},
		{"Write", map[string]any{}, ""},
		{"Edit", map[string]any{"old_string": "old", "new_string": "new\nline"}, "@@\n-old\n+new\n+line"},
		{"Edit", map[string]any{"old_string": "gone"}, "@@\n-gone"},
		{"Edit", map[string]any{"new_string": "added"}, "@@\n+added"},
		{"MultiEdit", map[string]any{"edits": []any{
			map[string]any{"old_string": "a", "new_string": "b"},
			map[string]any{"old_string": "c\nd", "new_string": "e"},
		}}, "@@\n-a\n+b\n@@\n-c\n-d\n+e"},
		{"MultiEdit", map[string]any{"edits": []any{"not an edit"}}, "@@"},
		{"MultiEdit", map[string]any{"edits": "not a list"}, ""},
		{"NotebookEdit", map[string]any{"new_source": "print(1)\n"}, "+print(1)"},
	}
	for _, test := range tests {
		if got := claudeEditDiff(test.name, test.input); got != test.want {
			t.Errorf("claudeEditDiff(%s, %v) = %q, want %q", test.name, test.input, got, test.want)
		}
	}
}

func TestClaudeToolUseKinds(t *testing.T) {
	tests := []struct {
		block claudeBlock
		want  Event
	}{
		{
			claudeBlock{ID: "1", Name: "PowerShell", Input: map[string]any{"command": "dir"}},
			Event{Kind: KindCommand, CallID: "1", Name: "PowerShell", Input: map[string]any{"command": "dir"}, Command: "dir", Status: "running"},
		},
		{
			claudeBlock{ID: "2", Name: "Write", Input: map[string]any{"file_path": "a.txt", "content": "hi"}},
			Event{Kind: KindFileChange, CallID: "2", Name: "Write", Path: "a.txt", Diff: "+hi"},
		},
		{
			claudeBlock{ID: "3", Name: "NotebookEdit", Input: map[string]any{"notebook_path": "n.ipynb", "new_source": "x"}},
			Event{Kind: KindFileChange, CallID: "3", Name: "NotebookEdit", Path: "n.ipynb", Diff: "+x"},
		},
		{
			claudeBlock{ID: "4", Name: "TodoWrite", Input: map[string]any{"todos": []any{
				map[string]any{"content": "first", "status": "completed"},
				"malformed",
			}}},
			Event{Kind: KindPlan, CallID: "4", Name: "TodoWrite", Input: map[string]any{"todos": []any{
				map[string]any{"content": "first", "status": "completed"},
				"malformed",
			}}, Items: []PlanItem{{Text: "first", Status: "completed"}, {}}},
		},
		{
			claudeBlock{ID: "5", Name: "TaskCreate", Input: map[string]any{"subject": "Write tests"}},
			Event{Kind: KindPlan, CallID: "5", Name: "TaskCreate", Input: map[string]any{"subject": "Write tests"}, Items: []PlanItem{{Text: "Write tests", Status: "pending"}}},
		},
		{
			claudeBlock{ID: "6", Name: "TaskUpdate", Input: map[string]any{"subject": "Write tests", "status": "completed"}},
			Event{Kind: KindPlan, CallID: "6", Name: "TaskUpdate", Input: map[string]any{"subject": "Write tests", "status": "completed"}, Items: []PlanItem{{Text: "Write tests", Status: "completed"}}},
		},
		{
			claudeBlock{ID: "7", Name: "TaskUpdate", Input: map[string]any{"taskId": float64(3), "status": "in_progress"}},
			Event{Kind: KindPlan, CallID: "7", Name: "TaskUpdate", Input: map[string]any{"taskId": float64(3), "status": "in_progress"}, Items: []PlanItem{{Text: "task 3", Status: "in_progress"}}},
		},
		{
			claudeBlock{ID: "8", Name: "WebFetch", Input: map[string]any{"url": "https://example.com"}},
			Event{Kind: KindToolCall, CallID: "8", Name: "WebFetch", Input: map[string]any{"url": "https://example.com"}},
		},
	}
	for _, test := range tests {
		assertEvents(t, []Event{claudeToolUse(test.block)}, []Event{test.want})
	}
}

func TestClaudeToolResultContent(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantText   string
		wantImages []string
	}{
		{"empty", "", "", nil},
		{"string", `"plain output"`, "plain output", nil},
		{"blocks", `[{"type":"text","text":"one"},{"type":"text","text":"two"}]`, "one\ntwo", nil},
		{"images", `[{"type":"image","source":{"type":"url","url":"https://example.com/a.png"}},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}},{"type":"image","source":{}}]`, "", []string{"https://example.com/a.png", "data:image/png;base64,AAAA"}},
		{"unknown block", `[{"type":"document","text":"skipped"},{"type":"text","text":"kept"}]`, "kept", nil},
		{"object", `{"unexpected":true}`, `{"unexpected":true}`, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			text, images := claudeToolResultContent(json.RawMessage(test.raw))
			if text != test.wantText {
				t.Errorf("text = %q, want %q", text, test.wantText)
			}
			if eventsJSON(t, []Event{{Images: images}}) != eventsJSON(t, []Event{{Images: test.wantImages}}) {
				t.Errorf("images = %v, want %v", images, test.wantImages)
			}
		})
	}
}

func TestClaudeMapperLines(t *testing.T) {
	mapper := &claudeMapper{}
	got := mapLines(mapper,
		`not json`,
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"  "},{"type":"thinking","thinking":""},{"type":"thinking","thinking":"hmm"},{"type":"tool_use","id":"b","name":"Bash","input":{"command":"false"}},{"type":"tool_use","id":"r","name":"Read","input":{"file_path":"x"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"text","text":"ignored"},{"type":"tool_result","tool_use_id":"b","content":"boom","is_error":true},{"type":"tool_result","tool_use_id":"r","content":[{"type":"image","source":{"url":"https://example.com/i.png"}}]}]}}`,
		`{"type":"result","is_error":true,"result":"rate limited","usage":{"input_tokens":5,"output_tokens":7,"cache_read_input_tokens":2,"cache_creation_input_tokens":3},"total_cost_usd":0.25}`,
		`{"type":"result","is_error":true,"result":""}`,
	)
	assertEvents(t, got, []Event{
		{Kind: KindReasoning, Text: "hmm"},
		{Kind: KindCommand, CallID: "b", Name: "Bash", Input: map[string]any{"command": "false"}, Command: "false", Status: "running"},
		{Kind: KindToolCall, CallID: "r", Name: "Read", Input: map[string]any{"file_path": "x"}},
		{Kind: KindCommand, CallID: "b", Output: "boom", IsError: true, ExitCode: intPtr(1), Status: "done"},
		{Kind: KindToolResult, CallID: "r", Images: []string{"https://example.com/i.png"}},
		{Kind: KindUsage, Usage: &Usage{InputTokens: 5, OutputTokens: 7, CacheReadTokens: 2, CacheWriteTokens: 3, CostUSD: 0.25}},
		{Kind: KindSystem, Text: "rate limited", IsError: true},
		{Kind: KindUsage, Usage: &Usage{}},
	})
	if len(mapper.calls) != 0 {
		t.Errorf("calls left open after their results: %v", mapper.calls)
	}
}

func TestCodexMapperLines(t *testing.T) {
	got := mapLines(&codexMapper{},
		`not json`,
		`{"type":"thread.started","thread_id":"t"}`,
		`{"type":"turn.completed","usage":{"input_tokens":3,"cached_input_tokens":10,"cache_write_input_tokens":4,"output_tokens":6}}`,
		`{"type":"turn.failed","error":{"message":"quota exceeded"}}`,
		`{"type":"error","message":"stream disconnected"}`,
		`{"type":"item.started","item":{"id":"c","type":"command_execution","command":"ls"}}`,
		`{"type":"item.completed","item":{"id":"c","type":"command_execution","command":"ls","aggregated_output":"out","exit_code":2,"status":"completed"}}`,
		`{"type":"item.completed","item":{"id":"k","type":"command_execution","command":"x","status":"failed"}}`,
		`{"type":"item.updated","item":{"id":"m","type":"agent_message","text":"partial"}}`,
		`{"type":"item.completed","item":{"id":"r","type":"reasoning","text":"  "}}`,
		`{"type":"item.started","item":{"id":"f","type":"file_change","changes":[{"path":"a","kind":"add"}]}}`,
		`{"type":"item.completed","item":{"id":"f","type":"file_change","changes":[{"path":"a","kind":"add","diff":"+x"},{"path":"b","kind":"delete"}]}}`,
		`{"type":"item.completed","item":{"id":"e1","type":"mcp_tool_call","server":"gh","tool":"search","arguments":{"q":"x"},"error":{"message":"denied"}}}`,
		`{"type":"item.completed","item":{"id":"e2","type":"mcp_tool_call","tool":"echo","result":{"structured":1},"status":"failed"}}`,
		`{"type":"item.completed","item":{"id":"e3","type":"mcp_tool_call","tool":"noop"}}`,
		`{"type":"item.started","item":{"id":"err","type":"error","message":"warning"}}`,
		`{"type":"item.completed","item":{"id":"err","type":"error","message":"model refused"}}`,
		`{"type":"item.completed","item":{"id":"u","type":"future_item"}}`,
		`{"type":"item.completed"}`,
	)
	assertEvents(t, got, []Event{
		{Kind: KindUsage, Usage: &Usage{InputTokens: 0, OutputTokens: 6, CacheReadTokens: 10, CacheWriteTokens: 4}},
		{Kind: KindSystem, Text: "quota exceeded", IsError: true},
		{Kind: KindSystem, Text: "stream disconnected", IsError: true},
		{Kind: KindCommand, CallID: "c", Command: "ls", Status: "running"},
		{Kind: KindCommand, CallID: "c", Command: "ls", Output: "out", IsError: true, ExitCode: intPtr(2), Status: "done"},
		{Kind: KindCommand, CallID: "k", Command: "x", IsError: true, Status: "done"},
		{Kind: KindFileChange, CallID: "f", Path: "a", Status: "add", Diff: "+x"},
		{Kind: KindFileChange, CallID: "f", Path: "b", Status: "delete"},
		{Kind: KindToolResult, CallID: "e1", Name: "gh.search", Input: map[string]any{"q": "x"}, Output: "denied", IsError: true},
		{Kind: KindToolResult, CallID: "e2", Name: "echo", Output: `{"structured":1}`, IsError: true},
		{Kind: KindToolResult, CallID: "e3", Name: "noop"},
		{Kind: KindSystem, Text: "model refused", IsError: true},
	})
}

func TestCodexWebSearchWithoutStartedItem(t *testing.T) {
	got := mapLines(&codexMapper{},
		`{"type":"item.completed","item":{"id":"w","type":"web_search","query":"aviary","status":"failed"}}`,
	)
	assertEvents(t, got, []Event{
		{Kind: KindToolCall, CallID: "w", Name: "web_search", Input: map[string]any{"query": "aviary"}},
		{Kind: KindToolResult, CallID: "w", Name: "web_search", Output: "failed", IsError: true},
	})
}

func TestStringValue(t *testing.T) {
	values := map[string]any{"s": "text", "n": float64(42), "f": 3.9, "b": true, "nil": nil}
	tests := []struct {
		m    map[string]any
		key  string
		want string
	}{
		{nil, "s", ""},
		{values, "missing", ""},
		{values, "nil", ""},
		{values, "s", "text"},
		{values, "n", "42"},
		{values, "f", "3"},
		{values, "b", "true"},
	}
	for _, test := range tests {
		if got := stringValue(test.m, test.key); got != test.want {
			t.Errorf("stringValue(%v, %q) = %q, want %q", test.m, test.key, got, test.want)
		}
	}
}
