package transcript

import (
	"os"
	"strings"
	"testing"
)

func mapFile(t *testing.T, mapper Mapper, name string) []Event {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	log, _ := Open("")
	recorder := log.Recorder(mapper)
	if _, err := recorder.Write(data); err != nil {
		t.Fatal(err)
	}
	recorder.Flush()
	if got := log.Raw(0); got != string(data) {
		t.Fatalf("raw output = %d bytes, want %d", len(got), len(data))
	}
	return log.Events(0)
}

func kinds(events []Event) string {
	parts := make([]string, len(events))
	for i, event := range events {
		parts[i] = string(event.Kind)
	}
	return strings.Join(parts, ",")
}

// The fixtures are real streams captured from claude 2.1.312 and codex-cli
// 0.153.2.
func TestClaudeStreamJSONMapsToEvents(t *testing.T) {
	events := mapFile(t, NewMapper(FormatClaudeStreamJSON), "claude-stream.jsonl")
	want := "command,tool_call,tool_result,command,message,tool_call,tool_result,plan,tool_result,plan,tool_result,message,usage"
	if got := kinds(events); got != want {
		t.Fatalf("kinds = %s\nwant    %s", got, want)
	}
	start, done := events[0], events[3]
	if start.Command != "echo hi" || start.Status != "running" || start.CallID == "" {
		t.Fatalf("command start = %+v", start)
	}
	if done.CallID != start.CallID || done.Output != "hi" || done.Status != "done" || done.ExitCode == nil || *done.ExitCode != 0 {
		t.Fatalf("command result = %+v", done)
	}
	if plan := events[7]; len(plan.Items) != 1 || plan.Items[0].Text != "Run echo hi via Bash" {
		t.Fatalf("plan = %+v", plan)
	}
	if usage := events[len(events)-1].Usage; usage == nil || usage.OutputTokens == 0 || usage.CostUSD == 0 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestClaudeEditsBecomeFileChanges(t *testing.T) {
	mapper := NewMapper(FormatClaudeStreamJSON)
	events := mapper.Map([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"/w/a.go","old_string":"a\nb","new_string":"c"}},{"type":"tool_use","id":"t2","name":"TodoWrite","input":{"todos":[{"content":"one","status":"completed"},{"content":"two","status":"pending"}]}}]}}`))
	if kinds(events) != "file_change,plan" {
		t.Fatalf("kinds = %s", kinds(events))
	}
	if events[0].Path != "/w/a.go" || events[0].Diff != "@@\n-a\n-b\n+c" {
		t.Fatalf("file change = %+v", events[0])
	}
	if len(events[1].Items) != 2 || events[1].Items[0].Status != "completed" {
		t.Fatalf("plan = %+v", events[1])
	}
	result := mapper.Map([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"ok"}]}]}}`))
	if len(result) != 1 || result[0].Kind != KindToolResult || result[0].Output != "ok" {
		t.Fatalf("result = %+v", result)
	}
}

func TestCodexJSONMapsToEvents(t *testing.T) {
	events := mapFile(t, NewMapper(FormatCodexJSON), "codex-command.jsonl")
	if got, want := kinds(events), "message,command,command,message,usage"; got != want {
		t.Fatalf("kinds = %s, want %s", got, want)
	}
	done := events[2]
	if done.Status != "done" || done.Output != "hi\n" || done.ExitCode == nil || *done.ExitCode != 0 || done.IsError {
		t.Fatalf("command = %+v", done)
	}
	if events[4].Usage.InputTokens != 35022 || events[4].Usage.CacheReadTokens != 29312 {
		t.Fatalf("usage = %+v", events[4].Usage)
	}
	changes := mapFile(t, NewMapper(FormatCodexJSON), "codex-file-change.jsonl")
	if got, want := kinds(changes), "message,file_change,message,usage"; got != want {
		t.Fatalf("kinds = %s, want %s", got, want)
	}
	if changes[1].Path != "/work/note.txt" || changes[1].Status != "add" {
		t.Fatalf("file change = %+v", changes[1])
	}
}

