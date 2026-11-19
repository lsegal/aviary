package browser

import (
	"context"
	"sync"
	"time"
)

// loadStagger is the minimum spacing browser mode puts between two page
// loads. A tick reloads every watched target's page and re-reads the
// conversation of every issue an agent is working, and those loads used to be
// issued back to back: a run watching enough repositories, boards, and
// conversations fired the whole burst at GitHub the moment the tick began, N
// requests at once every interval (issue #450). Spacing them means a run's
// requests arrive as a steady trickle rather than a spike, so 5 watched pages
// load at +0s, +2.5s, +5s, +7.5s and +10s.
const loadStagger = 2500 * time.Millisecond

// loadOverload is the spacing below which the queue has been compressed
// hard enough to be worth saying so, and how often it says it. A queue this
// tight is a property of how much the run is watching rather than of any one
// load, so it is reported on a timer instead of on every load that crosses the
// line.
const (
	loadOverload      = time.Second
	loadOverloadEvery = time.Minute
)

// loadWorkingSetCycles is how many poll intervals a page stays in the
// queue's working set for after its last load. It is more than one because a
// compressed queue takes a whole interval to work through the pages it is
// pacing, and a page must not fall out of the count between two of its own
// loads or the queue would keep rediscovering the demand it is already serving.
// A page Aviary has genuinely stopped watching ages out of the count instead of
// holding the spacing down for the rest of the run.
const loadWorkingSetCycles = 3

// LoadQueue paces every page load a browser-mode run makes. It is one
// queue for the whole run rather than one per reader, because the burst it
// exists to smooth is the sum of what all the readers do on the same tick: the
// issues page of every repository, every project board, and the conversation of
// every issue an agent is working.
//
// Loads are spaced loadStagger apart by default. That spacing only holds
// while the pages due within one poll interval fit inside it; past that point a
// run that insisted on 2.5s per load would take longer than its own interval to
// get round them all and fall further behind on every tick, so the spacing
// compresses to interval/N.
//
// N is the number of distinct pages the queue has loaded recently, not the
// number of callers waiting on it: the readers mostly load their pages one
// after another (the issue list walks its targets in turn), so at any instant
// the queue has one caller and no idea that twenty more loads are behind it.
// Counting the pages themselves sees the whole demand even while the queue is
// throttling it, and settles where a full pass over them takes one interval.
type LoadQueue struct {
	// interval is the run's poll interval: the window a pass over every page
	// the queue is pacing has to fit inside.
	interval time.Duration
	logf     func(string, ...interface{})
	// now and sleep are test seams for the queue's clock and its wait, so the
	// pacing can be exercised without spending the delays it hands out.
	now   func() time.Time
	sleep func(context.Context, time.Duration) bool

	mu sync.Mutex
	// pages is when each page the queue is pacing was last loaded, which is
	// the demand the spacing is computed from.
	pages map[string]time.Time
	// next is the earliest moment the queue will let another load start.
	next time.Time
	// warned is when the overload warning was last logged, so an overloaded
	// queue says so at most once every loadOverloadEvery.
	warned time.Time
}

// DefaultWatchInterval is the poll interval browser mode defaults to. Reading a
// page Aviary already has a tab open on is far cheaper than the API path's `gh`
// invocations, so browser mode can poll faster than the half-minute the API
// default is chosen to stay inside rate limits. It is not free, though: a tick
// reloads every watched target's page, waits out GitHub's client-side render,
// and re-reads the conversation of every issue an agent is working, none of
// which fits in the five seconds this was first set to (issue #441).
const DefaultWatchInterval = 20 * time.Second

// NewLoadQueue builds the queue a browser-mode run paces its page loads
// with. An interval of zero falls back to browser mode's own default, so a
// queue is never left dividing by a window it does not have.
func NewLoadQueue(interval time.Duration, logf func(string, ...interface{})) *LoadQueue {
	if interval <= 0 {
		interval = DefaultWatchInterval
	}
	return &LoadQueue{interval: interval, logf: logf, pages: map[string]time.Time{}}
}

// Interval reports the poll interval the queue spaces its loads across.
func (q *LoadQueue) Interval() time.Duration { return q.interval }

// clock reports the queue's current time.
func (q *LoadQueue) clock() time.Time {
	if q.now != nil {
		return q.now()
	}
	return time.Now()
}

// spacing reports how far apart the queue puts loads while it is pacing demand
// pages. The default stagger holds until the interval can no longer hold that
// many loads, at which point they are spread evenly across the interval
// instead: a pass that cannot finish within its own interval is worse than one
// whose requests sit closer together than intended.
func (q *LoadQueue) spacing(demand int) time.Duration {
	if demand < 1 {
		demand = 1
	}
	spacing := loadStagger
	if compressed := q.interval / time.Duration(demand); compressed < spacing {
		spacing = compressed
	}
	if spacing < 0 {
		spacing = 0
	}
	return spacing
}

// reserve records a load of page and reports when it may start. The slot is
// taken under the lock and waited for afterwards, so concurrent readers queue
// up behind each other in the order they arrived rather than all waking to the
// same moment.
func (q *LoadQueue) reserve(page string) time.Time {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.clock()
	slot := now
	if q.next.After(slot) {
		slot = q.next
	}
	if q.pages == nil {
		q.pages = map[string]time.Time{}
	}
	// A page nothing has asked for in several intervals is no longer part of
	// the demand: a repository that was dropped from the run, or an issue whose
	// agent has finished, should not keep the spacing compressed for everything
	// still being watched.
	stale := slot.Add(-loadWorkingSetCycles * q.interval)
	for at, last := range q.pages {
		if !last.After(stale) {
			delete(q.pages, at)
		}
	}
	q.pages[page] = slot
	spacing := q.spacing(len(q.pages))
	q.next = slot.Add(spacing)
	q.warnOverloaded(now, len(q.pages), spacing)
	return slot
}

// warnOverloaded reports a queue compressed below loadOverload, at most
// once every loadOverloadEvery. It is called with the lock held.
func (q *LoadQueue) warnOverloaded(now time.Time, demand int, spacing time.Duration) {
	if q.logf == nil || spacing >= loadOverload {
		return
	}
	if !q.warned.IsZero() && now.Sub(q.warned) < loadOverloadEvery {
		return
	}
	q.warned = now
	q.logf("browser page load queue is overloaded: %d page(s) to load every %s, so they are being spaced %s apart instead of the usual %s; watch fewer targets or raise -interval", demand, q.interval, spacing.Round(time.Millisecond), loadStagger)
}

// wait blocks until this load's turn comes round, reporting false when the run
// is shut down while it waits so a cancelled poll stops instead of sitting out
// its slot.
func (q *LoadQueue) wait(ctx context.Context, page string) bool {
	if q == nil {
		return true
	}
	slot := q.reserve(page)
	delay := slot.Sub(q.clock())
	if delay <= 0 {
		return ctx == nil || ctx.Err() == nil
	}
	if q.sleep != nil {
		return q.sleep(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	if ctx == nil {
		<-timer.C
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
