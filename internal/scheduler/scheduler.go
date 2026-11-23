package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/schedule"
	"github.com/lsegal/aviary/internal/store"
	"github.com/lsegal/aviary/internal/wake"
)

// missedGrace is how late a fire may run before it counts as missed. A timer
// that fires later than this, or a wake check that finds an overdue fire,
// means the machine slept or Aviary was stopped through the due time.
const missedGrace = time.Minute

// armed is a task's pending time-based fire.
type armed struct {
	spec  schedule.Spec
	next  time.Time
	timer *time.Timer
	gen   int
}

// Scheduler orchestrates time-based triggers, file-watch triggers, and job execution.
type Scheduler struct {
	queue  *JobQueue
	pool   *WorkerPool
	watch  *FileWatcher
	agents *agent.Manager
	state  *stateStore
	mu     sync.Mutex
	tasks  map[string]config.TaskConfig // task name → config snapshot
	// disabled holds tasks turned off in config, which are listed but never
	// fire.
	disabled map[string]config.TaskConfig
	armed    map[string]*armed
	gen      int
	// halted is set once shutdown begins; nothing is enqueued or started
	// after it.
	halted bool
	cancel context.CancelFunc
	// now returns the wall-clock time without a monotonic reading, so
	// comparisons stay correct across sleeps.
	now func() time.Time
}

// New creates and wires a Scheduler. Call Start to begin processing.
func New(agents *agent.Manager, workers int) (*Scheduler, error) {
	fw, err := NewFileWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating file watcher: %w", err)
	}
	queue := NewJobQueue()
	queue.RecoverStuck()

	s := &Scheduler{
		queue:  queue,
		pool:   NewWorkerPool(queue, agents, workers),
		watch:  fw,
		agents: agents,
		state:  loadStateStore(scheduleStatePath()),
		tasks:  make(map[string]config.TaskConfig),
		armed:  make(map[string]*armed),
		now:    func() time.Time { return time.Now().Round(0) },
	}
	s.pool.onAttemptDone = s.recordAttempt
	s.reconcileStranded()
	return s, nil
}

// Start begins wake detection, file watching, and worker pool processing.
func (s *Scheduler) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	s.pool.Start(ctx)
	go s.watch.Start()
	go s.wakeLoop(ctx, wake.Subscribe(ctx))
}

// errHalted is returned for runs requested after shutdown began.
var errHalted = errors.New("scheduler is shutting down")

// Halt stops scheduling when shutdown begins: it disarms every trigger, stops
// file watching and claiming queued jobs, and refuses every later fire,
// manual trigger, and force-start. Running jobs keep running until a
// checkpoint or Stop ends them. Halt is idempotent.
func (s *Scheduler) Halt() {
	s.mu.Lock()
	if s.halted {
		s.mu.Unlock()
		return
	}
	s.halted = true
	for key := range s.armed {
		s.disarmLocked(key)
	}
	s.mu.Unlock()
	s.watch.Stop()
	s.pool.Halt()
	slog.Info("scheduler: halted for shutdown")
}

// Stop halts all scheduling and waits for workers to finish.
func (s *Scheduler) Stop() {
	s.Halt()
	if s.cancel != nil {
		s.cancel()
	}
	s.pool.Stop()
}

// Reconcile idempotently applies the scheduler configuration from cfg.
// Added tasks get registered; removed tasks are unregistered.
func (s *Scheduler) Reconcile(cfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.halted {
		return
	}

	desired := make(map[string]struct{})
	s.disabled = make(map[string]config.TaskConfig)
	for _, ac := range cfg.Agents {
		for _, tc := range ac.Tasks {
			if !config.BoolOr(tc.Enabled, true) {
				s.disabled[taskKey(ac.Name, tc.Name)] = tc
				continue
			}
			key := taskKey(ac.Name, tc.Name)
			desired[key] = struct{}{}

			if existing, ok := s.tasks[key]; ok && existing == tc {
				continue // unchanged
			}
			s.removeTriggersLocked(key)
			s.tasks[key] = tc

			if tc.IsWorkflow() {
				// Workflows fire from channel messages; see FireWorkflows.
				slog.Info("scheduler: workflow registered", "key", key, "channel", tc.Channel)
				continue
			}
			if fields := TaskFields(tc); fields.HasTrigger() {
				spec, err := schedule.Parse(fields)
				if err != nil {
					slog.Warn("scheduler: invalid trigger", "task", key, "err", err)
				} else {
					s.armLocked(key, spec, true)
				}
			}
			if tc.Watch != "" {
				watchGlob := resolveWatchGlob(ac.Name, tc.Watch)
				if err := s.watch.Add(key, watchGlob, func(_ string) { s.fireFromWatch(key) }); err != nil {
					slog.Warn("scheduler: watch failed", "task", key, "glob", watchGlob, "err", err)
				}
			}
			slog.Info("scheduler: task registered", "key", key)
		}
	}

	// Remove tasks no longer in config.
	for key := range s.tasks {
		if _, ok := desired[key]; !ok {
			s.removeTriggersLocked(key)
			delete(s.tasks, key)
			slog.Info("scheduler: task removed", "key", key)
		}
	}
}

