package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
)

func (r *recordingToolClient) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func heldToolTurn(t *testing.T, id string) (*AgentRunner, *recordingToolClient, string) {
	t.Helper()
	setTestDataDir(t)
	toolClient := &recordingToolClient{tools: []ToolInfo{{Name: "web_search", Description: "Search the web"}}}
	SetToolClientFactory(func(_ context.Context) (ToolClient, error) { return toolClient, nil })
	t.Cleanup(func() { SetToolClientFactory(nil) })
	provider := &sequenceProvider{responses: [][]llm.Event{
		{{Type: llm.EventTypeToolCall, ToolCall: &llm.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang"}}}, {Type: llm.EventTypeDone}},
		{{Type: llm.EventTypeText, Text: "final answer"}, {Type: llm.EventTypeDone}},
	}}
	runner := NewAgentRunner(&domain.Agent{ID: id, Name: id, Model: "test/model"}, &config.AgentConfig{Name: id, Model: "test/model"}, provider, nil)
	sess, err := NewSessionManager().GetOrCreateNamed(id, "main")
	assert.NoError(t, err)
	return runner, toolClient, sess.ID
}

// #459: a turn whose hold is engaged starts no tool call until the hold is
// released, then finishes normally.
func TestTurnHoldHoldsToolCallsUntilReleased(t *testing.T) {
	runner, toolClient, sessionID := heldToolTurn(t, "agent_turn_hold")
	hold := &TurnHold{}
	hold.Engage()
	ended := make(chan StreamEventType, 1)
	ctx := WithTurnHold(WithSessionID(context.Background(), sessionID), hold)
	runner.Prompt(ctx, "search", func(e StreamEvent) {
		switch e.Type {
		case StreamEventDone, StreamEventError, StreamEventStop:
			ended <- e.Type
		}
	})
	select {
	case typ := <-ended:
		t.Fatalf("held turn ended with %s", typ)
	case <-time.After(200 * time.Millisecond):
	}
	assert.Equal(t, 0, toolClient.callCount())
	hold.Release()
	select {
	case typ := <-ended:
		assert.Equal(t, StreamEventDone, typ)
	case <-time.After(3 * time.Second):
		t.Fatal("released turn did not finish")
	}
	runner.Wait()
	assert.Equal(t, 1, toolClient.callCount())
}

// Cancelling a held turn stops it without running the held tool call.
func TestTurnHoldStopsWithTheTurn(t *testing.T) {
	runner, toolClient, sessionID := heldToolTurn(t, "agent_turn_hold_cancel")
	hold := &TurnHold{}
	hold.Engage()
	ctx, cancel := context.WithCancel(WithTurnHold(WithSessionID(context.Background(), sessionID), hold))
	ended := make(chan StreamEventType, 1)
	runner.Prompt(ctx, "search", func(e StreamEvent) {
		switch e.Type {
		case StreamEventDone, StreamEventError, StreamEventStop:
			ended <- e.Type
		}
	})
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case typ := <-ended:
		assert.Equal(t, StreamEventStop, typ)
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled held turn did not stop")
	}
	runner.Wait()
	assert.Equal(t, 0, toolClient.callCount())
}

func TestTurnHoldNilNeverWaits(t *testing.T) {
	var hold *TurnHold
	assert.NoError(t, hold.Wait(context.Background()))
	assert.False(t, hold.Engaged())
	assert.Nil(t, TurnHoldFromContext(WithTurnHold(context.Background(), nil)))
}
