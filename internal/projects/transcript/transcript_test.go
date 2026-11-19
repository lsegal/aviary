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

// The fixture is a real `aviary agent run --output-format json` capture from
// an agent backed by codex-cli 0.153.2.
func TestAviaryJSONMapsToEvents(t *testing.T) {
	events := mapFile(t, NewMapper(FormatAviaryJSON), "aviary-agent-run.jsonl")
	want := "message,tool_call,tool_result,tool_call,tool_result,message"
	if got := kinds(events); got != want {
		t.Fatalf("kinds = %s\nwant    %s", got, want)
	}
	call, failed := events[1], events[2]
	if call.Name != "shell" || call.CallID == "" || call.Input["command"] != "/bin/zsh -lc 'cat missing.txt'" {
		t.Fatalf("tool call = %+v", call)
	}
	if failed.CallID != call.CallID || !failed.IsError || !strings.Contains(failed.Output, "No such file") {
		t.Fatalf("failed result = %+v", failed)
	}
	if ok := events[4]; ok.CallID != events[3].CallID || ok.IsError || ok.Output != "note.txt\nout.txt\n" {
		t.Fatalf("result = %+v", ok)
	}
	if !strings.HasPrefix(events[5].Text, "`cat missing.txt` failed") {
		t.Fatalf("message = %+v", events[5])
	}
}

func TestAviaryJSONFileChangesAndErrors(t *testing.T) {
	mapper := NewMapper(FormatAviaryJSON)
	change := mapper.Map([]byte(`{"type":"file_change","name":"file_write","path":"/w/a.go","diff":"+x"}`))
	if len(change) != 1 || change[0].Kind != KindFileChange || change[0].Path != "/w/a.go" || change[0].Diff != "+x" {
		t.Fatalf("file change = %+v", change)
	}
	failed := mapper.Map([]byte(`{"type":"error","text":"boom"}`))
	if len(failed) != 1 || failed[0].Kind != KindSystem || !failed[0].IsError || failed[0].Text != "boom" {
		t.Fatalf("error = %+v", failed)
	}
	if got := mapper.Map([]byte("plain text")); got != nil {
		t.Fatalf("plain text mapped to %+v", got)
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
