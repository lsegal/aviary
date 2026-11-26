package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// mockChannel is a test implementation of Channel.
type mockChannel struct {
	mu        sync.Mutex
	started   bool
	stopped   bool
	msgFn     func(IncomingMessage)
	sendCalls []sendCall
	startErr  error
}

type sendCall struct {
	channel string
	text    string
}

func (m *mockChannel) Start(ctx context.Context) error {
	m.mu.Lock()
	m.started = true
	m.mu.Unlock()
	if m.startErr != nil {
		return m.startErr
	}
	<-ctx.Done()
	return nil
}

func (m *mockChannel) Stop() {
	m.mu.Lock()
	m.stopped = true
	m.mu.Unlock()
}

func (m *mockChannel) Send(channel, text string) error {
	m.mu.Lock()
	m.sendCalls = append(m.sendCalls, sendCall{channel, text})
	m.mu.Unlock()
	return nil
}

func (m *mockChannel) OnMessage(fn func(IncomingMessage)) {
	m.mu.Lock()
	m.msgFn = fn
	m.mu.Unlock()
}

// TestManager_NewManager verifies constructor initializes properly.
func TestManager_NewManager(t *testing.T) {
	mgr := NewManager()
	assert.NotNil(t, mgr)
	assert.Equal(t, 0, len(mgr.List()))

}

// TestManager_Stop_Empty verifies Stop on empty manager doesn't panic.
func TestManager_Stop_Empty(_ *testing.T) {
	mgr := NewManager()
	mgr.Stop() // should not panic
}

// TestManager_Stop_ClearsChannels verifies Stop clears internal state.
func TestManager_Stop_ClearsChannels(t *testing.T) {
	mgr := NewManager()

	// Manually inject a mock channel to simulate a running channel.
	mock := &mockChannel{}
	key := "agent1/signal/0"
	_, cancel := context.WithCancel(context.Background())

	mgr.mu.Lock()
	mgr.channels[key] = mock
	mgr.cancels[key] = cancel
	mgr.startTimes[key] = time.Now()
	mgr.sinks[key] = newLogSink()
	mgr.mu.Unlock()

	mgr.Stop()
	assert.Equal(t, 0, len(mgr.channels))
	assert.True(t, mock.stopped)

}

// TestManager_List verifies List returns correct status entries.
func TestManager_List(t *testing.T) {
	mgr := NewManager()

	mock := &mockChannel{}
	key := "myagent/slack/2"
	now := time.Now()

	mgr.mu.Lock()
	mgr.channels[key] = mock
	mgr.cancels[key] = noopCancel
	mgr.startTimes[key] = now
	mgr.sinks[key] = newLogSink()
	mgr.mu.Unlock()

	list := mgr.List()
	assert.Equal(t, 1, len(list))

	s := list[0]
	assert.Equal(t, "myagent", s.Agent)
	assert.Equal(t, "slack", s.Type)
	assert.Equal(t, "2", s.ID)

}

func TestResolveChannelAuthRefs(t *testing.T) {
	dataDir := t.TempDir()
	store.SetDataDir(dataDir)
	t.Cleanup(func() { store.SetDataDir("") })

	authStore, err := auth.NewFileStore(filepath.Join(store.SubDir(store.DirAuth), "credentials.json"))
	assert.NoError(t, err)
	assert.NoError(t, authStore.Set("slack_app_token", "xapp-test"))
	assert.NoError(t, authStore.Set("slack_bot_token", "xoxb-test"))

	resolved, err := resolveChannelAuthRefs(config.ChannelConfig{
		Type:  "slack",
		URL:   "auth:slack_app_token",
		Token: "auth:slack_bot_token",
	})
	assert.NoError(t, err)
	assert.Equal(t, "xapp-test", resolved.URL)
	assert.Equal(t, "xoxb-test", resolved.Token)
}

// TestManager_SubscribeLogs verifies SubscribeLogs returns history for known keys.
func TestManager_SubscribeLogs(t *testing.T) {
	mgr := NewManager()

	key := "agent/signal/0"
	sink := newLogSink()
	sink.Write("test log line")

	mgr.mu.Lock()
	mgr.sinks[key] = sink
	mgr.mu.Unlock()

	history, live, unsub, ok := mgr.SubscribeLogs(key)
	assert.True(t, ok)

	defer unsub()
	_ = live
	assert.Len(t, history, 1)
	assert.Equal(t, "test log line", history[0])

}

// TestManager_SubscribeLogs_UnknownKey verifies ok=false for unknown key.
func TestManager_SubscribeLogs_UnknownKey(t *testing.T) {
	mgr := NewManager()

	_, _, _, ok := mgr.SubscribeLogs("nonexistent/key/0")
	assert.False(t, ok)

}

// TestManager_RouteDelivery_NoChannels verifies error when no channels match.
func TestManager_RouteDelivery_NoChannels(t *testing.T) {
	mgr := NewManager()
	err := mgr.RouteDelivery("signal", "+1", "hello")
	assert.Error(t, err)

}

// TestManager_RouteDelivery_Success verifies message is delivered via matching channel.
func TestManager_RouteDelivery_Success(t *testing.T) {
	mgr := NewManager()

	mock := &mockChannel{}
	key := "agent1/signal/0"

	mgr.mu.Lock()
	mgr.channels[key] = mock
	mgr.cancels[key] = noopCancel
	mgr.startTimes[key] = time.Now()
	mgr.sinks[key] = newLogSink()
	mgr.mu.Unlock()

	err := mgr.RouteDelivery("signal", "+15551111111", "hello there")
	assert.NoError(t, err)

	mock.mu.Lock()
	calls := mock.sendCalls
	mock.mu.Unlock()
	assert.Equal(t, 1, len(calls))
	assert.Equal(t, "+15551111111", calls[0].channel)
	assert.Equal(t, "hello there", calls[0].text)

}

// TestManager_RouteDelivery_WrongType verifies non-matching channel type returns error.
func TestManager_RouteDelivery_WrongType(t *testing.T) {
	mgr := NewManager()

	mock := &mockChannel{}
	key := "agent1/slack/0"
	mgr.mu.Lock()
	mgr.channels[key] = mock
	mgr.cancels[key] = noopCancel
	mgr.startTimes[key] = time.Now()
	mgr.sinks[key] = newLogSink()
	mgr.mu.Unlock()

	err := mgr.RouteDelivery("signal", "+1", "msg")
	assert.Error(t, err)

}

