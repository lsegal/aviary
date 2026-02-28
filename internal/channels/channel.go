// Package channels implements messaging channel integrations.
package channels

import "context"

// IncomingMessage represents a message received on a channel.
type IncomingMessage struct {
	From    string // user ID or name
	Channel string // channel ID or name
	Text    string
}

// Channel is the interface implemented by all messaging channel backends.
type Channel interface {
	// Start connects to the channel and begins listening. Blocks until Stop.
	Start(ctx context.Context) error
	// Stop disconnects from the channel.
	Stop()
	// Send posts a text message to the given channel/conversation.
	Send(channel, text string) error
	// OnMessage registers a callback for incoming messages.
	OnMessage(fn func(IncomingMessage))
}
