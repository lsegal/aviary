package projects

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/scheduler"
)

// manualSource is a GitHub source that counts every discovery read, the
// issue searches a manual project must never make, and answers reads of
// single issues, the only reads a manual project makes besides following
// its jobs.
type manualSource struct {
	*fakeClosureSource
	mu       sync.Mutex
	hydrated []int
	state    string
}

func (s *manualSource) HydrateIssue(_ context.Context, _ string, issue *Issue) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hydrated = append(s.hydrated, issue.Number)
	issue.Title, issue.State = "Requested", s.state
	return nil
}

func (s *manualSource) hydrateCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.hydrated)
}

func (s *manualSource) setState(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
}

func (s *manualSource) listCalls() int {
	s.fakeSource.mu.Lock()
	defer s.fakeSource.mu.Unlock()
	return s.calls
}

func newManualSource() *manualSource {
	// Issue #99 matches the filter: a watched project would pick it up.
	src := &manualSource{fakeClosureSource: &fakeClosureSource{fakeSource: &fakeSource{batches: [][]Issue{{{Number: 99}}}}}, state: "open"}
	return src
}

func runEngine(t *testing.T, w *Engine) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	var runErr error
	go func() {
		runErr = w.Run(ctx)
		close(stopped)
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})
	readyCtx, readyCancel := context.WithTimeout(context.Background(), runLoopWait)
	defer readyCancel()
	if err := w.checkReady(readyCtx); err != nil {
		select {
		case <-stopped:
			t.Fatalf("run stopped before it was ready: %v", runErr)
		default:
		}
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(runLoopWait)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// A manual project (#1307) makes no issue searches, board reads or
// Discussion listings, however often it polls, and dispatches nothing on its
// own.
func TestManualProjectMakesNoDiscoveryCalls(t *testing.T) {
	src := newManualSource()
	discussions := &fakeDiscussionSource{batches: [][]Discussion{{{Number: 3}}}}
	board := &fakeProjectState{}
	runner := &fakeRunner{dispatched: make(chan int, 4)}
	w := &Engine{
		Repo: "o/r", Targets: []string{"o/r", "https://github.com/o/r/discussions", "https://github.com/users/o/projects/1"},
		Manual:   core.NewManualTargets([]string{"o/r", "https://github.com/o/r/discussions", "https://github.com/users/o/projects/1"}),
		Interval: time.Millisecond, Concurrency: 1, StatePath: filepath.Join(t.TempDir(), "state.json"),
		Issues: src, Discussions: discussions, Projects: board, UseWebhooks: true, Events: make(chan WebhookEvent),
		fallbackInterval: time.Millisecond, probeInterval: time.Millisecond, Runner: runner, Out: &syncBuffer{}, UI: &snapshotReporter{},
	}
	runEngine(t, w)
	waitFor(t, "a poll", func() bool {
		snapshot := w.published.Load()
		return snapshot != nil && !snapshot.LastPoll.IsZero()
	})
	time.Sleep(50 * time.Millisecond)
	if calls := src.listCalls(); calls != 0 {
		t.Fatalf("a manual project searched for issues %d time(s)", calls)
	}
	discussions.mu.Lock()
	listed := discussions.calls
	discussions.mu.Unlock()
	if listed != 0 {
		t.Fatalf("a manual Discussions board was listed %d time(s)", listed)
	}
	if probes := board.callCount(); probes != 0 {
		t.Fatalf("a manual board was read %d time(s)", probes)
	}
	select {
	case n := <-runner.dispatched:
		t.Fatalf("a manual project dispatched issue #%d on its own", n)
	default:
	}
	if counts := w.published.Load().IssueCounts; len(counts) != 0 {
		t.Fatalf("a manual project reported issue counts %v", counts)
	}
}

// An issue asked to be fixed on a manual project runs, is followed to its
// merge, and is then dropped from the requests, without a single search.
func TestManualProjectRunsRequestedFixToCompletion(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	src := newManualSource()
	src.fakeClosureSource.state = OriginatingWorkState{IssueState: "closed", PullRequests: []PullRequestWorkState{{Number: 8, State: "merged", Merged: true}}}
	runner := &finishingSessionRunner{agent: "claude", sessions: make(chan AgentSession, 4), finish: 1}
	w := &Engine{
		Repo: "o/r", Manual: core.NewManualTargets([]string{"o/r"}), Interval: time.Millisecond, Concurrency: 1,
		StatePath: statePath, Issues: src, Runner: runner, Out: &syncBuffer{}, closureInterval: time.Millisecond,
	}
	runEngine(t, w)
	if err := w.RequestFix("o/r", "", 7); err != nil {
		t.Fatal(err)
	}
	waitForSession(t, runner.sessions)
	waitFor(t, "the requested fix to complete", func() bool {
		state, err := loadWorkState(statePath)
		return err == nil && state[7].Status == "completed"
	})
	// The issue is still open, so the completed job stays tracked until it
	// closes (issue #1431).
	time.Sleep(20 * time.Millisecond)
	if requests := w.fixRequests().forTarget("o/r"); len(requests) != 1 {
		t.Fatalf("a completed job with an open issue stopped being tracked: %+v", requests)
	}
	src.setState("closed")
	waitFor(t, "the closed issue's request to be dropped", func() bool { return len(w.fixRequests().forTarget("o/r")) == 0 })
	if calls := src.listCalls(); calls != 0 {
		t.Fatalf("a manual project searched for issues %d time(s)", calls)
	}
	select {
	case extra := <-runner.sessions:
		t.Fatalf("a completed requested fix relaunched its agent: %+v", extra)
	default:
	}
}

// A requested issue that is already closed is dropped without a run.
func TestManualProjectDropsClosedRequest(t *testing.T) {
	src := newManualSource()
	src.state = "closed"
	runner := &fakeRunner{dispatched: make(chan int, 1)}
	w := &Engine{
		Repo: "o/r", Manual: core.NewManualTargets([]string{"o/r"}), Interval: time.Millisecond, Concurrency: 1,
		StatePath: filepath.Join(t.TempDir(), "state.json"), Issues: src, Runner: runner, Out: &syncBuffer{},
	}
	runEngine(t, w)
	if err := w.RequestFix("o/r", "", 7); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the closed request to be dropped", func() bool { return len(w.fixRequests().forTarget("o/r")) == 0 })
	select {
	case n := <-runner.dispatched:
		t.Fatalf("closed issue #%d was dispatched", n)
	default:
	}
}

// Switching a project to manual stops its searches at once and keeps its
// running job; switching back searches for issues right away, all without a
// restart.
func TestManagerSwitchesProjectModeLive(t *testing.T) {
	src := newManualSource()
	runner := &fakeRunner{dispatched: make(chan int, 4), allow: make(chan struct{})}
	manual := core.NewManualTargets(nil)
	w := &Engine{
		Repo: "o/r", Manual: manual, Interval: time.Hour, Concurrency: 1,
		StatePath: filepath.Join(t.TempDir(), "state.json"), Issues: src, Runner: runner, Out: &syncBuffer{},
	}
	runEngine(t, w)
	if n := <-runner.dispatched; n != 99 {
		t.Fatalf("dispatched #%d, want #99", n)
	}
	m := newTestManager(t)
	run := &engineRun{engine: w, filters: core.NewFilterOverrides(nil), manual: manual}
	settings := engineSettings{Concurrency: 1}

	settings.ManualTargets = []string{"o/r"}
	if err := m.applyLive(run, settings); err != nil {
		t.Fatal(err)
	}
	before := src.listCalls()
	w.nudgePoll()
	time.Sleep(50 * time.Millisecond)
	if after := src.listCalls(); after != before {
		t.Fatalf("a project switched to manual searched for issues: %d search(es) before, %d after", before, after)
	}
	runner.mu.Lock()
	active := runner.active
	runner.mu.Unlock()
	if active != 1 {
		t.Fatalf("switching to manual stopped the running job: %d active", active)
	}

	settings.ManualTargets = nil
	if err := m.applyLive(run, settings); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a search after switching back to watch", func() bool { return src.listCalls() > before })
	close(runner.allow)
}

// A scheduled project task still runs on a manual project.
func TestManualProjectStillRunsProjectTasks(t *testing.T) {
	runner := &scheduledRunner{started: make(chan Issue, 1)}
	src := newManualSource()
	w := &Engine{
		Repo: "o/r", Manual: core.NewManualTargets([]string{"o/r"}), Interval: time.Hour, Concurrency: 1,
		Issues: src, Runner: runner, UI: &snapshotReporter{},
	}
	runEngine(t, w)
	fire := time.Date(2026, 11, 25, 9, 0, 0, 0, time.UTC)
	if _, err := w.runProjectTask(context.Background(), scheduler.ProjectRun{TaskID: "bot/triage", Name: "triage", Project: "o/r", Prompt: "Triage", Fire: fire}, "o/r", AgentSession{}); err != nil {
		t.Fatal(err)
	}
	if issue := <-runner.started; issue.ScheduleID != "bot/triage" {
		t.Fatalf("runner got %+v", issue)
	}
	if calls := src.listCalls(); calls != 0 {
		t.Fatalf("a manual project searched for issues %d time(s)", calls)
	}
}

// gh-fix on a manual project requests the issue instead of waiting for a
// search to find it.
func TestManagerQueueFixesRequestsOnManualProject(t *testing.T) {
	w := &Engine{Repo: "o/r", Targets: []string{"o/r"}, Manual: core.NewManualTargets([]string{"o/r"}), StatePath: filepath.Join(t.TempDir(), "state.json")}
	m := newTestManager(t)
	m.running = &engineRun{engine: w}
	result, err := m.QueueJobs(context.Background(), "o/r", "", SkillFix, []int{7}, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcomes := result["results"].([]QueueOutcome)
	if len(outcomes) != 1 || !outcomes[0].Queued || outcomes[0].Key != "o/r#7" {
		t.Fatalf("outcomes = %+v", outcomes)
	}
	requests := w.fixRequests().forTarget("o/r")
	if len(requests) != 1 || requests[0].Number != 7 || requests[0].Repository != "o/r" {
		t.Fatalf("requests = %+v", requests)
	}
	// Fix requests survive a restart.
	reloaded := &fixRequests{path: w.fixRequests().path}
	if got := reloaded.forTarget("o/r"); len(got) != 1 || got[0].Number != 7 {
		t.Fatalf("reloaded requests = %+v", got)
	}
}
