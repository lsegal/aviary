package projects

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

func TestKeepaliveDelayDoublesUpToItsCeiling(t *testing.T) {
	w := &Engine{closureInterval: 30 * time.Second}
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 15 * time.Minute, 15 * time.Minute}
	for stalled, delay := range want {
		if got := w.keepaliveDelay(stalled); got != delay {
			t.Errorf("keepaliveDelay(%d) = %s, want %s", stalled, got, delay)
		}
	}
}

// endingTurnRunner ends every turn at once, the way an agent stuck on a
// blocker does, and calls turn after each so a test can change what GitHub
// reports between turns.
type endingTurnRunner struct {
	sessions chan AgentSession
	turn     func(n int)
	mu       sync.Mutex
	calls    int
	started  []time.Time
}

func (r *endingTurnRunner) AgentName() string                { return "claude" }
func (r *endingTurnRunner) Run(context.Context, Issue) error { return nil }
func (r *endingTurnRunner) RunSessionWithOutput(ctx context.Context, issue Issue, session AgentSession, update func(AgentSession), _ io.Writer) error {
	return r.RunSession(ctx, issue, session, update)
}

func (r *endingTurnRunner) RunSession(ctx context.Context, _ Issue, session AgentSession, _ func(AgentSession)) error {
	r.mu.Lock()
	r.calls++
	n := r.calls
	r.started = append(r.started, time.Now())
	r.mu.Unlock()
	if r.turn != nil {
		r.turn(n)
	}
	select {
	case r.sessions <- session:
	case <-ctx.Done():
	}
	return nil
}

func (r *endingTurnRunner) turns() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func startKeepaliveEngine(t *testing.T, runner *endingTurnRunner, src *fakeClosureSource, closureInterval time.Duration) (*Engine, *snapshotReporter, *syncBuffer, string) {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "state.json")
	reporter := &snapshotReporter{}
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: statePath,
		Issues: src, Runner: runner, Active: &activeAgents{}, Out: logs, UI: reporter,
		closureInterval: closureInterval,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return w, reporter, logs, statePath
}

func TestEnginePausesAKeepaliveThatMakesNoProgress(t *testing.T) {
	src := &fakeClosureSource{fakeSource: &fakeSource{batches: [][]Issue{{{Number: 7}}}}}
	src.state = OriginatingWorkState{IssueState: "OPEN"}
	runner := &endingTurnRunner{sessions: make(chan AgentSession, 16)}
	w, reporter, logs, statePath := startKeepaliveEngine(t, runner, src, 10*time.Millisecond)

	record := waitForPaused(t, statePath, 7, core.PausedManual)
	if record.Reason != core.StalledPauseReason {
		t.Fatalf("paused record = %+v, want the stalled reason", record)
	}
	if got, want := runner.turns(), maxStalledKeepalives+1; got != want {
		t.Fatalf("agent ran %d turns before the pause, want %d", got, want)
	}
	job := waitForJobStatus(t, reporter, 7, "paused")
	if job.WaitReason != core.StalledPauseReason {
		t.Fatalf("paused job = %+v, want the stalled reason", job)
	}
	waitForLog(t, logs, "no new commits, pull requests, or comments; pausing it until it is retried")
	waitForLog(t, logs, "issue #7 paused: "+core.StalledPauseReason)
	// Each resume waited at least its backoff, which doubled with every turn
	// that made no progress.
	runner.mu.Lock()
	started := append([]time.Time(nil), runner.started...)
	runner.mu.Unlock()
	for turn := 1; turn < len(started); turn++ {
		if gap, delay := started[turn].Sub(started[turn-1]), w.keepaliveDelay(turn-1); gap < delay {
			t.Errorf("turn %d started %s after the previous one, want at least the %s backoff", turn+1, gap, delay)
		}
	}
	// The pause holds: nothing GitHub reports and no later poll dispatches
	// the job again until the owner retries it.
	time.Sleep(50 * time.Millisecond)
	if got := runner.turns(); got != maxStalledKeepalives+1 {
		t.Fatalf("paused job ran again: %d turns", got)
	}
}

func TestEngineKeepsResumingAKeepaliveThatMakesProgress(t *testing.T) {
	src := &fakeClosureSource{fakeSource: &fakeSource{batches: [][]Issue{{{Number: 7}}}}}
	src.state = OriginatingWorkState{IssueState: "OPEN"}
	// Every turn pushes a new head commit to the pull request, which is
	// progress even though the issue stays open.
	runner := &endingTurnRunner{sessions: make(chan AgentSession, 64), turn: func(n int) {
		src.mu.Lock()
		src.state = OriginatingWorkState{IssueState: "OPEN", PullRequests: []PullRequestWorkState{{Number: 8, State: "open", IsDraft: true, HeadSHA: strings.Repeat("a", n)}}}
		src.mu.Unlock()
	}}
	_, _, logs, statePath := startKeepaliveEngine(t, runner, src, time.Millisecond)

	for range 2 * maxStalledKeepalives {
		waitForSession(t, runner.sessions)
	}
	state, err := loadWorkState(statePath)
	if err != nil || state[7].Status != "active" {
		t.Fatalf("work making progress was not left active, state=%v err=%v", state[7], err)
	}
	if strings.Contains(logs.String(), "pausing it until it is retried") {
		t.Fatalf("work making progress was paused:\n%s", logs.String())
	}
}

func TestWorkProgressChangesWithCommitsPullRequestsAndComments(t *testing.T) {
	base := OriginatingWorkState{IssueState: "open", IssueBody: "body"}
	pull := PullRequestWorkState{Number: 8, State: "open", IsDraft: true, HeadSHA: "abc"}
	withPull := base
	withPull.PullRequests = []PullRequestWorkState{pull}
	for name, changed := range map[string]OriginatingWorkState{
		"opened pull request": withPull,
		"issue comment":       {IssueState: "open", IssueBody: "body", IssueComments: 1},
		"pushed commit":       {IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 8, State: "open", IsDraft: true, HeadSHA: "def"}}},
		"pull comment":        {IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 8, State: "open", IsDraft: true, HeadSHA: "abc", Comments: 1}}},
		"marked ready":        {IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 8, State: "open", HeadSHA: "abc"}}},
	} {
		previous := base
		if name != "opened pull request" && name != "issue comment" {
			previous = withPull
		}
		if core.WorkProgress(previous) == core.WorkProgress(changed) {
			t.Errorf("%s: fingerprint did not change", name)
		}
	}
	reordered := base
	reordered.PullRequests = []PullRequestWorkState{{Number: 9, State: "closed"}, pull}
	again := base
	again.PullRequests = []PullRequestWorkState{pull, {Number: 9, State: "closed"}}
	if core.WorkProgress(reordered) != core.WorkProgress(again) {
		t.Error("pull request order changed the fingerprint")
	}
}
