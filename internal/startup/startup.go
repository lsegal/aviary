// Package startup times the server's boot phases and tracks which
// subsystems are still initializing in the background.
//
// The HTTP server and /mcp come up as soon as config and state are loaded;
// slower work (channel connections, Code Loop discovery, skill sync) runs
// afterwards in goroutines. A Trace records how long each phase took, when
// /mcp became ready, and which subsystems are still pending, so /readyz,
// `aviary doctor` and `aviary serve --trace-startup` can report it.
package startup

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/store"
)

// MCPReadyTarget is how soon after boot begins /mcp should accept requests
// on a warm restart. TestTimeToMCPReady fails when a boot exceeds it.
const MCPReadyTarget = 2 * time.Second

// Subsystem readiness states.
const (
	StatePending = "pending"
	StateReady   = "ready"
	StateFailed  = "failed"
)

// Phase is one timed step of a boot.
type Phase struct {
	Name string `json:"name"`
	// StartMS is when the phase began, in milliseconds since boot began.
	StartMS int64 `json:"start_ms"`
	// DurationMS is how long the phase took.
	DurationMS int64 `json:"duration_ms"`
	// Background is true for phases that run off the critical path, after
	// /mcp is up.
	Background bool   `json:"background,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Subsystem is the readiness of one part of the server.
type Subsystem struct {
	Name  string `json:"name"`
	State string `json:"state"`
	// SinceMS is when the subsystem entered State, in milliseconds since
	// boot began.
	SinceMS int64  `json:"since_ms"`
	Error   string `json:"error,omitempty"`
}

// Report is a snapshot of a boot.
type Report struct {
	StartedAt time.Time `json:"started_at"`
	PID       int       `json:"pid,omitempty"`
	// MCPReadyMS is when /mcp began accepting requests, in milliseconds since
	// boot began; 0 until then.
	MCPReadyMS int64 `json:"mcp_ready_ms"`
	// TotalMS is when the last phase, background ones included, ended.
	TotalMS    int64       `json:"total_ms"`
	Ready      bool        `json:"ready"`
	Phases     []Phase     `json:"phases"`
	Subsystems []Subsystem `json:"subsystems"`
}

// Trace records a boot.
type Trace struct {
	mu         sync.Mutex
	began      time.Time
	mcpReady   time.Duration
	phases     []Phase
	subsystems map[string]*Subsystem
	order      []string
	now        func() time.Time
}

var (
	currentMu sync.Mutex
	current   = newTrace(time.Now)
)

func newTrace(now func() time.Time) *Trace {
	return &Trace{began: now(), subsystems: map[string]*Subsystem{}, now: now}
}

// Begin starts a new boot trace and makes it the current one.
func Begin() *Trace {
	t := newTrace(time.Now)
	currentMu.Lock()
	current = t
	currentMu.Unlock()
	return t
}

// Current returns the trace of the boot under way or last completed.
func Current() *Trace {
	currentMu.Lock()
	defer currentMu.Unlock()
	return current
}

func (t *Trace) since() time.Duration { return t.now().Sub(t.began) }

// Phase starts timing the named critical-path phase and returns the function
// that ends it.
func (t *Trace) Phase(name string) func() {
	return t.phase(name, false)
}

func (t *Trace) phase(name string, background bool) func() {
	start := t.since()
	return func() { t.record(name, start, background, nil) }
}

func (t *Trace) record(name string, start time.Duration, background bool, err error) {
	end := t.since()
	p := Phase{Name: name, StartMS: start.Milliseconds(), DurationMS: (end - start).Milliseconds(), Background: background}
	if err != nil {
		p.Error = err.Error()
	}
	t.mu.Lock()
	t.phases = append(t.phases, p)
	t.mu.Unlock()
	attrs := []any{"component", "startup", "phase", name, "duration_ms", p.DurationMS, "at_ms", end.Milliseconds()}
	if background {
		attrs = append(attrs, "background", true)
	}
	if err != nil {
		attrs = append(attrs, "err", err)
	}
	slog.Info("startup: phase done", attrs...)
}

// Time runs fn as the named critical-path phase.
func (t *Trace) Time(name string, fn func()) {
	defer t.Phase(name)()
	fn()
}

// Pending marks a subsystem as still initializing.
func (t *Trace) Pending(name string) {
	t.setState(name, StatePending, nil)
}

// Go marks subsystem pending and runs fn in the background as the named
// phase, marking the subsystem ready, or failed when fn returns an error,
// once fn returns. done, when not nil, is called after that.
func (t *Trace) Go(subsystem, phase string, fn func() error, done func()) {
	t.Pending(subsystem)
	go func() {
		if done != nil {
			defer done()
		}
		start := t.since()
		err := fn()
		t.record(phase, start, true, err)
		if err != nil {
			t.setState(subsystem, StateFailed, err)
		} else {
			t.setState(subsystem, StateReady, nil)
		}
		if t.Snapshot().Ready {
			t.Persist()
		}
	}()
}

func (t *Trace) setState(name, state string, err error) {
	at := t.since().Milliseconds()
	t.mu.Lock()
	s, ok := t.subsystems[name]
	if !ok {
		s = &Subsystem{Name: name}
		t.subsystems[name] = s
		t.order = append(t.order, name)
	}
	s.State, s.SinceMS, s.Error = state, at, ""
	if err != nil {
		s.Error = err.Error()
	}
	t.mu.Unlock()
}

// MarkMCPReady records that /mcp is accepting requests. Only the first call
// counts; a listener rotation does not move it.
func (t *Trace) MarkMCPReady() {
	t.mu.Lock()
	first := t.mcpReady == 0
	if first {
		t.mcpReady = max(t.since(), time.Millisecond)
	}
	ready := t.mcpReady
	t.mu.Unlock()
	if first {
		t.setState("mcp", StateReady, nil)
		slog.Info("startup: /mcp ready", "component", "startup", "at_ms", ready.Milliseconds())
		t.Persist()
	}
}

// State returns a subsystem's state, and false when it was never tracked.
func (t *Trace) State(name string) (Subsystem, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.subsystems[name]
	if !ok {
		return Subsystem{}, false
	}
	return *s, true
}

// Snapshot returns the boot so far.
func (t *Trace) Snapshot() Report {
	t.mu.Lock()
	defer t.mu.Unlock()
	r := Report{
		StartedAt:  t.began,
		MCPReadyMS: t.mcpReady.Milliseconds(),
		Phases:     slices.Clone(t.phases),
		Ready:      t.mcpReady > 0,
	}
	for _, p := range r.Phases {
		r.TotalMS = max(r.TotalMS, p.StartMS+p.DurationMS)
	}
	for _, name := range t.order {
		s := *t.subsystems[name]
		if s.State == StatePending {
			r.Ready = false
		}
		r.Subsystems = append(r.Subsystems, s)
	}
	return r
}

// Path is where the server keeps the report of its latest boot, for
// `aviary doctor` to read.
func Path() string {
	return filepath.Join(store.DataDir(), "startup.json")
}

// Persist writes the boot so far to Path.
func (t *Trace) Persist() {
	r := t.Snapshot()
	r.PID = os.Getpid()
	if err := store.WriteJSON(Path(), r); err != nil {
		slog.Debug("startup: failed to save boot report", "err", err)
	}
}

// Load reads the report Persist wrote.
func Load() (Report, error) {
	return store.ReadJSON[Report](Path())
}

// InitializingError is returned for work whose subsystem has not finished
// initializing.
type InitializingError struct {
	Subsystem  string
	RetryAfter time.Duration
}

func (e *InitializingError) Error() string {
	return fmt.Sprintf("aviary is still initializing %s; retry in %ds", e.Subsystem, int(e.RetryAfter.Round(time.Second)/time.Second))
}

// retryAfter is the retry hint an InitializingError gives.
const retryAfter = 2 * time.Second

// CheckReady returns an InitializingError while subsystem is pending in the
// current boot, and nil once it is ready, failed, or was never tracked.
func CheckReady(subsystem string) error {
	if s, ok := Current().State(subsystem); ok && s.State == StatePending {
		return &InitializingError{Subsystem: subsystem, RetryAfter: retryAfter}
	}
	return nil
}

// Summary is a one-line account of the boot, such as
// "boot: 3.2s, MCP ready at 0.4s; slowest: channels connect 2.8s, Code Loop discovery 1.1s".
func (r Report) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "boot: %s", seconds(r.TotalMS))
	if r.MCPReadyMS > 0 {
		fmt.Fprintf(&b, ", MCP ready at %s", seconds(r.MCPReadyMS))
	} else {
		b.WriteString(", MCP not ready yet")
	}
	if pending := r.pendingNames(); len(pending) > 0 {
		fmt.Fprintf(&b, ", still initializing: %s", strings.Join(pending, ", "))
	}
	if slow := r.Slowest(3); len(slow) > 0 {
		parts := make([]string, len(slow))
		for i, p := range slow {
			parts[i] = p.Name + " " + seconds(p.DurationMS)
		}
		fmt.Fprintf(&b, "; slowest: %s", strings.Join(parts, ", "))
	}
	return b.String()
}

// Redacted returns r without error text.
func (r Report) Redacted() Report {
	r.Phases = slices.Clone(r.Phases)
	for i := range r.Phases {
		r.Phases[i].Error = ""
	}
	r.Subsystems = slices.Clone(r.Subsystems)
	for i := range r.Subsystems {
		r.Subsystems[i].Error = ""
	}
	return r
}

// Slowest returns up to n phases, longest first.
func (r Report) Slowest(n int) []Phase {
	phases := slices.Clone(r.Phases)
	slices.SortStableFunc(phases, func(a, b Phase) int { return int(b.DurationMS - a.DurationMS) })
	return phases[:min(n, len(phases))]
}

func (r Report) pendingNames() []string {
	var names []string
	for _, s := range r.Subsystems {
		if s.State == StatePending {
			names = append(names, s.Name)
		}
	}
	return names
}

// Detail is the full per-phase trace, in the order the phases began.
func (r Report) Detail() string {
	var b strings.Builder
	b.WriteString(r.Summary())
	b.WriteString("\n")
	phases := slices.Clone(r.Phases)
	slices.SortStableFunc(phases, func(a, b Phase) int { return int(a.StartMS - b.StartMS) })
	for _, p := range phases {
		where := "critical"
		if p.Background {
			where = "background"
		}
		fmt.Fprintf(&b, "  %8s  +%-8s %-10s %s", seconds(p.DurationMS), seconds(p.StartMS), where, p.Name)
		if p.Error != "" {
			fmt.Fprintf(&b, " (error: %s)", p.Error)
		}
		b.WriteString("\n")
	}
	for _, s := range r.Subsystems {
		fmt.Fprintf(&b, "  subsystem %-10s %s at +%s", s.Name, s.State, seconds(s.SinceMS))
		if s.Error != "" {
			fmt.Fprintf(&b, " (error: %s)", s.Error)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func seconds(ms int64) string {
	return fmt.Sprintf("%.2fs", float64(ms)/1000)
}
