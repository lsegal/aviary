package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/logging"
)

// logEntry is the shape of a single captured log record.
type logEntry struct {
	Seq       int64             `json:"seq"`
	Timestamp string            `json:"ts"`
	Level     string            `json:"level"`
	Component string            `json:"component"`
	Message   string            `json:"msg"`
	Attrs     map[string]string `json:"attrs,omitempty"`
}

// logHub captures slog records into a ring buffer and fans them out to SSE
// clients.  It implements slog.Handler so it can be composed with the default
// text/JSON handler.
type logHub struct {
	mu       sync.Mutex
	ring     []logEntry
	ringCap  int
	seq      int64
	subs     map[chan logEntry]struct{}
	delegate slog.Handler // forwards to the normal stderr handler
}

var globalHub = newLogHub(500)

func newLogHub(bufCap int) *logHub {
	return &logHub{
		ringCap: bufCap,
		ring:    make([]logEntry, 0, bufCap),
		subs:    make(map[chan logEntry]struct{}),
	}
}

// setDelegate sets the slog.Handler to which records are also forwarded.
func (h *logHub) setDelegate(d slog.Handler) {
	h.mu.Lock()
	h.delegate = d
	h.mu.Unlock()
}

// ── slog.Handler ─────────────────────────────────────────────────────────────

func (h *logHub) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *logHub) Handle(_ context.Context, r slog.Record) error {
	entry := h.recordToEntry(r)

	h.mu.Lock()
	h.ring = append(h.ring, entry)
	if len(h.ring) > h.ringCap {
		h.ring = h.ring[len(h.ring)-h.ringCap:]
	}
	subs := make([]chan logEntry, 0, len(h.subs))
	for ch := range h.subs {
		subs = append(subs, ch)
	}
	delegate := h.delegate
	h.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- entry:
		default: // drop if the client is too slow
		}
	}

	if delegate != nil {
		_ = delegate.Handle(context.Background(), r)
	}
	return nil
}

func (h *logHub) WithAttrs(attrs []slog.Attr) slog.Handler {
	h.mu.Lock()
	d := h.delegate
	h.mu.Unlock()
	if d != nil {
		return &hubChild{parent: h, delegate: d.WithAttrs(attrs), extra: attrs}
	}
	return &hubChild{parent: h, delegate: nil, extra: attrs}
}

func (h *logHub) WithGroup(name string) slog.Handler {
	h.mu.Lock()
	d := h.delegate
	h.mu.Unlock()
	if d != nil {
		return &hubGroup{parent: h, delegate: d.WithGroup(name), group: name}
	}
	return &hubGroup{parent: h, delegate: nil, group: name}
}

func (h *logHub) recordToEntry(r slog.Record) logEntry {
	h.mu.Lock()
	h.seq++
	seq := h.seq
	h.mu.Unlock()

	attrs := map[string]string{}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = fmt.Sprintf("%v", a.Value.Any())
		return true
	})

	comp := extractComponent(r.Message, attrs)

	level := "info"
	switch {
	case r.Level >= slog.LevelError:
		level = "error"
	case r.Level >= slog.LevelWarn:
		level = "warn"
	case r.Level >= slog.LevelDebug:
		level = "debug"
	}

	var out map[string]string
	if len(attrs) > 0 {
		out = attrs
	}

	return logEntry{
		Seq:       seq,
		Timestamp: r.Time.UTC().Format(time.RFC3339Nano),
		Level:     level,
		Component: comp,
		Message:   r.Message,
		Attrs:     out,
	}
}

// extractComponent determines the component name from a log message and attrs.
// It looks for a "component" attribute first, then parses the message prefix
// (text before the first colon), then defaults to "server".
func extractComponent(msg string, attrs map[string]string) string {
	if c, ok := attrs["component"]; ok {
		delete(attrs, "component")
		return c
	}
	if i := strings.Index(msg, ":"); i > 0 && i < 24 {
		prefix := strings.ToLower(strings.TrimSpace(msg[:i]))
		// Keep only simple word prefixes (no spaces inside).
		if !strings.Contains(prefix, " ") {
			return prefix
		}
		// Multi-word prefix: take just the first word.
		parts := strings.Fields(prefix)
		if len(parts) > 0 {
			return parts[0]
		}
	}
	return "server"
}

// ── child handlers (WithAttrs / WithGroup) ───────────────────────────────────

type hubChild struct {
	parent   *logHub
	delegate slog.Handler
	extra    []slog.Attr
}

func (c *hubChild) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (c *hubChild) WithAttrs(attrs []slog.Attr) slog.Handler {
	all := append(c.extra, attrs...)
	var d slog.Handler
	if c.delegate != nil {
		d = c.delegate.WithAttrs(attrs)
	}
	return &hubChild{parent: c.parent, delegate: d, extra: all}
}
func (c *hubChild) WithGroup(name string) slog.Handler {
	var d slog.Handler
	if c.delegate != nil {
		d = c.delegate.WithGroup(name)
	}
	return &hubGroup{parent: c.parent, delegate: d, group: name}
}
func (c *hubChild) Handle(ctx context.Context, r slog.Record) error {
	for _, a := range c.extra {
		r.AddAttrs(a)
	}
	return c.parent.Handle(ctx, r)
}

