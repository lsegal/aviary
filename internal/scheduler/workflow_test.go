package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

func TestWildcardMatch(t *testing.T) {
	for _, tc := range []struct {
		pattern, text string
		want          bool
	}{
		{"", "anything at all", true},
		{"*", "anything at all", true},
		{"*", "", true},
		{"*crash*", "the app CRASHED again", true},
		{"*crash*", "all good", false},
		{"crash", "crash", true},
		{"crash", "crash report", false},
		{"crash*", "Crash report", true},
		{"*report", "crash report", true},
		{"c?sh", "crash", false},
		{"cr?sh", "crash", true},
		{"*error*", "line one\nan error here\nline three", true},
		{"a*b*c", "a-x-b-y-c", true},
		{"a*b*c", "a-x-c-y-b", false},
	} {
		assert.Equal(t, tc.want, WildcardMatch(tc.pattern, tc.text), "%q vs %q", tc.pattern, tc.text)
	}
}

func TestWorkflowMatches(t *testing.T) {
	cc := config.ChannelConfig{Type: "slack", ID: "team", Name: "Team Slack"}
	wf := config.TaskConfig{Name: "triage", Trigger: config.TriggerMessage, Channel: "Team Slack", Entry: "C1", Match: "*crash*"}

	assert.True(t, WorkflowMatches(wf, cc, "C1", "it crashed"))
	assert.False(t, WorkflowMatches(wf, cc, "C2", "it crashed"), "other entry")
	assert.False(t, WorkflowMatches(wf, cc, "C1", "all fine"), "no match")
	assert.False(t, WorkflowMatches(wf, config.ChannelConfig{Type: "slack", ID: "other"}, "C1", "it crashed"), "other channel")

	byID := wf
	byID.Channel = "team"
	assert.True(t, WorkflowMatches(byID, cc, "C1", "crash"), "channel picked by ID")

	anyEntry := wf
	anyEntry.Entry, anyEntry.Match = "", ""
	assert.True(t, WorkflowMatches(anyEntry, cc, "C9", "hello"), "empty entry and match take every message")

	scheduled := wf
	scheduled.Trigger = "daily"
	assert.False(t, WorkflowMatches(scheduled, cc, "C1", "crash"), "not a workflow")
}

func TestDisplayNameDropsRawIDs(t *testing.T) {
	assert.Equal(t, "Alice", DisplayName("+15551234567", "Alice"))
	assert.Equal(t, "", DisplayName("+15551234567", "+15551234567"))
	assert.Equal(t, "", DisplayName("x", "+1 (555) 123-4567"))
	assert.Equal(t, "", DisplayName("x", "U012AB3CD"))
	assert.Equal(t, "", DisplayName("x", "1f0e3dad-9990-4e0b-8b9e-4b7c1e1a2b3c"))
	assert.Equal(t, "", DisplayName("U1", ""))
	assert.Equal(t, "Bob Smith", DisplayName("U1", "Bob Smith"))
}

func workflowConfig(tasks ...config.TaskConfig) *config.Config {
	return &config.Config{Agents: []config.AgentConfig{{
		Name:     "alpha",
		Channels: []config.ChannelConfig{{Type: "slack", ID: "team", Name: "Team Slack"}},
		Tasks:    tasks,
	}}}
}

