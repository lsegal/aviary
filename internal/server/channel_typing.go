package server

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lsegal/aviary/internal/channels"
)

// channelTypingInterval is how often a turn re-sends its typing indicator.
// Signal clients drop the indicator after about 15 seconds without a refresh.
var channelTypingInterval = 10 * time.Second

// channelTyping keeps a channel's typing indicator on for one turn.
//
// Every send holds mu, and a refresh checks stopped while holding it, so the
// stop is always the last indicator the channel sends: a refresh that is
// already in flight finishes before the stop goes out, and no refresh starts
// after it. Signal sends each indicator on its own connection, so without
// this ordering a late refresh could reach the recipient after the stop and
// leave "typing…" showing after the reply.
type channelTyping struct {
	ts      channels.TypingSender
	channel string

	mu       sync.Mutex
	stopped  atomic.Bool
	done     chan struct{}
	stopOnce sync.Once
}

// startChannelTyping turns the indicator on and keeps it on until Stop is
// called or ctx ends.
func startChannelTyping(ctx context.Context, ts channels.TypingSender, channel string) *channelTyping {
	t := &channelTyping{
		ts:      ts,
		channel: channel,
		done:    make(chan struct{}),
	}
	t.refresh()
	go t.keepalive(ctx)
	return t
}

func (t *channelTyping) keepalive(ctx context.Context) {
	ticker := time.NewTicker(channelTypingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-t.done:
			return
		case <-ctx.Done():
			t.Stop()
			return
		case <-ticker.C:
			t.refresh()
		}
	}
}

func (t *channelTyping) refresh() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped.Load() {
		return
	}
	if err := t.ts.SendTyping(t.channel, false); err != nil {
		slog.Debug("server: failed to send typing indicator", "channel", t.channel, "err", err)
	}
}

// Stop turns the indicator off without waiting for the stop to be sent.
// Calling it again does nothing.
func (t *channelTyping) Stop() {
	t.stopOnce.Do(func() {
		t.stopped.Store(true)
		close(t.done)
		go func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			// A lost stop leaves the indicator on until the client times it
			// out, so retry once.
			for attempt := 0; attempt < 2; attempt++ {
				err := t.ts.SendTyping(t.channel, true)
				if err == nil {
					return
				}
				slog.Warn("server: failed to stop typing indicator", "channel", t.channel, "attempt", attempt+1, "err", err)
			}
		}()
	})
}
