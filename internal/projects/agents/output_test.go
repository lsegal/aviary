package agents

import (
	"reflect"
	"strings"
	"testing"
)

// definitionWithOutput is a valid definition apart from the output block under
// test, so a validation failure can only come from that block.
func definitionWithOutput(output Output) Definition {
	return Definition{
		Name: "muse", Binary: "muse",
		Session: Session{Assign: AssignNone},
		Output:  output,
		Args: Args{
			Run:    []Fragment{{Args: []string{"{prompt}"}}},
			Resume: []Fragment{{Args: []string{"{prompt}"}}},
		},
	}
}

func TestOutputFormatAliasesResolveToTheSameDecoder(t *testing.T) {
	for _, test := range []struct{ format, want string }{
		{"text", FormatText},
		{"plain", FormatText},
		{"stream-json", FormatStreamJSON},
		{"claude-stream-json", FormatStreamJSON},
	} {
		output := Output{Format: test.format}
		if err := definitionWithOutput(output).Validate(); err != nil {
			t.Fatalf("format %q was rejected: %v", test.format, err)
		}
		if got := output.Decoder(); got != test.want {
			t.Fatalf("format %q decodes as %q, want %q", test.format, got, test.want)
		}
	}
}

func TestOutputValidationRejectsWhatCannotBeDecoded(t *testing.T) {
	paths := JSONL{Text: "delta.text"}
	for _, test := range []struct {
		name   string
		output Output
		want   string
	}{
		{"unknown format", Output{Format: "ndjson"}, `"output.format"`},
		{"jsonl without paths", Output{Format: FormatJSONL}, `"output.jsonl" is required`},
		{"paths without jsonl", Output{Format: FormatText, JSONL: &paths}, `only meaningful`},
		{"nothing to read", Output{Format: FormatJSONL, JSONL: &JSONL{Type: "event"}}, `at least one of "text" or "toolName"`},
		{"input without a name", Output{Format: FormatJSONL, JSONL: &JSONL{Text: "text", ToolInput: "input"}}, `"output.jsonl.toolInput"`},
		{"ignore without a type", Output{Format: FormatJSONL, JSONL: &JSONL{Text: "text", Ignore: []string{"usage"}}}, `"output.jsonl.ignore" needs`},
		{"empty ignore", Output{Format: FormatJSONL, JSONL: &JSONL{Type: "event", Text: "text", Ignore: []string{" "}}}, `cannot contain an empty value`},
		{"deltas without text", Output{Format: FormatJSONL, JSONL: &JSONL{ToolName: "tool.name", TextDelta: true}}, `"output.jsonl.textDelta" needs`},
		{"malformed path", Output{Format: FormatJSONL, JSONL: &JSONL{Text: "delta..text"}}, `"output.jsonl.text"`},
		{"path with an index", Output{Format: FormatJSONL, JSONL: &JSONL{Text: "content[0].text"}}, `"output.jsonl.text"`},
		{"prefix without a name", Output{Format: FormatJSONL, JSONL: &JSONL{Text: "text", ToolNamePrefix: "tool."}}, `"output.jsonl.toolNamePrefix" needs`},
		{"events without a type", Output{Format: FormatJSONL, JSONL: &JSONL{Text: "text", Events: map[string]JSONLEvent{"call": {ToolName: "name"}}}}, `"output.jsonl.events" needs`},
		{"empty event type", Output{Format: FormatJSONL, JSONL: &JSONL{Type: "kind", Text: "text", Events: map[string]JSONLEvent{" ": {ToolName: "name"}}}}, `cannot contain an empty event type`},
		{"override of an ignored type", Output{Format: FormatJSONL, JSONL: &JSONL{Type: "kind", Text: "text", Ignore: []string{"usage"}, Events: map[string]JSONLEvent{"usage": {ToolName: "name"}}}}, `also in "output.jsonl.ignore"`},
		{"override reading nothing", Output{Format: FormatJSONL, JSONL: &JSONL{Type: "kind", Text: "text", Events: map[string]JSONLEvent{"call": {ToolInput: "input"}}}}, `"output.jsonl.events.call.toolInput" needs`},
		{"override with no paths", Output{Format: FormatJSONL, JSONL: &JSONL{Type: "kind", Text: "text", Events: map[string]JSONLEvent{"call": {}}}}, `"output.jsonl.events.call" needs at least one`},
		{"override prefix without a name", Output{Format: FormatJSONL, JSONL: &JSONL{Type: "kind", Text: "text", Events: map[string]JSONLEvent{"call": {Text: "t", ToolNamePrefix: "tool."}}}}, `"output.jsonl.events.call.toolNamePrefix" needs`},
		{"malformed override path", Output{Format: FormatJSONL, JSONL: &JSONL{Type: "kind", Text: "text", Events: map[string]JSONLEvent{"call": {ToolName: "call..name"}}}}, `"output.jsonl.events.call.toolName"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := definitionWithOutput(test.output).Validate()
			if err == nil {
				t.Fatalf("output %#v was accepted", test.output)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want it to name %s", err, test.want)
			}
		})
	}
}

// TestMuseDeclaresItsTextEventsAreDeltas pins why muse is the one built-in
// that sets textDelta: its only incremental text event streams token-sized
// fragments, so without it a sentence is decoded as a line per token. The
// other JSONL agents carry a whole message per text event and must not set it.
func TestMuseDeclaresItsTextEventsAreDeltas(t *testing.T) {
	registry := MustBuiltin()
	muse, ok := registry.Lookup("muse")
	if !ok {
		t.Fatal("no built-in definition for muse")
	}
	if muse.Output.JSONL == nil || !muse.Output.JSONL.TextDelta {
		t.Fatal("muse decodes run.output.delta as whole lines, which breaks a sentence across a line per token")
	}
	for _, other := range []string{"cline", "opencode"} {
		definition, ok := registry.Lookup(other)
		if !ok {
			t.Fatalf("no built-in definition for %q", other)
		}
		if definition.Output.JSONL.TextDelta {
			t.Fatalf("%s declares text deltas, but its text events carry whole messages", other)
		}
	}
}

// TestJSONLEventsCanCarryTheOnlyPathsRead proves a definition may leave the
// shared paths empty and describe each of its event types on its own, which is
// the point of the override block: an envelope whose types share no field
// layout has nothing to put in a shared path.
func TestJSONLEventsCanCarryTheOnlyPathsRead(t *testing.T) {
	output := Output{Format: FormatJSONL, JSONL: &JSONL{
		Type: "kind",
		Events: map[string]JSONLEvent{
			"chunk": {Text: "delta.text"},
			"call":  {ToolName: "tool.name", ToolInput: "tool.arguments", ToolNamePrefix: "fn."},
		},
	}}
	if err := output.validate(); err != nil {
		t.Fatalf("a per-event-type JSONL configuration was rejected: %v", err)
	}
}

// TestMuseReadsToolNamesPerEventType pins the definition that drove the
// override block: only task.lifecycle.proposed carries a Muse tool name, so
// reading it from a shared path would name a model turn on every other event.
func TestMuseReadsToolNamesPerEventType(t *testing.T) {
	registry := MustBuiltin()
	muse, ok := registry.Lookup("muse")
	if !ok || muse.Output.JSONL == nil {
		t.Fatal("the built-in muse definition has no JSONL block")
	}
	if muse.Output.JSONL.ToolName != "" {
		t.Fatal("muse names a shared tool-call path, but its tool names live on one event type")
	}
	if _, ok := muse.Output.JSONL.Events["task.lifecycle.proposed"]; !ok {
		t.Fatal("muse names no per-event-type override for task.lifecycle.proposed")
	}
	for _, name := range []string{"cline", "opencode"} {
		definition, ok := registry.Lookup(name)
		if !ok || definition.Output.JSONL == nil {
			t.Fatalf("the built-in %s definition has no JSONL block", name)
		}
		if len(definition.Output.JSONL.Events) > 0 {
			t.Errorf("%s names per-event-type overrides, but one set of paths describes its whole stream", name)
		}
	}
}

func TestJSONLPathsAcceptArrayStepsAndNesting(t *testing.T) {
	output := Output{Format: FormatJSONL, JSONL: &JSONL{
		Type: "type", Text: "message.content[].text",
		ToolName: "message.content[].name", ToolInput: "message.content[].input",
		Ignore: []string{"system"},
	}}
	if err := definitionWithOutput(output).Validate(); err != nil {
		t.Fatalf("a Claude-shaped JSONL configuration was rejected: %v", err)
	}
	if got := output.Decoder(); got != FormatJSONL {
		t.Fatalf("decoder = %q, want %q", got, FormatJSONL)
	}
}

// A definition that names no missing-session phrases is detected by the shared
// list; one that names its own is detected by those as well, so a distinctive
// message is added for that agent without loosening detection for the others.
func TestMissingSessionPatternsDefaultToTheSharedList(t *testing.T) {
	definition := definitionWithOutput(Output{Format: FormatText})
	if got := definition.MissingSessionPatterns(); !reflect.DeepEqual(got, DefaultMissingSessionPatterns) {
		t.Fatalf("patterns = %#v, want the shared defaults %#v", got, DefaultMissingSessionPatterns)
	}
	definition.MissingSession = []string{"Thread has expired"}
	want := append(append([]string(nil), DefaultMissingSessionPatterns...), "Thread has expired")
	if got := definition.MissingSessionPatterns(); !reflect.DeepEqual(got, want) {
		t.Fatalf("patterns = %#v, want the shared defaults plus the definition's own %#v", got, want)
	}
	definition.MissingSession = []string{" "}
	if err := definition.Validate(); err == nil {
		t.Fatal("an empty missing-session phrase was accepted, which matches everything")
	}
}

// The built-ins keep decoding exactly as they did before decoding became
// declarable, since their rendered output is what the existing tests pin.
func TestBuiltinOutputDecoders(t *testing.T) {
	registry := MustBuiltin()
	for name, want := range map[string]string{
		"claude": FormatStreamJSON, "codex": FormatText,
		"cline": FormatJSONL, "opencode": FormatJSONL, "muse": FormatJSONL,
	} {
		definition, ok := registry.Lookup(name)
		if !ok {
			t.Fatalf("no built-in definition for %q", name)
		}
		if got := definition.Output.Decoder(); got != want {
			t.Fatalf("%s decodes as %q, want %q", name, got, want)
		}
		if got := definition.MissingSessionPatterns(); !reflect.DeepEqual(got, DefaultMissingSessionPatterns) {
			t.Fatalf("%s missing-session patterns = %#v, want the shared defaults", name, got)
		}
	}
}

// Gemini prints two wordings no other agent does, which is exactly what a
// per-agent list is for: they are detected for gemini and for nothing else.
func TestGeminiNamesItsOwnMissingSessionPhrases(t *testing.T) {
	registry := MustBuiltin()
	gemini, ok := registry.Lookup("gemini")
	if !ok {
		t.Skip("no gemini definition in this build")
	}
	patterns := strings.Join(gemini.MissingSessionPatterns(), "|")
	for _, want := range append(append([]string(nil), DefaultMissingSessionPatterns...), "no previous sessions found", "invalid session identifier") {
		if !strings.Contains(patterns, want) {
			t.Fatalf("gemini patterns = %v, want them to include %q", gemini.MissingSessionPatterns(), want)
		}
	}
	for _, name := range registry.Names() {
		definition, _ := registry.Lookup(name)
		if name == "gemini" {
			continue
		}
		if strings.Contains(strings.Join(definition.MissingSessionPatterns(), "|"), "no previous sessions found") {
			t.Fatalf("%s is detected by gemini's own wording", name)
		}
	}
}
