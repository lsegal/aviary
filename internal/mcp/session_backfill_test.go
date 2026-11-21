package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/store"
)

func TestAgentRun_SendsToMainSessionMissingItsHeader(t *testing.T) {
	d, model := setupScriptedAgent(t)
	// Main Chat's file starts with a message: its header was lost when the
	// file was removed while messages were still being written.
	require.NoError(t, store.AppendJSONL(store.SessionPath("bot", "main"), map[string]any{
		"id": "msg_1", "role": "user", "content": "hi", "timestamp": "2026-11-21T12:29:00Z",
	}))

	model.set("agent_list", map[string]any{})
	out, err := d.CallTool(context.Background(), "agent_run", map[string]any{
		"name": "bot", "session_id": "main", "message": "hey it looks like the tool call thing is resolved?",
	})
	require.NoError(t, err)
	assert.NotContains(t, out, "missing agent metadata")
	assert.Contains(t, out, "bot")

	lines, err := store.ReadJSONL[map[string]any](store.SessionPath("bot", "main"))
	require.NoError(t, err)
	require.NotEmpty(t, lines)
	assert.Equal(t, "main", lines[0]["id"])
	assert.Equal(t, "main", lines[0]["type"])
	assert.NotEmpty(t, lines[0]["created_at"])

	sess, err := loadSessionByID("bot", "main")
	require.NoError(t, err)
	assert.Equal(t, "bot", sess.AgentID)
}
