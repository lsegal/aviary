package agent

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
)

func TestActiveTurnRecordsToolsAndText(t *testing.T) {
	run := beginActiveTurn("agent_turn", "sess-turn")
	t.Cleanup(run.end)

	turn := GetActiveTurn("agent_turn", "sess-turn")
	assert.True(t, turn.Processing)
	assert.Empty(t, turn.Items)

	run.record(StreamEvent{Type: StreamEventText, Text: "Looking"})
	run.record(StreamEvent{Type: StreamEventText, Text: " it up"})
	run.record(StreamEvent{Type: StreamEventTool, Tool: &ToolEvent{ID: "call_1", Name: "web_search", Args: map[string]any{"q": "x"}}})
	run.record(StreamEvent{Type: StreamEventStatus, Text: "ignored"})

	turn = GetActiveTurn("agent_turn", "sess-turn")
	require.Len(t, turn.Items, 2)
	assert.Equal(t, "Looking it up", turn.Items[0].Text)
	require.NotNil(t, turn.Items[1].Tool)
	assert.Equal(t, "web_search", turn.Items[1].Tool.Name)
	assert.False(t, turn.Items[1].Tool.Done)

	// The call's result updates its entry instead of adding another (#740).
	run.record(StreamEvent{Type: StreamEventTool, Tool: &ToolEvent{ID: "call_1", Name: "web_search", Result: "found", Done: true}})
	// A start delivered again does not reopen a finished call.
	run.record(StreamEvent{Type: StreamEventTool, Tool: &ToolEvent{ID: "call_1", Name: "web_search"}})
	run.record(StreamEvent{Type: StreamEventText, Text: "Done"})

	turn = GetActiveTurn("agent_turn", "sess-turn")
	require.Len(t, turn.Items, 3)
	assert.True(t, turn.Items[1].Tool.Done)
	assert.Equal(t, "found", turn.Items[1].Tool.Result)
	assert.Equal(t, "Done", turn.Items[2].Text)
}

func TestActiveTurnEndsCleanly(t *testing.T) {
	first := beginActiveTurn("agent_turn", "sess-end")
	second := beginActiveTurn("agent_turn", "sess-end")
	first.record(StreamEvent{Type: StreamEventText, Text: "one"})
	second.record(StreamEvent{Type: StreamEventText, Text: "two"})

	turn := GetActiveTurn("agent_turn", "sess-end")
	require.Len(t, turn.Items, 2)
	assert.Equal(t, "one", turn.Items[0].Text)
	assert.Equal(t, "two", turn.Items[1].Text)

	first.end()
	turn = GetActiveTurn("agent_turn", "sess-end")
	assert.True(t, turn.Processing)
	require.Len(t, turn.Items, 1)
	assert.Equal(t, "two", turn.Items[0].Text)

	second.end()
	turn = GetActiveTurn("agent_turn", "sess-end")
	assert.False(t, turn.Processing)
	assert.Empty(t, turn.Items)
	for _, active := range ActiveTurns() {
		assert.NotEqual(t, "sess-end", active.SessionID)
	}
}

func TestActiveTurnObserverEndsWithIdleTurn(t *testing.T) {
	var mu sync.Mutex
	var got []ActiveTurn
	SetActiveTurnObserver(func(turn ActiveTurn) {
		if turn.SessionID != "sess-obs" {
			return
		}
		mu.Lock()
		got = append(got, turn)
		mu.Unlock()
	})
	t.Cleanup(func() { SetActiveTurnObserver(nil) })

	run := beginActiveTurn("agent_turn", "sess-obs")
	run.record(StreamEvent{Type: StreamEventText, Text: "partial"})
	run.record(StreamEvent{Type: StreamEventTool, Tool: &ToolEvent{ID: "call_1", Name: "exec"}})
	run.record(StreamEvent{Type: StreamEventText, Text: "more"})
	run.end()

	// A delayed text update must not land after the turn's end.
	time.Sleep(2 * activeTurnNotifyDelay)
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, got)
	assert.True(t, got[0].Processing)
	last := got[len(got)-1]
	assert.False(t, last.Processing)
	assert.Empty(t, last.Items)
	var sawTool bool
	for _, turn := range got {
		for _, item := range turn.Items {
			if item.Tool != nil && item.Tool.ID == "call_1" {
				sawTool = true
			}
		}
	}
	assert.True(t, sawTool)
}

func TestAgentRunnerRecordsActiveTurn(t *testing.T) {
	var mu sync.Mutex
	var got []ActiveTurn
	SetActiveTurnObserver(func(turn ActiveTurn) {
		if turn.AgentID != "a_turn" {
			return
		}
		mu.Lock()
		got = append(got, turn)
		mu.Unlock()
	})
	t.Cleanup(func() { SetActiveTurnObserver(nil) })

	runner := NewAgentRunner(&domain.Agent{ID: "a_turn", Model: "anthropic/test"}, &config.AgentConfig{Name: "bot"}, nil, nil)
	promptAndWait(t.Context(), t, runner, "hello", nil, RunOverrides{})

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, got)
	assert.True(t, got[0].Processing)
	assert.False(t, got[len(got)-1].Processing)
}
