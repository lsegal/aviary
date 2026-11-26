package channels

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/lsegal/aviary/internal/config"
)

// SlackChannel connects to Slack using Socket Mode (no public URL required).
type SlackChannel struct {
	appToken      string // xapp-... token for socket mode
	botToken      string // xoxb-... token for posting
	allowFrom     []config.AllowFromEntry
	model         string
	fallbacks     []string
	disabledTools []string
	showStatus    bool

	botUserID         string // populated on connect via auth.test
	resolvedAllowFrom []config.AllowFromEntry

	client          *slack.Client
	sm              *socketmode.Client
	handler         func(IncomingMessage)
	groupLogHandler func(IncomingMessage)
	handlerMu       sync.RWMutex
	identityMu      sync.RWMutex
	userAliases     map[string]string
	userNames       map[string]string
	channelAliases  map[string]string
	channelNames    map[string]string
	seenMessages    receipts
	seenEvents      receipts
	stopOnce        sync.Once
	cancel          context.CancelFunc
	logSinkMu       sync.RWMutex
	logSink         *LogSink
	runStopsMu      sync.Mutex
	runStops        map[string]map[uint64]func()
	nextRunStopID   uint64
}

// NewSlackChannel creates a SlackChannel.
// appToken is the App-Level token (xapp-), botToken is the Bot token (xoxb-).
func NewSlackChannel(appToken, botToken string, allowFrom []config.AllowFromEntry, model string, fallbacks []string) *SlackChannel {
	api := slack.New(botToken, slack.OptionAppLevelToken(appToken))
	sm := socketmode.New(api)
	return &SlackChannel{
		appToken:   appToken,
		botToken:   botToken,
		allowFrom:  allowFrom,
		model:      model,
		fallbacks:  fallbacks,
		showStatus: true,
		client:     api,
		sm:         sm,
	}
}

// SetLogSink attaches a LogSink that receives Slack connection/runtime logs.
func (c *SlackChannel) SetLogSink(s *LogSink) {
	c.logSinkMu.Lock()
	c.logSink = s
	c.logSinkMu.Unlock()
}

func (c *SlackChannel) logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	c.logSinkMu.RLock()
	sink := c.logSink
	c.logSinkMu.RUnlock()
	if sink != nil {
		sink.Write(time.Now().UTC().Format(time.RFC3339) + " " + line)
	}
}

// OnMessage registers a callback for incoming messages.
func (c *SlackChannel) OnMessage(fn func(IncomingMessage)) {
	c.handlerMu.Lock()
	defer c.handlerMu.Unlock()
	c.handler = fn
}

// OnGroupChatMessage registers a callback invoked for all group messages before
// allowFrom filtering, enabling a full channel transcript to be maintained.
func (c *SlackChannel) OnGroupChatMessage(fn func(IncomingMessage)) {
	c.handlerMu.Lock()
	defer c.handlerMu.Unlock()
	c.groupLogHandler = fn
}

// Send posts a message to a Slack channel.
func (c *SlackChannel) Send(channel, text string) error {
	resolvedChannel, err := c.resolveDeliveryTarget(context.Background(), channel)
	if err != nil {
		return err
	}
	_, _, err = c.client.PostMessage(resolvedChannel, slack.MsgOptionText(slackMrkdwn(text), false))
	return err
}

// SendAndGetID posts a message and returns the Slack message timestamp, which
// serves as the message ID for EditMessage.
func (c *SlackChannel) SendAndGetID(channel, text string) (string, error) {
	resolvedChannel, err := c.resolveDeliveryTarget(context.Background(), channel)
	if err != nil {
		return "", err
	}
	_, timestamp, err := c.client.PostMessage(resolvedChannel, slack.MsgOptionText(slackMrkdwn(text), false))
	return timestamp, err
}

// SendThreadMessageAndGetID posts a reply to a Slack thread and returns the
// message timestamp, which can later be passed to EditMessage.
func (c *SlackChannel) SendThreadMessageAndGetID(channel, threadTS, text string) (string, error) {
	resolvedChannel, err := c.resolveDeliveryTarget(context.Background(), channel)
	if err != nil {
		return "", err
	}
	threadTS = strings.TrimSpace(threadTS)
	if threadTS == "" {
		return "", fmt.Errorf("slack thread timestamp is required")
	}
	opts := []slack.MsgOption{
		slack.MsgOptionText(slackMrkdwn(text), false),
		slack.MsgOptionTS(threadTS),
	}
	_, timestamp, err := c.client.PostMessage(
		resolvedChannel,
		opts...,
	)
	return timestamp, err
}

// SendThreadBlocksAndGetID posts a reply with Block Kit content to a Slack
// thread and returns the message timestamp.
func (c *SlackChannel) SendThreadBlocksAndGetID(channel, threadTS, fallbackText string, blocks ...slack.Block) (string, error) {
	resolvedChannel, err := c.resolveDeliveryTarget(context.Background(), channel)
	if err != nil {
		return "", err
	}
	threadTS = strings.TrimSpace(threadTS)
	if threadTS == "" {
		return "", fmt.Errorf("slack thread timestamp is required")
	}
	opts := []slack.MsgOption{
		slack.MsgOptionText(slackMrkdwn(fallbackText), false),
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionTS(threadTS),
	}
	_, timestamp, err := c.client.PostMessage(resolvedChannel, opts...)
	return timestamp, err
}

