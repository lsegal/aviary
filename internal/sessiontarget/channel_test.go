package sessiontarget

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

func setupStore(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AVIARY_CONFIG_BASE_DIR", "")
	require.NoError(t, store.EnsureDirs())
}

// conversation creates a session the way an inbound channel message does.
func conversation(t *testing.T, agentName, name string, target store.SessionChannel) *domain.Session {
	t.Helper()
	sess, err := agent.NewSessionManager().GetOrCreateNamed(agentName, name)
	require.NoError(t, err)
	require.NoError(t, store.EnsureSessionChannelTarget(agentName, sess.ID, target))
	require.NoError(t, agent.AppendMessageToSessionWithSender(agentName, sess.ID, domain.MessageRoleUser, "hi", nil))
	return sess
}

// sessionText is the raw JSONL of bot's session id.
func sessionText(t *testing.T, id string) string {
	t.Helper()
	data, err := os.ReadFile(store.FindSessionPath("bot", id))
	require.NoError(t, err)
	return string(data)
}

func names(sessions []*domain.Session) []string {
	out := make([]string, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s.Name)
	}
	return out
}

func TestEnsureChannelCreatesOwnSessionWithTarget(t *testing.T) {
	setupStore(t)
	cc := config.ChannelConfig{Type: "slack", ID: "ws", Name: "Team", DeliverTo: "C1"}

	sess, err := EnsureChannel("bot", cc, nil)
	require.NoError(t, err)
	assert.Equal(t, "channel:slack:ws", sess.Name)
	assert.Equal(t, domain.SessionTypeChannel, sess.Type)
	sidecar, err := store.ReadSessionChannels("bot", sess.ID)
	require.NoError(t, err)
	assert.Equal(t, []store.SessionChannel{{Type: "slack", ConfiguredID: "ws", ID: "C1"}}, sidecar.Channels)

	// Again, with a new conversation: the same session points at it.
	cc.DeliverTo = "C2"
	again, err := EnsureChannel("bot", cc, nil)
	require.NoError(t, err)
	assert.Equal(t, sess.ID, again.ID)
	sidecar, err = store.ReadSessionChannels("bot", sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "C2", sidecar.Channels[0].ID)
}

func TestChannelSessionsAndOwner(t *testing.T) {
	setupStore(t)
	a := config.AgentConfig{Name: "bot", Channels: []config.ChannelConfig{
		{Type: "slack", ID: "ws", Name: "Team"},
		{Type: "signal", ID: "+1"},
	}}
	_, err := EnsureChannel("bot", a.Channels[0], nil)
	require.NoError(t, err)
	conversation(t, "bot", "slack:C9", store.SessionChannel{Type: "slack", ConfiguredID: "ws", ID: "C9"})
	conversation(t, "bot", "signal:+5", store.SessionChannel{Type: "signal", ConfiguredID: "+1", ID: "+5"})
	user, err := agent.NewSessionManager().Create("bot")
	require.NoError(t, err)

	team, err := ChannelSessions(a, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"channel:slack:ws", "slack:C9"}, names(team), "own session first")
	signal, err := ChannelSessions(a, 1)
	require.NoError(t, err)
	assert.Equal(t, []string{"signal:+5"}, names(signal))
	assert.Equal(t, -1, ChannelOwner(a, user))
}

func TestReleaseChannelArchive(t *testing.T) {
	setupStore(t)
	a := config.AgentConfig{Name: "bot", Channels: []config.ChannelConfig{{Type: "slack", ID: "ws", Name: "Team"}}}
	own, err := EnsureChannel("bot", a.Channels[0], nil)
	require.NoError(t, err)
	require.NoError(t, agent.AppendMessageToSessionWithSender("bot", own.ID, domain.MessageRoleUser, "keep me", nil))
	conversation(t, "bot", "slack:C9", store.SessionChannel{Type: "slack", ConfiguredID: "ws", ID: "C9"})

	require.NoError(t, ReleaseChannel(a, 0, false))

	assert.Empty(t, store.FindSessionPath("bot", own.ID), "the channel session moved")
	all, err := agent.NewSessionManager().List("bot")
	require.NoError(t, err)
	titles := map[string]domain.SessionType{}
	for _, s := range all {
		titles[s.Title] = s.Type
		sidecar, err := store.ReadSessionChannels("bot", s.ID)
		require.NoError(t, err)
		assert.Empty(t, sidecar.Channels, "archived sessions lose channel delivery")
	}
	assert.Equal(t, domain.SessionTypeUser, titles["Team (archived)"])
	assert.Equal(t, domain.SessionTypeUser, titles["Team: slack:C9 (archived)"])
	for _, s := range all {
		if s.Title == "Team (archived)" {
			assert.Contains(t, sessionText(t, s.ID), "keep me")
		}
	}
}

func TestReleaseChannelDelete(t *testing.T) {
	setupStore(t)
	a := config.AgentConfig{Name: "bot", Channels: []config.ChannelConfig{{Type: "slack", ID: "ws"}}}
	own, err := EnsureChannel("bot", a.Channels[0], nil)
	require.NoError(t, err)
	conv := conversation(t, "bot", "slack:C9", store.SessionChannel{Type: "slack", ConfiguredID: "ws", ID: "C9"})

	require.NoError(t, ReleaseChannel(a, 0, true))
	assert.Empty(t, store.FindSessionPath("bot", own.ID))
	assert.Empty(t, store.FindSessionPath("bot", conv.ID))
}

func TestRekeyChannelsFollowsIDChange(t *testing.T) {
	setupStore(t)
	prev := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Channels: []config.ChannelConfig{{Type: "signal", ID: "phone", Name: "Phone"}}}}}
	next := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Channels: []config.ChannelConfig{{Type: "signal", ID: "+1555", Name: "Phone"}}}}}
	own, err := EnsureChannel("bot", prev.Agents[0].Channels[0], nil)
	require.NoError(t, err)
	require.NoError(t, agent.AppendMessageToSessionWithSender("bot", own.ID, domain.MessageRoleUser, "history", nil))
	conv := conversation(t, "bot", "signal:+9", store.SessionChannel{Type: "signal", ConfiguredID: "phone", ID: "+9"})

	require.NoError(t, RekeyChannels(prev, next))

	sessions, err := ChannelSessions(next.Agents[0], 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"channel:signal:+1555", "signal:+9"}, names(sessions))
	assert.Contains(t, sessionText(t, sessions[0].ID), "history")
	sidecar, err := store.ReadSessionChannels("bot", conv.ID)
	require.NoError(t, err)
	assert.Equal(t, "+1555", sidecar.Channels[0].ConfiguredID)
}
