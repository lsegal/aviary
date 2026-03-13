package channels

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/lsegal/aviary/internal/config"
)

// DiscordChannel connects to Discord using a bot token.
type DiscordChannel struct {
	token         string
	allowFrom     []config.AllowFromEntry
	model         string
	fallbacks     []string
	disabledTools []string

	session   *discordgo.Session
	handler   func(IncomingMessage)
	handlerMu sync.RWMutex
	stopOnce  sync.Once
	done      chan struct{}
}

// NewDiscordChannel creates a DiscordChannel with the given bot token.
func NewDiscordChannel(token string, allowFrom []config.AllowFromEntry, model string, fallbacks []string) *DiscordChannel {
	return &DiscordChannel{
		token:     token,
		allowFrom: allowFrom,
		model:     model,
		fallbacks: fallbacks,
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
		botUserID := c.discordBotUserID()
		if !c.handleMessage(m.Message, botUserID) {
			return
		}
	})

	s.AddHandler(func(_ *discordgo.Session, m *discordgo.MessageUpdate) {
		if m == nil {
			return
		}
		msg := m.Message
		if msg == nil {
			msg = &discordgo.Message{}
		}
		if m.BeforeUpdate != nil {
			if msg.Author == nil {
				msg.Author = m.BeforeUpdate.Author
			}
			if msg.ChannelID == "" {
				msg.ChannelID = m.BeforeUpdate.ChannelID
			}
			if msg.GuildID == "" {
				msg.GuildID = m.BeforeUpdate.GuildID
			}
		}
		c.handleMessage(msg, c.discordBotUserID())
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

func (c *DiscordChannel) discordBotUserID() string {
	if c.session == nil || c.session.State == nil || c.session.State.User == nil {
		return ""
	}
	return c.session.State.User.ID
}

func (c *DiscordChannel) handleMessage(msg *discordgo.Message, botUserID string) bool {
	if msg == nil || msg.Author == nil || msg.Author.Bot || strings.TrimSpace(msg.Content) == "" {
		return false
	}

	// Messages sent in a guild channel have a non-empty GuildID.
	isGroup := msg.GuildID != ""
	result := checkAllowed(c.allowFrom, msg.Author.ID, msg.ChannelID, msg.Content, isGroup, botUserID, false)
	if !result.allowed {
		return false
	}

	c.handlerMu.RLock()
	fn := c.handler
	c.handlerMu.RUnlock()
	if fn != nil {
		receivedAt := time.Now().UTC()
		if !msg.Timestamp.IsZero() {
			receivedAt = msg.Timestamp.UTC()
		}
		im := IncomingMessage{
			Type:          "discord",
			From:          msg.Author.ID,
			Channel:       msg.ChannelID,
			Text:          msg.Content,
			ReceivedAt:    receivedAt,
			RestrictTools: result.restrictTools,
			DisabledTools: c.disabledTools,
			Model:         result.model,
			Fallbacks:     result.fallbacks,
		}
		if im.Model == "" {
			im.Model = c.model
		}
		if len(im.Fallbacks) == 0 {
			im.Fallbacks = c.fallbacks
		}
		fn(im)
	} else {
		slog.Debug("discord: no handler registered", "from", msg.Author.ID)
	}
	return true
}
