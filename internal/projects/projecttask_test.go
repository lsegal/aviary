package projects

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/scheduler"
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

func TestEngineRunsProjectTaskAsBoardJob(t *testing.T) {
	runner := &scheduledRunner{started: make(chan Issue, 1)}
	w, ctx := startScheduledEngine(t, runner)
	fire := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)

	summary, err := w.runProjectTask(ctx, scheduler.ProjectRun{TaskID: "bot/triage", Name: "triage", Project: "o/r", Prompt: "Triage new issues", Fire: fire}, "o/r")
	if err != nil {
		t.Fatal(err)
	}
	if summary != "Opened issue #12 for the stale lockfile" {
		t.Fatalf("summary = %q", summary)
	}
	issue := <-runner.started
	if issue.Prompt != "Triage new issues" || issue.ScheduleID != "bot/triage" || issue.Repository != "o/r" {
		t.Fatalf("runner got %+v", issue)
	}
	job, ok := scheduledJob(w, int(fire.Unix()))
	if !ok {
		t.Fatal("the project task has no board job numbered by its fire time")
	}
	if job.Kind != core.JobKindSchedule || job.ScheduleID != "bot/triage" || job.Title != "triage" || job.Status != "complete" {
		t.Fatalf("job = %+v", job)
	}
	if job.IssueURL() != "" {
		t.Fatalf("a scheduled job links an issue: %s", job.IssueURL())
	}
}

func TestEngineProjectTasksGetDistinctJobs(t *testing.T) {
	runner := &scheduledRunner{started: make(chan Issue, 2)}
	w, ctx := startScheduledEngine(t, runner)
	fire := time.Now()
	for _, id := range []string{"bot/a", "bot/b"} {
		if _, err := w.runProjectTask(ctx, scheduler.ProjectRun{TaskID: id, Project: "o/r", Prompt: id, Fire: fire}, "o/r"); err != nil {
			t.Fatal(err)
		}
	}
	first, okFirst := scheduledJob(w, int(fire.Unix()))
	second, okSecond := scheduledJob(w, int(fire.Unix())+1)
	if !okFirst || !okSecond || first.ScheduleID == second.ScheduleID {
		t.Fatalf("two runs fired in the same second did not get distinct jobs: %+v %+v", first, second)
	}
}

func TestEngineStopsProjectTaskAndRefusesPauseAndRetry(t *testing.T) {
	runner := &scheduledRunner{started: make(chan Issue, 1), block: true}
	w, ctx := startScheduledEngine(t, runner)
	fire := time.Now()
	errs := make(chan error, 1)
	go func() {
		_, err := w.runProjectTask(ctx, scheduler.ProjectRun{TaskID: "bot/watch", Project: "o/r", Prompt: "watch", Fire: fire}, "o/r")
		errs <- err
	}()
	select {
	case <-runner.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the project task did not start")
	}
	number := int(fire.Unix())
	if err := w.handleJobAction(ctx, jobAction{Action: "pause", Target: "o/r", Number: number}); err == nil || !strings.Contains(err.Error(), "cannot be paused") {
		t.Fatalf("pause = %v, want it refused", err)
	}
	if err := w.handleJobAction(ctx, jobAction{Action: "retry", Target: "o/r", Number: number}); !errors.Is(err, errScheduledRetry) {
		t.Fatalf("retry = %v, want it left to core's scheduler", err)
	}
	if err := w.handleJobAction(ctx, jobAction{Action: "stop", Target: "o/r", Number: number}); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := <-errs; !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v, want it canceled", err)
	}
	if job, _ := scheduledJob(w, number); job.Status != "failed" {
		t.Fatalf("stopped job status = %q", job.Status)
	}
}

func TestEngineProjectTaskStopsWithItsCaller(t *testing.T) {
	runner := &scheduledRunner{started: make(chan Issue, 1), block: true}
	w, ctx := startScheduledEngine(t, runner)
	callerCtx, cancel := context.WithCancel(ctx)
	errs := make(chan error, 1)
	go func() {
		_, err := w.runProjectTask(callerCtx, scheduler.ProjectRun{TaskID: "bot/x", Project: "o/r", Prompt: "x"}, "o/r")
		errs <- err
	}()
	<-runner.started
	cancel()
	select {
	case err := <-errs:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("run error = %v, want it canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelling the caller did not stop the run")
	}
}

func TestProjectTaskOutcome(t *testing.T) {
	if got := projectTaskOutcome(nil, nil, "a\n\nlast line\n"); got.err != nil || got.summary != "last line" || projectTaskStatus(got) != "complete" {
		t.Fatalf("complete = %+v", got)
	}
	if got := projectTaskOutcome(errors.New("exit status 1\nmore"), nil, ""); got.err == nil || got.summary != "exit status 1" || projectTaskStatus(got) != "failed" {
		t.Fatalf("failed = %+v", got)
	}
	if got := projectTaskOutcome(context.Canceled, errWorkStoppedFromWebUI, ""); !errors.Is(got.err, context.Canceled) || projectTaskStatus(got) != "stopped" {
		t.Fatalf("stopped = %+v", got)
	}
	if got := projectTaskOutcome(&scheduledWakeup{Delay: time.Minute}, nil, "done"); got.err != nil {
		t.Fatalf("a pending wakeup = %+v, want complete", got)
	}
}

func TestManagerRunProjectTaskNeedsAWatchedProject(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.RunProjectTask(context.Background(), scheduler.ProjectRun{TaskID: "bot/x", Project: "o/unwatched", Prompt: "x"}); err == nil || !strings.Contains(err.Error(), "not watched") {
		t.Fatalf("err = %v, want the unwatched project refused", err)
	}
}

func TestManagerScheduledRetry(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.ScheduledRetry("o/r", 1); !errors.Is(err, ErrNotWatching) {
		t.Fatalf("without an engine = %v, want ErrNotWatching", err)
	}
	m.running = &engineRun{engine: &Engine{}}
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{
		{Number: 1, Target: "o/r", Kind: core.JobKindSchedule, ScheduleID: "bot/digest", Status: "complete"},
		{Number: 2, Target: "o/r", Kind: core.JobKindSchedule, ScheduleID: "bot/digest", Status: "active"},
		{Number: 3, Target: "o/r", Kind: core.JobKindIssue, Status: "failed"},
	}})
	if id, err := m.ScheduledRetry("o/r", 1); err != nil || id != "bot/digest" {
		t.Fatalf("finished scheduled job = %q, %v; want its task", id, err)
	}
	if _, err := m.ScheduledRetry("o/r", 2); err == nil {
		t.Fatal("retry of a running scheduled job was accepted")
	}
	if id, err := m.ScheduledRetry("o/r", 3); err != nil || id != "" {
		t.Fatalf("issue job = %q, %v; want it left to JobAction", id, err)
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