// EditMessage updates a previously posted Slack message in place.
func (c *SlackChannel) EditMessage(channel, msgID, text string) error {
	resolvedChannel, err := c.resolveDeliveryTarget(context.Background(), channel)
	if err != nil {
		return err
	}
	_, _, _, err = c.client.UpdateMessage(resolvedChannel, msgID, slack.MsgOptionText(slackMrkdwn(text), false))
	return err
}

// EditMessageBlocks updates a previously posted Slack message with Block Kit
// content.
func (c *SlackChannel) EditMessageBlocks(channel, msgID, fallbackText string, blocks ...slack.Block) error {
	resolvedChannel, err := c.resolveDeliveryTarget(context.Background(), channel)
	if err != nil {
		return err
	}
	_, _, _, err = c.client.UpdateMessage(
		resolvedChannel,
		msgID,
		slack.MsgOptionText(slackMrkdwn(fallbackText), false),
		slack.MsgOptionBlocks(blocks...),
	)
	return err
}

// ShowAssistantStatus reports whether Slack assistant status updates are
// enabled for this channel.
func (c *SlackChannel) ShowAssistantStatus() bool {
	return c.showStatus
}

// SetAssistantProcessing sets the Slack agent session for a thread to
// processing (Slack shows "is working" with a Stop control) or back to active.
func (c *SlackChannel) SetAssistantProcessing(channel, threadTS, initiatorUserID string, processing bool) error {
	resolvedChannel, err := c.resolveDeliveryTarget(context.Background(), channel)
	if err != nil {
		return err
	}
	threadTS = strings.TrimSpace(threadTS)
	if threadTS == "" {
		return fmt.Errorf("slack agent session status requires a thread timestamp")
	}
	params := slack.AgentSessionSetStatusParameters{
		Status:    slack.AgentSessionStatusActive,
		ChannelID: resolvedChannel,
		ThreadTS:  threadTS,
	}
	if processing {
		params.Status = slack.AgentSessionStatusProcessing
		params.InitiatorUserID = strings.TrimSpace(initiatorUserID)
	}
	resp, err := c.client.SetAgentSessionStatusContext(context.Background(), params)
	if err != nil {
		return err
	}
	if warning := strings.TrimSpace(resp.Warning); warning != "" {
		c.logf("slack: agents.sessions.setStatus warning channel=%s thread=%s warning=%s", resolvedChannel, threadTS, warning)
	}
	return nil
}

// RegisterRunStop registers a stop function for an in-flight run in a Slack
// thread. Slack's Stop control (the agent_session_stopped event) calls every
// stop function registered for that channel and thread.
func (c *SlackChannel) RegisterRunStop(channel, threadTS string, stop func()) func() {
	key := slackRunKey(channel, threadTS)
	if key == "" || stop == nil {
		return func() { /* nothing was registered, so there is nothing to unregister */ }
	}
	c.runStopsMu.Lock()
	if c.runStops == nil {
		c.runStops = make(map[string]map[uint64]func())
	}
	c.nextRunStopID++
	id := c.nextRunStopID
	if c.runStops[key] == nil {
		c.runStops[key] = make(map[uint64]func())
	}
	c.runStops[key][id] = stop
	c.runStopsMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			c.runStopsMu.Lock()
			defer c.runStopsMu.Unlock()
			if stops := c.runStops[key]; stops != nil {
				delete(stops, id)
				if len(stops) == 0 {
					delete(c.runStops, key)
				}
			}
		})
	}
}

// stopThreadRuns calls and removes every stop function registered for the
// thread, returning how many runs were stopped.
func (c *SlackChannel) stopThreadRuns(channel, threadTS string) int {
	key := slackRunKey(channel, threadTS)
	if key == "" {
		return 0
	}
	c.runStopsMu.Lock()
	stops := c.runStops[key]
	delete(c.runStops, key)
	c.runStopsMu.Unlock()
	for _, stop := range stops {
		stop()
	}
	return len(stops)
}

func slackRunKey(channel, threadTS string) string {
	channel = strings.TrimSpace(channel)
	threadTS = strings.TrimSpace(threadTS)
	if channel == "" || threadTS == "" {
		return ""
	}
	return channel + "|" + threadTS
}

// handleAgentSessionStopped handles a user pressing Stop on the "is working"
// indicator. It cancels only the runs in that channel and thread, then moves
// the agent session out of processing. A stop for a thread with no active run
// only clears the status.
func (c *SlackChannel) handleAgentSessionStopped(event *slackevents.AgentSessionStoppedEvent) {
	if event == nil {
		return
	}
	stopped := c.stopThreadRuns(event.Channel, event.ThreadTimestamp)
	c.logf("slack: agent session stopped channel=%s thread=%s user=%s runs=%d", event.Channel, event.ThreadTimestamp, event.User, stopped)
	if strings.TrimSpace(event.Channel) == "" || strings.TrimSpace(event.ThreadTimestamp) == "" {
		return
	}
	if err := c.SetAssistantProcessing(event.Channel, event.ThreadTimestamp, "", false); err != nil {
		c.logf("slack: failed to clear agent session status after stop: %v", err)
	}
}

