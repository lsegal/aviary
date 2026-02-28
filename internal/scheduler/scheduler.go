package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
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
			s.tasks[key] = tc

			agentName := ac.Name
			agentID := fmt.Sprintf("agent_%s", ac.Name)
			taskID := key
			prompt := tc.Prompt

			enqueue := func() {
				if _, err := s.queue.Enqueue(taskID, agentID, agentName, prompt, 0); err != nil {
					slog.Warn("scheduler: enqueue failed", "task", taskID, "err", err)
				}
			}

			if tc.Schedule != "" {
				if err := s.cron.Add(key, tc.Schedule, enqueue); err != nil {
					slog.Warn("scheduler: invalid cron expression", "task", key, "schedule", tc.Schedule, "err", err)
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
			s.cron.Remove(key)
			s.watch.Remove(key)
			delete(s.tasks, key)
			slog.Info("scheduler: task removed", "key", key)
		}
	}
}

// Queue returns the underlying job queue for external inspection.
func (s *Scheduler) Queue() *JobQueue { return s.queue }

func taskKey(agentName, taskName string) string {
	return agentName + "/" + taskName
}
