package projects

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/projects/transcript"
)

// museJSONL describes an event stream shaped unlike Claude's, so the generic
// decoder is proved on paths it does not share with the decoder it replaces.
var museJSONL = agents.JSONL{
	Type: "event", Text: "delta.text",
	ToolName: "delta.tool.name", ToolInput: "delta.tool.arguments",
	Ignore: []string{"heartbeat", "usage"},
}

func TestJSONLOutputWriterDecodesConfiguredStream(t *testing.T) {
	var output bytes.Buffer
	w := newJSONLOutputWriter(&output, museJSONL)
	lines := []string{
		`{"event":"message","delta":{"text":"reading the issue"}}`,
		`{"event":"heartbeat","delta":{"text":"ignored entirely"}}`,
		`{"event":"usage","delta":{"text":"also ignored"}}`,
		`{"event":"message","delta":{"tool":{"name":"Read","arguments":{"file_path":"main.go"}}}}`,
		`{"event":"message","delta":{"tool":{"name":"Bash","arguments":{"command":"go test ./..."}}}}`,
		`{"event":"telemetry","payload":{"tokens":12}}`,
		`{"event":"message","delta":{"text":"done"}}`,
	}
	if _, err := io.WriteString(w, strings.Join(lines, "\n")+"\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	want := "reading the issue\nRunning: Read main.go\nRunning: go test ./...\ndone\n"
	if got := output.String(); got != want {
		t.Fatalf("decoded stream = %q, want %q", got, want)
	}
}

// A malformed line is the agent talking in some other voice -- a banner, a
// warning, a stack trace -- and has to survive rather than be swallowed, with
// the events on either side of it decoded as usual.
func TestJSONLOutputWriterPassesMalformedLinesThrough(t *testing.T) {
	var output bytes.Buffer
	w := newJSONLOutputWriter(&output, museJSONL)
	lines := []string{
		`{"event":"message","delta":{"text":"before"}}`,
		`warning: config file not found`,
		`{"event":"message",`,
		`[]`,
		`{"event":"message","delta":{"text":"after"}}`,
	}
	for _, line := range lines {
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	want := "before\nwarning: config file not found\n{\"event\":\"message\",\nafter\n"
	if got := output.String(); got != want {
		t.Fatalf("decoded stream = %q, want %q", got, want)
	}
}

// The decoder is written to in whatever chunks the pipe delivers, which do not
// respect event boundaries, and the last event may arrive without a newline.
func TestJSONLOutputWriterDecodesAcrossWritesAndFlushesTheTail(t *testing.T) {
	var output bytes.Buffer
	w := newJSONLOutputWriter(&output, museJSONL)
	for _, chunk := range []string{
		`{"event":"message","delta":{"te`, `xt":"split across writes"}}` + "\n" +
			`{"event":"message","delta":{"text":"no trailing newline"}}`,
	} {
		if _, err := io.WriteString(w, chunk); err != nil {
			t.Fatal(err)
		}
	}
	if got := output.String(); got != "split across writes\n" {
		t.Fatalf("output before the flush = %q, want only the completed event", got)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if want := "split across writes\nno trailing newline\n"; output.String() != want {
		t.Fatalf("output after the flush = %q, want %q", output.String(), want)
	}
}

// A tool call is rendered with its own arguments even when the blocks around
// it carry none, which is what pairing by shared path prefix buys.
func TestJSONLOutputWriterPairsToolCallsWithTheirOwnInput(t *testing.T) {
	var output bytes.Buffer
	w := newJSONLOutputWriter(&output, agents.JSONL{
		Type: "type", Text: "content[].text",
		ToolName: "content[].tool_name", ToolInput: "content[].tool_input",
	})
	line := `{"type":"turn","content":[` +
		`{"text":"looking"},` +
		`{"tool_name":"Grep","tool_input":{"pattern":"runOnce"}},` +
		`{"tool_name":"Write"},` +
		`{"tool_name":"Read","tool_input":{"file_path":"agents/output.go"}}]}`
	if _, err := io.WriteString(w, line+"\n"); err != nil {
		t.Fatal(err)
	}
	want := "looking\nRunning: Grep runOnce\nRunning: Write\nRunning: Read agents/output.go\n"
	if got := output.String(); got != want {
		t.Fatalf("decoded tool calls = %q, want %q", got, want)
	}
}

// An event the definition describes nothing in renders nothing at all, rather
// than a blank line or Go's rendering of a map.
func TestJSONLOutputWriterSkipsEventsItDescribesNothingIn(t *testing.T) {
	var output bytes.Buffer
	w := newJSONLOutputWriter(&output, museJSONL)
	for _, line := range []string{
		`{"event":"message"}`,
		`{"event":"message","delta":{"text":"   "}}`,
		`{"event":"message","delta":{"text":{"nested":"object"}}}`,
		`{"event":"message","delta":null}`,
		``,
	} {
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	if got := output.String(); got != "" {
		t.Fatalf("decoded output = %q, want nothing", got)
	}
}

func TestNewOutputDecoderSelectsByFormat(t *testing.T) {
	for _, test := range []struct {
		format string
		want   string
	}{
		{format: "text"},
		{format: "plain"},
		{format: "stream-json", want: "*projects.claudeJSONOutputWriter"},
		{format: "claude-stream-json", want: "*projects.claudeJSONOutputWriter"},
		{format: "jsonl", want: "*projects.jsonlOutputWriter"},
		{format: "codex-json", want: "*projects.transcriptOutputWriter"},
		{format: "aviary-json", want: "*projects.transcriptOutputWriter"},
		{format: "gemini-json", want: "*projects.transcriptOutputWriter"},
	} {
		t.Run(test.format, func(t *testing.T) {
			output := agents.Output{Format: test.format}
			if test.format == "jsonl" {
				output.JSONL = &museJSONL
			}
			decoder := newOutputDecoder(output, io.Discard)
			if test.want == "" {
				if decoder != nil {
					t.Fatalf("format %q selected %T, want the agent's output shown as written", test.format, decoder)
				}
				return
			}
			if got := fmt.Sprintf("%T", decoder); got != test.want {
				t.Fatalf("format %q selected %s, want %s", test.format, got, test.want)
			}
		})
	}
}

// #388: Gemini streams its reply as deltas, so the decoder joins them into one
// line, ends the message before a line that is not an event, renders tool
// calls and errors as progress lines, and writes the message a cut-off stream
// left on Flush.
func TestGeminiJSONOutputWriterRendersTheStream(t *testing.T) {
	var output bytes.Buffer
	w := newTranscriptOutputWriter(&output, transcript.FormatGeminiJSON)
	lines := []string{
		`{"type":"init","session_id":"s","model":"gemini-3.5-flash"}`,
		`{"type":"message","role":"user","content":"the prompt"}`,
		`{"type":"message","role":"assistant","content":"reading ","delta":true}`,
		`{"type":"message","role":"assistant","content":"the issue","delta":true}`,
		`Error executing tool replace: no match`,
		`{"type":"tool_use","tool_name":"replace","tool_id":"r1","parameters":{"file_path":"/w/a.go","old_string":"a","new_string":"b"}}`,
		`{"type":"tool_result","tool_id":"r1","status":"error","output":"no match"}`,
		`{"type":"error","severity":"error","message":"quota exhausted"}`,
		`{"type":"message","role":"assistant","content":"stopped ","delta":true}`,
		`{"type":"message","role":"assistant","content":"early","delta":true}`,
	}
	if _, err := io.WriteString(w, strings.Join(lines, "\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	want := "reading the issue\nError executing tool replace: no match\nEditing: /w/a.go\nAgent error: quota exhausted\nstopped early\n"
	if got := output.String(); got != want {
		t.Fatalf("decoded stream = %q, want %q", got, want)
	}
}

// A definition's own missing-session phrase is matched for its runs on top of
// the shared defaults, which every agent keeps.
func TestMissingSessionDetectorUsesTheDefinitionsOwnPatterns(t *testing.T) {
	ownPatterns := func(phrases ...string) []string {
		return agents.Definition{MissingSession: phrases}.MissingSessionPatterns()
	}
	for _, test := range []struct {
		name     string
		patterns []string
		output   string
		want     bool
	}{
		{name: "default list", output: "error: No conversation found", want: true},
		{name: "own phrase", patterns: ownPatterns("Thread has expired"), output: "thread has expired\n", want: true},
		{name: "shared defaults still apply", patterns: ownPatterns("Thread has expired"), output: "no conversation found", want: true},
		{name: "unrelated failure", patterns: ownPatterns("Thread has expired"), output: "compilation failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			detector := &missingSessionDetector{output: io.Discard, patterns: test.patterns}
			if _, err := io.WriteString(detector, test.output); err != nil {
				t.Fatal(err)
			}
			if got := detector.sessionMissing(); got != test.want {
				t.Fatalf("sessionMissing() = %v for %q with patterns %v, want %v", got, test.output, test.patterns, test.want)
			}
		})
	}
}

// deltaJSONL describes the same stream as museJSONL, except that its text
// events carry a fragment of a message rather than a whole one.
var deltaJSONL = agents.JSONL{
	Type: "event", Text: "delta.text", TextDelta: true,
	ToolName: "delta.tool.name", ToolInput: "delta.tool.arguments",
	Ignore: []string{"heartbeat", "usage"},
}

// TestJSONLOutputWriterJoinsTextDeltasIntoWholeLines proves the capability
// #545 added: a stream whose text arrives token by token is shown as the
// sentences it spells rather than one line per token. The message ends on the
// first event that carries no text -- a tool call, a bookkeeping event of a
// shape the definition describes nothing in, or the end of the stream.
func TestJSONLOutputWriterJoinsTextDeltasIntoWholeLines(t *testing.T) {
	var output bytes.Buffer
	w := newJSONLOutputWriter(&output, deltaJSONL)
	lines := []string{
		`{"event":"message","delta":{"text":"a.txt "}}`,
		`{"event":"message","delta":{"text":"contains "}}`,
		`{"event":"message","delta":{"text":"hi"}}`,
		`{"event":"telemetry","payload":{"tokens":12}}`,
		`{"event":"message","delta":{"text":"now "}}`,
		`{"event":"message","delta":{"text":"reading it"}}`,
		`{"event":"message","delta":{"tool":{"name":"Read","arguments":{"file_path":"a.txt"}}}}`,
		`{"event":"message","delta":{"text":"all "}}`,
		`{"event":"usage","delta":{"text":"dropped before anything is read"}}`,
		`{"event":"message","delta":{"text":"done"}}`,
	}
	if _, err := io.WriteString(w, strings.Join(lines, "\n")+"\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	// "all done" survives the ignored event between its two halves: an
	// ignored type is dropped before the decoder reads anything out of it, so
	// it never ends the message the way an unrecognised event does.
	want := "a.txt contains hi\nnow reading it\nRunning: Read a.txt\nall done\n"
	if got := output.String(); got != want {
		t.Fatalf("decoded stream = %q, want %q", got, want)
	}
}

// TestJSONLOutputWriterKeepsDeltasWholeAcrossWritesAndBanners proves the two
// edges of the buffer: a partial line held over from one write still joins the
// message, and a line that is not an event at all ends the message first
// rather than landing in the middle of it.
func TestJSONLOutputWriterKeepsDeltasWholeAcrossWritesAndBanners(t *testing.T) {
	var output bytes.Buffer
	w := newJSONLOutputWriter(&output, deltaJSONL)
	for _, chunk := range []string{
		`{"event":"message","delta":{"text":"split "}}` + "\n" + `{"event":"message","delta":{"te`,
		`xt":"across writes"}}` + "\n" + "warning: config file not found\n",
		`{"event":"message","delta":{"text":"no trailing newline"}}`,
	} {
		if _, err := io.WriteString(w, chunk); err != nil {
			t.Fatal(err)
		}
	}
	if got := output.String(); got != "split across writes\nwarning: config file not found\n" {
		t.Fatalf("output before the flush = %q, want the joined message and the banner", got)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	want := "split across writes\nwarning: config file not found\nno trailing newline\n"
	if got := output.String(); got != want {
		t.Fatalf("decoded stream = %q, want %q", got, want)
	}
}

// TestJSONLOutputWriterWithoutTextDeltaIsUnchanged pins the other half of the
// field: the same stream read by a definition that does not declare deltas
// decodes exactly as it did before #545, one line per event.
func TestJSONLOutputWriterWithoutTextDeltaIsUnchanged(t *testing.T) {
	var output bytes.Buffer
	w := newJSONLOutputWriter(&output, museJSONL)
	lines := []string{
		`{"event":"message","delta":{"text":"a.txt "}}`,
		`{"event":"message","delta":{"text":"contains "}}`,
		`{"event":"message","delta":{"text":"hi"}}`,
	}
	if _, err := io.WriteString(w, strings.Join(lines, "\n")+"\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "a.txt\ncontains\nhi\n" {
		t.Fatalf("decoded stream = %q, want a line per event", got)
	}
}

// perEventJSONL describes a stream that spreads one logical tool call over two
// typed events, which is what a single set of paths cannot say: `proposed`
// keeps the call's name in a `kind` field it shares with model turns, and
// `invoked` keeps the name and its arguments somewhere else entirely.
var perEventJSONL = agents.JSONL{
	Type: "event", Text: "delta.text",
	Events: map[string]agents.JSONLEvent{
		"proposed": {ToolName: "task.kind", ToolNamePrefix: "tool."},
		"invoked":  {ToolName: "call.name", ToolInput: "call.arguments"},
		"note":     {Text: "note.body"},
	},
}

// TestJSONLOutputWriterReadsPathsPerEventType proves the capability #547 added:
// each event type is read by the paths its own shape needs, the shared paths
// still cover every type that names no override, and a name that does not
// carry the override's prefix is another kind of turn rather than a call.
func TestJSONLOutputWriterReadsPathsPerEventType(t *testing.T) {
	var output bytes.Buffer
	w := newJSONLOutputWriter(&output, perEventJSONL)
	lines := []string{
		`{"event":"message","delta":{"text":"reading it"}}`,
		`{"event":"proposed","task":{"kind":"model.meta.response"}}`,
		`{"event":"proposed","task":{"kind":"tool.read_file"}}`,
		`{"event":"invoked","call":{"name":"Read","arguments":{"file_path":"a.txt"}}}`,
		`{"event":"note","note":{"body":"a note"},"delta":{"text":"not this one"}}`,
		`{"event":"message","delta":{"text":"done"}}`,
	}
	if _, err := io.WriteString(w, strings.Join(lines, "\n")+"\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	want := "reading it\nRunning: read_file\nRunning: Read a.txt\na note\ndone\n"
	if got := output.String(); got != want {
		t.Fatalf("decoded stream = %q, want %q", got, want)
	}
}

// TestJSONLOutputWriterWithoutEventOverridesIsUnchanged pins the other half of
// the field: a definition that names no overrides reads every event through
// the shared paths exactly as it did before #547.
func TestJSONLOutputWriterWithoutEventOverridesIsUnchanged(t *testing.T) {
	shared := perEventJSONL
	shared.Events = nil
	shared.ToolName, shared.ToolInput = "call.name", "call.arguments"
	var output bytes.Buffer
	w := newJSONLOutputWriter(&output, shared)
	lines := []string{
		`{"event":"proposed","task":{"kind":"tool.read_file"}}`,
		`{"event":"invoked","call":{"name":"Read","arguments":{"file_path":"a.txt"}}}`,
	}
	if _, err := io.WriteString(w, strings.Join(lines, "\n")+"\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	// The proposed event keeps its name where the shared path does not look,
	// so it renders nothing at all -- the gap the overrides close.
	want := "Running: Read a.txt\n"
	if got := output.String(); got != want {
		t.Fatalf("decoded stream = %q, want %q", got, want)
	}
}

// TestJSONLOutputWriterOverridesTextDeltaStreams proves an override applies to
// a delta stream too: the tool call read out of one event type ends the
// message the deltas were spelling instead of landing inside it.
func TestJSONLOutputWriterOverridesTextDeltaStreams(t *testing.T) {
	delta := perEventJSONL
	delta.TextDelta = true
	var output bytes.Buffer
	w := newJSONLOutputWriter(&output, delta)
	lines := []string{
		`{"event":"message","delta":{"text":"a.txt "}}`,
		`{"event":"message","delta":{"text":"contains "}}`,
		`{"event":"message","delta":{"text":"hi"}}`,
		`{"event":"proposed","task":{"kind":"tool.read_file"}}`,
	}
	if _, err := io.WriteString(w, strings.Join(lines, "\n")+"\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	want := "a.txt contains hi\nRunning: read_file\n"
	if got := output.String(); got != want {
		t.Fatalf("decoded stream = %q, want %q", got, want)
	}
}
