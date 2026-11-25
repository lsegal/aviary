package scheduler

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// newAlphaPool returns a worker pool whose manager runs agent "alpha", which
// has no provider and so answers every prompt with a configuration error.
func newAlphaPool(t *testing.T, tasks ...config.TaskConfig) (*WorkerPool, *JobQueue) {
	t.Helper()
	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "alpha", Model: "m", Tasks: tasks}}})
	q := NewJobQueue()
	return NewWorkerPool(q, mgr, 1), q
}

func TestNewWorkerPoolDefaultsToGOMAXPROCS(t *testing.T) {
	assert.Equal(t, runtime.GOMAXPROCS(0), NewWorkerPool(NewJobQueue(), nil, 0).n)
	assert.Equal(t, 3, NewWorkerPool(NewJobQueue(), nil, 3).n)
}

func TestWorkerPool_RunClaimsAndCompletesQueuedJobs(t *testing.T) {
	setupSchedulerDataDir(t)
	p, q := newAlphaPool(t)
	job, err := q.Enqueue("alpha/daily", "alpha", "hello", "", 1, "", "")
	require.NoError(t, err)

	p.Start(context.Background())
	assert.Eventually(t, func() bool {
		return readJob(t, job.ID).Status == domain.JobStatusCompleted
	}, 10*time.Second, 10*time.Millisecond)

	// A job run outside the queue once the pool started completes too.
	now, err := q.StartImmediate("alpha/adhoc", "alpha", "hello", "", "", "")
	require.NoError(t, err)
	p.ExecuteNow(now)
	assert.Eventually(t, func() bool {
		return readJob(t, now.ID).Status == domain.JobStatusCompleted
	}, 10*time.Second, 10*time.Millisecond)
	p.Stop()
}

func TestWorkerPool_RunClaimsNothingWhenStopped(t *testing.T) {
	// Each case's workers exit on their own before Stop is called.
	for _, tc := range []struct {
		name  string
		start func(p *WorkerPool)
	}{
		{"halted before start", func(p *WorkerPool) { p.Halt(); p.Start(context.Background()) }},
		{"context canceled", func(p *WorkerPool) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			p.Start(ctx)
		}},
		{"held for a safe restart", func(p *WorkerPool) {
			ctx, cancel := context.WithCancel(context.Background())
			p.held.Store(true)
			p.Start(ctx)
			cancel()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupSchedulerDataDir(t)
			p, q := newAlphaPool(t)
			job, err := q.Enqueue("alpha/daily", "alpha", "hello", "", 1, "", "")
			require.NoError(t, err)

			tc.start(p)
			p.wg.Wait()
			p.Stop()
			saved := readJob(t, job.ID)
			assert.Equal(t, domain.JobStatusPending, saved.Status)
			assert.Zero(t, saved.Attempts)
		})
	}
}

func TestWorkerPool_ProcessJobFailureSchedulesRetry(t *testing.T) {
	setupSchedulerDataDir(t)
	p, q := newAlphaPool(t)
	var attempts []domain.Job
	p.onAttemptDone = func(job domain.Job, _ time.Duration) { attempts = append(attempts, job) }

	job, err := q.Enqueue("ghost/daily", "ghost", "hello", "", 2, "", "")
	require.NoError(t, err)
	claimed, err := q.Claim()
	require.NoError(t, err)
	p.processJob(context.Background(), claimed)

	saved := readJob(t, job.ID)
	assert.Equal(t, domain.JobStatusPending, saved.Status, "an attempt remains")
	assert.Equal(t, `agent "ghost" not found`, saved.Error)
	assert.NotNil(t, saved.NextRetryAt)
	require.Len(t, attempts, 1)
	assert.Equal(t, saved.Error, attempts[0].Error, "the observer sees the job as persisted")

	// A job that is not on disk, or no longer readable, has nothing to
	// report once it ends, whether it failed or succeeded.
	p.processJob(context.Background(), &domain.Job{ID: "job_gone", AgentID: "ghost"})
	p.processJob(context.Background(), &domain.Job{ID: "job_gone_ok", TaskID: "alpha/adhoc", AgentID: "alpha", Prompt: "hi"})
	require.NoError(t, os.WriteFile(store.JobPath("alpha", "job_corrupt"), []byte("not-json"), 0o600))
	p.processJob(context.Background(), &domain.Job{ID: "job_corrupt", AgentID: "ghost"})
	assert.Len(t, attempts, 1)
}