// Start connects via Socket Mode and blocks until ctx is cancelled.
func (c *SlackChannel) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.logf("slack: starting socket mode session")

	c.loadIdentity(ctx)

	// Events are acked as they arrive and handled in order on a separate
	// goroutine, so slow handling (API lookups, file downloads) never delays
	// the ack of a later event into a Slack retry.
	work := make(chan func(), slackEventQueueSize)
	go runSlackEventWork(ctx, work)
	go c.pumpSocketEvents(ctx, work)

	err := c.sm.RunContext(ctx)
	if ctx.Err() == nil {
		c.logf("slack: socket mode exited with error: %v", err)
	} else {
		c.logf("slack: socket mode stopped")
	}
	return err
}

// loadIdentity fetches the bot's own user ID (so direct @mentions in groups
// can be detected) and the users/channels name cache. Failures are logged and
// only disable the features that depend on them.
func (c *SlackChannel) loadIdentity(ctx context.Context) {
	if resp, err := c.client.AuthTestContext(ctx); err == nil {
		c.botUserID = resp.UserID
		c.logf("slack: auth ok user_id=%s team=%s", resp.UserID, strings.TrimSpace(resp.Team))
	} else {
		c.logf("slack: auth.test failed: %v", err)
		slog.Warn("slack: auth.test failed; direct-mention detection disabled", "err", err)
	}
	if err := c.refreshIdentityCache(ctx); err != nil {
		c.logf("slack: failed to refresh users/channels: %v", err)
		slog.Warn("slack: failed to load users/channels; name-based routing disabled", "err", err)
	} else {
		c.logf("slack: identity cache ready")
	}
}

// runSlackEventWork runs queued event handlers in order until ctx is done.
func runSlackEventWork(ctx context.Context, work <-chan func()) {
	for {
		select {
		case <-ctx.Done():
			return
		case fn := <-work:
			fn()
		}
	}
}

// pumpSocketEvents acks Socket Mode events and queues their handlers on work
// until ctx is done or the event stream closes.
func (c *SlackChannel) pumpSocketEvents(ctx context.Context, work chan<- func()) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-c.sm.Events:
			if !ok {
				c.logf("slack: event stream closed")
				return
			}
			if !enqueueSlackWork(ctx, work, c.dispatch(evt)) {
				return
			}
		}
	}
}

// enqueueSlackWork queues fn (if non-nil) and reports false when ctx ended first.
func enqueueSlackWork(ctx context.Context, work chan<- func(), fn func()) bool {
	if fn == nil {
		return true
	}
	select {
	case work <- fn:
		return true
	case <-ctx.Done():
		return false
	}
}

// Stop disconnects from Slack.
func (c *SlackChannel) Stop() {
	c.stopOnce.Do(func() {
		c.logf("slack: stop requested")
		if c.cancel != nil {
			c.cancel()
		}
	})
}

// dispatch acks a Socket Mode request and returns the work that handles it,
// or nil when there is nothing to handle.
func (c *SlackChannel) dispatch(evt socketmode.Event) func() {
	switch evt.Type {
	case socketmode.EventTypeConnecting:
		c.logf("slack: connecting")
	case socketmode.EventTypeConnected:
		c.logf("slack: connected")
	case socketmode.EventTypeConnectionError:
		c.logf("slack: connection error")
	case socketmode.EventTypeInvalidAuth:
		c.logf("slack: invalid auth")
	case socketmode.EventTypeDisconnect:
		c.logf("slack: disconnected")
	}
	if evt.Type != socketmode.EventTypeEventsAPI {
		return nil
	}
	if evt.Request != nil {
		if err := c.sm.Ack(*evt.Request); err != nil {
			c.logf("slack: failed to ack event: %v", err)
		}
	}

	eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		return nil
	}
	return func() { c.handleEventsAPI(evt.Request, eventsAPI) }
}

// handleEventsAPI handles one Events API delivery. Slack redelivers an event
// it thinks went unacknowledged, with the same event_id, so a delivery whose
// event_id was already handled is dropped.
func (c *SlackChannel) handleEventsAPI(req *socketmode.Request, eventsAPI slackevents.EventsAPIEvent) {
	if callback, ok := eventsAPI.Data.(*slackevents.EventsAPICallbackEvent); ok && callback != nil {
		if c.seenEvents.seen(callback.EventID) {
			retry, reason := 0, ""
			if req != nil {
				retry, reason = req.RetryAttempt, req.RetryReason
			}
			c.logf("slack: dropped duplicate event event_id=%s retry=%d reason=%s", callback.EventID, retry, reason)
			return
		}
	}
	switch inner := eventsAPI.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		c.handleMessageEvent(inner)
	case *slackevents.AppMentionEvent:
		c.handleAppMentionEvent(inner)
	case *slackevents.AgentSessionStoppedEvent:
		c.handleAgentSessionStopped(inner)
	}
}

