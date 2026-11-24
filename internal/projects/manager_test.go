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
	"github.com/lsegal/aviary/internal/store"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.SetEngineEnabled(false)
	m.agents = &config.Config{Models: config.ModelsConfig{Defaults: &config.ModelDefaults{Model: "cli/codex"}}}
	return m
}

// ownedBy returns a config whose coder agent, on cli/codex, owns projects.
// ownedBy is a config whose coder agent owns targets, under the shared
// projects settings.
func ownedBy(settings config.ProjectsConfig, targets ...string) *config.Config {
	var list []config.ProjectConfig
	for _, target := range targets {
		list = append(list, config.ProjectConfig{Target: target})
	}
	return &config.Config{
		Agents:   []config.AgentConfig{{Name: "coder", Model: "cli/codex", Projects: list}},
		Projects: settings,
	}
}

func TestManagerSettingsMergeConfigAndWatches(t *testing.T) {
	m := newTestManager(t)
	m.Reconcile(&config.Config{
		Agents: []config.AgentConfig{
			{Name: "shared", Model: "cli/codex", Projects: config.AgentProjectsConfig{{Target: "o/b"}}},
			{Name: "opus", Model: "cli/claude/opus", Fallbacks: []string{"cli/codex"}, Projects: config.AgentProjectsConfig{{Target: "o/a"}}},
			{Name: "high", Model: "cli/codex:high"},
		},
		Projects: config.ProjectsConfig{Concurrency: 2, Interval: "1m"},
	})
	// Both agents' projects share the one engine-wide concurrency.
	m.mu.Lock()
	shared, err := m.settingsLocked()
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if shared.Concurrency != 2 || !reflect.DeepEqual(shared.Targets, []string{"o/b", "o/a"}) {
		t.Fatalf("shared settings = %+v", shared)
	}
	if err := m.AddWatch(Watch{Targets: []string{"o/c", "o/d"}, Agent: "high", Settings: config.ProjectsConfig{Concurrency: 6, NoMerge: true}}); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	s, err := m.settingsLocked()
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"o/b", "o/a", "o/c", "o/d"}; !reflect.DeepEqual(s.Targets, want) {
		t.Fatalf("targets = %v, want %v", s.Targets, want)
	}
	if got := s.TargetAgents["o/a"]; !reflect.DeepEqual(got, []string{"claude/opus", "codex"}) {
		t.Fatalf("a project's jobs should run on its owning agent's model and fallbacks: %v", got)
	}
	if got := s.TargetAgents["o/b"]; !reflect.DeepEqual(got, []string{"codex"}) {
		t.Fatalf("o/b should run on the agent that owns it: %v", got)
	}
	if got := s.TargetAgents["o/c"]; !reflect.DeepEqual(got, []string{"codex:high"}) {
		t.Fatalf("watch agents = %v", got)
	}
	if s.Concurrency != 6 || s.Interval != time.Minute || !s.NoMerge || s.Delivery != DeliveryBrowser {
		t.Fatalf("settings = %+v", s)
	}
	if !reflect.DeepEqual(s.Agents, []string{"codex"}) {
		t.Fatalf("agents = %v", s.Agents)
	}
	if got := m.Status(t.Context(), 0, nil)["agents"]; !reflect.DeepEqual(got, []string{"codex", "claude/opus", "codex:high"}) {
		t.Fatalf("status agents = %v", got)
	}
	// Each provider is listed once, carrying the models its jobs run on,
	// including the default model a bare provider spec resolves to.
	want := []StatusCLI{{Name: "codex", Models: []string{"gpt-6.1-terra"}}, {Name: "claude", Models: []string{"opus"}}}
	if got := m.Status(t.Context(), 0, nil)["clis"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("status clis = %+v", got)
	}
}

