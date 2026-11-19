package ghbrowser

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// DefaultInterval is how often browser delivery reloads its pages. A tick
// reloads every watched page and waits out GitHub's client-side render, which
// does not fit in much less.
const DefaultInterval = 20 * time.Second

// loadStagger is the usual spacing between two page loads, so a tick's loads
// reach GitHub as a trickle instead of a burst: five pages load at +0s,
// +2.5s, +5s, +7.5s and +10s.
const loadStagger = 2500 * time.Millisecond

// loadOverload is the spacing below which the queue warns, at most once every
// loadOverloadEvery, that it is watching more than one interval can hold.
const (
	loadOverload      = time.Second
	loadOverloadEvery = time.Minute
)

// loadWorkingSetCycles is how many intervals a page counts toward demand after
// its last load, so a page that is no longer watched stops compressing the
// spacing for the rest.
const loadWorkingSetCycles = 3

// LoadQueue paces every page load browser delivery makes. There is one queue
// for all readers, because the burst it smooths is the sum of their loads.
// Loads are spaced loadStagger apart until the pages due within one interval
// no longer fit, and then the spacing compresses to interval/N, where N is the
// number of distinct pages loaded recently.
type LoadQueue struct {
	interval time.Duration
	now      func() time.Time
	sleep    func(context.Context, time.Duration) bool

	mu     sync.Mutex
	pages  map[string]time.Time
	next   time.Time
	warned time.Time
}

// NewLoadQueue builds a queue spacing loads across interval, or across
// DefaultInterval when interval is not positive.
func NewLoadQueue(interval time.Duration) *LoadQueue {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &LoadQueue{interval: interval, pages: map[string]time.Time{}}
}

func (q *LoadQueue) clock() time.Time {
	if q.now != nil {
		return q.now()
	}
	return time.Now()
}

func (q *LoadQueue) spacing(demand int) time.Duration {
	if demand < 1 {
		demand = 1
	}
	return min(loadStagger, q.interval/time.Duration(demand))
}

// reserve records a load of page and reports when it may start. Slots are
// taken under the lock, so concurrent readers load in arrival order.
func (q *LoadQueue) reserve(page string) time.Time {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.clock()
	slot := now
	if q.next.After(slot) {
		slot = q.next
	}
	stale := slot.Add(-loadWorkingSetCycles * q.interval)
	for p, last := range q.pages {
		if !last.After(stale) {
			delete(q.pages, p)
		}
	}
	q.pages[page] = slot
	spacing := q.spacing(len(q.pages))
	q.next = slot.Add(spacing)
	if spacing < loadOverload && (q.warned.IsZero() || now.Sub(q.warned) >= loadOverloadEvery) {
		q.warned = now
		slog.Warn("ghbrowser: page load queue is overloaded; watch fewer targets or raise the interval",
			"pages", len(q.pages), "interval", q.interval, "spacing", spacing.Round(time.Millisecond))
	}
	return slot
}

// Wait blocks until it is page's turn to load. It returns false when ctx ends
// first, so a cancelled read stops instead of sitting out its slot.
func (q *LoadQueue) Wait(ctx context.Context, page string) bool {
	if q == nil {
		return ctx.Err() == nil
	}
	delay := q.reserve(page).Sub(q.clock())
	if delay <= 0 {
		return ctx.Err() == nil
	}
	if q.sleep != nil {
		return q.sleep(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
