package clidefs

import (
	"testing"

	"github.com/lsegal/aviary/internal/transcript"
)

func TestJSONLResultEventDecidesTheTurnOutcome(t *testing.T) {
	output := Output{Format: FormatJSONL, JSONL: &JSONL{
		Type: "event.type", Text: "event.text",
		Result: &JSONLResult{Event: "done", Status: "event.reason", Success: []string{"completed"}},
	}}
	if err := definitionWithOutput(output).Validate(); err != nil {
		t.Fatalf("result block was rejected: %v", err)
	}
	for _, test := range []struct {
		line string
		want transcript.TurnOutcome
	}{
		{`{"event":{"type":"done","reason":"completed"}}`, transcript.TurnSucceeded},
		{`{"event":{"type":"done"}}`, transcript.TurnSucceeded},
		{`{"event":{"type":"done","reason":"aborted"}}`, transcript.TurnFailed},
		{`{"event":{"type":"content_end","text":"done"}}`, transcript.TurnUnfinished},
		{`{"type":"result","subtype":"success"}`, transcript.TurnUnfinished},
		{`not json`, transcript.TurnUnfinished},
	} {
		if got := output.TurnResult([]byte(test.line)); got != test.want {
			t.Fatalf("TurnResult(%s) = %v, want %v", test.line, got, test.want)
		}
	}
}

func TestOutputWithoutResultBlockUsesItsTranscriptFormat(t *testing.T) {
	output := Output{Format: FormatStreamJSON}
	if got := output.TurnResult([]byte(`{"type":"result","subtype":"success"}`)); got != transcript.TurnSucceeded {
		t.Fatalf("TurnResult = %v, want Claude's result to succeed", got)
	}
	jsonl := Output{Format: FormatJSONL, JSONL: &JSONL{Type: "type", Text: "text"}}
	if got := jsonl.TurnResult([]byte(`{"type":"result"}`)); got != transcript.TurnUnfinished {
		t.Fatalf("TurnResult = %v, want a JSONL stream without a result block to never finish", got)
	}
}

// TestBuiltinJSONLDefinitionsDeclareTheirFinalResult checks each bundled
// JSONL agent against the final result event its CLI really emits (#1141).
func TestBuiltinJSONLDefinitionsDeclareTheirFinalResult(t *testing.T) {
	for _, test := range []struct {
		name, success, failure, unfinished string
	}{
		{
			"cline",
			`{"type":"agent_event","event":{"type":"done","reason":"completed","text":"Completed"}}`,
			`{"type":"agent_event","event":{"type":"done","reason":"max_iterations"}}`,
			`{"type":"agent_event","event":{"type":"iteration_end","iteration":5}}`,
		},
		{
			"muse",
			`{"record_type":"event","payload_type":"run.terminal.completed","payload":{"terminal":"completed"}}`,
			`{"record_type":"event","payload_type":"run.terminal.failed","payload":{"terminal":"failed"}}`,
			`{"record_type":"event","payload_type":"assistant.message.delta","payload":{"text":"hi"}}`,
		},
		{
			"opencode",
			`{"type":"step_finish","part":{"reason":"stop"}}`,
			`{"type":"step_finish","part":{"reason":"tool-calls"}}`,
			`{"type":"text","part":{"text":"done"}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition, ok := MustBuiltin().Lookup(test.name)
			if !ok {
				t.Fatalf("no built-in definition for %q", test.name)
			}
			output := definition.Output
			if got := output.TurnResult([]byte(test.success)); got != transcript.TurnSucceeded {
				t.Fatalf("final result %s = %v, want success", test.success, got)
			}
			if got := output.TurnResult([]byte(test.failure)); got == transcript.TurnSucceeded {
				t.Fatalf("%s counted as a successful final result", test.failure)
			}
			if got := output.TurnResult([]byte(test.unfinished)); got != transcript.TurnUnfinished {
				t.Fatalf("%s = %v, want it not to end the turn", test.unfinished, got)
			}
		})
	}
}
