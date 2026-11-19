package projects

import (
	"reflect"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"
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
	if s.Concurrency != 6 || s.Interval != time.Minute || s.ReadyLabel != "" || !s.NoMerge || s.Delivery != "poll" {
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

func TestManagerRejectsABadUnblockInterval(t *testing.T) {
	m := newTestManager(t)
	m.Reconcile(config.ProjectsConfig{List: []config.ProjectConfig{{Target: "o/r"}}, UnblockInterval: "soon"})
	if status := m.Status(t.Context(), 0); status["error"] == nil {
		t.Fatalf("status = %v, want the settings error", status)
	}
}

func TestManagerLogsSince(t *testing.T) {
	m := newTestManager(t)
	for i := range managerLogLines + 5 {
		m.Log(string(rune('a' + i%26)))
	}
	lines, offset := m.logsSince(0, 3)
	if len(lines) != 3 || offset != managerLogLines+5 {
		t.Fatalf("lines = %v, offset = %d", lines, offset)
	}
	m.Log("new")
	lines, next := m.logsSince(offset, 100)
	if !reflect.DeepEqual(lines, []string{"new"}) || next != offset+1 {
		t.Fatalf("lines = %v, next = %d", lines, next)
	}
	// An offset older than the kept lines returns every kept line.
	lines, _ = m.logsSince(1, 0)
	if len(lines) != managerLogLines {
		t.Fatalf("stale offset returned %d lines", len(lines))
	}
}
