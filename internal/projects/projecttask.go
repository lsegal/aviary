package projects

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/clidoctor"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/process"
	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/scheduler"
)

// RunProjectTask runs one fire of a core "project" task as a job on the board
// of run.Project, which must be a watched project, and waits for its agent to
// exit. It is the scheduler.ProjectRunner core's scheduler hands project
// tasks to: the schedule itself lives in core, and Code Loop only runs the job.
func (m *Manager) RunProjectTask(ctx context.Context, run scheduler.ProjectRun) (string, error) {
	run.Project = strings.TrimSpace(run.Project)
	m.mu.Lock()
	settings, err := m.settingsLocked()
	agentName, agents := m.projectAgentLocked(run.Project), m.agents
	m.mu.Unlock()
	if agents == nil {
		agents = &config.Config{}
	}
	if err != nil {
		return "", err
	}
	if !slices.Contains(settings.Targets, run.Project) {
		return "", fmt.Errorf("project %q is not watched; add it with project_add first", run.Project)
	}
	var session AgentSession
	if model := strings.TrimSpace(run.Model); model != "" {
		spec, override, ok := config.ProjectModelSpec(agents, agentName, model)
		if !ok {
			return "", fmt.Errorf("model override %q: project %q is owned by no agent to run a model of an API provider on", model, run.Project)
		}
		session = AgentSession{Agent: spec, Model: override}
	}
	// A settings change restarting the engine holds refreshMu until the
	// new engine is running, so a run its restart interrupted and requeued
	// waits for that engine instead of finding none (issue #1028).
	m.refreshMu.Lock()
	m.refreshMu.Unlock() //nolint:staticcheck // waits out a refresh in progress
	engine := m.engine()
	if engine == nil {
		return "", ErrNotWatching
	}
	return engine.runProjectTask(ctx, run, issueRepository(run.Project, Issue{}), session)
}

// projectAgentLocked names the Aviary agent target runs under, as
// settingsLocked resolves it: a watch's agent, else the agent that owns it.
// It returns "" for a runtime watch no agent owns.
func (m *Manager) projectAgentLocked(target string) string {
	name := ""
	for _, watch := range m.watches {
		if agent := strings.TrimSpace(watch.Agent); agent != "" && slices.Contains(watch.Targets, target) {
			name = agent
		}
	}
	if name == "" {
		name = m.owners[target]
	}
	return strings.TrimSpace(name)
}

// ScheduledRetry returns the core task a board Retry on the job fires again
// when the job is a scheduled run, after checking the job can be retried. It
// returns "" for any other job, which JobAction retries itself.
func (m *Manager) ScheduledRetry(target string, number int) (string, error) {
	if m.engine() == nil {
		return "", ErrNotWatching
	}
	job, err := findJob(m.currentSnapshot().Jobs, target, number)
	if err != nil {
		return "", err
	}
	if job.Kind != core.JobKindSchedule {
		return "", nil
	}
	if available := core.JobActionsFor(job); !available.Retry {
		return "", fmt.Errorf("%s#%d cannot be retried: %s", job.Target, job.Number, available.RetryReason)
	}
	return job.ScheduleID, nil
}

// errScheduledRetry refuses a Retry that reaches the engine for a scheduled
// job: only core's scheduler fires its task again (see ScheduledRetry).
var errScheduledRetry = errors.New("scheduled jobs are retried by running their task again with task_run")

type projectTaskRequest struct {
	ctx        context.Context
	run        scheduler.ProjectRun
	repository string
	// session is the agent spec, and model override, the run starts on; an
	// empty Agent runs it on the project's agent.
	session AgentSession
	done    chan projectTaskResult
}

type projectTaskResult struct {
	summary string
	err     error
}

// projectTaskChannel carries project task runs into the Run loop, as
// jobActionRequests carries dashboard actions.
type projectTaskChannel struct {
	once sync.Once
	ch   chan projectTaskRequest
}

func (w *Engine) projectTaskRequests() chan projectTaskRequest {
	w.projectTasks.once.Do(func() { w.projectTasks.ch = make(chan projectTaskRequest) })
	return w.projectTasks.ch
}

