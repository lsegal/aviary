package store

import (
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"

	"github.com/stretchr/testify/assert"
)

func TestUpdateChannelMetadataStateUsesConfiguredChannelID(t *testing.T) {
	setupStoreDir(t)

	channel := config.ChannelConfig{Type: "slack", ID: "alerts"}
	before := &config.Config{}
	enabled := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Channels: []config.ChannelConfig{channel}}}}
	enabledAt := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)

	assert.NoError(t, UpdateChannelMetadataState(before, enabled, enabledAt))
	state, err := ReadAppState()
	assert.NoError(t, err)
	meta, ok := state.Channels["bot/slack/alerts"]
	assert.True(t, ok)
	assert.Equal(t, enabledAt, meta.EnabledAt)
	assert.NotContains(t, state.Channels, "bot/slack/0")

	disabledValue := false
	disabled := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Channels: []config.ChannelConfig{{
		Type:    "slack",
		ID:      "alerts",
		Enabled: &disabledValue,
	}}}}}
	disabledAt := enabledAt.Add(time.Hour)
	assert.NoError(t, UpdateChannelMetadataState(enabled, disabled, disabledAt))
	state, err = ReadAppState()
	assert.NoError(t, err)
	assert.Equal(t, disabledAt, state.Channels["bot/slack/alerts"].DisabledAt)
}

func TestUpdateChannelMetadataStateTracksRemovalAndReaddition(t *testing.T) {
	setupStoreDir(t)

	configured := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Channels: []config.ChannelConfig{{
		Type: "signal",
		ID:   "primary",
	}}}}}
	initial := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	assert.NoError(t, UpdateChannelMetadataState(&config.Config{}, configured, initial))

	removedAt := initial.Add(time.Hour)
	assert.NoError(t, UpdateChannelMetadataState(configured, &config.Config{}, removedAt))
	state, err := ReadAppState()
	assert.NoError(t, err)
	assert.Equal(t, removedAt, state.Channels["bot/signal/primary"].DisabledAt)

	readdedAt := removedAt.Add(time.Hour)
	assert.NoError(t, UpdateChannelMetadataState(&config.Config{}, configured, readdedAt))
	state, err = ReadAppState()
	assert.NoError(t, err)
	meta := state.Channels["bot/signal/primary"]
	assert.Equal(t, readdedAt, meta.EnabledAt)
	assert.Equal(t, removedAt, meta.DisabledAt)
	assert.True(t, meta.EnabledAt.After(meta.DisabledAt))
}

func TestUpdateChannelMetadataStateMatchesReorderedChannelsByID(t *testing.T) {
	setupStoreDir(t)

	disabledValue := false
	prev := &config.Config{Agents: []config.AgentConfig{
		{Name: "bot", Channels: []config.ChannelConfig{
			{Type: "slack", ID: "alerts"},
			{Type: "slack", ID: "support", Enabled: &disabledValue},
		}},
	}}
	originalEnabledAt := time.Date(2026, time.July, 13, 10, 0, 0, 0, time.UTC)
	originalDisabledAt := originalEnabledAt.Add(time.Hour)
	assert.NoError(t, WriteAppState(&AppState{Channels: map[string]ChannelMetadata{
		"bot/slack/alerts":  {EnabledAt: originalEnabledAt},
		"bot/slack/support": {DisabledAt: originalDisabledAt},
	}}))

	next := &config.Config{Agents: []config.AgentConfig{
		{Name: "bot", Channels: []config.ChannelConfig{
			{Type: "slack", ID: "support"},
			{Type: "slack", ID: "alerts"},
		}},
	}}
	now := originalDisabledAt.Add(time.Hour)
	assert.NoError(t, UpdateChannelMetadataState(prev, next, now))

	state, err := ReadAppState()
	assert.NoError(t, err)
	assert.Equal(t, originalEnabledAt, state.Channels["bot/slack/alerts"].EnabledAt)
	assert.Equal(t, now, state.Channels["bot/slack/support"].EnabledAt)
}
