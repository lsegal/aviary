package sessiontarget

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/channels"
	"github.com/lsegal/aviary/internal/store"
)

// fakeSender records which Manager route each delivery took.
type fakeSender struct {
	calls []string
	err   error
}

func (f *fakeSender) SendThreadOnConfiguredChannel(agentName, channelType, configuredID, channelID, threadTS, text string) error {
	f.calls = append(f.calls, strings.Join([]string{"thread", agentName, channelType, configuredID, channelID, threadTS, text}, "|"))
	return f.err
}

func (f *fakeSender) SendOnConfiguredChannel(agentName, channelType, configuredID, channelID, text string) error {
	f.calls = append(f.calls, strings.Join([]string{"configured", agentName, channelType, configuredID, channelID, text}, "|"))
	return f.err
}

func (f *fakeSender) RouteDelivery(channelType, channelID, text string) error {
	f.calls = append(f.calls, strings.Join([]string{"route", channelType, channelID, text}, "|"))
	return f.err
}

func (f *fakeSender) SendMediaOnConfiguredChannel(agentName, channelType, configuredID, channelID, caption, filePath string) error {
	f.calls = append(f.calls, strings.Join([]string{"configured-media", agentName, channelType, configuredID, channelID, caption, filePath}, "|"))
	return f.err
}

func (f *fakeSender) RouteMediaDelivery(channelType, channelID, caption, filePath string) error {
	f.calls = append(f.calls, strings.Join([]string{"route-media", channelType, channelID, caption, filePath}, "|"))
	return f.err
}

// captureLogs sends slog output to a buffer for the rest of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// newSession creates a session for bot and drops its delivery on cleanup.
func newSession(t *testing.T) string {
	t.Helper()
	sess, err := agent.NewSessionManager().Create("bot")
	require.NoError(t, err)
	t.Cleanup(func() { agent.ClearSessionDelivery("bot", sess.ID) })
	return sess.ID
}

// reply sends text through the session's registered text delivery.
func reply(t *testing.T, sessionID, text string) {
	t.Helper()
	require.NoError(t, agent.AppendReplyToSession("bot", sessionID, text))
}

func TestRegisterTextDeliveryRoutes(t *testing.T) {
	tests := []struct {
		name   string
		target store.SessionChannel
		want   string
	}{
		{
			name:   "slack thread on configured channel",
			target: store.SessionChannel{Type: "slack", ConfiguredID: "ws", ID: "C1", ThreadTS: "171.5"},
			want:   "thread|Bot|slack|ws|C1|171.5|hello",
		},
		{
			name:   "configured channel",
			target: store.SessionChannel{Type: "signal", ConfiguredID: "+1", ID: "+5"},
			want:   "configured|Bot|signal|+1|+5|hello",
		},
		{
			name:   "thread outside slack uses configured channel",
			target: store.SessionChannel{Type: "discord", ConfiguredID: "srv", ID: "D1", ThreadTS: "9"},
			want:   "configured|Bot|discord|srv|D1|hello",
		},
		{
			name:   "slack thread without configured channel routes",
			target: store.SessionChannel{Type: "slack", ID: "C1", ThreadTS: "171.5"},
			want:   "route|slack|C1|hello",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupStore(t)
			logs := captureLogs(t)
			id := newSession(t)
			fake := &fakeSender{}

			register("bot", "Bot", id, tt.target, fake)
			reply(t, id, "hello")

			assert.Equal(t, []string{tt.want}, fake.calls)
			assert.NotContains(t, logs.String(), "failed to deliver")
		})
	}
}

func TestRegisterTextDeliveryLogsFailure(t *testing.T) {
	setupStore(t)
	logs := captureLogs(t)
	id := newSession(t)
	fake := &fakeSender{err: errors.New("send boom")}

	register("bot", "Bot", id, store.SessionChannel{Type: "signal", ConfiguredID: "+1", ID: "+5"}, fake)
	reply(t, id, "hello")

	assert.Len(t, fake.calls, 1)
	assert.Contains(t, logs.String(), "session target: failed to deliver text")
	assert.Contains(t, logs.String(), "send boom")
}

