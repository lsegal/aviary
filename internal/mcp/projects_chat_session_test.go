package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

func TestProjectsChatSession_PersistsAndStaysOutOfChats(t *testing.T) {
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
	require.NoError(t, store.EnsureDirs())

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{})
	SetDeps(&Deps{Agents: mgr})
	c, err := NewInProcessClient(context.Background(), NewServer())
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() }) //nolint:errcheck

	sess, err := agent.NewSessionManager().GetOrCreateNamed(config.ProjectsAgentName, config.ProjectsChatSession)
	require.NoError(t, err)
	assert.Equal(t, config.ProjectsChatSession, sess.ID)
	assert.Equal(t, domain.SessionTypeHidden, sess.Type)
	require.NoError(t, agent.AppendMessageToSession(config.ProjectsAgentName, sess.ID, domain.MessageRoleUser, "What is running?"))
	other, err := agent.NewSessionManager().Create(config.ProjectsAgentName)
	require.NoError(t, err)

	// The Chats list holds the agent's chats but never the prompt chat.
	res, err := c.CallTool(context.Background(), "session_list", map[string]any{"agent": config.ProjectsAgentName})
	require.NoError(t, err)
	var listed []struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(extractText(res)), &listed))
	ids := make([]string, 0, len(listed))
	for _, s := range listed {
		ids = append(ids, s.ID)
	}
	assert.Contains(t, ids, other.ID)
	assert.NotContains(t, ids, config.ProjectsChatSession)

	// A fresh session manager, as after a restart, reopens the same hidden
	// session with its history.
	again, err := agent.NewSessionManager().GetOrCreateNamed(config.ProjectsAgentName, config.ProjectsChatSession)
	require.NoError(t, err)
	assert.Equal(t, sess.ID, again.ID)
	assert.Equal(t, domain.SessionTypeHidden, again.Type)
	res, err = c.CallTool(context.Background(), "session_messages", map[string]any{
		"agent":      config.ProjectsAgentName,
		"session_id": config.ProjectsChatSession,
	})
	require.NoError(t, err)
	assert.Contains(t, extractText(res), "What is running?")

	// Another agent's session of the same name is an ordinary chat.
	plain, err := agent.NewSessionManager().GetOrCreateNamed("bot", config.ProjectsChatSession)
	require.NoError(t, err)
	assert.Equal(t, domain.SessionTypeUser, plain.Type)
}
