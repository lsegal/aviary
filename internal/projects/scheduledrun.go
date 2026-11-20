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
)

// ScheduledRun is one fire of a scheduled prompt: a Projects job that runs
// Prompt against Repository instead of /gh-fix, takes a slot like any job,
// and finishes when its agent exits.
type ScheduledRun struct {
	// ScheduleID and Name identify the schedule the run fired from.
	ScheduleID string
	Name       string
	// Target is the watched project the job's card belongs to, and
	// Repository the OWNER/REPO the agent works on.
	Target     string
	Repository string
	Prompt     string
	// Agent is the coding-CLI spec to run, such as "claude:opus"; empty picks
	// the project's agent the way an issue dispatch does.
	Agent string
	Fire  time.Time
}

// Scheduled run outcomes.
const (
	ScheduledRunComplete = "complete"
	ScheduledRunFailed   = "failed"
	ScheduledRunStopped  = "stopped"
)

// ScheduledRunResult is how a scheduled run ended. Summary is the last line
// the agent wrote, or the error that ended the run.
type ScheduledRunResult struct {
	Status  string
	Summary string
	Job     int
}

type scheduledRunRequest struct {
	ctx  context.Context
	run  ScheduledRun
	done chan ScheduledRunResult
}

// scheduledRunChannel carries scheduled runs into the Run loop, as
// jobActionRequests carries dashboard actions.
type scheduledRunChannel struct {
	once sync.Once
	ch   chan scheduledRunRequest
}

func (w *Engine) scheduledRunRequests() chan scheduledRunRequest {
	w.scheduled.once.Do(func() { w.scheduled.ch = make(chan scheduledRunRequest) })
	return w.scheduled.ch
}

// RunScheduled runs a scheduled prompt as a board job and waits for its agent
// to exit. Cancelling ctx stops the run.
func (w *Engine) RunScheduled(ctx context.Context, run ScheduledRun) (ScheduledRunResult, error) {
	if err := w.checkReady(ctx); err != nil {
		return ScheduledRunResult{}, err
	}
	if strings.TrimSpace(run.Prompt) == "" {
		return ScheduledRunResult{}, errors.New("a scheduled run needs a prompt")
	}
	if run.Fire.IsZero() {
		run.Fire = time.Now()
	}
	request := scheduledRunRequest{ctx: ctx, run: run, done: make(chan ScheduledRunResult, 1)}
	select {
	case w.scheduledRunRequests() <- request:
	case <-ctx.Done():
		return ScheduledRunResult{}, ctx.Err()
	}
	// The run itself watches ctx, so its result always arrives.
	return <-request.done, nil
}

// SetScheduleRerun installs what a board Retry on a scheduled job calls: it
// fires the schedule again through the scheduler, so the rerun lands in the
// schedule's history like any other fire.
func (w *Engine) SetScheduleRerun(rerun func(scheduleID string) error) {
	w.scheduleRerunMu.Lock()
	w.scheduleRerun = rerun
	w.scheduleRerunMu.Unlock()
}

func (w *Engine) rerunSchedule(scheduleID string) error {
	w.scheduleRerunMu.Lock()
	rerun := w.scheduleRerun
	w.scheduleRerunMu.Unlock()
	if rerun == nil {
		return errors.New("scheduled jobs cannot be retried here; run the schedule again instead")
	}
	return rerun(scheduleID)
}

// scheduledRunOutcome turns how a scheduled run's agent exited into its result.
func scheduledRunOutcome(runErr, cause error, log string) ScheduledRunResult {
	var wakeup *scheduledWakeup
	switch {
	case errors.Is(cause, errWorkStoppedFromWebUI):
		return ScheduledRunResult{Status: ScheduledRunStopped, Summary: "stopped by the owner"}
	case cause != nil && runErr != nil:
		return ScheduledRunResult{Status: ScheduledRunStopped, Summary: clidoctor.FirstLine(cause.Error())}
	case runErr != nil && !errors.As(runErr, &wakeup):
		return ScheduledRunResult{Status: ScheduledRunFailed, Summary: clidoctor.FirstLine(runErr.Error())}
	}
	return ScheduledRunResult{Status: ScheduledRunComplete, Summary: lastLine(log)}
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

// scheduledJobStatus is the board status of a finished scheduled run.
func scheduledJobStatus(result ScheduledRunResult) string {
	if result.Status == ScheduledRunComplete {
		return "complete"
	}
	return "failed"
}

// RunScheduled runs a scheduled prompt as a job on the board of run.Target,
// which must be a watched project, and waits for its agent to exit.
func (m *Manager) RunScheduled(ctx context.Context, run ScheduledRun) (ScheduledRunResult, error) {
	run.Target = strings.TrimSpace(run.Target)
	m.mu.Lock()
	settings, err := m.settingsLocked()
	m.mu.Unlock()
	if err != nil {
		return ScheduledRunResult{}, err
	}
	if !slices.Contains(settings.Targets, run.Target) {
		return ScheduledRunResult{}, fmt.Errorf("project %q is not watched; add it with project_add first", run.Target)
	}
	if run.Repository == "" {
		run.Repository = issueRepository(run.Target, Issue{})
	}
	engine := m.engine()
	if engine == nil {
		return ScheduledRunResult{}, ErrNotWatching
	}
	return engine.RunScheduled(ctx, run)
}

// SetScheduleRerun installs what a board Retry on a scheduled job calls to
// fire its schedule again.
func (m *Manager) SetScheduleRerun(rerun func(scheduleID string) error) {
	m.scheduleRerun.Store(&rerun)
}

func (m *Manager) rerunSchedule(scheduleID string) error {
	rerun := m.scheduleRerun.Load()
	if rerun == nil || *rerun == nil {
		return errors.New("scheduled jobs cannot be retried here; run the schedule again instead")
	}
	return (*rerun)(scheduleID)
}
