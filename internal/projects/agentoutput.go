package projects

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/projects/transcript"
)

// outputDecoder wraps the writer an agent's stdout is forwarded to, turning
// whatever the agent prints into the human-readable lines aviary shows. Which
// decoder a run gets is the agent definition's to say, so a CLI with its own
// event stream is read by describing it rather than by adding another arm to
// runOnce.
type outputDecoder interface {
	io.Writer
	// Flush renders whatever the agent left without a trailing newline.
	Flush() error
}

// newOutputDecoder builds the decoder an output block selects, or nil when the
// agent's output is shown exactly as it is written. The definition has already
// been validated, so a format that selects the generic decoder carries its
// configuration.
func newOutputDecoder(output agents.Output, w io.Writer) outputDecoder {
	switch output.Decoder() {
	case agents.FormatStreamJSON:
		return newClaudeJSONOutputWriter(w)
	case agents.FormatJSONL:
		return newJSONLOutputWriter(w, *output.JSONL)
	case agents.FormatCodexJSON:
		return newCodexJSONOutputWriter(w)
	}
	return nil
}

// lineWriter buffers writes into whole lines, since a streaming agent's output
// arrives in chunks that do not respect event boundaries.
type lineWriter struct {
	mu     sync.Mutex
	buffer []byte
	line   func(line []byte) error
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buffer = append(w.buffer, p...)
	for {
		newline := bytes.IndexByte(w.buffer, '\n')
		if newline < 0 {
			break
		}
		line := append([]byte(nil), w.buffer[:newline]...)
		w.buffer = w.buffer[newline+1:]
		if err := w.line(line); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (w *lineWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buffer) == 0 {
		return nil
	}
	line := append([]byte(nil), w.buffer...)
	w.buffer = nil
	return w.line(line)
}

// jsonlOutputWriter decodes a line-delimited JSON event stream using the field
// paths the agent's own definition names. Most agents with a --json or
// streaming mode differ only in where the text and the tool calls live in each
// event, which is configuration rather than code.
type jsonlOutputWriter struct {
	*lineWriter
	config  agents.JSONL
	output  io.Writer
	ignored map[string]bool
	// pending holds the text fragments of a message still being streamed,
	// used only when the definition declares its text events are deltas. It
	// is guarded by lineWriter.mu along with the line buffer itself.
	pending []string
}

func newJSONLOutputWriter(output io.Writer, config agents.JSONL) *jsonlOutputWriter {
	w := &jsonlOutputWriter{lineWriter: &lineWriter{}, config: config, output: output}
	if len(config.Ignore) > 0 {
		w.ignored = make(map[string]bool, len(config.Ignore))
		for _, event := range config.Ignore {
			w.ignored[event] = true
		}
	}
	w.lineWriter.line = w.writeLine
	return w
}

func (w *jsonlOutputWriter) writeLine(line []byte) error {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil
	}
	var event any
	if err := json.Unmarshal(line, &event); err != nil {
		// A line that is not an event at all is still the agent talking:
		// banners, warnings, and stack traces all arrive this way, and a
		// dropped line is exactly the output someone debugging a failed run
		// came looking for. A message being streamed as deltas ends first, so
		// the banner does not land in the middle of the sentence.
		if w.config.TextDelta {
			if err := w.flushPending(nil); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintln(w.output, string(line))
		return err
	}
	if w.skips(event) {
		return nil
	}
	paths := w.paths(event)
	if w.config.TextDelta {
		return w.writeDelta(event, paths)
	}
	texts := jsonlStrings(event, paths.Text)
	for _, call := range w.toolCalls(event, paths) {
		texts = append(texts, "Running: "+call)
	}
	if len(texts) == 0 {
		// An event of a shape the definition does not describe renders
		// nothing, and the ones that follow it are decoded as usual.
		return nil
	}
	_, err := fmt.Fprintln(w.output, strings.Join(texts, "\n"))
	return err
}

// writeDelta decodes one event of a stream whose text arrives as token-sized
// fragments. Buffering them and ending the line only when the text stops is
// what turns "a.txt ", "contains ", "hi" into the sentence it spells, rather
// than the three lines the event-per-line decoder would write.
func (w *jsonlOutputWriter) writeDelta(event any, paths agents.JSONLEvent) error {
	fragments := jsonlFragments(event, paths.Text)
	w.pending = append(w.pending, fragments...)
	calls := w.toolCalls(event, paths)
	if len(fragments) > 0 && len(calls) == 0 {
		// The message is still being written. It ends on the next event that
		// adds nothing to it, or at the end of the stream.
		return nil
	}
	return w.flushPending(calls)
}

// flushPending writes the buffered message followed by the progress lines of
// the event that ended it. Either may be empty: an event of a shape the
// definition does not describe ends a message without adding a line of its own.
func (w *jsonlOutputWriter) flushPending(calls []string) error {
	var texts []string
	if joined := strings.TrimSpace(strings.Join(w.pending, "")); joined != "" {
		texts = append(texts, joined)
	}
	w.pending = nil
	for _, call := range calls {
		texts = append(texts, "Running: "+call)
	}
	if len(texts) == 0 {
		return nil
	}
	_, err := fmt.Fprintln(w.output, strings.Join(texts, "\n"))
	return err
}

// Flush ends the partial line the agent left, then writes the message the
// stream stopped in the middle of, which for a delta stream is the last thing
// the agent said.
func (w *jsonlOutputWriter) Flush() error {
	if err := w.lineWriter.Flush(); err != nil {
		return err
	}
	w.lineWriter.mu.Lock()
	defer w.lineWriter.mu.Unlock()
	return w.flushPending(nil)
}

// skips reports whether the event's type is one the definition drops.
func (w *jsonlOutputWriter) skips(event any) bool {
	if len(w.ignored) == 0 {
		return false
	}
	for _, kind := range jsonlStrings(event, w.config.Type) {
		if w.ignored[kind] {
			return true
		}
	}
	return false
}

// toolCalls renders the event's tool calls the way Claude's decoder renders
// its own, so a progress line reads the same whichever agent produced it.
func (w *jsonlOutputWriter) toolCalls(event any, paths agents.JSONLEvent) []string {
	if paths.ToolName == "" {
		return nil
	}
	// Name and input are read from the same block when their paths share a
	// prefix, so a call is never rendered with another call's arguments.
	prefix, name, input := splitJSONLPaths(paths.ToolName, paths.ToolInput)
	var calls []string
	for _, block := range jsonlLookup(event, prefix) {
		tool := firstJSONLString(jsonlLookup(block, name))
		if tool == "" {
			continue
		}
		if paths.ToolNamePrefix != "" {
			// The name field doubles as a kind covering more than tool
			// calls, so what does not carry the prefix is another kind of
			// turn rather than a call rendered under its internal name.
			trimmed, ok := strings.CutPrefix(tool, paths.ToolNamePrefix)
			if !ok || strings.TrimSpace(trimmed) == "" {
				continue
			}
			tool = trimmed
		}
		var arguments json.RawMessage
		if values := jsonlLookup(block, input); paths.ToolInput != "" && len(values) > 0 {
			if encoded, err := json.Marshal(values[0]); err == nil {
				arguments = encoded
			}
		}
		calls = append(calls, claudeToolUseSummary(tool, arguments))
	}
	return calls
}

// paths resolves the field paths that apply to one event: the definition's
// shared ones, with the override for the event's own type laid over them. It
// is what lets a stream that spreads a logical event over several typed events
// describe each of them, rather than pointing one path at all of them.
func (w *jsonlOutputWriter) paths(event any) agents.JSONLEvent {
	paths := agents.JSONLEvent{
		Text: w.config.Text, ToolName: w.config.ToolName,
		ToolInput: w.config.ToolInput, ToolNamePrefix: w.config.ToolNamePrefix,
	}
	if len(w.config.Events) == 0 {
		return paths
	}
	for _, kind := range jsonlStrings(event, w.config.Type) {
		override, ok := w.config.Events[kind]
		if !ok {
			continue
		}
		if override.Text != "" {
			paths.Text = override.Text
		}
		if override.ToolName != "" {
			// A call's name, input, and prefix replace the shared ones
			// together: half an override would pair this type's name with
			// another type's arguments.
			paths.ToolName, paths.ToolInput, paths.ToolNamePrefix = override.ToolName, override.ToolInput, override.ToolNamePrefix
		}
		return paths
	}
	return paths
}

// splitJSONLPaths returns the longest path both fields share along with what
// remains of each, so the shared part is walked once and its elements paired.
func splitJSONLPaths(name, input string) (prefix, nameRest, inputRest string) {
	if input == "" {
		return "", name, ""
	}
	nameSegments, inputSegments := strings.Split(name, "."), strings.Split(input, ".")
	shared := 0
	for shared < len(nameSegments)-1 && shared < len(inputSegments)-1 && nameSegments[shared] == inputSegments[shared] {
		shared++
	}
	return strings.Join(nameSegments[:shared], "."), strings.Join(nameSegments[shared:], "."), strings.Join(inputSegments[shared:], ".")
}

// jsonlLookup walks a field path through a decoded event, returning every
// value it names. A key suffixed with [] steps into an array and continues
// into each of its elements, so one path can name a value per content block.
// An empty path names the value it was given.
func jsonlLookup(value any, path string) []any {
	current := []any{value}
	if path == "" {
		return current
	}
	for _, segment := range strings.Split(path, ".") {
		key, array := strings.CutSuffix(segment, "[]")
		var next []any
		for _, item := range current {
			object, ok := item.(map[string]any)
			if !ok {
				continue
			}
			field, ok := object[key]
			if !ok || field == nil {
				continue
			}
			if !array {
				next = append(next, field)
				continue
			}
			items, ok := field.([]any)
			if !ok {
				continue
			}
			next = append(next, items...)
		}
		if current = next; len(current) == 0 {
			return nil
		}
	}
	return current
}

// jsonlStrings returns the non-empty strings a path names, ignoring values of
// any other type: a field the agent sometimes fills with an object is skipped
// rather than rendered as Go's idea of one.
func jsonlStrings(value any, path string) []string {
	if path == "" {
		return nil
	}
	var found []string
	for _, item := range jsonlLookup(value, path) {
		text, ok := item.(string)
		if !ok {
			continue
		}
		if text = strings.TrimSpace(text); text != "" {
			found = append(found, text)
		}
	}
	return found
}

// jsonlFragments returns the strings a path names without trimming them, since
// the space between two deltas is part of the message they spell. Empty
// strings are dropped: they add nothing to join.
func jsonlFragments(value any, path string) []string {
	if path == "" {
		return nil
	}
	var found []string
	for _, item := range jsonlLookup(value, path) {
		if text, ok := item.(string); ok && text != "" {
			found = append(found, text)
		}
	}
	return found
}

func firstJSONLString(values []any) string {
	for _, value := range values {
		if text, ok := value.(string); ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

// codexJSONOutputWriter renders `codex exec --json` for the job's scrollback:
// agent messages as text, commands, file changes and tool calls as progress
// lines, and the thread id in the "session id:" form the codex definition
// captures.
type codexJSONOutputWriter struct {
	lineWriter
	output io.Writer
	mapper transcript.Mapper
}

func newCodexJSONOutputWriter(output io.Writer) *codexJSONOutputWriter {
	w := &codexJSONOutputWriter{output: output, mapper: transcript.NewMapper(transcript.FormatCodexJSON)}
	w.line = w.writeLine
	return w
}

func (w *codexJSONOutputWriter) writeLine(line []byte) error {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil
	}
	var envelope struct {
		Type     string `json:"type"`
		ThreadID string `json:"thread_id"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		_, err = fmt.Fprintln(w.output, string(line))
		return err
	}
	if envelope.Type == "thread.started" && envelope.ThreadID != "" {
		_, err := fmt.Fprintf(w.output, "session id: %s\n", envelope.ThreadID)
		return err
	}
	var texts []string
	for _, event := range w.mapper.Map(line) {
		switch {
		case event.Kind == transcript.KindMessage:
			if text := strings.TrimSpace(event.Text); text != "" {
				texts = append(texts, text)
			}
		case event.Kind == transcript.KindCommand && event.Status == "running":
			texts = append(texts, "Running: "+event.Command)
		case event.Kind == transcript.KindFileChange:
			texts = append(texts, "Editing: "+event.Path)
		case event.Kind == transcript.KindToolCall:
			texts = append(texts, "Running: "+event.Name)
		case event.Kind == transcript.KindSystem && event.IsError:
			texts = append(texts, "Agent error: "+event.Text)
		}
	}
	if len(texts) == 0 {
		return nil
	}
	_, err := fmt.Fprintln(w.output, strings.Join(texts, "\n"))
	return err
}