// TestManager_Reconcile_Idempotent verifies calling Reconcile twice with same
// config does not create duplicate channels.
func TestManager_Reconcile_Idempotent(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Config with a signal channel (will fail to connect but that's ok for this test).
	cfg := &config.Config{
		Agents: []config.AgentConfig{
			{
				Name: "bot",
				Channels: []config.ChannelConfig{
					// Use empty signal URL which won't actually connect.
					{Type: "signal", ID: "+1", URL: ""},
				},
			},
		},
	}

	var msgCount int
	msgFn := func(_ string, _, _ string, _ Channel, _ IncomingMessage) { msgCount++ }

	mgr.Reconcile(ctx, cfg, msgFn)
	mgr.Reconcile(ctx, cfg, msgFn) // second call should be idempotent

	list := mgr.List()
	assert.Equal(t, 1, len(list))

	mgr.Stop()
}

// TestManager_Reconcile_RemovesChannel verifies channels removed from config are stopped.
func TestManager_Reconcile_RemovesChannel(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg1 := &config.Config{
		Agents: []config.AgentConfig{
			{
				Name: "bot",
				Channels: []config.ChannelConfig{
					{Type: "signal", ID: "+1", URL: ""},
				},
			},
		},
	}
	cfg2 := &config.Config{Agents: []config.AgentConfig{{Name: "bot"}}} // no channels

	mgr.Reconcile(ctx, cfg1, noopMsgFn)
	assert.Equal(t, 1, len(mgr.List()))

	mgr.Reconcile(ctx, cfg2, noopMsgFn)
	assert.Equal(t, 0, len(mgr.List()))

}

func TestManager_Reconcile_DisabledChannelNotStarted(t *testing.T) {
	disabled := false
	mgr := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := &config.Config{
		Agents: []config.AgentConfig{{
			Name: "bot",
			Channels: []config.ChannelConfig{{
				Type:    "signal",
				ID:      "+1",
				Enabled: &disabled,
			}},
		}},
	}

	mgr.Reconcile(ctx, cfg, noopMsgFn)
	assert.Empty(t, mgr.List())
}

func TestManager_Reconcile_RestartsWhenChannelConfigChanges(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg1 := &config.Config{
		Agents: []config.AgentConfig{{
			Name: "bot",
			Channels: []config.ChannelConfig{{
				Type: "signal",
			}},
		}},
	}
	showTyping := false
	cfg2 := &config.Config{
		Agents: []config.AgentConfig{{
			Name: "bot",
			Channels: []config.ChannelConfig{{
				Type:       "signal",
				ShowTyping: &showTyping,
			}},
		}},
	}

	mgr.Reconcile(ctx, cfg1, noopMsgFn)
	key := channelKey("bot", "signal", "")

	mgr.mu.Lock()
	first := mgr.channels[key]
	firstStarted := mgr.startTimes[key]
	mgr.mu.Unlock()

	time.Sleep(10 * time.Millisecond)
	mgr.Reconcile(ctx, cfg2, noopMsgFn)

	mgr.mu.Lock()
	second := mgr.channels[key]
	secondStarted := mgr.startTimes[key]
	mgr.mu.Unlock()

	assert.NotNil(t, first)
	assert.NotNil(t, second)
	assert.NotSame(t, first, second)
	assert.True(t, secondStarted.After(firstStarted))
}

func TestShouldProcessIncomingMessage_EnabledAtGate(t *testing.T) {
	enabledAt := time.Date(2026, time.March, 12, 10, 0, 0, 0, time.UTC)
	meta := store.ChannelMetadata{EnabledAt: enabledAt}

	assert.False(t, shouldProcessIncomingMessage(meta, IncomingMessage{
		ReceivedAt: enabledAt.Add(-time.Second),
	}))
	assert.True(t, shouldProcessIncomingMessage(meta, IncomingMessage{
		ReceivedAt: enabledAt,
	}))
	assert.True(t, shouldProcessIncomingMessage(meta, IncomingMessage{
		ReceivedAt: enabledAt.Add(time.Second),
	}))
}

// mockMediaSender implements both Channel and MediaSender.
type mockMediaSender struct {
	mockChannel
	mediaCalls []mediaCall
}

type mediaCall struct {
	channelID string
	caption   string
	filePath  string
}

func (m *mockMediaSender) SendMedia(channelID, caption, filePath string) error {
	m.mu.Lock()
	m.mediaCalls = append(m.mediaCalls, mediaCall{channelID, caption, filePath})
	m.mu.Unlock()
	return nil
}

func TestRouteMediaDelivery_NoChannels(t *testing.T) {
	mgr := NewManager()
	err := mgr.RouteMediaDelivery("slack", "C123", "caption", "/tmp/file.png")
	assert.Error(t, err)

}

func TestRouteMediaDelivery_WithMediaSender(t *testing.T) {
	mgr := NewManager()

	ms := &mockMediaSender{}
	mgr.mu.Lock()
	key := channelKey("bot", "slack", "0")
	mgr.channels[key] = ms
	mgr.startTimes[key] = time.Now()
	mgr.mu.Unlock()

	err := mgr.RouteMediaDelivery("slack", "C123", "hi", "/tmp/img.png")
	assert.NoError(t, err)

	ms.mu.Lock()
	defer ms.mu.Unlock()
	assert.Equal(t, 1, len(ms.mediaCalls))

}

func TestRouteMediaDelivery_WrongType(t *testing.T) {
	mgr := NewManager()

	ms := &mockMediaSender{}
	mgr.mu.Lock()
	key := channelKey("bot", "discord", "0")
	mgr.channels[key] = ms
	mgr.startTimes[key] = time.Now()
	mgr.mu.Unlock()

	// Routing to "slack" but channel is "discord" → error.
	err := mgr.RouteMediaDelivery("slack", "C123", "hi", "/tmp/img.png")
	assert.Error(t, err)

}

func TestNewChannel_UnknownType(t *testing.T) {
	ch := newChannel(config.ChannelConfig{Type: "unknown"}, "model", nil)
	assert.Nil(t, ch)

}

func TestNewChannel_Signal(t *testing.T) {
	// Signal channel should be created without error (even with dummy config).
	ch := newChannel(config.ChannelConfig{
		Type: "signal",
		ID:   "+15551234567",
		URL:  "http://localhost:8080",
	}, "model", nil)
	assert.NotNil(t, ch)

}

// ── Discord tests ─────────────────────────────────────────────────────────────

func TestDiscordChannel_Constructor(t *testing.T) {
	ch := NewDiscordChannel("token123", nil, "gpt-4", []string{"fallback"})
	assert.NotNil(t, ch)

}

func TestDiscordChannel_OnMessage(t *testing.T) {
	ch := NewDiscordChannel("t", nil, "m", nil)
	ch.OnMessage(ignoreIncoming)
	ch.handlerMu.RLock()
	fn := ch.handler
	ch.handlerMu.RUnlock()
	assert.NotNil(t, fn)

}

func TestDiscordChannel_Send_NotConnected(t *testing.T) {
	ch := NewDiscordChannel("t", nil, "m", nil)
	err := ch.Send("C123", "hello")
	assert.Error(t, err)

}

func TestDiscordChannel_SendAndGetID_NotConnected(t *testing.T) {
	ch := NewDiscordChannel("t", nil, "m", nil)
	_, err := ch.SendAndGetID("C123", "hello")
	assert.Error(t, err)

}

func TestDiscordChannel_EditMessage_NotConnected(t *testing.T) {
	ch := NewDiscordChannel("t", nil, "m", nil)
	err := ch.EditMessage("C123", "M123", "hello")
	assert.Error(t, err)

}

func TestDiscordChannel_SendMedia_NotConnected(t *testing.T) {
	ch := NewDiscordChannel("t", nil, "m", nil)
	err := ch.SendMedia("C123", "caption", "/tmp/file.png")
	assert.Error(t, err)

}

func TestDiscordChannel_Stop_Idempotent(_ *testing.T) {
	ch := NewDiscordChannel("t", nil, "m", nil)
	ch.Stop()
	ch.Stop() // should not panic
}

func TestDiscordChannel_HandleEditedMention(t *testing.T) {
	ch := NewDiscordChannel("t", []config.AllowFromEntry{{
		From:              "*",
		AllowedGroups:     "*",
		RespondToMentions: true,
	}}, "m", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	ok := ch.handleMessage(&discordgo.Message{
		Author:    &discordgo.User{ID: "U123"},
		ChannelID: "C123",
		GuildID:   "G123",
		Content:   "hi <@BOT123>",
	}, "BOT123")
	assert.True(t, ok)

	msg := waitMsg(t, msgs, time.Second)
	assert.Equal(t, "discord", msg.Type)
	assert.Equal(t, "U123", msg.From)
	assert.Equal(t, "C123", msg.Channel)
	assert.Equal(t, "hi <@BOT123>", msg.Text)
}

func TestDiscordChannel_HandleStructuredMention(t *testing.T) {
	ch := NewDiscordChannel("t", []config.AllowFromEntry{{
		From:              "*",
		AllowedGroups:     "*",
		RespondToMentions: true,
	}}, "m", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	ok := ch.handleMessage(&discordgo.Message{
		Author:    &discordgo.User{ID: "U123"},
		ChannelID: "C123",
		GuildID:   "G123",
		Content:   "hi aviary",
		Mentions: []*discordgo.User{{
			ID: "BOT123",
		}},
	}, "BOT123")
	assert.True(t, ok)

	msg := waitMsg(t, msgs, time.Second)
	assert.Equal(t, "discord", msg.Type)
	assert.Equal(t, "U123", msg.From)
	assert.Equal(t, "C123", msg.Channel)
	assert.Equal(t, "hi aviary", msg.Text)
}

func TestDiscordChannel_IngestsImageAttachment(t *testing.T) {
	base := t.TempDir()
	store.SetDataDir(base)
	t.Cleanup(func() { store.SetDataDir("") })

	imageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-bytes"))
	}))
	defer imageSrv.Close()

	ch := NewDiscordChannel("token123", []config.AllowFromEntry{{From: "*"}}, "gpt-4", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	ok := ch.handleMessage(&discordgo.Message{
		Author:    &discordgo.User{ID: "U123"},
		ChannelID: "C123",
		Content:   "",
		Attachments: []*discordgo.MessageAttachment{{
			Filename:    "photo.png",
			ContentType: "image/png",
			URL:         imageSrv.URL + "/photo.png",
		}},
	}, "BOT123")
	assert.True(t, ok)

	msg := waitMsg(t, msgs, time.Second)
	assert.Len(t, msg.Attachments, 1)
	assert.True(t, strings.HasPrefix(msg.Attachments[0].DataURL, "data:image/png;base64,"))
	entries, err := os.ReadDir(store.IncomingMediaDir("discord"))
	assert.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestDiscordChannel_SendAndEditAndMedia(t *testing.T) {
	var (
		sendAuthHeader   string
		sendBody         string
		editBody         string
		uploadAuthHeader string
		uploadBody       string
		uploadCT         string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v9/channels/C123/messages":
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			if strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
				uploadAuthHeader = r.Header.Get("Authorization")
				uploadBody = string(body)
				uploadCT = r.Header.Get("Content-Type")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"M456"}`))
				return
			}
			sendAuthHeader = r.Header.Get("Authorization")
			sendBody = string(body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"M123"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v9/channels/C123/messages/M123":
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			editBody = string(body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"M123"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	restore := overrideDiscordEndpointsForTest(srv.URL + "/")
	defer restore()

	session, err := discordgo.New("Bot test-token")
	assert.NoError(t, err)
	ch := NewDiscordChannel("token123", nil, "m", nil)
	ch.session = session

	msgID, err := ch.SendAndGetID("C123", "hello there")
	assert.NoError(t, err)
	assert.Equal(t, "M123", msgID)
	assert.Equal(t, "Bot test-token", sendAuthHeader)
	assert.Contains(t, sendBody, `"content":"hello there"`)

	err = ch.EditMessage("C123", "M123", "updated text")
	assert.NoError(t, err)
	assert.Contains(t, editBody, `"content":"updated text"`)

	filePath := filepath.Join(t.TempDir(), "image.png")
	err = os.WriteFile(filePath, []byte("png-bytes"), 0o600)
	assert.NoError(t, err)

	err = ch.SendMedia("C123", "look at this", filePath)
	assert.NoError(t, err)
	assert.Equal(t, "Bot test-token", uploadAuthHeader)
	assert.Contains(t, uploadCT, "multipart/form-data")
	assert.Contains(t, uploadBody, "look at this")
	assert.Contains(t, uploadBody, "image.png")
	assert.Contains(t, uploadBody, "png-bytes")
}

func TestNewChannel_Discord(t *testing.T) {
	ch := newChannel(config.ChannelConfig{
		Type:  "discord",
		Token: "bot-token",
	}, "model", nil)
	assert.NotNil(t, ch)

}

// ── Slack tests ───────────────────────────────────────────────────────────────

func TestSlackChannel_Constructor(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "gpt-4", nil)
	assert.NotNil(t, ch)

}

func TestSlackChannel_OnMessage(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	ch.OnMessage(ignoreIncoming)
	ch.handlerMu.RLock()
	fn := ch.handler
	ch.handlerMu.RUnlock()
	assert.NotNil(t, fn)

}

func TestSlackChannel_Stop_NilCancel(_ *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	ch.Stop() // should not panic when cancel is nil
	ch.Stop() // idempotent
}

func TestSlackChannel_LogSinkWritesRuntimeMessages(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	sink := newLogSink()
	ch.SetLogSink(sink)

	ch.logf("slack: connected")

	history, _, unsub := sink.Subscribe()
	defer unsub()
	assert.Len(t, history, 1)
	assert.Contains(t, history[0], "slack: connected")
}

func TestSlackChannel_Dispatch_WrongType(_ *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	// Non-eventsAPI event type causes early return before Ack — no panic.
	ch.dispatch(socketmode.Event{Type: socketmode.EventTypeHello})
}

func TestDiscordChannel_LogSinkWritesRuntimeMessages(t *testing.T) {
	ch := NewDiscordChannel("token", nil, "m", nil)
	sink := newLogSink()
	ch.SetLogSink(sink)

	ch.logf("discord: session connected")

	history, _, unsub := sink.Subscribe()
	defer unsub()
	assert.Len(t, history, 1)
	assert.Contains(t, history[0], "discord: session connected")
}

func TestSlackChannel_HandleEditedMention(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", []config.AllowFromEntry{{
		From:              "*",
		AllowedGroups:     "*",
		RespondToMentions: true,
	}}, "m", nil)
	ch.botUserID = "UBOT"
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	ch.handleMessageEvent(&slackevents.MessageEvent{
		Type:    "message",
		User:    "U123",
		Channel: "C123",
		SubType: "message_changed",
		Message: &slack.Msg{
			User:    "U123",
			Channel: "C123",
			Text:    "hi <@UBOT>",
			Edited:  &slack.Edited{},
		},
	})

	msg := waitMsg(t, msgs, time.Second)
	assert.Equal(t, "slack", msg.Type)
	assert.Equal(t, "U123", msg.From)
	assert.Equal(t, "C123", msg.Channel)
	assert.Equal(t, "hi <@UBOT>", msg.Text)
}

func TestSlackChannel_IgnoresThreadMetadataChange(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", []config.AllowFromEntry{{
		From: "*", AllowedGroups: "*", RespondToMentions: true,
	}}, "m", nil)
	ch.botUserID = "UBOT"
	var messages []IncomingMessage
	ch.OnMessage(func(m IncomingMessage) { messages = append(messages, m) })
	ch.handleMessageEvent(&slackevents.MessageEvent{
		SubType: "message_changed", Channel: "C123", TimeStamp: "1710000001.123456",
		Message:         &slack.Msg{User: "U123", Text: "<@UBOT> hi", Timestamp: "1710000000.123456"},
		PreviousMessage: &slack.Msg{User: "U123", Text: "<@UBOT> hi", Timestamp: "1710000000.123456"},
	})
	assert.Empty(t, messages)
}

func TestSlackChannel_HandleAppMention(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", []config.AllowFromEntry{{
		From:              "*",
		AllowedGroups:     "*",
		RespondToMentions: true,
	}}, "m", nil)
	ch.botUserID = "UBOT"
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	ch.handleAppMentionEvent(&slackevents.AppMentionEvent{
		Type:            "app_mention",
		User:            "U123",
		Channel:         "C123",
		Text:            "<@UBOT> hi",
		TimeStamp:       "1710000000.123456",
		ThreadTimeStamp: "1710000000.000001",
	})

	msg := waitMsg(t, msgs, time.Second)
	assert.Equal(t, "slack", msg.Type)
	assert.Equal(t, "U123", msg.From)
	assert.Equal(t, "C123", msg.Channel)
	assert.Equal(t, "1710000000.000001", msg.ThreadTS)
	assert.Equal(t, "<@UBOT> hi", msg.Text)
	assert.Equal(t, time.Unix(1710000000, 123456000).UTC(), msg.ReceivedAt)
}

func TestSlackChannel_DeduplicatesMessageAndAppMention(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", []config.AllowFromEntry{{
		From: "*", AllowedGroups: "*", RespondToMentions: true,
	}}, "m", nil)
	ch.botUserID = "UBOT"
	var messages []IncomingMessage
	ch.OnMessage(func(m IncomingMessage) { messages = append(messages, m) })
	ch.handleMessageEvent(&slackevents.MessageEvent{
		User: "U123", Channel: "C123", Text: "<@UBOT> hi", TimeStamp: "1710000000.123456",
	})
	ch.handleAppMentionEvent(&slackevents.AppMentionEvent{
		User: "U123", Channel: "C123", Text: "<@UBOT> hi", TimeStamp: "1710000000.123456",
	})
	assert.Len(t, messages, 1)
}

func TestSlackChannel_IgnoresOwnChannelJoin(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", []config.AllowFromEntry{{
		From: "*", AllowedGroups: "*", RespondToMentions: true,
	}}, "m", nil)
	ch.botUserID = "UBOT"
	var messages []IncomingMessage
	ch.OnMessage(func(m IncomingMessage) { messages = append(messages, m) })
	ch.handleMessageEvent(&slackevents.MessageEvent{
		User: "UBOT", Channel: "C123", Text: "<@UBOT> has joined the channel", TimeStamp: "1710000000.123456", SubType: "channel_join",
	})
	assert.Empty(t, messages)
}

func TestSlackChannel_IgnoresRootOnlyMessageReplied(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", []config.AllowFromEntry{{
		From: "*", AllowedGroups: "*", RespondToMentions: true,
	}}, "m", nil)
	ch.botUserID = "UBOT"
	var messages []IncomingMessage
	ch.OnMessage(func(m IncomingMessage) { messages = append(messages, m) })
	ch.handleMessageEvent(&slackevents.MessageEvent{
		SubType: "message_replied", Channel: "C123", TimeStamp: "1710000000.123456",
		Message: &slack.Msg{User: "U123", Text: "<@UBOT> hi", Timestamp: "1710000000.123456", LatestReply: "1710000000.123456"},
	})
	assert.Empty(t, messages)
}

func TestSlackChannel_HandleMessageUsesTimestampAsThreadFallback(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", []config.AllowFromEntry{{From: "*"}}, "m", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	ch.handleMessageEvent(&slackevents.MessageEvent{
		User:      "U123",
		Channel:   "D123",
		Text:      "hello",
		TimeStamp: "1710000000.123456",
	})

	msg := waitMsg(t, msgs, time.Second)
	assert.Equal(t, "1710000000.123456", msg.ThreadTS)
}

func TestSlackChannel_SendThreadMessageAndGetIDDoesNotBroadcast(t *testing.T) {
	var sawPost bool
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat.postMessage", r.URL.Path)
		assert.NoError(t, r.ParseForm())
		assert.Equal(t, "C123", r.FormValue("channel"))
		assert.Equal(t, "1710000000.123456", r.FormValue("thread_ts"))
		assert.Empty(t, r.FormValue("reply_broadcast"))
		assert.Equal(t, "hello", r.FormValue("text"))
		sawPost = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"channel": "C123",
			"ts":      "1710000001.000001",
		})
	}))
	defer api.Close()

	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	ch.client = slack.New("xoxb-token", slack.OptionAPIURL(api.URL+"/"))

	msgID, err := ch.SendThreadMessageAndGetID("C123", "1710000000.123456", "hello")

	assert.NoError(t, err)
	assert.Equal(t, "1710000001.000001", msgID)
	assert.True(t, sawPost)
}

func TestParseSlackMessageURL(t *testing.T) {
	ref, ok := parseSlackMessageURL("https://aviary.slack.com/archives/C123/p1710000000123456?thread_ts=1710000000.000100&cid=C999")

	assert.True(t, ok)
	assert.Equal(t, "C999", ref.ChannelID)
	assert.Equal(t, "1710000000.123456", ref.Timestamp)
	assert.Equal(t, "1710000000.000100", ref.ThreadTS)
}

func TestSlackChannel_EnrichesCopiedSlackMessageURL(t *testing.T) {
	var sawReplies bool
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/conversations.replies", r.URL.Path)
		assert.Equal(t, "C123", r.FormValue("channel"))
		assert.Equal(t, "1710000000.000100", r.FormValue("ts"))
		sawReplies = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"messages": []map[string]any{
				{"type": "message", "user": "U1", "text": "root message", "ts": "1710000000.000100"},
				{"type": "message", "user": "U2", "text": "thread reply", "ts": "1710000001.000200"},
			},
		})
	}))
	defer api.Close()

	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	ch.client = slack.New("xoxb-token", slack.OptionAPIURL(api.URL+"/"))
	ch.identityMu.Lock()
	ch.userNames = map[string]string{"U1": "Alice", "U2": "Bob"}
	ch.identityMu.Unlock()

	text := ch.enrichSlackMessageText(
		context.Background(),
		"look at <https://aviary.slack.com/archives/C123/p1710000000123456?thread_ts=1710000000.000100&cid=C123|this>",
		"D123",
		"1710000100.000000",
		"1710000100.000000",
		nil,
	)

	assert.True(t, sawReplies)
	assert.Contains(t, text, "look at")
	assert.Contains(t, text, "[linked message: C123 1710000000.000100]")
	assert.Contains(t, text, "Alice: root message")
	assert.Contains(t, text, "Bob: thread reply")
}

func TestSlackChannel_EnrichesCurrentThreadReply(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/conversations.replies", r.URL.Path)
		assert.Equal(t, "C123", r.FormValue("channel"))
		assert.Equal(t, "1710000000.000100", r.FormValue("ts"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"messages": []map[string]any{
				{"type": "message", "user": "U1", "text": "root context", "ts": "1710000000.000100"},
				{"type": "message", "user": "U2", "text": "earlier reply", "ts": "1710000001.000200"},
			},
		})
	}))
	defer api.Close()

	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	ch.client = slack.New("xoxb-token", slack.OptionAPIURL(api.URL+"/"))

	text := ch.enrichSlackMessageText(
		context.Background(),
		"latest reply",
		"C123",
		"1710000002.000300",
		"1710000000.000100",
		nil,
	)

	assert.Contains(t, text, "latest reply")
	assert.Contains(t, text, "[current thread: C123 1710000000.000100]")
	assert.Contains(t, text, "U1: root context")
	assert.Contains(t, text, "U2: earlier reply")
}

func TestSlackChannel_HandlesForwardedAttachmentText(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", []config.AllowFromEntry{{From: "*"}}, "m", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	ch.handleMessageEvent(&slackevents.MessageEvent{
		User:    "U123",
		Channel: "D123",
		Message: &slack.Msg{
			User:    "U123",
			Channel: "D123",
			Attachments: []slack.Attachment{{
				AuthorName: "Alice",
				Text:       "forwarded body",
				Fallback:   "forwarded fallback",
			}},
		},
	})

	msg := waitMsg(t, msgs, time.Second)
	assert.Contains(t, msg.Text, "Alice")
	assert.Contains(t, msg.Text, "forwarded body")
	assert.Contains(t, msg.Text, "forwarded fallback")
}

func TestSlackChannel_IngestsImageAttachment(t *testing.T) {
	base := t.TempDir()
	store.SetDataDir(base)
	t.Cleanup(func() { store.SetDataDir("") })

	imageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer xoxb-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-bytes"))
	}))
	defer imageSrv.Close()

	ch := NewSlackChannel("xapp-token", "xoxb-token", []config.AllowFromEntry{{From: "*"}}, "m", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	ch.handleMessageEvent(&slackevents.MessageEvent{
		User:    "U123",
		Channel: "D123",
		Message: &slack.Msg{
			User:    "U123",
			Channel: "D123",
			Files: []slack.File{{
				Name:               "photo.png",
				Mimetype:           "image/png",
				URLPrivateDownload: imageSrv.URL + "/photo.png",
			}},
		},
	})

	msg := waitMsg(t, msgs, time.Second)
	assert.Len(t, msg.Attachments, 1)
	assert.True(t, strings.HasPrefix(msg.Attachments[0].DataURL, "data:image/png;base64,"))
	entries, err := os.ReadDir(store.IncomingMediaDir("slack"))
	assert.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestSlackChannel_AllowFromResolvesUserAndChannelNames(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", []config.AllowFromEntry{{
		From:          "@alice",
		AllowedGroups: "#alerts",
	}}, "m", nil)
	ch.identityMu.Lock()
	ch.userAliases = map[string]string{"@alice": "U123", "alice": "U123"}
	ch.channelAliases = map[string]string{"alerts": "C123", "#alerts": "C123"}
	ch.resolvedAllowFrom = ch.resolveAllowEntries(ch.allowFrom)
	ch.identityMu.Unlock()

	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })
	ch.handleMessageEvent(&slackevents.MessageEvent{
		User:    "U123",
		Channel: "C123",
		Text:    "hello",
	})

	msg := waitMsg(t, msgs, time.Second)
	assert.Equal(t, "U123", msg.From)
	assert.Equal(t, "C123", msg.Channel)
}

func TestSlackChannel_SendResolvesChannelName(t *testing.T) {
	var postedChannel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, r.ParseForm())
		switch r.URL.Path {
		case "/chat.postMessage":
			postedChannel = r.Form.Get("channel")
			_, _ = w.Write([]byte(`{"ok":true,"channel":"C123","ts":"1710000000.000100","message":{"text":"hi"}}`))
		default:
			t.Fatalf("unexpected Slack API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	ch.client = slack.New("xoxb-token", slack.OptionAPIURL(server.URL+"/"))
	ch.identityMu.Lock()
	ch.channelAliases = map[string]string{"alerts": "C123"}
	ch.identityMu.Unlock()

	err := ch.Send("#alerts", "hi")
	assert.NoError(t, err)
	assert.Equal(t, "C123", postedChannel)
}

func TestSlackChannel_SendResolvesUsernameToDM(t *testing.T) {
	var (
		openUsers     string
		postedChannel string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, r.ParseForm())
		switch r.URL.Path {
		case "/conversations.open":
			openUsers = r.Form.Get("users")
			_, _ = w.Write([]byte(`{"ok":true,"channel":{"id":"D123"}}`))
		case "/chat.postMessage":
			postedChannel = r.Form.Get("channel")
			_, _ = w.Write([]byte(`{"ok":true,"channel":"D123","ts":"1710000000.000100","message":{"text":"hi"}}`))
		default:
			t.Fatalf("unexpected Slack API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	ch.client = slack.New("xoxb-token", slack.OptionAPIURL(server.URL+"/"))
	ch.identityMu.Lock()
	ch.userAliases = map[string]string{"@alice": "U123", "alice": "U123"}
	ch.identityMu.Unlock()

	err := ch.Send("@alice", "hi")
	assert.NoError(t, err)
	assert.Equal(t, "U123", openUsers)
	assert.Equal(t, "D123", postedChannel)
}

func TestNewChannel_Slack(t *testing.T) {
	ch := newChannel(config.ChannelConfig{
		Type:  "slack",
		Token: "xoxb-token",
		URL:   "xapp-token",
	}, "model", nil)
	assert.NotNil(t, ch)

}

func TestNewChannel_Slack_IgnoresShowTyping(t *testing.T) {
	enabled := true
	ch := newChannel(config.ChannelConfig{
		Type:       "slack",
		Token:      "xoxb-token",
		URL:        "xapp-token",
		ShowTyping: &enabled,
	}, "model", nil)
	assert.NotNil(t, ch)
	_, ok := ch.(TypingSender)
	assert.False(t, ok)
}

func TestRoutedSlackMessage_SharedConnectionRoutesMatchingSpec(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	ch.botUserID = "U0APX2B3ZJB"
	msg := IncomingMessage{
		Type:    "slack",
		From:    "U0AQFSYBQ84",
		Channel: "C0AQ5TNJ151",
		Text:    "<@U0APX2B3ZJB> hi",
	}

	ony := channelSpec{
		agentName: "Ony",
		channelConfig: config.ChannelConfig{
			Type: "slack",
			ID:   "U0APX2B3ZJB",
			AllowFrom: []config.AllowFromEntry{{
				From:              "*",
				AllowedGroups:     "C0AQ5TNJ151",
				RespondToMentions: true,
			}},
		},
		agentModel: "model/ony",
	}
	coder := channelSpec{
		agentName: "Coder",
		channelConfig: config.ChannelConfig{
			Type: "slack",
			ID:   "U0APX2B3ZJB",
			AllowFrom: []config.AllowFromEntry{{
				From:              "*",
				AllowedGroups:     "C0BDCC8G5T3",
				RespondToMentions: true,
			}},
		},
		agentModel: "model/coder",
	}

	routed, ok := routedSlackMessage(ch, ony, msg)
	assert.True(t, ok)
	assert.Equal(t, "model/ony", routed.Model)

	_, ok = routedSlackMessage(ch, coder, msg)
	assert.False(t, ok)
}

func TestRoutedSlackMessage_SharedConnectionResolvesChannelName(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	ch.botUserID = "UBOT"
	ch.channelAliases = map[string]string{"research": "C123"}
	spec := channelSpec{
		agentName: "Research",
		channelConfig: config.ChannelConfig{
			Type: "slack",
			ID:   "bot",
			AllowFrom: []config.AllowFromEntry{{
				From:              "*",
				AllowedGroups:     "#research",
				RespondToMentions: true,
			}},
		},
	}
	msg := IncomingMessage{Type: "slack", From: "U123", Channel: "C123", Text: "<@UBOT> hi"}

	_, ok := routedSlackMessage(ch, spec, msg)
	assert.True(t, ok)
	assert.True(t, matchesAnyAllowedGroup(ch.resolvedEntriesForRouting(spec.channelConfig.AllowFrom), msg.Channel))

	msg.Channel = "COTHER"
	_, ok = routedSlackMessage(ch, spec, msg)
	assert.False(t, ok)
}

func TestManager_ReconcileSharesSlackConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := &config.Config{Agents: []config.AgentConfig{
		{
			Name: "Ony",
			Channels: []config.ChannelConfig{{
				Type:  "slack",
				ID:    "U0APX2B3ZJB",
				Token: "xoxb-token",
				URL:   "xapp-token",
				AllowFrom: []config.AllowFromEntry{{
					From:              "*",
					AllowedGroups:     "C0AQ5TNJ151",
					RespondToMentions: true,
				}},
			}},
		},
		{
			Name: "Coder",
			Channels: []config.ChannelConfig{{
				Type:  "slack",
				ID:    "U0APX2B3ZJB",
				Token: "xoxb-token",
				URL:   "xapp-token",
				AllowFrom: []config.AllowFromEntry{{
					From:              "*",
					AllowedGroups:     "C0BDCC8G5T3",
					RespondToMentions: true,
				}},
			}},
		},
	}}

	mgr := NewManager()
	mgr.Reconcile(ctx, cfg, noopMsgFn)

	assert.Len(t, mgr.slack, 1)
	assert.Len(t, mgr.channels, 2)
	assert.Same(t, mgr.channels[channelKey("Ony", "slack", "U0APX2B3ZJB")], mgr.channels[channelKey("Coder", "slack", "U0APX2B3ZJB")])
}

func overrideDiscordEndpointsForTest(base string) func() {
	origDiscord := discordgo.EndpointDiscord
	origAPI := discordgo.EndpointAPI
	origChannels := discordgo.EndpointChannels
	origUsers := discordgo.EndpointUsers
	origGateway := discordgo.EndpointGateway
	origGatewayBot := discordgo.EndpointGatewayBot
	origWebhooks := discordgo.EndpointWebhooks

	discordgo.EndpointDiscord = base
	discordgo.EndpointAPI = base + "api/v" + discordgo.APIVersion + "/"
	discordgo.EndpointChannels = discordgo.EndpointAPI + "channels/"
	discordgo.EndpointUsers = discordgo.EndpointAPI + "users/"
	discordgo.EndpointGateway = discordgo.EndpointAPI + "gateway"
	discordgo.EndpointGatewayBot = discordgo.EndpointGateway + "/bot"
	discordgo.EndpointWebhooks = discordgo.EndpointAPI + "webhooks/"

	return func() {
		discordgo.EndpointDiscord = origDiscord
		discordgo.EndpointAPI = origAPI
		discordgo.EndpointChannels = origChannels
		discordgo.EndpointUsers = origUsers
		discordgo.EndpointGateway = origGateway
		discordgo.EndpointGatewayBot = origGatewayBot
		discordgo.EndpointWebhooks = origWebhooks
	}
}

// ── Signal ShowTyping tests ──────────────────────────────────────────────────

func TestSignalChannel_ShowTyping(t *testing.T) {
	ch := NewSignalChannel("+1", "", nil, true, false, false, false, "m", nil)
	assert.True(t, ch.ShowTyping())

	ch2 := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	assert.False(t, ch2.ShowTyping())

}

// ── downloadTempImage tests ───────────────────────────────────────────────────

func TestDownloadTempImage_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("fake image data"))
	}))
	defer srv.Close()

	path, err := downloadTempImage(context.Background(), srv.URL+"/img.png")
	assert.NoError(t, err)

	defer func() { _ = os.Remove(path) }()
	assert. //nolint:errcheck
		NotEqual(t, "", path)

	data, readErr := os.ReadFile(path)
	assert.Nil(t, readErr)
	assert.Equal(t, "fake image data", string(data))

}

func TestDownloadTempImage_NotFound(_ *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a 404 with no body — downloadTempImage should still succeed
		// (it copies whatever body is present); test that it at least returns
		// a path without error OR an empty-body error is tolerated.
		// The actual implementation doesn't check status code, only copy errors.
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// downloadTempImage does not check HTTP status, so it will still succeed
	// (copies the "404 page not found\n" body). Just verify no panic.
	path, err := downloadTempImage(context.Background(), srv.URL+"/notfound.png")
	if err == nil && path != "" {
		defer os.Remove(path) //nolint:errcheck
	}
	// Either outcome (success with 404 body, or error) is acceptable.
	_ = err
}

// ── streamToSink tests ────────────────────────────────────────────────────────

func TestSignalChannel_StreamToSink(t *testing.T) {
	ch := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	sink := newLogSink()
	ch.SetLogSink(sink)

	data := "line1\nline2\nline3\n"
	r := io.NopCloser(strings.NewReader(data))
	ch.streamToSink(r)

	history, _, unsub := sink.Subscribe()
	unsub()
	assert.GreaterOrEqual(t, len(history), 3)

}

func TestSignalChannel_StreamToSink_NoSink(_ *testing.T) {
	// streamToSink with nil LogSink should not panic.
	ch := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	// logSink is nil by default
	r := io.NopCloser(strings.NewReader("line1\nline2\n"))
	ch.streamToSink(r) // should not panic
}

// ── Manager lifecycle with real channel implementations ─────────────────────

// waitSlackStopped waits for a manager-started Slack connection's Start to
// return. Manager Slack tests use a cancelled context so Start never reaches
// the network; waiting keeps Stop from racing the still-starting channel.
func waitSlackStopped(t *testing.T, mgr *Manager, key string) {
	t.Helper()
	mgr.mu.Lock()
	sink := mgr.sinks[key]
	mgr.mu.Unlock()
	require.NotNil(t, sink)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(sinkHistory(sink), "slack: socket mode stopped") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("slack channel %s never stopped", key)
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// waitChannelError polls until the channel reports a runtime error.
func waitChannelError(t *testing.T, mgr *Manager, key string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mgr.mu.Lock()
		msg := mgr.errors[key]
		mgr.mu.Unlock()
		if msg != "" {
			return msg
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("channel %s never reported an error", key)
	return ""
}

// noopMsgFn is a Reconcile message callback for tests that never inspect
// routed messages.
func noopMsgFn(string, string, string, Channel, IncomingMessage) {
	// Messages are intentionally discarded.
}

// noopCancel stands in for a channel's context.CancelFunc in tests that seed
// Manager state directly; there is no context to cancel.
func noopCancel() {
	// Nothing to cancel.
}

// ignoreIncoming is an OnMessage handler for tests that only need one registered.
func ignoreIncoming(IncomingMessage) {
	// Messages are intentionally discarded.
}

func slackAgent(name, groups string) config.AgentConfig {
	return config.AgentConfig{Name: name, Channels: []config.ChannelConfig{{
		Type: "slack", ID: "UBOT", Token: "xoxb-token", URL: "xapp-token",
		AllowFrom: []config.AllowFromEntry{{From: "*", AllowedGroups: groups}},
	}}}
}

func TestManager_ReconcileSharedSlackLifecycle(t *testing.T) {
	useTempDataDir(t)
	ctx := cancelledContext()
	mgr := NewManager()
	defer mgr.Stop()

	key := channelKey("Ony", "slack", "UBOT")
	connKey := slackConnectionKey(config.ChannelConfig{Token: "xoxb-token", URL: "xapp-token"})
	cfg := &config.Config{Agents: []config.AgentConfig{slackAgent("Ony", "C1")}}

	mgr.Reconcile(ctx, cfg, noopMsgFn)
	waitSlackStopped(t, mgr, key)
	first := mgr.slack[connKey]
	require.NotNil(t, first)

	// Unchanged config keeps the running connection.
	mgr.Reconcile(ctx, cfg, noopMsgFn)
	assert.Same(t, first, mgr.slack[connKey])
	assert.Same(t, first.ch, mgr.channels[key])

	// Changed allowFrom replaces the shared connection.
	cfg = &config.Config{Agents: []config.AgentConfig{slackAgent("Ony", "C2")}}
	mgr.Reconcile(ctx, cfg, noopMsgFn)
	second := mgr.slack[connKey]
	require.NotNil(t, second)
	assert.NotSame(t, first, second)
	assert.Equal(t, "C2", second.specs[0].channelConfig.AllowFrom[0].AllowedGroups)
	waitSlackStopped(t, mgr, key)

	// Dropping Slack from the config stops the connection and its aliases.
	mgr.Reconcile(ctx, &config.Config{Agents: []config.AgentConfig{{Name: "Ony"}}}, noopMsgFn)
	assert.Empty(t, mgr.slack)
	assert.Empty(t, mgr.slackAlias)
	assert.Empty(t, mgr.List())
}

func TestManager_ReconcileSkipsChannelsThatFailToStart(t *testing.T) {
	useTempDataDir(t)
	require.NoError(t, os.WriteFile(store.StatePath(), []byte("{not json"), 0o600))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewManager()
	defer mgr.Stop()

	mgr.Reconcile(ctx, &config.Config{Agents: []config.AgentConfig{{
		Name: "bot",
		Channels: []config.ChannelConfig{
			{Type: "carrier-pigeon", ID: "coop"},
			{Type: "slack", ID: "UBOT", Token: "auth:missing_slack_token", URL: "xapp-token"},
			{Type: "signal", ID: ""},
		},
	}}}, noopMsgFn)

	list := mgr.List()
	require.Len(t, list, 1, "an unreadable state file still starts valid channels")
	assert.Equal(t, "signal", list[0].Type)
	assert.Empty(t, mgr.slack)
}

func TestManager_Restart(t *testing.T) {
	useTempDataDir(t)
	ctx := cancelledContext()
	mgr := NewManager()
	defer mgr.Stop()

	signalKey := channelKey("bot", "signal", "")
	slackKey := channelKey("Ony", "slack", "UBOT")
	connKey := slackConnectionKey(config.ChannelConfig{Token: "xoxb-token", URL: "xapp-token"})
	cfg := &config.Config{Agents: []config.AgentConfig{
		{Name: "bot", Channels: []config.ChannelConfig{{Type: "signal"}}},
		slackAgent("Ony", "C1"),
	}}
	mgr.Reconcile(ctx, cfg, noopMsgFn)
	waitSlackStopped(t, mgr, slackKey)

	t.Run("unknown key", func(t *testing.T) {
		err := mgr.Restart(ctx, "bot/signal/nope", noopMsgFn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("signal channel is recreated", func(t *testing.T) {
		mgr.mu.Lock()
		before := mgr.channels[signalKey]
		mgr.mu.Unlock()

		require.NoError(t, mgr.Restart(ctx, signalKey, noopMsgFn))

		mgr.mu.Lock()
		after := mgr.channels[signalKey]
		mgr.mu.Unlock()
		require.NotNil(t, after)
		assert.NotSame(t, before, after)
		assert.Equal(t, "bot", after.(*SignalChannel).AgentName)
	})

	t.Run("shared slack connection is recreated", func(t *testing.T) {
		before := mgr.slack[connKey]
		require.NoError(t, mgr.Restart(ctx, slackKey, noopMsgFn))
		waitSlackStopped(t, mgr, slackKey)

		mgr.mu.Lock()
		defer mgr.mu.Unlock()
		after := mgr.slack[connKey]
		require.NotNil(t, after)
		assert.NotSame(t, before, after)
		assert.Same(t, after.ch, mgr.channels[slackKey])
	})

	t.Run("shared slack restart failure", func(t *testing.T) {
		mgr.mu.Lock()
		mgr.slack[connKey].specs[0].channelConfig.Token = "auth:missing_slack_token"
		mgr.mu.Unlock()

		err := mgr.Restart(ctx, slackKey, noopMsgFn)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "resolve channel auth refs")
		mgr.mu.Lock()
		defer mgr.mu.Unlock()
		assert.Empty(t, mgr.slack, "the old connection was stopped before the failed start")
		assert.NotContains(t, mgr.channels, slackKey)
	})

	t.Run("slack alias without an active connection", func(t *testing.T) {
		mgr.mu.Lock()
		mgr.slackAlias[slackKey] = connKey
		mgr.mu.Unlock()
		err := mgr.Restart(ctx, slackKey, noopMsgFn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not active")
	})

	t.Run("channel restart failure", func(t *testing.T) {
		mgr.mu.Lock()
		spec := mgr.specs[signalKey]
		spec.channelConfig.Type = "carrier-pigeon"
		mgr.specs[signalKey] = spec
		mgr.mu.Unlock()

		err := mgr.Restart(ctx, signalKey, noopMsgFn)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "could not be created")
		mgr.mu.Lock()
		defer mgr.mu.Unlock()
		assert.NotContains(t, mgr.channels, signalKey)
	})
}

func TestManager_StartChannelRecordsRuntimeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"401: Unauthorized","code":0}`))
	}))
	defer server.Close()
	restore := overrideDiscordEndpointsForTest(server.URL + "/")
	defer restore()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewManager()
	defer mgr.Stop()

	key := channelKey("bot", "discord", "main")
	mgr.Reconcile(ctx, &config.Config{Agents: []config.AgentConfig{{
		Name:     "bot",
		Channels: []config.ChannelConfig{{Type: "discord", ID: "main", Token: "bad-token"}},
	}}}, noopMsgFn)

	waitChannelError(t, mgr, key)
	list := mgr.List()
	require.Len(t, list, 1)
	assert.NotEmpty(t, list[0].Error)
}

