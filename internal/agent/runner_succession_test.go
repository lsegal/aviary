package agent

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// eventLog collects a prompt's stream events.
type eventLog struct {
	mu     sync.Mutex
	events []StreamEvent
}

func (l *eventLog) consume(e StreamEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func (l *eventLog) ofType(typ StreamEventType) []StreamEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []StreamEvent
	for _, e := range l.events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func startBlockedPrompt(t *testing.T, runner *AgentRunner, bp *blockingProvider, sessionID string) *eventLog {
	t.Helper()
	log := &eventLog{}
	runner.Prompt(WithSessionID(context.Background(), sessionID), "file the bug", log.consume)
	select {
	case <-bp.ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for stream to start")
	}
	return log
}

func TestManager_Reconcile_UpdateLetsInFlightPromptFinish(t *testing.T) {
	setTestDataDir(t)
	m := newTestManager(t)
	bp := newBlockingProvider()
	old := newTestRunner(m, "bot", "bot", bp)
	log := startBlockedPrompt(t, old, bp, "sess_reload")

	// A config change (a channel rename, a settings save) updates the agent.
	m.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}})
	m.WaitRecovery()

	next, ok := m.Get("bot")
	require.True(t, ok)
	require.NotSame(t, old, next)
	assert.Empty(t, log.ofType(StreamEventStop), "a config change must not stop the running turn")

	// Recovery by the new runner must not replay the turn still running.
	entries, err := os.ReadDir(store.CheckpointDir("bot"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	cp, err := store.ReadJSON[RunCheckpoint](filepath.Join(store.CheckpointDir("bot"), entries[0].Name()))
	require.NoError(t, err)
	assert.Zero(t, cp.RetryCount)

	close(bp.release)
	next.Wait()
	assert.Len(t, log.ofType(StreamEventDone), 1)
	assert.Empty(t, log.ofType(StreamEventStop))
	assert.Equal(t, 0, checkpointCount("bot"))
}

func TestManager_Reconcile_ReplacedRunnerForwardsNewPrompts(t *testing.T) {
	setTestDataDir(t)
	m := newTestManager(t)
	old := newTestRunner(m, "bot", "bot", &sequenceProvider{})
	m.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}})
	next, _ := m.Get("bot")
	provider := &sequenceProvider{}
	next.provider = provider

	log := &eventLog{}
	old.Prompt(WithSessionID(context.Background(), "sess_forward"), "hello", log.consume)
	next.Wait()

	assert.Equal(t, 1, provider.callCount(), "a prompt reaching the replaced runner runs on its successor")
	assert.Len(t, log.ofType(StreamEventDone), 1)
	assert.Empty(t, log.ofType(StreamEventStop))
}

func TestManager_Reconcile_StopReachesReplacedRunner(t *testing.T) {
	setTestDataDir(t)
	m := newTestManager(t)
	bp := newBlockingProvider()
	old := newTestRunner(m, "bot", "bot", bp)
	log := startBlockedPrompt(t, old, bp, "sess_stop")
	m.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}})
	next, _ := m.Get("bot")

	// agent_stop stops the agent's current runner; the turn still running on
	// the runner it replaced stops with it, reported once as a requested stop.
	next.Stop()
	next.Wait()

	stops := log.ofType(StreamEventStop)
	require.Len(t, stops, 1)
	assert.False(t, stops[0].Interrupted)
}

func TestRunner_StopEventReason(t *testing.T) {
	for _, tc := range []struct {
		name        string
		stop        func(*AgentRunner)
		interrupted bool
	}{
		{name: "stop request", stop: (*AgentRunner).Stop},
		{name: "shutdown", stop: (*AgentRunner).Interrupt, interrupted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setTestDataDir(t)
			m := newTestManager(t)
			bp := newBlockingProvider()
			runner := newTestRunner(m, "bot", "bot", bp)
			log := startBlockedPrompt(t, runner, bp, "sess_reason")

			tc.stop(runner)
			runner.Wait()

			stops := log.ofType(StreamEventStop)
			require.Len(t, stops, 1, "a stopped turn reports its stop once")
			assert.Equal(t, tc.interrupted, stops[0].Interrupted)
		})
	}
}

func TestManager_Stop_InterruptsPrompts(t *testing.T) {
	setTestDataDir(t)
	m := newTestManager(t)
	bp := newBlockingProvider()
	runner := newTestRunner(m, "bot", "bot", bp)
	log := startBlockedPrompt(t, runner, bp, "sess_shutdown")

	m.Stop()
	runner.Wait()

	stops := log.ofType(StreamEventStop)
	require.Len(t, stops, 1)
	assert.True(t, stops[0].Interrupted)
	assert.Equal(t, 1, checkpointCount("bot"), "an interrupted prompt is kept for recovery")
}

func TestRecoverCheckpoints_DeliversReplyOfStreamedPrompt(t *testing.T) {
	setTestDataDir(t)
	m := newTestManager(t)
	provider := &sequenceProvider{responses: [][]llm.Event{{{Type: llm.EventTypeText, Text: "filed it"}}}}
	runner := newTestRunner(m, "bot", "bot", provider)

	var mu sync.Mutex
	var delivered []string
	RegisterSessionDelivery("bot", "sess_recover", "slack", "C123", func(text string) {
		mu.Lock()
		defer mu.Unlock()
		delivered = append(delivered, text)
	})
	t.Cleanup(func() { ClearSessionDelivery("bot", "sess_recover") })

	// A Slack prompt streams its reply itself, so its checkpoint records
	// suppressed delivery; after a restart that stream is gone.
	writeCheckpointFile(t, "bot", "cp_slack", &RunCheckpoint{
		AgentName: "bot",
		SessionID: "sess_recover",
		Message:   "file the bug",
		Overrides: RunOverrides{SuppressDelivery: true},
		CreatedAt: time.Now(),
	})
	m.recoverCheckpoints(runner)
	runner.Wait()

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"filed it"}, delivered)
}