func TestFireWorkflows(t *testing.T) {
	setupSchedulerDataDir(t)
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	s := newPolicyScheduler(t, &now)
	paused := false
	cfg := workflowConfig(
		config.TaskConfig{Name: "triage", Trigger: config.TriggerMessage, Channel: "Team Slack", Entry: "C1", Match: "*crash*", Prompt: "Triage this crash.", Target: config.ReplyTarget},
		config.TaskConfig{Name: "log-all", Trigger: config.TriggerMessage, Channel: "Team Slack", Prompt: "Log it."},
		config.TaskConfig{Name: "paused", Enabled: &paused, Trigger: config.TriggerMessage, Channel: "Team Slack", Prompt: "Never."},
		config.TaskConfig{Name: "hourly", Every: "1h", Prompt: "Scheduled."},
	)
	s.Reconcile(cfg)
	cc := cfg.Agents[0].Channels[0]

	s.mu.Lock()
	_, armed := s.armed["alpha/triage"]
	s.mu.Unlock()
	assert.False(t, armed, "workflows are not armed on a schedule")

	assert.Equal(t, []string{"alpha/log-all", "alpha/triage"}, s.MatchWorkflows("alpha", cc, "C1", "it crashed"), "every active match, paused left out")
	assert.Equal(t, []string{"alpha/log-all"}, s.MatchWorkflows("alpha", cc, "C1", "hello"))
	assert.Empty(t, s.MatchWorkflows("beta", cc, "C1", "it crashed"), "other agent")

	trigger := domain.TriggerContext{
		ChannelType: "slack", ConfiguredID: "team", Channel: "Team Slack", Entry: "C1", SessionID: "sess",
		Message: domain.TriggerMessage{Sender: "Alice", Text: "it crashed", Attachments: []string{"log.txt"}, Timestamp: now},
		Before:  []domain.TriggerMessage{{Sender: "Bob", Text: "deploying now", Timestamp: now.Add(-time.Minute)}},
	}
	fires := s.FireWorkflows("alpha", cc, trigger)
	require.Len(t, fires, 2)
	for _, f := range fires {
		assert.Equal(t, domain.TaskRunQueued, f.Status)
		assert.NotEmpty(t, f.JobID)
	}

	jobs, err := s.Queue().List("alpha/triage")
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	job := jobs[0]
	assert.Equal(t, "message", job.Trigger)
	assert.Equal(t, config.ReplyTarget, job.OutputChannel)
	require.NotNil(t, job.TriggerContext)
	assert.Equal(t, "Alice", job.TriggerContext.Message.Sender)
	assert.Equal(t, []string{"log.txt"}, job.TriggerContext.Message.Attachments)
	assert.Contains(t, job.Prompt, "Triage this crash.")
	assert.Contains(t, job.Prompt, "Alice: it crashed")
	assert.Contains(t, job.Prompt, "Bob: deploying now")
	assert.Equal(t, domain.TaskRunQueued, lastRun(t, s, "alpha/triage").Status)
	assert.Equal(t, "message", lastRun(t, s, "alpha/triage").Reason)

	// Resuming the paused workflow makes it fire again.
	resumed := true
	cfg.Agents[0].Tasks[2].Enabled = &resumed
	s.Reconcile(cfg)
	assert.Contains(t, s.MatchWorkflows("alpha", cc, "C1", "hi"), "alpha/paused")

	var listed domain.ScheduledTask
	for _, task := range s.ListTasks() {
		if task.Name == "triage" {
			listed = task
		}
	}
	assert.Equal(t, domain.TriggerTypeMessage, listed.TriggerType)
	assert.Equal(t, "Team Slack", listed.Channel)
	assert.Equal(t, "messages matching *crash* on Team Slack (C1)", listed.Description)
	assert.Nil(t, listed.NextRun)
}

func TestFireWorkflowsOverlapSkip(t *testing.T) {
	setupSchedulerDataDir(t)
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	s := newPolicyScheduler(t, &now)
	cfg := workflowConfig(config.TaskConfig{Name: "once", Trigger: config.TriggerMessage, Channel: "Team Slack", Overlap: "skip", Prompt: "p"})
	s.Reconcile(cfg)
	cc := cfg.Agents[0].Channels[0]
	trigger := domain.TriggerContext{Entry: "C1", Message: domain.TriggerMessage{Text: "a", Timestamp: now}}

	first := s.FireWorkflows("alpha", cc, trigger)
	second := s.FireWorkflows("alpha", cc, trigger)
	require.Len(t, first, 1)
	require.Len(t, second, 1)
	assert.Equal(t, domain.TaskRunQueued, first[0].Status)
	assert.Equal(t, domain.TaskRunSkipped, second[0].Status, "the previous run is still pending")
	assert.Equal(t, 1, jobCount(t, s, "alpha/once"))
}

func TestWorkerDeliversWorkflowReplies(t *testing.T) {
	p := &WorkerPool{}
	var replied, delivered []string
	p.workflowReply = func(_ string, tc domain.TriggerContext, text string) error {
		replied = append(replied, tc.Entry+":"+text)
		return nil
	}
	p.deliver = func(_ domain.OutputSource, _, route, text string) (*domain.JobDelivery, error) {
		delivered = append(delivered, route+":"+text)
		return nil, nil
	}
	job := &domain.Job{TriggerContext: &domain.TriggerContext{Entry: "C1"}}
	require.NoError(t, p.deliverFor(job)("alpha", "reply", "on it"))
	require.NoError(t, p.deliverFor(job)("alpha", "session:ops", "fyi"))
	assert.Equal(t, []string{"C1:on it"}, replied)
	assert.Equal(t, []string{"session:ops:fyi"}, delivered)

	assert.Error(t, p.deliverFor(&domain.Job{})("alpha", "reply", "x"), "a run without a triggering message cannot reply")

	// Without the hooks wired, other targets are dropped quietly but a reply
	// reports that it cannot be sent.
	bare := &WorkerPool{}
	assert.NoError(t, bare.deliverFor(job)("alpha", "session:ops", "fyi"))
	assert.EqualError(t, bare.deliverFor(job)("alpha", "reply", "on it"), "workflow replies are not available")
}

