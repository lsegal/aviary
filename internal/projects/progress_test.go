package projects

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestOriginatingWorkStateCountsPullRequestChecklist(t *testing.T) {
	responses := [][]byte{
		[]byte(`{"state":"OPEN"}`),
		[]byte(`[{"event":"cross-referenced","source":{"issue":{"number":9,"body":"Closes #7","pull_request":{"merged_at":null}}}}]`),
		[]byte(`{"state":"open","draft":true,"body":"## Plan\n- [x] parse\n- [ ] render\n- [ ] test\n\nCloses #7"}`),
	}
	calls := 0
	gh := GHCLI{runCommand: func(_ context.Context, _ ...string) ([]byte, error) {
		calls++
		return responses[calls-1], nil
	}}
	state, err := gh.OriginatingWorkState(context.Background(), "owner/repo", 7)
	if err != nil || len(state.PullRequests) != 1 {
		t.Fatalf("OriginatingWorkState() = (%#v, %v)", state, err)
	}
	if got := state.PullRequests[0]; got.TasksDone != 1 || got.TasksTotal != 3 {
		t.Fatalf("pull request checklist = %d/%d, want 1/3", got.TasksDone, got.TasksTotal)
	}
}

func TestEngineUpdatesJobProgressAsChecklistChanges(t *testing.T) {
	reporter := &snapshotReporter{}
	src := &fakeClosureSource{fakeSource: &fakeSource{batches: [][]Issue{{{Number: 7, Title: "bug"}}}}}
	runner := &fakeRunner{release: make(chan struct{})}
	w := &Engine{
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

	latest := func() JobSnapshot {
		reporter.mu.Lock()
		defer reporter.mu.Unlock()
		if len(reporter.snapshots) == 0 {
			return JobSnapshot{}
		}
		for _, job := range reporter.snapshots[len(reporter.snapshots)-1].Jobs {
			if job.Number == 7 {
				return job
			}
		}
		return JobSnapshot{}
	}
	checkpoint := func(tasksDone, tasksTotal, want int) {
		t.Helper()
		src.mu.Lock()
		src.state = OriginatingWorkState{IssueState: "OPEN", PullRequests: []PullRequestWorkState{{Repository: "o/r", Number: 9, State: "OPEN", IsDraft: true, TasksDone: tasksDone, TasksTotal: tasksTotal}}}
		src.mu.Unlock()
		deadline := time.Now().Add(runLoopWait)
		for time.Now().Before(deadline) {
			if job := latest(); job.HasProgress() == (tasksTotal > 0) && job.Progress == want && job.TasksTotal == tasksTotal {
				return
			}
			time.Sleep(time.Millisecond)
		}
		job := latest()
		t.Fatalf("job progress = %d%% (%d/%d), want %d%% (%d/%d)", job.Progress, job.TasksDone, job.TasksTotal, want, tasksDone, tasksTotal)
	}

	// A draft pull request without a checklist yet shows no progress.
	checkpoint(0, 0, 0)
	checkpoint(1, 4, 25)
	checkpoint(2, 4, 50)
	// The plan grew at the next checkpoint, so the percentage drops.
	checkpoint(2, 5, 40)
}

func TestEngineRecordsPullRequestOpenedOncePerJob(t *testing.T) {
	reporter := &snapshotReporter{}
	src := &fakeClosureSource{fakeSource: &fakeSource{batches: [][]Issue{{{Number: 7, Title: "bug"}}}}}
	runner := &fakeRunner{release: make(chan struct{})}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1,
		Issues: src, Runner: runner, UI: reporter, Transcripts: newTranscriptStore(""),
		closureInterval: time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		close(runner.release)
		<-done
	}()

	latest := func() JobSnapshot {
		reporter.mu.Lock()
		defer reporter.mu.Unlock()
		if len(reporter.snapshots) == 0 {
			return JobSnapshot{}
		}
		for _, job := range reporter.snapshots[len(reporter.snapshots)-1].Jobs {
			if job.Number == 7 {
				return job
			}
		}
		return JobSnapshot{}
	}
	// poll serves the pull requests in state and waits until the job shows
	// pull request number with tasksDone of its checklist checked.
	poll := func(number, tasksDone int, pullRequests ...PullRequestWorkState) {
		t.Helper()
		src.mu.Lock()
		src.state = OriginatingWorkState{IssueState: "OPEN", PullRequests: pullRequests}
		src.mu.Unlock()
		deadline := time.Now().Add(runLoopWait)
		for time.Now().Before(deadline) {
			if job := latest(); job.PullRequest == number && job.TasksDone == tasksDone {
				return
			}
			time.Sleep(time.Millisecond)
		}
		job := latest()
		t.Fatalf("job pull request = #%d with %d tasks done, want #%d with %d", job.PullRequest, job.TasksDone, number, tasksDone)
	}
	draft := func(number, tasksDone int) PullRequestWorkState {
		return PullRequestWorkState{Repository: "o/r", Number: number, State: "OPEN", IsDraft: true, TasksDone: tasksDone, TasksTotal: 4}
	}
	ready := draft(9, 4)
	ready.IsDraft = false

	poll(9, 0, draft(9, 0))
	poll(9, 1, draft(9, 1))
	poll(9, 2, draft(9, 2))
	poll(9, 4, ready)
	// A poll that briefly misses the pull request does not reopen it.
	poll(0, 0)
	poll(9, 4, ready)
	// A different pull request for the same job is recorded once too.
	poll(12, 1, draft(12, 1))
	poll(12, 2, draft(12, 2))

	var opened []string
	for _, event := range w.Transcripts.For(issueKey(Issue{Number: 7, Target: "o/r"})).Events(0) {
		if strings.HasPrefix(event.Text, "pull request opened") {
			opened = append(opened, event.Text)
		}
	}
	if want := []string{"pull request opened: PR #9", "pull request opened: PR #12"}; !slices.Equal(opened, want) {
		t.Fatalf("pull request opened events = %q, want %q", opened, want)
	}
}
