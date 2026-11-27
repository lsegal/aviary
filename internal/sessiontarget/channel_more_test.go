package sessiontarget

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// tick separates timestamps so recency ordering is unambiguous on platforms
// with a coarse wall clock.
func tick() { time.Sleep(20 * time.Millisecond) }

func TestEnsureChannelRejectsIncompleteChannel(t *testing.T) {
	setupStore(t)
	for name, tc := range map[string]struct {
		agent string
		cc    config.ChannelConfig
	}{
		"no agent": {" ", config.ChannelConfig{Type: "slack", ID: "ws"}},
		"no type":  {"bot", config.ChannelConfig{Type: " ", ID: "ws"}},
		"no id":    {"bot", config.ChannelConfig{Type: "slack", ID: ""}},
	} {
		t.Run(name, func(t *testing.T) {
			sess, err := EnsureChannel(tc.agent, tc.cc, nil)
			require.EqualError(t, err, "channel needs an agent, a type and an id")
			assert.Nil(t, sess)
		})
	}
	all, err := agent.NewSessionManager().List("bot")
	require.NoError(t, err)
	assert.Empty(t, all, "no session is created for an incomplete channel")
}

func TestEnsureChannelsNilConfig(t *testing.T) {
	setupStore(t)
	assert.NoError(t, EnsureChannels(nil, nil))
}

func TestEnsureChannelsCreatesEverySession(t *testing.T) {
	setupStore(t)
	cfg := &config.Config{Agents: []config.AgentConfig{
		{Name: "bot", Channels: []config.ChannelConfig{
			{Type: "slack", ID: "ws", DeliverTo: "C1"},
			{Type: "signal", ID: "+1"},
		}},
		{Name: "helper", Channels: []config.ChannelConfig{{Type: "discord", ID: "guild"}}},
		{Name: "quiet"},
	}}

	require.NoError(t, EnsureChannels(cfg, nil))

	bot, err := agent.NewSessionManager().List("bot")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"channel:slack:ws", "channel:signal:+1"}, names(bot))
	for _, s := range bot {
		assert.Equal(t, domain.SessionTypeChannel, s.Type)
	}
	helper, err := agent.NewSessionManager().List("helper")
	require.NoError(t, err)
	assert.Equal(t, []string{"channel:discord:guild"}, names(helper))

	// Idempotent: a second load reuses the same sessions.
	require.NoError(t, EnsureChannels(cfg, nil))
	again, err := agent.NewSessionManager().List("bot")
	require.NoError(t, err)
	assert.Len(t, again, 2)
}

func TestEnsureChannelsTriesAllAndReturnsFirstError(t *testing.T) {
	setupStore(t)
	cfg := &config.Config{Agents: []config.AgentConfig{
		{Name: "bot", Channels: []config.ChannelConfig{
			{Type: "slack", Name: "No ID"},
			{Type: "signal", ID: "+1"},
		}},
		{Name: "helper", Channels: []config.ChannelConfig{
			{ID: "untyped"},
			{Type: "discord", ID: "guild"},
		}},
	}}

	err := EnsureChannels(cfg, nil)
	require.Error(t, err)
	assert.Equal(t, `agent "bot" channel "No ID": channel needs an agent, a type and an id`, err.Error())

	bot, err := agent.NewSessionManager().List("bot")
	require.NoError(t, err)
	assert.Equal(t, []string{"channel:signal:+1"}, names(bot), "channels after the invalid one are still set up")
	helper, err := agent.NewSessionManager().List("helper")
	require.NoError(t, err)
	assert.Equal(t, []string{"channel:discord:guild"}, names(helper), "later agents are still set up")
}

func TestChannelSessionsIndexOutOfRange(t *testing.T) {
	setupStore(t)
	a := config.AgentConfig{Name: "bot", Channels: []config.ChannelConfig{{Type: "slack", ID: "ws"}}}
	for _, idx := range []int{-1, 1} {
		sessions, err := ChannelSessions(a, idx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "out of range")
		assert.Nil(t, sessions)
	}
}

func TestChannelSessionsOrdersOwnThenNewest(t *testing.T) {
	setupStore(t)
	a := config.AgentConfig{Name: "bot", Channels: []config.ChannelConfig{{Type: "slack", ID: "ws"}}}
	target := func(id string) store.SessionChannel {
		return store.SessionChannel{Type: "slack", ConfiguredID: "ws", ID: id}
	}
	oldest := conversation(t, "bot", "slack:A", target("A"))
	tick()
	conversation(t, "bot", "slack:B", target("B"))
	tick()
	// No messages: its creation time counts as its activity.
	quiet, err := agent.NewSessionManager().GetOrCreateNamed("bot", "slack:C")
	require.NoError(t, err)
	require.NoError(t, store.EnsureSessionChannelTarget("bot", quiet.ID, target("C")))
	tick()
	// The own session is created last but still lists first.
	_, err = EnsureChannel("bot", a.Channels[0], nil)
	require.NoError(t, err)
	tick()
	require.NoError(t, agent.AppendMessageToSessionWithSender("bot", oldest.ID, domain.MessageRoleUser, "again", nil))

	sessions, err := ChannelSessions(a, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"channel:slack:ws", "slack:A", "slack:C", "slack:B"}, names(sessions))
}

func TestReleaseChannelIndexOutOfRange(t *testing.T) {
	setupStore(t)
	a := config.AgentConfig{Name: "bot", Channels: []config.ChannelConfig{{Type: "slack", ID: "ws"}}}
	require.Error(t, ReleaseChannel(a, 3, false))
	require.Error(t, ReleaseChannel(a, -1, true))
}

