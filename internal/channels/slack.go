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
	stopOnce        sync.Once
	cancel          context.CancelFunc
	logSinkMu       sync.RWMutex
	logSink         *LogSink
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
	_, _, err = c.client.PostMessage(resolvedChannel, slack.MsgOptionText(text, false))
	return err
}

// SendAndGetID posts a message and returns the Slack message timestamp, which
// serves as the message ID for EditMessage.
func (c *SlackChannel) SendAndGetID(channel, text string) (string, error) {
	resolvedChannel, err := c.resolveDeliveryTarget(context.Background(), channel)
	if err != nil {
		return "", err
	}
	_, timestamp, err := c.client.PostMessage(resolvedChannel, slack.MsgOptionText(text, false))
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
		slack.MsgOptionText(text, false),
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
		slack.MsgOptionText(fallbackText, false),
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
	_, _, _, err = c.client.UpdateMessage(resolvedChannel, msgID, slack.MsgOptionText(text, false))
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
		slack.MsgOptionText(fallbackText, false),
		slack.MsgOptionBlocks(blocks...),
	)
	return err
}

// ShowAssistantStatus reports whether Slack assistant status updates are
// enabled for this channel.
func (c *SlackChannel) ShowAssistantStatus() bool {
	return c.showStatus
}

// SendAssistantStatus updates Slack's native assistant thread status. Passing
// an empty status clears any existing indicator.
func (c *SlackChannel) SendAssistantStatus(channel, threadTS, status string) error {
	resolvedChannel, err := c.resolveDeliveryTarget(context.Background(), channel)
	if err != nil {
		return err
	}
	threadTS = strings.TrimSpace(threadTS)
	if threadTS == "" {
		return fmt.Errorf("slack assistant status requires a thread timestamp")
	}
	return c.client.SetAssistantThreadsStatusContext(context.Background(), slack.AssistantThreadsSetStatusParameters{
		ChannelID: resolvedChannel,
		ThreadTS:  threadTS,
		Status:    strings.TrimSpace(status),
	})
}

// Start connects via Socket Mode and blocks until ctx is cancelled.
func (c *SlackChannel) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.logf("slack: starting socket mode session")

	// Fetch the bot's own user ID so we can detect direct @mentions in groups.
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

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-c.sm.Events:
				if !ok {
					c.logf("slack: event stream closed")
					return
				}
				c.dispatch(evt)
			}
		}
	}()

	err := c.sm.RunContext(ctx)
	if err != nil && ctx.Err() == nil {
		c.logf("slack: socket mode exited with error: %v", err)
	} else {
		c.logf("slack: socket mode stopped")
	}
	return err
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

func (c *SlackChannel) dispatch(evt socketmode.Event) {
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
		return
	}
	if err := c.sm.Ack(*evt.Request); err != nil {
		c.logf("slack: failed to ack event: %v", err)
	}

	eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		return
	}
	switch inner := eventsAPI.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		c.handleMessageEvent(inner)
	case *slackevents.AppMentionEvent:
		c.handleAppMentionEvent(inner)
	}
}

