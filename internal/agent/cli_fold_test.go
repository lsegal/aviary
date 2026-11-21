package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/cliagent"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// thinkingCLISession sits on a turn ending in "first" without answering
// until it is cancelled, and answers every other turn at once.
type thinkingCLISession struct {
	opts     cliagent.Options
	mu       *sync.Mutex
	turns    *[]fakeCLITurn
	thinking chan struct{}
}

func (s *thinkingCLISession) Send(ctx context.Context, turn cliagent.Turn, onEvent func(cliagent.Event)) (cliagent.Result, error) {
	if strings.HasSuffix(turn.Text, "first") {
		close(s.thinking)
		<-ctx.Done()
		return cliagent.Result{}, ctx.Err()
	}
	s.mu.Lock()
	*s.turns = append(*s.turns, fakeCLITurn{opts: s.opts, text: turn.Text})
	s.mu.Unlock()
	onEvent(cliagent.Event{Type: cliagent.EventText, Text: "one reply"})
	return cliagent.Result{Text: "one reply"}, nil
}

func (s *thinkingCLISession) Close() error { return nil }

func TestCLITurn_FollowUpFoldsIntoTheUnansweredTurn(t *testing.T) {
	setTestDataDir(t)
	var mu sync.Mutex
	var turns []fakeCLITurn
	var sessions int
	thinking := make(chan struct{})
	prev := newCLISession
	newCLISession = func(opts cliagent.Options) (cliagent.Session, error) {
		mu.Lock()
		sessions++
		mu.Unlock()
		return &thinkingCLISession{opts: opts, mu: &mu, turns: &turns, thinking: thinking}, nil
	}
	t.Cleanup(func() { newCLISession = prev })

	runner := newCLITestRunner(t, "agent_fold", nil)
	sess, err := NewSessionManager().GetOrCreateNamed("agent_fold", "main")
	require.NoError(t, err)

	var firstText strings.Builder
	firstDone := make(chan error, 1)
	runner.Prompt(WithSessionID(context.Background(), sess.ID), "what's your n\nfirst", func(e StreamEvent) {
		switch e.Type {
		case StreamEventText:
			firstText.WriteString(e.Text)
		case StreamEventError:
			firstDone <- e.Err
		case StreamEventDone, StreamEventStop:
			firstDone <- nil
		}
	})
	select {
	case <-thinking:
	case <-time.After(10 * time.Second):
		t.Fatal("the first turn never reached the CLI")
	}

	text, _ := runCLIPrompt(t, runner, sess.ID, "oops name")
	waitDone(t, firstDone)
	runner.Wait()

	assert.Equal(t, "one reply", text)
	assert.Empty(t, firstText.String(), "the folded turn answers nothing of its own")
	require.Len(t, turns, 1, "both messages reach the CLI as one turn")
	assert.Equal(t, "what's your n\nfirst\n\noops name", turns[0].text)
	assert.False(t, turns[0].opts.Ephemeral, "the follow-up runs on the session's own conversation, not an overflow one")
	assert.NotContains(t, turns[0].text, "<prior_conversation>", "the live CLI session is not sent the backlog")
	assert.Equal(t, 1, sessions)

	msgs, err := store.ReadJSONL[domain.Message](store.SessionPath("agent_fold", sess.ID))
	require.NoError(t, err)
	var replies []string
	for _, m := range msgs {
		switch m.Role {
		case domain.MessageRoleUser:
			assert.True(t, HasMessageResponse("agent_fold", sess.ID, m.ID), "message %q is answered", m.Content)
		case domain.MessageRoleAssistant:
			replies = append(replies, m.Content)
		}
	}
	assert.Equal(t, []string{"one reply"}, replies)
}

func TestCLITurn_AnsweredTurnIsNotFolded(t *testing.T) {
	setTestDataDir(t)
	fb := useFakeCLI(t)
	runner := newCLITestRunner(t, "agent_nofold", nil)
	sess, err := NewSessionManager().GetOrCreateNamed("agent_nofold", "main")
	require.NoError(t, err)

	runCLIPrompt(t, runner, sess.ID, "hi")
	runCLIPrompt(t, runner, sess.ID, "again")
	require.Len(t, fb.turns, 2)
	assert.Equal(t, "again", fb.turns[1].text, "a later message is its own turn, sent without the backlog")
}
