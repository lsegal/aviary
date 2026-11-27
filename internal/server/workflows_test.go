package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// fakeWorkflowChannels records workflow replies sent through configured
// channels and fails them with err when set.
type fakeWorkflowChannels struct {
	err   error
	sends []string
}

func (f *fakeWorkflowChannels) SendOnConfiguredChannel(agentName, channelType, configuredID, channelID, text string) error {
	f.sends = append(f.sends, strings.Join([]string{agentName, channelType, configuredID, channelID, "", text}, "|"))
	return f.err
}

func (f *fakeWorkflowChannels) SendThreadOnConfiguredChannel(agentName, channelType, configuredID, channelID, threadTS, text string) error {
	f.sends = append(f.sends, strings.Join([]string{agentName, channelType, configuredID, channelID, threadTS, text}, "|"))
	return f.err
}

func TestWorkflowReplyWithoutSessionGoesThroughChannel(t *testing.T) {
	tc := domain.TriggerContext{ChannelType: "slack", ConfiguredID: "team", Entry: "C1"}
	tests := []struct {
		name      string
		sessionID string
		threadTS  string
		err       error
		want      string
	}{
		{name: "no session", want: "bot|slack|team|C1||done"},
		{name: "session no longer exists", sessionID: "gone", want: "bot|slack|team|C1||done"},
		{name: "threaded message", threadTS: "171.5", want: "bot|slack|team|C1|171.5|done"},
		{name: "delivery error", err: errors.New("slack down"), want: "bot|slack|team|C1||done"},
		{name: "threaded delivery error", threadTS: "171.5", err: errors.New("slack down"), want: "bot|slack|team|C1|171.5|done"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newWorkflowServer(t)
			fake := &fakeWorkflowChannels{err: tt.err}
			srv.workflowChannels = fake
			tc := tc
			tc.SessionID = tt.sessionID
			tc.ThreadTS = tt.threadTS

			err := srv.replyToWorkflowTrigger("bot", tc, "done")

			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, []string{tt.want}, fake.sends)
		})
	}
}

func TestWorkflowReplyFailsWhenChannelInactive(t *testing.T) {
	srv := newWorkflowServer(t)

	err := srv.replyToWorkflowTrigger("bot", domain.TriggerContext{ChannelType: "slack", ConfiguredID: "team", Entry: "C1"}, "done")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not active")

	err = srv.replyToWorkflowTrigger("bot", domain.TriggerContext{ChannelType: "slack", ConfiguredID: "team", Entry: "C1", ThreadTS: "1.2"}, "done")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not active")
}

func TestWorkflowMessageWithoutSchedulerIsNotHandled(t *testing.T) {
	srv := &Server{}
	assert.False(t, srv.handleWorkflowMessage("bot", config.ChannelConfig{Type: "slack", ID: "team"}, "", channels.IncomingMessage{Text: "crash"}))
}

func TestWorkflowMessagesAfterWithoutSession(t *testing.T) {
	setupServerDataDir(t)
	assert.Nil(t, workflowMessagesAfter("bot", domain.TriggerContext{RecordedAt: time.Now()}))
}

// writeSession writes msgs as the session file of bot's session id.
func writeSession(t *testing.T, id string, msgs ...domain.Message) {
	t.Helper()
	path := store.SessionPath("bot", id)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	var b strings.Builder
	for _, m := range msgs {
		line, err := json.Marshal(m)
		require.NoError(t, err)
		b.Write(line)
		b.WriteByte('\n')
	}
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))
}

