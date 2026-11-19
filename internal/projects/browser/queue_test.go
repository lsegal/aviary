package browser

import (
	"context"
	"testing"
	"time"
)

// testLoadQueue builds a queue on a virtual clock: waiting for a slot advances
// the clock by the delay instead of spending it, so the pacing can be measured
// without the test sitting out minutes of stagger. It reports the queue and the
// clock reader.
func testLoadQueue(interval time.Duration, logf func(string, ...interface{})) (*LoadQueue, func() time.Time) {
	queue := NewLoadQueue(interval, logf)
	clock := time.Unix(0, 0).UTC()
	queue.now = func() time.Time { return clock }
	queue.sleep = func(_ context.Context, d time.Duration) bool {
		clock = clock.Add(d)
		return true
	}
	return queue, func() time.Time { return clock }
}

// The point of the queue: five watched pages are loaded 2.5s apart rather than
// all at once, which is what browser mode did before (issue #450).
func TestLoadQueueStaggersLoadsByDefault(t *testing.T) {
	queue, clock := testLoadQueue(20*time.Second, nil)
	start := clock()
	var at []time.Duration
	for _, page := range []string{"a", "b", "c", "d", "e"} {
		if !queue.wait(context.Background(), page) {
			t.Fatalf("wait(%s) reported the run was cancelled", page)
		}
		at = append(at, clock().Sub(start))
	}
	want := []time.Duration{0, 2500 * time.Millisecond, 5 * time.Second, 7500 * time.Millisecond, 10 * time.Second}
	for i := range want {
		if at[i] != want[i] {
			t.Fatalf("load %d ran at %s, want %s (all loads: %v)", i, at[i], want[i], at)
		}
	}
}

// A run watching more pages than fit in its interval at the default stagger
// spreads them across the interval instead, so a full pass still takes one
// interval rather than falling further behind on every tick.
func TestLoadQueueCompressesToFitTheInterval(t *testing.T) {
	const pages = 20
	queue, clock := testLoadQueue(20*time.Second, nil)
	names := make([]string, 0, pages)
	for i := 0; i < pages; i++ {
		names = append(names, "page-"+string(rune('a'+i)))
	}
	// The first pass discovers the demand one page at a time; the passes after
	// it are the steady state the queue settles into.
	for pass := 0; pass < 3; pass++ {
		start := clock()
		for _, name := range names {
			queue.wait(context.Background(), name)
		}
		if pass == 0 {
			continue
		}
		elapsed := clock().Sub(start)
		if elapsed != 20*time.Second {
			t.Fatalf("pass %d over %d pages took %s, want one 20s interval", pass, pages, elapsed)
		}
	}
	if got := queue.spacing(pages); got != time.Second {
		t.Fatalf("spacing for %d pages = %s, want interval/N = 1s", pages, got)
	}
	if got := queue.spacing(4); got != loadStagger {
		t.Fatalf("spacing for 4 pages = %s, want the %s default", got, loadStagger)
	}
}

// An overloaded queue says so, but a queue that stays overloaded says it on a
// timer rather than on every load it paces.
func TestLoadQueueWarnsAtMostOnceAMinute(t *testing.T) {
	var warnings []string
	queue, clock := testLoadQueue(10*time.Second, func(format string, _ ...interface{}) {
		warnings = append(warnings, format)
	})
	names := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		names = append(names, "page-"+string(rune('a'+i)))
	}
	start := clock()
	// Five passes over 40 pages on a 10s interval is 50s of virtual time at a
	// quarter-second spacing: hundreds of loads, well over a minute in total.
	for pass := 0; pass < 6; pass++ {
		for _, name := range names {
			queue.wait(context.Background(), name)
		}
	}
	elapsed := clock().Sub(start)
	if elapsed < time.Minute {
		t.Fatalf("test ran for %s of virtual time, too short to exercise the once-a-minute limit", elapsed)
	}
	minutes := int(elapsed/time.Minute) + 1
	if len(warnings) == 0 {
		t.Fatalf("an overloaded queue logged nothing in %s", elapsed)
	}
	if len(warnings) > minutes {
		t.Fatalf("overload warning logged %d times in %s, want at most one a minute (%d)", len(warnings), elapsed, minutes)
	}
}

