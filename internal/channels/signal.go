package channels

import (
	"context"
	"log/slog"
)

// SignalChannel is a deferred stub. Signal integration requires a running
// signal-cli or signald daemon, which is platform-specific and out of scope
// for the initial implementation.
type SignalChannel struct {
	phone string
}

// NewSignalChannel creates a stub SignalChannel.
func NewSignalChannel(phone string, _ []string) *SignalChannel {
	return &SignalChannel{phone: phone}
}

// OnMessage is a no-op stub.
func (c *SignalChannel) OnMessage(_ func(IncomingMessage)) {}

// Send logs a warning; Signal is not yet implemented.
func (c *SignalChannel) Send(_, text string) error {
	slog.Warn("signal: send not implemented", "text", text)
	return nil
}

// Start logs a warning and blocks until ctx is done.
func (c *SignalChannel) Start(ctx context.Context) error {
	slog.Warn("signal channel: not yet implemented; deferred to Phase 11")
	<-ctx.Done()
	return nil
}

// Stop is a no-op.
func (c *SignalChannel) Stop() {}
