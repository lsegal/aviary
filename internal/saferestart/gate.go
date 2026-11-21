package saferestart

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrRestarting is returned to a call the restart gate turned away because a
// restart holds, or is about to take, the exclusive lock. The caller can retry
// once the server is back.
var ErrRestarting = errors.New("server restarting, try again")

// Default restart gate timings.
const (
	// DefaultCallGrace is how long a pending restart keeps admitting new calls.
	DefaultCallGrace = time.Minute
	// DefaultCallMaxWait is how long a restart waits for in-flight calls
	// before it proceeds without them.
	DefaultCallMaxWait = 5 * time.Minute
	// DefaultCallAdmitWait is how long a call turned away by a pending or
	// running restart waits for the gate to reopen.
	DefaultCallAdmitWait = 30 * time.Second
)

// Gate is a reference-counted readers/writer lock between calls (shared
// holders) and a restart (the exclusive holder).
//
// A restart first becomes pending. A pending restart waits until no calls are
// active, then takes the lock. New calls are still admitted for the grace
// period after the restart became pending, so a short call is not refused for
// a restart that could wait; after it they are blocked, so a steady stream of
// calls cannot hold off the restart forever. A restart stops waiting for
// in-flight calls after its max wait, so a long call cannot either.
type Gate struct {
	mu      sync.Mutex
	changed chan struct{}
	active  int
	pending time.Time // when the pending restart was requested; zero when none
	locked  bool
	gen     uint64 // counts Lock calls, so a superseded one leaves the gate alone

	grace     time.Duration
	maxWait   time.Duration
	admitWait time.Duration
}

// NewGate returns an open gate using the default timings.
func NewGate() *Gate {
	return &Gate{
		changed:   make(chan struct{}),
		grace:     DefaultCallGrace,
		maxWait:   DefaultCallMaxWait,
		admitWait: DefaultCallAdmitWait,
	}
}

// MCPCalls guards every MCP tool call Aviary serves against restarts.
var MCPCalls = NewGate()

// SetTimings sets the grace period, the max wait, and how long a blocked call
// waits to be admitted. A zero or negative value selects the default.
func (g *Gate) SetTimings(grace, maxWait, admitWait time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.grace = orDefault(grace, DefaultCallGrace)
	g.maxWait = orDefault(maxWait, DefaultCallMaxWait)
	g.admitWait = orDefault(admitWait, DefaultCallAdmitWait)
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// notify wakes everyone waiting on a state change. g.mu must be held.
func (g *Gate) notify() {
	close(g.changed)
	g.changed = make(chan struct{})
}

// Active is the number of calls holding the gate.
func (g *Gate) Active() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active
}

// blocked reports whether a new call must wait. g.mu must be held.
func (g *Gate) blocked(now time.Time) bool {
	return g.locked || (!g.pending.IsZero() && now.Sub(g.pending) >= g.grace)
}

// Acquire admits a call as a shared holder. While a restart holds the lock,
// or has been pending longer than the grace period, the call waits up to the
// admit wait for the gate to reopen and then fails with ErrRestarting. The
// returned release must be called exactly once when the call ends; calling it
// again does nothing.
func (g *Gate) Acquire(ctx context.Context) (release func(), err error) {
	g.mu.Lock()
	var deadline <-chan time.Time
	for g.blocked(time.Now()) {
		if deadline == nil {
			timer := time.NewTimer(g.admitWait)
			defer timer.Stop()
			deadline = timer.C
		}
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
			return nil, ErrRestarting
		case <-changed:
		}
		g.mu.Lock()
	}
	g.active++
	g.notify()
	g.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			g.active--
			g.notify()
			g.mu.Unlock()
		})
	}, nil
}

// LockOptions configures Gate.Lock.
type LockOptions struct {
	// Force takes the lock without waiting for in-flight calls.
	Force bool
	// Cancel withdraws the pending restart when closed.
	Cancel <-chan struct{}
	// OnWait is called with the number of active calls whenever the pending
	// restart has to wait for them and that number changes.
	OnWait func(active int)
}

// Lock makes a restart pending, waits until no calls are active (at most the
// max wait), then takes the exclusive lock, so no new call starts until unlock
// is called. It also returns how many calls were still active when it took the
// lock, which is only nonzero when the max wait ran out or Force was set. It
// fails with ctx's error, or ErrCanceled when opts.Cancel closes, after
// reopening the gate. unlock, and a failed Lock, leave the gate alone once a
// newer Lock call has taken over the restart.
func (g *Gate) Lock(ctx context.Context, opts LockOptions) (unlock func(), abandoned int, err error) {
	g.mu.Lock()
	if g.pending.IsZero() {
		g.pending = time.Now()
	}
	g.gen++
	gen := g.gen
	maxWait := time.NewTimer(g.maxWait)
	defer maxWait.Stop()
	reported := -1
	for g.active > 0 && !opts.Force {
		if opts.OnWait != nil && g.active != reported {
			reported = g.active
			n := g.active
			g.mu.Unlock()
			opts.OnWait(n)
			g.mu.Lock()
			continue
		}
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			g.withdraw(gen)
			return nil, 0, ctx.Err()
		case <-opts.Cancel:
			g.withdraw(gen)
			return nil, 0, ErrCanceled
		case <-maxWait.C:
			g.mu.Lock()
			return g.lockHeld(gen)
		case <-changed:
		}
		g.mu.Lock()
	}
	return g.lockHeld(gen)
}

// lockHeld takes the exclusive lock for Lock call gen. g.mu must be held; it
// is released.
func (g *Gate) lockHeld(gen uint64) (unlock func(), abandoned int, err error) {
	g.locked = true
	abandoned = g.active
	g.notify()
	g.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { g.withdraw(gen) }) }, abandoned, nil
}

// ErrCanceled is returned by Lock when the pending restart is withdrawn.
var ErrCanceled = errors.New("restart canceled")

// withdraw reopens the gate for a canceled Lock unless a newer Lock call has
// taken over the pending restart.
func (g *Gate) withdraw(gen uint64) {
	g.mu.Lock()
	if g.gen == gen {
		g.reopen()
	}
	g.mu.Unlock()
}

// reopen admits new calls again. g.mu must be held.
func (g *Gate) reopen() {
	g.locked = false
	g.pending = time.Time{}
	g.notify()
}