func TestStatusCLIsListEachProviderOnce(t *testing.T) {
	got := statusCLIs(
		[]string{"claude/opus", "claude", "codex:high", "claude/sonnet", "claude/opus:high"},
		map[string]string{"claude": "week 66% left", "gemini": "signed out"},
	)
	want := []StatusCLI{
		{Name: "claude", Models: []string{"opus", "sonnet"}, Quota: "week 66% left"},
		{Name: "codex", Models: []string{"gpt-6.1-terra"}},
		{Name: "gemini", Models: []string{}, Quota: "signed out"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("statusCLIs = %+v, want %+v", got, want)
	}
}

func TestManagerSettingsRejectUnattachedProjects(t *testing.T) {
	m := newTestManager(t)
	m.agents = &config.Config{Agents: []config.AgentConfig{{Name: "nomodel"}}}
	for name, owner := range map[string]string{
		"no agent":      "", // and no models.defaults
		"unknown agent": "nobody",
		"no model":      "nomodel",
	} {
		m.mu.Lock()
		m.cfg = config.ProjectsConfig{}
		m.list = []config.ProjectConfig{{Target: "o/r"}}
		m.owners = map[string]string{}
		if owner != "" {
			m.owners["o/r"] = owner
		}
		_, err := m.settingsLocked()
		m.mu.Unlock()
		if err == nil || !strings.Contains(err.Error(), "o/r") {
			t.Errorf("%s: err = %v, want the project named", name, err)
		}
	}
}

func TestManagerAgentModelsDefaultFromModelsSection(t *testing.T) {
	m := newTestManager(t)
	m.Reconcile(&config.Config{
		Agents: []config.AgentConfig{
			{Name: "plain", Projects: config.AgentProjectsConfig{{Target: "o/a"}}},
			{Name: "api", Model: "anthropic/claude-sonnet-5-5", Fallbacks: []string{"cli/claude/opus"}, Projects: config.AgentProjectsConfig{{Target: "o/b"}}},
		},
		Models: config.ModelsConfig{Defaults: &config.ModelDefaults{Model: "cli/gemini/pro", Fallbacks: []string{"cli/codex"}}},
	})
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

func TestManagerProjectModelOverride(t *testing.T) {
	m := newTestManager(t)
	cfg := &config.Config{
		Agents: []config.AgentConfig{
			{Name: "Aviary", Model: "cli/claude/sonnet", Fallbacks: []string{"cli/gemini/pro"}, Projects: config.AgentProjectsConfig{{Target: "o/a"}}},
		},
	}
	m.Reconcile(cfg)
	if got := m.ProjectModels()["o/a"]; got.String() != "claude/sonnet (from agent Aviary)" || !reflect.DeepEqual(got.Fallbacks, []string{"gemini/pro"}) {
		t.Fatalf("with no override the agent's models apply: %+v", got)
	}

	cfg.Projects = config.ProjectsConfig{Model: "claude/opus", Fallbacks: []string{"codex/gpt-5.6"}}
	m.Reconcile(cfg)
	m.mu.Lock()
	s, err := m.settingsLocked()
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if got := s.TargetAgents["o/a"]; !reflect.DeepEqual(got, []string{"claude/opus", "codex/gpt-5.6"}) {
		t.Fatalf("the override should replace the agent's models: %v", got)
	}
	if got := quotaAgentNames(s); !reflect.DeepEqual(got, []string{"claude", "codex"}) {
		t.Fatalf("quota should follow the override: %v", got)
	}
	status := m.Status(context.Background(), 0, nil)
	if got := status["model_labels"].(map[string]string)["o/a"]; got != "claude/opus (project override)" {
		t.Fatalf("status model = %q", got)
	}
	clis := status["clis"].([]StatusCLI)
	if len(clis) != 2 || clis[0].Name != "claude" || clis[1].Name != "codex" {
		t.Fatalf("status clis = %+v", clis)
	}

	// A runtime watch's settings override the saved ones.
	if err := m.AddWatch(Watch{Targets: []string{"o/b"}, Settings: config.ProjectsConfig{Model: "codex:gpt-5.6"}}); err != nil {
		t.Fatal(err)
	}
	models := m.ProjectModels()
	if got := models["o/b"]; got.Model != "codex/gpt-5.6" || got.Source != config.ProjectModelSourceOverride {
		t.Fatalf("watch model = %+v", got)
	}
	if !models["o/b"].runsOn("codex", "gpt-5.6") || models["o/b"].runsOn("claude", "opus") {
		t.Fatal("runsOn should match only the project's specs")
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
		"targets":  func(s *engineSettings) { s.Targets = []string{"o/r", "o/s"} },
		"agents":   func(s *engineSettings) { s.Agents = []string{"claude"} },
		"interval": func(s *engineSettings) { s.Interval = time.Hour },
		"delivery": func(s *engineSettings) { s.Delivery = "webhook" },
		"filter":   func(s *engineSettings) { s.Filter = "label:x" },
		"yolo":     func(s *engineSettings) { s.Approval = clidefs.ApprovalFull },
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
	if got := m.Status(t.Context(), 0, nil)["allowed_commenters"]; !reflect.DeepEqual(got, []string{}) {
		t.Fatalf("unconfigured allowed_commenters = %#v, want an empty list", got)
	}
	m.Reconcile(ownedBy(config.ProjectsConfig{AllowedCommenters: []string{"alice", "bob"}}, "o/r"))
	if got := m.Status(t.Context(), 0, nil)["allowed_commenters"]; !reflect.DeepEqual(got, []string{"alice", "bob"}) {
		t.Fatalf("configured allowed_commenters = %v", got)
	}
	// A running engine reports the commenters it enforces, such as the
	// authenticated gh user it falls back to when none are configured.
	m.Reconcile(ownedBy(config.ProjectsConfig{}, "o/r"))
	m.snapshot.Store(&core.Snapshot{AllowedCommenters: []string{"me"}})
	m.mu.Lock()
	m.running = &engineRun{}
	m.mu.Unlock()
	if got := m.Status(t.Context(), 0, nil)["allowed_commenters"]; !reflect.DeepEqual(got, []string{"me"}) {
		t.Fatalf("running allowed_commenters = %v", got)
	}
	m.mu.Lock()
	m.running = nil
	m.mu.Unlock()
}

func TestManagerRejectsABadUnblockInterval(t *testing.T) {
	m := newTestManager(t)
	m.Reconcile(ownedBy(config.ProjectsConfig{UnblockInterval: "soon"}, "o/r"))
	if status := m.Status(t.Context(), 0, nil); status["error"] == nil {
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

// TestManagerLogsThroughSlog checks Code Loop keeps no log buffer of its own:
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
	status := m.Status(t.Context(), 0, nil)
	if logs := status["recent_logs"]; !reflect.DeepEqual(logs, []string{"engine started"}) {
		t.Fatalf("recent_logs = %v", logs)
	}
	offset := status["log_offset"].(int)
	m.Log("new")
	status = m.Status(t.Context(), offset, nil)
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
	m.agents = ownedBy(config.ProjectsConfig{})
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

// TestManagerSettingsResolveApprovalMode checks Code Loop jobs run under the
// Code Loop override, fall back to the general approval mode without one, and
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
	// The coder agent owns o/a, and a watch no agent owns runs on
	// models.defaults; general is the approval: section.
	general := config.ApprovalConfig{}
	reconcile := func(p config.ProjectsConfig) {
		cfg := ownedBy(p, "o/a")
		cfg.Models = config.ModelsConfig{Defaults: &config.ModelDefaults{Model: "cli/codex"}}
		cfg.Approval = general
		m.Reconcile(cfg)
	}

	reconcile(config.ProjectsConfig{})
	if got := approval(); got != clidefs.ApprovalAuto {
		t.Fatalf("no setting: approval = %q, want auto", got)
	}
	general = config.ApprovalConfig{Mode: clidefs.ApprovalReadOnly, Chat: clidefs.ApprovalFull}
	reconcile(config.ProjectsConfig{})
	if got := approval(); got != clidefs.ApprovalReadOnly {
		t.Fatalf("general setting: approval = %q, want read-only", got)
	}
	reconcile(config.ProjectsConfig{ApprovalMode: clidefs.ApprovalAuto})
	if got := approval(); got != clidefs.ApprovalAuto {
		t.Fatalf("Code Loop override: approval = %q, want auto", got)
	}
	reconcile(config.ProjectsConfig{Yolo: true})
	if got := approval(); got != clidefs.ApprovalFull {
		t.Fatalf("yolo: approval = %q, want full", got)
	}
	reconcile(config.ProjectsConfig{})
	if err := m.AddWatch(Watch{Targets: []string{"o/b"}, Settings: config.ProjectsConfig{Yolo: true}}); err != nil {
		t.Fatal(err)
	}
	if got := approval(); got != clidefs.ApprovalFull {
		t.Fatalf("watch --yolo: approval = %q, want full", got)
	}
}

func TestManagerIdentityIsRememberedAcrossRestarts(t *testing.T) {
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })

	first, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if first.Identity() == "" || restarted.Identity() != first.Identity() {
		t.Fatalf("restarted identity = %q, want %q", restarted.Identity(), first.Identity())
	}
	state, err := store.ReadAppState()
	if err != nil {
		t.Fatal(err)
	}
	if state.InstanceID != first.Identity() {
		t.Fatalf("stored instance id = %q, want %q", state.InstanceID, first.Identity())
	}
}
