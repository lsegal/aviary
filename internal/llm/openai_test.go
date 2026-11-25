package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCodexTestServer returns a Codex provider pointed at a test server that
// records the request body and replies with the given SSE lines.
func newCodexTestServer(t *testing.T, lines ...string) (*OpenAICodexProvider, *map[string]any) {
	t.Helper()
	body := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		assert.NoError(t, json.Unmarshal(raw, &body))
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range lines {
			_, _ = fmt.Fprintf(w, "%s\n\n", line)
		}
	}))
	t.Cleanup(srv.Close)
	p := NewOpenAICodexProvider("test-token", "gpt-5.5")
	p.httpClient = &http.Client{Transport: &rewriteURLTransport{target: srv.URL, base: http.DefaultTransport}}
	return p, &body
}

// newOpenAITestServer returns an OpenAI provider pointed at a test server that
// records the request body and replies with the given chat completion chunks.
func newOpenAITestServer(t *testing.T, chunks ...string) (*OpenAIProvider, *map[string]any) {
	t.Helper()
	body := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		assert.NoError(t, json.Unmarshal(raw, &body))
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range chunks {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return NewOpenAIProvider("test-key", "gpt-4o", srv.URL), &body
}

func openAIToolChunk(index int, id, name, args string) string {
	return fmt.Sprintf(`{"id":"c","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":%d,"id":%q,"type":"function","function":{"name":%q,"arguments":%q}}]}}]}`, index, id, name, args)
}

func eventsOfType(events []Event, typ EventType) []Event {
	var out []Event
	for _, ev := range events {
		if ev.Type == typ {
			out = append(out, ev)
		}
	}
	return out
}

func TestOpenAICodexProvider_Stream_EncodesToolConversationAndTools(t *testing.T) {
	p, body := newCodexTestServer(t, "data: [DONE]")
	ch, err := p.Stream(context.Background(), Request{
		Messages: []Message{
			{Role: RoleUser, Content: "list files"},
			{Role: RoleAssistant, ToolCall: &ToolCall{ID: "call_1", Name: "ls", Arguments: map[string]any{"path": "."}}},
			{Role: RoleUser, Result: &ToolResult{ToolCallID: "call_1", Content: "a.txt"}},
		},
		Tools: []ToolDefinition{{
			Name:        "ls",
			Description: "List files",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
		}},
	})
	require.NoError(t, err)
	collectEvents(t, ch)

	assert.Equal(t, false, (*body)["store"])
	assert.Equal(t, true, (*body)["stream"])
	input, _ := (*body)["input"].([]any)
	require.Len(t, input, 3)
	assert.Equal(t, map[string]any{"type": "function_call", "call_id": "call_1", "name": "ls", "arguments": `{"path":"."}`}, input[1])
	assert.Equal(t, map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "a.txt"}, input[2])

	tools, _ := (*body)["tools"].([]any)
	require.Len(t, tools, 1)
	tool := tools[0].(map[string]any)
	assert.Equal(t, "function", tool["type"])
	assert.Equal(t, "ls", tool["name"])
	assert.Equal(t, true, tool["strict"])
	params := tool["parameters"].(map[string]any)
	assert.Equal(t, []any{"path"}, params["required"])
	assert.Equal(t, false, params["additionalProperties"])
}

