package mcp

import (
	"context"
	"errors"
	"reflect"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/agent"
)

// A native runner reports a tool call and then its result without IDs, and a
// result may be empty, so the result is told apart by Done and paired with
// the oldest unfinished call of the same tool.
func TestAgentRunEventsPairToolCallsWithoutIDs(t *testing.T) {
	events := &agentRunEvents{}
	var got []AgentRunEvent
	for _, e := range []agent.StreamEvent{
		{Type: agent.StreamEventText, Text: "Reading."},
		{Type: agent.StreamEventTool, Tool: &agent.ToolEvent{Name: "file_read", Args: map[string]any{"path": "a"}}},
		{Type: agent.StreamEventTool, Tool: &agent.ToolEvent{Name: "file_read", Args: map[string]any{"path": "b"}}},
		{Type: agent.StreamEventTool, Tool: &agent.ToolEvent{Name: "file_read", Done: true}},
		{Type: agent.StreamEventTool, Tool: &agent.ToolEvent{Name: "file_read", Error: "denied", Done: true}},
		{Type: agent.StreamEventFile, File: &agent.FileChangeEvent{Tool: "file_write", Path: "/w/a", Diff: "+x"}},
		{Type: agent.StreamEventTool, Tool: &agent.ToolEvent{ID: "item_1", Name: "shell", Result: "ok", Done: true}},
		{Type: agent.StreamEventStatus, Text: "verbose"},
		{Type: agent.StreamEventError, Err: errors.New("boom")},
		{Type: agent.StreamEventDone},
	} {
		if event, ok := events.fromStream(e); ok {
			got = append(got, event)
		}
	}
	want := []AgentRunEvent{
		{Type: "text", Text: "Reading."},
		{Type: "tool_call", ID: "call_1", Name: "file_read", Input: map[string]any{"path": "a"}},
		{Type: "tool_call", ID: "call_2", Name: "file_read", Input: map[string]any{"path": "b"}},
		{Type: "tool_result", ID: "call_1", Name: "file_read"},
		{Type: "tool_result", ID: "call_2", Name: "file_read", Error: "denied"},
		{Type: "file_change", Name: "file_write", Path: "/w/a", Diff: "+x"},
		{Type: "tool_result", ID: "item_1", Name: "shell", Output: "ok"},
		{Type: "error", Text: "boom"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events =\n%+v\nwant\n%+v", got, want)
	}
}

// Every progress message a tool sends reaches the caller's handler, in order,
// before the call returns, even though the SDK delivers them out of step with
// the result.
func TestCallToolWithProgressDeliversProgressBeforeReturning(t *testing.T) {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test"}, nil)
	sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "count"}, func(ctx context.Context, req *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		for i, message := range []string{"one", "two", "three", "last"} {
			_ = req.Session.NotifyProgress(ctx, &sdkmcp.ProgressNotificationParams{
				ProgressToken: req.Params.GetProgressToken(),
				Progress:      float64(i + 1),
				Message:       message,
			})
		}
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "done"}}}, struct{}{}, nil
	})
	client, err := NewInProcessClient(context.Background(), srv)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close() //nolint:errcheck
	var got []string
	result, err := client.CallToolWithProgress(context.Background(), "count", map[string]any{}, func(message string) bool {
		got = append(got, message)
		return message == "last"
	})
	if err != nil {
		t.Fatal(err)
	}
	if text := extractText(result); text != "done" {
		t.Fatalf("result = %q", text)
	}
	if !reflect.DeepEqual(got, []string{"one", "two", "three", "last"}) {
		t.Fatalf("progress = %v", got)
	}
}

// A tool call's announcement and its result reach the web chat as tool
// progress with the same ID, and the result carries its outcome, so the chat
// can finish the call's one block instead of adding a second (#610).
func TestToolProgressSharesIDBetweenCallAndResult(t *testing.T) {
	events := &agentRunEvents{}
	args := map[string]any{"command": "gh repo view"}
	got := []toolProgress{
		events.toolProgress(&agent.ToolEvent{Name: "exec", Args: args}),
		events.toolProgress(&agent.ToolEvent{Name: "exec", Args: args, Result: "public", Done: true}),
		events.toolProgress(&agent.ToolEvent{Name: "exec", Args: args}),
		events.toolProgress(&agent.ToolEvent{Name: "exec", Args: args, Error: "denied", Done: true}),
		events.toolProgress(&agent.ToolEvent{ID: "toolu_1", Name: "Bash", Args: args}),
	}
	want := []toolProgress{
		{ID: "call_1", Name: "exec", Args: args},
		{ID: "call_1", Name: "exec", Args: args, Result: "public", Done: true},
		{ID: "call_2", Name: "exec", Args: args},
		{ID: "call_2", Name: "exec", Args: args, Error: "denied", Done: true},
		{ID: "toolu_1", Name: "Bash", Args: args},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("progress =\n%+v\nwant\n%+v", got, want)
	}
}