func TestReleaseChannelWithoutSessionsIsNoop(t *testing.T) {
	setupStore(t)
	a := config.AgentConfig{Name: "bot", Channels: []config.ChannelConfig{{Type: "slack", ID: "ws"}}}
	other, err := agent.NewSessionManager().Create("bot")
	require.NoError(t, err)

	require.NoError(t, ReleaseChannel(a, 0, false))
	require.NoError(t, ReleaseChannel(a, 0, true))

	assert.NotEmpty(t, store.FindSessionPath("bot", other.ID), "unrelated sessions stay")
	all, err := agent.NewSessionManager().List("bot")
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

func TestReleaseChannelArchiveUsesConversationTitle(t *testing.T) {
	setupStore(t)
	a := config.AgentConfig{Name: "bot", Channels: []config.ChannelConfig{{Type: "slack", ID: "ws", Name: "Team"}}}
	conv := conversation(t, "bot", "slack:C9", store.SessionChannel{Type: "slack", ConfiguredID: "ws", ID: "C9"})
	require.NoError(t, agent.NewSessionManager().Rename("bot", conv.ID, "Standup"))

	require.NoError(t, ReleaseChannel(a, 0, false))

	titles := sessionTitles(t)
	assert.Equal(t, domain.SessionTypeUser, titles["Team: Standup (archived)"])
	assert.NotContains(t, titles, "Team: slack:C9 (archived)")
}

func TestRekeyChannelsNilConfigs(t *testing.T) {
	setupStore(t)
	cfg := &config.Config{}
	assert.NoError(t, RekeyChannels(nil, cfg))
	assert.NoError(t, RekeyChannels(cfg, nil))
	assert.NoError(t, RekeyChannels(nil, nil))
}

func TestRekeyChannelsLeavesUnmatchedChannels(t *testing.T) {
	setupStore(t)
	agents := func(chs ...config.ChannelConfig) []config.AgentConfig {
		return []config.AgentConfig{{Name: "bot", Channels: chs}}
	}
	sig := config.ChannelConfig{Type: "signal", ID: "phone"}
	_, err := EnsureChannel("bot", sig, nil)
	require.NoError(t, err)
	conv := conversation(t, "bot", "signal:+9", store.SessionChannel{Type: "signal", ConfiguredID: "phone", ID: "+9"})

	prev := &config.Config{Agents: agents(sig)}
	for name, next := range map[string]*config.Config{
		"new agent only":     {Agents: []config.AgentConfig{{Name: "other", Channels: []config.ChannelConfig{{Type: "signal", ID: "+1"}}}}},
		"same id":            {Agents: agents(config.ChannelConfig{Type: "signal", ID: " phone ", Name: "Renamed"})},
		"type changed":       {Agents: agents(config.ChannelConfig{Type: "slack", ID: "ws"})},
		"id cleared":         {Agents: agents(config.ChannelConfig{Type: "signal", ID: " "})},
		"old id still used":  {Agents: agents(config.ChannelConfig{Type: "signal", ID: "+1"}, sig)},
		"channel added only": {Agents: agents(sig, config.ChannelConfig{Type: "slack", ID: "ws"})},
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, RekeyChannels(prev, next))
			sessions, err := ChannelSessions(prev.Agents[0], 0)
			require.NoError(t, err)
			assert.Equal(t, []string{"channel:signal:phone", "signal:+9"}, names(sessions))
			sidecar, err := store.ReadSessionChannels("bot", conv.ID)
			require.NoError(t, err)
			assert.Equal(t, "phone", sidecar.Channels[0].ConfiguredID)
		})
	}
}

func TestRekeyChannelsKeepsExistingTargetSession(t *testing.T) {
	setupStore(t)
	prev := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Channels: []config.ChannelConfig{{Type: "signal", ID: "phone"}}}}}
	next := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Channels: []config.ChannelConfig{{Type: "signal", ID: "+1555"}}}}}
	old, err := EnsureChannel("bot", prev.Agents[0].Channels[0], nil)
	require.NoError(t, err)
	existing, err := EnsureChannel("bot", next.Agents[0].Channels[0], nil)
	require.NoError(t, err)
	conv := conversation(t, "bot", "signal:+9", store.SessionChannel{Type: "signal", ConfiguredID: "phone", ID: "+9"})
	// Owned through its name alone: it has no channel sidecar to update.
	bare, err := agent.NewSessionManager().GetOrCreateNamedTyped("bot", "signal:+7", domain.SessionTypeChannel)
	require.NoError(t, err)

	require.NoError(t, RekeyChannels(prev, next))

	assert.NotEmpty(t, store.FindSessionPath("bot", old.ID), "the old session is not moved onto an existing name")
	bareSidecar, err := store.ReadSessionChannels("bot", bare.ID)
	require.NoError(t, err)
	assert.Empty(t, bareSidecar.Channels, "a session without a sidecar is left alone")
	loaded, err := agent.NewSessionManager().Load("bot", existing.ID)
	require.NoError(t, err)
	assert.Equal(t, "channel:signal:+1555", loaded.Name)
	sidecar, err := store.ReadSessionChannels("bot", conv.ID)
	require.NoError(t, err)
	assert.Equal(t, "+1555", sidecar.Channels[0].ConfiguredID, "conversations still follow the new ID")
}
