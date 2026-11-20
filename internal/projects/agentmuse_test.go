package projects

import (
	"testing"

	"github.com/lsegal/aviary/internal/clidefs"
)

// museStream is a captured `muse exec --json` stream, the sample recorded on
// #545 with the lifecycle events that surround it. Muse writes its whole
// message twice: once token by token on run.output.delta, and once complete on
// run.terminal.completed after the run is over. Decoding the deltas is what
// makes progress visible while the agent is still working, so the terminal
// events are ignored rather than printing the message a second time, and
// tool.result is ignored too because its payload.text is the tool's entire
// output rather than a summary of the call.
const museStream = `{"payload_type":"run.lifecycle.started","payload":{"session_id":"3f2504e0-4f89-11d3-9a0c-0305e82c3301"}}\n` +
	`{"payload_type":"task.lifecycle.proposed","payload":{"task":{"kind":"model.meta.response"}}}\n` +
	`{"payload_type":"task.lifecycle.proposed","payload":{"task":{"kind":"tool.read_file"}}}\n` +
	`{"payload_type":"tool.result","payload":{"text":"1: hi"}}\n` +
	`{"payload_type":"run.output.delta","payload":{"text":"a.txt "}}\n` +
	`{"payload_type":"run.output.delta","payload":{"text":"contains "}}\n` +
	`{"payload_type":"run.output.delta","payload":{"text":"hi"}}\n` +
	`{"payload_type":"run.terminal.completed","payload":{"text":"a.txt contains hi"}}`

// museDecoded is museStream read through the definition: the tool call the run
// proposed, then one sentence rather than the three lines an event-per-line
// decoder would write, and no second copy of it from the terminal event. The
// model turn proposed on the same event type carries no "tool." prefix, so it
// contributes no progress line of its own.
const museDecoded = "Running: read_file\na.txt contains hi"

// TestMuseDefinitionContract proves the shipped Meta Muse Code definition
// against the fake CLI. Muse takes a caller-assigned session ID like Claude
// does, so a resume is the same `muse exec` invocation carrying the ID aviary
// already holds, and its JSONL stream is decoded by the field paths its own
// definition names. A job that names no model runs the definition's default.
func TestMuseDefinitionContract(t *testing.T) {
	agentContract{
		Definition: builtinDefinition(t, "muse"),
		Repo:       "o/r",
		Number:     7,
		SessionID:  "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
		Stdout:     museStream,
		WantRun: []string{
			"exec", "--session-id", "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
			"--approval-mode", "never", "--model", "muse-spark-1.3", "--user-input-auto-resolve", "--json", freshPrompt("o/r", 7),
		},
		WantResume: []string{
			"exec", "--session-id", "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
			"--approval-mode", "never", "--model", "muse-spark-1.3", "--user-input-auto-resolve", "--json", resumePrompt(),
		},
		WantOutput: museDecoded,
	}.check(t)
}

// TestMuseYoloDefinitionContract proves the yolo arm swaps the approval mode
// for Muse's own bypass rather than passing both.
func TestMuseYoloDefinitionContract(t *testing.T) {
	agentContract{
		Definition: builtinDefinition(t, "muse"),
		Repo:       "o/r",
		Number:     7,
		SessionID:  "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
		Yolo:       true,
		Stdout:     museStream,
		WantRun: []string{
			"exec", "--session-id", "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
			"--yolo", "--model", "muse-spark-1.3", "--user-input-auto-resolve", "--json", freshPrompt("o/r", 7),
		},
		WantResume: []string{
			"exec", "--session-id", "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
			"--yolo", "--model", "muse-spark-1.3", "--user-input-auto-resolve", "--json", resumePrompt(),
		},
		WantOutput: museDecoded,
	}.check(t)
}

// TestMuseProgressCarriesToolCalls pins the one thing Muse's stream needed the
// decoder to learn. No single Muse event pairs a tool name with the input it
// was called with: the name appears alone on task.lifecycle.proposed as a task
// kind shared with model turns ("tool.read_file" beside "model.meta.response"),
// and on tool.result beside payload.text, which is the tool's entire output
// rather than a summary of the call. The definition therefore reads the name
// out of task.lifecycle.proposed through a per-event-type override, and tells
// the two kinds apart by the "tool." prefix that only a call carries. The name
// renders without a detail because no event carries the call's arguments.
// Nothing here is special-cased for Muse in Go.
func TestMuseProgressCarriesToolCalls(t *testing.T) {
	jsonl := builtinDefinition(t, "muse").Output.JSONL
	if jsonl.ToolName != "" {
		t.Fatal("muse names a shared tool-call path, but only task.lifecycle.proposed carries a tool name")
	}
	proposed, ok := jsonl.Events["task.lifecycle.proposed"]
	if !ok {
		t.Fatal("muse names no per-event-type override for task.lifecycle.proposed, which is where its tool names are")
	}
	if proposed.ToolName == "" {
		t.Fatal("the task.lifecycle.proposed override reads no tool name")
	}
	if proposed.ToolNamePrefix == "" {
		t.Fatal("the task.lifecycle.proposed override matches no prefix, so a model turn renders as a tool call")
	}
}

// TestMuseAsksForItsJSONStreamOnlyWhereItIsDecoded keeps the modes whose output
// is read as an event stream asking for one, and the vision read off it.
func TestMuseAsksForItsJSONStreamOnlyWhereItIsDecoded(t *testing.T) {
	definition := builtinDefinition(t, "muse")
	// The vision call reads a board screenshot and its answer is parsed as
	// prose, so that mode alone stays on Muse's plain output.
	for _, arg := range definition.Render(clidefs.ModeVision, clidefs.Values{Prompt: "do it", Image: "/tmp/shot.png"}) {
		if arg == "--json" {
			t.Fatal("the vision argv asks for --json, but its answer is read as prose")
		}
	}
	for _, mode := range []clidefs.Mode{clidefs.ModeRun, clidefs.ModeResume} {
		asked := false
		for _, arg := range definition.Render(mode, clidefs.Values{Prompt: "do it"}) {
			asked = asked || arg == "--json"
		}
		if !asked {
			t.Fatalf("%s argv does not ask for --json, but its output is decoded as a JSON stream", mode)
		}
	}
}
