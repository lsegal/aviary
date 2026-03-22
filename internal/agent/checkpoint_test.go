package agent

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingProvider blocks in Stream until release is closed (or ctx is done).
// The ready channel is closed when Stream is first entered.
type blockingProvider struct {
	ready   chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingProvider() *blockingProvider {
	return &blockingProvider{
		ready:   make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (b *blockingProvider) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Event, error) {
	b.once.Do(func() { close(b.ready) })
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	ch := make(chan llm.Event, 1)
	ch <- llm.Event{Type: llm.EventTypeDone}
	close(ch)
	return ch, nil
}

// checkpointCount returns the number of .json checkpoint files for an agent.
func checkpointCount(agentID string) int {
	entries, err := os.ReadDir(store.CheckpointDir(agentID))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && len(e.Name()) > 5 && e.Name()[len(e.Name())-5:] == ".json" {
			n++
		}
	}
	return n
}

// newTestRunner creates a minimal AgentRunner and registers it in m.
func newTestRunner(m *Manager, agentID, agentName string, provider llm.Provider) *AgentRunner {
	a := &domain.Agent{ID: agentID, Name: agentName}
	cfg := &config.AgentConfig{Name: agentName}
	runner := NewAgentRunner(a, cfg, provider, nil)
	m.runners[agentName] = runner
	return runner
}

// writeTestCheckpoint writes a checkpoint file for agentID and returns its path.
func writeTestCheckpoint(t *testing.T, agentID, sessionID string, createdAt time.Time) string {
	t.Helper()
	cp := &RunCheckpoint{
		AgentName: "testagent",
		SessionID: sessionID,
		Message:   "hello",
		CreatedAt: createdAt,
	}
	path := store.CheckpointPath(agentID, "cp_001")
	require.NoError(t, store.WriteJSON(path, cp))
	return path
}

func writeCheckpointFile(t *testing.T, agentID, checkpointID string, cp *RunCheckpoint) string {
	t.Helper()
	path := store.CheckpointPath(agentID, checkpointID)
	require.NoError(t, store.WriteJSON(path, cp))
	return path
}

// --- recoverCheckpoints tests ---

func TestRecoverCheckpoints_NoDir(t *testing.T) {
	setTestDataDir(t)
	m := NewManager(nil)
	runner := newTestRunner(m, "agent_nodir", "nodir", &mockProvider{})
	// No checkpoint dir exists; should return without error or panic.
	m.recoverCheckpoints(runner)
}

func TestRecoverCheckpoints_TimedOut(t *testing.T) {
	setTestDataDir(t)
	m := NewManager(nil) // cfg == nil → uses DefaultFailedTaskTimeout (6h)
	runner := newTestRunner(m, "agent_timedout", "timedout", &mockProvider{})

	sessionID := "sess_abc"
	path := writeTestCheckpoint(t, "agent_timedout", sessionID, time.Now().Add(-7*time.Hour))

	m.recoverCheckpoints(runner)

	// Checkpoint file should be deleted.
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "checkpoint file should be deleted after timeout")

	// Session should have a timeout message appended.
	msgs, err := store.ReadJSONL[domain.Message](store.SessionPath("agent_timedout", sessionID))
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, domain.MessageRoleAssistant, msgs[0].Role)
	assert.Contains(t, msgs[0].Content, "interrupted")
}

func TestRecoverCheckpoints_Fresh(t *testing.T) {
	setTestDataDir(t)

	provider := &sequenceProvider{}
	m := NewManager(nil)
	runner := newTestRunner(m, "agent_fresh", "fresh", provider)

	sessionID := "sess_fresh"
	path := writeTestCheckpoint(t, "agent_fresh", sessionID, time.Now())
	require.NoError(t, store.AppendJSONL(store.SessionPath("agent_fresh", sessionID), domain.Message{
		ID:        "cp_001",
		Role:      domain.MessageRoleUser,
		Content:   "hello",
		Timestamp: time.Now(),
	}))

	m.recoverCheckpoints(runner)

	// RetryCount should be incremented synchronously before the goroutine starts.
	cp, err := store.ReadJSON[RunCheckpoint](path)
	require.NoError(t, err)
	assert.Equal(t, 1, cp.RetryCount)
	assert.False(t, cp.LastRecoveredAt.IsZero())

	// Wait for the re-issued prompt to finish, then verify the provider was called.
	runner.Wait()
	assert.GreaterOrEqual(t, provider.callCount(), 1)
	assert.Equal(t, 0, checkpointCount("agent_fresh"), "original checkpoint should be deleted after successful recovery")

	msgs, err := store.ReadJSONL[domain.Message](store.SessionPath("agent_fresh", sessionID))
	require.NoError(t, err)
	userCount := 0
	for _, msg := range msgs {
		if msg.Role == domain.MessageRoleUser {
			userCount++
		}
	}
	assert.Equal(t, 1, userCount, "recovery should not append a duplicate user message")
}