// runProjectTask runs a project task's prompt against repository as a board
// job on session's agent, or the project's when it names none, and waits for
// its agent to exit. Cancelling ctx stops the run.
func (w *Engine) runProjectTask(ctx context.Context, run scheduler.ProjectRun, repository string, session AgentSession) (string, error) {
	if err := w.checkReady(ctx); err != nil {
		return "", err
	}
	if strings.TrimSpace(run.Prompt) == "" {
		return "", errors.New("a project task needs a prompt")
	}
	if run.Fire.IsZero() {
		run.Fire = time.Now()
	}
	request := projectTaskRequest{ctx: ctx, run: run, repository: repository, session: session, done: make(chan projectTaskResult, 1)}
	select {
	case w.projectTaskRequests() <- request:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	// The run itself watches ctx, so its result always arrives.
	result := <-request.done
	return result.summary, result.err
}

// cloneProjectRepository clones repository into dir, an empty directory, with
// gh's own authentication, as a project task's agent clones it; tests
// replace it.
var cloneProjectRepository = func(ctx context.Context, repository, dir string) error {
	cmd := process.Command(ctx, "gh", "repo", "clone", repository, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh repo clone: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runCompiledProjectTask runs a precompiled project task's script in a fresh
// clone of its repository, under one of the engine's clone roots, writing its
// output to the job's log. It reports whether the script completed, and
// whether it failed so the run falls back to the model; when it did not
// complete, the log says why and the caller runs the prompt on the agent.
func (w *Engine) runCompiledProjectTask(ctx context.Context, key string, run scheduler.ProjectRun, repository string, output io.Writer, updateSession func(AgentSession)) (compiled, fellBack bool) {
	logf := func(format string, args ...any) { _, _ = fmt.Fprintf(output, format+"\n", args...) }
	var checkout string
	defer func() {
		if checkout != "" {
			if err := os.RemoveAll(checkout); err != nil {
				w.logf("scheduled run %s: removing its clone %s: %v", key, checkout, err)
			}
		}
	}()
	clone := func(ctx context.Context) (string, error) {
		root, err := w.CloneRoot()
		if err != nil {
			return "", err
		}
		dir, err := os.MkdirTemp(root, "aviary-schedule-")
		if err != nil {
			return "", err
		}
		checkout = dir
		if repository == "" {
			repository = run.Project
		}
		if err := cloneProjectRepository(ctx, repository, dir); err != nil {
			return "", err
		}
		updateSession(AgentSession{CheckoutDirectory: dir})
		return dir, nil
	}
	result, err := run.RunCompiled(ctx, clone, logf)
	switch {
	case err == nil:
		if result = strings.TrimSpace(result); result != "" {
			logf("%s", result)
		}
		w.jobSystem(key, "scheduled run finished its precompiled script without the model")
		return true, false
	case errors.Is(err, scheduler.ErrProjectTaskNotCompiled):
		w.jobSystem(key, "the prompt is not deterministic, so the model runs it")
	case ctx.Err() != nil:
		// A stopped run does not fall back; the caller reports the stop.
		return false, false
	default:
		logf("precompiled script failed, so the model runs the task: %v", err)
		w.jobSystem(key, "precompiled script failed, falling back to the model: "+clidoctor.FirstLine(err.Error()))
		return false, true
	}
	return false, false
}

// projectTaskOutcome turns how a project task's agent exited into the summary
// and error core's scheduler records: a stop wraps context.Canceled, and a
// failure carries its first line.
func projectTaskOutcome(runErr, cause error, log string) projectTaskResult {
	var wakeup *scheduledWakeup
	switch {
	case errors.Is(cause, errWorkStoppedFromWebUI):
		return projectTaskResult{summary: "stopped by the owner", err: context.Canceled}
	case cause != nil && runErr != nil:
		summary := clidoctor.FirstLine(cause.Error())
		return projectTaskResult{summary: summary, err: fmt.Errorf("%s: %w", summary, context.Canceled)}
	case runErr != nil && !errors.As(runErr, &wakeup):
		summary := clidoctor.FirstLine(runErr.Error())
		return projectTaskResult{summary: summary, err: errors.New(summary)}
	}
	return projectTaskResult{summary: lastLine(log)}
}

// projectTaskStatus names how a project task run ended, for the job log.
func projectTaskStatus(result projectTaskResult) string {
	switch {
	case result.err == nil:
		return "complete"
	case errors.Is(result.err, context.Canceled):
		return "stopped"
	case errors.Is(result.err, scheduler.ErrProjectRunInterrupted):
		return "interrupted"
	}
	return "failed"
}

// lastLine is the last non-blank line of log, trimmed to fit a history entry.
func lastLine(log string) string {
	lines := strings.Split(strings.TrimSpace(log), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return truncate(line, 200)
		}
	}
	return ""
}
