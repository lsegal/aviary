package projects

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/core"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.SetEngineEnabled(false)
	return m
}

func TestManagerSettingsMergeConfigAndWatches(t *testing.T) {
	m := newTestManager(t)
	m.Reconcile(config.ProjectsConfig{
		List:        []config.ProjectConfig{{Target: "o/a", Agents: []string{"claude/opus", "codex"}}, {Target: "o/b"}},
		Concurrency: 2,
		Interval:    "1m",
	})
	label := ""
	if err := m.AddWatch(Watch{Targets: []string{"o/c", "o/a"}, Agents: []string{"codex:high"}, Settings: config.ProjectsConfig{Concurrency: 6, ReadyLabel: &label, NoMerge: true}}); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	s, err := m.settingsLocked()
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"o/a", "o/b", "o/c"}; !reflect.DeepEqual(s.Targets, want) {
		t.Fatalf("targets = %v, want %v", s.Targets, want)
	}
	if got := s.TargetAgents["o/c"]; !reflect.DeepEqual(got, []string{"codex:high"}) {
		t.Fatalf("watch agents = %v", got)
	}
	if got := s.TargetAgents["o/a"]; !reflect.DeepEqual(got, []string{"codex:high"}) {
		t.Fatalf("a watch's agents should replace the saved project's: %v", got)
	}
	if s.Concurrency != 6 || s.Interval != time.Minute || s.ReadyLabel != "" || !s.NoMerge || s.Delivery != DeliveryBrowser {
		t.Fatalf("settings = %+v", s)
	}
	if !reflect.DeepEqual(s.Agents, []string{"codex"}) {
		t.Fatalf("agents = %v", s.Agents)
	}
	if got := m.Status(t.Context(), 0)["agents"]; !reflect.DeepEqual(got, []string{"codex", "codex:high"}) {
		t.Fatalf("status agents = %v", got)
	}
}

func TestManagerRestartKeyIgnoresLiveSettings(t *testing.T) {
	base := engineSettings{Targets: []string{"o/r"}, Agents: []string{"codex"}, Interval: time.Minute}
	live := base
	live.Concurrency, live.NoMerge, live.NoChangelog, live.TempDirs = 9, true, true, []string{"/tmp/x"}
	live.AllowedCommenters, live.UnblockInterval, live.UnblockAgent = []string{"me"}, time.Hour, "claude"
	if base.restartKey() != live.restartKey() {
		t.Fatal("a live setting changed the restart key")
	}
	for name, change := range map[string]func(*engineSettings){
		"targets":     func(s *engineSettings) { s.Targets = []string{"o/r", "o/s"} },
		"agents":      func(s *engineSettings) { s.Agents = []string{"claude"} },
		"interval":    func(s *engineSettings) { s.Interval = time.Hour },
		"delivery":    func(s *engineSettings) { s.Delivery = "webhook" },
		"ready label": func(s *engineSettings) { s.ReadyLabel = "go" },
		"filter":      func(s *engineSettings) { s.Filter = "label:x" },
		"yolo":        func(s *engineSettings) { s.Yolo = true },
	} {
		changed := base
		change(&changed)
		if changed.restartKey() == base.restartKey() {
			t.Errorf("changing %s does not restart the engine", name)
		}
	}
}

func TestManagerWatchesComeAndGo(t *testing.T) {
	m := newTestManager(t)
	if err := m.AddWatch(Watch{}); err == nil {
		t.Fatal("a watch without targets was accepted")
	}
	if err := m.AddWatch(Watch{Targets: []string{"not a target"}}); err == nil {
		t.Fatal("an invalid target was accepted")
	}
	if err := m.AddWatch(Watch{Targets: []string{" o/r ", "o/s"}}); err != nil {
		t.Fatal(err)
	}
	if !m.RemoveWatch("o/r") || m.RemoveWatch("o/r") {
		t.Fatal("RemoveWatch should remove o/r exactly once")
	}
	m.mu.Lock()
	s, _ := m.settingsLocked()
	m.mu.Unlock()
	if !reflect.DeepEqual(s.Targets, []string{"o/s"}) {
		t.Fatalf("targets = %v", s.Targets)
	}
	m.Shutdown()
	m.Shutdown()
}

func TestManagerStatusReportsAllowedCommenters(t *testing.T) {
	m := newTestManager(t)
	if got := m.Status(t.Context(), 0)["allowed_commenters"]; !reflect.DeepEqual(got, []string{}) {
		t.Fatalf("unconfigured allowed_commenters = %#v, want an empty list", got)
	}
	m.Reconcile(config.ProjectsConfig{List: []config.ProjectConfig{{Target: "o/r"}}, AllowedCommenters: []string{"alice", "bob"}})
	if got := m.Status(t.Context(), 0)["allowed_commenters"]; !reflect.DeepEqual(got, []string{"alice", "bob"}) {
		t.Fatalf("configured allowed_commenters = %v", got)
	}
	// A running engine reports the commenters it enforces, such as the
	// authenticated gh user it falls back to when none are configured.
	m.Reconcile(config.ProjectsConfig{List: []config.ProjectConfig{{Target: "o/r"}}})
	m.snapshot.Store(&core.Snapshot{AllowedCommenters: []string{"me"}})
	m.mu.Lock()
	m.running = &engineRun{}
	m.mu.Unlock()
	if got := m.Status(t.Context(), 0)["allowed_commenters"]; !reflect.DeepEqual(got, []string{"me"}) {
		t.Fatalf("running allowed_commenters = %v", got)
	}
	m.mu.Lock()
	m.running = nil
	m.mu.Unlock()
}

