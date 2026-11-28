package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

func TestSnapshotUsageSession(t *testing.T) {
	setupSessionDir(t)
	m := NewSessionManager()
	sess, err := m.CreateWithName("bot", "")
	require.NoError(t, err)

	t.Run("an untitled chat is a new chat", func(t *testing.T) {
		u := &domain.UsageRecord{AgentID: "bot", SessionID: sess.ID}
		snapshotUsageSession(context.Background(), u)
		assert.Equal(t, "New chat", u.SessionName)
		assert.Equal(t, domain.UsageKindChat, u.SessionKind)
	})

	t.Run("a titled chat takes its title", func(t *testing.T) {
		require.NoError(t, m.Rename("bot", sess.ID, "Plan the trip"))
		u := &domain.UsageRecord{AgentID: "bot", SessionID: sess.ID, Channel: "slack"}
		snapshotUsageSession(context.Background(), u)
		assert.Equal(t, "Plan the trip", u.SessionName)
		assert.Equal(t, "slack", u.SessionOwner)
	})

	t.Run("main chat", func(t *testing.T) {
		main, err := m.GetOrCreate("bot")
		require.NoError(t, err)
		u := &domain.UsageRecord{AgentID: "bot", SessionID: main.ID}
		snapshotUsageSession(context.Background(), u)
		assert.Equal(t, "Main Chat", u.SessionName)
	})

	t.Run("a task run", func(t *testing.T) {
		u := &domain.UsageRecord{AgentID: "bot", SessionID: "x", Task: "bot/digest"}
		snapshotUsageSession(context.Background(), u)
		assert.Equal(t, UsageSession{Name: "digest", Kind: domain.UsageKindTask, Owner: "bot/digest"},
			UsageSession{Name: u.SessionName, Kind: u.SessionKind, Owner: u.SessionOwner})
	})

	t.Run("a workflow run", func(t *testing.T) {
		u := &domain.UsageRecord{AgentID: "bot", SessionID: "x", Task: "bot/triage", Channel: "Team Slack"}
		snapshotUsageSession(context.Background(), u)
		assert.Equal(t, UsageSession{Name: "triage", Kind: domain.UsageKindWorkflow, Owner: "Team Slack"},
			UsageSession{Name: u.SessionName, Kind: u.SessionKind, Owner: u.SessionOwner})
	})

	t.Run("the caller names the run", func(t *testing.T) {
		want := UsageSession{Name: "Fix the parser", Kind: domain.UsageKindJob, Owner: "o/r#1"}
		u := &domain.UsageRecord{AgentID: "bot", SessionID: sess.ID, Task: "bot/digest"}
		snapshotUsageSession(WithUsageSession(context.Background(), want), u)
		assert.Equal(t, want, UsageSession{Name: u.SessionName, Kind: u.SessionKind, Owner: u.SessionOwner})
	})
}

func TestSessionDeleteRecordsTombstone(t *testing.T) {
	setupSessionDir(t)
	m := NewSessionManager()
	sess, err := m.CreateWithName("bot", "")
	require.NoError(t, err)
	require.NoError(t, m.Rename("bot", sess.ID, "Weekly notes"))
	moved, err := m.CreateWithName("bot", "")
	require.NoError(t, err)

	_, err = m.Archive("bot", moved.ID, "Old")
	require.NoError(t, err)
	require.NoError(t, m.Delete("bot", sess.ID))

	got, err := store.ReadJSONL[domain.DeletedSession](store.DeletedSessionsPath())
	require.NoError(t, err)
	require.Len(t, got, 1, "archiving moves a session rather than deleting it")
	assert.Equal(t, "bot", got[0].AgentID)
	assert.Equal(t, sess.ID, got[0].SessionID)
	assert.Equal(t, "Weekly notes", got[0].Name)
	assert.False(t, got[0].DeletedAt.IsZero())
}

func TestRecordSessionsDeleted(t *testing.T) {
	setupSessionDir(t)
	sess, err := NewSessionManager().CreateWithName("bot", "named")
	require.NoError(t, err)
	RecordSessionsDeleted("bot", []*domain.Session{sess})
	got, err := store.ReadJSONL[domain.DeletedSession](store.DeletedSessionsPath())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "named", got[0].Name)
}
