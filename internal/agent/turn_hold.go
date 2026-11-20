package agent

import (
	"context"
	"sync"
)

// TurnHold pauses a running turn's tool calls from outside the turn. While it
// is engaged, a turn carrying it starts no new tool call: each one waits until
// the hold is released or the turn is cancelled. A call already running is not
// interrupted. The zero value is a released hold, and a nil hold never waits.
type TurnHold struct {
	mu      sync.Mutex
	engaged bool
	release chan struct{}
}

// Engage makes tool calls wait until Release.
func (h *TurnHold) Engage() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.engaged {
		h.engaged = true
		h.release = make(chan struct{})
	}
}

// Release lets waiting and later tool calls run.
func (h *TurnHold) Release() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.engaged {
		h.engaged = false
		close(h.release)
	}
}

// Engaged reports whether tool calls are being held.
func (h *TurnHold) Engaged() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.engaged
}

// Wait returns once the hold is released, or with ctx's error once ctx ends
// first.
func (h *TurnHold) Wait(ctx context.Context) error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	engaged, release := h.engaged, h.release
	h.mu.Unlock()
	if !engaged {
		return nil
	}
	select {
	case <-release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type turnHoldKey struct{}

// WithTurnHold makes the tool calls of a turn run with ctx wait on hold.
func WithTurnHold(ctx context.Context, hold *TurnHold) context.Context {
	if hold == nil {
		return ctx
	}
	return context.WithValue(ctx, turnHoldKey{}, hold)
}

// TurnHoldFromContext returns the turn's hold, or nil when it has none.
func TurnHoldFromContext(ctx context.Context) *TurnHold {
	hold, _ := ctx.Value(turnHoldKey{}).(*TurnHold)
	return hold
}
