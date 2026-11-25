package projects

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/core"
)

// recordEvents collects the manager's project events.
func recordEvents(m *Manager) func() []ProjectEvent {
	var mu sync.Mutex
	var events []ProjectEvent
	m.SetProjectObserver(func(ev ProjectEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})
	return func() []ProjectEvent {
		mu.Lock()
		defer mu.Unlock()
		out := events
		events = nil
		return out
	}
}

func TestManagerEmitsProjectAddedAndRemoved(t *testing.T) {
	m := newTestManager(t)
	m.Reconcile(ownedBy(config.ProjectsConfig{}, "o/a"))
	events := recordEvents(m)

	m.ReconcileInBackground(ownedBy(config.ProjectsConfig{}, "o/a", "o/b"))
	if got, want := events(), []ProjectEvent{{Type: ProjectAdded, Target: "o/b", Agent: "coder"}}; !slices.Equal(got, want) {
		t.Fatalf("saved add: events = %+v, want %+v", got, want)
	}

	if err := m.AddWatchInBackground(Watch{Targets: []string{"o/c"}, Agent: "coder"}); err != nil {
		t.Fatal(err)
	}
	if got, want := events(), []ProjectEvent{{Type: ProjectAdded, Target: "o/c"}}; !slices.Equal(got, want) {
		t.Fatalf("runtime add: events = %+v, want %+v", got, want)
	}

	if !m.RemoveWatchInBackground("o/c") {
		t.Fatal("o/c had a runtime watch")
	}
	if got, want := events(), []ProjectEvent{{Type: ProjectRemoved, Target: "o/c"}}; !slices.Equal(got, want) {
		t.Fatalf("runtime remove: events = %+v, want %+v", got, want)
	}

	m.ReconcileInBackground(ownedBy(config.ProjectsConfig{}, "o/a"))
	if got, want := events(), []ProjectEvent{{Type: ProjectRemoved, Target: "o/b", Agent: "coder"}}; !slices.Equal(got, want) {
		t.Fatalf("saved remove: events = %+v, want %+v", got, want)
	}

	// Reapplying the same config changes nothing.
	m.Reconcile(ownedBy(config.ProjectsConfig{}, "o/a"))
	if got := events(); len(got) != 0 {
		t.Fatalf("unchanged config: events = %+v, want none", got)
	}
}

func TestManagerRejectedWatchIsNotAdded(t *testing.T) {
	m := newTestManager(t)
	events := recordEvents(m)
	err := m.AddWatchInBackground(Watch{Targets: []string{"o/r"}, Settings: config.ProjectsConfig{UnblockInterval: "soon"}})
	if err == nil {
		t.Fatal("a watch with rejected settings was added")
	}
	if got := events(); len(got) != 0 {
		t.Fatalf("events = %+v, want none", got)
	}
	m.mu.Lock()
	watches := len(m.watches)
	m.mu.Unlock()
	if watches != 0 {
		t.Fatalf("rejected watch kept: %d watches", watches)
	}
}

func TestManagerEmitsProjectUpdatedWhenCountsChange(t *testing.T) {
	m := newTestManager(t)
	m.Reconcile(ownedBy(config.ProjectsConfig{}, "o/a", "o/b"))
	events := recordEvents(m)

	snapshot := core.Snapshot{Targets: []string{"o/a", "o/b"}}
	m.Snapshot(snapshot)
	events() // the first snapshot reports every target

	snapshot.IssueCounts = map[string]int{"o/a": 3}
	m.Snapshot(snapshot)
	if got, want := events(), []ProjectEvent{{Type: ProjectUpdated, Target: "o/a", Agent: "coder"}}; !slices.Equal(got, want) {
		t.Fatalf("o/a polled: events = %+v, want %+v", got, want)
	}

	// The same counts again, with only a job's output changed, are quiet.
	snapshot.Jobs = []JobSnapshot{{Target: "o/b", Number: 1, Status: "active", Log: "one"}}
	m.Snapshot(snapshot)
	events()
	snapshot.Jobs = []JobSnapshot{{Target: "o/b", Number: 1, Status: "active", Log: "one\ntwo"}}
	m.Snapshot(snapshot)
	if got := events(); len(got) != 0 {
		t.Fatalf("output only: events = %+v, want none", got)
	}
}

func TestStatusShowsAddedProjectLoadingBeforeTheEngineRestarts(t *testing.T) {
	m := newTestManager(t)
	m.Reconcile(ownedBy(config.ProjectsConfig{}, "o/a"))
	// The running engine has polled o/a only.
	m.snapshot.Store(&core.Snapshot{Targets: []string{"o/a"}, IssueCounts: map[string]int{"o/a": 2}, LastPoll: time.Now()})

	// applyConfig is ReconcileInBackground before its engine restart, which
	// a disabled engine would answer by dropping the snapshot.
	m.applyConfig(ownedBy(config.ProjectsConfig{}, "o/a", "o/b"))
	status := m.Status(context.Background(), 0, nil)
	if got := status["targets"].([]string); !slices.Equal(got, []string{"o/a", "o/b"}) {
		t.Fatalf("targets = %v, want o/a then o/b", got)
	}
	if got := status["loading_targets"].([]string); !slices.Equal(got, []string{"o/b"}) {
		t.Fatalf("loading_targets = %v, want [o/b]", got)
	}

	m.applyConfig(ownedBy(config.ProjectsConfig{}, "o/b"))
	status = m.Status(context.Background(), 0, nil)
	if got := status["targets"].([]string); !slices.Equal(got, []string{"o/b"}) {
		t.Fatalf("after removing o/a, targets = %v, want [o/b]", got)
	}
}
