package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
)

func TestUnansweredUserMessageID(t *testing.T) {
	setTestDataDir(t)
	path := store.SessionPath("retry", "sess")
	require.NoError(t, store.AppendJSONL(path, domain.Message{ID: "msg_1", Role: domain.MessageRoleUser, Content: "hello"}))
	require.NoError(t, store.AppendJSONL(path, domain.Message{ID: "tool_1", Role: domain.MessageRoleTool, Content: "{}"}))

	assert.Equal(t, "msg_1", UnansweredUserMessageID("retry", "sess", "hello", nil), "tool records from the failed turn are skipped")
	assert.Empty(t, UnansweredUserMessageID("retry", "sess", "other", nil), "a different message is not a retry")
	assert.Empty(t, UnansweredUserMessageID("retry", "sess", "hello", []string{"data:image/png;base64,AA"}), "different media is not a retry")

	require.NoError(t, MarkMessageResponded("retry", "sess", "msg_1", "msg_2"))
	assert.Empty(t, UnansweredUserMessageID("retry", "sess", "hello", nil), "an answered message is not run again")

	require.NoError(t, store.AppendJSONL(path, domain.Message{ID: "msg_3", Role: domain.MessageRoleUser, Content: "again"}))
	require.NoError(t, store.AppendJSONL(path, domain.Message{ID: "msg_4", Role: domain.MessageRoleAssistant, Content: "partial"}))
	assert.Empty(t, UnansweredUserMessageID("retry", "sess", "again", nil), "a turn that moved on is not run again")
}

func TestAgentRunner_RetryReusesFailedUserMessage(t *testing.T) {
	setTestDataDir(t)
	sess, err := NewSessionManager().Create("retry-agent")
	require.NoError(t, err)
	ctx := WithSessionID(context.Background(), sess.ID)
	a := &domain.Agent{ID: "retry-agent", Model: "anthropic/test"}
	cfg := &config.AgentConfig{Name: "bot"}

	run := func(r *AgentRunner, overrides RunOverrides) {
		done := make(chan struct{})
		r.PromptWithOverrides(ctx, "hi", overrides, func(e StreamEvent) {
			if e.Type == StreamEventDone || e.Type == StreamEventError {
				close(done)
			}
		})
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			require.FailNow(t, "timeout")
		}
	}

	run(NewAgentRunner(a, cfg, &mockProvider{err: errors.New("502 Bad Gateway")}, nil), RunOverrides{})
	provider := &sequenceProvider{responses: [][]llm.Event{{{Type: llm.EventTypeText, Text: "hello"}, {Type: llm.EventTypeDone}}}}
	run(NewAgentRunner(a, cfg, provider, nil), RunOverrides{Retry: true})

	lines, err := store.ReadJSONL[domain.Message](store.SessionPath(a.ID, sess.ID))
	require.NoError(t, err)
	var users, replies int
	for _, m := range lines {
		switch m.Role {
		case domain.MessageRoleUser:
			users++
			assert.True(t, HasMessageResponse(a.ID, sess.ID, m.ID), "the retried message is marked answered")
		case domain.MessageRoleAssistant:
			replies++
		}
	}
	assert.Equal(t, 1, users, "Retry does not store the message again")
	assert.Equal(t, 1, replies)
	require.Len(t, provider.requests, 1)
	var userTurns int
	for _, m := range provider.requests[0].Messages {
		if m.Role == llm.RoleUser {
			userTurns++
		}
	}
	assert.Equal(t, 1, userTurns, "the model sees the message once")
}
