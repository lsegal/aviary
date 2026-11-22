package channels

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sessionStatusCall struct {
	Channel   string
	Thread    string
	Status    string
	Initiator string
}

func newSessionStatusServer(t *testing.T) (*httptest.Server, func() []sessionStatusCall) {
	t.Helper()
	var (
		mu    sync.Mutex
		calls []sessionStatusCall
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, r.ParseForm())
		switch r.URL.Path {
		case "/agents.sessions.setStatus":
			mu.Lock()
			calls = append(calls, sessionStatusCall{
				Channel:   r.Form.Get("channel_id"),
				Thread:    r.Form.Get("thread_ts"),
				Status:    r.Form.Get("status"),
				Initiator: r.Form.Get("initiator_user_id"),
			})
			mu.Unlock()
			_, _ = w.Write([]byte(`{"ok":true,"status":"` + r.Form.Get("status") + `"}`))
		default:
			t.Errorf("unexpected Slack API path: %s", r.URL.Path)
			_, _ = w.Write([]byte(`{"ok":false,"error":"unknown_method"}`))
		}
	}))
	t.Cleanup(server.Close)
	return server, func() []sessionStatusCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]sessionStatusCall{}, calls...)
	}
}

func newTestSlackChannel(serverURL string) *SlackChannel {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	ch.client = slack.New("xoxb-token", slack.OptionAPIURL(serverURL+"/"))
	return ch
}

func TestSlackChannel_SetAssistantProcessing_UsesAgentSessions(t *testing.T) {
	server, calls := newSessionStatusServer(t)
	ch := newTestSlackChannel(server.URL)
	ch.identityMu.Lock()
	ch.channelAliases = map[string]string{"alerts": "C123"}
	ch.identityMu.Unlock()

	require.NoError(t, ch.SetAssistantProcessing("#alerts", "1710000000.123456", "U123", true))
	require.NoError(t, ch.SetAssistantProcessing("#alerts", "1710000000.123456", "U123", false))

	assert.Equal(t, []sessionStatusCall{
		{Channel: "C123", Thread: "1710000000.123456", Status: "processing", Initiator: "U123"},
		{Channel: "C123", Thread: "1710000000.123456", Status: "active"},
	}, calls())
}

func TestSlackChannel_SetAssistantProcessing_RequiresThread(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	assert.Error(t, ch.SetAssistantProcessing("C123", " ", "U123", true))
}

func TestSlackAgentSessionStoppedEvent_Parses(t *testing.T) {
	raw := `{"type":"event_callback","team_id":"T1","api_app_id":"A1","event":{"type":"agent_session_stopped","channel":"D123","thread_ts":"1710000000.123456","user":"U123","streaming_message_ts":["1710000001.000100"],"event_ts":"1710000002.000200"}}`
	evt, err := slackevents.ParseEvent(json.RawMessage(raw), slackevents.OptionNoVerifyToken())
	require.NoError(t, err)

	stopped, ok := evt.InnerEvent.Data.(*slackevents.AgentSessionStoppedEvent)
	require.True(t, ok, "dispatch relies on *slackevents.AgentSessionStoppedEvent, got %T", evt.InnerEvent.Data)
	assert.Equal(t, "D123", stopped.Channel)
	assert.Equal(t, "1710000000.123456", stopped.ThreadTimestamp)
	assert.Equal(t, []string{"1710000001.000100"}, stopped.StreamingMessageTimestamps)
}

func TestSlackChannel_AgentSessionStopped_StopsOnlyThatThread(t *testing.T) {
	server, calls := newSessionStatusServer(t)
	ch := newTestSlackChannel(server.URL)

	var stoppedA, stoppedA2, stoppedB, stoppedOtherChannel int
	ch.RegisterRunStop("D123", "1.0001", func() { stoppedA++ })
	ch.RegisterRunStop("D123", "1.0001", func() { stoppedA2++ })
	ch.RegisterRunStop("D123", "1.0002", func() { stoppedB++ })
	ch.RegisterRunStop("C999", "1.0001", func() { stoppedOtherChannel++ })

	ch.handleAgentSessionStopped(&slackevents.AgentSessionStoppedEvent{
		Type:            "agent_session_stopped",
		Channel:         "D123",
		ThreadTimestamp: "1.0001",
		User:            "U123",
	})

	assert.Equal(t, 1, stoppedA)
	assert.Equal(t, 1, stoppedA2)
	assert.Zero(t, stoppedB)
	assert.Zero(t, stoppedOtherChannel)
	assert.Equal(t, []sessionStatusCall{{Channel: "D123", Thread: "1.0001", Status: "active"}}, calls())

	// A second stop for the same thread finds nothing to cancel but still
	// clears any leftover status without error.
	ch.handleAgentSessionStopped(&slackevents.AgentSessionStoppedEvent{Channel: "D123", ThreadTimestamp: "1.0001"})
	assert.Equal(t, 1, stoppedA)
	assert.Len(t, calls(), 2)
}

func TestSlackChannel_AgentSessionStopped_AfterRunFinishedIsNoop(t *testing.T) {
	server, calls := newSessionStatusServer(t)
	ch := newTestSlackChannel(server.URL)

	stopped := 0
	unregister := ch.RegisterRunStop("D123", "1.0001", func() { stopped++ })
	unregister()
	unregister() // idempotent

	ch.handleAgentSessionStopped(&slackevents.AgentSessionStoppedEvent{Channel: "D123", ThreadTimestamp: "1.0001"})

	assert.Zero(t, stopped)
	assert.Equal(t, []sessionStatusCall{{Channel: "D123", Thread: "1.0001", Status: "active"}}, calls())

	// A new run in the same thread registers and stops normally.
	ch.RegisterRunStop("D123", "1.0001", func() { stopped++ })
	ch.handleAgentSessionStopped(&slackevents.AgentSessionStoppedEvent{Channel: "D123", ThreadTimestamp: "1.0001"})
	assert.Equal(t, 1, stopped)
}

func TestSlackChannel_RegisterRunStop_IgnoresMissingThread(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	called := false
	unregister := ch.RegisterRunStop("D123", "", func() { called = true })
	unregister()
	assert.Zero(t, ch.stopThreadRuns("D123", ""))
	assert.False(t, called)
}
