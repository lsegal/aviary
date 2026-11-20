// Package transcript defines the normalized per-job event schema Projects
// records for every coding-CLI run, and the mappers that turn each CLI's
// machine-readable event stream into it.
package transcript

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Kind names one event type in a job transcript.
type Kind string

// The event kinds of a job transcript. The first eight come from the coding
// CLI's own stream; comment_relay and system are recorded by Projects itself.
const (
	KindMessage      Kind = "message"
	KindReasoning    Kind = "reasoning"
	KindToolCall     Kind = "tool_call"
	KindToolResult   Kind = "tool_result"
	KindCommand      Kind = "command"
	KindFileChange   Kind = "file_change"
	KindPlan         Kind = "plan"
	KindUsage        Kind = "usage"
	KindCommentRelay Kind = "comment_relay"
	KindSystem       Kind = "system"
)

// Event is one entry in a job transcript. Only the fields its Kind uses are
// set.
type Event struct {
	Kind Kind      `json:"kind"`
	Time time.Time `json:"time"`
	// Text is a message, reasoning text, a relayed comment, or a system
	// event's description.
	Text string `json:"text,omitempty"`
	// Name and Input describe a tool call; CallID ties a tool call to its
	// result.
	Name   string         `json:"name,omitempty"`
	Input  map[string]any `json:"input,omitempty"`
	CallID string         `json:"call_id,omitempty"`
	// Output and IsError describe a tool result or a command's output.
	Output  string   `json:"output,omitempty"`
	IsError bool     `json:"is_error,omitempty"`
	Images  []string `json:"images,omitempty"`
	// Command, ExitCode and Status describe a shell command; Status is
	// "running" or "done".
	Command  string `json:"command,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Status   string `json:"status,omitempty"`
	// Path and Diff describe a file change.
	Path string `json:"path,omitempty"`
	Diff string `json:"diff,omitempty"`
	// Items is a plan's checklist.
	Items []PlanItem `json:"items,omitempty"`
	// Usage is a turn's token use and cost.
	Usage *Usage `json:"usage,omitempty"`
	// Author and URL identify a relayed comment.
	Author string `json:"author,omitempty"`
	URL    string `json:"url,omitempty"`
}

// PlanItem is one step of an agent's plan.
type PlanItem struct {
	Text   string `json:"text"`
	Status string `json:"status,omitempty"`
}

// Usage is the token use and cost an agent reported.
type Usage struct {
	InputTokens      int     `json:"input_tokens,omitempty"`
	OutputTokens     int     `json:"output_tokens,omitempty"`
	CacheReadTokens  int     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int     `json:"cache_write_tokens,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
}

// Mapper turns one line of a CLI's machine-readable output into transcript
// events. A line it does not recognize maps to nothing.
type Mapper interface {
	Map(line []byte) []Event
}

// Flusher is a Mapper that holds events back until a later line completes
// them, such as a message streamed as deltas. Flush returns what it still
// holds when the stream ends.
type Flusher interface {
	Flush() []Event
}

// Format names that select a Mapper. They match the agent definitions'
// output formats.
const (
	FormatClaudeStreamJSON = "stream-json"
	FormatCodexJSON        = "codex-json"
	FormatOpencodeJSON     = "opencode-json"
	FormatClineJSON        = "cline-json"
	FormatAviaryJSON       = "aviary-json"
	FormatGeminiJSON       = "gemini-json"
)

// NewMapper returns the mapper for an agent definition's output format, or for
// the transcript format a generic JSONL definition names, or nil when the
// format has no structured mapping yet. Such a run still records its raw
// output and Projects' own system events.
func NewMapper(format string) Mapper {
	switch format {
	case FormatClaudeStreamJSON, "claude-stream-json":
		return &claudeMapper{}
	case FormatCodexJSON:
		return &codexMapper{}
	case FormatOpencodeJSON:
		return opencodeMapper{}
	case FormatClineJSON:
		return &clineMapper{}
	case FormatAviaryJSON:
		return aviaryMapper{}
	case FormatGeminiJSON:
		return &geminiMapper{}
	}
	return nil
}

// maxEvents bounds how many events a job keeps in memory; the file keeps all
// of them.
const maxEvents = 2000

// maxRawBytes bounds how much raw output a job keeps in memory.
const maxRawBytes = 256 * 1024

// Log is one job's transcript: its events and its raw output. It is safe for
// concurrent use. When it has a directory, every event and every raw byte is
// also appended to files there, so a transcript outlives a restart.
type Log struct {
	mu     sync.Mutex
	dir    string
	events []Event
	raw    []byte
	now    func() time.Time
}