func TestCodexTodoListAndToolCalls(t *testing.T) {
	mapper := NewMapper(FormatCodexJSON)
	plan := mapper.Map([]byte(`{"type":"item.updated","item":{"id":"i","type":"todo_list","items":[{"text":"a","completed":true},{"text":"b","completed":false}]}}`))
	if len(plan) != 1 || plan[0].Kind != KindPlan || plan[0].Items[0].Status != "completed" || plan[0].Items[1].Status != "pending" {
		t.Fatalf("plan = %+v", plan)
	}
	call := mapper.Map([]byte(`{"type":"item.started","item":{"id":"m","type":"mcp_tool_call","server":"aviary","tool":"file_read","arguments":{"path":"x"}}}`))
	if len(call) != 1 || call[0].Kind != KindToolCall || call[0].Name != "aviary.file_read" {
		t.Fatalf("call = %+v", call)
	}
	failed := mapper.Map([]byte(`{"type":"item.completed","item":{"id":"m","type":"mcp_tool_call","server":"aviary","tool":"file_read","error":{"message":"denied"}}}`))
	if len(failed) != 1 || !failed[0].IsError || failed[0].Output != "denied" {
		t.Fatalf("result = %+v", failed)
	}
}

func TestLogPersistsAcrossOpen(t *testing.T) {
	dir := t.TempDir()
	log, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	log.System("claimed")
	recorder := log.Recorder(nil)
	_, _ = recorder.Write([]byte("raw line\n"))
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	events := reopened.Events(0)
	if len(events) != 1 || events[0].Kind != KindSystem || events[0].Text != "claimed" || events[0].Time.IsZero() {
		t.Fatalf("events = %+v", events)
	}
	if reopened.Raw(0) != "raw line\n" {
		t.Fatalf("raw = %q", reopened.Raw(0))
	}
	if got := reopened.Events(1); len(got) != 1 {
		t.Fatalf("limited events = %d", len(got))
	}
}

func TestUnknownFormatHasNoMapper(t *testing.T) {
	if NewMapper("text") != nil {
		t.Fatal("text output should have no structured mapper")
	}
}

func TestAviaryJSONMapsToEvents(t *testing.T) {
	log, _ := Open("")
	recorder := log.Recorder(NewMapper(FormatAviaryJSON))
	stream := strings.Join([]string{
		`{"kind":"message","time":"2026-11-19T10:00:00Z","text":"Renaming."}`,
		`{"kind":"tool_call","name":"file_edit","input":{"path":"main.go"}}`,
		`{"kind":"file_change","name":"file_edit","path":"/clone/main.go","diff":"--- a\n+++ b\n@@ -1 +1 @@\n-package main\n+package app\n"}`,
		`{"kind":"comment_relay","author":"someone","text":"not from the agent"}`,
		`{"kind":"system","text":"not from the agent"}`,
		`Error: agent "x" not found`,
	}, "\n") + "\n"
	if _, err := recorder.Write([]byte(stream)); err != nil {
		t.Fatal(err)
	}
	recorder.Flush()
	events := log.Events(0)
	if kinds(events) != "message,tool_call,file_change" {
		t.Fatalf("kinds = %s", kinds(events))
	}
	if events[0].Text != "Renaming." || events[0].Time.IsZero() {
		t.Fatalf("message = %+v", events[0])
	}
	if events[1].Name != "file_edit" || events[1].Input["path"] != "main.go" {
		t.Fatalf("tool call = %+v", events[1])
	}
	change := events[2]
	if change.Name != "file_edit" || change.Path != "/clone/main.go" || !strings.Contains(change.Diff, "+package app") || change.Time.IsZero() {
		t.Fatalf("file change = %+v", change)
	}
}
