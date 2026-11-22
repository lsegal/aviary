package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
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
	// held stops workers from claiming new jobs while a safe restart waits.
	held atomic.Bool
	// checkpointNote is set during a safe restart's shutdown checkpoint:
	// jobs canceled then are requeued with this note instead of canceled.
	checkpointNote atomic.Pointer[string]
	// onAttemptDone, when set, receives each job as persisted after one
	// execution attempt finishes, with the attempt's duration.
	onAttemptDone func(job domain.Job, duration time.Duration)
	// projectRunner runs a "project" task's prompt as a Code Loop board job.
	projectRunner ProjectRunner
	// projectCompiler compiles precompiled project tasks.
	projectCompiler ProjectTaskCompiler
}

// ProjectRun is one fire of a "project" task: its prompt, run as a job on the
// board of a watched project.
type ProjectRun struct {
	// TaskID is the task's "agent/task" key, which a board Retry passes back
	// to Trigger.
	TaskID  string
	Name    string
	Project string
	// Model overrides the project agent's model for the run; empty runs it
	// with the agent's own.
	Model  string
	Prompt string
	Fire   time.Time
	// RunCompiled is set for a task with precompile on. The runner calls it
	// before starting the agent, with clone, which makes a fresh clone of
	// Project and returns its path; the runner removes the clone afterwards.
	// RunCompiled runs the task's compiled script there and returns its
	// output. When it fails, the runner logs why and runs the prompt on the
	// agent instead; ErrProjectTaskNotCompiled means the prompt is not
	// deterministic, which is no failure.
	RunCompiled func(ctx context.Context, clone func(context.Context) (string, error), logf func(format string, args ...any)) (string, error)
}

// ErrProjectTaskNotCompiled is RunCompiled's error for a task whose prompt did
// not pass the determinism gate, so each run uses the model.
var ErrProjectTaskNotCompiled = errors.New("the task's prompt is not deterministic enough to compile")

// ProjectTaskCompiler returns a precompiled project task's current script,
// compiling it first when its prompt, project or tools changed; "" means its
// prompt is not deterministic.
type ProjectTaskCompiler func(ctx context.Context, agentName, taskName string) (string, error)

// ProjectRunner runs a project task and waits for its agent to exit. It
// returns the run's one-line summary, and an error when the run failed or
// was stopped.
type ProjectRunner func(ctx context.Context, run ProjectRun) (summary string, err error)