// Open returns the transcript stored in dir, loading any events already
// there. An empty dir keeps the transcript in memory only.
func Open(dir string) (*Log, error) {
	l := &Log{dir: dir, now: time.Now}
	if dir == "" {
		return l, nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	if f, err := os.Open(filepath.Join(dir, "events.jsonl")); err == nil {
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for scanner.Scan() {
			var event Event
			if json.Unmarshal(scanner.Bytes(), &event) == nil {
				l.events = append(l.events, event)
			}
		}
		_ = f.Close()
		l.trim()
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if raw, err := readTail(filepath.Join(dir, "stdout.log"), maxRawBytes); err == nil {
		l.raw = raw
	}
	return l, nil
}

func readTail(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > limit {
		if _, err := f.Seek(info.Size()-limit, io.SeekStart); err != nil {
			return nil, err
		}
	}
	return io.ReadAll(f)
}

// Add records events, stamping any without a time.
func (l *Log) Add(events ...Event) {
	if l == nil || len(events) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var lines []byte
	for _, event := range events {
		if event.Time.IsZero() {
			event.Time = l.now()
		}
		l.events = append(l.events, event)
		if l.dir != "" {
			if data, err := json.Marshal(event); err == nil {
				lines = append(append(lines, data...), '\n')
			}
		}
	}
	l.trim()
	if len(lines) > 0 {
		appendFile(filepath.Join(l.dir, "events.jsonl"), lines)
	}
}

// System records one of Projects' own events, such as a claim or a restart.
func (l *Log) System(text string) {
	l.Add(Event{Kind: KindSystem, Text: text})
}

// Events returns the last limit events, or all of them when limit is zero or
// less.
func (l *Log) Events(limit int) []Event {
	if l == nil {
		return []Event{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	events := l.events
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	return append([]Event{}, events...)
}

// Raw returns the captured raw output, newest last, limited to limit bytes
// when limit is positive.
func (l *Log) Raw(limit int) string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	raw := l.raw
	if limit > 0 && len(raw) > limit {
		raw = raw[len(raw)-limit:]
	}
	return string(raw)
}

func (l *Log) trim() {
	if len(l.events) > maxEvents {
		l.events = append([]Event(nil), l.events[len(l.events)-maxEvents:]...)
	}
}

// Recorder returns a writer that captures a run's raw output into the log and
// maps each complete line through mapper, which may be nil.
func (l *Log) Recorder(mapper Mapper) *Recorder {
	return &Recorder{log: l, mapper: mapper}
}

func (l *Log) addRaw(p []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.raw = append(l.raw, p...)
	if len(l.raw) > maxRawBytes {
		l.raw = append([]byte(nil), l.raw[len(l.raw)-maxRawBytes:]...)
	}
	if l.dir != "" {
		appendFile(filepath.Join(l.dir, "stdout.log"), p)
	}
}

func appendFile(path string, data []byte) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return
	}
	_, _ = f.Write(data)
	_ = f.Close()
}

// Recorder is an io.Writer over a run's raw output stream.
type Recorder struct {
	log    *Log
	mapper Mapper
	mu     sync.Mutex
	buffer []byte
	usage  Usage
	tools  int
	calls  map[string]bool
}

// Write records p as raw output and maps every line it completes.
func (r *Recorder) Write(p []byte) (int, error) {
	if r == nil || r.log == nil {
		return len(p), nil
	}
	r.log.addRaw(p)
	if r.mapper == nil {
		return len(p), nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buffer = append(r.buffer, p...)
	for {
		i := indexNewline(r.buffer)
		if i < 0 {
			break
		}
		line := r.buffer[:i]
		r.add(r.mapper.Map(line))
		r.buffer = r.buffer[i+1:]
	}
	return len(p), nil
}

// Flush maps a final line that ended without a newline, then records what
// the mapper still held back.
func (r *Recorder) Flush() {
	if r == nil || r.mapper == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buffer) > 0 {
		r.add(r.mapper.Map(r.buffer))
		r.buffer = nil
	}
	if f, ok := r.mapper.(Flusher); ok {
		r.add(f.Flush())
	}
}

// add records mapped events and totals the usage and tool calls they report.
// r.mu must be held.
func (r *Recorder) add(events []Event) {
	for _, event := range events {
		switch {
		case event.Kind == KindUsage && event.Usage != nil:
			r.usage.InputTokens += event.Usage.InputTokens
			r.usage.OutputTokens += event.Usage.OutputTokens
			r.usage.CacheReadTokens += event.Usage.CacheReadTokens
			r.usage.CacheWriteTokens += event.Usage.CacheWriteTokens
			r.usage.CostUSD += event.Usage.CostUSD
		case event.Kind == KindToolCall, event.Kind == KindFileChange, event.Kind == KindCommand:
			// A call is counted once however many events report on it.
			if event.CallID == "" {
				r.tools++
			} else if !r.calls[event.CallID] {
				if r.calls == nil {
					r.calls = map[string]bool{}
				}
				r.calls[event.CallID] = true
				r.tools++
			}
		}
	}
	r.log.Add(events...)
}

// Usage returns the token use and cost the run's stream has reported so far,
// summed across its turns, and how many tool calls it made.
func (r *Recorder) Usage() (Usage, int) {
	if r == nil {
		return Usage{}, 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.usage, r.tools
}

func indexNewline(b []byte) int {
	for i, c := range b {
		if c == '\n' {
			return i
		}
	}
	return -1
}
