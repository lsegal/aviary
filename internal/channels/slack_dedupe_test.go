package channels

import (
	"encoding/json"
	"testing"

	"github.com/lsegal/aviary/internal/config"

	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDedupeTestSlackChannel(t *testing.T) (*SlackChannel, func() []IncomingMessage) {
	t.Helper()
	ch := NewSlackChannel("xapp-token", "xoxb-token", []config.AllowFromEntry{{
		From: "*", AllowedGroups: "*", RespondToMentions: true,
	}}, "m", nil)
	ch.botUserID = "UBOT"
	var messages []IncomingMessage
	ch.OnMessage(func(m IncomingMessage) { messages = append(messages, m) })
	return ch, func() []IncomingMessage { return messages }
}

func parseSlackEvent(t *testing.T, raw string) slackevents.EventsAPIEvent {
	t.Helper()
	evt, err := slackevents.ParseEvent(json.RawMessage(raw), slackevents.OptionNoVerifyToken())
	require.NoError(t, err)
	return evt
}

const slackThreadMessageEvent = `{"type":"event_callback","team_id":"T1","api_app_id":"A1","event_id":"Ev01","event_time":1710000000,` +
	`"event":{"type":"message","channel":"C123","user":"U123","text":"<@UBOT> file the bug","ts":"1710000001.000100","thread_ts":"1710000000.123456","client_msg_id":"8c1e3d5a-0b6f-4f0e-9b7a-1d2c3e4f5a6b","event_ts":"1710000001.000100"}}`

func TestSlackChannel_RetriedEventDeliversOneMessage(t *testing.T) {
	ch, messages := newDedupeTestSlackChannel(t)
	evt := parseSlackEvent(t, slackThreadMessageEvent)

	ch.handleEventsAPI(&socketmode.Request{Type: "events_api", EnvelopeID: "env-1"}, evt)
	ch.handleEventsAPI(&socketmode.Request{Type: "events_api", EnvelopeID: "env-2", RetryAttempt: 1, RetryReason: "timeout"}, evt)
	ch.handleEventsAPI(&socketmode.Request{Type: "events_api", EnvelopeID: "env-3", RetryAttempt: 2, RetryReason: "timeout"}, evt)

	require.Len(t, messages(), 1)
	assert.Equal(t, "1710000000.123456", messages()[0].ThreadTS)
}

func TestSlackChannel_ResentMessageSharingClientMsgIDDeliversOnce(t *testing.T) {
	ch, messages := newDedupeTestSlackChannel(t)
	first := parseSlackEvent(t, slackThreadMessageEvent)
	// Slack resends the same message as new events: new event_ids, the same
	// client_msg_id and ts.
	second := parseSlackEvent(t, `{"type":"event_callback","event_id":"Ev02","event":{"type":"message","channel":"C123","user":"U123","text":"<@UBOT> file the bug","ts":"1710000001.000100","thread_ts":"1710000000.123456","client_msg_id":"8c1e3d5a-0b6f-4f0e-9b7a-1d2c3e4f5a6b"}}`)
	// The same client_msg_id under a different ts is still the same message.
	third := parseSlackEvent(t, `{"type":"event_callback","event_id":"Ev03","event":{"type":"message","channel":"C123","user":"U123","text":"<@UBOT> file the bug","ts":"1710000002.000200","thread_ts":"1710000000.123456","client_msg_id":"8c1e3d5a-0b6f-4f0e-9b7a-1d2c3e4f5a6b"}}`)

	ch.handleEventsAPI(nil, first)
	ch.handleEventsAPI(nil, second)
	ch.handleEventsAPI(nil, third)

	assert.Len(t, messages(), 1)
}

func TestSlackChannel_DistinctMessagesAndEditsStillDeliver(t *testing.T) {
	ch, messages := newDedupeTestSlackChannel(t)
	ch.handleEventsAPI(nil, parseSlackEvent(t, slackThreadMessageEvent))
	// A different message in the same thread.
	ch.handleEventsAPI(nil, parseSlackEvent(t, `{"type":"event_callback","event_id":"Ev04","event":{"type":"message","channel":"C123","user":"U123","text":"<@UBOT> and the docs","ts":"1710000003.000300","thread_ts":"1710000000.123456","client_msg_id":"0f9e8d7c-6b5a-4f3e-8d2c-1b0a9f8e7d6c"}}`))
	// An edit keeps the ts and client_msg_id but changes the text.
	ch.handleEventsAPI(nil, parseSlackEvent(t, `{"type":"event_callback","event_id":"Ev05","event":{"type":"message","subtype":"message_changed","channel":"C123","ts":"1710000004.000400",`+
		`"message":{"type":"message","user":"U123","text":"<@UBOT> file the bug, please","ts":"1710000001.000100","thread_ts":"1710000000.123456","client_msg_id":"8c1e3d5a-0b6f-4f0e-9b7a-1d2c3e4f5a6b","edited":{"user":"U123","ts":"1710000004.000400"}},`+
		`"previous_message":{"type":"message","user":"U123","text":"<@UBOT> file the bug","ts":"1710000001.000100"}}}`))

	assert.Len(t, messages(), 3)
}

func TestSlackChannel_DispatchDefersHandling(t *testing.T) {
	ch, messages := newDedupeTestSlackChannel(t)
	evt := parseSlackEvent(t, slackThreadMessageEvent)

	work := ch.dispatch(socketmode.Event{Type: socketmode.EventTypeEventsAPI, Data: evt})
	require.NotNil(t, work)
	assert.Empty(t, messages(), "dispatch must return to the ack loop before the event is handled")
	work()
	assert.Len(t, messages(), 1)

	assert.Nil(t, ch.dispatch(socketmode.Event{Type: socketmode.EventTypeConnected}))
}
