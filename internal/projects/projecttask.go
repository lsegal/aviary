package projects

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/clidoctor"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/scheduler"
)

// RunProjectTask runs one fire of a core "project" task as a job on the board
// of run.Project, which must be a watched project, and waits for its agent to
// exit. It is the scheduler.ProjectRunner core's scheduler hands project
// tasks to: the schedule itself lives in core, and Projects only runs the job.
func (m *Manager) RunProjectTask(ctx context.Context, run scheduler.ProjectRun) (string, error) {
	run.Project = strings.TrimSpace(run.Project)
	m.mu.Lock()
	settings, err := m.settingsLocked()
	agentName := m.projectAgentLocked(run.Project)
	m.mu.Unlock()
	if err != nil {
		return "", err
	}
	if !slices.Contains(settings.Targets, run.Project) {
		return "", fmt.Errorf("project %q is not watched; add it with project_add first", run.Project)
	}
	var session AgentSession
	if model := strings.TrimSpace(run.Model); model != "" {
		spec, override, ok := config.ProjectModelSpec(agentName, model)
		if !ok {
			return "", fmt.Errorf("model override %q: project %q is attached to no agent to run a model that is not a cli/ model on", model, run.Project)
		}
		session = AgentSession{Agent: spec, Model: override}
	}
	engine := m.engine()
	if engine == nil {
		return "", ErrNotWatching
	}
	return engine.runProjectTask(ctx, run, issueRepository(run.Project, Issue{}), session)
}

// projectAgentLocked names the Aviary agent target is attached to, as
// settingsLocked resolves it: a watch's agent, else the projects config's.
// It returns "" for a project attached to no agent.
func (m *Manager) projectAgentLocked(target string) string {
	cfg, name := m.cfg, ""
	for _, watch := range m.watches {
		cfg = mergeProjects(cfg, watch.Settings)
		if agent := strings.TrimSpace(watch.Agent); agent != "" && slices.Contains(watch.Targets, target) {
			name = agent
		}
	}
	if name == "" {
		name = cfg.AgentFor(target)
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
