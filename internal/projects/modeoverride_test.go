package projects

import (
	"context"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/core"
)

// A project's own mode overrides the shared projects mode, which every
// project without one follows (issue #1371).
func TestManagerModeOverridesGlobalDefault(t *testing.T) {
	for _, tc := range []struct {
		name    string
		global  string
		modes   map[string]string
		manual  []string
		sources map[string]string
	}{
		{"global watch, one manual", "", map[string]string{"o/b": "manual"}, []string{"o/b"},
			map[string]string{"o/a": "global", "o/b": "project", "o/c": "global"}},
		{"global manual, one watch", "manual", map[string]string{"o/b": "watch"}, []string{"o/a", "o/c"},
			map[string]string{"o/a": "global", "o/b": "project", "o/c": "global"}},
		{"global manual, no overrides", "manual", nil, []string{"o/a", "o/b", "o/c"},
			map[string]string{"o/a": "global", "o/b": "global", "o/c": "global"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestManager(t)
			cfg := ownedBy(config.ProjectsConfig{Mode: tc.global}, "o/a", "o/b", "o/c")
			for i := range cfg.Agents[0].Projects {
				cfg.Agents[0].Projects[i].Mode = tc.modes[cfg.Agents[0].Projects[i].Target]
			}
			m.Reconcile(cfg)
			m.mu.Lock()
			settings, err := m.settingsLocked()
			m.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(settings.ManualTargets, tc.manual) {
				t.Errorf("manual targets = %v, want %v", settings.ManualTargets, tc.manual)
			}
			modes, sources := m.ProjectModes([]string{"o/a", "o/b", "o/c"})
			if !maps.Equal(sources, tc.sources) {
				t.Errorf("mode sources = %v, want %v", sources, tc.sources)
			}
			for target, mode := range modes {
				if want := slices.Contains(tc.manual, target); want != (mode == config.ProjectModeManual) {
					t.Errorf("%s mode = %q", target, mode)
				}
			}
		})
	}
}

// Changing the shared mode moves every project without an override, and
// none with one.
func TestManagerGlobalModeChangeSkipsOverrides(t *testing.T) {
	m := newTestManager(t)
	cfg := ownedBy(config.ProjectsConfig{}, "o/a", "o/b")
	cfg.Agents[0].Projects[1].Mode = config.ProjectModeWatch
	m.Reconcile(cfg)
	if mode, source := m.ProjectMode("o/a"); mode != "watch" || source != "global" {
		t.Fatalf("o/a = %s from %s", mode, source)
	}
	cfg.Projects.Mode = config.ProjectModeManual
	m.Reconcile(cfg)
	if mode, source := m.ProjectMode("o/a"); mode != "manual" || source != "global" {
		t.Errorf("o/a did not follow the shared mode: %s from %s", mode, source)
	}
	if mode, source := m.ProjectMode("o/b"); mode != "watch" || source != "project" {
		t.Errorf("o/b's override was not kept: %s from %s", mode, source)
	}
}

// A runtime watch's override wins over the saved one, and inherit clears it.
func TestManagerRuntimeModeOverrideInherits(t *testing.T) {
	m := newTestManager(t)
	m.Reconcile(&config.Config{Models: m.agents.Models, Projects: config.ProjectsConfig{Mode: config.ProjectModeManual}})
	if err := m.AddWatch(Watch{Targets: []string{"o/r", "o/s"}, Modes: map[string]string{"o/r": config.ProjectModeWatch}}); err != nil {
		t.Fatal(err)
	}
	if mode, source := m.ProjectMode("o/r"); mode != "watch" || source != "project" {
		t.Fatalf("o/r = %s from %s", mode, source)
	}
	if mode, _ := m.ProjectMode("o/s"); mode != "manual" {
		t.Fatalf("o/s = %s; want the shared manual", mode)
	}
	if _, err := m.SetWatchMode("o/r", config.ProjectModeInherit); err != nil {
		t.Fatal(err)
	}
	if mode, source := m.ProjectMode("o/r"); mode != "manual" || source != "global" {
		t.Errorf("inherit left o/r at %s from %s", mode, source)
	}
	if _, err := m.SetWatchMode("o/r", "sometimes"); err == nil {
		t.Error("an unknown mode was accepted")
	}
}

// targetSource records the targets it is searched for.
type targetSource struct {
	*manualSource
	mu      sync.Mutex
	targets []string
}

func (s *targetSource) ListIssues(ctx context.Context, target string) ([]Issue, error) {
	s.mu.Lock()
	s.targets = append(s.targets, target)
	s.mu.Unlock()
	return s.manualSource.ListIssues(ctx, target)
}

func (s *targetSource) searched() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.targets)
}

// Only the project in manual mode skips its issue search; the other project
// keeps polling.
func TestEngineSkipsDiscoveryPerProject(t *testing.T) {
	src := &targetSource{manualSource: newManualSource()}
	w := &Engine{
		Repo: "o/watch", Targets: []string{"o/watch", "o/manual"}, Manual: core.NewManualTargets([]string{"o/manual"}),
		Interval: time.Millisecond, Concurrency: 1, StatePath: filepath.Join(t.TempDir(), "state.json"),
		Issues: src, Runner: &fakeRunner{dispatched: make(chan int, 16)}, Out: &syncBuffer{},
	}
	runEngine(t, w)
	waitFor(t, "two searches", func() bool { return len(src.searched()) >= 2 })
	for _, target := range src.searched() {
		if target != "o/watch" {
			t.Fatalf("searched %s, a manual project", target)
		}
	}
}