type activeJob struct {
	taskID    string
	agentID   string
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

		if p.held.Load() {
			select {
			case <-time.After(pollInterval):
			case <-p.stop:
				return
			case <-ctx.Done():
				return
			}
			continue
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
// The worker runs its own copy of job, so the caller's job is never mutated.
func (p *WorkerPool) ExecuteNow(job *domain.Job) {
	running := *job
	job = &running
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
	p.registerActiveJob(job.ID, job.TaskID, job.AgentID, cancel)
	defer p.unregisterActiveJob(job.ID)

	stopHeartbeat := make(chan struct{})
	defer close(stopHeartbeat)
	go p.heartbeatJob(jobCtx, job.ID, stopHeartbeat)

	slog.Info("executing job", "id", job.ID, "task", job.TaskID, "agent", job.AgentID)
	started := time.Now()
	defer p.attemptDone(job.ID, started)
	if err := p.executeJob(jobCtx, job); err != nil {
		if errors.Is(err, context.Canceled) {
			p.finishCanceled(job.ID)
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

// finishCanceled records a canceled job: requeued for after the restart when a
// safe restart's checkpoint canceled it, otherwise canceled.
func (p *WorkerPool) finishCanceled(jobID string) {
	if note := p.checkpointNote.Load(); note != nil {
		if err := p.queue.Requeue(jobID, *note); err != nil {
			slog.Warn("requeueing checkpointed job", "id", jobID, "err", err)
		}
		return
	}
	if err := p.queue.Cancel(jobID); err != nil {
		slog.Warn("marking job canceled", "id", jobID, "err", err)
	}
}

func (p *WorkerPool) attemptDone(jobID string, started time.Time) {
	if p.onAttemptDone == nil {
		return
	}
	path := store.FindJobPath(jobID)
	if path == "" {
		return
	}
	job, err := store.ReadJSON[domain.Job](path)
	if err != nil {
		return
	}
	p.onAttemptDone(job, time.Since(started))
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
	runner, ok := p.agents.Get(job.AgentID)
	if !ok {
		return fmt.Errorf("agent %q not found", job.AgentID)
	}

	sessionID := job.SessionID
	if sessionID == "" {
		// Use the task's own session so scheduled work is resumable across
		// retries and future runs, and shows under the task in the sidebar.
		if sess, err := agent.EnsureTaskSession(job.AgentID, jobSessionName(job)); err != nil {
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
	ctx = agent.WithSessionAgentID(ctx, job.AgentID)
	ctx = agent.WithTaskID(ctx, job.TaskID)
	ctx = agent.WithJobID(ctx, job.ID)

	taskType, prompt, _ := resolveTaskExecution(job, runner.Config())
	if taskType == "project" {
		return p.executeProjectJob(ctx, job, runner.Config())
	}
	if taskType == "script" {
		result, err := executeScriptJob(ctx, job, runner.Config(), p.deliver)
		if result.Logs != "" {
			if persistErr := p.queue.UpdateOutput(job.ID, result.Logs); persistErr != nil {
				slog.Warn("job: failed to persist output", "id", job.ID, "err", persistErr)
			}
		}
		return err
	}

	if job.SessionID != "" && (job.Attempts > 1 || job.ResumeNote != "") {
		prompt = "Continue the unfinished scheduled task from this existing session. Complete any remaining work for the original request:\n\n" + job.Prompt
	}
	if job.ResumeNote != "" {
		prompt = job.ResumeNote + "\n\n" + prompt
		if err := p.queue.ClearResumeNote(job.ID); err != nil {
			slog.Warn("job: failed to clear resume note", "id", job.ID, "err", err)
		}
	}

	var lastErr error
	var reply strings.Builder
	var logs jobLogBuilder
	live := &liveJobOutput{jobID: job.ID, agentID: job.AgentID}
	startedAt := time.Now().UTC()
	idle := taskIdleTimeout(job, runner.Config())
	runErr := runPromptWatched(ctx, idle, func(ctx context.Context, consume agent.StreamConsumer) {
		runner.Prompt(ctx, prompt, consume)
	}, func(e agent.StreamEvent) {
		switch e.Type {
		case agent.StreamEventText:
			reply.WriteString(e.Text)
			live.Text(e.Text)
		case agent.StreamEventStatus:
			logs.Addf("status: %s", e.Text)
			live.Line("status: %s", e.Text)
		case agent.StreamEventTool:
			if e.Tool != nil && !e.Tool.Done {
				live.Line("tool: %s", e.Tool.Name)
			}
		case agent.StreamEventMedia:
			logs.Addf("media: %s", e.MediaURL)
			live.Line("media: %s", e.MediaURL)
		case agent.StreamEventDone:
			live.Flush()
		case agent.StreamEventStop:
			logs.Addf("stopped")
			live.Line("stopped")
		case agent.StreamEventError:
			lastErr = e.Err
			logs.Addf("error: %v", e.Err)
			live.Line("error: %v", e.Err)
		}
	})
	var stalled *jobStalledError
	switch {
	case errors.As(runErr, &stalled):
		// The agent went quiet: stop it and fail the run with what it did.
		if sessionID != "" {
			agent.StopSession(job.AgentID, sessionID)
		}
		live.Line("error: %v", runErr)
		logs.Addf("error: %v", runErr)
		lastErr = runErr
	case runErr != nil:
		return runErr
	}
	if sessionID != "" {
		sessionLogs, err := collectJobSessionToolLogs(job.AgentID, sessionID, startedAt, time.Now().UTC().Add(time.Second))
		if err != nil {
			logs.Addf("error collecting tool logs: %v", err)
		} else {
			logs.AddBlock(sessionLogs)
		}
	}
	output := logs.String()
	if output != "" {
		if err := p.queue.UpdateOutput(job.ID, output); err != nil {
			slog.Warn("job: failed to persist output", "id", job.ID, "err", err)
		}
	}
	replyOutput := reply.String()
	if replyOutput != "" {
		// Reply to the originating session/channel if one was recorded.
		if job.ReplyAgentID != "" && job.ReplySessionID != "" {
			if err := agent.AppendReplyToSession(job.ReplyAgentID, job.ReplySessionID, replyOutput); err != nil {
				slog.Warn("job: failed to send reply to session", "id", job.ID, "session", job.ReplySessionID, "err", err)
			}
		}
		if agent.ShouldDeliverReply(replyOutput) && job.OutputChannel != "" && p.deliver != nil {
			if err := p.deliver(job.AgentID, job.OutputChannel, replyOutput); err != nil {
				slog.Warn("job: failed to deliver output to task channel", "id", job.ID, "route", job.OutputChannel, "err", err)
			}
		}
	}
	return lastErr
}

func resolveTaskExecution(job *domain.Job, cfg *config.AgentConfig) (taskType, prompt, script string) {
	taskType = strings.ToLower(strings.TrimSpace(job.TaskType))
	prompt = job.Prompt
	script = job.Prompt
	if taskType != "" {
		return taskType, prompt, script
	}
	if cfg == nil {
		return "prompt", prompt, script
	}
	taskName := jobSessionName(job)
	for _, task := range cfg.Tasks {
		if task.Name != taskName {
			continue
		}
		taskType = strings.ToLower(strings.TrimSpace(task.Type))
		if taskType == "" {
			taskType = "prompt"
		}
		if strings.TrimSpace(prompt) == "" {
			prompt = task.Prompt
		}
		if strings.TrimSpace(script) == "" {
			script = task.Prompt
		}
		return taskType, prompt, script
	}
	return "prompt", prompt, script
}

// executeProjectJob hands a project task to the Code Loop engine, which runs
// its prompt in an isolated clone as a board job, and records the run's
// summary as the job's output, in the task session, and at its target.
func (p *WorkerPool) executeProjectJob(ctx context.Context, job *domain.Job, cfg *config.AgentConfig) error {
	if p.projectRunner == nil {
		return errors.New("project tasks need the Code Loop engine, which is not running")
	}
	run := ProjectRun{TaskID: job.TaskID, Name: jobSessionName(job), Prompt: job.Prompt, Fire: time.Now()}
	precompile := false
	if cfg != nil {
		for _, task := range cfg.Tasks {
			if task.Name == run.Name {
				run.Project, run.Model = strings.TrimSpace(task.Project), strings.TrimSpace(task.Model)
				if strings.TrimSpace(run.Prompt) == "" {
					run.Prompt = task.Prompt
				}
				precompile = config.BoolOr(task.Precompile, false)
				break
			}
		}
	}
	if run.Project == "" {
		return fmt.Errorf("project task %q names no project", job.TaskID)
	}
	var compiledErr error
	if precompile && p.projectCompiler != nil && cfg != nil {
		jobCtx := ctx
		run.RunCompiled = func(runCtx context.Context, clone func(context.Context) (string, error), logf func(string, ...any)) (string, error) {
			// The script runs as the job's agent, in its task session, which
			// the job's context names; the runner's context only stops it.
			scriptCtx, cancel := context.WithCancelCause(jobCtx)
			defer cancel(nil)
			stop := context.AfterFunc(runCtx, func() { cancel(context.Cause(runCtx)) })
			defer stop()
			output, err := p.runCompiledProjectTask(scriptCtx, job, cfg.Name, run, clone, logf)
			compiledErr = err
			return output, err
		}
	}
	summary, err := p.projectRunner(ctx, run)
	if compiledErr != nil && !errors.Is(compiledErr, ErrProjectTaskNotCompiled) {
		// The fallback is part of the run's result, not only the board log.
		note := "Precompiled script failed, so the model ran the task: " + compiledErr.Error()
		summary = strings.TrimSpace(note + "\n" + summary)
		slog.Warn("job: precompiled project task fell back to the model", "id", job.ID, "task", job.TaskID, "err", compiledErr)
	}
	if summary != "" {
		if persistErr := p.queue.UpdateOutput(job.ID, summary); persistErr != nil {
			slog.Warn("job: failed to persist output", "id", job.ID, "err", persistErr)
		}
		if deliverErr := recordTaskOutput(job, summary, p.deliver); deliverErr != nil {
			slog.Warn("job: failed to deliver project task output", "id", job.ID, "route", job.OutputChannel, "err", deliverErr)
		}
	}
	return err
}

// runCompiledProjectTask runs a precompiled project task's script in checkout,
// a fresh clone of its project, with the same environment the task's agent
// gets: its tools and exec permissions, the clone granted to them and their
// default directory, and the clone and repository in the command env.
func (p *WorkerPool) runCompiledProjectTask(ctx context.Context, job *domain.Job, agentName string, run ProjectRun, clone func(context.Context) (string, error), logf func(string, ...any)) (string, error) {
	script, err := p.projectCompiler(ctx, agentName, run.Name)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(script) == "" {
		return "", ErrProjectTaskNotCompiled
	}
	checkout, err := clone(ctx)
	if err != nil {
		return "", fmt.Errorf("cloning %s: %w", run.Project, err)
	}
	logf("running the precompiled script in %s", checkout)
	ctx = agent.WithFilesystemGrant(ctx, checkout)
	ctx = agent.WithCommandEnv(ctx, map[string]string{"AVIARY_CHECKOUT_DIRECTORY": checkout, "GH_REPO": run.Project})
	scriptJob := *job
	scriptJob.Script = script
	return executeScriptJobOutput(ctx, &scriptJob, logf)
}

// SetTaskOutputDelivery configures the callback used to forward completed task output.
func (p *WorkerPool) SetTaskOutputDelivery(fn func(agentName, route, text string) error) {
	p.deliver = fn
}

func (p *WorkerPool) registerActiveJob(jobID, taskID, agentID string, cancel context.CancelFunc) {
	p.activeMu.Lock()
	defer p.activeMu.Unlock()
	p.active[jobID] = activeJob{taskID: taskID, agentID: agentID, cancel: cancel}
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
			agent.StopSession(job.agentID, job.sessionID)
		}
		if job.cancel != nil {
			job.cancel()
		}
		stopped++
	}
	return stopped
}
