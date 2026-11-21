package projects

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// pausableRunner runs each issue until its context is cancelled, then waits
// for exit to close, like an agent still pushing when it is stopped. It
// reports its checkout so a pause has work to checkpoint.
type pausableRunner struct {
	checkout string
	sessions chan Issue
	exit     chan struct{}
}

func (r *pausableRunner) AgentName() string                { return "codex" }
func (r *pausableRunner) Run(context.Context, Issue) error { return nil }
func (r *pausableRunner) RunSession(ctx context.Context, issue Issue, _ AgentSession, update func(AgentSession)) error {
	update(AgentSession{ID: "session-" + issue.Title, CheckoutDirectory: r.checkout})
	r.sessions <- issue
	<-ctx.Done()
	<-r.exit
	return ctx.Err()
}

// recordingGit records the checkouts a checkpoint ran git in and fails every
// command, so the checkpoint reports a failure without touching anything.
type recordingGit struct {
	mu   sync.Mutex
	dirs []string
}

func (g *recordingGit) run(_ context.Context, dir string, _ ...string) (string, error) {
	g.mu.Lock()
	g.dirs = append(g.dirs, dir)
	g.mu.Unlock()
	return "", os.ErrNotExist
}

func waitForLog(t *testing.T, logs *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("log never said %q:\n%s", want, logs)
		}
		time.Sleep(time.Millisecond)
	}
}

// listCalls reports how many times src listed issues.
func listCalls(src *fakeSource) int {
	src.mu.Lock()
	defer src.mu.Unlock()
	return src.calls
}

// pollTwice runs two more polls and waits for both to list issues.
func pollTwice(t *testing.T, w *Engine, src *fakeSource, logs *syncBuffer) {
	t.Helper()
	for range 2 {
		before := listCalls(src)
		w.nudgePoll()
		deadline := time.Now().Add(5 * time.Second)
		for listCalls(src) <= before {
			if time.Now().After(deadline) {
				t.Fatalf("no poll ran:\n%s", logs)
			}
			w.nudgePoll()
			time.Sleep(time.Millisecond)
		}
	}
}

// Pausing an active job stops its run the way Stop does, replying only once
// the agent has exited and its work was checkpointed, and then holds it as a
// manual pause that later polls never dispatch, even once its pull request
// merges.
func TestEngineWebUIPausesActiveWork(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	checkout := t.TempDir()
	src := pausingSource([]Issue{{Number: 7, Title: "7", Repository: "o/r"}})
	src.states[7] = OriginatingWorkState{IssueState: "open"}
	runner := &pausableRunner{checkout: checkout, sessions: make(chan Issue, 4), exit: make(chan struct{})}
	git := &recordingGit{}
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: statePath,
		Issues: src, Runner: runner, Out: logs, closureInterval: time.Hour, checkpointGit: git.run,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()
	select {
	case <-runner.sessions:
	case <-time.After(5 * time.Second):
		t.Fatal("issue was not dispatched")
	}

	paused := make(chan error, 1)
	go func() { paused <- w.handleJobAction(ctx, jobAction{Action: "pause", Target: "o/r", Number: 7}) }()
	select {
	case err := <-paused:
		close(runner.exit)
		t.Fatalf("pause returned before the agent exited: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(runner.exit)
	select {
	case err := <-paused:
		if err != nil {
			t.Fatalf("pause failed: %v\n%s", err, logs)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pause did not return after the agent exited")
	}
	record := waitForPaused(t, statePath, 7, core.PausedManual)
	if record.Kind != core.PausedManual {
		t.Fatalf("paused record = %+v", record)
	}
	state, err := loadWorkState(statePath)
	if err != nil || state[7].SessionID != "session-7" {
		t.Fatalf("the paused job lost its session: %+v, %v", state[7], err)
	}
	git.mu.Lock()
	checkpointed := len(git.dirs) > 0 && git.dirs[0] == checkout
	git.mu.Unlock()
	if !checkpointed {
		t.Fatalf("pause did not checkpoint the run's checkout %s: %v", checkout, git.dirs)
	}
	waitForLog(t, logs, "issue #7 paused from web UI; its agent has exited; checkpoint")

	// Nothing GitHub reports ends a manual pause, not even a merged pull
	// request.
	src.mu.Lock()
	src.states[7] = OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 15, State: "closed", Merged: true}}}
	src.mu.Unlock()
	pollTwice(t, w, src.fakeSource, logs)
	waitForLog(t, logs, "issue #7 paused: paused by owner")
	select {
	case extra := <-runner.sessions:
		t.Fatalf("the paused job was dispatched again: %+v", extra)
	default:
	}
	waitForPaused(t, statePath, 7, core.PausedManual)
}

