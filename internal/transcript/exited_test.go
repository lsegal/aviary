package transcript

import (
	"strings"
	"testing"
)

// A tool call or command the agent's process died in the middle of is closed
// as interrupted once the process exits, followed by an "agent process
// exited" line, rather than shown running forever (issue #1426).
func TestRecorderExitedClosesOpenCalls(t *testing.T) {
	log, _ := Open("")
	recorder := log.Recorder(NewMapper(FormatClaudeStreamJSON))
	_, _ = recorder.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"done","name":"Bash","input":{"command":"gh pr merge 7"}}]}}` + "\n"))
	_, _ = recorder.Write([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"done","content":"merged"}]}}` + "\n"))
	_, _ = recorder.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"rm","name":"Bash","input":{"command":"rm -rf /tmp/clone"}},{"type":"tool_use","id":"read","name":"Read","input":{"file_path":"/tmp/a"}}]}}` + "\n"))
	recorder.Flush()
	recorder.Exited(true, "it did not exit cleanly (WaitDelay expired)")

	events := log.Events(0)
	tail := events[len(events)-3:]
	if command := tail[0]; command.Kind != KindCommand || command.CallID != "rm" || command.Status != InterruptedStatus || !command.IsError || command.Command != "rm -rf /tmp/clone" {
		t.Fatalf("open command closed as %+v", command)
	}
	if result := tail[1]; result.Kind != KindToolResult || result.CallID != "read" || !result.IsError || !strings.Contains(result.Output, "interrupted") {
		t.Fatalf("open tool call closed as %+v", result)
	}
	if exited := tail[2]; exited.Kind != KindSystem || exited.Text != "agent process exited: it did not exit cleanly (WaitDelay expired)" {
		t.Fatalf("exit line = %+v", exited)
	}
	for _, event := range events[:len(events)-3] {
		if event.CallID == "done" && event.Status == InterruptedStatus {
			t.Fatalf("finished command was interrupted: %+v", event)
		}
	}
	// Nothing is left open to close twice.
	recorder.Exited(true, "")
	if again := log.Events(0); len(again) != len(events)+1 || again[len(again)-1].Text != "agent process exited" {
		t.Fatalf("second exit added %+v", again[len(events):])
	}
}

// A run that finished every call and exited cleanly records nothing more.
func TestRecorderExitedCleanRunAddsNothing(t *testing.T) {
	log, _ := Open("")
	recorder := log.Recorder(NewMapper(FormatClaudeStreamJSON))
	_, _ = recorder.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"r","name":"Read","input":{"file_path":"/tmp/a"}}]}}` + "\n"))
	_, _ = recorder.Write([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"r","content":"ok"}]}}` + "\n"))
	before := len(log.Events(0))
	recorder.Exited(false, "")
	if after := len(log.Events(0)); after != before {
		t.Fatalf("clean exit added %d events", after-before)
	}
}
