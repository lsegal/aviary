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

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/browser"
	"github.com/lsegal/aviary/internal/projects/core"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.SetEngineEnabled(false)
	m.SetAgents(nil, config.ModelsConfig{Defaults: &config.ModelDefaults{Model: "cli/codex"}}, config.ApprovalConfig{})
	return m
}

func TestManagerSettingsMergeConfigAndWatches(t *testing.T) {
	m := newTestManager(t)
	m.SetAgents([]config.AgentConfig{
		{Name: "shared", Model: "cli/codex"},
		{Name: "opus", Model: "cli/claude/opus", Fallbacks: []string{"cli/codex"}},
		{Name: "high", Model: "cli/codex:high"},
	}, config.ModelsConfig{}, config.ApprovalConfig{})
	m.Reconcile(config.ProjectsConfig{
		List:        []config.ProjectConfig{{Target: "o/a", Agent: "opus"}, {Target: "o/b"}},
		Agent:       "shared",
		Concurrency: 2,
		Interval:    "1m",
	})
	label := ""
	if err := m.AddWatch(Watch{Targets: []string{"o/c", "o/d"}, Agent: "high", Settings: config.ProjectsConfig{Concurrency: 6, ReadyLabel: &label, NoMerge: true}}); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	s, err := m.settingsLocked()
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"o/a", "o/b", "o/c", "o/d"}; !reflect.DeepEqual(s.Targets, want) {
		t.Fatalf("targets = %v, want %v", s.Targets, want)
	}
	if got := s.TargetAgents["o/a"]; !reflect.DeepEqual(got, []string{"claude/opus", "codex"}) {
		t.Fatalf("a project's jobs should run on its agent's model and fallbacks: %v", got)
	}
	if got := s.TargetAgents["o/b"]; !reflect.DeepEqual(got, []string{"codex"}) {
		t.Fatalf("a project without an agent should use the section's: %v", got)
	}
	if got := s.TargetAgents["o/c"]; !reflect.DeepEqual(got, []string{"codex:high"}) {
		t.Fatalf("watch agents = %v", got)
	}
	if s.Concurrency != 6 || s.Interval != time.Minute || s.ReadyLabel != "" || !s.NoMerge || s.Delivery != DeliveryBrowser {
		t.Fatalf("settings = %+v", s)
	}
	if !reflect.DeepEqual(s.Agents, []string{"codex"}) {
		t.Fatalf("agents = %v", s.Agents)
	}
	if got := m.Status(t.Context(), 0)["agents"]; !reflect.DeepEqual(got, []string{"codex", "claude/opus", "codex:high"}) {
		t.Fatalf("status agents = %v", got)
	}
}

func TestManagerSettingsRejectUnattachedProjects(t *testing.T) {
	m := newTestManager(t)
	m.SetAgents([]config.AgentConfig{{Name: "nomodel"}}, config.ModelsConfig{}, config.ApprovalConfig{})
	for name, cfg := range map[string]config.ProjectsConfig{
		"no agent":      {List: []config.ProjectConfig{{Target: "o/r"}}}, // and no models.defaults
		"unknown agent": {List: []config.ProjectConfig{{Target: "o/r", Agent: "nobody"}}},
		"no model":      {List: []config.ProjectConfig{{Target: "o/r"}}, Agent: "nomodel"},
	} {
		m.mu.Lock()
		m.cfg = cfg
		_, err := m.settingsLocked()
		m.mu.Unlock()
		if err == nil || !strings.Contains(err.Error(), "o/r") && !strings.Contains(err.Error(), "section") {
			t.Errorf("%s: err = %v, want the unattached project named", name, err)
		}
	}
}

