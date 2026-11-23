package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/domain"
)

func TestChannelActivityNamesSendersFromMembers(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	addTeamChannel(t)
	ctx := context.Background()

	dm, err := agent.NewSessionManager().GetOrCreateNamed("bot", "slack:D1")
	require.NoError(t, err)
	group, err := agent.NewSessionManager().GetOrCreateNamed("bot", "slack:C1")
	require.NoError(t, err)
	// Alice's first message only recorded her raw ID; a later one, on another
	// conversation, carries her name.
	require.NoError(t, agent.AppendMediaMessageToSessionWithSender("bot", dm.ID, domain.MessageRoleUser, "see attached", []string{"data:image/png;base64,AAAA", "/tmp/notes.pdf"}, domain.NewMessageSender("+15550001", "+15550001", true)))
	require.NoError(t, agent.AppendWorkflowMessageToSession("bot", group.ID, "the app crashed", nil, domain.NewMessageSender("+15550001", "Alice", true), []domain.MessageWorkflow{{TaskID: "bot/triage", Name: "triage"}}, time.Now()))
	require.NoError(t, agent.AppendMessageToSessionWithSender("bot", group.ID, domain.MessageRoleUser, "who am i", domain.NewMessageSender("U9", "U9", true)))
	require.NoError(t, agent.AppendMessageToSession("bot", group.ID, domain.MessageRoleAssistant, "on it"))

	raw, err := d.CallTool(ctx, "channel_activity", map[string]any{"agent": "bot", "channel": "Team"})
	require.NoError(t, err)
	var got channelActivity
	require.NoError(t, json.Unmarshal([]byte(raw), &got))

	assert.Equal(t, 3, got.Messages24h, "the agent's own messages are not counted")
	assert.NotEmpty(t, got.LastActivity)
	require.Len(t, got.Messages, 3)
	assert.Equal(t, "Alice", got.Messages[0].Sender, "named from her later message, never her number")
	assert.Equal(t, "D1", got.Messages[0].Entry)
	assert.Equal(t, []string{"image", "file"}, got.Messages[0].Attachments)
	assert.Equal(t, "Alice", got.Messages[1].Sender)
	assert.Equal(t, []string{"triage"}, got.Messages[1].Workflows)
	assert.Empty(t, got.Messages[2].Sender, "a sender known only by ID stays unnamed")

	raw, err = d.CallTool(ctx, "channel_activity", map[string]any{"agent": "bot", "channel": "Team", "limit": 1})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(raw), &got))
	require.Len(t, got.Messages, 1)
	assert.Equal(t, "who am i", got.Messages[0].Text)
	assert.Equal(t, 3, got.Messages24h)

	raw, err = d.CallTool(ctx, "workflow_preview", map[string]any{"agent": "bot", "channel": "Team", "entry": "D1"})
	require.NoError(t, err)
	var preview []workflowPreviewMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &preview))
	require.Len(t, preview, 1)
	assert.Equal(t, "Alice", preview[0].Sender, "the preview names senders the same way")
}

func TestChannelActivityEmpty(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	addTeamChannel(t)
	raw, err := d.CallTool(context.Background(), "channel_activity", map[string]any{"agent": "bot", "channel": "Team"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"messages_24h":0,"messages":[]}`, raw)
}

func TestMediaKind(t *testing.T) {
	for u, want := range map[string]string{
		"data:image/png;base64,AA":           "image",
		"data:audio/ogg;base64,AA":           "audio",
		"data:application/pdf;name=a;base64": "file",
		"/media/clip.MP4":                    "video",
		"https://x.test/a.jpg?size=2":        "image",
		"/tmp/notes":                         "file",
	} {
		assert.Equal(t, want, mediaKind(u), u)
	}
}