func TestManager_SignalChannelHandlersFilterAndLogGroupChat(t *testing.T) {
	useTempDataDir(t)
	key := channelKey("bot", "signal", "")
	enabledAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	require.NoError(t, store.WriteAppState(&store.AppState{Channels: map[string]store.ChannelMetadata{key: {EnabledAt: enabledAt}}}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewManager()
	defer mgr.Stop()

	type routed struct {
		agent, typ, id string
		msg            IncomingMessage
	}
	var got []routed
	mgr.Reconcile(ctx, &config.Config{Agents: []config.AgentConfig{{
		Name: "bot", Channels: []config.ChannelConfig{{Type: "signal"}},
	}}}, func(agentName, channelType, configuredID string, _ Channel, msg IncomingMessage) {
		got = append(got, routed{agentName, channelType, configuredID, msg})
	})

	mgr.mu.Lock()
	sc := mgr.channels[key].(*SignalChannel)
	mgr.mu.Unlock()
	sc.handlerMu.RLock()
	handler, groupLog := sc.handler, sc.groupLogHandler
	sc.handlerMu.RUnlock()
	require.NotNil(t, handler)
	require.NotNil(t, groupLog)

	handler(IncomingMessage{Type: "signal", Text: "stale", ReceivedAt: enabledAt.Add(-time.Second)})
	handler(IncomingMessage{Type: "signal", Text: "fresh", ReceivedAt: enabledAt.Add(time.Second)})
	require.Len(t, got, 1, "messages received before the channel was enabled are dropped")
	assert.Equal(t, routed{"bot", "signal", "", IncomingMessage{Type: "signal", Text: "fresh", ReceivedAt: enabledAt.Add(time.Second)}}, got[0])

	groupLog(IncomingMessage{Type: "signal", Channel: "G1", From: "+1555", SenderName: "Ann", Text: "  side chatter  "})
	sess, err := agent.NewSessionManager().GetOrCreateNamed("bot", "signal:G1")
	require.NoError(t, err)
	msgs, err := store.ReadJSONL[domain.Message](store.SessionPath("bot", sess.ID))
	require.NoError(t, err)
	var texts []string
	for _, m := range msgs {
		if m.Role == domain.MessageRoleUser {
			texts = append(texts, m.Content)
		}
	}
	assert.Equal(t, []string{"side chatter"}, texts)
}

func userSessionTexts(t *testing.T, agentName, sessionName string) []string {
	t.Helper()
	sess, err := agent.NewSessionManager().GetOrCreateNamed(agentName, sessionName)
	require.NoError(t, err)
	msgs, err := store.ReadJSONL[domain.Message](store.SessionPath(agentName, sess.ID))
	require.NoError(t, err)
	var texts []string
	for _, m := range msgs {
		if m.Role == domain.MessageRoleUser {
			texts = append(texts, m.Content)
		}
	}
	return texts
}

func TestManager_SharedSlackHandlersRoutePerAgent(t *testing.T) {
	useTempDataDir(t)
	lateKey := channelKey("Late", "slack", "UBOT")
	require.NoError(t, store.WriteAppState(&store.AppState{Channels: map[string]store.ChannelMetadata{
		lateKey: {EnabledAt: time.Now().Add(time.Hour)},
	}}))
	ctx := cancelledContext()

	quiet := slackAgent("Quiet", "C1")
	quiet.Channels[0].GroupChatHistory = -1
	quiet.Channels[0].SeparateTopLevelSessions = boolPtr(true)
	cfg := &config.Config{Agents: []config.AgentConfig{
		slackAgent("Ony", "C1"),
		slackAgent("Coder", "C2"),
		slackAgent("Late", "C1"),
		quiet,
	}}
	var routedTo []string
	mgr := NewManager()
	mgr.Reconcile(ctx, cfg, func(agentName, _, _ string, _ Channel, msg IncomingMessage) {
		routedTo = append(routedTo, agentName+":"+msg.Text)
	})
	ch := mgr.channels[channelKey("Ony", "slack", "UBOT")].(*SlackChannel)
	assert.True(t, ch.ShowAssistantStatus())

	ch.handleMessageEvent(&slackevents.MessageEvent{
		User: "U9", Channel: "C1", Text: "hello team", TimeStamp: "1710000000.000100",
	})

	assert.ElementsMatch(t, []string{"Ony:hello team", "Quiet:hello team"}, routedTo,
		"only agents whose allowFrom matches, and that were enabled before the message, receive it")
	assert.Equal(t, []string{"hello team"}, userSessionTexts(t, "Ony", "slack:C1"))
	assert.Empty(t, userSessionTexts(t, "Coder", "slack:C1"), "group transcript is limited to allowed groups")
	assert.Empty(t, userSessionTexts(t, "Late", "slack:C1"), "group transcript respects the enable time")
	assert.Empty(t, userSessionTexts(t, "Quiet", "slack:C1:1710000000.000100"), "history disabled")
}

func boolPtr(v bool) *bool { return &v }

func TestManager_SendThreadAndMediaOnConfiguredChannel(t *testing.T) {
	server, calls := newFakeSlackAPI(t, map[string]string{"/chat.postMessage": slackPostOK})
	slackCh := newTestSlackChannel(server.URL)
	media := &mockMediaSender{}
	plain := &mockChannel{}

	mgr := NewManager()
	mgr.channels[channelKey("bot", "slack", "UBOT")] = slackCh
	mgr.channels[channelKey("bot", "signal", "+1")] = media
	mgr.channels[channelKey("bot", "discord", "d")] = plain

	tests := []struct {
		name    string
		call    func() error
		wantErr string
	}{
		{"thread on missing channel", func() error {
			return mgr.SendThreadOnConfiguredChannel("bot", "slack", "nope", "C1", "1.0", "x")
		}, "not active"},
		{"thread unsupported", func() error {
			return mgr.SendThreadOnConfiguredChannel("bot", "discord", "d", "C1", "1.0", "x")
		}, "does not support threaded delivery"},
		{"thread delivered", func() error {
			return mgr.SendThreadOnConfiguredChannel("bot", "slack", "UBOT", "C123", "1710000000.000100", "in thread")
		}, ""},
		{"media on missing channel", func() error {
			return mgr.SendMediaOnConfiguredChannel("bot", "signal", "nope", "+2", "cap", "/f.png")
		}, "not active"},
		{"media unsupported", func() error {
			return mgr.SendMediaOnConfiguredChannel("bot", "discord", "d", "+2", "cap", "/f.png")
		}, "does not support media delivery"},
		{"media delivered", func() error {
			return mgr.SendMediaOnConfiguredChannel("bot", "signal", "+1", "+2", "cap", "/f.png")
		}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}

	posts := callsTo(calls(), "/chat.postMessage")
	require.Len(t, posts, 1)
	assert.Equal(t, "1710000000.000100", posts[0].Form.Get("thread_ts"))
	assert.Equal(t, "in thread", posts[0].Form.Get("text"))
	assert.Equal(t, []mediaCall{{"+2", "cap", "/f.png"}}, media.mediaCalls)
}

type failingMediaSender struct{ mockChannel }

func (f *failingMediaSender) SendMedia(string, string, string) error {
	return fmt.Errorf("upload rejected")
}

func TestRouteMediaDelivery_ReturnsLastError(t *testing.T) {
	mgr := NewManager()
	mgr.channels[channelKey("bot", "signal", "+1")] = &failingMediaSender{}

	err := mgr.RouteMediaDelivery("signal", "+2", "cap", "/f.png")

	require.Error(t, err)
	assert.Equal(t, "upload rejected", err.Error())
}

func TestChannelSessionName(t *testing.T) {
	sep := config.ChannelConfig{SeparateTopLevelSessions: boolPtr(true)}
	tests := []struct {
		name string
		cc   config.ChannelConfig
		msg  IncomingMessage
		want string
	}{
		{"non-slack ignores thread", sep, IncomingMessage{Type: "signal", Channel: "G1", ThreadTS: "1.0"}, "signal:G1"},
		{"slack without thread", sep, IncomingMessage{Type: "slack", Channel: "C1", ThreadTS: " "}, "slack:C1"},
		{"slack thread shares channel session by default", config.ChannelConfig{}, IncomingMessage{Type: "slack", Channel: "C1", ThreadTS: "1.0"}, "slack:C1"},
		{"slack thread with separate sessions", sep, IncomingMessage{Type: "slack", Channel: "C1", ThreadTS: " 1.0 "}, "slack:C1:1.0"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, channelSessionName(tt.cc, tt.msg), tt.name)
	}
}

func TestSlackRoutingHelpers(t *testing.T) {
	disabled := false
	assert.False(t, matchesAnyAllowedGroup([]config.AllowFromEntry{
		{AllowedGroups: "C1", Enabled: &disabled},
		{AllowedGroups: "C2"},
	}, "C1"), "disabled entries do not grant groups")
	assert.True(t, matchesAnyAllowedGroup([]config.AllowFromEntry{{AllowedGroups: "C2"}}, "C2"))

	assert.False(t, anySlackStatusEnabled([]channelSpec{
		{channelConfig: config.ChannelConfig{ShowTyping: &disabled}},
	}))
	assert.True(t, anySlackStatusEnabled([]channelSpec{
		{channelConfig: config.ChannelConfig{ShowTyping: &disabled}},
		{channelConfig: config.ChannelConfig{}},
	}))
}

func TestRoutedSlackMessage_FallbacksAndThreadContinuation(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	spec := channelSpec{
		agentName: "Ony",
		channelConfig: config.ChannelConfig{
			Type: "slack", ID: "UCFG",
			AllowFrom: []config.AllowFromEntry{{From: "*", AllowedGroups: "C1", RespondToMentions: true}},
		},
		agentModel:     "agent/model",
		agentFallbacks: []string{"agent/fallback"},
	}

	routed, ok := routedSlackMessage(ch, spec, IncomingMessage{From: "U1", Channel: "C1", Text: "<@UCFG> hi"})
	require.True(t, ok, "the configured ID stands in for an unknown bot user ID")
	assert.Equal(t, "agent/model", routed.Model)
	assert.Equal(t, []string{"agent/fallback"}, routed.Fallbacks)

	_, ok = routedSlackMessage(ch, spec, IncomingMessage{From: "U1", Channel: "C1", Text: "no mention"})
	assert.False(t, ok)

	spec.channelConfig.Fallbacks = []string{"channel/fallback"}
	routed, ok = routedSlackMessage(ch, spec, IncomingMessage{From: "U1", Channel: "C1", Text: "no mention", IsThreadReply: true})
	require.True(t, ok, "a thread reply continues an allowed conversation without a new mention")
	assert.Equal(t, []string{"channel/fallback"}, routed.Fallbacks)
}

func TestResolveChannelAuthRefs_MissingCredentials(t *testing.T) {
	useTempDataDir(t)
	for _, cc := range []config.ChannelConfig{
		{Token: "auth:missing_token"},
		{Token: "plain", URL: "auth:missing_url"},
	} {
		_, err := resolveChannelAuthRefs(cc)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "auth:missing_")
	}
}

func TestShouldProcessIncomingMessage_DefaultsAndMissingState(t *testing.T) {
	assert.True(t, shouldProcessIncomingMessage(store.ChannelMetadata{}, IncomingMessage{ReceivedAt: time.Unix(0, 0)}),
		"channels without an enable time accept everything")
	assert.True(t, shouldProcessIncomingMessage(store.ChannelMetadata{EnabledAt: time.Now().Add(-time.Hour)}, IncomingMessage{}),
		"a message without a timestamp counts as received now")
	assert.False(t, shouldProcessIncomingMessage(store.ChannelMetadata{EnabledAt: time.Now().Add(time.Hour)}, IncomingMessage{}))

	assert.Equal(t, store.ChannelMetadata{}, channelMetadata(nil, "k"))
	assert.Equal(t, store.ChannelMetadata{}, channelMetadata(&store.AppState{}, "k"))
}

func TestNewChannel_DiscordAndSlackOptions(t *testing.T) {
	off := false
	discord := newChannel(config.ChannelConfig{Type: "discord", Token: "t", ShowTyping: &off, DisabledTools: []string{"exec"}}, "m", nil)
	require.IsType(t, &DiscordChannel{}, discord)
	assert.Equal(t, []string{"exec"}, discord.(*DiscordChannel).disabledTools)

	slackCh := newChannel(config.ChannelConfig{Type: "slack", Token: "xoxb", URL: "xapp", ShowTyping: &off}, "m", nil)
	require.IsType(t, &SlackChannel{}, slackCh)
	assert.False(t, slackCh.(*SlackChannel).ShowAssistantStatus())
}
