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
	if events[4].Usage.InputTokens != 35022-29312 || events[4].Usage.CacheReadTokens != 29312 {
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

// The fixture is a real stream from gemini-cli 0.58.0 running a plan, two
// shell commands (the second failing), a file creation, an edit, a read and an
// edit that matches nothing, then streaming its reply in three deltas. Gemini's
// Code Assist sign-in is closed to this CLI, so the model side of the run was
// a local stand-in served through GOOGLE_GEMINI_BASE_URL; every line is the
// CLI's own output.
func TestGeminiStreamJSONMapsToEvents(t *testing.T) {
	events := mapFile(t, NewMapper(FormatGeminiJSON), "gemini-run.jsonl")
	want := "message,plan,tool_result,command,command,file_change,tool_result,file_change,tool_result,tool_call,tool_result,command,command,file_change,tool_result,plan,tool_result,message,usage"
	if got := kinds(events); got != want {
		t.Fatalf("kinds = %s\nwant    %s", got, want)
	}
	if msg := events[0]; msg.Text != "I will plan the work first." {
		t.Fatalf("first message = %+v", msg)
	}
	if plan := events[1]; len(plan.Items) != 3 || plan.Items[0] != (PlanItem{Text: "Run echo hi", Status: "in_progress"}) || plan.Items[2].Status != "pending" {
		t.Fatalf("plan = %+v", plan)
	}
	start, done := events[3], events[4]
	if start.Command != "echo hi" || start.Status != "running" || start.CallID == "" {
		t.Fatalf("command start = %+v", start)
	}
	if done.CallID != start.CallID || done.Output != "hi" || done.Status != "done" || done.IsError || done.ExitCode != nil {
		t.Fatalf("command result = %+v", done)
	}
	if added := events[5]; added.Path != "/work/note.txt" || added.Diff != "+hello" {
		t.Fatalf("added file = %+v", added)
	}
	if edited := events[7]; edited.Diff != "@@\n-hello\n+hello world" {
		t.Fatalf("edited file = %+v", edited)
	}
	if read := events[9]; read.Name != "read_file" || read.Input["file_path"] != "/work/note.txt" {
		t.Fatalf("tool call = %+v", read)
	}
	if missed := events[14]; !missed.IsError || !strings.Contains(missed.Output, "Could not find an exact match") || missed.CallID != events[13].CallID {
		t.Fatalf("failed edit = %+v", missed)
	}
	if plan := events[15]; plan.Items[2].Status != "completed" {
		t.Fatalf("final plan = %+v", plan)
	}
	if msg := events[17]; msg.Text != "Done: `note.txt` now says \"hello world\"." {
		t.Fatalf("streamed message = %q", msg.Text)
	}
	if usage := events[18].Usage; usage == nil || usage.InputTokens != 3600 || usage.CacheReadTokens != 7200 || usage.OutputTokens != 180 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestGeminiErrorsAndUnfinishedMessages(t *testing.T) {
	mapper := NewMapper(FormatGeminiJSON)
	if got := mapper.Map([]byte(`{"type":"message","role":"user","content":"do it"}`)); got != nil {
		t.Fatalf("user message = %+v", got)
	}
	warning := mapper.Map([]byte(`{"type":"error","severity":"warning","message":"retrying after 429"}`))
	if kinds(warning) != "system" || warning[0].IsError || warning[0].Text != "retrying after 429" {
		t.Fatalf("warning = %+v", warning)
	}
	failed := mapper.Map([]byte(`{"type":"result","status":"error","error":{"type":"FatalTurnLimitedError","message":"Reached max session turns"},"stats":{"input_tokens":10,"output_tokens":2,"cached":0,"input":10}}`))
	if kinds(failed) != "usage,system" || !failed[1].IsError || failed[1].Text != "Reached max session turns" {
		t.Fatalf("failed result = %+v", failed)
	}
	// A stream cut off mid-message still records what was said.
	log, _ := Open("")
	recorder := log.Recorder(NewMapper(FormatGeminiJSON))
	_, _ = recorder.Write([]byte(`{"type":"message","role":"assistant","content":"half ","delta":true}` + "\n" + `{"type":"message","role":"assistant","content":"a reply","delta":true}`))
	recorder.Flush()
	if events := log.Events(0); kinds(events) != "message" || events[0].Text != "half a reply" {
		t.Fatalf("unfinished message = %+v", events)
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

func TestRecorderTotalsUsageAndToolCalls(t *testing.T) {
	log, _ := Open("")
	recorder := log.Recorder(NewMapper(FormatCodexJSON))
	for _, line := range []string{
		`{"type":"item.started","item":{"id":"c","type":"command_execution","command":"ls"}}`,
		`{"type":"item.updated","item":{"id":"c","type":"command_execution","command":"ls"}}`,
		`{"type":"item.completed","item":{"id":"c","type":"command_execution","command":"ls","exit_code":0}}`,
		`{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":4,"output_tokens":2}}`,
		`{"type":"turn.completed","usage":{"input_tokens":20,"cached_input_tokens":5,"output_tokens":3}}`,
	} {
		_, _ = recorder.Write([]byte(line + "\n"))
	}
	usage, tools := recorder.Usage()
	if want := (Usage{InputTokens: 21, OutputTokens: 5, CacheReadTokens: 9}); usage != want {
		t.Fatalf("usage = %+v, want %+v", usage, want)
	}
	if tools != 1 {
		t.Fatalf("tool calls = %d, want the command counted once", tools)
	}
}

func TestCodexWebSearchAndMCPResults(t *testing.T) {
	mapper := NewMapper(FormatCodexJSON)
	started := mapper.Map([]byte(`{"type":"item.started","item":{"id":"w","type":"web_search","query":"go"}}`))
	done := mapper.Map([]byte(`{"type":"item.completed","item":{"id":"w","type":"web_search","query":"go","status":"completed"}}`))
	if got := kinds(append(started, done...)); got != "tool_call,tool_result" {
		t.Fatalf("web search kinds = %s", got)
	}
	result := mapper.Map([]byte(`{"type":"item.completed","item":{"id":"m","type":"mcp_tool_call","server":"gh","tool":"view","result":{"content":[{"type":"text","text":"issue body"}]}}}`))
	if len(result) != 1 || result[0].Output != "issue body" {
		t.Fatalf("mcp result = %+v", result)
	}
}
