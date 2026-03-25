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
	"github.com/lsegal/aviary/internal/store"
)

// Scheduler orchestrates cron triggers, file-watch triggers, and job execution.
type Scheduler struct {
	queue     *JobQueue
	pool      *WorkerPool
	cron      *CronRunner
	watch     *FileWatcher
	agents    *agent.Manager
	mu        sync.Mutex
	tasks     map[string]config.TaskConfig // task name → config snapshot
	onceFired map[string]bool
	timers    map[string]*time.Timer
	cancel    context.CancelFunc
}

// New creates and wires a Scheduler. Call Start to begin processing.
func New(agents *agent.Manager, workers int) (*Scheduler, error) {
	fw, err := NewFileWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating file watcher: %w", err)
	}
	queue := NewJobQueue()
	queue.RecoverStuck()

	return &Scheduler{
		queue:     queue,
		pool:      NewWorkerPool(queue, agents, workers),
		cron:      NewCronRunner(),
		watch:     fw,
		agents:    agents,
		tasks:     make(map[string]config.TaskConfig),
		onceFired: make(map[string]bool),
		timers:    make(map[string]*time.Timer),
	}, nil
}

// Start begins cron scheduling, file watching, and worker pool processing.
func (s *Scheduler) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	s.cron.Start()
	s.pool.Start(ctx)
	go s.watch.Start()
}

// Stop halts all scheduling and waits for workers to finish.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.cron.Stop()
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
			delete(s.onceFired, key)

			agentID := ac.Name
			taskID := key
			prompt := tc.Prompt
			taskType := strings.ToLower(strings.TrimSpace(tc.Type))
			if taskType == "" {
				taskType = "prompt"
			}
			script := tc.Prompt

			enqueue := func() {
				if _, err := s.queue.EnqueueWithType(taskID, taskType, agentID, prompt, script, tc.Target, 0, "", ""); err != nil {
					slog.Warn("scheduler: enqueue failed", "task", taskID, "err", err)
				}
			}

			now := time.Now().UTC()
			startAt, hasStartAt := parseStartAt(tc.StartAt)

			if tc.RunOnce && hasStartAt {
				delay := time.Until(startAt)
				if delay < 0 {
					delay = 0
				}
				s.timers[key] = time.AfterFunc(delay, func() {
					if !s.beginRunOnce(key) {
						return
					}
					enqueue()
				})
				slog.Info("scheduler: one-time task armed", "key", key, "start_at", startAt.Format(time.RFC3339))
			} else if tc.Schedule != "" {
				registerCron := func() {
					if tc.RunOnce {
						if err := s.cron.Add(key, tc.Schedule, func() {
							if !s.beginRunOnce(key) {
								return
							}
							enqueue()
						}); err != nil {
							slog.Warn("scheduler: invalid cron expression", "task", key, "schedule", tc.Schedule, "err", err)
						}
						return
					}
					if err := s.cron.Add(key, tc.Schedule, enqueue); err != nil {
						slog.Warn("scheduler: invalid cron expression", "task", key, "schedule", tc.Schedule, "err", err)
					}
				}

				if hasStartAt && startAt.After(now) {
					delay := time.Until(startAt)
					s.timers[key] = time.AfterFunc(delay, func() {
						s.mu.Lock()
						if s.onceFired[key] {
							s.mu.Unlock()
							return
						}
						delete(s.timers, key)
						s.mu.Unlock()
						registerCron()
					})
					slog.Info("scheduler: delayed cron task armed", "key", key, "start_at", startAt.Format(time.RFC3339))
				} else {
					registerCron()
				}
			}
			if tc.Watch != "" {
				watchGlob := resolveWatchGlob(agentID, tc.Watch)
				if err := s.watch.Add(key, watchGlob, func(_ string) { enqueue() }); err != nil {
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
			delete(s.onceFired, key)
			slog.Info("scheduler: task removed", "key", key)
		}
	}
}

// Queue returns the underlying job queue for external inspection.
func (s *Scheduler) Queue() *JobQueue { return s.queue }

// ListTasks returns configured task definitions currently registered with the scheduler.
func (s *Scheduler) ListTasks() []domain.ScheduledTask {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]domain.ScheduledTask, 0, len(s.tasks))
	for key, tc := range s.tasks {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			continue
		}
		agentName := parts[0]
		taskName := parts[1]
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
			Watch:     tc.Watch,
		}
		if tc.Watch != "" {
			task.TriggerType = domain.TriggerTypeWatch
		} else {
			task.TriggerType = domain.TriggerTypeCron
		}
		if startAt, ok := parseStartAt(tc.StartAt); ok {
			task.StartAt = &startAt
		}
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

// Trigger immediately starts a one-off run for a configured task by name,
// bypassing normal queue claiming and worker-pool scheduling.
// name may be the full "agent/task" key or just the task name.
func (s *Scheduler) Trigger(name string) (*domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, tc := range s.tasks {
		if key != name && tc.Name != name {
			continue
		}
		parts := strings.SplitN(key, "/", 2)
		agentID := parts[0]
		taskType := strings.ToLower(strings.TrimSpace(tc.Type))
		if taskType == "" {
			taskType = "prompt"
		}
		job, err := s.queue.StartImmediateWithType(key, taskType, agentID, tc.Prompt, tc.Prompt, tc.Target, "", "")
		if err != nil {
			return nil, err
		}
		s.pool.ExecuteNow(job)
		return job, nil
	}
	return nil, fmt.Errorf("task %q not found", name)
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

func parseStartAt(v string) (time.Time, bool) {
	if v == "" {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, false
	}
	return ts.UTC(), true
}

func (s *Scheduler) removeTriggersLocked(key string) {
	s.cron.Remove(key)
	s.watch.Remove(key)
	if timer, ok := s.timers[key]; ok {
		timer.Stop()
		delete(s.timers, key)
	}
}

func (s *Scheduler) beginRunOnce(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onceFired[key] {
		return false
	}
	s.markRunOnceCompleteLocked(key)
	return true
}

func (s *Scheduler) markRunOnceCompleteLocked(key string) {
	s.onceFired[key] = true
	s.removeTriggersLocked(key)
	slog.Info("scheduler: one-time task completed", "key", key)
}
