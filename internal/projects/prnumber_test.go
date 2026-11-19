package projects

import (
	"context"
	"testing"
	"time"
)

func TestGlorpTracksCurrentPullRequestInJobSnapshot(t *testing.T) {
	reporter := &snapshotReporter{}
	src := &fakeClosureSource{fakeSource: &fakeSource{batches: [][]Issue{{{Number: 7, Title: "bug"}}}}}
	runner := &fakeRunner{release: make(chan struct{})}
	w := &Glorp{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1,
		Issues: src, Runner: runner, UI: reporter, closureInterval: time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		close(runner.release)
		<-done
	}()

	latest := func() (JobSnapshot, bool) {
		reporter.mu.Lock()
		defer reporter.mu.Unlock()
		if len(reporter.snapshots) == 0 {
			return JobSnapshot{}, false
		}
		for _, job := range reporter.snapshots[len(reporter.snapshots)-1].Jobs {
			if job.Number == 7 {
				return job, true
			}
		}
		return JobSnapshot{}, false
	}
	waitFor := func(want int, wantRepo string) {
		t.Helper()
		deadline := time.Now().Add(webhookDispatchWait)
		for time.Now().Before(deadline) {
			if job, ok := latest(); ok && job.PullRequest == want && job.PullRequestRepository == wantRepo {
				return
			}
			time.Sleep(time.Millisecond)
		}
		job, _ := latest()
		t.Fatalf("job pull request = %q#%d, want %q#%d", job.PullRequestRepository, job.PullRequest, wantRepo, want)
	}

	// Running before its draft pull request exists, the job has none.
	deadline := time.Now().Add(webhookDispatchWait)
	for time.Now().Before(deadline) {
		if job, ok := latest(); ok && job.Status == "active" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if job, _ := latest(); job.PullRequest != 0 {
		t.Fatalf("job without a pull request shows #%d", job.PullRequest)
	}

	src.mu.Lock()
	src.state = OriginatingWorkState{IssueState: "OPEN", PullRequests: []PullRequestWorkState{{Repository: "o/r", Number: 9, State: "OPEN", IsDraft: true}}}
	src.mu.Unlock()
	waitFor(9, "")

	// A newer pull request supersedes the one the job showed.
	src.mu.Lock()
	src.state = OriginatingWorkState{IssueState: "OPEN", PullRequests: []PullRequestWorkState{{Repository: "o/r", Number: 9, State: "OPEN"}, {Repository: "fork/r", Number: 11, State: "OPEN", IsDraft: true}}}
	src.mu.Unlock()
	waitFor(11, "fork/r")
}
