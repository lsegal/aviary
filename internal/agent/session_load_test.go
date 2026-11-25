package agent

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
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

func TestSessionManager_Delete(t *testing.T) {
	setupSessionDir(t)
	sm := NewSessionManager()
	_, err := sm.GetOrCreate("assistant")
	require.NoError(t, err)
	_, err = sm.GetOrCreateNamed(config.ProjectsAgentName, config.ProjectsChatSession)
	require.NoError(t, err)

	for _, tc := range []struct {
		name, agentID, sessionID, wantErr string
	}{
		{"missing session", "assistant", "nope", `session "nope" not found`},
		{"main session is permanent", "assistant", "main", "cannot be deleted; clear it instead"},
		{"code loop chat is permanent", config.ProjectsAgentName, config.ProjectsChatSession, "cannot be deleted; clear it instead"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := sm.Delete(tc.agentID, tc.sessionID)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
	assert.NotEmpty(t, store.FindSessionPath("assistant", "main"), "a refused delete keeps the session")

	sess, err := sm.CreateWithName("assistant", "scratch")
	require.NoError(t, err)
	require.NoError(t, sm.Rename("assistant", sess.ID, "Scratch pad"))
	require.NoError(t, store.WriteSessionChannels(&store.SessionChannelsConfig{
		AgentID: "assistant", SessionID: sess.ID,
		Channels: []store.SessionChannel{{Type: "slack", ID: "C1"}},
	}))

	require.NoError(t, sm.Delete("assistant", sess.ID))
	assert.Empty(t, store.FindSessionPath("assistant", sess.ID))
	for _, sidecar := range []string{store.SessionMetaPath("assistant", sess.ID), store.SessionChannelsPath("assistant", sess.ID)} {
		_, err := os.Stat(sidecar)
		assert.True(t, os.IsNotExist(err), "sidecar %s is removed", sidecar)
	}
}

func TestSessionManager_RenameAndTitle(t *testing.T) {
	setupSessionDir(t)
	sm := NewSessionManager()
	sess, err := sm.Create("assistant")
	require.NoError(t, err)
	assert.Empty(t, SessionTitle("assistant", sess.ID), "a new session has no title")

	err = sm.Rename("assistant", sess.ID, "   ")
	require.EqualError(t, err, "session name is required")
	err = sm.Rename("assistant", "nope", "Title")
	require.EqualError(t, err, `session "nope" not found`)

	require.NoError(t, sm.Rename("assistant", sess.ID, "  Trip planning "))
	assert.Equal(t, "Trip planning", SessionTitle("assistant", sess.ID))
	loaded, err := sm.Load("assistant", sess.ID)
	require.NoError(t, err)
	assert.Equal(t, sess.ID, loaded.Name, "renaming keeps the session name")

	require.NoError(t, os.WriteFile(store.SessionMetaPath("assistant", sess.ID), []byte("{not json"), 0o600))
	assert.Empty(t, SessionTitle("assistant", sess.ID), "an unreadable sidecar has no title")
}

func TestSessionManager_ClearMissingSession(t *testing.T) {
	setupSessionDir(t)
	err := NewSessionManager().Clear("assistant", "nope")
	require.EqualError(t, err, `session "nope" not found`)
}

// seedChannelSession creates agentID's channel session id with one message,
// a title and a channel delivery target.
func seedChannelSession(t *testing.T, agentID, id string) {
	t.Helper()
	sm := NewSessionManager()
	_, err := sm.GetOrCreateNamed(agentID, id)
	require.NoError(t, err)
	require.NoError(t, AppendMessageToSession(agentID, id, domain.MessageRoleUser, "hello from slack"))
	require.NoError(t, sm.Rename(agentID, id, "Team chat"))
	require.NoError(t, store.WriteSessionChannels(&store.SessionChannelsConfig{
		AgentID: agentID, SessionID: id,
		Channels: []store.SessionChannel{{Type: "slack", ID: "C1"}},
	}))
}

func sessionMessages(t *testing.T, agentID, sessionID string) []domain.Message {
	t.Helper()
	lines, err := store.ReadJSONL[domain.Message](store.SessionPath(agentID, sessionID))
	require.NoError(t, err)
	var msgs []domain.Message
	for _, l := range lines {
		if l.Role != "" {
			msgs = append(msgs, l)
		}
	}
	return msgs
}

func TestSessionManager_ArchiveDetachesChannelSession(t *testing.T) {
	setupSessionDir(t)
	seedChannelSession(t, "assistant", "slack:C1")

	archived, err := NewSessionManager().Archive("assistant", "slack:C1", " Old team chat ")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(archived.ID, "sess_"), archived.ID)
	assert.Equal(t, archived.ID, archived.Name)
	assert.Equal(t, domain.SessionTypeUser, archived.Type, "an archived channel session becomes a regular chat")
	assert.Equal(t, "Old team chat", archived.Title)

	msgs := sessionMessages(t, "assistant", archived.ID)
	require.Len(t, msgs, 1)
	assert.Equal(t, "hello from slack", msgs[0].Content)

	assert.Empty(t, store.FindSessionPath("assistant", "slack:C1"), "the channel session is removed")
	cfg, err := store.ReadSessionChannels("assistant", archived.ID)
	require.NoError(t, err)
	assert.Empty(t, cfg.Channels, "an archived session loses its channel delivery")

	_, err = NewSessionManager().Archive("assistant", "slack:C1", "again")
	require.EqualError(t, err, `session "slack:C1" not found`)
}

func TestSessionManager_RekeyMovesChannelDelivery(t *testing.T) {
	setupSessionDir(t)
	seedChannelSession(t, "assistant", "slack:C1")
	sm := NewSessionManager()

	moved, err := sm.Rekey("assistant", "slack:C1", "slack:C2")
	require.NoError(t, err)
	assert.Equal(t, "slack:C2", moved.ID)
	assert.Equal(t, "slack:C2", moved.Name)
	assert.Equal(t, domain.SessionTypeChannel, moved.Type, "rekeying keeps the type")
	assert.Equal(t, "Team chat", moved.Title, "rekeying keeps the title")
	require.Len(t, sessionMessages(t, "assistant", "slack:C2"), 1)
	assert.Empty(t, store.FindSessionPath("assistant", "slack:C1"))

	cfg, err := store.ReadSessionChannels("assistant", "slack:C2")
	require.NoError(t, err)
	assert.Equal(t, "slack:C2", cfg.SessionID)
	require.Len(t, cfg.Channels, 1)
	assert.Equal(t, "C1", cfg.Channels[0].ID)

	_, err = sm.GetOrCreateNamed("assistant", "slack:C3")
	require.NoError(t, err)
	_, err = sm.Rekey("assistant", "slack:C2", "slack:C3")
	require.EqualError(t, err, `session "slack:C3" already exists`)
	assert.NotEmpty(t, store.FindSessionPath("assistant", "slack:C2"), "a refused rekey keeps the session")
}

func TestSessionManager_RekeyHeaderlessSessionWritesHeader(t *testing.T) {
	setupSessionDir(t)
	writeHeaderlessSession(t, "assistant", "slack:C1")

	moved, err := NewSessionManager().Rekey("assistant", "slack:C1", "slack:C9")
	require.NoError(t, err)
	assert.Equal(t, "slack:C9", moved.ID)
	header := headerOf(t, "assistant", "slack:C9")
	require.NotNil(t, header)
	assert.Equal(t, "slack:C9", header["id"])
	msgs := sessionMessages(t, "assistant", "slack:C9")
	require.Len(t, msgs, 1)
	assert.Equal(t, "msg_1", msgs[0].ID)
}

func TestAppendMediaAndWorkflowMessages(t *testing.T) {
	setupSessionDir(t)
	sess, err := NewSessionManager().Create("assistant")
	require.NoError(t, err)

	require.NoError(t, AppendMediaMessageToSession("assistant", sess.ID, domain.MessageRoleUser, "look", []string{"file:///a.png"}))
	sender := domain.NewMessageSender("U1", "Ann", true)
	at := time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC)
	workflows := []domain.MessageWorkflow{{TaskID: "triage", Name: "Triage", JobID: "job_1"}}
	require.NoError(t, AppendWorkflowMessageToSession("assistant", sess.ID, "bug report", []string{"file:///b.png"}, sender, workflows, at))

	msgs := sessionMessages(t, "assistant", sess.ID)
	require.Len(t, msgs, 2)
	assert.Equal(t, domain.MessageRoleUser, msgs[0].Role)
	assert.Equal(t, "look", msgs[0].Content)
	assert.Equal(t, []string{"file:///a.png"}, msgs[0].MediaURLs)
	assert.Nil(t, msgs[0].Sender)

	assert.Equal(t, domain.MessageRoleUser, msgs[1].Role, "a workflow-handled channel message is a user message")
	assert.Equal(t, "bug report", msgs[1].Content)
	assert.Equal(t, []string{"file:///b.png"}, msgs[1].MediaURLs)
	assert.Equal(t, workflows, msgs[1].Workflows)
	require.NotNil(t, msgs[1].Sender)
	assert.Equal(t, "Ann", msgs[1].Sender.Name)
	assert.True(t, at.Equal(msgs[1].Timestamp), "the message keeps the time workflows handled it")

	meta, err := store.ReadSessionMeta(store.SessionMetaPath("assistant", sess.ID))
	require.NoError(t, err)
	require.NotNil(t, meta.LastMessageAt)
	assert.True(t, meta.LastMessageAt.After(at), "an older workflow message does not move the last message time back")
}
