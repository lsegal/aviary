package scheduler

import (
	"context"
	"fmt"
	"log/slog"
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
)

// missedGrace is how late a fire may run before it counts as missed. A timer
// that fires later than this, or a wake check that finds an overdue fire,
// means the machine slept or Aviary was stopped through the due time.
const missedGrace = time.Minute

// wakeCheckInterval is how often the scheduler looks for fires that a sleep
// delayed past their due time.
const wakeCheckInterval = 30 * time.Second

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
	armed  map[string]*armed
	gen    int
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
	return s, nil
}

// Start begins wake detection, file watching, and worker pool processing.
func (s *Scheduler) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	s.pool.Start(ctx)
	go s.watch.Start()
	go s.wakeLoop(ctx)
}

// Stop halts all scheduling and waits for workers to finish.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Lock()
	for key := range s.armed {
		s.disarmLocked(key)
	}
	s.mu.Unlock()
	s.watch.Stop()
	s.pool.Stop()
}

// Reconcile idempotently applies the scheduler configuration from cfg.
// Added tasks get registered; removed tasks are unregistered.
func (s *Scheduler) Reconcile(cfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()

	desired := make(map[string]struct{})
	for _, ac := range cfg.Agents {
		for _, tc := range ac.Tasks {
			if !config.BoolOr(tc.Enabled, true) {
				continue
			}
			key := taskKey(ac.Name, tc.Name)
			desired[key] = struct{}{}

			if existing, ok := s.tasks[key]; ok && existing == tc {
				continue // unchanged
			}
			s.removeTriggersLocked(key)
			s.tasks[key] = tc

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
				s.fireLocked(key, spec, "schedule", due)
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
		s.fireLocked(key, spec, "missed", due)
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
	if a == nil || a.gen != gen {
		return
	}
	delete(s.armed, key)
	if s.now().Sub(a.next) > missedGrace {
		// The timer ran late: the machine slept through the due time.
		s.armLocked(key, a.spec, true)
		return
	}
	s.fireLocked(key, a.spec, "schedule", a.next)
	if !a.spec.RunOnce {
		s.armLocked(key, a.spec, false)
	}
}

// wakeLoop periodically catches up fires that a sleep delayed past their due
// time, since timers may not advance while the machine sleeps.
func (s *Scheduler) wakeLoop(ctx context.Context) {
	ticker := time.NewTicker(wakeCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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
	s.fireLocked(key, spec, "watch", time.Time{})
}

// fireLocked enqueues a run of key unless the overlap policy skips it, and
// records the fire. due is the scheduled fire time, zero for watch fires.
func (s *Scheduler) fireLocked(key string, spec schedule.Spec, reason string, due time.Time) *domain.Job {
	tc, ok := s.tasks[key]
	if !ok {
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
		job, err = s.queue.EnqueueWithType(key, taskType(tc), agentID, tc.Prompt, tc.Prompt, tc.Target, 0, "", "")
		if err != nil {
			slog.Warn("scheduler: enqueue failed", "task", key, "err", err)
			run.Status = domain.TaskRunFailed
			run.Summary = err.Error()
		} else {
			run.Status = domain.TaskRunQueued
			run.JobID = job.ID
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

// hasActiveJob reports whether key has a pending or running job.
func (s *Scheduler) hasActiveJob(key string) bool {
	jobs, err := s.queue.List(key)
	if err != nil {
		return false
	}
	for _, j := range jobs {
		if j.Status == domain.JobStatusPending || j.Status == domain.JobStatusInProgress {
			return true
		}
	}
	return false
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
	s.state.finishRun(job.TaskID, job.ID, status, summarize(job.Output), duration)
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

	out := make([]domain.ScheduledTask, 0, len(s.tasks))
	for key, tc := range s.tasks {
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
		}
		if fields := TaskFields(tc); fields.HasTrigger() {
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
	if !fields.HasTrigger() {
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

	key, tc, ok := s.findTaskLocked(name)
	if !ok {
		return nil, fmt.Errorf("task %q not found", name)
	}
	agentID, _, _ := strings.Cut(key, "/")
	job, err := s.queue.StartImmediateWithType(key, taskType(tc), agentID, tc.Prompt, tc.Prompt, tc.Target, "", "")
	if err != nil {
		return nil, err
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

// SetTaskOutputDelivery registers an optional function used to deliver
// completed task output to configured task channels.
func (s *Scheduler) SetTaskOutputDelivery(fn func(agentName, route, text string) error) {
	s.pool.SetTaskOutputDelivery(fn)
}

// RunJobNow force-starts an existing pending job immediately, bypassing queue
// scheduling and worker-pool concurrency limits.
func (s *Scheduler) RunJobNow(jobID string) (*domain.Job, error) {
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