func (c *SlackChannel) handleMessageEvent(event *slackevents.MessageEvent) {
	event, ok := c.normalizeMessageEvent(event)
	if !ok {
		return
	}
	m := extractSlackMessage(event)
	if c.ignoreSlackMessage(m) {
		return
	}

	// Slack DM channels start with 'D'; everything else is a group/channel.
	isGroup := !strings.HasPrefix(m.channelID, "D")

	if c.seenMessage(m.channelID, m.from, m.rawTimestamp, m.clientMsgID, m.text) {
		c.logf("slack: dropped duplicate message channel=%s ts=%s client_msg_id=%s", m.channelID, m.rawTimestamp, m.clientMsgID)
		return
	}
	isThreadReply := strings.TrimSpace(m.threadTS) != "" && strings.TrimSpace(m.rawTimestamp) != "" && m.threadTS != m.rawTimestamp
	receivedAt := time.Now().UTC()
	if ts, ok := parseSlackTimestamp(m.rawTimestamp); ok {
		receivedAt = ts
	}
	enrichedText := c.enrichSlackMessageText(context.Background(), m.text, m.channelID, m.rawTimestamp, m.threadTS, m.attachments)

	// Log all group messages before allowFrom filtering.
	if isGroup {
		c.logGroupMessage(IncomingMessage{
			Type:          "slack",
			From:          m.from,
			SenderName:    c.displayNameForUser(m.from),
			Channel:       m.channelID,
			ThreadTS:      m.threadTS,
			IsThreadReply: isThreadReply,
			Text:          enrichedText,
			ReceivedAt:    receivedAt,
		})
	}

	result := checkAllowed(c.allowedEntries(), m.from, m.channelID, m.text, isGroup, c.botUserID, false)
	if !result.allowed && isThreadReply {
		result = checkAllowedReplyContinuation(c.allowedEntries(), m.from, m.channelID, isGroup)
	}
	if !result.allowed {
		c.logf("slack: ignored message from=%s channel=%s", m.from, m.channelID)
		return
	}

	c.handlerMu.RLock()
	fn := c.handler
	c.handlerMu.RUnlock()
	if fn == nil {
		c.logf("slack: no message handler registered for from=%s", m.from)
		slog.Debug("slack: no handler registered", "from", m.from)
		return
	}
	im := IncomingMessage{
		Type:          "slack",
		From:          m.from,
		SenderName:    c.displayNameForUser(m.from),
		Channel:       m.channelID,
		ThreadTS:      m.threadTS,
		IsThreadReply: isThreadReply,
		Text:          enrichedText,
		Attachments:   c.slackAttachments(m.files),
		ReceivedAt:    receivedAt,
		RestrictTools: result.restrictTools,
		DisabledTools: c.disabledTools,
		Model:         result.model,
		Fallbacks:     result.fallbacks,
	}
	// Apply channel-level overrides if entry-level ones are absent.
	if im.Model == "" {
		im.Model = c.model
	}
	if len(im.Fallbacks) == 0 {
		im.Fallbacks = c.fallbacks
	}
	fn(im)
}

// normalizeMessageEvent resolves message_replied events to the reply they
// announce and reports false for events that should not start a run.
func (c *SlackChannel) normalizeMessageEvent(event *slackevents.MessageEvent) (*slackevents.MessageEvent, bool) {
	if event == nil {
		return nil, false
	}
	if event.SubType == slack.MsgSubTypeMessageReplied {
		normalized, ok := c.normalizeMessageRepliedEvent(context.Background(), event)
		if !ok {
			return nil, false
		}
		event = normalized
	}
	if event.SubType == "message_changed" && event.Message != nil {
		// Slack also emits message_changed when a thread's reply count changes.
		// Only a content edit should start another agent run.
		if event.PreviousMessage != nil && event.PreviousMessage.Text == event.Message.Text {
			return nil, false
		}
		if event.PreviousMessage == nil && event.Message.Edited == nil {
			return nil, false
		}
	}
	return event, true
}

// slackMessage holds the fields of a message event, taken from the edited
// message where an edit overrides them.
type slackMessage struct {
	channelID, from, text, botID, clientMsgID string
	rawTimestamp, threadTS                    string
	files                                     []slack.File
	attachments                               []slack.Attachment
}

func extractSlackMessage(event *slackevents.MessageEvent) slackMessage {
	m := slackMessage{
		channelID:    event.Channel,
		from:         event.User,
		text:         event.Text,
		botID:        event.BotID,
		clientMsgID:  event.ClientMsgID,
		rawTimestamp: event.TimeStamp,
		threadTS:     strings.TrimSpace(event.ThreadTimeStamp),
	}
	if event.Message != nil {
		m.files = event.Message.Files
		m.attachments = event.Message.Attachments
	}
	isEdited := event.IsEdited() || (event.SubType == "message_changed" && event.Message != nil)
	if isEdited && event.Message != nil {
		edited := event.Message
		m.channelID = orDefault(edited.Channel, m.channelID)
		m.from = orDefault(edited.User, m.from)
		m.text = orDefault(edited.Text, m.text)
		m.botID = orDefault(edited.BotID, m.botID)
		m.clientMsgID = orDefault(edited.ClientMsgID, m.clientMsgID)
		m.rawTimestamp = orDefault(edited.Timestamp, m.rawTimestamp)
		if edited.ThreadTimestamp != "" {
			m.threadTS = strings.TrimSpace(edited.ThreadTimestamp)
		}
	}
	if m.threadTS == "" {
		m.threadTS = m.rawTimestamp
	}
	return m
}

