package projects

import (
	"context"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/saferestart"
)

func TestManagerSafeRestartParticipant(t *testing.T) {
	m := newTestManager(t)
	var _ saferestart.Participant = m
	m.Snapshot(core.Snapshot{Jobs: []core.JobSnapshot{
		{Target: "o/r", Number: 1, Title: "running", Status: "active"},
		{Target: "o/projects/3", Repository: "o/app", Number: 4, Title: "board item", Status: "active"},
		{Target: "o/r", Number: 2, Title: "paused", Status: "paused"},
		{Target: "o/r", Number: 3, Title: "waiting", Status: "waiting"},
		{Target: "o/r", Number: 5, Title: "queued", Status: "queued"},
	}})
	want := []saferestart.Job{
		{Source: "projects", ID: "o/r#1", Title: "running"},
		{Source: "projects", ID: "o/app#4", Title: "board item"},
	}
	if got := m.RunningJobs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("RunningJobs() = %+v, want %+v", got, want)
	}
	m.HoldNewJobs(true)
	if err := m.Checkpoint(t.Context(), saferestart.Marker{}); err != nil {
		t.Fatal(err)
	}
	if !m.heldForRestart() {
		t.Fatal("the hold was not recorded")
	}
	m.HoldNewJobs(false)
	if m.heldForRestart() {
		t.Fatal("the hold was not released")
	}
}

func TestManagerRestartNoticesAreTakenOnce(t *testing.T) {
	m := newTestManager(t)
	marker := saferestart.Marker{Reason: "upgrade", FromVersion: "v1.0.0", ToVersion: "v1.1.0", Jobs: []saferestart.Job{
		{Source: "projects", ID: "o/app#4"},
		{Source: "tasks", ID: "o/r#9"},
	}}
	m.ResumeAfterRestart(marker)

	board := Issue{Target: "o/projects/3", Repository: "o/app", Number: 4}
	if got := m.restartNotice(board); got != marker.Message() {
		t.Fatalf("restartNotice() = %q, want %q", got, marker.Message())
	}
	if got := m.restartNotice(board); got != "" {
		t.Fatalf("second restartNotice() = %q, want it taken", got)
	}
	if got := m.restartNotice(Issue{Target: "o/r", Number: 9}); got != "" {
		t.Fatalf("a task job's notice reached Projects: %q", got)
	}
}

func TestEngineResumesSafeRestartJobInItsSessionWithTheNotice(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	persisted := workState{Status: "active", SessionID: "session-7", Agent: "codex", CheckoutDirectory: dir}
	if err := saveWorkState(statePath, map[int]workState{7: persisted}); err != nil {
		t.Fatal(err)
	}
	m := newTestManager(t)
	marker := saferestart.Marker{Reason: "restart", Jobs: []saferestart.Job{{Source: "projects", ID: "o/r#7"}}}
	m.ResumeAfterRestart(marker)
	runner := &fakeSessionRunner{agent: "codex", sessions: make(chan AgentSession, 1)}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: statePath,
		Issues: &fakeSource{batches: [][]Issue{{{Number: 7}}}}, Runner: runner,
		RestartNotice: m.restartNotice,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	select {
	case got := <-runner.sessions:
		if !got.Resume || got.ID != persisted.SessionID || got.Agent != persisted.Agent {
			t.Fatalf("resumed session = %#v, want persisted %#v", got, persisted)
		}
		if got.Update != marker.Message() {
			t.Fatalf("resumed session update = %q, want the restart notice %q", got.Update, marker.Message())
		}
	case <-time.After(time.Second):
		t.Fatal("the checkpointed job was not resumed")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestEngineHeldForRestartDispatchesNothingUntilReleased(t *testing.T) {
	var held atomic.Bool
	held.Store(true)
	source := &fakeSource{batches: [][]Issue{{{Number: 7}}}}
	runner := &fakeSessionRunner{agent: "codex", sessions: make(chan AgentSession, 1)}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(t.TempDir(), "state.json"),
		Issues: source, Runner: runner, Held: held.Load,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	select {
	case got := <-runner.sessions:
		t.Fatalf("a held engine dispatched %#v", got)
	case <-time.After(200 * time.Millisecond):
	}
	source.mu.Lock()
	calls := source.calls
	source.mu.Unlock()
	if calls != 0 {
		t.Fatalf("a held engine listed issues %d time(s)", calls)
	}

	held.Store(false)
	w.nudgePoll()
	select {
	case <-runner.sessions:
	case <-time.After(time.Second):
		t.Fatal("releasing the hold did not dispatch the waiting issue")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