// A queue that is not overloaded stays quiet, so the warning means what it says.
func TestLoadQueueDoesNotWarnAtTheDefaultStagger(t *testing.T) {
	var warnings int
	queue, _ := testLoadQueue(20*time.Second, func(string, ...interface{}) { warnings++ })
	for pass := 0; pass < 3; pass++ {
		for _, page := range []string{"a", "b", "c", "d", "e"} {
			queue.wait(context.Background(), page)
		}
	}
	if warnings != 0 {
		t.Fatalf("warned %d time(s) about a queue running at the default stagger", warnings)
	}
}

// Pages the run has stopped loading -- a repository dropped from the run, an
// issue whose agent has finished -- age out of the demand instead of holding
// the spacing compressed for the pages still being watched.
func TestLoadQueueForgetsPagesItStopsLoading(t *testing.T) {
	queue, _ := testLoadQueue(20*time.Second, nil)
	for i := 0; i < 20; i++ {
		queue.wait(context.Background(), "old-"+string(rune('a'+i)))
	}
	if len(queue.pages) < 20 {
		t.Fatalf("queue is pacing %d pages, want the 20 it just loaded", len(queue.pages))
	}
	// Long enough for the abandoned pages to fall out of the working set.
	for i := 0; i < 200; i++ {
		queue.wait(context.Background(), "kept")
	}
	if len(queue.pages) != 1 {
		t.Fatalf("queue is still pacing %d pages, want only the one still being loaded", len(queue.pages))
	}
	if got := queue.spacing(len(queue.pages)); got != loadStagger {
		t.Fatalf("spacing = %s, want the %s default once the demand is gone", got, loadStagger)
	}
}

// A run being shut down does not sit out its slot first.
func TestLoadQueueWaitStopsWhenTheRunIsCancelled(t *testing.T) {
	queue := NewLoadQueue(20*time.Second, nil)
	if !queue.wait(context.Background(), "first") {
		t.Fatal("the first load waited for a slot it should have had immediately")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if queue.wait(ctx, "second") {
		t.Fatal("wait reported success for a cancelled run instead of giving up its slot")
	}
}

// An interval that was never resolved falls back to browser mode's own default
// rather than leaving the queue dividing by zero.
func TestNewLoadQueueDefaultsItsInterval(t *testing.T) {
	if got := NewLoadQueue(0, nil).interval; got != DefaultWatchInterval {
		t.Fatalf("interval = %s, want the %s browser-mode default", got, DefaultWatchInterval)
	}
}

// A tab reloads the page it was last navigated to, so a reload queues as that
// page rather than as demand of its own.
func TestTabReloadQueuesAsTheSamePage(t *testing.T) {
	queue, _ := testLoadQueue(20*time.Second, nil)
	tab := &Tab{}
	tab.paceWith(context.Background(), queue)
	tab.awaitLoadSlot("https://github.com/owner/repo/issues")
	tab.awaitLoadSlot("")
	tab.awaitLoadSlot("")
	if len(queue.pages) != 1 {
		t.Fatalf("queue is pacing %d pages, want the one URL the tab loads", len(queue.pages))
	}
	if _, ok := queue.pages["https://github.com/owner/repo/issues"]; !ok {
		t.Fatalf("queue is pacing %v, want the tab's own URL", queue.pages)
	}
}

// A tab with no queue -- the browser `Aviary auth` signs in through -- loads its
// page immediately.
func TestTabWithoutQueueDoesNotWait(t *testing.T) {
	tab := &Tab{}
	tab.awaitLoadSlot("https://github.com/login")
	if tab.loadURL != "https://github.com/login" {
		t.Fatalf("tab URL = %q, want the page it was pointed at", tab.loadURL)
	}
}