func TestWorkerPool_RunCompiledWorkflow(t *testing.T) {
	setupSchedulerDataDir(t)
	precompile := true
	cfg := &config.AgentConfig{Name: "alpha", Tasks: []config.TaskConfig{
		{Name: "triage", Trigger: config.TriggerMessage, Channel: "team", Prompt: "Echo the crash report", Target: "reply", Precompile: &precompile},
		{Name: "plain", Trigger: config.TriggerMessage, Channel: "team", Prompt: "Echo the crash report", Target: "reply"},
	}}
	q := NewJobQueue()
	p := NewWorkerPool(q, agent.NewManager(nil), 1)
	var replies []string
	p.workflowReply = func(_ string, _ domain.TriggerContext, text string) error {
		replies = append(replies, text)
		return nil
	}
	// The script reads the triggering message from the trigger global.
	script, compileErr := `print("crash: " .. trigger.message.text .. " after " .. #trigger.before)`, error(nil)
	var compiledFor []string
	p.taskCompiler = func(_ context.Context, agentName, taskName string) (string, error) {
		compiledFor = append(compiledFor, agentName+"/"+taskName)
		return script, compileErr
	}
	var reported []string
	p.scriptReporter = func(_, taskName string, err error) error {
		result := "ok"
		if err != nil {
			result = "failed"
		}
		reported = append(reported, taskName+": "+result)
		return nil
	}
	enqueue := func(task string) *domain.Job {
		job, err := q.EnqueueWithType("alpha/"+task, "prompt", "alpha", "Echo the crash report", "", "reply", 1, "", "")
		require.NoError(t, err)
		job.OutputChannel = "reply"
		job.TriggerContext = &domain.TriggerContext{
			Channel: "team", Entry: "C1",
			Message: domain.TriggerMessage{Sender: "Ana", Text: "app crashed on launch"},
			Before:  []domain.TriggerMessage{{Sender: "Ana", Text: "hi"}},
		}
		return job
	}
	persisted := func(job *domain.Job) domain.Job {
		got, err := store.ReadJSON[domain.Job](store.FindJobPath(job.ID))
		require.NoError(t, err)
		return got
	}

	// A compiled script handles the run and replies in the conversation.
	job := enqueue("triage")
	logs, handled, err := p.runCompiledPrompt(context.Background(), job, cfg, p.deliverFor(job))
	require.NoError(t, err)
	assert.True(t, handled, "a completed script runs no model")
	assert.Equal(t, []string{"crash: app crashed on launch after 1"}, replies)
	assert.Contains(t, logs, "running the precompiled script")
	assert.Empty(t, persisted(job).ScriptError)
	assert.Equal(t, []string{"triage: ok"}, reported)

	// A failing script falls back to the model, and the run is marked.
	script = `error("lookup failed")`
	job = enqueue("triage")
	logs, handled, err = p.runCompiledPrompt(context.Background(), job, cfg, p.deliverFor(job))
	require.NoError(t, err)
	assert.False(t, handled, "the model runs a workflow whose script failed")
	assert.Contains(t, logs, "precompiled script failed (lookup failed); falling back to the model")
	assert.Contains(t, persisted(job).ScriptError, "lookup failed")
	assert.Equal(t, []string{"triage: ok", "triage: failed"}, reported)
	assert.Len(t, replies, 1, "a failed script sends no reply of its own")

	// A failed compile falls back as well, but is not the script's failure.
	script, compileErr = "", errors.New("provider unavailable")
	job = enqueue("triage")
	_, handled, err = p.runCompiledPrompt(context.Background(), job, cfg, p.deliverFor(job))
	require.NoError(t, err)
	assert.False(t, handled)
	assert.Contains(t, persisted(job).ScriptError, "provider unavailable")
	assert.Len(t, reported, 2)

	// A prompt that is not deterministic just runs on the model.
	compileErr = nil
	job = enqueue("triage")
	logs, handled, err = p.runCompiledPrompt(context.Background(), job, cfg, p.deliverFor(job))
	require.NoError(t, err)
	assert.False(t, handled)
	assert.Empty(t, logs)
	assert.Empty(t, job.ScriptError)

	// A workflow without precompile never compiles.
	compiledFor = nil
	job = enqueue("plain")
	_, handled, err = p.runCompiledPrompt(context.Background(), job, cfg, p.deliverFor(job))
	require.NoError(t, err)
	assert.False(t, handled)
	assert.Empty(t, compiledFor)
}

