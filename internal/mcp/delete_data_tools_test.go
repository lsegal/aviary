package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// ranWorkflow creates bot's workflow name on the Team channel and gives it
// the session and run record a run leaves.
func ranWorkflow(t *testing.T, d *Dispatcher, name string) string {
	t.Helper()
	_, err := d.CallTool(context.Background(), "task_schedule", map[string]any{
		"agent": "bot", "name": name, "content": "Be nice.",
		"trigger": "message", "channel": "Team", "target": "reply",
	})
	require.NoError(t, err)
	sess, err := agent.EnsureTaskSession("bot", name)
	require.NoError(t, err)
	require.NoError(t, agent.AppendMessageToSession("bot", sess.ID, domain.MessageRoleAssistant, "Hello!"))
	run := store.JobPath("bot", "job_"+name)
	require.NoError(t, store.WriteJSON(run, domain.Job{ID: "job_" + name, TaskID: "bot/" + name, AgentID: "bot"}))
	return run
}

// chatTitles lists the titles session_list shows in Chats.
func chatTitles(t *testing.T, d *Dispatcher) []string {
	t.Helper()
	var titles []string
	for _, s := range callList(t, d, "session_list", map[string]any{"agent": "bot"}) {
		title, _ := s["title"].(string)
		if title == "" {
			title, _ = s["name"].(string)
		}
		titles = append(titles, title)
	}
	return titles
}

func TestTaskDeleteDataRemovesSessionAndRuns(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	addTeamChannel(t)
	run := ranWorkflow(t, d, "be-nice")

	counts := callJSON(t, d, "delete_preview", map[string]any{"kind": "workflow", "name": "bot/be-nice"})
	assert.Equal(t, map[string]any{"sessions": 1.0, "messages": 1.0, "runs": 1.0, "files": 0.0}, counts)

	out := callText(t, d, "task_delete", map[string]any{"name": "bot/be-nice", "delete_data": true})
	assert.Contains(t, out, "with its session and run history")
	assert.Empty(t, store.FindSessionPath("bot", "be-nice"))
	assert.NoFileExists(t, run)
	assert.NotContains(t, chatTitles(t, d), "be-nice")
	assert.NotContains(t, chatTitles(t, d), "be-nice (archived)")
}

func TestTaskDeleteKeepsDataAsRegularChat(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	addTeamChannel(t)
	run := ranWorkflow(t, d, "be-nice")
	assert.NotContains(t, chatTitles(t, d), "be-nice", "an owned task session is not a chat")

	out := callText(t, d, "task_delete", map[string]any{"name": "bot/be-nice"})
	assert.Contains(t, out, "kept as a regular chat")
	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Empty(t, cfg.Agents[0].Tasks)
	assert.FileExists(t, run, "the run history stays on disk")
	titles := chatTitles(t, d)
	assert.NotContains(t, titles, "be-nice", "no session lingers as if the workflow existed")
	assert.Contains(t, titles, "be-nice (archived)")
}

func TestAgentDeleteData(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	t.Setenv("AVIARY_CONFIG_BASE_DIR", store.DataDir())
	cfg, err := config.Load("")
	require.NoError(t, err)
	cfg.Agents = append(cfg.Agents, config.AgentConfig{Name: "keeper", Model: "test/x"}, config.AgentConfig{Name: "goner", Model: "test/x"})
	require.NoError(t, config.Save("", cfg))
	for _, name := range []string{"keeper", "goner"} {
		_, err := agent.NewSessionManager().Create(name)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(store.AgentDir(name), "MEMORY.md"), []byte("remember"), 0o600))
	}

	counts := callJSON(t, d, "delete_preview", map[string]any{"kind": "agent", "name": "goner"})
	assert.Equal(t, 1.0, counts["sessions"])
	assert.GreaterOrEqual(t, counts["files"], 1.0)

	callText(t, d, "agent_delete", map[string]any{"name": "keeper"})
	assert.FileExists(t, filepath.Join(store.AgentDir("keeper"), "MEMORY.md"), "unchecked only deregisters the agent")
	out := callText(t, d, "agent_delete", map[string]any{"name": "goner", "delete_data": true})
	assert.Contains(t, out, "with its data")
	assert.NoDirExists(t, store.AgentDir("goner"))

	cfg, err = config.Load("")
	require.NoError(t, err)
	for _, a := range cfg.Agents {
		assert.NotContains(t, []string{"keeper", "goner"}, a.Name)
	}
}

func TestDeletePreviewValidation(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	toolCallContains(t, d, "delete_preview", map[string]any{"kind": "project", "name": "x"}, "kind must be")
	toolCallContains(t, d, "delete_preview", map[string]any{"kind": "agent", "name": "ghost"}, "not found")
	toolCallContains(t, d, "delete_preview", map[string]any{"kind": "channel", "agent": "bot", "name": "Nope"}, "no channel named")
}
