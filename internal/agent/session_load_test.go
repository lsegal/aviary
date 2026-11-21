package agent

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// writeHeaderlessSession writes a session file that starts with a message, as
// left behind when messages land after the session file was removed.
func writeHeaderlessSession(t *testing.T, agentID, sessionID string) {
	t.Helper()
	require.NoError(t, store.AppendJSONL(store.SessionPath(agentID, sessionID), map[string]any{
		"id": "msg_1", "role": "user", "content": "hi", "timestamp": "2026-11-21T12:29:00Z",
	}))
}

func headerOf(t *testing.T, agentID, sessionID string) map[string]any {
	t.Helper()
	lines, err := store.ReadJSONL[map[string]any](store.SessionPath(agentID, sessionID))
	require.NoError(t, err)
	return sessionHeader(lines)
}

func setupSessionDir(t *testing.T) {
	t.Helper()
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
	require.NoError(t, store.EnsureDirs())
}

func TestSessionLoad_BackfillsMissingHeaderAndKeepsMessages(t *testing.T) {
	setupSessionDir(t)
	writeHeaderlessSession(t, "assistant", "main")

	sess, err := NewSessionManager().Load("assistant", "main")
	require.NoError(t, err)
	assert.Equal(t, "assistant", sess.AgentID)
	assert.Equal(t, "main", sess.ID)
	assert.Equal(t, "main", sess.Name)
	assert.Equal(t, domain.SessionTypeMain, sess.Type)
	assert.Equal(t, "2026-11-21T12:29:00Z", sess.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"))

	header := headerOf(t, "assistant", "main")
	require.NotNil(t, header, "the repaired header is written back")
	assert.Equal(t, "main", header["id"])
	lines, err := store.ReadJSONL[map[string]any](store.SessionPath("assistant", "main"))
	require.NoError(t, err)
	require.Len(t, lines, 2)
	assert.Equal(t, "msg_1", lines[1]["id"])

	// A restart reads the session from disk again and finds the same identity.
	again, err := NewSessionManager().GetOrCreate("assistant")
	require.NoError(t, err)
	assert.Equal(t, sess.ID, again.ID)
	assert.Equal(t, sess.Type, again.Type)
	assert.Equal(t, sess.CreatedAt, again.CreatedAt)
}

func TestSessionLoad_NotFound(t *testing.T) {
	setupSessionDir(t)
	_, err := NewSessionManager().Load("assistant", "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSessionClear_KeepsBackfilledHeader(t *testing.T) {
	setupSessionDir(t)
	writeHeaderlessSession(t, "assistant", "main")
	sm := NewSessionManager()

	require.NoError(t, sm.Clear("assistant", "main"))
	sess, err := sm.Load("assistant", "main")
	require.NoError(t, err)
	assert.Equal(t, "assistant", sess.AgentID)
	assert.Equal(t, domain.SessionTypeMain, sess.Type)

	require.NoError(t, AppendMessageToSession("assistant", "main", domain.MessageRoleUser, "again"))
	require.NoError(t, sm.Clear("assistant", "main"))
	lines, err := store.ReadJSONL[map[string]any](store.SessionPath("assistant", "main"))
	require.NoError(t, err)
	require.Len(t, lines, 1)
	assert.Equal(t, "main", lines[0]["id"])
	assert.Equal(t, string(domain.SessionTypeMain), lines[0]["type"])
}

func TestAppendMessage_WritesHeaderForMissingSessionFile(t *testing.T) {
	setupSessionDir(t)
	sm := NewSessionManager()
	sess, err := sm.Create("assistant")
	require.NoError(t, err)
	require.NoError(t, os.Remove(store.SessionPath("assistant", sess.ID)))

	require.NoError(t, AppendMessageToSession("assistant", sess.ID, domain.MessageRoleAssistant, "late reply"))
	require.NoError(t, MarkMessageResponded("assistant", "main", "msg_1", "msg_2"))
	for _, id := range []string{sess.ID, "main"} {
		header := headerOf(t, "assistant", id)
		require.NotNil(t, header, "session %s", id)
		assert.Equal(t, id, header["id"])
	}
}