func TestWorkerPool_InterruptedRunDuringCheckpointGetsRestartNote(t *testing.T) {
	setupSchedulerDataDir(t)
	p, q := newAlphaPool(t, config.TaskConfig{Name: "triage", Type: "project", Project: "o/r", Prompt: "Triage"})
	p.projectRunner = func(context.Context, ProjectRun) (string, error) { return "", ErrProjectRunInterrupted }
	note := "Aviary restarted; carry on."
	p.checkpointNote.Store(&note)

	_, err := q.EnqueueWithType("alpha/triage", "project", "alpha", "Triage", "", "", 2, "", "")
	require.NoError(t, err)
	claimed, err := q.Claim()
	require.NoError(t, err)
	p.processJob(context.Background(), claimed)

	saved := readJob(t, claimed.ID)
	assert.Equal(t, domain.JobStatusPending, saved.Status)
	assert.Equal(t, note, saved.ResumeNote, "the safe restart's note wins over the engine's")
}

func TestWorkerPool_ExecuteJob_ScriptTaskPersistsLogs(t *testing.T) {
	setupSchedulerDataDir(t)
	p, q := newAlphaPool(t)
	job, err := q.EnqueueWithType("alpha/lua", "script", "alpha", "", `error("feed down")`, "", 1, "", "")
	require.NoError(t, err)

	err = p.executeJob(context.Background(), job)
	assert.ErrorContains(t, err, "feed down")
	assert.Contains(t, readJob(t, job.ID).Output, "feed down")
}

func TestWorkerPool_ExecuteJob_DeliversReplyToTarget(t *testing.T) {
	setupSchedulerDataDir(t)
	p, q := newAlphaPool(t)
	var delivered []string
	p.SetTaskOutputDelivery(func(source domain.OutputSource, _, route, text string) (*domain.JobDelivery, error) {
		delivered = append(delivered, source.Task+"|"+route+"|"+text)
		return &domain.JobDelivery{Target: route, Status: domain.DeliveryDelivered}, nil
	})
	job, err := q.Enqueue("alpha/daily", "alpha", "hello", "session:ops", 1, "", "")
	require.NoError(t, err)

	require.NoError(t, p.executeJob(context.Background(), job))
	require.Len(t, delivered, 1)
	assert.True(t, strings.HasPrefix(delivered[0], "alpha/daily|session:ops|"))
	assert.Contains(t, delivered[0], "no LLM provider configured")
	saved := readJob(t, job.ID)
	require.NotNil(t, saved.Delivery)
	assert.Equal(t, domain.DeliveryDelivered, saved.Delivery.Status)
}

func TestWorkerPool_ExecuteJob_SkippedReplyIsRecorded(t *testing.T) {
	setupSchedulerDataDir(t)
	p, q := newAlphaPool(t)
	var delivered int
	p.SetTaskOutputDelivery(func(domain.OutputSource, string, string, string) (*domain.JobDelivery, error) {
		delivered++
		return nil, nil
	})
	job, err := q.Enqueue("alpha/daily", "alpha", "hello", "session:ops", 1, "", "")
	require.NoError(t, err)
	agent.SkipReply(job.ID, "")

	require.NoError(t, p.executeJob(context.Background(), job))
	assert.Zero(t, delivered)
	saved := readJob(t, job.ID)
	require.NotNil(t, saved.Delivery)
	assert.Equal(t, domain.DeliverySkipped, saved.Delivery.Status)
	assert.Equal(t, "the run chose not to reply", saved.Delivery.Message)
}

