package channels

import (
	"context"
	"fmt"
	"log/slog"
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
}

// NewSlackChannel creates a SlackChannel.
// appToken is the App-Level token (xapp-), botToken is the Bot token (xoxb-).
func NewSlackChannel(appToken, botToken string, allowFrom []config.AllowFromEntry, model string, fallbacks []string) *SlackChannel {
	api := slack.New(botToken, slack.OptionAppLevelToken(appToken))
	sm := socketmode.New(api)
	return &SlackChannel{
		appToken:  appToken,
		botToken:  botToken,
		allowFrom: allowFrom,
		model:     model,
		fallbacks: fallbacks,
		client:    api,
		sm:        sm,
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

// EditMessage updates a previously posted Slack message in place.
func (c *SlackChannel) EditMessage(channel, msgID, text string) error {
	resolvedChannel, err := c.resolveDeliveryTarget(context.Background(), channel)
	if err != nil {
		return err
	}
	_, _, _, err = c.client.UpdateMessage(resolvedChannel, msgID, slack.MsgOptionText(text, false))
	return err
}

// Start connects via Socket Mode and blocks until ctx is cancelled.
func (c *SlackChannel) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// Fetch the bot's own user ID so we can detect direct @mentions in groups.
	if resp, err := c.client.AuthTestContext(ctx); err == nil {
		c.botUserID = resp.UserID
	} else {
		slog.Warn("slack: auth.test failed; direct-mention detection disabled", "err", err)
	}
	if err := c.refreshIdentityCache(ctx); err != nil {
		slog.Warn("slack: failed to load users/channels; name-based routing disabled", "err", err)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-c.sm.Events:
				if !ok {
					return
				}
				c.dispatch(evt)
			}
		}
	}()

	return c.sm.RunContext(ctx)
}

// Stop disconnects from Slack.
func (c *SlackChannel) Stop() {
	c.stopOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
	})
}

func (c *SlackChannel) dispatch(evt socketmode.Event) {
	if evt.Type != socketmode.EventTypeEventsAPI {
		return
	}
	c.sm.Ack(*evt.Request)

	eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		return
	}
	inner, ok := eventsAPI.InnerEvent.Data.(*slackevents.MessageEvent)
	if !ok {
		return
	}
	c.handleMessageEvent(inner)
}

func (c *SlackChannel) handleMessageEvent(event *slackevents.MessageEvent) {
	if event == nil {
		return
	}

	channelID := event.Channel
	from := event.User
	text := event.Text
	botID := event.BotID
	var files []slack.File
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
	} else if event.Message != nil {
		files = event.Message.Files
	}
	if botID != "" || (strings.TrimSpace(text) == "" && len(files) == 0) || from == "" || channelID == "" {
		return
	}

	// Slack DM channels start with 'D'; everything else is a group/channel.
	isGroup := !strings.HasPrefix(channelID, "D")

	receivedAt := time.Now().UTC()
	rawTimestamp := event.TimeStamp
	if isEdited && event.Message != nil && event.Message.Timestamp != "" {
		rawTimestamp = event.Message.Timestamp
	}
	if ts, ok := parseSlackTimestamp(rawTimestamp); ok {
		receivedAt = ts
	}

	// Log all group messages before allowFrom filtering.
	if isGroup {
		c.handlerMu.RLock()
		logFn := c.groupLogHandler
		c.handlerMu.RUnlock()
		if logFn != nil {
			logFn(IncomingMessage{
				Type:       "slack",
				From:       from,
				SenderName: from,
				Channel:    channelID,
				Text:       text,
				ReceivedAt: receivedAt,
			})
		}
	}

	result := checkAllowed(c.allowedEntries(), from, channelID, text, isGroup, c.botUserID, false)
	if !result.allowed {
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
			Text:          text,
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
		slog.Debug("slack: no handler registered", "from", from)
	}
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
