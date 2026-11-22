package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

func TestEnsureTaskSession_NamesAndTitlesTheSessionAfterTheTask(t *testing.T) {
	setupSessionDir(t)

	sess, err := EnsureTaskSession("assistant", "daily-report")
	require.NoError(t, err)
	assert.Equal(t, "daily-report", sess.ID)
	assert.Equal(t, domain.SessionTypeTask, sess.Type)
	assert.Equal(t, "daily-report", SessionTitle("assistant", sess.ID))

	// The scheduler's runs share the same session.
	again, err := NewSessionManager().GetOrCreateNamedTyped("assistant", "daily-report", domain.SessionTypeTask)
	require.NoError(t, err)
	assert.Equal(t, sess.ID, again.ID)
}

func TestEnsureTaskSession_TitlesASessionFromEarlierRuns(t *testing.T) {
	setupSessionDir(t)
	sm := NewSessionManager()
	old, err := sm.GetOrCreateNamedTyped("assistant", "daily-report", domain.SessionTypeTask)
	require.NoError(t, err)
	require.NoError(t, AppendMessageToSession("assistant", old.ID, domain.MessageRoleAssistant, "report"))

	sess, err := EnsureTaskSession("assistant", "daily-report")
	require.NoError(t, err)
	assert.Equal(t, old.ID, sess.ID)
	assert.Equal(t, "daily-report", SessionTitle("assistant", sess.ID))
	lines, err := store.ReadJSONL[map[string]any](store.SessionPath("assistant", sess.ID))
	require.NoError(t, err)
	assert.Len(t, lines, 2, "earlier runs stay in the session")
}

func TestTaskSessionOwner(t *testing.T) {
	a := config.AgentConfig{Name: "assistant", Tasks: []config.TaskConfig{{Name: "a"}, {Name: "b"}}}
	assert.Equal(t, 1, TaskSessionOwner(a, &domain.Session{Name: "b", Type: domain.SessionTypeTask}))
	assert.Equal(t, -1, TaskSessionOwner(a, &domain.Session{Name: "gone", Type: domain.SessionTypeTask}), "a deleted task's session stays a chat")
	assert.Equal(t, -1, TaskSessionOwner(a, &domain.Session{Name: "b", Type: domain.SessionTypeUser}), "a chat named like a task is not owned")
	assert.Equal(t, -1, TaskSessionOwner(a, nil))
}

func TestRenameTaskSession_CarriesHistoryAndTitle(t *testing.T) {
	setupSessionDir(t)
	sess, err := EnsureTaskSession("assistant", "old")
	require.NoError(t, err)
	require.NoError(t, AppendMessageToSession("assistant", sess.ID, domain.MessageRoleAssistant, "run 1"))

	require.NoError(t, RenameTaskSession("assistant", "old", "new"))

	assert.Empty(t, store.FindSessionPath("assistant", "old"))
	moved, err := NewSessionManager().Load("assistant", "new")
	require.NoError(t, err)
	assert.Equal(t, domain.SessionTypeTask, moved.Type)
	assert.Equal(t, "new", moved.Name)
	assert.Equal(t, "new", SessionTitle("assistant", "new"))
	lines, err := store.ReadJSONL[map[string]any](store.SessionPath("assistant", "new"))
	require.NoError(t, err)
	assert.Len(t, lines, 2)
}

func TestRenameTaskSession_SkipsMissingAndNonTaskSessions(t *testing.T) {
	setupSessionDir(t)
	require.NoError(t, RenameTaskSession("assistant", "never-ran", "renamed"))
	assert.Empty(t, store.FindSessionPath("assistant", "renamed"))

	_, err := NewSessionManager().GetOrCreateNamed("assistant", "chat")
	require.NoError(t, err)
	require.NoError(t, RenameTaskSession("assistant", "chat", "other"))
	assert.Empty(t, store.FindSessionPath("assistant", "other"))
}