// TaskFields returns the trigger settings of tc.
func TaskFields(tc config.TaskConfig) schedule.Fields {
	return schedule.Fields{
		Trigger:  tc.Trigger,
		Schedule: tc.Schedule,
		StartAt:  tc.StartAt,
		RunOnce:  tc.RunOnce,
		Every:    tc.Every,
		At:       tc.At,
		Days:     tc.Days,
		Timezone: tc.Timezone,
		Overlap:  tc.Overlap,
		Missed:   tc.Missed,
	}
}

// armLocked schedules key's next fire. With catchUp set it first settles
// fires that were due since the task last fired, applying the missed-run
// policy: this runs when a task is registered and after a detected sleep.
func (s *Scheduler) armLocked(key string, spec schedule.Spec, catchUp bool) {
	s.disarmLocked(key)
	now := s.now()
	st := s.state.get(key)
	if spec.RunOnce && !st.LastFire.IsZero() {
		return // a run-once task that already fired stays done
	}
	if catchUp {
		since := st.since()
		if since.IsZero() {
			// A new task: fires due within the grace window still run.
			since = now.Add(-missedGrace)
			s.state.update(key, func(t *taskState) { t.Baseline = since })
		}
		if due := spec.Next(since); !due.IsZero() && !due.After(now) {
			fired := spec.Missed == schedule.MissedRunOnce
			if due.Before(now.Add(-missedGrace)) {
				s.settleMissedLocked(key, spec, due, now)
			} else {
				s.fireLocked(key, spec, "schedule", due, nil)
				fired = true
			}
			if spec.RunOnce && fired {
				return
			}
		}
	}
	next := spec.Next(now)
	if next.IsZero() {
		return
	}
	s.gen++
	a := &armed{spec: spec, next: next, gen: s.gen}
	gen := a.gen
	a.timer = time.AfterFunc(next.Sub(now), func() { s.onTimer(key, gen) })
	s.armed[key] = a
}

// settleMissedLocked applies the missed-run policy to fires due since due.
func (s *Scheduler) settleMissedLocked(key string, spec schedule.Spec, due, now time.Time) {
	slog.Info("scheduler: missed fire detected", "key", key, "due", due.Format(time.RFC3339), "policy", spec.Missed)
	if spec.Missed == schedule.MissedRunOnce {
		s.fireLocked(key, spec, "missed", due, nil)
		return
	}
	s.state.update(key, func(t *taskState) {
		t.Baseline = now
		t.addRun(domain.TaskRun{FireTime: due, Reason: "missed", Status: domain.TaskRunMissed})
	})
}

func (s *Scheduler) onTimer(key string, gen int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.armed[key]
	if a == nil || a.gen != gen || s.halted {
		return
	}
	delete(s.armed, key)
	if s.now().Sub(a.next) > missedGrace {
		// The timer ran late: the machine slept through the due time.
		s.armLocked(key, a.spec, true)
		return
	}
	s.fireLocked(key, a.spec, "schedule", a.next, nil)
	if !a.spec.RunOnce {
		s.armLocked(key, a.spec, false)
	}
}

// wakeLoop catches up fires that a sleep delayed past their due time each time
// the machine wakes, since timers may not advance while the machine sleeps.
func (s *Scheduler) wakeLoop(ctx context.Context, wakes <-chan time.Duration) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-wakes:
			s.checkOverdue()
		}
	}
}

