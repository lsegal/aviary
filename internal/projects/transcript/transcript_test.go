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

// The fixtures are real streams captured from opencode 1.18.27 and cline
// 3.0.74 running the same four-step task: a shell command, a file creation, an
// edit and a read.
func TestOpencodeJSONMapsToEvents(t *testing.T) {
	events := mapFile(t, NewMapper(FormatOpencodeJSON), "opencode-run.jsonl")
	want := "command,command,usage,file_change,tool_result,usage,file_change,tool_result,usage,tool_call,tool_result,usage,message,usage"
	if got := kinds(events); got != want {
		t.Fatalf("kinds = %s\nwant    %s", got, want)
	}
	start, done := events[0], events[1]
	if start.Command != "echo hi" || start.Status != "running" || start.CallID == "" {
		t.Fatalf("command start = %+v", start)
	}
	if done.CallID != start.CallID || done.Output != "hi\n" || done.Status != "done" || done.ExitCode == nil || *done.ExitCode != 0 || done.IsError {
		t.Fatalf("command result = %+v", done)
	}
	added, edited := events[3], events[6]
	if added.Path != "/work/note.txt" || added.Status != "add" || !strings.Contains(added.Diff, "+hello") {
		t.Fatalf("added file = %+v", added)
	}
	if edited.Status != "update" || !strings.Contains(edited.Diff, "-hello\n+hello world") {
		t.Fatalf("edited file = %+v", edited)
	}
	if read := events[9]; read.Name != "read" || read.Input["filePath"] != "/work/note.txt" {
		t.Fatalf("tool call = %+v", read)
	}
	if usage := events[2].Usage; usage == nil || usage.InputTokens != 9668 || usage.OutputTokens != 32 {
		t.Fatalf("usage = %+v", usage)
	}
	if msg := events[12]; msg.Text != "`note.txt` now contains `hello world`." {
		t.Fatalf("message = %+v", msg)
	}
}

func TestOpencodeFailedToolsAndEdits(t *testing.T) {
	mapper := NewMapper(FormatOpencodeJSON)
	failed := mapper.Map([]byte(`{"type":"tool_use","part":{"type":"tool","tool":"bash","callID":"c1","state":{"status":"completed","input":{"command":"false"},"output":"","metadata":{"exit":1}}}}`))
	if kinds(failed) != "command,command" || !failed[1].IsError || *failed[1].ExitCode != 1 {
		t.Fatalf("failed command = %+v", failed)
	}
	edit := mapper.Map([]byte(`{"type":"tool_use","part":{"type":"tool","tool":"edit","callID":"c2","state":{"status":"error","input":{"filePath":"/w/a.go","oldString":"a","newString":"b"},"error":"no match"}}}`))
	if kinds(edit) != "file_change,tool_result" || edit[0].Path != "/w/a.go" || edit[0].Diff != "@@\n-a\n+b" || !edit[1].IsError || edit[1].Output != "no match" {
		t.Fatalf("edit = %+v", edit)
	}
	plan := mapper.Map([]byte(`{"type":"tool_use","part":{"type":"tool","tool":"todowrite","callID":"c3","state":{"status":"completed","input":{"todos":[{"content":"one","status":"completed"}]}}}}`))
	if kinds(plan) != "plan,tool_result" || plan[0].Items[0].Text != "one" {
		t.Fatalf("plan = %+v", plan)
	}
	if got := mapper.Map([]byte(`{"type":"error","error":{"name":"APIError","data":{"message":"rate limited"}}}`)); len(got) != 1 || !got[0].IsError || got[0].Text != "rate limited" {
		t.Fatalf("error = %+v", got)
	}
}

func TestClineJSONMapsToEvents(t *testing.T) {
	events := mapFile(t, NewMapper(FormatClineJSON), "cline-run.jsonl")
	want := "message,reasoning,command,command,file_change,tool_result,file_change,tool_result,tool_call,tool_result,message,usage"
	if got := kinds(events); got != want {
		t.Fatalf("kinds = %s\nwant    %s", got, want)
	}
	start, done := events[2], events[3]
	if start.Command != "echo hi" || start.Status != "running" || start.CallID == "" {
		t.Fatalf("command start = %+v", start)
	}
	if done.CallID != start.CallID || done.Output != "hi\n" || done.Status != "done" || done.ExitCode == nil || *done.ExitCode != 0 {
		t.Fatalf("command result = %+v", done)
	}
	if added := events[4]; added.Path != "note.txt" || added.Status != "add" || added.Diff != "+hello" {
		t.Fatalf("added file = %+v", added)
	}
	if edited := events[6]; edited.Status != "update" || edited.Diff != "@@\n-hello\n+hello world" {
		t.Fatalf("edited file = %+v", edited)
	}
	if read := events[9]; read.Output != "1 | hello world" || read.IsError {
		t.Fatalf("tool result = %+v", read)
	}
	if usage := events[11].Usage; usage == nil || usage.InputTokens != 42481 || usage.CacheReadTokens != 29952 || usage.CostUSD == 0 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestClineFailuresAndEditor(t *testing.T) {
	mapper := NewMapper(FormatClineJSON)
	edit := mapper.Map([]byte(`{"type":"agent_event","event":{"type":"content_start","contentType":"tool","toolCallId":"e","toolName":"editor","input":{"path":"/w/a.go","old_text":"a","new_text":"b"}}}`))
	if kinds(edit) != "file_change" || edit[0].Path != "/w/a.go" || edit[0].Diff != "@@\n-a\n+b" {
		t.Fatalf("edit = %+v", edit)
	}
	mapper.Map([]byte(`{"type":"agent_event","event":{"type":"content_start","contentType":"tool","toolCallId":"r","toolName":"run_commands","input":{"commands":["make","go test"]}}}`))
	failed := mapper.Map([]byte(`{"type":"agent_event","event":{"type":"content_end","contentType":"tool","toolCallId":"r","toolName":"run_commands","output":[{"query":"make","result":"ok","success":true},{"query":"go test","result":"FAIL","error":"exit 1","success":false}]}}`))
	if len(failed) != 1 || failed[0].Kind != KindCommand || !failed[0].IsError || *failed[0].ExitCode != 1 || failed[0].Output != "ok\nFAIL\nexit 1" {
		t.Fatalf("failed command = %+v", failed)
	}
	aborted := mapper.Map([]byte(`{"type":"agent_event","event":{"type":"done","reason":"error","text":"provider failed","usage":{"inputTokens":5}}}`))
	if kinds(aborted) != "usage,system" || !aborted[1].IsError || aborted[1].Text != "provider failed" {
		t.Fatalf("done = %+v", aborted)
	}
	if got := mapper.Map([]byte(`{"ts":"2026-11-19T10:55:27.395Z","type":"error","message":"cline requires re-authentication."}`)); len(got) != 1 || !got[0].IsError {
		t.Fatalf("error = %+v", got)
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
