package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// teardownFailingRunner reports a run that errored after its work landed, as
// a cleanup failing once the pull request merged does.
type teardownFailingRunner struct{ runs chan int }

func (r teardownFailingRunner) Run(_ context.Context, issue Issue) error {
	r.runs <- issue.Number
	return errors.New("removing TempDir: directory not empty")
}

func mergedState() OriginatingWorkState {
	return OriginatingWorkState{IssueState: "closed", PullRequests: []PullRequestWorkState{{Number: 15, State: "closed", Merged: true}}}
}

func TestMergedWork(t *testing.T) {
	for _, test := range []struct {
		name   string
		state  OriginatingWorkState
		merged int
		ok     bool
	}{
		{"merged and closed", mergedState(), 15, true},
		{"merged with the issue still open", OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 15, State: "closed", Merged: true}}}, 0, false},
		{"closed unmerged", OriginatingWorkState{IssueState: "closed", PullRequests: []PullRequestWorkState{{Number: 15, State: "closed"}}}, 0, false},
		{"no pull request", OriginatingWorkState{IssueState: "closed"}, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			merged, ok := mergedWork(test.state)
			if merged != test.merged || ok != test.ok {
				t.Fatalf("mergedWork = %d, %v; want %d, %v", merged, ok, test.merged, test.ok)
			}
		})
	}
}

// A run that errors after its pull request merged and closed the issue is
// done, not failed: the merge outranks the run's exit (issue #685).
func TestEngineCompletesAFailedRunWhosePullRequestMerged(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	src := pausingSource([]Issue{{Number: 7, Repository: "o/r"}})
	src.states[7] = mergedState()
	runner := teardownFailingRunner{runs: make(chan int, 4)}
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: statePath,
		Issues: src, Runner: runner, Out: logs, closureInterval: time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	waitForWorkStatus(t, statePath, 7, "completed")
	if !strings.Contains(logs.String(), "issue #7 run ended with an error, but pull request #15 merged and closed the issue; completing it") {
		t.Fatalf("reconciliation was not logged:\n%s", logs)
	}
	if strings.Contains(logs.String(), "issue #7 failed:") {
		t.Fatalf("a merged run was reported failed:\n%s", logs)
	}
}

// Failed work whose issue the poll no longer lists is read back, so a pull
// request merged after the run failed completes it on the next poll.
func TestEngineCompletesFailedWorkWhenItsPullRequestMergesLater(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{"o/r#7": {"status": "failed"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	src := &countingPausingSource{fakePausingSource: pausingSource([]Issue{})}
	src.states[7] = OriginatingWorkState{IssueState: "closed", PullRequests: []PullRequestWorkState{{Number: 15, State: "open"}}}
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: 5 * time.Millisecond, Concurrency: 1, StatePath: statePath,
		Issues: src, Runner: teardownFailingRunner{runs: make(chan int, 4)}, Out: logs, closureInterval: time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	waitForReads(t, src, 7, 3)
	if state, err := loadWorkState(statePath); err != nil || state[7].Status != "failed" {
		t.Fatalf("failed work with its pull request still open changed: %v %v", state, err)
	}

	src.mu.Lock()
	src.states[7] = mergedState()
	src.mu.Unlock()
	waitForWorkStatus(t, statePath, 7, "completed")
	// The completed status is saved before the reconciliation is logged, so
	// wait on the log line itself rather than reading it once.
	waitForLog(t, logs, "issue #7 failed run reconciled: pull request #15 merged and closed the issue")
}

// A pull request closed unmerged leaves the failure standing, and once
// nothing is left open to merge the work is not read back again.
func TestEngineKeepsFailedWorkWhosePullRequestClosedUnmerged(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(statePath, []byte(`{"o/r#7": {"status": "failed"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	src := &countingPausingSource{fakePausingSource: pausingSource([]Issue{})}
	src.states[7] = OriginatingWorkState{IssueState: "closed", PullRequests: []PullRequestWorkState{{Number: 15, State: "closed"}}}
	logs := &syncBuffer{}
	w := &Engine{
		Repo: "o/r", Interval: 5 * time.Millisecond, Concurrency: 1, StatePath: statePath,
		Issues: src, Runner: teardownFailingRunner{runs: make(chan int, 4)}, Out: logs, closureInterval: time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	waitForPolls(t, src.fakeSource, 5)
	if state, err := loadWorkState(statePath); err != nil || state[7].Status != "failed" {
		t.Fatalf("failed work with its pull request closed unmerged changed: %v %v", state, err)
	}
	if reads := src.reads(7); reads != 1 {
		t.Fatalf("settled failed work was read back %d times, want once", reads)
	}
}

type countingPausingSource struct {
	*fakePausingSource
	counts map[int]int
}

func (f *countingPausingSource) OriginatingWorkState(ctx context.Context, repo string, number int) (OriginatingWorkState, error) {
	f.mu.Lock()
	if f.counts == nil {
		f.counts = make(map[int]int)
	}
	f.counts[number]++
	f.mu.Unlock()
	return f.fakePausingSource.OriginatingWorkState(ctx, repo, number)
}

func (f *countingPausingSource) reads(number int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[number]
}

func waitForReads(t *testing.T, src *countingPausingSource, number, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for src.reads(number) < want {
		if time.Now().After(deadline) {
			t.Fatalf("issue #%d was read back %d times, want at least %d", number, src.reads(number), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func waitForPolls(t *testing.T, src *fakeSource, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for listCalls(src) < want {
		if time.Now().After(deadline) {
			t.Fatalf("issues were listed %d times, want at least %d", listCalls(src), want)
		}
		time.Sleep(time.Millisecond)
	}
}