// checkOverdue re-arms every task whose next fire is overdue by more than
// missedGrace, settling the missed fire by its policy.
func (s *Scheduler) checkOverdue() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for key, a := range s.armed {
		if now.Sub(a.next) > missedGrace {
			s.armLocked(key, a.spec, true)
		}
	}
}

func (s *Scheduler) fireFromWatch(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	spec, _ := schedule.Parse(TaskFields(s.tasks[key]))
	if spec.Overlap == "" {
		spec.Overlap = schedule.OverlapQueue
	}
	s.fireLocked(key, spec, "watch", time.Time{}, nil)
}

// fireLocked enqueues a run of key unless the overlap policy skips it, and
// records the fire. due is the scheduled fire time, zero for watch and
// message fires; trigger is the message that fired a workflow.
func (s *Scheduler) fireLocked(key string, spec schedule.Spec, reason string, due time.Time, trigger *domain.TriggerContext) *domain.Job {
	tc, ok := s.tasks[key]
	if !ok {
		return nil
	}
	if s.halted {
		slog.Info("scheduler: fire dropped during shutdown", "key", key, "reason", reason)
		return nil
	}
	now := s.now()
	run := domain.TaskRun{FireTime: now, Reason: reason}
	var job *domain.Job
	if spec.Overlap == schedule.OverlapSkip && s.hasActiveJob(key) {
		run.Status = domain.TaskRunSkipped
		run.Summary = "previous run still active"
		slog.Info("scheduler: fire skipped by overlap policy", "key", key)
	} else {
		agentID, _, _ := strings.Cut(key, "/")
		var err error
		prompt := tc.Prompt
		if trigger != nil && taskType(tc) == "prompt" {
			prompt = WorkflowPrompt(tc.Prompt, trigger)
		}
		job, err = s.queue.EnqueueWithType(key, taskType(tc), agentID, prompt, tc.Prompt, tc.Target, 0, "", "")
		if err != nil {
			slog.Warn("scheduler: enqueue failed", "task", key, "err", err)
			run.Status = domain.TaskRunFailed
			run.Summary = err.Error()
		} else {
			run.Status = domain.TaskRunQueued
			run.JobID = job.ID
			if err := s.queue.SetTrigger(job.ID, reason); err != nil {
				slog.Warn("scheduler: recording trigger failed", "task", key, "err", err)
			} else {
				job.Trigger = reason
			}
			if trigger != nil {
				if err := s.queue.SetTriggerContext(job.ID, trigger); err != nil {
					slog.Warn("scheduler: recording trigger context failed", "task", key, "err", err)
				} else {
					job.TriggerContext = trigger
				}
			}
		}
	}
	s.state.update(key, func(t *taskState) {
		if !due.IsZero() {
			t.LastFire = due
			t.Baseline = now
		}
		t.addRun(run)
	})
	return job
}

// Stale-run timeouts bound how long one stranded job can block the overlap
// policy. A due pending job that nothing claimed for staleQueuedTimeout, or a
// run in progress for longer than staleRunTimeout or without a heartbeat for
// lockTimeout, no longer counts as active.
var (
	staleQueuedTimeout = time.Hour
	staleRunTimeout    = 6 * time.Hour
)

// hasActiveJob reports whether key has a pending or running job that is not
// stale.
func (s *Scheduler) hasActiveJob(key string) bool {
	jobs, err := s.queue.List(key)
	if err != nil {
		return false
	}
	now := s.now()
	for _, j := range jobs {
		if j.Status != domain.JobStatusPending && j.Status != domain.JobStatusInProgress {
			continue
		}
		if staleJob(j, now) {
			slog.Warn("scheduler: ignoring stale run for overlap policy", "key", key, "job", j.ID, "status", j.Status)
			continue
		}
		return true
	}
	return false
}

// staleJob reports whether the pending or in-progress job j is stranded.
func staleJob(j domain.Job, now time.Time) bool {
	if j.Status == domain.JobStatusInProgress {
		if j.LockedAt == nil || now.Sub(*j.LockedAt) > lockTimeout {
			return true
		}
		return now.Sub(j.CreatedAt) > staleRunTimeout
	}
	due := j.UpdatedAt
	for _, t := range []*time.Time{j.NextRetryAt, j.ScheduledFor} {
		if t != nil && t.After(due) {
			due = *t
		}
	}
	return now.Sub(due) > staleQueuedTimeout
}

