package channels

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

	session         *discordgo.Session
	handler         func(IncomingMessage)
	groupLogHandler func(IncomingMessage)
	handlerMu       sync.RWMutex
	stopOnce        sync.Once
	done            chan struct{}
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

// OnGroupChatMessage registers a callback invoked for all group messages before
// allowFrom filtering, enabling a full channel transcript to be maintained.
func (c *DiscordChannel) OnGroupChatMessage(fn func(IncomingMessage)) {
	c.handlerMu.Lock()
	defer c.handlerMu.Unlock()
	c.groupLogHandler = fn
}

// Send posts a message to a Discord channel by ID.
func (c *DiscordChannel) Send(channelID, text string) error {
	if c.session == nil {
		return fmt.Errorf("discord: not connected")
	}
	_, err := c.session.ChannelMessageSend(channelID, text)
	return err
}

// SendAndGetID posts a message and returns the Discord message ID, which can
// later be passed to EditMessage.
func (c *DiscordChannel) SendAndGetID(channelID, text string) (string, error) {
	if c.session == nil {
		return "", fmt.Errorf("discord: not connected")
	}
	msg, err := c.session.ChannelMessageSend(channelID, text)
	if err != nil {
		return "", err
	}
	return msg.ID, nil
}

// EditMessage updates a previously posted Discord message in place.
func (c *DiscordChannel) EditMessage(channelID, msgID, text string) error {
	if c.session == nil {
		return fmt.Errorf("discord: not connected")
	}
	_, err := c.session.ChannelMessageEditComplex(discordgo.NewMessageEdit(channelID, msgID).SetContent(text))
	return err
}

// SendMedia sends a file attachment with an optional caption to a Discord channel.
func (c *DiscordChannel) SendMedia(channelID, caption, filePath string) error {
	if c.session == nil {
		return fmt.Errorf("discord: not connected")
	}
	if strings.TrimSpace(filePath) == "" {
		return fmt.Errorf("discord: file path is required")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close() //nolint:errcheck

	name := filepath.Base(filePath)
	_, err = c.session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content: strings.TrimSpace(caption),
		Files: []*discordgo.File{{
			Name:   name,
			Reader: file,
		}},
	})
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

	s.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent
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
	if msg == nil || msg.Author == nil || msg.Author.Bot || (strings.TrimSpace(msg.Content) == "" && len(msg.Attachments) == 0) {
		return false
	}

	// Messages sent in a guild channel have a non-empty GuildID.
	isGroup := msg.GuildID != ""

	receivedAt := time.Now().UTC()
	if !msg.Timestamp.IsZero() {
		receivedAt = msg.Timestamp.UTC()
	}
	mediaURL := firstDiscordImageDataURL(msg.Attachments)
	senderName := strings.TrimSpace(msg.Author.GlobalName)
	if senderName == "" {
		senderName = strings.TrimSpace(msg.Author.Username)
	}

	// Log all group messages before allowFrom filtering.
	if isGroup {
		c.handlerMu.RLock()
		logFn := c.groupLogHandler
		c.handlerMu.RUnlock()
		if logFn != nil {
			logFn(IncomingMessage{
				Type:       "discord",
				From:       msg.Author.ID,
				SenderName: senderName,
				Channel:    msg.ChannelID,
				Text:       msg.Content,
				ReceivedAt: receivedAt,
			})
		}
	}

	result := checkAllowed(c.allowFrom, msg.Author.ID, msg.ChannelID, msg.Content, isGroup, botUserID, false)
	if !result.allowed {
		return false
	}

	c.handlerMu.RLock()
	fn := c.handler
	c.handlerMu.RUnlock()
	if fn != nil {
		im := IncomingMessage{
			Type:          "discord",
			From:          msg.Author.ID,
			SenderName:    senderName,
			Channel:       msg.ChannelID,
			Text:          msg.Content,
			MediaURL:      mediaURL,
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

func firstDiscordImageDataURL(attachments []*discordgo.MessageAttachment) string {
	for _, attachment := range attachments {
		if attachment == nil || !looksLikeImage(attachment.ContentType, attachment.Filename) {
			continue
		}
		sourceURL := strings.TrimSpace(attachment.URL)
		if sourceURL == "" {
			sourceURL = strings.TrimSpace(attachment.ProxyURL)
		}
		if sourceURL == "" {
			continue
		}
		mediaURL, err := ingestRemoteMedia(context.Background(), "discord", sourceURL, attachment.Filename, nil)
		if err == nil {
			return mediaURL
		}
		slog.Warn("discord: failed to ingest image attachment", "file", attachment.Filename, "err", err)
	}
	return ""
}