type hubGroup struct {
	parent   *logHub
	delegate slog.Handler
	group    string
}

func (g *hubGroup) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (g *hubGroup) WithAttrs(attrs []slog.Attr) slog.Handler {
	var d slog.Handler
	if g.delegate != nil {
		d = g.delegate.WithAttrs(attrs)
	}
	return &hubChild{parent: g.parent, delegate: d, extra: attrs}
}
func (g *hubGroup) WithGroup(name string) slog.Handler {
	var d slog.Handler
	if g.delegate != nil {
		d = g.delegate.WithGroup(name)
	}
	return &hubGroup{parent: g.parent, delegate: d, group: name}
}
func (g *hubGroup) Handle(ctx context.Context, r slog.Record) error {
	return g.parent.Handle(ctx, r)
}

// ── REST history endpoint ─────────────────────────────────────────────────────

// logsHistoryHandler returns a JSON array of historical log entries from the
// log file.  Query params: skip (lines from end already seen), limit (max to
// return, capped at 1000).  Response: {"entries":[...],"hasMore":bool}
func logsHistoryHandler(w http.ResponseWriter, r *http.Request) {
	skip := 0
	limit := 500
	if v := r.URL.Query().Get("skip"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &skip); n == 0 || err != nil {
			skip = 0
		}
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &limit); n == 0 || err != nil {
			limit = 500
		}
	}
	if limit > 1000 {
		limit = 1000
	}

	path := logging.LogFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"entries":[],"hasMore":false}`)
		return
	}

	lines := bytes.Split(data, []byte("\n"))
	// Drop empty trailing line from final newline.
	for len(lines) > 0 && len(bytes.TrimSpace(lines[len(lines)-1])) == 0 {
		lines = lines[:len(lines)-1]
	}

	total := len(lines)
	end := total - skip
	if end <= 0 {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"entries":[],"hasMore":false}`)
		return
	}
	start := end - limit
	hasMore := start > 0
	if start < 0 {
		start = 0
	}

	var entries []logEntry
	var seq int64
	for _, ln := range lines[start:end] {
		line := strings.TrimSpace(string(ln))
		if line == "" {
			continue
		}
		seq++
		e := parseLogLine(line)
		e.Seq = seq
		entries = append(entries, e)
	}

	type response struct {
		Entries []logEntry `json:"entries"`
		HasMore bool       `json:"hasMore"`
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response{Entries: entries, HasMore: hasMore})
}

// ── SSE endpoint ─────────────────────────────────────────────────────────────

// logsHandler streams log entries via SSE.  Protected by BearerMiddleware.
func logsHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	var seq int64
	sendEntry := func(e logEntry) bool {
		if e.Seq == 0 {
			seq++
			e.Seq = seq
		} else if e.Seq > seq {
			seq = e.Seq
		}
		data, err := json.Marshal(e)
		if err != nil {
			return true
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		return true
	}

	path := logging.LogFilePath()
	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		lines := bytes.Split(data, []byte("\n"))
		start := 0
		if len(lines) > 500 {
			start = len(lines) - 500
		}
		for _, ln := range lines[start:] {
			line := strings.TrimSpace(string(ln))
			if line == "" {
				continue
			}
			e := parseLogLine(line)
			sendEntry(e)
		}
	}

	// Stream live file appends so logs from other processes also appear.
	ctx := r.Context()
	var offset int64
	if st, err := os.Stat(path); err == nil {
		offset = st.Size()
	}
	var remainder string
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			st, err := f.Stat()
			if err != nil {
				_ = f.Close()
				continue
			}
			if st.Size() < offset {
				offset = 0
				remainder = ""
			}
			if st.Size() == offset {
				_ = f.Close()
				continue
			}
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				_ = f.Close()
				continue
			}
			chunk, err := io.ReadAll(f)
			_ = f.Close()
			if err != nil || len(chunk) == 0 {
				continue
			}
			offset += int64(len(chunk))

			text := remainder + string(chunk)
			parts := strings.Split(text, "\n")
			remainder = parts[len(parts)-1]
			for _, part := range parts[:len(parts)-1] {
				line := strings.TrimSpace(part)
				if line == "" {
					continue
				}
				sendEntry(parseLogLine(line))
			}
		}
	}
}

func parseLogLine(line string) logEntry {
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return logEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Level:     "info",
			Component: "log",
			Message:   line,
		}
	}

	ts, _ := raw["time"].(string)
	msg, _ := raw["msg"].(string)
	level, _ := raw["level"].(string)
	if level == "" {
		level = "info"
	}
	level = strings.ToLower(level)

	attrs := map[string]string{}
	for k, v := range raw {
		switch k {
		case "time", "level", "msg", "component":
			continue
		default:
			attrs[k] = fmt.Sprintf("%v", v)
		}
	}

	var component string
	if c, ok := raw["component"].(string); ok && c != "" {
		component = c
	} else {
		component = extractComponent(msg, attrs)
	}

	if ts == "" {
		ts = time.Now().UTC().Format(time.RFC3339Nano)
	}

	if len(attrs) == 0 {
		attrs = nil
	}

	return logEntry{
		Timestamp: ts,
		Level:     level,
		Component: component,
		Message:   msg,
		Attrs:     attrs,
	}
}