func TestWorkerPool_ExecuteJob_WorkflowRecordsMessagesAfterTrigger(t *testing.T) {
	setupSchedulerDataDir(t)
	p, q := newAlphaPool(t, config.TaskConfig{Name: "triage", Trigger: config.TriggerMessage, Channel: "team", Prompt: "Answer", Target: "reply"})
	var replies []string
	var afterFor []string
	followUp := []domain.TriggerMessage{{Sender: "Ana", Text: "any update?"}}
	p.workflowReply = func(_ string, tc domain.TriggerContext, text string) error {
		replies = append(replies, tc.Entry+":"+text)
		return nil
	}
	p.workflowAfter = func(agentName string, tc domain.TriggerContext) []domain.TriggerMessage {
		afterFor = append(afterFor, agentName+":"+tc.Entry)
		return followUp
	}
	job, err := q.Enqueue("alpha/triage", "alpha", "Answer", "reply", 1, "", "")
	require.NoError(t, err)
	job.TriggerContext = &domain.TriggerContext{Channel: "team", Entry: "C1", Message: domain.TriggerMessage{Sender: "Ana", Text: "crash"}}
	require.NoError(t, q.SetTriggerContext(job.ID, job.TriggerContext))

	require.NoError(t, p.executeJob(context.Background(), job))
	require.Len(t, replies, 1)
	assert.True(t, strings.HasPrefix(replies[0], "C1:"), "the reply answers the triggering conversation")
	assert.Equal(t, []string{"alpha:C1"}, afterFor)
	saved := readJob(t, job.ID)
	require.NotNil(t, saved.TriggerContext)
	assert.Equal(t, followUp, saved.TriggerContext.After)
	assert.Equal(t, "crash", saved.TriggerContext.Message.Text)

	// With nothing after the trigger, the recorded context is left alone.
	p.workflowAfter = func(string, domain.TriggerContext) []domain.TriggerMessage { return nil }
	again, err := q.Enqueue("alpha/triage", "alpha", "Answer", "reply", 1, "", "")
	require.NoError(t, err)
	again.TriggerContext = &domain.TriggerContext{Entry: "C2"}
	require.NoError(t, p.executeJob(context.Background(), again))
	assert.Nil(t, readJob(t, again.ID).TriggerContext)

	// Nor is it without a hook to ask.
	p.workflowAfter = nil
	require.NoError(t, p.executeJob(context.Background(), again))
	assert.Nil(t, readJob(t, again.ID).TriggerContext)
	assert.Equal(t, []string{"alpha:C1"}, afterFor)
}

func TestWorkerPool_ExecuteJob_CompiledPromptSkipsModel(t *testing.T) {
	setupSchedulerDataDir(t)
	precompile := true
	p, q := newAlphaPool(t, config.TaskConfig{Name: "digest", Every: "1h", Prompt: "Post the digest", Target: "session:ops", Precompile: &precompile})
	p.taskCompiler = func(context.Context, string, string) (string, error) { return `print("digest ready")`, nil }
	var delivered []string
	p.SetTaskOutputDelivery(func(_ domain.OutputSource, _, _, text string) (*domain.JobDelivery, error) {
		delivered = append(delivered, text)
		return nil, nil
	})
	job, err := q.Enqueue("alpha/digest", "alpha", "Post the digest", "session:ops", 1, "", "")
	require.NoError(t, err)

	require.NoError(t, p.executeJob(context.Background(), job))
	assert.Equal(t, []string{"digest ready"}, delivered, "the script's output, not the model's")
	assert.Contains(t, readJob(t, job.ID).Output, "running the precompiled script")

	// A run stopped before its script finishes ends with the stop, and runs
	// no model.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg, _ := p.agents.Get("alpha")
	_, handled, err := p.runCompiledPrompt(ctx, job, cfg.Config(), p.deliverFor(job))
	assert.True(t, handled)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Len(t, delivered, 1)
}