func TestSessionMessagesAround(t *testing.T) {
	setupServerDataDir(t)
	at := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	msg := func(offset int, role domain.MessageRole, text string) domain.Message {
		m := domain.Message{Role: role, Content: text, Timestamp: at.Add(time.Duration(offset) * time.Minute)}
		if role == domain.MessageRoleUser {
			m.Sender = domain.NewMessageSender("U1", "Alice", true)
		}
		return m
	}
	writeSession(t, "busy",
		msg(-5, domain.MessageRoleUser, "b5"),
		msg(-4, domain.MessageRoleAssistant, "b4"),
		msg(-3, domain.MessageRoleUser, "b3"),
		msg(-2, domain.MessageRoleTool, "tool output"),
		msg(-2, domain.MessageRoleUser, "b2"),
		msg(-1, domain.MessageRoleAssistant, "b1"),
		msg(0, domain.MessageRoleUser, "trigger"),
		msg(1, domain.MessageRoleAssistant, "a1"),
		msg(2, domain.MessageRoleUser, "a2"),
		msg(3, domain.MessageRoleSystem, "system note"),
		msg(3, domain.MessageRoleUser, "a3"),
		msg(4, domain.MessageRoleAssistant, "a4"),
	)
	texts := func(ms []domain.TriggerMessage) []string {
		out := make([]string, 0, len(ms))
		for _, m := range ms {
			out = append(out, m.Text)
		}
		return out
	}

	before := sessionMessagesAround("bot", "busy", at, true)
	assert.Equal(t, []string{"b3", "b2", "b1"}, texts(before), "the latest three before, oldest first; tool messages skipped")
	assert.Equal(t, "Alice", before[0].Sender)
	assert.False(t, before[0].FromAgent)
	assert.True(t, before[2].FromAgent)
	assert.Empty(t, before[2].Sender, "agent replies carry no sender")
	assert.Equal(t, at.Add(-time.Minute), before[2].Timestamp)

	after := sessionMessagesAround("bot", "busy", at, false)
	assert.Equal(t, []string{"a1", "a2", "a3"}, texts(after), "the first three after; system messages skipped")
	assert.Equal(t, []string{"a1", "a2", "a3"}, texts(workflowMessagesAfter("bot", domain.TriggerContext{SessionID: "busy", RecordedAt: at})))

	assert.Empty(t, sessionMessagesAround("bot", "busy", at.Add(-time.Hour), true), "trigger before every message")
	assert.Empty(t, sessionMessagesAround("bot", "busy", at.Add(time.Hour), false), "trigger after every message")

	writeSession(t, "empty")
	assert.Empty(t, sessionMessagesAround("bot", "empty", at, true))
	assert.Empty(t, sessionMessagesAround("bot", "empty", at, false))

	assert.Nil(t, sessionMessagesAround("bot", "missing", at, true), "no session file")

	dir := store.SessionPath("bot", "unreadable")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	assert.Nil(t, sessionMessagesAround("bot", "unreadable", at, true), "unreadable session file")
}

func TestAttachmentLabels(t *testing.T) {
	tests := []struct {
		name string
		atts []channels.Attachment
		want []string
	}{
		{name: "no attachments", want: []string{}},
		{name: "named file", atts: []channels.Attachment{{Name: "log.txt", ContentType: "text/plain"}}, want: []string{"log.txt"}},
		{name: "unnamed attachment", atts: []channels.Attachment{{ContentType: "image/png"}}, want: []string{"image/png"}},
		{name: "saved file", atts: []channels.Attachment{{Name: "a.png", Path: "/tmp/a.png"}, {ContentType: "audio/ogg", Path: "/tmp/v.ogg"}}, want: []string{"a.png (/tmp/a.png)", "audio/ogg (/tmp/v.ogg)"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, attachmentLabels(tt.atts))
		})
	}
}

func TestMessageTime(t *testing.T) {
	received := time.Date(2026, 3, 4, 5, 6, 7, 0, time.FixedZone("X", 3600))
	assert.True(t, received.Equal(messageTime(received)), "a received time is kept")

	start := time.Now().UTC()
	got := messageTime(time.Time{})
	assert.Equal(t, time.UTC, got.Location(), "missing time falls back to now in UTC")
	assert.False(t, got.Before(start.Add(-time.Second)))
	assert.False(t, got.After(time.Now().UTC().Add(time.Second)))
}

func TestWorkflowStillHandlesMessageWhenRecordingFails(t *testing.T) {
	srv := newWorkflowServer(t, workflow("all", ""))
	// A directory where the session file belongs makes recording fail.
	require.NoError(t, os.MkdirAll(store.SessionPath("bot", "broken"), 0o700))

	handled := srv.handleWorkflowMessage("bot", config.ChannelConfig{Type: "slack", ID: "team", Name: "Team"}, "broken", channels.IncomingMessage{
		Type: "slack", Channel: "C1", From: "U1", SenderName: "Alice", Text: "crash",
	})

	assert.True(t, handled, "the workflow still replaces the chat reply")
	jobs := workflowJobs(t, srv, "bot/all")
	require.Len(t, jobs, 1)
	assert.Equal(t, "broken", jobs[0].TriggerContext.SessionID)
	assert.Empty(t, jobs[0].TriggerContext.Before)
}
