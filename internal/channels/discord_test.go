package channels

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
)

// newDiscordRESTSession returns a discordgo session whose REST endpoints point
// at an httptest server running handler.
func newDiscordRESTSession(t *testing.T, handler http.HandlerFunc) *discordgo.Session {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Cleanup(overrideDiscordEndpointsForTest(srv.URL + "/"))
	session, err := discordgo.New("Bot test-token")
	require.NoError(t, err)
	session.MaxRestRetries = 0
	return session
}

func discordAPIError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"message":"Missing Access","code":50001}`))
}

func newDiscordLoggedChannel(allow []config.AllowFromEntry) (*DiscordChannel, *LogSink) {
	ch := NewDiscordChannel("token123", allow, "default-model", []string{"default-fallback"})
	sink := newLogSink()
	ch.SetLogSink(sink)
	return ch, sink
}

func discordLogHistory(sink *LogSink) string {
	history, _, unsub := sink.Subscribe()
	unsub()
	return strings.Join(history, "\n")
}

func TestDiscordChannel_SendPostsMessage(t *testing.T) {
	var gotPath, gotBody string
	session := newDiscordRESTSession(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotPath = r.Method + " " + r.URL.Path
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"M1"}`))
	})
	ch := NewDiscordChannel("token123", nil, "m", nil)
	ch.session = session

	require.NoError(t, ch.Send("C9", "hello"))
	assert.Equal(t, "POST /api/v"+discordgo.APIVersion+"/channels/C9/messages", gotPath)
	assert.Contains(t, gotBody, `"content":"hello"`)
}

func TestDiscordChannel_SendAPIError(t *testing.T) {
	session := newDiscordRESTSession(t, func(w http.ResponseWriter, _ *http.Request) { discordAPIError(w) })
	ch := NewDiscordChannel("token123", nil, "m", nil)
	ch.session = session

	err := ch.Send("C9", "hello")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Missing Access")

	id, err := ch.SendAndGetID("C9", "hello")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Missing Access")
	assert.Empty(t, id)

	path := filepath.Join(t.TempDir(), "note.txt")
	require.NoError(t, os.WriteFile(path, []byte("hi"), 0o600))
	err = ch.SendMedia("C9", "caption", path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Missing Access")
}

func TestDiscordChannel_SendMediaValidatesFile(t *testing.T) {
	requests := 0
	session := newDiscordRESTSession(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		discordAPIError(w)
	})
	ch := NewDiscordChannel("token123", nil, "m", nil)
	ch.session = session

	err := ch.SendMedia("C9", "caption", "   ")
	require.EqualError(t, err, "discord: file path is required")

	err = ch.SendMedia("C9", "caption", filepath.Join(t.TempDir(), "missing.png"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist), "got %v", err)
	assert.Zero(t, requests, "no upload should be attempted for an invalid file")
}

func TestDiscordChannel_BotUserID(t *testing.T) {
	withState := func(user *discordgo.User) *discordgo.Session {
		s := &discordgo.Session{State: discordgo.NewState()}
		s.State.User = user
		return s
	}
	tests := []struct {
		name    string
		session *discordgo.Session
		want    string
	}{
		{name: "nil session"},
		{name: "nil state", session: &discordgo.Session{}},
		{name: "nil user", session: withState(nil)},
		{name: "ready user", session: withState(&discordgo.User{ID: "BOT1"}), want: "BOT1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := NewDiscordChannel("t", nil, "m", nil)
			ch.session = tt.session
			assert.Equal(t, tt.want, ch.discordBotUserID())
		})
	}
}

