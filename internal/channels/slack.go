package channels

import (
	"context"
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

	botUserID string // populated on connect via auth.test

	client    *slack.Client
	sm        *socketmode.Client
	handler   func(IncomingMessage)
	handlerMu sync.RWMutex
	stopOnce  sync.Once
	cancel    context.CancelFunc
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

// Send posts a message to a Slack channel.
func (c *SlackChannel) Send(channel, text string) error {
	_, _, err := c.client.PostMessage(channel, slack.MsgOptionText(text, false))
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
	}
	if botID != "" || strings.TrimSpace(text) == "" || from == "" || channelID == "" {
		return
	}

	// Slack DM channels start with 'D'; everything else is a group/channel.
	isGroup := !strings.HasPrefix(channelID, "D")
	result := checkAllowed(c.allowFrom, from, channelID, text, isGroup, c.botUserID, false)
	if !result.allowed {
		return
	}

	c.handlerMu.RLock()
	fn := c.handler
	c.handlerMu.RUnlock()

	if fn != nil {
		receivedAt := time.Now().UTC()
		rawTimestamp := event.TimeStamp
		if isEdited && event.Message != nil && event.Message.Timestamp != "" {
			rawTimestamp = event.Message.Timestamp
		}
		if ts, ok := parseSlackTimestamp(rawTimestamp); ok {
			receivedAt = ts
		}
		im := IncomingMessage{
			Type:          "slack",
			From:          from,
			Channel:       channelID,
			Text:          text,
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
