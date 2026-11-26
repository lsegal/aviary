package projects

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// runFailingEngine runs an engine over src whose every run fails as a killed
// agent does, reporting its snapshots.
func runFailingEngine(t *testing.T, src *fakePausingSource, interval time.Duration) *snapshotReporter {
	t.Helper()
	reporter := &snapshotReporter{}
	w := &Engine{
		Repo: "o/r", Interval: interval, Concurrency: 1, StatePath: filepath.Join(t.TempDir(), "state.json"),
		Issues: src, Runner: killedRunner{}, UI: reporter, Out: &syncBuffer{}, closureInterval: time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return reporter
}

// waitForReportedJob waits until the latest snapshot's job number satisfies ok.
func waitForReportedJob(t *testing.T, reporter *snapshotReporter, number int, ok func(JobSnapshot) bool) JobSnapshot {
	t.Helper()
	deadline := time.Now().Add(runLoopWait)
	for time.Now().Before(deadline) {
		if job, found := reporter.job(number); found && ok(job) {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	job, _ := reporter.job(number)
	t.Fatalf("job #%d never reached the expected state; last seen %+v", number, job)
	return JobSnapshot{}
}

// A run that errors while its issue is still listed is dispatched again by
// the next poll, so the board shows it waiting with the reason, never Failed
// (issue #1424).
func TestEngineShowsAFailedRunItDispatchesAgainAsWaiting(t *testing.T) {
	reporter := runFailingEngine(t, pausingSource([]Issue{{Number: 9, Repository: "o/r"}}), time.Hour)

	job := waitForReportedJob(t, reporter, 9, func(job JobSnapshot) bool { return job.Status == "failed" })
	if !job.Retrying || job.Failed() || job.BoardState() != core.BoardPaused {
		t.Fatalf("job = %+v (board state %s), want it waiting to be dispatched again", job, job.BoardState())
	}
	if !strings.Contains(job.WaitReason, "signal: killed") || !strings.Contains(job.WaitReason, "dispatches it again") {
		t.Fatalf("wait reason = %q, want the error and what happens next", job.WaitReason)
	}
}

// Once a poll no longer lists failed work, nothing dispatches it again, so
// its failure is terminal and the board shows it Failed.
func TestEngineFailsARunOnceNothingDispatchesItAgain(t *testing.T) {
	reporter := runFailingEngine(t, pausingSource([]Issue{{Number: 9, Repository: "o/r"}}, []Issue{}), 5*time.Millisecond)

	job := waitForReportedJob(t, reporter, 9, func(job JobSnapshot) bool { return job.Status == "failed" && !job.Retrying })
	if !job.Failed() || job.BoardState() != core.BoardFailed || job.WaitReason != "" {
		t.Fatalf("job = %+v (board state %s), want it Failed", job, job.BoardState())
	}
}

// killedRunner reports every run as one whose agent was killed.
type killedRunner struct{}

func (killedRunner) Run(context.Context, Issue) error {
	return errorString("agent failed: signal: killed")
}

type errorString string

func (e errorString) Error() string { return string(e) }
