package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/notify"
)

func TestNotifyToolsSendThroughTheServiceAndEditSettings(t *testing.T) {
	d, _ := setupScriptedAgent(t)
	var published []notify.Notification
	n := notify.New()
	n.Publish = func(note notify.Notification) { published = append(published, note) }
	n.AfterFunc = func(time.Duration, func()) {
		// Never fire delayed callbacks: the test drives notifications directly.
	}
	GetDeps().Notifier = n
	ctx := context.Background()

	out, err := d.CallTool(ctx, "notify", map[string]any{"title": "Build is green", "severity": "success", "link": "/agents/bot/chat/main"})
	require.NoError(t, err)
	assert.Contains(t, out, `"delivered"`)
	require.Len(t, published, 1)
	assert.Equal(t, notify.TypeCustom, published[0].Type)
	assert.Equal(t, "/agents/bot/chat/main", published[0].Link)

	out, err = d.CallTool(ctx, "notification_list", map[string]any{})
	require.NoError(t, err)
	assert.Contains(t, out, "Build is green")

	out, err = d.CallTool(ctx, "notification_settings_get", map[string]any{})
	require.NoError(t, err)
	assert.Contains(t, out, `"eventTypes"`)

	settings := notify.DefaultSettings()
	settings.Enabled = false
	_, err = d.CallTool(ctx, "notification_settings_set", map[string]any{"settings": settings})
	require.NoError(t, err)
	got, err := notify.ReadSettings()
	require.NoError(t, err)
	assert.False(t, got.Enabled)

	out, err = d.CallTool(ctx, "notify", map[string]any{"title": "muted"})
	require.NoError(t, err)
	assert.Contains(t, out, `"suppressed"`, "the master switch applies to the tool too")
	assert.Len(t, published, 1)

	_, err = d.CallTool(ctx, "notification_settings_set", map[string]any{"reset": true})
	require.NoError(t, err)
	got, err = notify.ReadSettings()
	require.NoError(t, err)
	assert.Equal(t, notify.DefaultSettings(), got)
}

func TestNotifyToolNeedsTheServer(t *testing.T) {
	d, _ := setupScriptedAgent(t)
	_, err := d.CallTool(context.Background(), "notify", map[string]any{"title": "x"})
	out := requireToolErrorText(t, err)
	assert.Contains(t, out, "running Aviary server")
}
