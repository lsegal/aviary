package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/channels"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// newWorkflowServer returns a server whose agent "bot" has the Slack channel
// "Team" and the given workflows.
func newWorkflowServer(t *testing.T, workflows ...config.TaskConfig) *Server {
	t.Helper()
	setupServerDataDir(t)
	resetSlogForTest()
	cfg := &config.Config{Agents: []config.AgentConfig{{
		Name:     "bot",
		Model:    "stub",
		Channels: []config.ChannelConfig{{Type: "slack", ID: "team", Name: "Team"}},
		Tasks:    workflows,
	}}}
	srv := New(cfg, "tok")
	srv.agents.Reconcile(cfg)
	require.NotNil(t, srv.sched)
	srv.sched.Reconcile(cfg)
	t.Cleanup(func() {
		if runner, ok := srv.agents.Get("bot"); ok {
			runner.Wait()
		}
		srv.sched.Stop()
	})
	return srv
}

func workflow(name, match string) config.TaskConfig {
	return config.TaskConfig{Name: name, Trigger: config.TriggerMessage, Channel: "Team", Entry: "C1", Match: match, Prompt: "Handle it."}
}

// sendSlack delivers text from Alice on conversation C1 and returns the
// conversation's session messages once the handler is done with it.
func sendSlack(t *testing.T, srv *Server, from, text string) []domain.Message {
	t.Helper()
	srv.handleIncomingChannelMessage(context.Background(), "bot", "slack", "team", stubChannel{}, channels.IncomingMessage{
		Type: "slack", Channel: "C1", From: from, SenderName: "Alice", Text: text, ReceivedAt: time.Now(),
	})
	if runner, ok := srv.agents.Get("bot"); ok {
		runner.Wait()
	}
	sess, err := agent.NewSessionManager().GetOrCreateNamed("bot", "slack:C1")
	require.NoError(t, err)
	lines, err := store.ReadJSONL[domain.Message](store.SessionPath("bot", sess.ID))
	require.NoError(t, err)
	return lines
}

func lastUserMessage(msgs []domain.Message) domain.Message {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == domain.MessageRoleUser {
			return msgs[i]
		}
	}
	return domain.Message{}
}

func hasAssistant(msgs []domain.Message) bool {
	for _, m := range msgs {
		if m.Role == domain.MessageRoleAssistant {
			return true
		}
	}
	return false
}

func workflowJobs(t *testing.T, srv *Server, key string) []domain.Job {
	t.Helper()
	jobs, err := srv.sched.Queue().List(key)
	require.NoError(t, err)
	return jobs
}

func TestWorkflowMatchReplacesChatReply(t *testing.T) {
	srv := newWorkflowServer(t, workflow("triage", "*crash*"))

	msgs := sendSlack(t, srv, "U1", "the app crashed")

	user := lastUserMessage(msgs)
	assert.Equal(t, "the app crashed", user.Content, "the message is still recorded")
	require.Len(t, user.Workflows, 1)
	assert.Equal(t, "bot/triage", user.Workflows[0].TaskID)
	assert.Equal(t, "triage", user.Workflows[0].Name)
	assert.NotEmpty(t, user.Workflows[0].JobID, "links to the run")
	assert.False(t, hasAssistant(msgs), "the agent does not reply on its own")

	jobs := workflowJobs(t, srv, "bot/triage")
	require.Len(t, jobs, 1)
	tc := jobs[0].TriggerContext
	require.NotNil(t, tc)
	assert.Equal(t, "Team", tc.Channel)
	assert.Equal(t, "C1", tc.Entry)
	assert.Equal(t, "Alice", tc.Message.Sender)
	assert.Equal(t, "the app crashed", tc.Message.Text)
	assert.NotContains(t, jobs[0].Prompt, "U1", "no raw sender ID")
}

func TestWorkflowNoMatchRespondsNormally(t *testing.T) {
	srv := newWorkflowServer(t, workflow("triage", "*crash*"))

	msgs := sendSlack(t, srv, "U1", "hello there")

	assert.Empty(t, lastUserMessage(msgs).Workflows)
	assert.True(t, hasAssistant(msgs), "the agent replies")
	assert.Empty(t, workflowJobs(t, srv, "bot/triage"))
}

