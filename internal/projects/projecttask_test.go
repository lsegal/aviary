package projects

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"
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

	summary, err := w.runProjectTask(ctx, scheduler.ProjectRun{TaskID: "bot/triage", Name: "triage", Project: "o/r", Prompt: "Triage new issues", Fire: fire}, "o/r", AgentSession{})
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
	if job.Summary != "Opened issue #12 for the stale lockfile" || job.Finished.IsZero() || job.Fallback {
		t.Fatalf("finished job summary = %q, finished %v, fallback %v", job.Summary, job.Finished, job.Fallback)
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
		if _, err := w.runProjectTask(ctx, scheduler.ProjectRun{TaskID: id, Project: "o/r", Prompt: id, Fire: fire}, "o/r", AgentSession{}); err != nil {
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
		_, err := w.runProjectTask(ctx, scheduler.ProjectRun{TaskID: "bot/watch", Project: "o/r", Prompt: "watch", Fire: fire}, "o/r", AgentSession{})
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
	if job, _ := scheduledJob(w, number); job.Status != "failed" || job.Dismissed {
		t.Fatalf("stopped job status = %q, dismissed %v", job.Status, job.Dismissed)
	}
	if err := w.handleJobAction(ctx, jobAction{Action: "dismiss", Target: "o/r", Number: number}); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	if job, _ := scheduledJob(w, number); !job.Dismissed {
		t.Fatal("dismiss left the failed run on the board's Failed state")
	}
}

func TestEngineProjectTaskStopsWithItsCaller(t *testing.T) {
	runner := &scheduledRunner{started: make(chan Issue, 1), block: true}
	w, ctx := startScheduledEngine(t, runner)
	callerCtx, cancel := context.WithCancel(ctx)
	errs := make(chan error, 1)
	go func() {
		_, err := w.runProjectTask(callerCtx, scheduler.ProjectRun{TaskID: "bot/x", Project: "o/r", Prompt: "x"}, "o/r", AgentSession{})
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

// #1028: an engine stopping under a project task run, as a settings change
// restarts it, reports the run interrupted so core's scheduler requeues it,
// not stopped.
func TestEngineStoppingInterruptsProjectTask(t *testing.T) {
	runner := &scheduledRunner{started: make(chan Issue, 1), block: true}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1,
		Issues: &fakeSource{batches: [][]Issue{{}}},
		Runner: runner, UI: &snapshotReporter{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	errs := make(chan error, 1)
	go func() {
		_, err := w.runProjectTask(context.Background(), scheduler.ProjectRun{TaskID: "bot/watch", Project: "o/r", Prompt: "watch"}, "o/r", AgentSession{})
		errs <- err
	}()
	select {
	case <-runner.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the project task did not start")
	}
	cancel()
	select {
	case err := <-errs:
		if !errors.Is(err, scheduler.ErrProjectRunInterrupted) || errors.Is(err, context.Canceled) {
			t.Fatalf("run error = %v, want it interrupted, not canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stopping the engine did not end the run")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
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
	if got := (projectTaskResult{err: scheduler.ErrProjectRunInterrupted}); projectTaskStatus(got) != "interrupted" {
		t.Fatalf("interrupted status = %q", projectTaskStatus(got))
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

// #1028: a run requeued by an engine restart waits for the restart to finish
// before it looks for the engine.
func TestManagerRunProjectTaskWaitsForARestart(t *testing.T) {
	m := newTestManager(t)
	m.refreshMu.Lock()
	errs := make(chan error, 1)
	go func() {
		_, err := m.RunProjectTask(context.Background(), scheduler.ProjectRun{TaskID: "bot/x", Project: "o/unwatched", Prompt: "x"})
		errs <- err
	}()
	select {
	case err := <-errs:
		m.refreshMu.Unlock()
		t.Fatalf("the run went ahead during a restart: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	m.refreshMu.Unlock()
	select {
	case err := <-errs:
		if err == nil || !strings.Contains(err.Error(), "not watched") {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run never went ahead after the restart")
	}
}

// #507: a project task's model override runs on the project's agent with that
// model: a cli/ model as its spec, any other as the agent's in-process turn.
func TestEngineRunsProjectTaskWithModelOverride(t *testing.T) {
	runner := &scheduledRunner{started: make(chan Issue, 1)}
	w, ctx := startScheduledEngine(t, runner)
	fire := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	if _, err := w.runProjectTask(ctx, scheduler.ProjectRun{TaskID: "bot/triage", Name: "triage", Project: "o/r", Prompt: "Triage", Fire: fire}, "o/r", AgentSession{Agent: "claude/opus"}); err != nil {
		t.Fatal(err)
	}
	<-runner.started
	job, ok := scheduledJob(w, int(fire.Unix()))
	if !ok || job.Agent != "claude" || job.Model != "opus" {
		t.Fatalf("job = %+v, want it run on claude/opus rather than the runner's codex", job)
	}
}

func TestManagerProjectAgentResolvesTheOwningAgent(t *testing.T) {
	m := newTestManager(t)
	m.Reconcile(&config.Config{Agents: []config.AgentConfig{
		{Name: "opus", Model: "cli/claude/opus", Projects: config.AgentProjectsConfig{{Target: "o/a"}}},
		{Name: "shared", Model: "cli/codex", Projects: config.AgentProjectsConfig{{Target: "o/b"}}},
	}})
	m.mu.Lock()
	defer m.mu.Unlock()
	if got := m.projectAgentLocked("o/a"); got != "opus" {
		t.Fatalf("o/a agent = %q, want its owner", got)
	}
	if got := m.projectAgentLocked("o/b"); got != "shared" {
		t.Fatalf("o/b agent = %q, want its owner", got)
	}
	if got := m.projectAgentLocked("o/c"); got != "" {
		t.Fatalf("unowned o/c agent = %q, want none", got)
	}
}

func TestManagerRunProjectTaskRefusesAProviderModelWithoutAnAgent(t *testing.T) {
	m := newTestManager(t)
	if err := m.AddWatch(Watch{Targets: []string{"o/r"}}); err != nil {
		t.Fatal(err)
	}
	_, err := m.RunProjectTask(context.Background(), scheduler.ProjectRun{TaskID: "bot/x", Project: "o/r", Prompt: "x", Model: "anthropic/claude-opus-4-5"})
	if err == nil || !strings.Contains(err.Error(), "owned by no agent") {
		t.Fatalf("err = %v, want the unowned project refused", err)
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

// stubProjectClone makes cloneProjectRepository record the repository instead
// of running gh, and points the engine's clones at a temp directory.
func stubProjectClone(t *testing.T, w *Engine) (root string, cloned *[]string) {
	t.Helper()
	root = t.TempDir()
	w.clones.mu.Lock()
	w.clones.tempDirs = []string{root}
	w.clones.mu.Unlock()
	var repositories []string
	prev := cloneProjectRepository
	cloneProjectRepository = func(_ context.Context, repository, dir string) error {
		repositories = append(repositories, repository)
		return os.WriteFile(filepath.Join(dir, "README.md"), []byte("cloned"), 0o600)
	}
	t.Cleanup(func() { cloneProjectRepository = prev })
	return root, &repositories
}

func TestEngineRunsPrecompiledProjectTaskWithoutTheAgent(t *testing.T) {
	runner := &scheduledRunner{started: make(chan Issue, 1)}
	w, ctx := startScheduledEngine(t, runner)
	root, cloned := stubProjectClone(t, w)
	fire := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	var checkout string
	run := scheduler.ProjectRun{TaskID: "bot/stale", Name: "stale", Project: "o/r", Prompt: "Label stale issues", Fire: fire,
		RunCompiled: func(ctx context.Context, clone func(context.Context) (string, error), logf func(string, ...any)) (string, error) {
			dir, err := clone(ctx)
			if err != nil {
				return "", err
			}
			checkout = dir
			if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
				t.Errorf("the script does not run in the clone: %v", err)
			}
			logf("labeled #3")
			return "Labeled 1 stale issue", nil
		}}

	summary, err := w.runProjectTask(ctx, run, "o/r", AgentSession{})
	if err != nil {
		t.Fatal(err)
	}
	if summary != "Labeled 1 stale issue" {
		t.Fatalf("summary = %q", summary)
	}
	select {
	case issue := <-runner.started:
		t.Fatalf("the agent ran a precompiled task: %+v", issue)
	default:
	}
	if len(*cloned) != 1 || (*cloned)[0] != "o/r" {
		t.Fatalf("cloned %v", *cloned)
	}
	if filepath.Dir(checkout) != root {
		t.Fatalf("clone %s is not under the clone root %s", checkout, root)
	}
	if _, err := os.Stat(checkout); !os.IsNotExist(err) {
		t.Fatalf("the clone was left behind: %v", err)
	}
	job, ok := scheduledJob(w, int(fire.Unix()))
	if !ok || job.Status != "complete" || !strings.Contains(job.Log, "labeled #3") {
		t.Fatalf("job = %+v", job)
	}
}

func TestEnginePrecompiledProjectTaskFallsBackToTheAgent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		failure bool
	}{
		{name: "script failure", err: errors.New("lua: exec gh: exit status 1"), failure: true},
		{name: "not deterministic", err: scheduler.ErrProjectTaskNotCompiled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &scheduledRunner{started: make(chan Issue, 1)}
			w, ctx := startScheduledEngine(t, runner)
			stubProjectClone(t, w)
			fire := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
			run := scheduler.ProjectRun{TaskID: "bot/ci", Name: "ci", Project: "o/r", Prompt: "Rerun flaky CI", Fire: fire,
				RunCompiled: func(ctx context.Context, clone func(context.Context) (string, error), _ func(string, ...any)) (string, error) {
					if tc.failure {
						if _, err := clone(ctx); err != nil {
							return "", err
						}
					}
					return "", tc.err
				}}

			summary, err := w.runProjectTask(ctx, run, "o/r", AgentSession{})
			if err != nil {
				t.Fatal(err)
			}
			if summary != "Opened issue #12 for the stale lockfile" {
				t.Fatalf("summary = %q, want the agent's", summary)
			}
			if issue := <-runner.started; issue.Prompt != "Rerun flaky CI" {
				t.Fatalf("agent got %+v", issue)
			}
			job, _ := scheduledJob(w, int(fire.Unix()))
			if got := strings.Contains(job.Log, "precompiled script failed"); got != tc.failure {
				t.Fatalf("log records the failure = %v, want %v:\n%s", got, tc.failure, job.Log)
			}
			if job.Fallback != tc.failure {
				t.Fatalf("job fallback = %v, want %v", job.Fallback, tc.failure)
			}
		})
	}
}