func TestOpenAICodexProvider_Stream_EmitsToolCallsInOutputOrder(t *testing.T) {
	p, _ := newCodexTestServer(t,
		"event: ignored",
		"data: not json",
		"data: ",
		`data: {"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","call_id":"c2","name":"second"}}`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"c0","name":"first"}}`,
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","call_id":"c1","name":" "}}`,
		`data: {"type":"response.output_item.added","output_index":3,"item":{"type":"message"}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"a\":"}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"1}"}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":9,"delta":"orphan"}`,
		`data: {"type":"response.function_call_arguments.done","output_index":0,"arguments":"{\"a\":1}"}`,
		`data: {"type":"response.output_text.delta","delta":""}`,
		`data: {"type":"response.completed","response":{"id":"resp_42","usage":{"input_tokens":0,"output_tokens":0}}}`,
	)
	ch, err := p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "go"}}})
	require.NoError(t, err)
	events := collectEvents(t, ch)

	assert.Empty(t, eventsOfType(events, EventTypeText))
	// Zero usage is not reported.
	assert.Empty(t, eventsOfType(events, EventTypeUsage))
	calls := eventsOfType(events, EventTypeToolCall)
	require.Len(t, calls, 2)
	assert.Equal(t, ToolCall{ID: "c0", Name: "first", Arguments: map[string]any{"a": float64(1)}}, *calls[0].ToolCall)
	assert.Equal(t, ToolCall{ID: "c2", Name: "second", Arguments: map[string]any{}}, *calls[1].ToolCall)
	done := events[len(events)-1]
	assert.Equal(t, EventTypeDone, done.Type)
	assert.Equal(t, "resp_42", done.ResponseID)
}

func TestOpenAICodexProvider_Stream_InvalidToolArguments(t *testing.T) {
	p, _ := newCodexTestServer(t,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"c0","name":"bad"}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{not json"}`,
	)
	ch, err := p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "go"}}})
	require.NoError(t, err)
	events := collectEvents(t, ch)
	require.Len(t, events, 1)
	assert.Equal(t, EventTypeError, events[0].Type)
	assert.ErrorContains(t, events[0].Error, `openai codex tool arguments for "bad"`)
}

func TestOpenAICodexProvider_Stream_FailedWithoutCode(t *testing.T) {
	p, _ := newCodexTestServer(t, `data: {"type":"response.failed","response":{"error":{"message":"boom"}}}`)
	ch, err := p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "go"}}})
	require.NoError(t, err)
	events := collectEvents(t, ch)
	require.Len(t, events, 1)
	assert.EqualError(t, events[0].Error, "openai codex: response.failed: boom")
}

func TestOpenAICodexProvider_Stream_ScannerError(t *testing.T) {
	p, _ := newCodexTestServer(t, "data: "+strings.Repeat("x", sseScannerMaxTokenSize+1))
	ch, err := p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "go"}}})
	require.NoError(t, err)
	events := collectEvents(t, ch)
	require.Len(t, events, 1)
	assert.ErrorContains(t, events[0].Error, "openai codex stream:")
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("dial refused")
}

func TestOpenAICodexProvider_Stream_TransportError(t *testing.T) {
	p := NewOpenAICodexProvider("test-token", "gpt-5.5")
	p.httpClient = &http.Client{Transport: failingTransport{}}
	_, err := p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "go"}}})
	require.ErrorContains(t, err, "openai codex:")
	require.ErrorContains(t, err, "dial refused")
}

func TestOpenAICodexProvider_Stream_NilContextFailsBuildingRequest(t *testing.T) {
	p := NewOpenAICodexProvider("test-token", "gpt-5.5")
	//nolint:staticcheck // a nil context is the only way to make request construction fail.
	_, err := p.Stream(nil, Request{})
	require.ErrorContains(t, err, "openai codex: building request")
}