// orDefault returns v, or def when v is empty.
func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

// ignoreSlackMessage reports whether m is from a bot (including this one),
// has no content, or lacks a sender or channel.
func (c *SlackChannel) ignoreSlackMessage(m slackMessage) bool {
	if m.botID != "" || (c.botUserID != "" && m.from == c.botUserID) {
		return true
	}
	if strings.TrimSpace(m.text) == "" && len(m.files) == 0 && len(m.attachments) == 0 {
		return true
	}
	return m.from == "" || m.channelID == ""
}

// logGroupMessage forwards msg to the group log handler, if one is set.
func (c *SlackChannel) logGroupMessage(msg IncomingMessage) {
	c.handlerMu.RLock()
	logFn := c.groupLogHandler
	c.handlerMu.RUnlock()
	if logFn != nil {
		logFn(msg)
	}
}

// Slack can deliver the same mention as both a message and an app_mention
// event, and can resend a message as a new event that shares its ts and
// client_msg_id. Keep a short-lived receipt of each so one user message
// starts one agent run. An edit keeps its ts but changes the text, so the
// text is part of the receipt and an edit still starts a run.
func (c *SlackChannel) seenMessage(channelID, from, timestamp, clientMsgID, text string) bool {
	prefix := channelID + "\x00" + from + "\x00"
	var keys []string
	if timestamp != "" {
		keys = append(keys, prefix+"ts\x00"+timestamp+"\x00"+text)
	}
	if clientMsgID != "" {
		keys = append(keys, prefix+"client\x00"+clientMsgID+"\x00"+text)
	}
	seen := false
	for _, key := range keys {
		// Record every key, so a later copy matching only one of them is
		// still recognized.
		if c.seenMessages.seen(key) {
			seen = true
		}
	}
	return seen
}

// slackEventQueueSize bounds the events acked but not yet handled.
const slackEventQueueSize = 256

