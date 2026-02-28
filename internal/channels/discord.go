package channels

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/bwmarrin/discordgo"
)

// DiscordChannel connects to Discord using a bot token.
type DiscordChannel struct {
	token     string
	allowFrom []string

	session   *discordgo.Session
	handler   func(IncomingMessage)
	handlerMu sync.RWMutex
	stopOnce  sync.Once
	done      chan struct{}
}

// NewDiscordChannel creates a DiscordChannel with the given bot token.
func NewDiscordChannel(token string, allowFrom []string) *DiscordChannel {
	return &DiscordChannel{
		token:     token,
		allowFrom: allowFrom,
		done:      make(chan struct{}),
	}
}

// OnMessage registers a callback for incoming messages.
func (c *DiscordChannel) OnMessage(fn func(IncomingMessage)) {
	c.handlerMu.Lock()
	defer c.handlerMu.Unlock()
	c.handler = fn
}

// Send posts a message to a Discord channel by ID.
func (c *DiscordChannel) Send(channelID, text string) error {
	if c.session == nil {
		return fmt.Errorf("discord: not connected")
	}
	_, err := c.session.ChannelMessageSend(channelID, text)
	return err
}

// Start opens the Discord WebSocket connection.
func (c *DiscordChannel) Start(ctx context.Context) error {
	s, err := discordgo.New("Bot " + c.token)
	if err != nil {
		return fmt.Errorf("discord: creating session: %w", err)
	}
	c.session = s

	s.AddHandler(func(_ *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author.Bot {
			return
		}
		if !c.allowed(m.Author.ID) {
			return
		}
		c.handlerMu.RLock()
		fn := c.handler
		c.handlerMu.RUnlock()
		if fn != nil {
			fn(IncomingMessage{
				From:    m.Author.ID,
				Channel: m.ChannelID,
				Text:    m.Content,
			})
		} else {
			slog.Debug("discord: no handler registered", "from", m.Author.ID)
		}
	})

	s.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages
	if err := s.Open(); err != nil {
		return fmt.Errorf("discord: opening session: %w", err)
	}

	// Block until context is done.
	select {
	case <-ctx.Done():
	case <-c.done:
	}
	return s.Close()
}

// Stop disconnects from Discord.
func (c *DiscordChannel) Stop() {
	c.stopOnce.Do(func() { close(c.done) })
}

func (c *DiscordChannel) allowed(userID string) bool {
	if len(c.allowFrom) == 0 {
		return false
	}
	for _, a := range c.allowFrom {
		if a == "*" || a == userID {
			return true
		}
	}
	return false
}
