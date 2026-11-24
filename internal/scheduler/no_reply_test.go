package scheduler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// noReplyToolClient offers task_no_reply the way Aviary's in-process MCP
// server does: for the job the client's context names.
type noReplyToolClient struct{ ctx context.Context }

func (c noReplyToolClient) ListTools(context.Context) ([]agent.ToolInfo, error) {
	return []agent.ToolInfo{{Name: taskNoReplyTool}}, nil
}

func (c noReplyToolClient) CallToolText(_ context.Context, _ string, args map[string]any) (string, error) {
	jobID, _ := agent.JobIDFromContext(c.ctx)
	reason, _ := args["reason"].(string)
	agent.SkipReply(jobID, reason)
	return "ok", nil
}

func (noReplyToolClient) Close() error { return nil }

// #1030: a precompiled workflow script ends without replying by calling
// task_no_reply, and the run records the skip.
func TestWorkerPool_RunCompiledWorkflowNoReply(t *testing.T) {
	setupSchedulerDataDir(t)
	agent.SetToolClientFactory(func(ctx context.Context) (agent.ToolClient, error) { return noReplyToolClient{ctx: ctx}, nil })
	t.Cleanup(func() { agent.SetToolClientFactory(nil) })
	precompile := true
	cfg := &config.AgentConfig{Name: "alpha", Tasks: []config.TaskConfig{
		{Name: "be-nice", Trigger: config.TriggerMessage, Channel: "team", Prompt: "Reply only to positive messages", Target: "reply", Precompile: &precompile},
	}}
	q := NewJobQueue()
	p := NewWorkerPool(q, agent.NewManager(nil), 1)
	var replies []string
	p.workflowReply = func(_ string, _ domain.TriggerContext, text string) error {
		replies = append(replies, text)
		return nil
	}
	script := ""
	p.taskCompiler = func(context.Context, string, string) (string, error) { return script, nil }
	run := func() (*domain.Job, bool) {
		job, err := q.EnqueueWithType("alpha/be-nice", "prompt", "alpha", "Reply only to positive messages", "", "reply", 1, "", "")
		require.NoError(t, err)
		job.TriggerContext = &domain.TriggerContext{Channel: "team", Entry: "C1", Message: domain.TriggerMessage{Text: "test you're mean"}}
		_, handled, err := p.runCompiledPrompt(agent.WithJobID(context.Background(), job.ID), job, cfg, p.deliverFor(job))
		require.NoError(t, err)
		return job, handled
	}

	script = `tool.task_no_reply({reason = "message was negative"})`
	job, handled := run()
	assert.True(t, handled)
	assert.Empty(t, replies)
	got, err := store.ReadJSON[domain.Job](store.FindJobPath(job.ID))
	require.NoError(t, err)
	require.NotNil(t, got.Delivery)
	assert.Equal(t, domain.DeliverySkipped, got.Delivery.Status)
	assert.Equal(t, "message was negative", got.Delivery.Message)

	// Even when the script also prints, a skipped reply delivers nothing.
	script = `tool.task_no_reply({reason = "negative"}) print("I didn't reply")`
	_, handled = run()
	assert.True(t, handled)
	assert.Empty(t, replies)

	// A script that fails after choosing falls back to the model, which
	// decides afresh.
	script = `tool.task_no_reply({reason = "negative"}) error("lookup failed")`
	job, handled = run()
	assert.False(t, handled)
	_, pending := agent.TakeSkippedReply(job.ID)
	assert.False(t, pending)

	// A script that replies delivers exactly its reply.
	script = `print("thanks!")`
	_, handled = run()
	assert.True(t, handled)
	assert.Equal(t, []string{"thanks!"}, replies)
}

// #1030: a script task that calls task_no_reply delivers nothing to its
// target or the conversation that started it.
func TestExecuteScriptJob_NoReply(t *testing.T) {
	setupSchedulerDataDir(t)
	agent.SetToolClientFactory(func(ctx context.Context) (agent.ToolClient, error) { return noReplyToolClient{ctx: ctx}, nil })
	t.Cleanup(func() { agent.SetToolClientFactory(nil) })
	q := NewJobQueue()
	p := &WorkerPool{queue: q}
	job, err := q.EnqueueWithType("alpha/digest", "script", "alpha", "", "", "session:ops", 1, "", "")
	require.NoError(t, err)
	job.Script = `tool.task_no_reply({reason = "nothing new"}) print("nothing to report")`
	var delivered []string
	result, err := executeScriptJob(agent.WithJobID(context.Background(), job.ID), job, &config.AgentConfig{}, func(_, route, text string) error {
		delivered = append(delivered, route+": "+text)
		return nil
	}, func() bool { return p.skipReply(job) })
	require.NoError(t, err)
	assert.Equal(t, "nothing to report", result.Output)
	assert.Empty(t, delivered)
	require.NotNil(t, job.Delivery)
	assert.Equal(t, "nothing new", job.Delivery.Message)
}

func TestReplyNote(t *testing.T) {
	workflow := replyNote(&domain.Job{OutputChannel: "reply", TriggerContext: &domain.TriggerContext{}})
	assert.Contains(t, workflow, "sent verbatim as a reply to the conversation that triggered this workflow")
	assert.Contains(t, workflow, "call the task_no_reply tool")
	assert.Contains(t, replyNote(&domain.Job{OutputChannel: "channel:team"}), "output target (channel:team)")
	assert.Empty(t, replyNote(&domain.Job{OutputChannel: "silent"}), "a silent run's message goes nowhere")
	assert.Empty(t, replyNote(&domain.Job{}))
}