func TestDiscordUpdatedMessage(t *testing.T) {
	before := &discordgo.Message{
		Author:    &discordgo.User{ID: "U-before"},
		ChannelID: "C-before",
		GuildID:   "G-before",
	}
	tests := []struct {
		name                       string
		update                     *discordgo.MessageUpdate
		author, channelID, guildID string
	}{
		{
			name:   "nil message without cache",
			update: &discordgo.MessageUpdate{},
		},
		{
			name:      "nil message backfilled from cache",
			update:    &discordgo.MessageUpdate{BeforeUpdate: before},
			author:    "U-before",
			channelID: "C-before",
			guildID:   "G-before",
		},
		{
			name: "partial message backfilled from cache",
			update: &discordgo.MessageUpdate{
				Message:      &discordgo.Message{ID: "M1", Content: "edited"},
				BeforeUpdate: before,
			},
			author:    "U-before",
			channelID: "C-before",
			guildID:   "G-before",
		},
		{
			name: "complete message keeps its own fields",
			update: &discordgo.MessageUpdate{
				Message: &discordgo.Message{
					Author:    &discordgo.User{ID: "U-new"},
					ChannelID: "C-new",
					GuildID:   "G-new",
				},
				BeforeUpdate: before,
			},
			author:    "U-new",
			channelID: "C-new",
			guildID:   "G-new",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := discordUpdatedMessage(tt.update)
			require.NotNil(t, msg)
			if tt.author == "" {
				assert.Nil(t, msg.Author)
			} else {
				require.NotNil(t, msg.Author)
				assert.Equal(t, tt.author, msg.Author.ID)
			}
			assert.Equal(t, tt.channelID, msg.ChannelID)
			assert.Equal(t, tt.guildID, msg.GuildID)
		})
	}
}

func TestDiscordChannel_GatewayHandlersDeliverMessages(t *testing.T) {
	ch := NewDiscordChannel("t", []config.AllowFromEntry{{
		From:              "*",
		AllowedGroups:     "*",
		RespondToMentions: true,
	}}, "m", nil)
	ch.session = &discordgo.Session{State: discordgo.NewState()}
	ch.session.State.User = &discordgo.User{ID: "BOT1"}
	msgs := make(chan IncomingMessage, 2)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	ch.onMessageCreate(nil, nil)
	ch.onMessageUpdate(nil, nil)

	ch.onMessageCreate(nil, &discordgo.MessageCreate{Message: &discordgo.Message{
		ID:        "M1",
		Author:    &discordgo.User{ID: "U1"},
		ChannelID: "C1",
		GuildID:   "G1",
		Content:   "hello",
		Mentions:  []*discordgo.User{{ID: "BOT1"}},
	}})
	created := waitMsg(t, msgs, time.Second)
	assert.Equal(t, "U1", created.From)
	assert.Equal(t, "hello", created.Text)

	// A partial edit relies on the cached message for its author, channel,
	// and guild, and on the session's bot user for the mention check.
	ch.onMessageUpdate(nil, &discordgo.MessageUpdate{
		Message: &discordgo.Message{
			ID:       "M1",
			Content:  "hello again",
			Mentions: []*discordgo.User{{ID: "BOT1"}},
		},
		BeforeUpdate: &discordgo.Message{
			Author:    &discordgo.User{ID: "U1"},
			ChannelID: "C1",
			GuildID:   "G1",
		},
	})
	updated := waitMsg(t, msgs, time.Second)
	assert.Equal(t, "U1", updated.From)
	assert.Equal(t, "C1", updated.Channel)
	assert.Equal(t, "hello again", updated.Text)

	select {
	case extra := <-msgs:
		t.Fatalf("unexpected message: %+v", extra)
	default:
	}
}

func TestDiscordChannel_StartOpenFailure(t *testing.T) {
	ch, sink := newDiscordLoggedChannel(nil)
	ch.openSession = func(*discordgo.Session) error { return errors.New("gateway down") }

	err := ch.Start(context.Background())
	require.EqualError(t, err, "discord: opening session: gateway down")
	require.NotNil(t, ch.session)
	assert.Equal(t, discordgo.IntentsGuildMessages|discordgo.IntentsDirectMessages|discordgo.IntentsMessageContent, ch.session.Identify.Intents)
	assert.Contains(t, discordLogHistory(sink), "discord: opening session failed: gateway down")
}