// reconcileStranded settles run history entries left queued, running, or
// retrying by an earlier process, so stranded runs clear before the scheduler
// starts. Each takes its job's current status; a run whose job is gone is
// marked canceled.
func (s *Scheduler) reconcileStranded() {
	jobs, err := s.queue.List("")
	if err != nil {
		slog.Warn("scheduler: listing jobs to reconcile stranded runs", "err", err)
		return
	}
	byID := make(map[string]domain.Job, len(jobs))
	for _, j := range jobs {
		byID[j.ID] = j
	}
	s.state.reconcile(func(run domain.TaskRun) (domain.TaskRunStatus, string, bool) {
		switch run.Status {
		case domain.TaskRunQueued, domain.TaskRunRunning, domain.TaskRunRetrying:
		default:
			return "", "", false
		}
		j, ok := byID[run.JobID]
		if !ok {
			return domain.TaskRunCanceled, "run interrupted: Aviary stopped before it finished", true
		}
		status := taskRunStatus(j.Status)
		if status == run.Status || (status == domain.TaskRunQueued && run.Status == domain.TaskRunRetrying) {
			return "", "", false
		}
		summary := run.Summary
		if j.Status == domain.JobStatusFailed && j.Error != "" {
			summary = summarize(j.Error)
		}
		return status, summary, true
	})
}

// taskRunStatus maps a job's status to the run history status it shows.
func taskRunStatus(status domain.JobStatus) domain.TaskRunStatus {
	switch status {
	case domain.JobStatusPending:
		return domain.TaskRunQueued
	case domain.JobStatusInProgress:
		return domain.TaskRunRunning
	case domain.JobStatusFailed:
		return domain.TaskRunFailed
	case domain.JobStatusCanceled:
		return domain.TaskRunCanceled
	}
	return domain.TaskRunCompleted
}

// recordAttempt updates run history when a job attempt finishes.
func (s *Scheduler) recordAttempt(job domain.Job, duration time.Duration) {
	status := domain.TaskRunCompleted
	switch job.Status {
	case domain.JobStatusFailed:
		status = domain.TaskRunFailed
	case domain.JobStatusCanceled:
		status = domain.TaskRunCanceled
	case domain.JobStatusPending:
		status = domain.TaskRunRetrying
	}
	summary := summarize(job.Output)
	if (status == domain.TaskRunFailed || status == domain.TaskRunRetrying) && job.Error != "" {
		summary = summarize(job.Error)
	}
	s.state.finishRun(job.TaskID, job.ID, status, summary, duration)
}

func taskType(tc config.TaskConfig) string {
	if t := strings.ToLower(strings.TrimSpace(tc.Type)); t != "" {
		return t
	}
	return "prompt"
}

// Queue returns the underlying job queue for external inspection.
func (s *Scheduler) Queue() *JobQueue { return s.queue }

