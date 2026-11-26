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
)

// addTeamChannel gives the test agent "bot" the Slack channel "Team".
func addTeamChannel(t *testing.T) {
	t.Helper()
	cfg, err := config.Load("")
	require.NoError(t, err)
	cfg.Agents[0].Channels = []config.ChannelConfig{{Type: "slack", ID: "team", Name: "Team"}}
	require.NoError(t, config.Save("", cfg))
	require.NoError(t, reloadTaskConfig())
}

func TestWorkflowLifecycleThroughTaskTools(t *testing.T) {
	d, s := setupDispatcherWithScheduler(t)
	addTeamChannel(t)
	ctx := context.Background()

	out, err := d.CallTool(ctx, "task_schedule", map[string]any{
		"agent": "bot", "name": "triage", "content": "Triage the crash.",
		"trigger": "message", "channel": "team", "entry": "C1", "match": "*crash*", "target": "reply",
	})
	require.NoError(t, err)
	assert.Contains(t, out, `Workflow "triage" created`)

	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Len(t, cfg.Agents[0].Tasks, 1)
	task := cfg.Agents[0].Tasks[0]
	assert.True(t, task.IsWorkflow())
	assert.Equal(t, "Team", task.Channel, "stored by the channel's name")
	assert.Equal(t, "C1", task.Entry)
	assert.Equal(t, "*crash*", task.Match)
	assert.Equal(t, "reply", task.Target)

	var listed []domain.ScheduledTask
	raw, err := d.CallTool(ctx, "task_list", map[string]any{})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(raw), &listed))
	require.Len(t, listed, 1)
	assert.Equal(t, domain.TriggerTypeMessage, listed[0].TriggerType)

	cc := cfg.Agents[0].Channels[0]
	assert.Len(t, s.MatchWorkflows("bot", cc, "C1", "crash"), 1)

	// Pause, then resume.
	_, err = d.CallTool(ctx, "task_update", map[string]any{"name": "bot/triage", "enabled": false})
	require.NoError(t, err)
	assert.Empty(t, s.MatchWorkflows("bot", cc, "C1", "crash"))
	_, err = d.CallTool(ctx, "task_update", map[string]any{"name": "bot/triage", "enabled": true, "match": ""})
	require.NoError(t, err)
	assert.Len(t, s.MatchWorkflows("bot", cc, "C1", "anything"), 1, "an empty match takes every message")

	// Run now goes through the same queue.
	_, err = d.CallTool(ctx, "task_run", map[string]any{"name": "bot/triage"})
	require.NoError(t, err)

	_, err = d.CallTool(ctx, "task_delete", map[string]any{"name": "bot/triage"})
	require.NoError(t, err)
	assert.Empty(t, s.MatchWorkflows("bot", cc, "C1", "crash"))
}

func TestWorkflowToolValidation(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	addTeamChannel(t)
	ctx := context.Background()

	assert.Contains(t, toolErrorText(d.CallTool(ctx, "task_schedule", map[string]any{
		"agent": "bot", "content": "x", "trigger": "message", "channel": "nope",
	})), `no channel named "nope"`)
	assert.Contains(t, toolErrorText(d.CallTool(ctx, "task_schedule", map[string]any{
		"agent": "bot", "content": "x", "trigger": "message", "channel": "Team", "schedule": "* * * * *",
	})), "take no schedule")
	assert.Contains(t, toolErrorText(d.CallTool(ctx, "task_schedule", map[string]any{
		"agent": "bot", "content": "x", "every": "1h", "match": "*",
	})), "only supported for workflows")
	assert.Contains(t, toolErrorText(d.CallTool(ctx, "task_schedule", map[string]any{
		"agent": "bot", "content": "x", "every": "1h", "target": "reply", "precompile": false,
	})), "only workflows can reply")
}

func TestWorkflowPreviewAndEntries(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	addTeamChannel(t)
	ctx := context.Background()

	sess, err := agent.NewSessionManager().GetOrCreateNamed("bot", "slack:C1")
	require.NoError(t, err)
	require.NoError(t, agent.AppendMessageToSessionWithSender("bot", sess.ID, domain.MessageRoleUser, "the app crashed", domain.NewMessageSender("U1", "Alice", true)))
	require.NoError(t, agent.AppendMessageToSessionWithSender("bot", sess.ID, domain.MessageRoleUser, "all good", domain.NewMessageSender("U2", "U2", true)))
	require.NoError(t, agent.AppendMessageToSession("bot", sess.ID, domain.MessageRoleAssistant, "crash noted"))

	raw, err := d.CallTool(ctx, "workflow_entries", map[string]any{"agent": "bot", "channel": "Team"})
	require.NoError(t, err)
	var entries []workflowEntry
	require.NoError(t, json.Unmarshal([]byte(raw), &entries))
	require.Len(t, entries, 1)
	assert.Equal(t, "C1", entries[0].Entry)

	raw, err = d.CallTool(ctx, "workflow_preview", map[string]any{"agent": "bot", "channel": "Team", "entry": "C1", "match": "*crash*"})
	require.NoError(t, err)
	var msgs []workflowPreviewMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &msgs))
	require.Len(t, msgs, 2, "the agent's own messages are left out")
	assert.True(t, msgs[0].Matches)
	assert.Equal(t, "Alice", msgs[0].Sender)
	assert.False(t, msgs[1].Matches)
	assert.Empty(t, msgs[1].Sender, "raw IDs are not shown as names")
}

func TestChannelRemoveDeletesItsWorkflows(t *testing.T) {
	d, s := setupDispatcherWithScheduler(t)
	addTeamChannel(t)
	ctx := context.Background()
	_, err := d.CallTool(ctx, "task_schedule", map[string]any{
		"agent": "bot", "name": "triage", "content": "x", "trigger": "message", "channel": "Team",
	})
	require.NoError(t, err)

	_, err = d.CallTool(ctx, "channel_rename", map[string]any{"agent": "bot", "channel": "Team", "new_name": "Ops"})
	require.NoError(t, err)
	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Len(t, cfg.Agents[0].Tasks, 1)
	assert.Equal(t, "Ops", cfg.Agents[0].Tasks[0].Channel, "the workflow follows the rename")

	_, err = d.CallTool(ctx, "channel_remove", map[string]any{"agent": "bot", "channel": "Ops", "delete_data": true})
	require.NoError(t, err)
	cfg, err = config.Load("")
	require.NoError(t, err)
	assert.Empty(t, cfg.Agents[0].Tasks)
	assert.Empty(t, s.ListTasks())
}