func TestRegisterMediaDeliveryRoutes(t *testing.T) {
	tests := []struct {
		name   string
		target store.SessionChannel
		route  string
	}{
		{name: "configured channel", target: store.SessionChannel{Type: "signal", ConfiguredID: "+1", ID: "+5"}, route: "configured-media|Bot|signal|+1|+5|cap|"},
		{name: "route fallback", target: store.SessionChannel{Type: "signal", ID: "+5"}, route: "route-media|signal|+5|cap|"},
	}
	for _, tt := range tests {
		t.Run(tt.name+" staged", func(t *testing.T) {
			setupStore(t)
			logs := captureLogs(t)
			id := newSession(t)
			src := filepath.Join(t.TempDir(), "photo.png")
			require.NoError(t, os.WriteFile(src, []byte("png"), 0o600))
			fake := &fakeSender{}

			register("bot", "Bot", id, tt.target, fake)
			agent.DeliverMediaToSession("bot", id, "cap", src)

			require.Len(t, fake.calls, 1)
			require.True(t, strings.HasPrefix(fake.calls[0], tt.route), fake.calls[0])
			staged := strings.TrimPrefix(fake.calls[0], tt.route)
			assert.NotEqual(t, src, staged, "delivers the staged copy")
			assert.Equal(t, store.OutgoingMediaDir("signal"), filepath.Dir(staged))
			data, err := os.ReadFile(staged)
			require.NoError(t, err)
			assert.Equal(t, "png", string(data))
			assert.NotContains(t, logs.String(), "session target:")
		})
		t.Run(tt.name+" unstaged", func(t *testing.T) {
			setupStore(t)
			logs := captureLogs(t)
			id := newSession(t)
			missing := filepath.Join(t.TempDir(), "missing.png")
			fake := &fakeSender{}

			register("bot", "Bot", id, tt.target, fake)
			agent.DeliverMediaToSession("bot", id, "cap", missing)

			assert.Equal(t, []string{tt.route + missing}, fake.calls, "falls back to the original path")
			assert.Contains(t, logs.String(), "session target: failed to stage outgoing media")
			assert.NotContains(t, logs.String(), "failed to deliver media")
		})
	}
}

func TestRegisterMediaDeliveryLogsFailure(t *testing.T) {
	setupStore(t)
	logs := captureLogs(t)
	id := newSession(t)
	fake := &fakeSender{err: errors.New("media boom")}

	register("bot", "Bot", id, store.SessionChannel{Type: "signal", ID: "+5"}, fake)
	agent.DeliverMediaToSession("bot", id, "cap", filepath.Join(t.TempDir(), "missing.png"))

	assert.Len(t, fake.calls, 1)
	assert.Contains(t, logs.String(), "session target: failed to deliver media")
	assert.Contains(t, logs.String(), "media boom")
}

func TestRegisterGuard(t *testing.T) {
	setupStore(t)
	valid := store.SessionChannel{Type: "signal", ID: "+5"}
	tests := []struct {
		name      string
		sessionID string
		target    store.SessionChannel
		mgr       *channels.Manager
	}{
		{name: "nil manager", sessionID: "s1", target: valid},
		{name: "empty session", target: valid, mgr: channels.NewManager()},
		{name: "empty type", sessionID: "s1", target: store.SessionChannel{ID: "+5"}, mgr: channels.NewManager()},
		{name: "empty id", sessionID: "s1", target: store.SessionChannel{Type: "signal"}, mgr: channels.NewManager()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(func() { agent.ClearSessionDelivery("bot", tt.sessionID) })
			Register("bot", "Bot", tt.sessionID, tt.target, tt.mgr)
			assert.False(t, agent.HasSessionMediaDelivery("bot", tt.sessionID))
		})
	}
}

func TestRegisterWithManagerReportsInactiveChannel(t *testing.T) {
	setupStore(t)
	logs := captureLogs(t)
	id := newSession(t)

	Register("bot", "Bot", id, store.SessionChannel{Type: "signal", ID: "+5"}, channels.NewManager())
	require.True(t, agent.HasSessionMediaDelivery("bot", id))
	reply(t, id, "hello")

	assert.Contains(t, logs.String(), "failed to deliver text")
	assert.Contains(t, logs.String(), `no active channel of type \"signal\"`)
}

func TestSetPersistsAndRegisters(t *testing.T) {
	setupStore(t)
	id := newSession(t)
	target := store.SessionChannel{Type: "slack", ConfiguredID: "ws", ID: "C1", ThreadTS: "171.5"}

	require.NoError(t, Set("bot", "Bot", id, target, channels.NewManager()))

	sidecar, err := store.ReadSessionChannels("bot", id)
	require.NoError(t, err)
	assert.Equal(t, []store.SessionChannel{target}, sidecar.Channels)
	assert.True(t, agent.HasSessionMediaDelivery("bot", id))
}

func TestSetWithoutManagerOnlyPersists(t *testing.T) {
	setupStore(t)
	id := newSession(t)
	target := store.SessionChannel{Type: "signal", ConfiguredID: "+1", ID: "+5"}

	require.NoError(t, Set("bot", "Bot", id, target, nil))

	sidecar, err := store.ReadSessionChannels("bot", id)
	require.NoError(t, err)
	assert.Equal(t, []store.SessionChannel{target}, sidecar.Channels)
	assert.False(t, agent.HasSessionMediaDelivery("bot", id))
}

func TestSetReturnsStoreError(t *testing.T) {
	setupStore(t)
	id := newSession(t)
	// A directory where the sidecar file goes makes the write fail.
	require.NoError(t, os.MkdirAll(store.SessionChannelsPath("bot", id), 0o700))

	err := Set("bot", "Bot", id, store.SessionChannel{Type: "signal", ID: "+5"}, channels.NewManager())

	require.Error(t, err)
	assert.False(t, agent.HasSessionMediaDelivery("bot", id), "a failed write registers nothing")
}
