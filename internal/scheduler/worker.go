package scheduler

import (
	"context"
	"errors"
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
	ctxMu    sync.RWMutex
	ctx      context.Context
	deliver  func(agentName, route, text string) error
	activeMu sync.Mutex
	active   map[string]activeJob
}

type activeJob struct {
	taskID    string
	sessionID string
	cancel    context.CancelFunc
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
		active: make(map[string]activeJob),
	}
}

// Start launches worker goroutines. Returns immediately.
func (p *WorkerPool) Start(ctx context.Context) {
	p.ctxMu.Lock()
	p.ctx = ctx
	p.ctxMu.Unlock()
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

		p.processJob(ctx, job)
	}
}

// ExecuteNow runs a job immediately in its own goroutine, bypassing queue
// claiming and worker-pool concurrency limits while still persisting status.
func (p *WorkerPool) ExecuteNow(job *domain.Job) {
	ctx := context.Background()
	p.ctxMu.RLock()
	if p.ctx != nil {
		ctx = p.ctx
	}
	p.ctxMu.RUnlock()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.processJob(ctx, job)
	}()
}

func (p *WorkerPool) processJob(ctx context.Context, job *domain.Job) {
	jobCtx, cancel := context.WithCancel(ctx)
	p.registerActiveJob(job.ID, job.TaskID, cancel)
	defer p.unregisterActiveJob(job.ID)

	stopHeartbeat := make(chan struct{})
	defer close(stopHeartbeat)
	go p.heartbeatJob(jobCtx, job.ID, stopHeartbeat)

	slog.Info("executing job", "id", job.ID, "task", job.TaskID, "agent", job.AgentName)
	if err := p.executeJob(jobCtx, job); err != nil {
		if errors.Is(err, context.Canceled) {
			if cancelErr := p.queue.Cancel(job.ID); cancelErr != nil {
				slog.Warn("marking job canceled", "id", job.ID, "err", cancelErr)
			}
			return
		}
		slog.Warn("job failed", "id", job.ID, "err", err)
		if failErr := p.queue.Fail(job.ID, err); failErr != nil {
			slog.Warn("marking job failed", "id", job.ID, "err", failErr)
		}
		return
	}
	if err := p.queue.Complete(job.ID); err != nil {
		slog.Warn("marking job complete", "id", job.ID, "err", err)
	}
}

func (p *WorkerPool) heartbeatJob(ctx context.Context, jobID string, stop <-chan struct{}) {
	ticker := time.NewTicker(lockHeartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := p.queue.Heartbeat(jobID); err != nil {
				slog.Warn("job: heartbeat failed", "id", jobID, "err", err)
			}
		case <-stop:
			return
		case <-ctx.Done():
			return
		}
	}
}

// jobSessionName returns the stable named session used for a job.
// Task runs should reuse the same named session so retries and future runs can resume context.
func jobSessionName(job *domain.Job) string {
	parts := strings.SplitN(job.TaskID, "/", 2)
	name := job.TaskID
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		name = parts[1]
	}
	return name
}

func (p *WorkerPool) executeJob(ctx context.Context, job *domain.Job) error {
	runner, ok := p.agents.Get(job.AgentName)
	if !ok {
		return fmt.Errorf("agent %q not found", job.AgentName)
	}

	sessionID := job.SessionID
	if sessionID == "" {
		// Use a stable named session so scheduled work is resumable across retries and future runs.
		if sess, err := agent.NewSessionManager().GetOrCreateNamedTyped(job.AgentID, jobSessionName(job), domain.SessionTypeTask); err != nil {
			slog.Warn("job: failed to create session, falling back to main", "id", job.ID, "err", err)
		} else {
			sessionID = sess.ID
			job.SessionID = sess.ID
			if err := p.queue.SetSession(job.ID, sess.ID); err != nil {
				slog.Warn("job: failed to persist session", "id", job.ID, "session", sess.ID, "err", err)
			}
		}
	}
	if sessionID != "" {
		p.setActiveJobSession(job.ID, sessionID)
		ctx = agent.WithSessionID(ctx, sessionID)
	}

	prompt := job.Prompt
	if job.SessionID != "" && job.Attempts > 1 {
		prompt = "Continue the unfinished scheduled task from this existing session. Complete any remaining work for the original request:\n\n" + job.Prompt
	}

	var lastErr error
	var buf strings.Builder
	done := make(chan struct{}, 1)
	runner.Prompt(ctx, prompt, func(e agent.StreamEvent) {
		switch e.Type {
		case agent.StreamEventText:
			buf.WriteString(e.Text)
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
	output := buf.String()
	if output != "" {
		if err := p.queue.UpdateOutput(job.ID, output); err != nil {
			slog.Warn("job: failed to persist output", "id", job.ID, "err", err)
		}
		// Reply to the originating session/channel if one was recorded.
		if job.ReplyAgentID != "" && job.ReplySessionID != "" {
			if err := agent.AppendReplyToSession(job.ReplyAgentID, job.ReplySessionID, output); err != nil {
				slog.Warn("job: failed to send reply to session", "id", job.ID, "session", job.ReplySessionID, "err", err)
			}
		}
		if agent.ShouldDeliverReply(output) && job.OutputChannel != "" && p.deliver != nil {
			if err := p.deliver(job.AgentName, job.OutputChannel, output); err != nil {
				slog.Warn("job: failed to deliver output to task channel", "id", job.ID, "route", job.OutputChannel, "err", err)
			}
		}
	}
	return lastErr
}

// SetTaskOutputDelivery configures the callback used to forward completed task output.
func (p *WorkerPool) SetTaskOutputDelivery(fn func(agentName, route, text string) error) {
	p.deliver = fn
}

func (p *WorkerPool) registerActiveJob(jobID, taskID string, cancel context.CancelFunc) {
	p.activeMu.Lock()
	defer p.activeMu.Unlock()
	p.active[jobID] = activeJob{taskID: taskID, cancel: cancel}
}

func (p *WorkerPool) setActiveJobSession(jobID, sessionID string) {
	p.activeMu.Lock()
	defer p.activeMu.Unlock()
	job, ok := p.active[jobID]
	if !ok {
		return
	}
	job.sessionID = sessionID
	p.active[jobID] = job
}

func (p *WorkerPool) unregisterActiveJob(jobID string) {
	p.activeMu.Lock()
	defer p.activeMu.Unlock()
	delete(p.active, jobID)
}

// StopJobs interrupts active jobs. If matcher is nil, all active jobs are
// stopped. It returns the number of jobs signaled for cancellation.
func (p *WorkerPool) StopJobs(matcher func(jobID, taskID string) bool) int {
	p.activeMu.Lock()
	defer p.activeMu.Unlock()

	stopped := 0
	for jobID, job := range p.active {
		if matcher != nil && !matcher(jobID, job.taskID) {
			continue
		}
		if job.sessionID != "" {
			agent.StopSession(job.sessionID)
		}
		if job.cancel != nil {
			job.cancel()
		}
		stopped++
	}
	return stopped
}