func (c *SlackChannel) normalizeMessageRepliedEvent(ctx context.Context, event *slackevents.MessageEvent) (*slackevents.MessageEvent, bool) {
	if event == nil || event.SubType != slack.MsgSubTypeMessageReplied || event.Message == nil {
		return event, false
	}
	channelID := firstNonEmpty(event.Channel, event.Message.Channel)
	threadTS := firstNonEmpty(event.ThreadTimeStamp, event.Message.ThreadTimestamp, event.Message.Timestamp, event.TimeStamp)
	latestReply := strings.TrimSpace(event.Message.LatestReply)
	if latestReply == "" && len(event.Message.Replies) > 0 {
		latestReply = strings.TrimSpace(event.Message.Replies[len(event.Message.Replies)-1].Timestamp)
	}
	if latestReply == threadTS {
		return event, false
	}
	if channelID == "" || threadTS == "" || latestReply == "" {
		c.logf("slack: ignored message_replied event with missing channel/thread/reply channel=%s thread=%s reply=%s", channelID, threadTS, latestReply)
		return event, false
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	msgs, _, _, err := c.client.GetConversationRepliesContext(fetchCtx, &slack.GetConversationRepliesParameters{
		ChannelID: channelID,
		Timestamp: threadTS,
		Inclusive: true,
		Oldest:    latestReply,
		Limit:     1,
	})
	if err != nil || len(msgs) == 0 {
		c.logf("slack: failed to fetch latest thread reply channel=%s thread=%s reply=%s: %v", channelID, threadTS, latestReply, err)
		return event, false
	}
	reply := msgs[len(msgs)-1]
	if reply.Timestamp != latestReply {
		c.logf("slack: latest thread reply fetch returned ts=%s expected=%s", reply.Timestamp, latestReply)
	}

	return &slackevents.MessageEvent{
		Type:            "message",
		User:            reply.User,
		Text:            reply.Text,
		TimeStamp:       firstNonEmpty(reply.Timestamp, latestReply),
		ThreadTimeStamp: firstNonEmpty(reply.ThreadTimestamp, threadTS),
		Channel:         channelID,
		ChannelType:     event.ChannelType,
		EventTimeStamp:  event.EventTimeStamp,
		BotID:           reply.BotID,
		Message:         &reply.Msg,
	}, true
}

func (c *SlackChannel) handleAppMentionEvent(event *slackevents.AppMentionEvent) {
	if event == nil {
		return
	}
	c.handleMessageEvent(&slackevents.MessageEvent{
		Type:            "message",
		User:            event.User,
		Text:            event.Text,
		TimeStamp:       event.TimeStamp,
		ThreadTimeStamp: event.ThreadTimeStamp,
		Channel:         event.Channel,
		BotID:           event.BotID,
	})
}

func (c *SlackChannel) slackAttachments(files []slack.File) []Attachment {
	var out []Attachment
	for _, file := range files {
		sourceURL := firstNonEmpty(file.URLPrivateDownload, file.URLPrivate)
		if sourceURL == "" {
			continue
		}
		name := firstNonEmpty(file.Name, file.Title)
		att, err := ingestRemoteMedia(
			context.Background(),
			"slack",
			sourceURL,
			name,
			file.Mimetype,
			map[string]string{"Authorization": "Bearer " + c.botToken},
		)
		if err != nil {
			slog.Warn("slack: failed to ingest attachment", "file", file.Name, "err", err)
			att = failedAttachment(name, file.Mimetype, int64(file.Size), err)
		}
		out = append(out, att)
	}
	return out
}

func (c *SlackChannel) enrichSlackMessageText(parent context.Context, text, channelID, timestamp, threadTS string, attachments []slack.Attachment) string {
	parts := []string{strings.TrimSpace(text)}
	for _, attachment := range attachments {
		if formatted := formatSlackAttachment(attachment); formatted != "" {
			parts = append(parts, formatted)
		}
	}

	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()

	refs := extractSlackMessageReferences(text, attachments)
	if strings.TrimSpace(threadTS) != "" && strings.TrimSpace(timestamp) != "" && threadTS != timestamp {
		refs = append(refs, slackMessageReference{
			ChannelID: channelID,
			Timestamp: threadTS,
			ThreadTS:  threadTS,
			Kind:      "current thread",
		})
	}

	seen := map[string]struct{}{}
	for _, ref := range refs {
		if ref.ChannelID == "" || ref.Timestamp == "" {
			continue
		}
		key := ref.ChannelID + "/" + ref.Timestamp + "/" + ref.ThreadTS
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		formatted, err := c.fetchSlackReference(ctx, ref)
		if err != nil {
			c.logf("slack: failed to fetch referenced message channel=%s ts=%s: %v", ref.ChannelID, ref.Timestamp, err)
			continue
		}
		if formatted != "" {
			parts = append(parts, formatted)
		}
	}

	return strings.TrimSpace(strings.Join(nonEmptyStrings(parts), "\n\n"))
}

type slackMessageReference struct {
	ChannelID string
	Timestamp string
	ThreadTS  string
	Kind      string
}

func extractSlackMessageReferences(text string, attachments []slack.Attachment) []slackMessageReference {
	candidates := []string{text}
	for _, attachment := range attachments {
		candidates = append(candidates, attachment.FromURL, attachment.OriginalURL, attachment.TitleLink, attachment.AuthorLink)
	}
	refs := make([]slackMessageReference, 0)
	for _, candidate := range candidates {
		for _, rawURL := range extractSlackURLs(candidate) {
			ref, ok := parseSlackMessageURL(rawURL)
			if ok {
				refs = append(refs, ref)
			}
		}
	}
	return refs
}

var slackMessageURLPattern = regexp.MustCompile(`https://[^\s>|]+\.slack\.com/archives/[A-Z0-9]+/p[0-9]{10,}[^\s>|]*`)

func extractSlackURLs(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	matches := slackMessageURLPattern.FindAllString(value, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, strings.TrimRight(match, ".,);]"))
	}
	return out
}

func parseSlackMessageURL(raw string) (slackMessageReference, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || !strings.HasSuffix(strings.ToLower(u.Hostname()), ".slack.com") {
		return slackMessageReference{}, false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "archives" || !strings.HasPrefix(parts[2], "p") {
		return slackMessageReference{}, false
	}
	ts, ok := timestampFromSlackPermalink(parts[2])
	if !ok {
		return slackMessageReference{}, false
	}
	ref := slackMessageReference{
		ChannelID: parts[1],
		Timestamp: ts,
		Kind:      "linked message",
	}
	if threadTS := strings.TrimSpace(u.Query().Get("thread_ts")); threadTS != "" {
		ref.ThreadTS = threadTS
	}
	if cid := strings.TrimSpace(u.Query().Get("cid")); cid != "" {
		ref.ChannelID = cid
	}
	return ref, true
}

func timestampFromSlackPermalink(raw string) (string, bool) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "p")
	if len(raw) < 11 {
		return "", false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	sec := raw[:10]
	frac := raw[10:]
	for len(frac) < 6 {
		frac += "0"
	}
	return sec + "." + frac[:6], true
}

