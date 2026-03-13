// Package channels implements messaging channel integrations.
package channels

import (
	"context"
	"time"
)

// IncomingMessage represents a message received on a channel.
type IncomingMessage struct {
	Type          string // channel type: "discord", "slack", "signal", etc.
	From          string // user ID or name
	Channel       string // channel ID or name
	Text          string
	ReceivedAt    time.Time
	RestrictTools []string // per-entry tool allow-list override; nil means use agent defaults
	DisabledTools []string // per-channel tool deny-list override; applied after the allow-list
	Model         string   // per-entry model override; "" means use agent default
	Fallbacks     []string // per-entry fallbacks override; nil means use agent defaults
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

// DaemonInfo describes a daemon process associated with a channel.
// For managed daemons (aviary-launched), PID and Started are populated.
// For external daemons, only Addr is set (PID=0).
type DaemonInfo struct {
	PID      int       `json:"pid"`
	Addr     string    `json:"addr"`
	Started  time.Time `json:"started"`
	External bool      `json:"external"` // true = aviary did not launch this process
}

// DaemonProvider is an optional interface implemented by channels that manage
// a subprocess daemon. Returns nil when the daemon is not currently running.
type DaemonProvider interface {
	DaemonInfo() *DaemonInfo
}

// LogSinkSetter is an optional interface for channels that capture subprocess
// stdout/stderr. The manager calls SetLogSink before starting the channel.
type LogSinkSetter interface {
	SetLogSink(s *LogSink)
}

// TypingSender is an optional interface implemented by channels that support
// typing indicators. SendTyping signals that the agent is composing a reply;
// pass stop=true to cancel the indicator. ShowTyping reports whether the typing
// indicator is enabled per the channel's configuration.
type TypingSender interface {
	ShowTyping() bool
	SendTyping(channel string, stop bool) error
}

// MediaSender is an optional interface implemented by channels that support
// sending media attachments (images, files, etc.).
// filePath is the local filesystem path to the file to send.
// caption is an optional text message accompanying the file.
type MediaSender interface {
	SendMedia(channel, caption, filePath string) error
}