func TestWorkerPool_PrecompiledProjectTaskStoppedOrSkipped(t *testing.T) {
	setupSchedulerDataDir(t)
	precompile := true
	p, q := newAlphaPool(t, config.TaskConfig{Name: "stale", Type: "project", Project: "o/r", Prompt: "Label", Target: "session:ops", Precompile: &precompile})
	p.taskCompiler = func(context.Context, string, string) (string, error) { return `print("Labeled 2 issues")`, nil }
	var reported []error
	p.scriptReporter = func(_, _ string, err error) error {
		reported = append(reported, err)
		return errors.New("note store unavailable")
	}
	var delivered int
	p.SetTaskOutputDelivery(func(domain.OutputSource, string, string, string) (*domain.JobDelivery, error) {
		delivered++
		return nil, nil
	})
	stopRun := false
	p.projectRunner = func(ctx context.Context, run ProjectRun) (string, error) {
		runCtx := ctx
		if stopRun {
			c, cancel := context.WithCancel(ctx)
			cancel()
			runCtx = c
		}
		return run.RunCompiled(runCtx, func(context.Context) (string, error) { return t.TempDir(), nil }, func(string, ...any) {})
	}
	job, err := q.EnqueueWithType("alpha/stale", "project", "alpha", "Label", "", "session:ops", 1, "", "")
	require.NoError(t, err)

	// A script result the reporter cannot record still completes the run;
	// the run chose not to reply, so nothing is delivered.
	agent.SkipReply(job.ID, "nothing new to report")
	require.NoError(t, p.executeJob(context.Background(), job))
	assert.Equal(t, []error{nil}, reported)
	assert.Zero(t, delivered)
	saved := readJob(t, job.ID)
	assert.Equal(t, "Labeled 2 issues", saved.Output)
	require.NotNil(t, saved.Delivery)
	assert.Equal(t, domain.DeliverySkipped, saved.Delivery.Status)
	assert.Equal(t, "nothing new to report", saved.Delivery.Message)

	// A run the runner stops is not the script's result.
	stopRun = true
	assert.Error(t, p.executeJob(context.Background(), job))
	assert.Len(t, reported, 1)
}

func TestWorkflowModel(t *testing.T) {
	cfg := &config.AgentConfig{Tasks: []config.TaskConfig{
		{Name: "triage", Trigger: config.TriggerMessage, Channel: "team", Model: " anthropic/opus "},
		{Name: "daily", Every: "1h", Model: "anthropic/haiku"},
	}}
	tc := &domain.TriggerContext{Entry: "C1"}
	for _, c := range []struct {
		name string
		job  domain.Job
		cfg  *config.AgentConfig
		want string
	}{
		{"workflow run", domain.Job{TaskID: "alpha/triage", TriggerContext: tc}, cfg, "anthropic/opus"},
		{"not a workflow run", domain.Job{TaskID: "alpha/triage"}, cfg, ""},
		{"no agent config", domain.Job{TaskID: "alpha/triage", TriggerContext: tc}, nil, ""},
		{"scheduled task", domain.Job{TaskID: "alpha/daily", TriggerContext: tc}, cfg, ""},
		{"unknown task", domain.Job{TaskID: "alpha/gone", TriggerContext: tc}, cfg, ""},
	} {
		assert.Equal(t, c.want, workflowModel(&c.job, c.cfg), c.name)
	}
}

func TestResolveTaskExecution(t *testing.T) {
	cfg := &config.AgentConfig{Tasks: []config.TaskConfig{
		{Name: "lua", Type: " Script ", Prompt: `print("x")`},
		{Name: "plain", Prompt: "configured prompt"},
	}}
	for _, c := range []struct {
		name                     string
		job                      domain.Job
		cfg                      *config.AgentConfig
		taskType, prompt, script string
	}{
		{"job type wins", domain.Job{TaskID: "alpha/lua", TaskType: " Project ", Prompt: "p"}, cfg, "project", "p", "p"},
		{"no config", domain.Job{TaskID: "alpha/lua", Prompt: "p"}, nil, "prompt", "p", "p"},
		{"type and body from config", domain.Job{TaskID: "alpha/lua"}, cfg, "script", `print("x")`, `print("x")`},
		{"job prompt kept", domain.Job{TaskID: "alpha/plain", Prompt: "job prompt"}, cfg, "prompt", "job prompt", "job prompt"},
		{"untyped task is a prompt", domain.Job{TaskID: "alpha/plain"}, cfg, "prompt", "configured prompt", "configured prompt"},
		{"unknown task", domain.Job{TaskID: "alpha/gone", Prompt: "p"}, cfg, "prompt", "p", "p"},
	} {
		taskType, prompt, script := resolveTaskExecution(&c.job, c.cfg)
		assert.Equal(t, []string{c.taskType, c.prompt, c.script}, []string{taskType, prompt, script}, c.name)
	}
}