func TestRecoverCheckpoints_SkipsRecentlyRecovered(t *testing.T) {
	setTestDataDir(t)

	provider := &sequenceProvider{}
	m := NewManager(nil)
	runner := newTestRunner(m, "agent_recent", "recent", provider)

	sessionID := "sess_recent"
	writeCheckpointFile(t, "agent_recent", "cp_recent", &RunCheckpoint{
		AgentName:       "recent",
		SessionID:       sessionID,
		Message:         "hello",
		CreatedAt:       time.Now().Add(-time.Minute),
		RetryCount:      1,
		LastRecoveredAt: time.Now().Add(-5 * time.Second),
	})

	m.recoverCheckpoints(runner)

	runner.Wait()
	assert.Equal(t, 0, provider.callCount(), "recently recovered checkpoint should not be re-issued again immediately")
	assert.Equal(t, 1, checkpointCount("agent_recent"), "checkpoint should remain for a later retry window")
}

func TestRecoverCheckpoints_RetryLimit(t *testing.T) {
	setTestDataDir(t)

	m := NewManager(nil)
	runner := newTestRunner(m, "agent_retry_limit", "retry-limit", &mockProvider{})

	sessionID := "sess_retry_limit"
	path := writeCheckpointFile(t, "agent_retry_limit", "cp_retry_limit", &RunCheckpoint{
		AgentName:       "retry-limit",
		SessionID:       sessionID,
		Message:         "hello",
		CreatedAt:       time.Now().Add(-time.Minute),
		RetryCount:      maxCheckpointRecoveryRetries,
		LastRecoveredAt: time.Now().Add(-2 * time.Minute),
	})

	m.recoverCheckpoints(runner)

	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "checkpoint should be deleted once the retry limit is reached")

	msgs, err := store.ReadJSONL[domain.Message](store.SessionPath("agent_retry_limit", sessionID))
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0].Content, "stopped retrying")
}

func TestRecoverCheckpoints_UnreadableFile(t *testing.T) {
	setTestDataDir(t)
	m := NewManager(nil)
	runner := newTestRunner(m, "agent_corrupt", "corrupt", &mockProvider{})

	// Write a corrupt (non-JSON) checkpoint file.
	dir := store.CheckpointDir("agent_corrupt")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	badPath := dir + "/bad.json"
	require.NoError(t, os.WriteFile(badPath, []byte("not json"), 0o600))

	m.recoverCheckpoints(runner)

	// Corrupt file should be deleted.
	_, err := os.Stat(badPath)
	assert.True(t, os.IsNotExist(err), "corrupt checkpoint file should be deleted")
}

// --- runner checkpoint lifecycle tests ---

func TestRunnerCheckpoint_WrittenAndDeletedOnCompletion(t *testing.T) {
	setTestDataDir(t)

	bp := newBlockingProvider()
	a := &domain.Agent{ID: "agent_wr", Name: "wr"}
	runner := NewAgentRunner(a, &config.AgentConfig{Name: "wr"}, bp, nil)

	runner.Prompt(context.Background(), "ping")

	// Wait until Stream is entered; checkpoint should be on disk by then.
	select {
	case <-bp.ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for stream to start")
	}
	assert.Equal(t, 1, checkpointCount("agent_wr"), "checkpoint should exist while prompt is running")

	// Unblock the stream and wait for the goroutine to fully exit (including defer).
	close(bp.release)
	runner.Wait()

	assert.Equal(t, 0, checkpointCount("agent_wr"), "checkpoint should be deleted after normal completion")
}

func TestRunnerCheckpoint_KeptOnServerStop(t *testing.T) {
	setTestDataDir(t)

	bp := newBlockingProvider()
	a := &domain.Agent{ID: "agent_stop", Name: "stop"}
	runner := NewAgentRunner(a, &config.AgentConfig{Name: "stop"}, bp, nil)

	runner.Prompt(context.Background(), "ping")

	// Wait until Stream is entered; checkpoint should be on disk.
	select {
	case <-bp.ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for stream to start")
	}
	assert.Equal(t, 1, checkpointCount("agent_stop"), "checkpoint should exist while prompt is running")

	// Stop the runner (server-initiated stop).
	runner.Stop()
	runner.Wait()

	assert.Equal(t, 1, checkpointCount("agent_stop"), "checkpoint should be kept after server stop")
}

func TestRunnerCheckpoint_NotWrittenForScheduledTask(t *testing.T) {
	setTestDataDir(t)

	bp := newBlockingProvider()
	a := &domain.Agent{ID: "agent_task", Name: "task"}
	runner := NewAgentRunner(a, &config.AgentConfig{Name: "task"}, bp, nil)

	ctx := WithTaskID(context.Background(), "task/check")
	runner.Prompt(ctx, "ping")

	select {
	case <-bp.ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for stream to start")
	}
	assert.Equal(t, 0, checkpointCount("agent_task"), "scheduled task runs should not create checkpoints")

	close(bp.release)
	runner.Wait()
}
