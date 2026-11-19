package ghbrowser

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// tabIdleFloor is the shortest a tab stays open unread, so a short interval
// does not close and reopen tabs between reads.
const tabIdleFloor = time.Minute

// TabIdleTimeout is how long a tab may go unread before it is closed: three
// intervals, and never less than a minute.
func TabIdleTimeout(interval time.Duration) time.Duration {
	return max(3*interval, tabIdleFloor)
}

// TabBrowser is the part of Aviary's browser manager the tab pool needs.
type TabBrowser interface {
	Browser
	Navigate(ctx context.Context, tabID, url string) error
}

// Tabs keeps one tab per target in Aviary's browser. Page loads go through a
// shared LoadQueue. Tabs unread for the idle timeout are closed, and a tab
// that stalled or died is replaced on its next load.
type Tabs struct {
	browser TabBrowser
	queue   *LoadQueue
	idle    time.Duration
	now     func() time.Time

	mu   sync.Mutex
	tabs map[string]*poolTab
}

type poolTab struct {
	id   string
	used time.Time
}

// NewTabs builds a tab pool reloading pages every interval.
func NewTabs(browser TabBrowser, interval time.Duration) *Tabs {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Tabs{
		browser: browser,
		queue:   NewLoadQueue(interval),
		idle:    TabIdleTimeout(interval),
		tabs:    map[string]*poolTab{},
	}
}

func (t *Tabs) clock() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

// Load loads url in target's tab, opening the tab when there is none and
// waiting for the load's turn in the queue. It returns the tab's ID.
func (t *Tabs) Load(ctx context.Context, target, url string) (string, error) {
	if !t.queue.Wait(ctx, url) {
		return "", ctx.Err()
	}
	id, opened, retired := t.take(target)
	t.closeAll(retired)
	if !opened {
		err := t.browser.Navigate(ctx, id, url)
		if err == nil {
			return id, nil
		}
		// The tab may have died, for example while the machine slept:
		// replace it instead of failing the read.
		slog.Info("ghbrowser: replacing dead tab", "target", target, "err", err)
		t.Discard(target)
	}
	id, err := t.browser.Open(ctx, url)
	if err != nil {
		return "", fmt.Errorf("open %s in Aviary's browser: %w", url, err)
	}
	t.mu.Lock()
	if old, ok := t.tabs[target]; ok && old.id != id {
		// A concurrent load opened one too; keep the newer tab.
		defer t.closeTab(target, old.id)
	}
	t.tabs[target] = &poolTab{id: id, used: t.clock()}
	t.mu.Unlock()
	return id, nil
}

// take marks target's tab used and reports it, or reports that one must be
// opened. It also hands back tabs that have gone idle, to close outside the
// lock.
func (t *Tabs) take(target string) (id string, opened bool, retired map[string]string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.clock()
	retired = map[string]string{}
	for name, tab := range t.tabs {
		if name != target && now.Sub(tab.used) >= t.idle {
			retired[name] = tab.id
			delete(t.tabs, name)
		}
	}
	tab, ok := t.tabs[target]
	if !ok {
		return "", true, retired
	}
	tab.used = now
	return tab.id, false, retired
}

// Discard drops target's tab, for example after a command on it stalled, so
// the next Load opens a fresh one.
func (t *Tabs) Discard(target string) {
	t.mu.Lock()
	tab, ok := t.tabs[target]
	delete(t.tabs, target)
	t.mu.Unlock()
	if ok {
		t.closeTab(target, tab.id)
	}
}

// Close closes every tab in the pool, for a clean exit.
func (t *Tabs) Close() {
	t.mu.Lock()
	tabs := map[string]string{}
	for name, tab := range t.tabs {
		tabs[name] = tab.id
	}
	t.tabs = map[string]*poolTab{}
	t.mu.Unlock()
	t.closeAll(tabs)
}

func (t *Tabs) closeAll(tabs map[string]string) {
	for name, id := range tabs {
		t.closeTab(name, id)
	}
}

func (t *Tabs) closeTab(target, id string) {
	if err := t.browser.CloseTab(id); err != nil {
		slog.Debug("ghbrowser: close tab", "target", target, "tab", id, "err", err)
	}
}
