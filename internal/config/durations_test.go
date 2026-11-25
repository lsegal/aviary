package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestServerConfig_EffectiveFailedTaskTimeout(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"", DefaultFailedTaskTimeout},
		{"90m", 90 * time.Minute},
		{"soon", DefaultFailedTaskTimeout},
		{"0s", DefaultFailedTaskTimeout},
		{"-1h", DefaultFailedTaskTimeout},
	} {
		got := ServerConfig{FailedTaskTimeout: tc.value}.EffectiveFailedTaskTimeout()
		assert.Equal(t, tc.want, got, "failed_task_timeout %q", tc.value)
	}
}

func TestServerConfig_EffectiveRestartCallDurations(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"", 0},
		{"45s", 45 * time.Second},
		{"later", 0},
		{"0s", 0},
		{"-5s", 0},
	} {
		s := ServerConfig{RestartCallGrace: tc.value, RestartCallMaxWait: tc.value}
		assert.Equal(t, tc.want, s.EffectiveRestartCallGrace(), "restart_call_grace %q", tc.value)
		assert.Equal(t, tc.want, s.EffectiveRestartCallMaxWait(), "restart_call_max_wait %q", tc.value)
	}
}

func TestTaskConfig_EffectiveIdleTimeout(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"", DefaultTaskIdleTimeout},
		{"  ", DefaultTaskIdleTimeout},
		{"0", 0},
		{"0s", 0},
		{" 2m ", 2 * time.Minute},
		{"forever", DefaultTaskIdleTimeout},
		{"-1m", DefaultTaskIdleTimeout},
	} {
		got := TaskConfig{IdleTimeout: tc.value}.EffectiveIdleTimeout()
		assert.Equal(t, tc.want, got, "idle_timeout %q", tc.value)
	}
}

func TestChannelConfig_EffectiveGroupChatHistory(t *testing.T) {
	assert.Equal(t, DefaultGroupChatHistory, ChannelConfig{}.EffectiveGroupChatHistory(), "unset uses the default")
	assert.Equal(t, 0, ChannelConfig{GroupChatHistory: -1}.EffectiveGroupChatHistory(), "-1 disables history")
	assert.Equal(t, 12, ChannelConfig{GroupChatHistory: 12}.EffectiveGroupChatHistory())
}