func TestWorkflowPausedOnlyMatchRespondsNormally(t *testing.T) {
	paused := workflow("triage", "*crash*")
	off := false
	paused.Enabled = &off
	srv := newWorkflowServer(t, paused)

	msgs := sendSlack(t, srv, "U1", "it crashed")

	assert.Empty(t, lastUserMessage(msgs).Workflows)
	assert.True(t, hasAssistant(msgs))
	assert.Empty(t, workflowJobs(t, srv, "bot/triage"))
}

func TestWorkflowWildcardTakesEveryMessage(t *testing.T) {
	srv := newWorkflowServer(t, workflow("all", ""))

	for _, text := range []string{"hello", "anything else", "123"} {
		msgs := sendSlack(t, srv, "U1", text)
		assert.Len(t, lastUserMessage(msgs).Workflows, 1, text)
		assert.False(t, hasAssistant(msgs), text)
	}
	assert.Len(t, workflowJobs(t, srv, "bot/all"), 3)
}

func TestWorkflowMultipleMatchesAllRun(t *testing.T) {
	srv := newWorkflowServer(t, workflow("triage", "*crash*"), workflow("all", "*"))

	msgs := sendSlack(t, srv, "U1", "crash!")

	user := lastUserMessage(msgs)
	require.Len(t, user.Workflows, 2)
	assert.Equal(t, "all", user.Workflows[0].Name, "fired in name order")
	assert.Equal(t, "triage", user.Workflows[1].Name)
	assert.False(t, hasAssistant(msgs))
	assert.Len(t, workflowJobs(t, srv, "bot/triage"), 1)
	assert.Len(t, workflowJobs(t, srv, "bot/all"), 1)
}

func TestWorkflowIgnoresAgentsOwnMessages(t *testing.T) {
	srv := newWorkflowServer(t, workflow("all", ""))

	// A message from the channel's own account is the agent's.
	srv.handleWorkflowMessage("bot", config.ChannelConfig{Type: "slack", ID: "team", Name: "Team"}, "", channels.IncomingMessage{
		Type: "slack", Channel: "C1", From: "team", Text: "my own reply",
	})
	assert.Empty(t, workflowJobs(t, srv, "bot/all"))
}

func TestWorkflowTriggerContextRecordsSurroundingMessages(t *testing.T) {
	srv := newWorkflowServer(t, workflow("triage", "*crash*"))

	sendSlack(t, srv, "U1", "deploying now")
	sendSlack(t, srv, "U1", "it crashed")

	jobs := workflowJobs(t, srv, "bot/triage")
	require.Len(t, jobs, 1)
	tc := *jobs[0].TriggerContext
	require.NotEmpty(t, tc.Before)
	assert.Equal(t, "deploying now", tc.Before[0].Text)

	sess, err := agent.NewSessionManager().GetOrCreateNamed("bot", "slack:C1")
	require.NoError(t, err)
	require.NoError(t, agent.AppendMessageToSessionWithSender("bot", sess.ID, domain.MessageRoleUser, "never mind", domain.NewMessageSender("U2", "Bob", true)))
	after := workflowMessagesAfter("bot", tc)
	require.Len(t, after, 1)
	assert.Equal(t, "never mind", after[0].Text)
	assert.Equal(t, "Bob", after[0].Sender)
}

func TestWorkflowReplyGoesToTriggeringConversation(t *testing.T) {
	srv := newWorkflowServer(t)
	sess, err := agent.NewSessionManager().GetOrCreateNamed("bot", "slack:C1")
	require.NoError(t, err)

	require.NoError(t, srv.replyToWorkflowTrigger("bot", domain.TriggerContext{SessionID: sess.ID, Entry: "C1"}, "looking into it"))

	lines, err := store.ReadJSONL[domain.Message](store.SessionPath("bot", sess.ID))
	require.NoError(t, err)
	require.NotEmpty(t, lines)
	last := lines[len(lines)-1]
	assert.Equal(t, domain.MessageRoleAssistant, last.Role)
	assert.Equal(t, "looking into it", last.Content)
}
