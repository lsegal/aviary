package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
)

// Scheduler orchestrates cron triggers, file-watch triggers, and job execution.
type Scheduler struct {
	queue   *JobQueue
	pool    *WorkerPool
	cron    *CronRunner
	watch   *FileWatcher
	agents  *agent.Manager
	mu      sync.Mutex
	tasks   map[string]config.TaskConfig // task name → config snapshot
	onceFired map[string]bool
	timers map[string]*time.Timer
	cancel  context.CancelFunc
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
		queue:  queue,
		pool:   NewWorkerPool(queue, agents, workers),
		cron:   NewCronRunner(),
		watch:  fw,
		agents: agents,
		tasks:  make(map[string]config.TaskConfig),
		onceFired: make(map[string]bool),
		timers: make(map[string]*time.Timer),
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
			key := taskKey(ac.Name, tc.Name)
			desired[key] = struct{}{}

			if existing, ok := s.tasks[key]; ok && existing == tc {
				continue // unchanged
			}
			s.removeTriggersLocked(key)
			s.tasks[key] = tc
			delete(s.onceFired, key)

			agentName := ac.Name
			agentID := fmt.Sprintf("agent_%s", ac.Name)
			taskID := key
			prompt := tc.Prompt

			enqueue := func() {
				if _, err := s.queue.Enqueue(taskID, agentID, agentName, prompt, 0); err != nil {
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
					enqueue()
					s.markRunOnceComplete(key)
				})
				slog.Info("scheduler: one-time task armed", "key", key, "start_at", startAt.Format(time.RFC3339))
			} else if tc.Schedule != "" {
				registerCron := func() {
					if tc.RunOnce {
						if err := s.cron.Add(key, tc.Schedule, func() {
							enqueue()
							s.markRunOnceComplete(key)
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
				if err := s.watch.Add(key, tc.Watch, func(_ string) { enqueue() }); err != nil {
					slog.Warn("scheduler: watch failed", "task", key, "glob", tc.Watch, "err", err)
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

// Trigger immediately enqueues a configured task by name.
// name may be the full "agent/task" key or just the task name.
func (s *Scheduler) Trigger(name string) (*domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, tc := range s.tasks {
		if key == name || tc.Name == name {
			parts := strings.SplitN(key, "/", 2)
			agentName := parts[0]
			agentID := fmt.Sprintf("agent_%s", agentName)
			return s.queue.Enqueue(key, agentID, agentName, tc.Prompt, 0)
		}
	}
	return nil, fmt.Errorf("task %q not found", name)
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

func (s *Scheduler) markRunOnceComplete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onceFired[key] = true
	s.removeTriggersLocked(key)
	slog.Info("scheduler: one-time task completed", "key", key)
}