func (c *SlackChannel) fetchSlackReference(ctx context.Context, ref slackMessageReference) (string, error) {
	threadTS := strings.TrimSpace(ref.ThreadTS)
	if threadTS != "" {
		msgs, _, _, err := c.client.GetConversationRepliesContext(ctx, &slack.GetConversationRepliesParameters{
			ChannelID: ref.ChannelID,
			Timestamp: threadTS,
			Inclusive: true,
			Limit:     50,
		})
		if err != nil {
			return "", err
		}
		return c.formatSlackMessages(ref, msgs), nil
	}

	resp, err := c.client.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{
		ChannelID: ref.ChannelID,
		Oldest:    ref.Timestamp,
		Latest:    ref.Timestamp,
		Inclusive: true,
		Limit:     1,
	})
	if err != nil {
		return "", err
	}
	if resp == nil || len(resp.Messages) == 0 {
		return "", nil
	}
	msg := resp.Messages[0]
	if msg.ReplyCount > 0 {
		msgs, _, _, err := c.client.GetConversationRepliesContext(ctx, &slack.GetConversationRepliesParameters{
			ChannelID: ref.ChannelID,
			Timestamp: ref.Timestamp,
			Inclusive: true,
			Limit:     50,
		})
		if err != nil {
			return c.formatSlackMessages(ref, []slack.Message{msg}), nil
		}
		return c.formatSlackMessages(ref, msgs), nil
	}
	return c.formatSlackMessages(ref, []slack.Message{msg}), nil
}

func (c *SlackChannel) formatSlackMessages(ref slackMessageReference, msgs []slack.Message) string {
	if len(msgs) == 0 {
		return ""
	}
	title := strings.TrimSpace(ref.Kind)
	if title == "" {
		title = "Slack message"
	}
	lines := []string{fmt.Sprintf("[%s: %s %s]", title, ref.ChannelID, firstNonEmpty(ref.ThreadTS, ref.Timestamp))}
	for _, msg := range msgs {
		content := formatSlackMessage(msg.Msg)
		if content == "" {
			continue
		}
		user := firstNonEmpty(c.displayNameForUser(msg.User), msg.Username, msg.BotID, "unknown")
		lines = append(lines, fmt.Sprintf("%s: %s", user, content))
	}
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func formatSlackMessage(msg slack.Msg) string {
	parts := []string{strings.TrimSpace(msg.Text)}
	for _, attachment := range msg.Attachments {
		if formatted := formatSlackAttachment(attachment); formatted != "" {
			parts = append(parts, formatted)
		}
	}
	return strings.Join(nonEmptyStrings(parts), "\n")
}

func formatSlackAttachment(attachment slack.Attachment) string {
	parts := []string{}
	for _, value := range []string{
		attachment.Pretext,
		attachment.AuthorName,
		attachment.Title,
		attachment.Text,
		attachment.Fallback,
	} {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, value)
		}
	}
	for _, field := range attachment.Fields {
		title := strings.TrimSpace(field.Title)
		value := strings.TrimSpace(field.Value)
		switch {
		case title != "" && value != "":
			parts = append(parts, title+": "+value)
		case value != "":
			parts = append(parts, value)
		}
	}
	return strings.Join(nonEmptyStrings(parts), "\n")
}

