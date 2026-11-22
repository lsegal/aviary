package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
)

func TestTaskSessionLivesUnderItsTaskAndFollowsRenames(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	cfg, err := config.Load("")
	require.NoError(t, err)
	cfg.Agents[0].Tasks = []config.TaskConfig{{Name: "report", Prompt: "write the report"}}
	require.NoError(t, config.Save("", cfg))

	// A run from before this change left history in a session named after
	// the task, with no title.
	old, err := agent.NewSessionManager().GetOrCreateNamedTyped("bot", "report", domain.SessionTypeTask)
	require.NoError(t, err)
	require.NoError(t, agent.AppendMessageToSession("bot", old.ID, domain.MessageRoleAssistant, "first report"))
	// A task deleted earlier left a session no task owns.
	_, err = agent.NewSessionManager().GetOrCreateNamedTyped("bot", "gone", domain.SessionTypeTask)
	require.NoError(t, err)

	sess := callJSON(t, d, "task_session", map[string]any{"agent": "bot", "name": "report"})
	assert.Equal(t, "report", sess["id"])
	assert.Equal(t, "report", sess["title"])

	listed := callList(t, d, "session_list", map[string]any{"agent": "bot"})
	assert.NotContains(t, listedNames(listed), "report", "a task's session is not a chat")
	require.Contains(t, listedNames(listed), "gone", "an orphaned task session stays reachable in Chats")
	for _, s := range listed {
		if s["name"] == "gone" {
			assert.Equal(t, "task", s["type"])
		}
	}

	callText(t, d, "config_task_rename", map[string]any{"agent": "bot", "task": "report", "new_name": "digest"})

	renamed := callJSON(t, d, "task_session", map[string]any{"agent": "bot", "name": "digest"})
	assert.Equal(t, "digest", renamed["id"])
	assert.Equal(t, "digest", renamed["title"])
	history := callText(t, d, "session_history", map[string]any{"agent": "bot", "session_id": "digest"})
	assert.Contains(t, history, "first report", "the renamed task keeps its run history")
	listed = callList(t, d, "session_list", map[string]any{"agent": "bot"})
	assert.NotContains(t, listedNames(listed), "digest")
	assert.NotContains(t, listedNames(listed), "report")

	toolCallContains(t, d, "task_session", map[string]any{"agent": "bot", "name": "report"}, "not found")
}
