// Package wake reports when the machine resumes from sleep, so subsystems that
// keep timers or connections can recover from a gap they did not run through.
//
// One detector serves the whole process. It compares how much wall-clock time
// passed between two of its ticks with how much should have: a process does
// not run while the machine sleeps, so the first tick after waking lands long
// after the one before it.
package wake

import (
	"context"
	"sync"
	"time"
)

// Threshold is the smallest wall-clock jump that counts as a wake.
const Threshold = time.Minute

// checkInterval is how often the shared detector reads the clock. It is a
// variable only so tests need not spend it.
var checkInterval = 15 * time.Second

var shared = struct {
	mu     sync.Mutex
	subs   map[chan time.Duration]struct{}
	cancel context.CancelFunc
}{subs: make(map[chan time.Duration]struct{})}

// Subscribe reports each wake, with roughly how long the machine slept, until
// ctx ends. Every subscriber shares one detector, which runs while anyone is
// subscribed. A wake is dropped rather than queued for a subscriber that has
// not handled the last one yet.
func Subscribe(ctx context.Context) <-chan time.Duration {
	ch := make(chan time.Duration, 1)
	shared.mu.Lock()
	shared.subs[ch] = struct{}{}
	if shared.cancel == nil {
		var detectCtx context.Context
		detectCtx, shared.cancel = context.WithCancel(context.Background())
		ticker := time.NewTicker(checkInterval)
		go func() {
			defer ticker.Stop()
			Detect(detectCtx, ticker.C, checkInterval, time.Now, broadcast)
		}()
	}
	shared.mu.Unlock()
	context.AfterFunc(ctx, func() {
		shared.mu.Lock()
		defer shared.mu.Unlock()
		delete(shared.subs, ch)
		if len(shared.subs) == 0 && shared.cancel != nil {
			shared.cancel()
			shared.cancel = nil
		}
	})
	return ch
}

func broadcast(slept time.Duration) {
	shared.mu.Lock()
	defer shared.mu.Unlock()
	for ch := range shared.subs {
		select {
		case ch <- slept:
		default:
		}
	}
}

// Detect is the detector's loop: it reads the clock once to start and once per
// tick, and calls onWake when a tick lands at least Threshold later than
// interval allows. Wall-clock time is compared with its monotonic reading
// stripped, because on most platforms the monotonic clock stops while the
// machine sleeps and would hide the gap. A clock set forward by hand looks the
// same, which costs no more than one extra recovery.
func Detect(ctx context.Context, ticks <-chan time.Time, interval time.Duration, now func() time.Time, onWake func(slept time.Duration)) {
	last := now().Round(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
		current := now().Round(0)
		if slept := current.Sub(last) - interval; slept >= Threshold {
			onWake(slept)
		}
		last = current
	}
}
