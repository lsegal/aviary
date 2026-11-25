package channels

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
)

// slackAPICall records one request made to a fake Slack Web API.
type slackAPICall struct {
	Path string
	Form url.Values
}

// newFakeSlackAPI serves canned JSON responses keyed by Web API path (for
// example "/chat.postMessage"). Unknown paths answer unknown_method.
func newFakeSlackAPI(t *testing.T, routes map[string]string) (*httptest.Server, func() []slackAPICall) {
	t.Helper()
	var (
		mu    sync.Mutex
		calls []slackAPICall
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		calls = append(calls, slackAPICall{Path: r.URL.Path, Form: r.Form})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		body, ok := routes[r.URL.Path]
		if !ok {
			body = `{"ok":false,"error":"unknown_method"}`
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, func() []slackAPICall {
		mu.Lock()
		defer mu.Unlock()
		return append([]slackAPICall{}, calls...)
	}
}

// callsTo filters recorded calls to one API path.
func callsTo(calls []slackAPICall, path string) []slackAPICall {
	var out []slackAPICall
	for _, c := range calls {
		if c.Path == path {
			out = append(out, c)
		}
	}
	return out
}

func sinkHistory(sink *LogSink) string {
	history, _, unsub := sink.Subscribe()
	unsub()
	return strings.Join(history, "\n")
}

const (
	slackPostOK   = `{"ok":true,"channel":"C123","ts":"1710000009.000900"}`
	slackUpdateOK = `{"ok":true,"channel":"C123","ts":"1710000001.000100","text":"x"}`
)

func TestSlackChannel_PostAndEditResolveTargetAndReturnTimestamp(t *testing.T) {
	block := slack.NewSectionBlock(slack.NewTextBlockObject("mrkdwn", "block body", false, false), nil, nil)
	tests := []struct {
		name     string
		call     func(*SlackChannel) (string, error)
		path     string
		wantTS   string
		wantForm map[string]string
		blocks   bool
	}{
		{
			name: "SendAndGetID",
			call: func(c *SlackChannel) (string, error) { return c.SendAndGetID("#alerts", "hello") },
			path: "/chat.postMessage", wantTS: "1710000009.000900",
			wantForm: map[string]string{"channel": "C123", "text": "hello"},
		},
		{
			name: "SendThreadBlocksAndGetID",
			call: func(c *SlackChannel) (string, error) {
				return c.SendThreadBlocksAndGetID("#alerts", " 1710000000.000100 ", "fallback", block)
			},
			path: "/chat.postMessage", wantTS: "1710000009.000900", blocks: true,
			wantForm: map[string]string{"channel": "C123", "text": "fallback", "thread_ts": "1710000000.000100"},
		},
		{
			name: "EditMessage",
			call: func(c *SlackChannel) (string, error) { return "", c.EditMessage("#alerts", "1710000001.000100", "edited") },
			path: "/chat.update",
			wantForm: map[string]string{"channel": "C123", "ts": "1710000001.000100", "text": "edited"},
		},
		{
			name: "EditMessageBlocks",
			call: func(c *SlackChannel) (string, error) {
				return "", c.EditMessageBlocks("#alerts", "1710000001.000100", "fallback", block)
			},
			path: "/chat.update", blocks: true,
			wantForm: map[string]string{"channel": "C123", "ts": "1710000001.000100", "text": "fallback"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, calls := newFakeSlackAPI(t, map[string]string{
				"/chat.postMessage": slackPostOK,
				"/chat.update":      slackUpdateOK,
			})
			ch := newTestSlackChannel(server.URL)
			ch.channelAliases = map[string]string{"alerts": "C123"}

			ts, err := tt.call(ch)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTS, ts)

			got := callsTo(calls(), tt.path)
			require.Len(t, got, 1)
			for k, v := range tt.wantForm {
				assert.Equal(t, v, got[0].Form.Get(k), k)
			}
			if tt.blocks {
				assert.Contains(t, got[0].Form.Get("blocks"), "block body")
			} else {
				assert.Empty(t, got[0].Form.Get("blocks"))
			}
		})
	}
}

func TestSlackChannel_SendMethodsRejectMissingTargetOrThread(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	tests := []struct {
		name    string
		call    func() error
		wantErr string
	}{
		{"Send no target", func() error { return ch.Send(" ", "x") }, "delivery target is required"},
		{"SendAndGetID no target", func() error { _, err := ch.SendAndGetID("", "x"); return err }, "delivery target is required"},
		{"SendThreadMessageAndGetID no target", func() error { _, err := ch.SendThreadMessageAndGetID("", "1.0", "x"); return err }, "delivery target is required"},
		{"SendThreadMessageAndGetID no thread", func() error { _, err := ch.SendThreadMessageAndGetID("C1", " ", "x"); return err }, "thread timestamp is required"},
		{"SendThreadBlocksAndGetID no target", func() error { _, err := ch.SendThreadBlocksAndGetID("", "1.0", "x"); return err }, "delivery target is required"},
		{"SendThreadBlocksAndGetID no thread", func() error { _, err := ch.SendThreadBlocksAndGetID("C1", "", "x"); return err }, "thread timestamp is required"},
		{"EditMessage no target", func() error { return ch.EditMessage("", "1.0", "x") }, "delivery target is required"},
		{"EditMessageBlocks no target", func() error { return ch.EditMessageBlocks("", "1.0", "x") }, "delivery target is required"},
		{"SetAssistantProcessing no target", func() error { return ch.SetAssistantProcessing("", "1.0", "U1", true) }, "delivery target is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSlackChannel_SetAssistantProcessing_APIErrorAndWarning(t *testing.T) {
	t.Run("api error is returned", func(t *testing.T) {
		server, _ := newFakeSlackAPI(t, map[string]string{
			"/agents.sessions.setStatus": `{"ok":false,"error":"thread_not_found"}`,
		})
		ch := newTestSlackChannel(server.URL)
		err := ch.SetAssistantProcessing("C123", "1.0001", "U1", true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "thread_not_found")
	})
	t.Run("warning is logged", func(t *testing.T) {
		server, _ := newFakeSlackAPI(t, map[string]string{
			"/agents.sessions.setStatus": `{"ok":true,"status":"active","warning":"superfluous_status"}`,
		})
		ch := newTestSlackChannel(server.URL)
		sink := newLogSink()
		ch.SetLogSink(sink)
		require.NoError(t, ch.SetAssistantProcessing("C123", "1.0001", "", false))
		assert.Contains(t, sinkHistory(sink), "warning=superfluous_status")
	})
}

func TestSlackChannel_AgentSessionStopped_EdgeCases(t *testing.T) {
	server, calls := newFakeSlackAPI(t, map[string]string{
		"/agents.sessions.setStatus": `{"ok":false,"error":"not_allowed"}`,
	})
	ch := newTestSlackChannel(server.URL)
	sink := newLogSink()
	ch.SetLogSink(sink)

	ch.handleAgentSessionStopped(nil)
	ch.handleAgentSessionStopped(&slackevents.AgentSessionStoppedEvent{Channel: "D123"})
	assert.Empty(t, calls(), "a nil event or one without a thread must not touch the session status")

	ch.handleAgentSessionStopped(&slackevents.AgentSessionStoppedEvent{Channel: "D123", ThreadTimestamp: "1.0001"})
	assert.Len(t, callsTo(calls(), "/agents.sessions.setStatus"), 1)
	assert.Contains(t, sinkHistory(sink), "failed to clear agent session status after stop")
}

func newStartTestSlackChannel(serverURL string, allowFrom []config.AllowFromEntry) *SlackChannel {
	ch := NewSlackChannel("xapp-token", "xoxb-token", allowFrom, "m", nil)
	ch.client = slack.New("xoxb-token", slack.OptionAPIURL(serverURL+"/"), slack.OptionAppLevelToken("xapp-token"))
	ch.sm = socketmode.New(ch.client)
	return ch
}

const (
	slackUsersListOK = `{"ok":true,"members":[` +
		`{"id":"U1","name":"alice","real_name":"Alice Smith","profile":{"display_name":"Ali","display_name_normalized":"ali","real_name_normalized":"Alice Smith"}},` +
		`{"id":"U2","name":"bob","real_name":"Bob Jones","profile":{}},` +
		`{"id":"U3","name":"gone","deleted":true,"profile":{"display_name":"Gone"}},` +
		`{"id":"","name":"blank"}],"response_metadata":{"next_cursor":""}}`
	slackConversationsListOK = `{"ok":true,"channels":[` +
		`{"id":"C1","name":"general","name_normalized":"general"},` +
		`{"id":"","name":"blank"}],"response_metadata":{"next_cursor":""}}`
	slackInvalidAuth = `{"ok":false,"error":"invalid_auth"}`
)

func TestSlackChannel_StartLoadsIdentityAndReturnsSocketModeError(t *testing.T) {
	server, _ := newFakeSlackAPI(t, map[string]string{
		"/auth.test":            `{"ok":true,"user_id":"UBOT","team":" Team "}`,
		"/users.list":           slackUsersListOK,
		"/conversations.list":   slackConversationsListOK,
		"/apps.connections.open": slackInvalidAuth,
	})
	ch := newStartTestSlackChannel(server.URL, []config.AllowFromEntry{{From: "@alice", AllowedGroups: "#general"}})
	sink := newLogSink()
	ch.SetLogSink(sink)

	err := ch.Start(context.Background())
	defer ch.Stop()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_auth")
	assert.Equal(t, "UBOT", ch.botUserID)
	assert.Equal(t, "Ali", ch.displayNameForUser("U1"))
	assert.Equal(t, "Bob Jones", ch.displayNameForUser("U2"))
	assert.Equal(t, "U3", ch.displayNameForUser("U3"), "deleted users are not cached")
	assert.Equal(t, "general", ch.channelNames["C1"])
	assert.Equal(t, []config.AllowFromEntry{{From: "@alice,U1", AllowedGroups: "#general,C1"}}, ch.allowedEntries())

	logs := sinkHistory(sink)
	assert.Contains(t, logs, "auth ok user_id=UBOT team=Team")
	assert.Contains(t, logs, "identity cache ready")
	assert.Contains(t, logs, "socket mode exited with error: invalid_auth")
}

func TestSlackChannel_StartContinuesWhenAuthAndDirectoryFail(t *testing.T) {
	server, _ := newFakeSlackAPI(t, map[string]string{
		"/auth.test":             slackInvalidAuth,
		"/users.list":            `{"ok":false,"error":"missing_scope"}`,
		"/apps.connections.open": slackInvalidAuth,
	})
	ch := newStartTestSlackChannel(server.URL, []config.AllowFromEntry{{From: "@alice"}})
	sink := newLogSink()
	ch.SetLogSink(sink)

	err := ch.Start(context.Background())
	defer ch.Stop()

	require.Error(t, err)
	assert.Empty(t, ch.botUserID)
	assert.Equal(t, []config.AllowFromEntry{{From: "@alice"}}, ch.allowedEntries(), "unresolved allowFrom falls back to the configured entries")
	logs := sinkHistory(sink)
	assert.Contains(t, logs, "auth.test failed")
	assert.Contains(t, logs, "failed to refresh users/channels: users.list: missing_scope")
}

func TestSlackChannel_RefreshIdentityCache_ConversationsError(t *testing.T) {
	server, _ := newFakeSlackAPI(t, map[string]string{
		"/users.list":         slackUsersListOK,
		"/conversations.list": `{"ok":false,"error":"missing_scope"}`,
	})
	ch := newTestSlackChannel(server.URL)

	err := ch.refreshIdentityCache(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "conversations.list: missing_scope")
	assert.Nil(t, ch.userAliases, "a partial refresh must not replace the cache")
}

func TestSlackChannel_DispatchLogsConnectionEventsAndQueuesEventsAPI(t *testing.T) {
	tests := []struct {
		typ     socketmode.EventType
		wantLog string
	}{
		{socketmode.EventTypeConnecting, "slack: connecting"},
		{socketmode.EventTypeConnected, "slack: connected"},
		{socketmode.EventTypeConnectionError, "slack: connection error"},
		{socketmode.EventTypeInvalidAuth, "slack: invalid auth"},
		{socketmode.EventTypeDisconnect, "slack: disconnected"},
	}
	for _, tt := range tests {
		t.Run(string(tt.typ), func(t *testing.T) {
			ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
			sink := newLogSink()
			ch.SetLogSink(sink)
			assert.Nil(t, ch.dispatch(socketmode.Event{Type: tt.typ}))
			assert.Contains(t, sinkHistory(sink), tt.wantLog)
		})
	}

	t.Run("events api with unexpected data", func(t *testing.T) {
		ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
		fn := ch.dispatch(socketmode.Event{
			Type:    socketmode.EventTypeEventsAPI,
			Request: &socketmode.Request{EnvelopeID: "env-1"},
			Data:    "not an events api payload",
		})
		assert.Nil(t, fn)
	})

	t.Run("agent session stopped is acked and handled", func(t *testing.T) {
		server, calls := newSessionStatusServer(t)
		ch := newTestSlackChannel(server.URL)
		stopped := 0
		ch.RegisterRunStop("D123", "1710000000.123456", func() { stopped++ })
		evt := parseSlackEvent(t, `{"type":"event_callback","team_id":"T1","api_app_id":"A1","event_id":"EvStop",`+
			`"event":{"type":"agent_session_stopped","channel":"D123","thread_ts":"1710000000.123456","user":"U123","event_ts":"1710000002.000200"}}`)

		fn := ch.dispatch(socketmode.Event{
			Type:    socketmode.EventTypeEventsAPI,
			Request: &socketmode.Request{EnvelopeID: "env-1"},
			Data:    evt,
		})
		require.NotNil(t, fn)
		assert.Zero(t, stopped, "handling is deferred to the returned work")
		fn()
		assert.Equal(t, 1, stopped)
		assert.Equal(t, []sessionStatusCall{{Channel: "D123", Thread: "1710000000.123456", Status: "active"}}, calls())
	})
}

func TestSlackChannel_HandleEventsAPI_AppMention(t *testing.T) {
	ch, messages := newDedupeTestSlackChannel(t)
	evt := parseSlackEvent(t, `{"type":"event_callback","team_id":"T1","api_app_id":"A1","event_id":"EvMention",`+
		`"event":{"type":"app_mention","channel":"C123","user":"U123","text":"<@UBOT> status?","ts":"1710000001.000100","event_ts":"1710000001.000100"}}`)

	ch.handleEventsAPI(nil, evt)

	got := messages()
	require.Len(t, got, 1)
	assert.Equal(t, "C123", got[0].Channel)
	assert.Equal(t, "<@UBOT> status?", got[0].Text)
}

func TestSlackChannel_HandleMessageEvent_Ignored(t *testing.T) {
	tests := []struct {
		name  string
		event *slackevents.MessageEvent
	}{
		{"nil event", nil},
		{"message_changed without previous message or edit marker", &slackevents.MessageEvent{
			SubType: "message_changed", Channel: "C123",
			Message: &slack.Msg{User: "U123", Text: "<@UBOT> hi"},
		}},
		{"edit made by a bot", &slackevents.MessageEvent{
			SubType: "message_changed", Channel: "C123",
			Message:         &slack.Msg{User: "U123", Text: "<@UBOT> new", BotID: "B1"},
			PreviousMessage: &slack.Msg{User: "U123", Text: "<@UBOT> old"},
		}},
		{"message_replied without a message", &slackevents.MessageEvent{SubType: "message_replied", Channel: "C123"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, messages := newDedupeTestSlackChannel(t)
			ch.handleMessageEvent(tt.event)
			ch.handleAppMentionEvent(nil)
			assert.Empty(t, messages())
		})
	}
}

func TestSlackChannel_HandleMessageEvent_GroupLogAndFiltering(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", []config.AllowFromEntry{{From: "*", AllowedGroups: "CALLOWED"}}, "m", nil)
	sink := newLogSink()
	ch.SetLogSink(sink)
	var logged, delivered []IncomingMessage
	ch.OnGroupChatMessage(func(m IncomingMessage) { logged = append(logged, m) })

	// Not allowed: logged to the transcript but not delivered.
	ch.OnMessage(func(m IncomingMessage) { delivered = append(delivered, m) })
	ch.handleMessageEvent(&slackevents.MessageEvent{User: "U1", Channel: "COTHER", Text: "chatter", TimeStamp: "1710000000.000100"})
	require.Len(t, logged, 1)
	assert.Equal(t, "COTHER", logged[0].Channel)
	assert.Equal(t, "chatter", logged[0].Text)
	assert.Empty(t, delivered)
	assert.Contains(t, sinkHistory(sink), "ignored message from=U1 channel=COTHER")

	// Allowed but no handler registered: logged, then dropped.
	ch.OnMessage(nil)
	ch.handleMessageEvent(&slackevents.MessageEvent{User: "U1", Channel: "CALLOWED", Text: "hello", TimeStamp: "1710000000.000200"})
	assert.Len(t, logged, 2)
	assert.Empty(t, delivered)
	assert.Contains(t, sinkHistory(sink), "no message handler registered for from=U1")

	// DMs are never written to the group transcript.
	ch.handleMessageEvent(&slackevents.MessageEvent{User: "U1", Channel: "D1", Text: "dm", TimeStamp: "1710000000.000300"})
	assert.Len(t, logged, 2)
}

func TestSlackChannel_MessageRepliedFetchesLatestReply(t *testing.T) {
	const (
		rootTS  = "1710000000.000100"
		replyTS = "1710000005.000500"
	)
	repliesOK := `{"ok":true,"messages":[{"type":"message","user":"U123","text":"<@UBOT> follow up","ts":"` + replyTS + `","thread_ts":"` + rootTS + `"}]}`
	tests := []struct {
		name        string
		replies     string
		message     *slack.Msg
		channel     string
		wantText    string
		wantLog     string
		wantFetches int
	}{
		{
			name:    "latest_reply field",
			replies: repliesOK,
			channel: "C123",
			message: &slack.Msg{Timestamp: rootTS, ThreadTimestamp: rootTS, LatestReply: replyTS},
			wantText: "<@UBOT> follow up", wantFetches: 2,
		},
		{
			name:    "replies list fallback with mismatched ts",
			replies: repliesOK,
			channel: "C123",
			message: &slack.Msg{Timestamp: rootTS, Replies: []slack.Reply{{Timestamp: "1710000004.000400"}}},
			wantText: "<@UBOT> follow up", wantLog: "fetch returned ts=" + replyTS + " expected=1710000004.000400", wantFetches: 2,
		},
		{
			name:    "fetch error",
			replies: `{"ok":false,"error":"channel_not_found"}`,
			channel: "C123",
			message: &slack.Msg{Timestamp: rootTS, LatestReply: replyTS},
			wantLog: "failed to fetch latest thread reply", wantFetches: 1,
		},
		{
			name:    "missing channel",
			replies: repliesOK,
			message: &slack.Msg{Timestamp: rootTS, LatestReply: replyTS},
			wantLog: "ignored message_replied event with missing channel/thread/reply",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, calls := newFakeSlackAPI(t, map[string]string{"/conversations.replies": tt.replies})
			ch := newTestSlackChannel(server.URL)
			ch.allowFrom = []config.AllowFromEntry{{From: "*", AllowedGroups: "*", RespondToMentions: true}}
			ch.botUserID = "UBOT"
			sink := newLogSink()
			ch.SetLogSink(sink)
			var messages []IncomingMessage
			ch.OnMessage(func(m IncomingMessage) { messages = append(messages, m) })

			ch.handleMessageEvent(&slackevents.MessageEvent{
				SubType: "message_replied", Channel: tt.channel, TimeStamp: "1710000006.000600", Message: tt.message,
			})

			fetches := callsTo(calls(), "/conversations.replies")
			assert.Len(t, fetches, tt.wantFetches)
			if tt.wantFetches > 0 {
				assert.Equal(t, "1", fetches[0].Form.Get("limit"))
				assert.Equal(t, rootTS, fetches[0].Form.Get("ts"))
			}
			if tt.wantText == "" {
				assert.Empty(t, messages)
			} else {
				require.Len(t, messages, 1)
				assert.Equal(t, "U123", messages[0].From)
				assert.Equal(t, rootTS, messages[0].ThreadTS)
				assert.True(t, messages[0].IsThreadReply)
				assert.True(t, strings.HasPrefix(messages[0].Text, tt.wantText), messages[0].Text)
			}
			if tt.wantLog != "" {
				assert.Contains(t, sinkHistory(sink), tt.wantLog)
			}
		})
	}
}

func TestSlackChannel_SlackAttachmentsSkipsAndRecordsFailures(t *testing.T) {
	missing := httptest.NewServer(http.NotFoundHandler())
	defer missing.Close()
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)

	atts := ch.slackAttachments([]slack.File{
		{Name: "no-url.png", Mimetype: "image/png"},
		{Title: "report.png", Mimetype: "image/png", Size: 42, URLPrivate: missing.URL + "/report.png"},
	})

	require.Len(t, atts, 1, "files without a download URL are skipped")
	assert.Equal(t, "report.png", atts[0].Name)
	assert.Empty(t, atts[0].Path)
	assert.NotEmpty(t, atts[0].Err)
	assert.EqualValues(t, 42, atts[0].Size)
}

func TestSlackChannel_EnrichDeduplicatesReferencesAndLogsFetchErrors(t *testing.T) {
	server, calls := newFakeSlackAPI(t, map[string]string{
		"/conversations.history": `{"ok":false,"error":"not_in_channel"}`,
	})
	ch := newTestSlackChannel(server.URL)
	sink := newLogSink()
	ch.SetLogSink(sink)
	link := "https://aviary.slack.com/archives/C123/p1710000000123456"

	text := ch.enrichSlackMessageText(context.Background(), "see "+link+" and "+link, "D1", "1710000100.000000", "", nil)

	assert.Equal(t, "see "+link+" and "+link, text)
	assert.Len(t, callsTo(calls(), "/conversations.history"), 1, "a repeated link is fetched once")
	assert.Contains(t, sinkHistory(sink), "failed to fetch referenced message channel=C123 ts=1710000000.123456: not_in_channel")
}

func TestSlackChannel_FetchSlackReference(t *testing.T) {
	const (
		histOne     = `{"ok":true,"messages":[{"type":"message","user":"U1","text":"root","ts":"1710000000.000100"}]}`
		histThread  = `{"ok":true,"messages":[{"type":"message","user":"U1","text":"root","ts":"1710000000.000100","reply_count":1}]}`
		repliesTwo  = `{"ok":true,"messages":[{"type":"message","user":"U1","text":"root","ts":"1710000000.000100"},{"type":"message","user":"U2","text":"reply","ts":"1710000001.000100"}]}`
		apiFailure  = `{"ok":false,"error":"channel_not_found"}`
		historyNone = `{"ok":true,"messages":[]}`
	)
	linked := slackMessageReference{ChannelID: "C1", Timestamp: "1710000000.000100", Kind: "linked message"}
	threaded := linked
	threaded.ThreadTS = "1710000000.000100"
	tests := []struct {
		name    string
		ref     slackMessageReference
		routes  map[string]string
		want    string
		wantErr string
	}{
		{"single message", linked, map[string]string{"/conversations.history": histOne},
			"[linked message: C1 1710000000.000100]\nAlice: root", ""},
		{"message with replies", linked, map[string]string{"/conversations.history": histThread, "/conversations.replies": repliesTwo},
			"[linked message: C1 1710000000.000100]\nAlice: root\nU2: reply", ""},
		{"replies fetch fails falls back to message", linked, map[string]string{"/conversations.history": histThread, "/conversations.replies": apiFailure},
			"[linked message: C1 1710000000.000100]\nAlice: root", ""},
		{"no history", linked, map[string]string{"/conversations.history": historyNone}, "", ""},
		{"history error", linked, map[string]string{"/conversations.history": apiFailure}, "", "channel_not_found"},
		{"thread replies error", threaded, map[string]string{"/conversations.replies": apiFailure}, "", "channel_not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _ := newFakeSlackAPI(t, tt.routes)
			ch := newTestSlackChannel(server.URL)
			ch.userNames = map[string]string{"U1": "Alice"}

			got, err := ch.fetchSlackReference(context.Background(), tt.ref)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSlackChannel_FormatSlackMessages(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	ref := slackMessageReference{ChannelID: "C1", Timestamp: "1.000001"}

	assert.Empty(t, ch.formatSlackMessages(ref, nil))
	assert.Empty(t, ch.formatSlackMessages(ref, []slack.Message{{Msg: slack.Msg{User: "U1", Text: "  "}}}),
		"messages without content produce no block")

	got := ch.formatSlackMessages(ref, []slack.Message{
		{Msg: slack.Msg{Text: " "}},
		{Msg: slack.Msg{Username: "deploybot", Text: "deployed"}},
		{Msg: slack.Msg{BotID: "B1", Attachments: []slack.Attachment{{Title: "Build", Fields: []slack.AttachmentField{
			{Title: "Status", Value: "green"},
			{Value: "value only"},
			{Title: "title only"},
		}}}}},
		{Msg: slack.Msg{Text: "orphan"}},
	})
	assert.Equal(t, "[Slack message: C1 1.000001]\n"+
		"deploybot: deployed\n"+
		"B1: Build\nStatus: green\nvalue only\n"+
		"unknown: orphan", got)
}

func TestParseSlackMessageURL_Rejects(t *testing.T) {
	for _, raw := range []string{
		"https://aviary.slack.com/%zz",
		"http://aviary.slack.com/archives/C1/p1710000000123456",
		"https://example.com/archives/C1/p1710000000123456",
		"https://aviary.slack.com/messages/C1/p1710000000123456",
		"https://aviary.slack.com/archives/C1",
		"https://aviary.slack.com/archives/C1/p171000000",
		"https://aviary.slack.com/archives/C1/p17100000001234ab",
	} {
		_, ok := parseSlackMessageURL(raw)
		assert.False(t, ok, raw)
	}

	ref, ok := parseSlackMessageURL("https://aviary.slack.com/archives/C1/p17100000001")
	require.True(t, ok)
	assert.Equal(t, slackMessageReference{ChannelID: "C1", Timestamp: "1710000000.100000", Kind: "linked message"}, ref)
}

func TestParseSlackTimestamp(t *testing.T) {
	tests := []struct {
		raw    string
		want   int64 // UnixNano
		wantOK bool
	}{
		{"", 0, false},
		{"abc.123", 0, false},
		{"1710000000.12a", 0, false},
		{"1710000000", 1710000000 * 1e9, true},
		{"1710000000.5", 1710000000*1e9 + 500000000, true},
		{"1710000000.1234567899", 1710000000*1e9 + 123456789, true},
	}
	for _, tt := range tests {
		got, ok := parseSlackTimestamp(tt.raw)
		assert.Equal(t, tt.wantOK, ok, tt.raw)
		if tt.wantOK {
			assert.Equal(t, tt.want, got.UnixNano(), tt.raw)
		}
	}
}

func TestSlackChannel_ResolveAllowEntries(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	ch.userAliases = map[string]string{"alice": "U1", "@alice": "U1"}
	ch.channelAliases = map[string]string{"general": "C1"}

	assert.Nil(t, ch.resolveAllowEntries(nil))
	assert.Equal(t, []config.AllowFromEntry{
		{From: "*", AllowedGroups: ""},
		{From: "alice,U1", AllowedGroups: "#general,C1"},
		{From: "U1", AllowedGroups: "C1"},
		{From: "#,alice,U1", AllowedGroups: "alice"},
	}, ch.resolveAllowEntries([]config.AllowFromEntry{
		{From: "*", AllowedGroups: " "},
		{From: "alice", AllowedGroups: "#general"},
		{From: "U1, U1", AllowedGroups: "C1"},
		{From: "#, alice", AllowedGroups: "alice"}, // user aliases never resolve groups
	}))
}

func TestSlackChannel_ResolveDeliveryTargetOpensDirectMessages(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		open     string
		want     string
		wantUser string
		wantErr  string
	}{
		{"raw user ID", "U999", `{"ok":true,"channel":{"id":"D999"}}`, "D999", "U999", ""},
		{"enterprise user ID", "W999", `{"ok":true,"channel":{"id":"D998"}}`, "D998", "W999", ""},
		{"unknown name passes through", "alerts", "", "alerts", "", ""},
		{"open fails", "U999", `{"ok":false,"error":"user_not_found"}`, "", "", "opening Slack DM with U999: user_not_found"},
		{"open returns no channel", "U999", `{"ok":true,"channel":{"id":" "}}`, "", "", "returned no channel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, calls := newFakeSlackAPI(t, map[string]string{"/conversations.open": tt.open})
			ch := newTestSlackChannel(server.URL)

			got, err := ch.resolveDeliveryTarget(context.Background(), tt.target)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			opens := callsTo(calls(), "/conversations.open")
			if tt.wantUser == "" {
				assert.Empty(t, opens)
			} else {
				require.Len(t, opens, 1)
				assert.Equal(t, tt.wantUser, opens[0].Form.Get("users"))
			}
		})
	}
}
