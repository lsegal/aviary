package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lsegal/aviary/internal/startup"
)

func TestChannelToolsReportInitializingWhileChannelsConnect(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	trace := startup.Begin()
	t.Cleanup(func() { startup.Begin() })
	trace.Pending("channels")

	toolCallContains(t, d, "channel_sessions", map[string]any{"agent": "bot", "channel": "Ops"},
		"aviary is still initializing channels; retry in 2s")
	// Tools that do not need channels keep working.
	callText(t, d, "agent_list", map[string]any{})

	ready := make(chan struct{})
	trace.Go("channels", "channels connect", func() error { return nil }, func() { close(ready) })
	<-ready
	out, _ := d.CallTool(t.Context(), "channel_sessions", map[string]any{"agent": "bot", "channel": "Ops"})
	assert.NotContains(t, out, "still initializing")
}

func TestToolSubsystem(t *testing.T) {
	assert.Equal(t, "channels", toolSubsystem("channel_create"))
	assert.Equal(t, "channels", toolSubsystem("channel_send_file"))
	assert.Empty(t, toolSubsystem("project_status"), "Code Loop tools serve saved state while discovery runs")
	assert.Empty(t, toolSubsystem("agent_list"))
}