func TestOpenAIProvider_Stream_EncodesToolConversation(t *testing.T) {
	p, body := newOpenAITestServer(t)
	ch, err := p.Stream(context.Background(), Request{
		Messages: []Message{
			{Role: RoleUser, Content: "check"},
			{Role: RoleAssistant, Content: "Let me look."},
			{Role: RoleAssistant, ToolCall: &ToolCall{Name: "read", Arguments: map[string]any{"f": "a"}}},
			{Role: RoleAssistant, Content: "And this.", ToolCall: &ToolCall{ID: "call_b", Name: "read"}},
			{Role: RoleUser, Result: &ToolResult{ToolCallID: "call_2", Content: "A"}},
			{Role: RoleUser, Result: &ToolResult{ToolCallID: "call_b", Content: "  "}},
			{Role: RoleUser, Result: &ToolResult{ToolCallID: "", Content: "orphan"}},
			{Role: RoleAssistant, Content: "Done."},
			{Role: RoleAssistant, Content: "Really."},
		},
	})
	require.NoError(t, err)
	collectEvents(t, ch)

	msgs, _ := (*body)["messages"].([]any)
	require.Len(t, msgs, 4)

	asst := msgs[1].(map[string]any)
	assert.Equal(t, "assistant", asst["role"])
	assert.Equal(t, "Let me look.\nAnd this.", asst["content"])
	calls := asst["tool_calls"].([]any)
	require.Len(t, calls, 2)
	first := calls[0].(map[string]any)
	// A tool call without an ID gets one derived from its message position.
	assert.Equal(t, "call_2", first["id"])
	assert.Equal(t, map[string]any{"name": "read", "arguments": `{"f":"a"}`}, first["function"])
	second := calls[1].(map[string]any)
	assert.Equal(t, "call_b", second["id"])
	assert.Equal(t, map[string]any{"name": "read", "arguments": "{}"}, second["function"])

	// Empty results and results without a tool call ID are dropped.
	assert.Equal(t, map[string]any{"role": "tool", "content": "A", "tool_call_id": "call_2"}, msgs[2])
	assert.Equal(t, map[string]any{"role": "assistant", "content": "Done.\nReally."}, msgs[3])
}

func TestOpenAIProvider_Stream_ToolCallWithoutText(t *testing.T) {
	p, body := newOpenAITestServer(t)
	ch, err := p.Stream(context.Background(), Request{Messages: []Message{
		{Role: RoleAssistant, ToolCall: &ToolCall{ID: "x", Name: "noop"}},
	}})
	require.NoError(t, err)
	collectEvents(t, ch)
	msgs := (*body)["messages"].([]any)
	require.Len(t, msgs, 1)
	_, hasContent := msgs[0].(map[string]any)["content"]
	assert.False(t, hasContent)
}

func TestOpenAIProvider_Stream_ToolDefinitionsIncludeExamples(t *testing.T) {
	p, body := newOpenAITestServer(t)
	ch, err := p.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []ToolDefinition{
			{
				Name:        "search",
				Description: "Search things",
				InputSchema: map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
				Examples:    []map[string]any{{"q": "a"}, {"q": "b"}, {"q": "c"}, {"q": "d"}},
			},
			{Name: "bare", Examples: []map[string]any{{"x": 1}}},
		},
	})
	require.NoError(t, err)
	collectEvents(t, ch)

	tools := (*body)["tools"].([]any)
	require.Len(t, tools, 2)
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	assert.Equal(t, "search", fn["name"])
	assert.Equal(t, true, fn["strict"])
	// Only the first three examples are included.
	assert.Equal(t, "Search things\n\nExample arguments:\n- {\"q\":\"a\"}\n- {\"q\":\"b\"}\n- {\"q\":\"c\"}", fn["description"])
	assert.Equal(t, []any{"q"}, fn["parameters"].(map[string]any)["required"])

	bare := tools[1].(map[string]any)["function"].(map[string]any)
	assert.Equal(t, "Example arguments:\n- {\"x\":1}", bare["description"])
	assert.Equal(t, map[string]any{"type": "object", "properties": map[string]any{}, "required": []any{}, "additionalProperties": false}, bare["parameters"])
}

func TestOpenAIProvider_Stream_AccumulatesToolCallDeltas(t *testing.T) {
	p, _ := newOpenAITestServer(t,
		openAIToolChunk(1, "id1", "later", `{"b":`),
		openAIToolChunk(0, "id0", "first", ""),
		openAIToolChunk(1, "", "", `2}`),
		openAIToolChunk(2, "id2", "", `{}`),
	)
	ch, err := p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "go"}}})
	require.NoError(t, err)
	events := collectEvents(t, ch)

	calls := eventsOfType(events, EventTypeToolCall)
	require.Len(t, calls, 2)
	assert.Equal(t, ToolCall{ID: "id0", Name: "first", Arguments: map[string]any{}}, *calls[0].ToolCall)
	assert.Equal(t, ToolCall{ID: "id1", Name: "later", Arguments: map[string]any{"b": float64(2)}}, *calls[1].ToolCall)
	assert.Equal(t, EventTypeDone, events[len(events)-1].Type)
}