func TestWorkerPool_ExecuteProjectJobUsesTaskConfig(t *testing.T) {
	setupSchedulerDataDir(t)
	p, q := newAlphaPool(t,
		config.TaskConfig{Name: "triage", Type: "project", Project: "o/r", Prompt: "Triage new issues"},
		config.TaskConfig{Name: "orphan", Type: "project", Prompt: "Do work"},
	)
	var got ProjectRun
	p.projectRunner = func(_ context.Context, run ProjectRun) (string, error) {
		got = run
		return "", nil
	}

	// A job without its own prompt runs the configured one, and no summary
	// leaves no output.
	job, err := q.EnqueueWithType("alpha/triage", "project", "alpha", "", "", "", 1, "", "")
	require.NoError(t, err)
	require.NoError(t, p.executeJob(context.Background(), job))
	assert.Equal(t, "Triage new issues", got.Prompt)
	assert.Empty(t, readJob(t, job.ID).Output)

	orphan, err := q.EnqueueWithType("alpha/orphan", "project", "alpha", "", "", "", 1, "", "")
	require.NoError(t, err)
	assert.EqualError(t, p.executeJob(context.Background(), orphan), `project task "alpha/orphan" names no project`)
}

func TestWorkerPool_PrecompiledProjectTaskFallsBackWhenCloneFails(t *testing.T) {
	setupSchedulerDataDir(t)
	precompile := true
	p, q := newAlphaPool(t, config.TaskConfig{Name: "stale", Type: "project", Project: "o/r", Prompt: "Label", Precompile: &precompile})
	p.taskCompiler = func(context.Context, string, string) (string, error) { return `print("done")`, nil }
	var reported int
	p.scriptReporter = func(string, string, error) error { reported++; return nil }
	p.projectRunner = func(ctx context.Context, run ProjectRun) (string, error) {
		_, err := run.RunCompiled(ctx, func(context.Context) (string, error) { return "", errors.New("disk full") }, func(string, ...any) {})
		require.Error(t, err)
		return "model summary", nil
	}
	job, err := q.EnqueueWithType("alpha/stale", "project", "alpha", "Label", "", "", 1, "", "")
	require.NoError(t, err)

	require.NoError(t, p.executeJob(context.Background(), job))
	saved := readJob(t, job.ID)
	assert.Equal(t, "cloning o/r: disk full", saved.ScriptError)
	assert.Contains(t, saved.Output, "Precompiled script failed (cloning o/r: disk full)")
	assert.Zero(t, reported, "a failed clone is not the script's result")
}

func TestWorkerPool_StopJobsMatcher(t *testing.T) {
	p := NewWorkerPool(NewJobQueue(), nil, 1)
	var mu sync.Mutex
	canceled := map[string]bool{}
	for _, id := range []string{"job_a", "job_b"} {
		p.registerActiveJob(id, "alpha/"+id, "alpha", func() { mu.Lock(); canceled[id] = true; mu.Unlock() })
	}

	assert.Equal(t, 1, p.StopJobs(func(jobID, _ string) bool { return jobID == "job_a" }))
	assert.Equal(t, map[string]bool{"job_a": true}, canceled)
	assert.Equal(t, 2, p.StopJobs(nil))
	assert.Equal(t, map[string]bool{"job_a": true, "job_b": true}, canceled)
}

func TestProjectScriptFailureCause(t *testing.T) {
	long := strings.Repeat("é", 100)
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"no error", nil, ""},
		{"gh status", errors.New("gh api failed (HTTP 422)"), "gh 422"},
		{"lua location dropped", errors.New("<string>:52: lookup failed\nstack traceback:"), "lookup failed"},
		{"chunk location dropped", errors.New(`[string "main"]:3: bad arg`), "bad arg"},
		{"shortened", errors.New(long), strings.Repeat("é", 79) + "…"},
		{"only a location", errors.New("<string>:1: "), "error"},
	} {
		assert.Equal(t, c.want, ProjectScriptFailureCause(c.err), c.name)
	}
}
