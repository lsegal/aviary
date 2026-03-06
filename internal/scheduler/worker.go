package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/domain"
)

const pollInterval = 5 * time.Second

// WorkerPool pulls jobs from the queue and executes them via agent runners.
type WorkerPool struct {
	queue    *JobQueue
	agents   *agent.Manager
	n        int
	wg       sync.WaitGroup
	stopOnce sync.Once
	stop     chan struct{}
}

// NewWorkerPool creates a WorkerPool with n concurrent workers.
// If n <= 0, GOMAXPROCS is used.
func NewWorkerPool(q *JobQueue, agents *agent.Manager, n int) *WorkerPool {
	if n <= 0 {
		n = runtime.GOMAXPROCS(0)
	}
	return &WorkerPool{
		queue:  q,
		agents: agents,
		n:      n,
		stop:   make(chan struct{}),
	}
}

// Start launches worker goroutines. Returns immediately.
func (p *WorkerPool) Start(ctx context.Context) {
	for range p.n {
		p.wg.Add(1)
		go p.run(ctx)
	}
}

// Stop signals all workers to exit and waits for them.
func (p *WorkerPool) Stop() {
	p.stopOnce.Do(func() { close(p.stop) })
	p.wg.Wait()
}

func (p *WorkerPool) run(ctx context.Context) {
	defer p.wg.Done()
	for {
		select {
		case <-p.stop:
			return
		case <-ctx.Done():
			return
		default:
		}

		job, err := p.queue.Claim()
		if err != nil {
			slog.Warn("worker: claim error", "err", err)
		}
		if job == nil {
			select {
			case <-time.After(pollInterval):
			case <-p.stop:
				return
			case <-ctx.Done():
				return
			}
			continue
		}

		slog.Info("executing job", "id", job.ID, "task", job.TaskID, "agent", job.AgentName)
		if err := p.executeJob(ctx, job); err != nil {
			slog.Warn("job failed", "id", job.ID, "err", err)
			if failErr := p.queue.Fail(job.ID, err); failErr != nil {
				slog.Warn("marking job failed", "id", job.ID, "err", failErr)
			}
		} else {
			if err := p.queue.Complete(job.ID); err != nil {
				slog.Warn("marking job complete", "id", job.ID, "err", err)
			}
		}
	}
}

func (p *WorkerPool) executeJob(ctx context.Context, job *domain.Job) error {
	runner, ok := p.agents.Get(job.AgentName)
	if !ok {
		return fmt.Errorf("agent %q not found", job.AgentName)
	}

	var lastErr error
	var buf strings.Builder
	done := make(chan struct{}, 1)
	runner.Prompt(ctx, job.Prompt, func(e agent.StreamEvent) {
		switch e.Type {
		case agent.StreamEventText:
			buf.WriteString(e.Text)
			slog.Info("job output", "job_id", job.ID, "agent", job.AgentName, "chunk", e.Text)
		case agent.StreamEventDone, agent.StreamEventStop:
			select {
			case done <- struct{}{}:
			default:
			}
		case agent.StreamEventError:
			lastErr = e.Err
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if buf.Len() > 0 {
		if err := p.queue.UpdateOutput(job.ID, buf.String()); err != nil {
			slog.Warn("job: failed to persist output", "id", job.ID, "err", err)
		}
	}
	return lastErr
}