func TestManagerAgentModelsDefaultFromModelsSection(t *testing.T) {
	m := newTestManager(t)
	m.SetAgents([]config.AgentConfig{{Name: "plain"}, {Name: "api", Model: "anthropic/claude-sonnet-5-5", Fallbacks: []string{"cli/claude/opus"}}},
		config.ModelsConfig{Defaults: &config.ModelDefaults{Model: "cli/gemini/pro", Fallbacks: []string{"cli/codex"}}}, config.ApprovalConfig{})
	m.Reconcile(config.ProjectsConfig{List: []config.ProjectConfig{{Target: "o/a", Agent: "plain"}, {Target: "o/b", Agent: "api"}}})
	m.mu.Lock()
	s, err := m.settingsLocked()
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if got := s.TargetAgents["o/a"]; !reflect.DeepEqual(got, []string{"gemini/pro", "codex"}) {
		t.Fatalf("an agent without a model should take models.defaults: %v", got)
	}
	if got := s.TargetAgents["o/b"]; !reflect.DeepEqual(got, []string{"aviary/api", "claude/opus"}) {
		t.Fatalf("an API model should run the job on the agent itself: %v", got)
	}
	if !reflect.DeepEqual(s.Agents, []string{"gemini/pro"}) {
		t.Fatalf("agents = %v", s.Agents)
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
		"yolo":        func(s *engineSettings) { s.Approval = clidefs.ApprovalFull },
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
	// The watch polls through gh: the default browser delivery would launch a
	// real Chrome on the retry, which can outlast the deadline on a loaded CI
	// runner (issue #448).
	previous := startBrowserDriver
	t.Cleanup(func() { startBrowserDriver = previous })
	startBrowserDriver = func(context.Context, browser.Config) (*browser.Browser, error) {
		t.Error("the engine launched a browser")
		return nil, errors.New("no browser in tests")
	}
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Shutdown)
	m.SetAgents([]config.AgentConfig{{Name: "coder", Model: "cli/codex"}}, config.ModelsConfig{}, config.ApprovalConfig{})
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
	if err := m.AddWatch(Watch{Targets: []string{"o/r"}, Agent: "coder", Settings: config.ProjectsConfig{Delivery: DeliveryPoll}}); !errors.Is(err, errLoginLookup) {
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
			t.Fatalf("engine did not start after %d lookups", lookups.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := lookups.Load(); got != 2 {
		t.Fatalf("lookups = %d, want 2", got)
	}
}

// TestManagerSettingsResolveApprovalMode checks Projects jobs run under the
// Projects override, fall back to the general approval mode without one, and
// that yolo, in the config or on a watch, is the override's full mode
// (issue #513).
func TestManagerSettingsResolveApprovalMode(t *testing.T) {
	m := newTestManager(t)
	approval := func() clidefs.ApprovalMode {
		t.Helper()
		m.mu.Lock()
		defer m.mu.Unlock()
		s, err := m.settingsLocked()
		if err != nil {
			t.Fatal(err)
		}
		return s.Approval
	}
	models := config.ModelsConfig{Defaults: &config.ModelDefaults{Model: "cli/codex"}}

	m.Reconcile(config.ProjectsConfig{List: []config.ProjectConfig{{Target: "o/a"}}})
	if got := approval(); got != clidefs.ApprovalAuto {
		t.Fatalf("no setting: approval = %q, want auto", got)
	}
	m.SetAgents(nil, models, config.ApprovalConfig{Mode: clidefs.ApprovalReadOnly, Chat: clidefs.ApprovalFull})
	m.Reconcile(config.ProjectsConfig{List: []config.ProjectConfig{{Target: "o/a"}}})
	if got := approval(); got != clidefs.ApprovalReadOnly {
		t.Fatalf("general setting: approval = %q, want read-only", got)
	}
	m.Reconcile(config.ProjectsConfig{List: []config.ProjectConfig{{Target: "o/a"}}, ApprovalMode: clidefs.ApprovalAuto})
	if got := approval(); got != clidefs.ApprovalAuto {
		t.Fatalf("Projects override: approval = %q, want auto", got)
	}
	m.Reconcile(config.ProjectsConfig{List: []config.ProjectConfig{{Target: "o/a"}}, Yolo: true})
	if got := approval(); got != clidefs.ApprovalFull {
		t.Fatalf("yolo: approval = %q, want full", got)
	}
	m.Reconcile(config.ProjectsConfig{List: []config.ProjectConfig{{Target: "o/a"}}})
	if err := m.AddWatch(Watch{Targets: []string{"o/b"}, Settings: config.ProjectsConfig{Yolo: true}}); err != nil {
		t.Fatal(err)
	}
	if got := approval(); got != clidefs.ApprovalFull {
		t.Fatalf("watch --yolo: approval = %q, want full", got)
	}
}