func TestDiscordChannel_StartRealOpenFailsAgainstGateway(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"401: Unauthorized","code":0}`))
	}))
	defer srv.Close()
	defer overrideDiscordEndpointsForTest(srv.URL + "/")()

	ch := NewDiscordChannel("bad-token", nil, "m", nil)
	err := ch.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "discord: opening session:")
	assert.NotEmpty(t, paths, "Open should query the gateway endpoint")
}

func TestDiscordChannel_StartReturnsOnContextCancel(t *testing.T) {
	ch, sink := newDiscordLoggedChannel(nil)
	opened := make(chan struct{})
	ch.openSession = func(*discordgo.Session) error {
		close(opened)
		return nil
	}
	// The default close is used: closing a session that never connected is a
	// no-op that succeeds.
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- ch.Start(ctx) }()

	<-opened
	cancel()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancellation")
	}
	logs := discordLogHistory(sink)
	assert.Contains(t, logs, "discord: session connected")
	assert.Contains(t, logs, "discord: context cancelled")
	assert.Contains(t, logs, "discord: session closed")
}

func TestDiscordChannel_StartReturnsOnStopWithCloseError(t *testing.T) {
	ch, sink := newDiscordLoggedChannel(nil)
	opened := make(chan struct{})
	ch.openSession = func(*discordgo.Session) error {
		close(opened)
		return nil
	}
	var closed *discordgo.Session
	ch.closeSession = func(s *discordgo.Session) error {
		closed = s
		return errors.New("close boom")
	}
	errCh := make(chan error, 1)
	go func() { errCh <- ch.Start(context.Background()) }()

	<-opened
	ch.Stop()
	select {
	case err := <-errCh:
		require.EqualError(t, err, "close boom")
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
	assert.Same(t, ch.session, closed)
	logs := discordLogHistory(sink)
	assert.Contains(t, logs, "discord: stop requested")
	assert.Contains(t, logs, "discord: close failed: close boom")
}

func TestDiscordChannel_HandleMessageIgnoresUnusableMessages(t *testing.T) {
	ch := NewDiscordChannel("t", []config.AllowFromEntry{{From: "*"}}, "m", nil)
	calls := 0
	ch.OnMessage(func(IncomingMessage) { calls++ })

	for name, msg := range map[string]*discordgo.Message{
		"nil":       nil,
		"no author": {Content: "hi"},
		"bot":       {Author: &discordgo.User{ID: "B", Bot: true}, Content: "hi"},
		"empty":     {Author: &discordgo.User{ID: "U"}, Content: "   "},
	} {
		assert.False(t, ch.handleMessage(msg, ""), name)
	}

	dm := &discordgo.Message{ID: "M1", Author: &discordgo.User{ID: "U"}, ChannelID: "D1", Content: "hi"}
	assert.True(t, ch.handleMessage(dm, ""))
	assert.False(t, ch.handleMessage(dm, ""), "a replayed message must not start another run")
	assert.Equal(t, 1, calls)
}

func TestDiscordChannel_HandleMessageFields(t *testing.T) {
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 3600))
	tests := []struct {
		name          string
		allow         config.AllowFromEntry
		author        *discordgo.User
		timestamp     time.Time
		wantSender    string
		wantModel     string
		wantFallbacks []string
		wantRestrict  []string
	}{
		{
			name:          "channel defaults and username",
			allow:         config.AllowFromEntry{From: "*"},
			author:        &discordgo.User{ID: "U1", Username: " user1 "},
			timestamp:     stamp,
			wantSender:    "user1",
			wantModel:     "default-model",
			wantFallbacks: []string{"default-fallback"},
		},
		{
			name: "allowFrom overrides and global name",
			allow: config.AllowFromEntry{
				From:          "U1",
				Model:         "entry-model",
				Fallbacks:     []string{"entry-fallback"},
				RestrictTools: []string{"web_search"},
			},
			author:        &discordgo.User{ID: "U1", Username: "user1", GlobalName: " Global One "},
			wantSender:    "Global One",
			wantModel:     "entry-model",
			wantFallbacks: []string{"entry-fallback"},
			wantRestrict:  []string{"web_search"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, _ := newDiscordLoggedChannel([]config.AllowFromEntry{tt.allow})
			ch.disabledTools = []string{"shell"}
			msgs := make(chan IncomingMessage, 1)
			ch.OnMessage(func(m IncomingMessage) { msgs <- m })

			before := time.Now().UTC()
			require.True(t, ch.handleMessage(&discordgo.Message{
				ID:        "M1",
				Author:    tt.author,
				ChannelID: "D1",
				Content:   "hello",
				Timestamp: tt.timestamp,
			}, "BOT1"))
			msg := waitMsg(t, msgs, time.Second)

			assert.Equal(t, "discord", msg.Type)
			assert.Equal(t, tt.wantSender, msg.SenderName)
			assert.Equal(t, tt.wantModel, msg.Model)
			assert.Equal(t, tt.wantFallbacks, msg.Fallbacks)
			assert.Equal(t, tt.wantRestrict, msg.RestrictTools)
			assert.Equal(t, []string{"shell"}, msg.DisabledTools)
			assert.Empty(t, msg.Attachments)
			if tt.timestamp.IsZero() {
				assert.False(t, msg.ReceivedAt.Before(before))
			} else {
				assert.Equal(t, stamp.UTC(), msg.ReceivedAt)
				assert.Equal(t, time.UTC, msg.ReceivedAt.Location())
			}
		})
	}
}

func TestDiscordChannel_HandleMessageGroupLogAndFiltering(t *testing.T) {
	ch, sink := newDiscordLoggedChannel([]config.AllowFromEntry{{From: "U-allowed", AllowedGroups: "*"}})
	var logged []IncomingMessage
	ch.OnGroupChatMessage(func(m IncomingMessage) { logged = append(logged, m) })
	handled := 0
	ch.OnMessage(func(IncomingMessage) { handled++ })

	ok := ch.handleMessage(&discordgo.Message{
		ID:        "M1",
		Author:    &discordgo.User{ID: "U-other", Username: "other"},
		ChannelID: "C1",
		GuildID:   "G1",
		Content:   "chatter",
	}, "BOT1")
	assert.False(t, ok)
	assert.Zero(t, handled)
	require.Len(t, logged, 1, "group messages are logged before allowFrom filtering")
	assert.Equal(t, "U-other", logged[0].From)
	assert.Equal(t, "other", logged[0].SenderName)
	assert.Equal(t, "C1", logged[0].Channel)
	assert.Equal(t, "chatter", logged[0].Text)
	assert.Contains(t, discordLogHistory(sink), "discord: ignored message from=U-other channel=C1")

	// Direct messages are not group-logged.
	ch.handleMessage(&discordgo.Message{
		ID:        "M2",
		Author:    &discordgo.User{ID: "U-allowed"},
		ChannelID: "D1",
		Content:   "dm",
	}, "BOT1")
	assert.Len(t, logged, 1)
	assert.Equal(t, 1, handled)
}

func TestDiscordChannel_HandleMessageWithoutHandler(t *testing.T) {
	ch, sink := newDiscordLoggedChannel([]config.AllowFromEntry{{From: "*"}})
	ok := ch.handleMessage(&discordgo.Message{
		ID:        "M1",
		Author:    &discordgo.User{ID: "U1"},
		ChannelID: "D1",
		Content:   "hello",
	}, "")
	assert.True(t, ok, "an allowed message is accepted even with no handler")
	assert.Contains(t, discordLogHistory(sink), "discord: no message handler registered for from=U1")
}

func TestDiscordAttachments(t *testing.T) {
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing.txt" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("proxy-bytes"))
	}))
	defer srv.Close()

	got := discordAttachments([]*discordgo.MessageAttachment{
		nil,
		{Filename: "no-url.txt"},
		{Filename: "proxied.txt", ContentType: "text/plain", ProxyURL: srv.URL + "/proxied.txt"},
		{Filename: "missing.txt", ContentType: "text/plain", Size: 42, URL: srv.URL + "/missing.txt"},
	})
	require.Len(t, got, 2, "nil attachments and attachments without a URL are skipped")

	assert.Equal(t, "proxied.txt", got[0].Name)
	assert.Empty(t, got[0].Err)
	assert.NotEmpty(t, got[0].Path)

	assert.Equal(t, "missing.txt", got[1].Name)
	assert.Equal(t, int64(42), got[1].Size)
	assert.Contains(t, got[1].Err, "404")

	assert.Nil(t, discordAttachments(nil))
}

func TestDiscordMentionsUser(t *testing.T) {
	mentions := []*discordgo.User{nil, {ID: "U1"}, {ID: "BOT1"}}
	assert.False(t, discordMentionsUser(mentions, ""), "an unknown bot user is never mentioned")
	assert.True(t, discordMentionsUser(mentions, "BOT1"))
	assert.False(t, discordMentionsUser(mentions, "BOT2"))
	assert.False(t, discordMentionsUser(nil, "BOT1"))
}
