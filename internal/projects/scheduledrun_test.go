package projects

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// scheduledRunner records the issue each run receives, writes a line of
// output, and either returns at once or blocks until its context ends.
type scheduledRunner struct {
	started chan Issue
	block   bool
}

func (r *scheduledRunner) AgentName() string                { return "codex" }
func (r *scheduledRunner) Run(context.Context, Issue) error { return nil }
func (r *scheduledRunner) RunWithOutput(ctx context.Context, issue Issue, output io.Writer) error {
	r.started <- issue
	_, _ = io.WriteString(output, "looked at the repository\nOpened issue #12 for the stale lockfile\n")
	if r.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

// startScheduledEngine runs an engine with no issues to dispatch.
func startScheduledEngine(t *testing.T, runner *scheduledRunner) (*Engine, context.Context) {
	t.Helper()
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1,
		Issues: &fakeSource{batches: [][]Issue{{}}},
		Runner: runner, UI: &snapshotReporter{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return w, ctx
}

func scheduledJob(w *Engine, number int) (JobSnapshot, bool) {
	snapshot := w.published.Load()
	if snapshot == nil {
		return JobSnapshot{}, false
	}
	for _, job := range snapshot.Jobs {
		if job.Number == number {
			return job, true
		}
	}
	return JobSnapshot{}, false
}

func TestEngineRunsScheduledPromptAsBoardJob(t *testing.T) {
	runner := &scheduledRunner{started: make(chan Issue, 1)}
	w, ctx := startScheduledEngine(t, runner)
	fire := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)

	result, err := w.RunScheduled(ctx, ScheduledRun{ScheduleID: "bot/triage", Name: "triage", Target: "o/r", Repository: "o/r", Prompt: "Triage new issues", Fire: fire})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ScheduledRunComplete || result.Summary != "Opened issue #12 for the stale lockfile" {
		t.Fatalf("result = %+v", result)
	}
	issue := <-runner.started
	if issue.Prompt != "Triage new issues" || issue.ScheduleID != "bot/triage" || issue.Repository != "o/r" {
		t.Fatalf("runner got %+v", issue)
	}
	if result.Job != int(fire.Unix()) {
		t.Fatalf("job number = %d, want the fire time %d", result.Job, fire.Unix())
	}
	job, ok := scheduledJob(w, result.Job)
	if !ok {
		t.Fatal("the scheduled run has no board job")
	}
	if job.Kind != core.JobKindSchedule || job.ScheduleID != "bot/triage" || job.Title != "triage" || job.Status != "complete" {
		t.Fatalf("job = %+v", job)
	}
	if job.IssueURL() != "" {
		t.Fatalf("a scheduled job links an issue: %s", job.IssueURL())
	}
}

func TestEngineScheduledRunsGetDistinctJobs(t *testing.T) {
	runner := &scheduledRunner{started: make(chan Issue, 2)}
	w, ctx := startScheduledEngine(t, runner)
	fire := time.Now()
	first, _ := w.RunScheduled(ctx, ScheduledRun{ScheduleID: "bot/a", Target: "o/r", Prompt: "a", Fire: fire})
	second, _ := w.RunScheduled(ctx, ScheduledRun{ScheduleID: "bot/b", Target: "o/r", Prompt: "b", Fire: fire})
	if first.Job == second.Job {
		t.Fatalf("two runs fired in the same second share job #%d", first.Job)
	}
}

func TestEngineStopsScheduledRunAndRefusesPause(t *testing.T) {
	runner := &scheduledRunner{started: make(chan Issue, 1), block: true}
	w, ctx := startScheduledEngine(t, runner)
	fire := time.Now()
	results := make(chan ScheduledRunResult, 1)
	go func() {
		result, err := w.RunScheduled(ctx, ScheduledRun{ScheduleID: "bot/watch", Target: "o/r", Prompt: "watch", Fire: fire})
		if err != nil {
			t.Error(err)
		}
		results <- result
	}()
	select {
	case <-runner.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the scheduled run did not start")
	}
	number := int(fire.Unix())
	if err := w.handleJobAction(ctx, jobAction{Action: "pause", Target: "o/r", Number: number}); err == nil || !strings.Contains(err.Error(), "cannot be paused") {
		t.Fatalf("pause = %v, want it refused", err)
	}
	if err := w.handleJobAction(ctx, jobAction{Action: "retry", Target: "o/r", Number: number}); err == nil {
		t.Fatal("retry of a running scheduled job was accepted")
	}
	if err := w.handleJobAction(ctx, jobAction{Action: "stop", Target: "o/r", Number: number}); err != nil {
		t.Fatalf("stop: %v", err)
	}
	result := <-results
	if result.Status != ScheduledRunStopped {
		t.Fatalf("result = %+v, want stopped", result)
	}
	if job, _ := scheduledJob(w, number); job.Status != "failed" {
		t.Fatalf("stopped job status = %q", job.Status)
	}
}

func TestEngineRetryOfScheduledJobFiresTheScheduleAgain(t *testing.T) {
	runner := &scheduledRunner{started: make(chan Issue, 1)}
	w, ctx := startScheduledEngine(t, runner)
	reran := make(chan string, 1)
	w.SetScheduleRerun(func(id string) error {
		reran <- id
		return nil
	})
	result, err := w.RunScheduled(ctx, ScheduledRun{ScheduleID: "bot/digest", Target: "o/r", Prompt: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.handleJobAction(ctx, jobAction{Action: "retry", Target: "o/r", Number: result.Job}); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if id := <-reran; id != "bot/digest" {
		t.Fatalf("reran %q", id)
	}
}

func TestEngineScheduledRunStopsWithItsCaller(t *testing.T) {
	runner := &scheduledRunner{started: make(chan Issue, 1), block: true}
	w, ctx := startScheduledEngine(t, runner)
	callerCtx, cancel := context.WithCancel(ctx)
	results := make(chan ScheduledRunResult, 1)
	go func() {
		result, _ := w.RunScheduled(callerCtx, ScheduledRun{ScheduleID: "bot/x", Target: "o/r", Prompt: "x"})
		results <- result
	}()
	<-runner.started
	cancel()
	select {
	case result := <-results:
		if result.Status != ScheduledRunStopped {
			t.Fatalf("result = %+v, want stopped", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelling the caller did not stop the run")
	}
}

func TestScheduledRunOutcome(t *testing.T) {
	if got := scheduledRunOutcome(nil, nil, "a\n\nlast line\n"); got.Status != ScheduledRunComplete || got.Summary != "last line" {
		t.Fatalf("complete = %+v", got)
	}
	if got := scheduledRunOutcome(errors.New("exit status 1\nmore"), nil, ""); got.Status != ScheduledRunFailed || got.Summary != "exit status 1" {
		t.Fatalf("failed = %+v", got)
	}
	if got := scheduledRunOutcome(context.Canceled, errWorkStoppedFromWebUI, ""); got.Status != ScheduledRunStopped {
		t.Fatalf("stopped = %+v", got)
	}
	if got := scheduledRunOutcome(&scheduledWakeup{Delay: time.Minute}, nil, "done"); got.Status != ScheduledRunComplete {
		t.Fatalf("a pending wakeup = %+v, want complete", got)
	}
}

func TestJobActionsForScheduledJobs(t *testing.T) {
	running := core.JobActionsFor(JobSnapshot{Kind: core.JobKindSchedule, Status: "active"})
	if running.Pause || running.Retry || !running.Stop {
		t.Fatalf("running scheduled job actions = %+v", running)
	}
	finished := core.JobActionsFor(JobSnapshot{Kind: core.JobKindSchedule, Status: "failed"})
	if finished.Pause || !finished.Retry || finished.Stop {
		t.Fatalf("finished scheduled job actions = %+v", finished)
	}
	if issue := core.JobActionsFor(JobSnapshot{Kind: core.JobKindIssue, Status: "active"}); !issue.Pause {
		t.Fatal("issue jobs lost Pause")
	}
}