func nonEmptyStrings(values []string) []string {
	out := values[:0]
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func parseSlackTimestamp(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	secs, frac, _ := strings.Cut(raw, ".")
	secVal, err := strconv.ParseInt(secs, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	nsec := int64(0)
	if frac != "" {
		if len(frac) > 9 {
			frac = frac[:9]
		}
		for len(frac) < 9 {
			frac += "0"
		}
		nsec, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return time.Time{}, false
		}
	}
	return time.Unix(secVal, nsec).UTC(), true
}

func (c *SlackChannel) allowedEntries() []config.AllowFromEntry {
	c.identityMu.RLock()
	defer c.identityMu.RUnlock()
	if len(c.resolvedAllowFrom) == 0 {
		return c.allowFrom
	}
	return c.resolvedAllowFrom
}

// resolvedEntriesForRouting applies the shared connection's identity cache to
// one agent's allowFrom rules before the per-agent routing check.
func (c *SlackChannel) resolvedEntriesForRouting(entries []config.AllowFromEntry) []config.AllowFromEntry {
	c.identityMu.RLock()
	defer c.identityMu.RUnlock()
	return c.resolveAllowEntries(entries)
}

func (c *SlackChannel) displayNameForUser(userID string) string {
	c.identityMu.RLock()
	defer c.identityMu.RUnlock()
	if name := strings.TrimSpace(c.userNames[userID]); name != "" {
		return name
	}
	return userID
}

func (c *SlackChannel) refreshIdentityCache(ctx context.Context) error {
	users, err := c.client.GetUsersContext(ctx, slack.GetUsersOptionLimit(200))
	if err != nil {
		return fmt.Errorf("users.list: %w", err)
	}
	conversations, err := c.client.GetAllConversationsContext(
		ctx,
		slack.GetConversationsOptionTypes([]string{"public_channel", "private_channel"}),
		slack.GetConversationsOptionExcludeArchived(true),
		slack.GetConversationsOptionLimit(200),
	)
	if err != nil {
		return fmt.Errorf("conversations.list: %w", err)
	}

	userAliases, userNames := slackUserIdentities(users)
	channelAliases, channelNames := slackChannelIdentities(conversations)

	c.identityMu.Lock()
	c.userAliases = userAliases
	c.userNames = userNames
	c.channelAliases = channelAliases
	c.channelNames = channelNames
	c.resolvedAllowFrom = c.resolveAllowEntries(c.allowFrom)
	c.identityMu.Unlock()
	return nil
}

// slackUserIdentities builds the normalized alias -> user ID map and the
// user ID -> display name map for active users.
func slackUserIdentities(users []slack.User) (aliases, names map[string]string) {
	aliases = map[string]string{}
	names = map[string]string{}
	for _, user := range users {
		if strings.TrimSpace(user.ID) == "" || user.Deleted {
			continue
		}
		names[user.ID] = firstNonEmpty(
			strings.TrimSpace(user.Profile.DisplayName),
			strings.TrimSpace(user.RealName),
			strings.TrimSpace(user.Name),
			user.ID,
		)
		addSlackAliases(aliases, user.ID,
			user.ID,
			user.Name,
			"@"+user.Name,
			user.Profile.DisplayName,
			"@"+user.Profile.DisplayName,
			user.Profile.DisplayNameNormalized,
			"@"+user.Profile.DisplayNameNormalized,
			user.RealName,
			user.Profile.RealNameNormalized,
		)
	}
	return aliases, names
}

// slackChannelIdentities builds the normalized alias -> channel ID map and the
// channel ID -> name map.
func slackChannelIdentities(conversations []slack.Channel) (aliases, names map[string]string) {
	aliases = map[string]string{}
	names = map[string]string{}
	for _, channel := range conversations {
		if strings.TrimSpace(channel.ID) == "" {
			continue
		}
		names[channel.ID] = firstNonEmpty(
			strings.TrimSpace(channel.Name),
			strings.TrimSpace(channel.NameNormalized),
			channel.ID,
		)
		addSlackAliases(aliases, channel.ID,
			channel.ID,
			channel.Name,
			"#"+channel.Name,
			channel.NameNormalized,
			"#"+channel.NameNormalized,
		)
	}
	return aliases, names
}

// addSlackAliases maps each non-empty normalized alias to id.
func addSlackAliases(dst map[string]string, id string, aliases ...string) {
	for _, alias := range aliases {
		if normalized := normalizeSlackAlias(alias); normalized != "" {
			dst[normalized] = id
		}
	}
}

func normalizeSlackAlias(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	return strings.TrimPrefix(value, "#")
}

func (c *SlackChannel) resolveAllowEntries(entries []config.AllowFromEntry) []config.AllowFromEntry {
	if len(entries) == 0 {
		return nil
	}
	resolved := make([]config.AllowFromEntry, 0, len(entries))
	for _, entry := range entries {
		entry.From = c.resolveAllowCSV(entry.From, true)
		entry.AllowedGroups = c.resolveAllowCSV(entry.AllowedGroups, false)
		resolved = append(resolved, entry)
	}
	return resolved
}

func (c *SlackChannel) resolveAllowCSV(raw string, allowUsers bool) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	seen := map[string]struct{}{}
	values := make([]string, 0)
	for _, part := range splitFrom(raw) {
		for _, resolved := range c.expandSlackAlias(part, allowUsers) {
			if _, ok := seen[resolved]; ok {
				continue
			}
			seen[resolved] = struct{}{}
			values = append(values, resolved)
		}
	}
	return strings.Join(values, ",")
}

func (c *SlackChannel) expandSlackAlias(value string, allowUsers bool) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if value == "*" {
		return []string{"*"}
	}
	out := []string{value}
	normalized := normalizeSlackAlias(value)
	if normalized == "" {
		return out
	}
	if channelID, ok := c.channelAliases[normalized]; ok && channelID != value {
		out = append(out, channelID)
	}
	if allowUsers {
		if userID, ok := c.userAliases[normalized]; ok && userID != value {
			out = append(out, userID)
		}
	}
	return out
}

func (c *SlackChannel) resolveDeliveryTarget(ctx context.Context, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("slack delivery target is required")
	}
	c.identityMu.RLock()
	channelID, hasChannel := c.channelAliases[normalizeSlackAlias(raw)]
	userID, hasUser := c.userAliases[normalizeSlackAlias(raw)]
	c.identityMu.RUnlock()

	switch {
	case hasChannel:
		return channelID, nil
	case strings.HasPrefix(raw, "C"), strings.HasPrefix(raw, "G"), strings.HasPrefix(raw, "D"):
		return raw, nil
	case hasUser:
		return c.openDirectConversation(ctx, userID)
	case strings.HasPrefix(raw, "U"), strings.HasPrefix(raw, "W"):
		return c.openDirectConversation(ctx, raw)
	default:
		return raw, nil
	}
}

func (c *SlackChannel) openDirectConversation(ctx context.Context, userID string) (string, error) {
	channel, _, _, err := c.client.OpenConversationContext(ctx, &slack.OpenConversationParameters{
		Users:    []string{userID},
		ReturnIM: true,
	})
	if err != nil {
		return "", fmt.Errorf("opening Slack DM with %s: %w", userID, err)
	}
	if channel == nil || strings.TrimSpace(channel.ID) == "" {
		return "", fmt.Errorf("opening Slack DM with %s returned no channel", userID)
	}
	return channel.ID, nil
}
