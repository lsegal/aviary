package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
)

func callList(t *testing.T, d *Dispatcher, tool string, args map[string]any) []map[string]any {
	t.Helper()
	out, err := d.CallTool(context.Background(), tool, args)
	require.NoError(t, err)
	var result []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &result), out)
	return result
}

func listedNames(rows []map[string]any) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		name, _ := r["name"].(string)
		out = append(out, name)
	}
	return out
}

func TestChannelToolsLifecycle(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)

	created := callJSON(t, d, "channel_create", map[string]any{"agent": "bot", "name": "Team Slack", "type": "slack"})
	sessionID, _ := created["session_id"].(string)
	require.NotEmpty(t, sessionID)
	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Len(t, cfg.Agents[0].Channels, 1)
	ch := cfg.Agents[0].Channels[0]
	assert.Equal(t, "Team Slack", ch.Name)
	assert.Equal(t, "team-slack", ch.ID)
	assert.False(t, config.BoolOr(ch.Enabled, true), "a new channel starts disabled")
	toolCallContains(t, d, "channel_create", map[string]any{"agent": "bot", "name": "team slack", "type": "slack"}, "already has a channel")
	toolCallContains(t, d, "channel_create", map[string]any{"agent": "bot", "name": "X", "type": "irc"}, "type must be")

	// Its session lives under the channel, not Chats.
	own := callList(t, d, "channel_sessions", map[string]any{"agent": "bot", "channel": "Team Slack"})
	require.Len(t, own, 1)
	assert.Equal(t, sessionID, own[0]["id"])
	assert.Equal(t, true, own[0]["own"])
	assert.NotContains(t, listedNames(callList(t, d, "session_list", map[string]any{"agent": "bot"})), "channel:slack:team-slack")

	// A task delivering to the channel follows a rename; the session stays.
	cfg.Agents[0].Tasks = []config.TaskConfig{{Name: "report", Schedule: "0 * * * *", Prompt: "x", Target: "channel:Team Slack"}}
	require.NoError(t, config.Save("", cfg))
	callText(t, d, "channel_rename", map[string]any{"agent": "bot", "channel": "Team Slack", "new_name": "Ops"})
	cfg, err = config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "Ops", cfg.Agents[0].Channels[0].Name)
	assert.Equal(t, "channel:Ops", cfg.Agents[0].Tasks[0].Target)
	renamed := callList(t, d, "channel_sessions", map[string]any{"agent": "bot", "channel": "Ops"})
	require.Len(t, renamed, 1)
	assert.Equal(t, sessionID, renamed[0]["id"])

	// Removing it with archive keeps the session as a regular chat.
	toolCallContains(t, d, "channel_remove", map[string]any{"agent": "bot", "channel": "Ops", "sessions": "keep"}, "sessions must be archive or delete")
	callText(t, d, "channel_remove", map[string]any{"agent": "bot", "channel": "Ops", "sessions": "archive"})
	cfg, err = config.Load("")
	require.NoError(t, err)
	assert.Empty(t, cfg.Agents[0].Channels)
	assert.Empty(t, cfg.Agents[0].Tasks[0].Target, "its delivery target is cleared")
	var titles []string
	for _, s := range callList(t, d, "session_list", map[string]any{"agent": "bot"}) {
		title, _ := s["title"].(string)
		titles = append(titles, title)
	}
	assert.Contains(t, titles, "Ops (archived)")
}

func callText(t *testing.T, d *Dispatcher, tool string, args map[string]any) string {
	t.Helper()
	out, err := d.CallTool(context.Background(), tool, args)
	require.NoError(t, err)
	return out
}