// Pausing a queued job dispatches and cancels nothing: it only leaves the
// queue, and later polls pass it over while other work runs. Only work this
// instance already owns waits in the queue for a slot (issue #652), so #8 is
// restored as active work from an earlier run.
func TestEngineWebUIPausesQueuedWork(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{"o/r#8": {"status": "active", "sessionId": "session-8", "agent": "codex"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	src := &fakeSource{batches: [][]Issue{{{Number: 7, Title: "7"}, {Number: 8, Title: "8"}}}}
	runner := &pausableRunner{sessions: make(chan Issue, 4), exit: make(chan struct{})}
	close(runner.exit)
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: statePath,
		Issues: src, Runner: runner, Out: logs,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()
	if issue := <-runner.sessions; issue.Number != 7 {
		t.Fatalf("dispatched #%d first, want #7", issue.Number)
	}
	waitForLog(t, logs, "issue #8 queued")

	if err := w.handleJobAction(ctx, jobAction{Action: "pause", Target: "o/r", Number: 8}); err != nil {
		t.Fatalf("pausing the queued job failed: %v\n%s", err, logs)
	}
	waitForPaused(t, statePath, 8, core.PausedManual)
	waitForLog(t, logs, "issue #8 pause requested from web UI; holding it out of the queue")

	// Stopping #7 frees its slot; the poll waiting on it dispatches nothing
	// for #8, and the next polls rerun #7 but still pass #8 over.
	if err := w.handleJobAction(ctx, jobAction{Action: "stop", Target: "o/r", Number: 7}); err != nil {
		t.Fatalf("stopping #7 failed: %v\n%s", err, logs)
	}
	pollTwice(t, w, src, logs)
	select {
	case issue := <-runner.sessions:
		if issue.Number != 7 {
			t.Fatalf("dispatched #%d, want only #7 to run again", issue.Number)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("other work was not dispatched while #8 stayed paused:\n%s", logs)
	}
	select {
	case issue := <-runner.sessions:
		t.Fatalf("dispatched #%d while #8 was paused", issue.Number)
	default:
	}
	waitForPaused(t, statePath, 8, core.PausedManual)
}

// A pause on a job in any other state is refused and changes nothing
// .
func TestEngineWebUIRefusesPauseOfPausedWork(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{"o/r#7": {"status": "paused", "sessionId": "session-7", "agent": "claude", "paused": {"kind": "blocked", "waitingOn": [12]}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	src := pausingSource(blockedOn12("open"))
	src.states[12] = OriginatingWorkState{IssueState: "open"}
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: statePath,
		Issues: src, Runner: &finishingSessionRunner{agent: "claude", sessions: make(chan AgentSession, 4)}, Out: logs,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()
	waitForLog(t, logs, "issue #7 paused: ")
	err := w.handleJobAction(ctx, jobAction{Action: "pause", Target: "o/r", Number: 7})
	if err == nil || !strings.Contains(err.Error(), "cannot be paused while paused") {
		t.Fatalf("pause error = %v, want it refused", err)
	}
	waitForPaused(t, statePath, 7, core.PausedBlocked)
}

// manuallyPausedUnlisted saves issue #7 as paused by its owner, with a session
// to resume, while the poll lists no issues at all.
func manuallyPausedUnlisted(t *testing.T, statePath string) *hydratingPausingSource {
	t.Helper()
	saved := `{"o/r#7": {"status": "paused", "sessionId": "session-7", "agent": "claude", "paused": {"kind": "manual"}}}`
	if err := os.WriteFile(statePath, []byte(saved), 0600); err != nil {
		t.Fatal(err)
	}
	src := &hydratingPausingSource{fakePausingSource: pausingSource([]Issue{}), issues: map[int]Issue{}, hydrateCh: make(chan struct{}, 1)}
	src.states[7] = OriginatingWorkState{IssueState: "open"}
	src.setIssue(Issue{Number: 7, State: "open"})
	return src
}

// A manually paused job whose issue the poll no longer lists is finished once
// its issue closes, instead of staying paused with nothing Retry could requeue.
// The closed issue finishes the job; it never runs it again.
func TestEngineFinishesAManualPauseWhoseIssueClosed(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	src := manuallyPausedUnlisted(t, statePath)
	runner := &finishingSessionRunner{agent: "claude", sessions: make(chan AgentSession, 4), finish: 1}
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: statePath,
		Issues: src.fakePausingSource, Runner: runner, Out: logs, closureInterval: time.Hour,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	// A merged pull request on an open issue still leaves it paused.
	src.mu.Lock()
	src.states[7] = OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 15, State: "closed", Merged: true}}}
	src.mu.Unlock()
	pollTwice(t, w, src.fakeSource, logs)
	waitForPaused(t, statePath, 7, core.PausedManual)

	src.mu.Lock()
	src.states[7] = OriginatingWorkState{IssueState: "closed"}
	src.mu.Unlock()
	w.nudgePoll()
	waitForWorkStatus(t, statePath, 7, "completed")
	waitForLog(t, logs, "issue #7 unpaused: issue #7 was closed; its paused work is finished")
	pollTwice(t, w, src.fakeSource, logs)
	select {
	case extra := <-runner.sessions:
		t.Fatalf("the closed issue resumed the manual pause: %+v", extra)
	default:
	}
}

// The paused watch reads a manual pause's issue back like any other paused
// work, so a closed issue finishes it even without its own state being read
// .
func TestEnginePausedWatchFinishesAManualPauseWhoseIssueClosed(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	src := manuallyPausedUnlisted(t, statePath)
	runner := &finishingSessionRunner{agent: "claude", sessions: make(chan AgentSession, 4), finish: 1}
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: 5 * time.Millisecond, Concurrency: 1, StatePath: statePath,
		Issues: src, Runner: runner, Out: logs, closureInterval: time.Hour, pausedInterval: 20 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	select {
	case <-src.hydrateCh:
	case <-time.After(5 * time.Second):
		t.Fatal("the manual pause's issue was never read back")
	}
	waitForLog(t, logs, "issue #7 paused: paused by owner")
	waitForPaused(t, statePath, 7, core.PausedManual)

	// Only the issue read back says it closed; its own state still reads
	// open, so the watch is what finishes it.
	src.setIssue(Issue{Number: 7, State: "closed"})
	waitForWorkStatus(t, statePath, 7, "completed")
	waitForLog(t, logs, "issue #7 unpaused: issue #7 was closed; its paused work is finished")
	select {
	case extra := <-runner.sessions:
		t.Fatalf("the closed issue resumed the manual pause: %+v", extra)
	default:
	}
}

// Stop clears a manually paused job whose open issue the poll no longer lists,
// which Retry cannot requeue: its record and card are dropped and its claim
// is released, so the issue is dispatched afresh once it is listed again
// .
func TestEngineWebUIStopClearsAnUnlistedManualPause(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	src := manuallyPausedUnlisted(t, statePath)
	runner := &finishingSessionRunner{agent: "claude", sessions: make(chan AgentSession, 4), finish: 1}
	comments := newFakeCommentClient()
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: statePath,
		Issues: src.fakePausingSource, Runner: runner, Comments: comments, Out: logs, closureInterval: time.Hour,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()
	pollTwice(t, w, src.fakeSource, logs)
	waitForPaused(t, statePath, 7, core.PausedManual)

	if err := w.handleJobAction(ctx, jobAction{Action: "stop", Target: "o/r", Number: 7}); err != nil {
		t.Fatalf("stop error = %v, want the paused job cleared", err)
	}
	state, err := loadWorkState(statePath)
	if _, ok := state[7]; err != nil || ok {
		t.Fatalf("state after stop = %v (err %v), want issue #7 dropped", state, err)
	}
	if err := w.handleJobAction(ctx, jobAction{Action: "stop", Target: "o/r", Number: 7}); err == nil || !strings.Contains(err.Error(), "job not found") {
		t.Fatalf("second stop error = %v, want the job card gone", err)
	}
	posted, _ := comments.ListComments(ctx, "o/r", 7)
	if len(posted) == 0 || !strings.Contains(posted[len(posted)-1].Body, releaseClaimBody) {
		t.Fatalf("comments on #7 = %+v, want the claim released", posted)
	}
	pollTwice(t, w, src.fakeSource, logs)
	select {
	case extra := <-runner.sessions:
		t.Fatalf("the cleared job ran while its issue was unlisted: %+v", extra)
	default:
	}

	src.mu.Lock()
	src.batches = [][]Issue{{{Number: 7, Repository: "o/r"}}}
	src.mu.Unlock()
	w.nudgePoll()
	select {
	case session := <-runner.sessions:
		if session.Resume {
			t.Fatalf("the relisted issue resumed its cleared session: %+v", session)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("the relisted issue was not dispatched:\n%s", logs)
	}
}