func TestOpenAIProvider_Stream_InvalidToolArguments(t *testing.T) {
	p, _ := newOpenAITestServer(t, openAIToolChunk(0, "id0", "bad", "[1,2]"))
	ch, err := p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "go"}}})
	require.NoError(t, err)
	events := collectEvents(t, ch)
	require.Len(t, events, 1)
	assert.ErrorContains(t, events[0].Error, `openai tool arguments for "bad"`)
}

func TestSchemaObject(t *testing.T) {
	empty := map[string]any{"type": "object", "properties": map[string]any{}}
	type typedSchema struct {
		Type  string `json:"type"`
		AnyOf []any  `json:"anyOf"`
	}
	tests := []struct {
		name   string
		schema any
		want   map[string]any
	}{
		{"nil", nil, empty},
		{"empty map", map[string]any{}, empty},
		{"unmarshalable map", map[string]any{"bad": make(chan int)}, empty},
		{"unmarshalable value", make(chan int), empty},
		{"non-object JSON", []string{"a"}, empty},
		{"struct strips combiners", typedSchema{Type: "object", AnyOf: []any{1}}, map[string]any{"type": "object", "properties": map[string]any{}}},
		{"untyped gets properties", map[string]any{"description": "d", "oneOf": []any{}, "allOf": []any{}, "not": map[string]any{}}, map[string]any{"description": "d", "properties": map[string]any{}}},
		{"non-object keeps shape", map[string]any{"type": "string"}, map[string]any{"type": "string"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, schemaObject(tt.schema))
		})
	}
}

func TestStrictSchemaObjectAppliesRulesToNestedSchemas(t *testing.T) {
	schema := strictSchemaObject(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"list": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":  "object",
					"anyOf": []any{},
				},
			},
			"tuple": map[string]any{
				"items": []any{map[string]any{"properties": map[string]any{"z": map[string]any{}}}},
			},
		},
	})
	list := schema["properties"].(map[string]any)["list"].(map[string]any)
	assert.Equal(t, map[string]any{"type": "object", "properties": map[string]any{}, "required": []any{}, "additionalProperties": false}, list["items"])

	tuple := schema["properties"].(map[string]any)["tuple"].(map[string]any)
	item := tuple["items"].([]any)[0].(map[string]any)
	assert.Equal(t, []any{"z"}, item["required"])
	assert.Equal(t, false, item["additionalProperties"])
}

func TestParseToolArguments(t *testing.T) {
	args, err := parseToolArguments("  ")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{}, args)

	args, err = parseToolArguments("null")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{}, args)

	args, err = parseToolArguments(`{"k":"v"}`)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"k": "v"}, args)

	_, err = parseToolArguments(`"str"`)
	require.Error(t, err)
}

func TestMustJSONMap(t *testing.T) {
	assert.Equal(t, "{}", mustJSONMap(nil))
	assert.Equal(t, "{}", mustJSONMap(map[string]any{"c": make(chan int)}))
	assert.Equal(t, `{"a":1}`, mustJSONMap(map[string]any{"a": 1}))
}

func TestSortHelpers(t *testing.T) {
	ints := []int{3, 1, 2, 1}
	sortInts(ints)
	assert.Equal(t, []int{1, 1, 2, 3}, ints)

	strs := []string{"b", "c", "a"}
	sortStrings(strs)
	assert.Equal(t, []string{"a", "b", "c"}, strs)
}

func TestExtractChatGPTAccountID_InvalidTokens(t *testing.T) {
	assert.Equal(t, "", extractChatGPTAccountID("no-dots"))
	assert.Equal(t, "", extractChatGPTAccountID("h.!!!.s"))
	assert.Equal(t, "", extractChatGPTAccountID("h.bm90LWpzb24.s")) // "not-json"
}

func TestOpenAIFileInput_NamedAndInvalid(t *testing.T) {
	_, _, ok := openAIFileInput("https://example.com/a.pdf")
	assert.False(t, ok)
}
