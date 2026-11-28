package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
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
	deliver  TaskOutputDelivery
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
	// taskCompiler compiles precompiled prompt and project tasks.
	taskCompiler TaskCompiler
	// scriptReporter records how each precompiled script run ended.
	scriptReporter TaskScriptReporter
	// workflowReply sends a workflow run's output back to the conversation
	// whose message triggered it, for the "reply" target.
	workflowReply func(agentName string, tc domain.TriggerContext, text string) error
	// workflowAfter returns the messages that followed a workflow's
	// triggering message, recorded with the run when it ends.
	workflowAfter func(agentName string, tc domain.TriggerContext) []domain.TriggerMessage
}

// ProjectRun is one fire of a "project" task: its prompt, run as a job on the
// board of a watched project.
type ProjectRun struct {
	// TaskID is the task's "agent/task" key, which a board Retry passes back
	// to Trigger.
	TaskID string
	// JobID is the scheduler job the fire runs as, which the board job
	// records so the task's run pages can show its output (issue #1646).
	JobID   string
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

// ErrProjectRunInterrupted is a ProjectRunner's error for a run the Code Loop
// engine stopped because it was restarting or shutting down, not because the
// run was stopped. The job is requeued to run again once the engine is back,
// instead of being canceled (issue #1028).
var ErrProjectRunInterrupted = errors.New("the Code Loop engine stopped while the run was going")

// projectRunInterruptedNote is the resume note of a project task job requeued
// after ErrProjectRunInterrupted.
const projectRunInterruptedNote = "This scheduled run was interrupted when the Code Loop engine restarted before it finished, and is running again. Check for work the interrupted run already did before repeating it."

// TaskCompiler returns a precompiled project task's current script,
// compiling it first when its prompt, project or tools changed; "" means its
// prompt is not deterministic.
type TaskCompiler func(ctx context.Context, agentName, taskName string) (string, error)

// TaskScriptReporter records how a precompiled project task's script
// run ended: err is nil when it completed.
type TaskScriptReporter func(agentName, taskName string, err error) error

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

// Halt stops workers from claiming queued jobs. Running jobs keep running;
// each worker exits once its current job ends.
func (p *WorkerPool) Halt() {
	p.stopOnce.Do(func() { close(p.stop) })
}

// Stop signals all workers to exit and waits for them.
func (p *WorkerPool) Stop() {
	p.Halt()
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
	defer p.queue.markRunning(job.ID)()

	stopHeartbeat := make(chan struct{})
	defer close(stopHeartbeat)
	go p.heartbeatJob(jobCtx, job.ID, stopHeartbeat)

	slog.Info("executing job", "id", job.ID, "task", job.TaskID, "agent", job.AgentID)
	started := time.Now()
	defer p.attemptDone(job.ID, started)
	if err := p.executeJob(jobCtx, job); err != nil {
		if errors.Is(err, ErrProjectRunInterrupted) && jobCtx.Err() == nil {
			p.requeueInterrupted(job.ID)
			return
		}
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

// requeueInterrupted requeues a project task job whose Code Loop engine
// stopped under it, so it runs again once the engine is back. A safe
// restart's note wins, since that restart is what stopped the engine.
func (p *WorkerPool) requeueInterrupted(jobID string) {
	note := projectRunInterruptedNote
	if checkpoint := p.checkpointNote.Load(); checkpoint != nil {
		note = *checkpoint
	}
	slog.Info("requeueing project task job its engine interrupted", "id", jobID)
	if err := p.queue.Requeue(jobID, note); err != nil {
		slog.Warn("requeueing interrupted project task job", "id", jobID, "err", err)
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
	if tc := job.TriggerContext; tc != nil {
		// Usage is attributed to the workflow and the channel that fired it.
		ctx = agent.WithUsageChannel(ctx, tc.Channel)
		defer p.recordWorkflowAfter(job)
	}
	deliver := p.deliverFor(job)
	// A choice not to reply that the run did not get to act on, such as one
	// made by a precompiled script that then failed, ends with the run.
	defer agent.TakeSkippedReply(job.ID)

	taskType, prompt, _ := resolveTaskExecution(job, runner.Config())
	if taskType == "project" {
		return p.executeProjectJob(ctx, job, runner.Config())
	}
	if taskType == "script" {
		result, err := executeScriptJob(ctx, job, runner.Config(), deliver, func() bool { return p.skipReply(job) })
		if result.Logs != "" {
			if persistErr := p.queue.UpdateOutput(job.ID, result.Logs); persistErr != nil {
				slog.Warn("job: failed to persist output", "id", job.ID, "err", persistErr)
			}
		}
		return err
	}

	// A precompiled prompt task or workflow runs its script first; the model
	// runs it only when there is no script or the script failed.
	scriptLogs, handled, err := p.runCompiledPrompt(ctx, job, runner.Config(), deliver)
	if handled {
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
	prompt += replyNote(job)

	var lastErr error
	var reply strings.Builder
	var logs jobLogBuilder
	logs.AddBlock(scriptLogs)
	live := &liveJobOutput{jobID: job.ID, agentID: job.AgentID}
	startedAt := time.Now().UTC()
	idle := taskIdleTimeout(job, runner.Config())
	overrides := agent.RunOverrides{Model: workflowModel(job, runner.Config())}
	runErr := runPromptWatched(ctx, idle, func(ctx context.Context, consume agent.StreamConsumer) {
		runner.PromptWithOverrides(ctx, prompt, overrides, consume)
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
	if p.skipReply(job) {
		return lastErr
	}
	replyOutput := reply.String()
	if replyOutput != "" {
		// Reply to the originating session/channel if one was recorded.
		if job.ReplyAgentID != "" && job.ReplySessionID != "" {
			if err := agent.AppendReplyToSession(job.ReplyAgentID, job.ReplySessionID, replyOutput); err != nil {
				slog.Warn("job: failed to send reply to session", "id", job.ID, "session", job.ReplySessionID, "err", err)
			}
		}
		if agent.ShouldDeliverReply(replyOutput) && job.OutputChannel != "" && deliver != nil {
			if err := deliver(job.AgentID, job.OutputChannel, replyOutput); err != nil {
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
	run := ProjectRun{TaskID: job.TaskID, JobID: job.ID, Name: jobSessionName(job), Prompt: job.Prompt, Fire: time.Now()}
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
	if job.ResumeNote != "" {
		run.Prompt = job.ResumeNote + "\n\n" + run.Prompt
		if err := p.queue.ClearResumeNote(job.ID); err != nil {
			slog.Warn("job: failed to clear resume note", "id", job.ID, "err", err)
		}
	}
	var compiledErr error
	scriptExecuted := false
	if precompile && p.taskCompiler != nil && cfg != nil {
		jobCtx := ctx
		run.RunCompiled = func(runCtx context.Context, clone func(context.Context) (string, error), logf func(string, ...any)) (string, error) {
			// The script runs as the job's agent, in its task session, which
			// the job's context names; the runner's context only stops it.
			scriptCtx, cancel := context.WithCancelCause(jobCtx)
			defer cancel(nil)
			stop := context.AfterFunc(runCtx, func() { cancel(context.Cause(runCtx)) })
			defer stop()
			output, executed, err := p.runCompiledProjectTask(scriptCtx, compiledProjectTask{
				job:       job,
				agentName: cfg.Name,
				run:       run,
				clone:     clone,
				logf:      logf,
			})
			compiledErr = err
			// Only the script's own result counts toward its failures, not
			// a failed compile or clone, nor a stopped run.
			scriptExecuted = executed && runCtx.Err() == nil
			return output, err
		}
	}
	summary, err := p.projectRunner(ctx, run)
	if scriptExecuted && ctx.Err() == nil && p.scriptReporter != nil {
		if reportErr := p.scriptReporter(cfg.Name, run.Name, compiledErr); reportErr != nil {
			slog.Warn("job: failed to record precompiled script result", "id", job.ID, "task", job.TaskID, "err", reportErr)
		}
	}
	scriptErr := ""
	if compiledErr != nil && !errors.Is(compiledErr, ErrProjectTaskNotCompiled) {
		scriptErr = compiledErr.Error()
	}
	p.setScriptError(job, scriptErr)
	if scriptErr != "" {
		// The run's summary and chat message are the model's result plus a
		// short note; the full error, often a Lua traceback with gh's JSON
		// body, stays in the run's logs.
		summary = fallbackSummary(summary, compiledErr)
		slog.Warn("job: precompiled project task fell back to the model", "id", job.ID, "task", job.TaskID, "err", compiledErr)
	}
	deliver := p.deliverFor(job)
	if p.skipReply(job) {
		deliver = nil
	}
	if summary != "" {
		if persistErr := p.queue.UpdateOutput(job.ID, summary); persistErr != nil {
			slog.Warn("job: failed to persist output", "id", job.ID, "err", persistErr)
		}
		if deliverErr := recordTaskOutput(job, summary, deliver); deliverErr != nil {
			slog.Warn("job: failed to deliver project task output", "id", job.ID, "route", job.OutputChannel, "err", deliverErr)
		}
	}
	return err
}

// runCompiledPrompt runs a precompiled prompt task's compiled script for
// job; a workflow's script reads the triggering message from the Lua global
// trigger. handled is true when the script completed, or the run was
// stopped, so the model does not run; err is then the run's result.
// Otherwise the run falls back to the model: logs say what the script did,
// and job.ScriptError why it failed, which marks the run as a fallback. A
// task whose prompt is not deterministic simply runs on the model.
func (p *WorkerPool) runCompiledPrompt(ctx context.Context, job *domain.Job, cfg *config.AgentConfig, deliver func(agentName, route, text string) error) (logs string, handled bool, err error) {
	if p.taskCompiler == nil || cfg == nil {
		return "", false, nil
	}
	name := jobSessionName(job)
	precompile := false
	for _, task := range cfg.Tasks {
		if task.Name == name && task.PrecompilesBesidePrompt() {
			precompile = config.BoolOr(task.Precompile, false)
			break
		}
	}
	if !precompile {
		return "", false, nil
	}
	var b jobLogBuilder
	script, runErr := p.taskCompiler(ctx, cfg.Name, name)
	executed := false
	output := ""
	if runErr == nil {
		if strings.TrimSpace(script) == "" {
			p.setScriptError(job, "")
			return "", false, nil
		}
		b.Addf("running the precompiled script")
		scriptJob := *job
		scriptJob.Script = script
		output, runErr = retryTransientScript(ctx, b.Addf, func() (string, error) {
			return executeScriptJobOutput(ctx, &scriptJob, b.Addf)
		})
		executed = true
	}
	// Only the script's own result counts toward its failures, not a failed
	// compile nor a stopped run.
	if executed && ctx.Err() == nil && p.scriptReporter != nil {
		if reportErr := p.scriptReporter(cfg.Name, name, runErr); reportErr != nil {
			slog.Warn("job: failed to record precompiled script result", "id", job.ID, "task", job.TaskID, "err", reportErr)
		}
	}
	if ctx.Err() != nil {
		return "", true, ctx.Err()
	}
	if runErr == nil {
		p.setScriptError(job, "")
		if persistErr := p.queue.UpdateOutput(job.ID, b.String()); persistErr != nil {
			slog.Warn("job: failed to persist output", "id", job.ID, "err", persistErr)
		}
		if p.skipReply(job) {
			deliver = nil
		}
		if output == "" {
			return b.String(), true, nil
		}
		return b.String(), true, recordTaskOutput(job, output, deliver)
	}
	// The model reruns the task and decides for itself whether to reply.
	agent.TakeSkippedReply(job.ID)
	p.setScriptError(job, runErr.Error())
	b.Addf("precompiled script failed (%s); falling back to the model", ProjectScriptFailureCause(runErr))
	slog.Warn("job: precompiled prompt task fell back to the model", "id", job.ID, "task", job.TaskID, "err", runErr)
	return b.String(), false, nil
}

// setScriptError records the attempt's script error on job, or clears a
// retried job's earlier one.
func (p *WorkerPool) setScriptError(job *domain.Job, scriptErr string) {
	if scriptErr == "" && job.ScriptError == "" {
		return
	}
	if err := p.queue.SetScriptError(job.ID, scriptErr); err != nil {
		slog.Warn("job: failed to persist script error", "id", job.ID, "err", err)
	}
	job.ScriptError = scriptErr
}

// fallbackSummary is a fallback run's summary: the model's result, then a
// short note on why the precompiled script did not run it, in its own
// paragraph so the chat shows it on its own line.
func fallbackSummary(summary string, scriptErr error) string {
	note := "_Precompiled script failed (" + ProjectScriptFailureCause(scriptErr) + "), fell back to the model; details in the run logs._"
	if summary = strings.TrimSpace(summary); summary == "" {
		return note
	}
	return summary + "\n\n" + note
}

// compiledProjectTask is one run of a precompiled project task's script.
type compiledProjectTask struct {
	job       *domain.Job
	agentName string
	run       ProjectRun
	clone     func(context.Context) (string, error) // makes a fresh clone of the project
	logf      func(string, ...any)
}

// runCompiledProjectTask runs a precompiled project task's script in checkout,
// a fresh clone of its project, with the same environment the task's agent
// gets: its tools and exec permissions, the clone granted to them and their
// default directory, and the clone and repository in the command env.
func (p *WorkerPool) runCompiledProjectTask(ctx context.Context, task compiledProjectTask) (output string, executed bool, err error) {
	run, logf := task.run, task.logf
	script, err := p.taskCompiler(ctx, task.agentName, run.Name)
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(script) == "" {
		return "", false, ErrProjectTaskNotCompiled
	}
	checkout, err := task.clone(ctx)
	if err != nil {
		return "", false, fmt.Errorf("cloning %s: %w", run.Project, err)
	}
	logf("running the precompiled script in %s", checkout)
	ctx = agent.WithProjectTaskScript(agent.WithFilesystemGrant(ctx, checkout))
	ctx = agent.WithCommandEnv(ctx, map[string]string{"AVIARY_CHECKOUT_DIRECTORY": checkout, "GH_REPO": run.Project})
	scriptJob := *task.job
	scriptJob.Script = script
	output, err = retryTransientScript(ctx, logf, func() (string, error) {
		return executeScriptJobOutput(ctx, &scriptJob, logf)
	})
	return output, true, err
}

// skipReply reports whether job chose not to reply, through the
// task_no_reply tool, and if so records the skip and its reason as the run's
// delivery, so the run log shows it in place of a delivery.
func (p *WorkerPool) skipReply(job *domain.Job) bool {
	reason, skipped := agent.TakeSkippedReply(job.ID)
	if !skipped {
		return false
	}
	if reason == "" {
		reason = "the run chose not to reply"
	}
	delivery := &domain.JobDelivery{Target: job.OutputChannel, Status: domain.DeliverySkipped, Message: reason, At: time.Now().UTC()}
	slog.Info("job: skipped reply", "id", job.ID, "task", job.TaskID, "target", delivery.Target, "reason", reason)
	if err := p.queue.SetDelivery(job.ID, delivery); err != nil {
		slog.Warn("job: failed to record skipped reply", "id", job.ID, "err", err)
	}
	job.Delivery = delivery
	return true
}

// replyNote tells a run whose final message is delivered somewhere that it
// is sent verbatim, and how to end the run without replying instead.
func replyNote(job *domain.Job) string {
	route := strings.TrimSpace(job.OutputChannel)
	where := ""
	switch {
	case config.IsReplyTarget(route) && job.TriggerContext != nil:
		where = "as a reply to the conversation that triggered this workflow"
	case route != "" && !strings.EqualFold(route, config.TargetSilent):
		where = "to this task's output target (" + route + ")"
	case job.ReplySessionID != "":
		where = "as a reply to the conversation that started this run"
	default:
		return ""
	}
	return "\n\n---\nYour final message is sent verbatim " + where + ". " +
		"If you decide not to reply, or there is nothing to report, call the " + taskNoReplyTool + " tool with a short reason instead of writing a message that says so; " +
		"any such message would itself be sent as the reply."
}

// taskNoReplyTool is the MCP tool a run calls to end without replying.
const taskNoReplyTool = "task_no_reply"

// TaskOutputDelivery sends a task or workflow run's output to its target and
// reports the outcome for the run log; a silent target reports nil.
type TaskOutputDelivery func(source domain.OutputSource, agentName, route, text string) (*domain.JobDelivery, error)

// deliverFor returns how job's output reaches its target: the "reply" target
// of a workflow run answers in the conversation that triggered it, and every
// other target goes through the task output delivery, whose outcome is
// recorded on the run.
func (p *WorkerPool) deliverFor(job *domain.Job) func(agentName, route, text string) error {
	return func(agentName, route, text string) error {
		if !config.IsReplyTarget(route) {
			if p.deliver == nil {
				return nil
			}
			source := domain.OutputSource{Task: job.TaskID, JobID: job.ID, Workflow: job.TriggerContext != nil}
			delivery, err := p.deliver(source, agentName, route, text)
			if delivery != nil {
				slog.Info("job: output delivery", "id", job.ID, "task", job.TaskID, "target", delivery.Target, "status", delivery.Status, "message", delivery.Message)
				if setErr := p.queue.SetDelivery(job.ID, delivery); setErr != nil {
					slog.Warn("job: failed to record output delivery", "id", job.ID, "err", setErr)
				}
			}
			return err
		}
		if job.TriggerContext == nil {
			return fmt.Errorf("only a run triggered by a channel message can reply to it")
		}
		if p.workflowReply == nil {
			return errors.New("workflow replies are not available")
		}
		return p.workflowReply(agentName, *job.TriggerContext, text)
	}
}

// workflowModel is the model override of the workflow job runs, or "".
func workflowModel(job *domain.Job, cfg *config.AgentConfig) string {
	if job.TriggerContext == nil || cfg == nil {
		return ""
	}
	name := jobSessionName(job)
	for _, task := range cfg.Tasks {
		if task.Name == name && task.IsWorkflow() {
			return strings.TrimSpace(task.Model)
		}
	}
	return ""
}

// recordWorkflowAfter records the messages that followed a workflow run's
// triggering message once the run ends, completing its trigger context.
func (p *WorkerPool) recordWorkflowAfter(job *domain.Job) {
	if p.workflowAfter == nil || job.TriggerContext == nil {
		return
	}
	after := p.workflowAfter(job.AgentID, *job.TriggerContext)
	if len(after) == 0 {
		return
	}
	tc := *job.TriggerContext
	tc.After = after
	if err := p.queue.SetTriggerContext(job.ID, &tc); err != nil {
		slog.Warn("job: failed to record messages after the trigger", "id", job.ID, "err", err)
	}
}

// SetTaskOutputDelivery configures the callback used to forward completed task output.
func (p *WorkerPool) SetTaskOutputDelivery(fn TaskOutputDelivery) {
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

// httpStatusPattern finds the status gh reports for a failed API call.
var httpStatusPattern = regexp.MustCompile(`\(HTTP (\d{3})\)`)

// luaLocationPattern matches the "<string>:52: " location Lua puts before an
// error raised by a script.
var luaLocationPattern = regexp.MustCompile(`^(?:<string>|\[string "[^"]*"\]):\d+:\s*`)

// ProjectScriptFailureCause is a short description of why a compiled script
// failed, for a run's summary and the task's note: "gh 404" for a failed gh
// API call, otherwise the error's first line without its Lua location,
// shortened. The full error belongs in the run's logs.
func ProjectScriptFailureCause(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if m := httpStatusPattern.FindStringSubmatch(text); m != nil {
		return "gh " + m[1]
	}
	line, _, _ := strings.Cut(text, "\n")
	line = strings.TrimSpace(luaLocationPattern.ReplaceAllString(strings.TrimSpace(line), ""))
	if r := []rune(line); len(r) > 80 {
		line = string(r[:79]) + "…"
	}
	if line == "" {
		return "error"
	}
	return line
}
