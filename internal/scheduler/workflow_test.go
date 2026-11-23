package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
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
	p.deliver = func(_, route, text string) error {
		delivered = append(delivered, route+":"+text)
		return nil
	}
	job := &domain.Job{TriggerContext: &domain.TriggerContext{Entry: "C1"}}
	require.NoError(t, p.deliverFor(job)("alpha", "reply", "on it"))
	require.NoError(t, p.deliverFor(job)("alpha", "session:ops", "fyi"))
	assert.Equal(t, []string{"C1:on it"}, replied)
	assert.Equal(t, []string{"session:ops:fyi"}, delivered)

	assert.Error(t, p.deliverFor(&domain.Job{})("alpha", "reply", "x"), "a run without a triggering message cannot reply")
}