func TestWorkerPool_RunCompiledScheduledPromptTask(t *testing.T) {
	setupSchedulerDataDir(t)
	precompile := true
	cfg := &config.AgentConfig{Name: "alpha", Tasks: []config.TaskConfig{
		{Name: "digest", Every: "1h", Prompt: "Post the digest", Target: "session:ops", Precompile: &precompile},
		{Name: "plain", Every: "1h", Prompt: "Post the digest", Target: "session:ops"},
		{Name: "lua", Type: "script", Every: "1h", Prompt: `print("x")`, Target: "session:ops", Precompile: &precompile},
	}}
	q := NewJobQueue()
	p := NewWorkerPool(q, agent.NewManager(nil), 1)
	var delivered []string
	p.deliver = func(_ domain.OutputSource, _, route, text string) (*domain.JobDelivery, error) {
		delivered = append(delivered, route+":"+text)
		return &domain.JobDelivery{Target: route, Status: domain.DeliveryDelivered}, nil
	}
	script := `print("digest: " .. tostring(trigger))`
	var compiledFor []string
	p.taskCompiler = func(_ context.Context, agentName, taskName string) (string, error) {
		compiledFor = append(compiledFor, agentName+"/"+taskName)
		return script, nil
	}
	enqueue := func(task string) *domain.Job {
		job, err := q.EnqueueWithType("alpha/"+task, "prompt", "alpha", "Post the digest", "", "session:ops", 1, "", "")
		require.NoError(t, err)
		return job
	}

	// A scheduled prompt task runs its script, with no trigger, and delivers
	// what it prints to its target without the model.
	job := enqueue("digest")
	logs, handled, err := p.runCompiledPrompt(context.Background(), job, cfg, p.deliverFor(job))
	require.NoError(t, err)
	assert.True(t, handled, "a completed script runs no model")
	assert.Contains(t, logs, "running the precompiled script")
	assert.Equal(t, []string{"session:ops:digest: nil"}, delivered)

	// A failing script falls back to the model.
	script = `error("feed down")`
	job = enqueue("digest")
	_, handled, err = p.runCompiledPrompt(context.Background(), job, cfg, p.deliverFor(job))
	require.NoError(t, err)
	assert.False(t, handled)
	assert.Contains(t, job.ScriptError, "feed down")

	// Neither a task without precompile nor a script task compiles.
	compiledFor = nil
	for _, name := range []string{"plain", "lua"} {
		job = enqueue(name)
		_, handled, err = p.runCompiledPrompt(context.Background(), job, cfg, p.deliverFor(job))
		require.NoError(t, err)
		assert.False(t, handled)
	}
	assert.Empty(t, compiledFor)
}

func TestDeliverForRecordsDeliveryOnRun(t *testing.T) {
	q := NewJobQueue()
	p := &WorkerPool{queue: q}
	job, err := q.Enqueue("alpha/digest", "alpha", "x", "codeloop:o/r#3", 1, "", "")
	require.NoError(t, err)
	var got domain.OutputSource
	p.deliver = func(source domain.OutputSource, _, route, _ string) (*domain.JobDelivery, error) {
		got = source
		return &domain.JobDelivery{Target: route, Status: domain.DeliveryQueued, Message: "queued for Code Loop o/r#3 until the job runs again", Project: "o/r", Number: 3}, nil
	}
	require.NoError(t, p.deliverFor(job)("alpha", "codeloop:o/r#3", "report"))
	assert.Equal(t, domain.OutputSource{Task: "alpha/digest", JobID: job.ID}, got)
	assert.Equal(t, "task alpha/digest", got.Label())

	jobs, err := q.List("alpha/digest")
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.NotNil(t, jobs[0].Delivery)
	assert.Equal(t, domain.DeliveryQueued, jobs[0].Delivery.Status)
	assert.Equal(t, 3, jobs[0].Delivery.Number)
}
