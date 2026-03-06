package channels

import (
	"context"
	"log/slog"
	"sync"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// SlackChannel connects to Slack using Socket Mode (no public URL required).
type SlackChannel struct {
	appToken  string // xapp-... token for socket mode
	botToken  string // xoxb-... token for posting
	allowFrom []string

	client    *slack.Client
	sm        *socketmode.Client
	handler   func(IncomingMessage)
	handlerMu sync.RWMutex
	stopOnce  sync.Once
	cancel    context.CancelFunc
}

// NewSlackChannel creates a SlackChannel.
// appToken is the App-Level token (xapp-), botToken is the Bot token (xoxb-).
func NewSlackChannel(appToken, botToken string, allowFrom []string) *SlackChannel {
	api := slack.New(botToken, slack.OptionAppLevelToken(appToken))
	sm := socketmode.New(api)
	return &SlackChannel{
		appToken:  appToken,
		botToken:  botToken,
		allowFrom: allowFrom,
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
	if !ok || inner.BotID != "" {
		return // ignore bot messages
	}

	if !c.allowed(inner.User) {
		return
	}

	c.handlerMu.RLock()
	fn := c.handler
	c.handlerMu.RUnlock()

	if fn != nil {
		fn(IncomingMessage{
			From:    inner.User,
			Channel: inner.Channel,
			Text:    inner.Text,
		})
	} else {
		slog.Debug("slack: no handler registered", "from", inner.User)
	}
}

func (c *SlackChannel) allowed(user string) bool {
	if len(c.allowFrom) == 0 {
		return false
	}
	for _, a := range c.allowFrom {
		if a == "*" || a == user {
			return true
		}
	}
	return false
}