func TestManagerRejectsABadUnblockInterval(t *testing.T) {
	m := newTestManager(t)
	m.Reconcile(config.ProjectsConfig{List: []config.ProjectConfig{{Target: "o/r"}}, UnblockInterval: "soon"})
	if status := m.Status(t.Context(), 0); status["error"] == nil {
		t.Fatalf("status = %v, want the settings error", status)
	}
}

// logRecorder is a slog handler standing in for core's log ring: it keeps
// every record's message and component, and pages them as a LogSource.
type logRecorder struct {
	mu      sync.Mutex
	records [][2]string
	attrs   []slog.Attr
	root    *logRecorder
}

func (r *logRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *logRecorder) Handle(_ context.Context, rec slog.Record) error {
	component := ""
	for _, a := range r.attrs {
		if a.Key == "component" {
			component = a.Value.String()
		}
	}
	rec.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			component = a.Value.String()
		}
		return true
	})
	root := r.root
	root.mu.Lock()
	root.records = append(root.records, [2]string{component, rec.Message})
	root.mu.Unlock()
	return nil
}

func (r *logRecorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &logRecorder{attrs: append(slices.Clone(r.attrs), attrs...), root: r.root}
}

func (r *logRecorder) WithGroup(string) slog.Handler { return r }

func (r *logRecorder) since(component string, offset, limit int) ([]string, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	lines := []string{}
	for i, rec := range r.records {
		if rec[0] == component && i+1 > offset {
			lines = append(lines, rec[1])
		}
	}
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines, len(r.records)
}

// recordLogs routes slog through a logRecorder for the test and makes it m's
// log source.
func recordLogs(t *testing.T, m *Manager) *logRecorder {
	t.Helper()
	recorder := &logRecorder{}
	recorder.root = recorder
	previous := slog.Default()
	slog.SetDefault(slog.New(recorder))
	t.Cleanup(func() { slog.SetDefault(previous) })
	m.SetLogSource(recorder.since)
	return recorder
}

// TestManagerLogsThroughSlog checks Projects keeps no log buffer of its own:
// its lines go to slog under the projects component, without a timestamp in
// the text, and project_status pages them back from the log source.
func TestManagerLogsThroughSlog(t *testing.T) {
	m := newTestManager(t)
	if lines, offset := m.logsSince(7, 0); lines != nil || offset != 7 {
		t.Fatalf("without a source: lines = %v, offset = %d", lines, offset)
	}
	recorder := recordLogs(t, m)
	slog.Info("unrelated", "component", "server")
	m.logf("engine %s", "started")
	if !slices.Contains(recorder.records, [2]string{"projects", "engine started"}) {
		t.Fatalf("records = %v, want the line under the projects component", recorder.records)
	}
	status := m.Status(t.Context(), 0)
	if logs := status["recent_logs"]; !reflect.DeepEqual(logs, []string{"engine started"}) {
		t.Fatalf("recent_logs = %v", logs)
	}
	offset := status["log_offset"].(int)
	m.Log("new")
	status = m.Status(t.Context(), offset)
	if logs := status["recent_logs"]; !reflect.DeepEqual(logs, []string{"new"}) || status["log_offset"] != offset+1 {
		t.Fatalf("recent_logs = %v, log_offset = %v", logs, status["log_offset"])
	}
}

// TestManagerRetriesAFailedLoginLookup checks a transient failure resolving
// the gh login for allowed_commenters delays the engine instead of leaving the
// watch without one.
func TestManagerRetriesAFailedLoginLookup(t *testing.T) {
	initial := loginRetryInitial
	loginRetryInitial = 10 * time.Millisecond
	t.Cleanup(func() { loginRetryInitial = initial })
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Shutdown)
	recordLogs(t, m)
	var lookups atomic.Int32
	m.ghRunCommand = func(_ context.Context, args ...string) ([]byte, error) {
		if slices.Equal(args, []string{"api", "user", "--jq", ".login"}) {
			if lookups.Add(1) == 1 {
				return []byte(`{"message": "API rate limit exceeded"}`), errors.New("exit status 1")
			}
			return []byte("octocat\n"), nil
		}
		return nil, errors.New("gh is unavailable in tests")
	}
	if err := m.AddWatch(Watch{Targets: []string{"o/r"}, Agents: []string{"codex"}}); !errors.Is(err, errLoginLookup) {
		t.Fatalf("AddWatch error = %v, want the login lookup failure", err)
	}
	if logs, _ := m.logsSince(0, 0); !slices.ContainsFunc(logs, func(line string) bool { return strings.Contains(line, "waiting 10ms to retry starting") }) {
		t.Fatalf("logs = %q, want the retry wait", logs)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		m.mu.Lock()
		run := m.running
		m.mu.Unlock()
		if run != nil {
			if got := run.engine.AllowedCommenters; !slices.Equal(got, []string{"octocat"}) {
				t.Fatalf("allowed commenters = %q", got)
			}
			break
		}
		if time.Now().After(deadline) {
			m.mu.Lock()
			lastError := m.lastError
			m.mu.Unlock()
			t.Fatalf("engine did not start after %d lookups: %s", lookups.Load(), lastError)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := lookups.Load(); got != 2 {
		t.Fatalf("lookups = %d, want 2", got)
	}
}
