package cmd

import (
	"strings"
	"testing"
)

// Text fragments join into one message per stretch between tool events, the
// done marker ends the run without being printed, and the tool's own error
// result is printed only when no error event carried it.
func TestAgentEventWriterJoinsTextIntoMessages(t *testing.T) {
	var out strings.Builder
	w := &agentEventWriter{out: &out}
	for _, message := range []string{
		`{"type":"text","text":"Reading "}`,
		`{"type":"text","text":"the file.\n"}`,
		`{"type":"tool_call","id":"call_1","name":"file_read","input":{"path":"a"}}`,
		`{"type":"tool_result","id":"call_1","name":"file_read","output":"hi"}`,
		`{"type":"text","text":"It says hi."}`,
		`not json`,
	} {
		if w.progress(message) {
			t.Fatalf("%s reported as the last message", message)
		}
	}
	if !w.progress(`{"type":"done"}`) {
		t.Fatal("done was not reported as the last message")
	}
	w.finish("Reading the file.\nIt says hi.", false)
	want := `{"type":"message","text":"Reading the file."}
{"type":"tool_call","id":"call_1","name":"file_read","input":{"path":"a"}}
{"type":"tool_result","id":"call_1","name":"file_read","output":"hi"}
{"type":"message","text":"It says hi."}
`
	if out.String() != want {
		t.Fatalf("output =\n%s\nwant\n%s", out.String(), want)
	}
}

// A server that streams no events still yields the reply, or the error, from
// the tool's result.
func TestAgentEventWriterFallsBackToTheResult(t *testing.T) {
	var out strings.Builder
	(&agentEventWriter{out: &out}).finish("hello", false)
	(&agentEventWriter{out: &out}).finish("agent \"x\" not found", true)
	errored := &agentEventWriter{out: &out}
	errored.progress(`{"type":"error","text":"boom"}`)
	errored.finish("boom", true)
	want := `{"type":"message","text":"hello"}
{"type":"error","text":"agent \"x\" not found"}
{"type":"error","text":"boom"}
`
	if out.String() != want {
		t.Fatalf("output =\n%s\nwant\n%s", out.String(), want)
	}
}
