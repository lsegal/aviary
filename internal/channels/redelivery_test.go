package channels

import (
	"testing"

	"github.com/lsegal/aviary/internal/config"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
)

func TestDiscordChannel_RedeliveredMessageStartsOneRun(t *testing.T) {
	ch := NewDiscordChannel("t", []config.AllowFromEntry{{From: "*", AllowedGroups: "*"}}, "m", nil)
	var messages []IncomingMessage
	ch.OnMessage(func(m IncomingMessage) { messages = append(messages, m) })
	msg := func(content string) *discordgo.Message {
		return &discordgo.Message{ID: "M1", Author: &discordgo.User{ID: "U123"}, ChannelID: "C123", Content: content}
	}

	assert.True(t, ch.handleMessage(msg("file the bug"), "BOT"))
	// A MESSAGE_UPDATE for an embed loading, and a replay after a resume,
	// carry the same content.
	assert.False(t, ch.handleMessage(msg("file the bug"), "BOT"))
	assert.False(t, ch.handleMessage(msg("file the bug"), "BOT"))
	// An edit changes the content and runs again.
	assert.True(t, ch.handleMessage(msg("file the bug, please"), "BOT"))

	assert.Len(t, messages, 2)
}

func TestSignalChannel_RedeliveredEnvelopeStartsOneRun(t *testing.T) {
	ch := NewSignalChannel("+12130000000", "", []config.AllowFromEntry{{From: "*"}}, false, false, false, false, "m", nil)
	var messages []IncomingMessage
	ch.OnMessage(func(m IncomingMessage) { messages = append(messages, m) })

	line := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+18005551234","timestamp":1710000000000,"dataMessage":{"message":"file the bug"}}}}`
	ch.dispatch([]byte(line))
	ch.dispatch([]byte(line))
	ch.dispatch([]byte(line))
	// A new message with the same text has its own timestamp.
	ch.dispatch([]byte(`{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+18005551234","timestamp":1710000005000,"dataMessage":{"message":"file the bug"}}}}`))

	assert.Len(t, messages, 2)
}