// ListTasks returns configured task definitions currently registered with the
// scheduler, with their next fire and run history.
func (s *Scheduler) ListTasks() []domain.ScheduledTask {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]domain.ScheduledTask, 0, len(s.tasks)+len(s.disabled))
	all := make(map[string]config.TaskConfig, len(s.tasks)+len(s.disabled))
	maps.Copy(all, s.disabled)
	maps.Copy(all, s.tasks)
	for key, tc := range all {
		agentName, taskName, ok := strings.Cut(key, "/")
		if !ok {
			continue
		}
		task := domain.ScheduledTask{
			ID:        key,
			AgentName: agentName,
			AgentID:   agentName,
			Name:      taskName,
			Type:      strings.TrimSpace(tc.Type),
			Prompt:    tc.Prompt,
			Target:    tc.Target,
			RunOnce:   tc.RunOnce,
			Schedule:  tc.Schedule,
			Every:     tc.Every,
			At:        tc.At,
			Days:      tc.Days,
			Timezone:  tc.Timezone,
			Watch:     tc.Watch,
			Channel:   tc.Channel,
			Entry:     tc.Entry,
			Match:     tc.Match,
			Project:   tc.Project,
			Model:     tc.Model,
			Enabled:   config.BoolOr(tc.Enabled, true),
		}
		if strings.EqualFold(task.Type, "project") || (tc.IsWorkflow() && taskType(tc) == "prompt") {
			task.Precompile = config.BoolOr(tc.Precompile, false)
			task.Compiled = task.Precompile && strings.TrimSpace(tc.Compiled) != ""
		}
		if tc.IsWorkflow() {
			task.TriggerType = domain.TriggerTypeMessage
			task.Description = workflowDescription(tc)
			task.Overlap = strings.ToLower(strings.TrimSpace(tc.Overlap))
		} else if fields := TaskFields(tc); fields.HasTrigger() {
			task.TriggerType = domain.TriggerType(fields.InferKind())
			if spec, err := schedule.Parse(fields); err == nil {
				task.Description = spec.Describe()
				task.Overlap = spec.Overlap
				task.Missed = spec.Missed
				if !spec.StartAt.IsZero() {
					startAt := spec.StartAt
					task.StartAt = &startAt
				}
			}
		} else if tc.Watch != "" {
			task.TriggerType = domain.TriggerTypeWatch
		}
		if a := s.armed[key]; a != nil {
			next := a.next
			task.NextRun = &next
		}
		st := s.state.get(key)
		if !st.LastFire.IsZero() {
			last := st.LastFire
			task.LastFire = &last
		}
		task.History = st.History
		out = append(out, task)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AgentName == out[j].AgentName {
			return out[i].Name < out[j].Name
		}
		return out[i].AgentName < out[j].AgentName
	})
	return out
}

// Preview returns up to n upcoming fires of the configured task name, which
// may be the full "agent/task" key or just the task name.
func (s *Scheduler) Preview(name string, n int) ([]time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, tc, ok := s.findTaskLocked(name)
	if !ok {
		return nil, fmt.Errorf("task %q not found", name)
	}
	fields := TaskFields(tc)
	if tc.IsWorkflow() || !fields.HasTrigger() {
		return nil, nil
	}
	spec, err := schedule.Parse(fields)
	if err != nil {
		return nil, err
	}
	if spec.RunOnce && !s.state.get(key).LastFire.IsZero() {
		return nil, nil
	}
	return spec.Preview(s.now(), n), nil
}

// UpcomingRun is one scheduled fire returned by Upcoming.
type UpcomingRun struct {
	TaskID string    `json:"task_id"`
	At     time.Time `json:"at"`
}