func (c *SlackChannel) handleMessageEvent(event *slackevents.MessageEvent) {
	if event == nil {
		return
	}
	if normalized, ok := c.normalizeMessageRepliedEvent(context.Background(), event); ok {
		event = normalized
	}

	channelID := event.Channel
	from := event.User
	text := event.Text
	botID := event.BotID
	var files []slack.File
	var attachments []slack.Attachment
	isEdited := event.IsEdited() || (event.SubType == "message_changed" && event.Message != nil)
	if isEdited && event.Message != nil {
		if event.Message.Channel != "" {
			channelID = event.Message.Channel
		}
		if event.Message.User != "" {
			from = event.Message.User
		}
		if event.Message.Text != "" {
			text = event.Message.Text
		}
		if event.Message.BotID != "" {
			botID = event.Message.BotID
		}
		files = event.Message.Files
		attachments = event.Message.Attachments
	} else if event.Message != nil {
		files = event.Message.Files
		attachments = event.Message.Attachments
	}
	if botID != "" || (strings.TrimSpace(text) == "" && len(files) == 0 && len(attachments) == 0) || from == "" || channelID == "" {
		return
	}

	// Slack DM channels start with 'D'; everything else is a group/channel.
	isGroup := !strings.HasPrefix(channelID, "D")

	receivedAt := time.Now().UTC()
	rawTimestamp := event.TimeStamp
	threadTS := strings.TrimSpace(event.ThreadTimeStamp)
	if isEdited && event.Message != nil && event.Message.Timestamp != "" {
		rawTimestamp = event.Message.Timestamp
	}
	if isEdited && event.Message != nil && event.Message.ThreadTimestamp != "" {
		threadTS = strings.TrimSpace(event.Message.ThreadTimestamp)
	}
	if threadTS == "" {
		threadTS = rawTimestamp
	}
	isThreadReply := strings.TrimSpace(threadTS) != "" && strings.TrimSpace(rawTimestamp) != "" && threadTS != rawTimestamp
	if ts, ok := parseSlackTimestamp(rawTimestamp); ok {
		receivedAt = ts
	}
	enrichedText := c.enrichSlackMessageText(context.Background(), text, channelID, rawTimestamp, threadTS, attachments)

	// Log all group messages before allowFrom filtering.
	if isGroup {
		c.handlerMu.RLock()
		logFn := c.groupLogHandler
		c.handlerMu.RUnlock()
		if logFn != nil {
			logFn(IncomingMessage{
				Type:          "slack",
				From:          from,
				SenderName:    from,
				Channel:       channelID,
				ThreadTS:      threadTS,
				IsThreadReply: isThreadReply,
				Text:          enrichedText,
				ReceivedAt:    receivedAt,
			})
		}
	}

	result := checkAllowed(c.allowedEntries(), from, channelID, text, isGroup, c.botUserID, false)
	if !result.allowed && isThreadReply {
		result = checkAllowedReplyContinuation(c.allowedEntries(), from, channelID, isGroup)
	}
	if !result.allowed {
		c.logf("slack: ignored message from=%s channel=%s", from, channelID)
		return
	}

	c.handlerMu.RLock()
	fn := c.handler
	c.handlerMu.RUnlock()

	if fn != nil {
		mediaURL := c.firstImageDataURL(files)
		im := IncomingMessage{
			Type:          "slack",
			From:          from,
			SenderName:    c.displayNameForUser(from),
			Channel:       channelID,
			ThreadTS:      threadTS,
			IsThreadReply: isThreadReply,
			Text:          enrichedText,
			MediaURL:      mediaURL,
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
	} else {
		c.logf("slack: no message handler registered for from=%s", from)
		slog.Debug("slack: no handler registered", "from", from)
	}
}

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

func (c *SlackChannel) firstImageDataURL(files []slack.File) string {
	for _, file := range files {
		if !looksLikeImage(file.Mimetype, file.Name) {
			continue
		}
		sourceURL := strings.TrimSpace(file.URLPrivateDownload)
		if sourceURL == "" {
			sourceURL = strings.TrimSpace(file.URLPrivate)
		}
		if sourceURL == "" {
			continue
		}
		mediaURL, err := ingestRemoteMedia(
			context.Background(),
			"slack",
			sourceURL,
			firstNonEmpty(file.Name, file.Title),
			map[string]string{"Authorization": "Bearer " + c.botToken},
		)
		if err == nil {
			return mediaURL
		}
		slog.Warn("slack: failed to ingest image attachment", "file", file.Name, "err", err)
	}
	return ""
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

	userAliases := map[string]string{}
	userNames := map[string]string{}
	for _, user := range users {
		if strings.TrimSpace(user.ID) == "" || user.Deleted {
			continue
		}
		userNames[user.ID] = firstNonEmpty(
			strings.TrimSpace(user.Profile.DisplayName),
			strings.TrimSpace(user.RealName),
			strings.TrimSpace(user.Name),
			user.ID,
		)
		for _, alias := range []string{
			user.ID,
			user.Name,
			"@" + user.Name,
			user.Profile.DisplayName,
			"@" + user.Profile.DisplayName,
			user.Profile.DisplayNameNormalized,
			"@" + user.Profile.DisplayNameNormalized,
			user.RealName,
			user.Profile.RealNameNormalized,
		} {
			if normalized := normalizeSlackAlias(alias); normalized != "" {
				userAliases[normalized] = user.ID
			}
		}
	}

	channelAliases := map[string]string{}
	channelNames := map[string]string{}
	for _, channel := range conversations {
		if strings.TrimSpace(channel.ID) == "" {
			continue
		}
		channelNames[channel.ID] = firstNonEmpty(
			strings.TrimSpace(channel.Name),
			strings.TrimSpace(channel.NameNormalized),
			channel.ID,
		)
		for _, alias := range []string{
			channel.ID,
			channel.Name,
			"#" + channel.Name,
			channel.NameNormalized,
			"#" + channel.NameNormalized,
		} {
			if normalized := normalizeSlackAlias(alias); normalized != "" {
				channelAliases[normalized] = channel.ID
			}
		}
	}

	c.identityMu.Lock()
	c.userAliases = userAliases
	c.userNames = userNames
	c.channelAliases = channelAliases
	c.channelNames = channelNames
	c.resolvedAllowFrom = c.resolveAllowEntries(c.allowFrom)
	c.identityMu.Unlock()
	return nil
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