// Upcoming returns the next limit fires across all tasks, soonest first,
// taking at most perTask fires from any one task.
func (s *Scheduler) Upcoming(limit, perTask int) []UpcomingRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	var out []UpcomingRun
	for key, a := range s.armed {
		fires := []time.Time{a.next}
		if !a.spec.RunOnce {
			fires = append(fires, a.spec.Preview(a.next, perTask-1)...)
		}
		for i, at := range fires {
			if i >= perTask || at.Before(now) {
				break
			}
			out = append(out, UpcomingRun{TaskID: key, At: at})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At.Equal(out[j].At) {
			return out[i].TaskID < out[j].TaskID
		}
		return out[i].At.Before(out[j].At)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Forget drops the persisted last fire and run history of the task key.
func (s *Scheduler) Forget(key string) { s.state.remove(key) }

func (s *Scheduler) findTaskLocked(name string) (string, config.TaskConfig, bool) {
	if tc, ok := s.tasks[name]; ok {
		return name, tc, true
	}
	for key, tc := range s.tasks {
		if tc.Name == name {
			return key, tc, true
		}
	}
	return "", config.TaskConfig{}, false
}

// Trigger immediately starts a one-off run for a configured task by name,
// bypassing normal queue claiming, worker-pool scheduling and the overlap
// policy. name may be the full "agent/task" key or just the task name.
func (s *Scheduler) Trigger(name string) (*domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.halted {
		return nil, errHalted
	}
	key, tc, ok := s.findTaskLocked(name)
	if !ok {
		return nil, fmt.Errorf("task %q not found", name)
	}
	agentID, _, _ := strings.Cut(key, "/")
	job, err := s.queue.StartImmediateWithType(key, taskType(tc), agentID, tc.Prompt, tc.Prompt, tc.Target, "", "")
	if err != nil {
		return nil, err
	}
	if err := s.queue.SetTrigger(job.ID, "manual"); err != nil {
		slog.Warn("scheduler: recording trigger failed", "task", key, "err", err)
	} else {
		job.Trigger = "manual"
	}
	s.state.update(key, func(t *taskState) {
		t.addRun(domain.TaskRun{FireTime: s.now(), Reason: "manual", Status: domain.TaskRunRunning, JobID: job.ID})
	})
	s.pool.ExecuteNow(job)
	return job, nil
}

func resolveWatchGlob(agentID, glob string) string {
	if glob == "" || filepath.IsAbs(glob) {
		return glob
	}
	return filepath.Join(store.AgentDir(agentID), glob)
}

// SetProjectRunner registers what runs "project" tasks: the Code Loop engine,
// which runs each fire's prompt as a board job.
func (s *Scheduler) SetProjectRunner(fn ProjectRunner) {
	s.pool.projectRunner = fn
}

// SetTaskCompiler registers what compiles precompiled project tasks and
// workflows before their runs; without one they always run on the model.
func (s *Scheduler) SetTaskCompiler(fn TaskCompiler) {
	s.pool.taskCompiler = fn
}

// SetTaskScriptReporter registers what records each precompiled
// project task or workflow script run's result, so a failing script can be set aside.
func (s *Scheduler) SetTaskScriptReporter(fn TaskScriptReporter) {
	s.pool.scriptReporter = fn
}

// SetTaskOutputDelivery registers an optional function used to deliver
// completed task output to configured task channels.
func (s *Scheduler) SetTaskOutputDelivery(fn func(agentName, route, text string) error) {
	s.pool.SetTaskOutputDelivery(fn)
}

// SetWorkflowHooks registers how workflow runs reply to the conversation
// whose message triggered them, and how a run's trigger context learns the
// messages that followed the triggering one.
func (s *Scheduler) SetWorkflowHooks(
	reply func(agentName string, tc domain.TriggerContext, text string) error,
	after func(agentName string, tc domain.TriggerContext) []domain.TriggerMessage,
) {
	s.pool.workflowReply = reply
	s.pool.workflowAfter = after
}

// RunJobNow force-starts an existing pending job immediately, bypassing queue
// scheduling and worker-pool concurrency limits.
func (s *Scheduler) RunJobNow(jobID string) (*domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.halted {
		return nil, errHalted
	}
	job, err := s.queue.ForceStart(jobID)
	if err != nil {
		return nil, err
	}
	s.pool.ExecuteNow(job)
	return job, nil
}

// StopJobs cancels queued or running jobs. If target is empty, all cancellable
// jobs are stopped. target may be a job ID, a full task key ("agent/task"), or
// a bare task name.
func (s *Scheduler) StopJobs(target string) (stopped int, err error) {
	matches := func(job domain.Job) bool {
		if strings.TrimSpace(target) == "" {
			return true
		}
		if job.ID == target || job.TaskID == target {
			return true
		}
		parts := strings.SplitN(job.TaskID, "/", 2)
		return len(parts) == 2 && parts[1] == target
	}

	jobs, err := s.queue.List("")
	if err != nil {
		return 0, err
	}

	for _, job := range jobs {
		if !matches(job) {
			continue
		}
		if job.Status == domain.JobStatusPending {
			if err := s.queue.Cancel(job.ID); err != nil {
				return stopped, err
			}
			stopped++
		}
	}

	stopped += s.pool.StopJobs(func(jobID, taskID string) bool {
		if strings.TrimSpace(target) == "" {
			return true
		}
		if jobID == target || taskID == target {
			return true
		}
		parts := strings.SplitN(taskID, "/", 2)
		return len(parts) == 2 && parts[1] == target
	})

	return stopped, nil
}

func taskKey(agentName, taskName string) string {
	return agentName + "/" + taskName
}

func (s *Scheduler) disarmLocked(key string) {
	if a, ok := s.armed[key]; ok {
		a.timer.Stop()
		delete(s.armed, key)
	}
}

func (s *Scheduler) removeTriggersLocked(key string) {
	s.disarmLocked(key)
	s.watch.Remove(key)
}
