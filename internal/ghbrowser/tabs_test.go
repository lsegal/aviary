package ghbrowser

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/browser"
)

var _ TabBrowser = (*browser.Manager)(nil)

// poolBrowser is a TabBrowser that numbers the tabs it opens.
type poolBrowser struct {
	opened    []string
	navigated []string
	closed    []string
	dead      map[string]bool
}

func (b *poolBrowser) Open(_ context.Context, url string) (string, error) {
	b.opened = append(b.opened, url)
	return fmt.Sprintf("tab%d", len(b.opened)), nil
}

func (b *poolBrowser) EvalJS(context.Context, string, string) (string, error) { return "", nil }

func (b *poolBrowser) Navigate(_ context.Context, tabID, url string) error {
	if b.dead[tabID] {
		return errors.New("no such target")
	}
	b.navigated = append(b.navigated, tabID+" "+url)
	return nil
}

func (b *poolBrowser) CloseTab(tabID string) error {
	b.closed = append(b.closed, tabID)
	return nil
}

// fakeClock is a clock whose sleeps advance it instead of waiting.
type fakeClock struct{ at time.Time }

func (c *fakeClock) now() time.Time { return c.at }

func (c *fakeClock) sleep(_ context.Context, d time.Duration) bool {
	c.at = c.at.Add(d)
	return true
}

func newTestTabs(b *poolBrowser, interval time.Duration) (*Tabs, *fakeClock) {
	clock := &fakeClock{at: time.Unix(1_000_000, 0)}
	tabs := NewTabs(b, interval)
	tabs.now = clock.now
	tabs.queue.now = clock.now
	tabs.queue.sleep = clock.sleep
	return tabs, clock
}

func TestTabsOneTabPerTarget(t *testing.T) {
	b := &poolBrowser{}
	tabs, _ := newTestTabs(b, DefaultInterval)
	ctx := context.Background()

	id, err := tabs.Load(ctx, "o/a", "https://github.com/o/a/issues")
	require.NoError(t, err)
	assert.Equal(t, "tab1", id)
	id, err = tabs.Load(ctx, "o/b", "https://github.com/o/b/issues")
	require.NoError(t, err)
	assert.Equal(t, "tab2", id)
	id, err = tabs.Load(ctx, "o/a", "https://github.com/o/a/issues")
	require.NoError(t, err)
	assert.Equal(t, "tab1", id, "a reload reuses the target's tab")
	assert.Len(t, b.opened, 2)
	assert.Equal(t, []string{"tab1 https://github.com/o/a/issues"}, b.navigated)
}

func TestTabsStaggerLoads(t *testing.T) {
	b := &poolBrowser{}
	tabs, clock := newTestTabs(b, DefaultInterval)
	start := clock.at
	var at []time.Duration
	for i := range 3 {
		_, err := tabs.Load(context.Background(), fmt.Sprintf("o/r%d", i), fmt.Sprintf("https://github.com/o/r%d/issues", i))
		require.NoError(t, err)
		at = append(at, clock.at.Sub(start))
	}
	assert.Equal(t, []time.Duration{0, loadStagger, 2 * loadStagger}, at)
}

func TestLoadQueueCompressesWhenOverloaded(t *testing.T) {
	q := NewLoadQueue(10 * time.Second)
	clock := &fakeClock{at: time.Unix(1_000_000, 0)}
	q.now, q.sleep = clock.now, clock.sleep
	for i := range 8 {
		q.Wait(context.Background(), fmt.Sprintf("page%d", i))
	}
	assert.Equal(t, 10*time.Second/8, q.spacing(8), "eight pages share a ten-second interval")
	assert.Less(t, q.next.Sub(clock.at), loadStagger)
}

func TestLoadQueueStopsOnCancel(t *testing.T) {
	q := NewLoadQueue(DefaultInterval)
	ctx, cancel := context.WithCancel(context.Background())
	assert.True(t, q.Wait(ctx, "a"))
	cancel()
	assert.False(t, q.Wait(ctx, "b"), "a cancelled read does not sit out its slot")
}

func TestTabsRetireIdleTabs(t *testing.T) {
	b := &poolBrowser{}
	tabs, clock := newTestTabs(b, DefaultInterval)
	ctx := context.Background()
	_, err := tabs.Load(ctx, "o/a", "https://github.com/o/a/issues")
	require.NoError(t, err)
	clock.at = clock.at.Add(TabIdleTimeout(DefaultInterval))
	_, err = tabs.Load(ctx, "o/b", "https://github.com/o/b/issues")
	require.NoError(t, err)
	assert.Equal(t, []string{"tab1"}, b.closed, "the unread tab is closed")

	_, err = tabs.Load(ctx, "o/a", "https://github.com/o/a/issues")
	require.NoError(t, err)
	assert.Len(t, b.opened, 3, "a retired target gets a new tab")
}

func TestTabIdleTimeout(t *testing.T) {
	assert.Equal(t, time.Minute, TabIdleTimeout(5*time.Second))
	assert.Equal(t, 3*time.Minute, TabIdleTimeout(time.Minute))
}

func TestTabsReplaceDeadTab(t *testing.T) {
	b := &poolBrowser{dead: map[string]bool{}}
	tabs, _ := newTestTabs(b, DefaultInterval)
	ctx := context.Background()
	_, err := tabs.Load(ctx, "o/a", "https://github.com/o/a/issues")
	require.NoError(t, err)
	b.dead["tab1"] = true // for example after the machine slept
	id, err := tabs.Load(ctx, "o/a", "https://github.com/o/a/issues")
	require.NoError(t, err)
	assert.Equal(t, "tab2", id)
	assert.Contains(t, b.closed, "tab1")
}

func TestTabsDiscardAndClose(t *testing.T) {
	b := &poolBrowser{}
	tabs, _ := newTestTabs(b, DefaultInterval)
	ctx := context.Background()
	_, _ = tabs.Load(ctx, "o/a", "https://github.com/o/a/issues")
	_, _ = tabs.Load(ctx, "o/b", "https://github.com/o/b/issues")
	tabs.Discard("o/a")
	assert.Equal(t, []string{"tab1"}, b.closed, "a stalled tab is dropped")
	id, _ := tabs.Load(ctx, "o/a", "https://github.com/o/a/issues")
	assert.Equal(t, "tab3", id)

	tabs.Close()
	assert.ElementsMatch(t, []string{"tab1", "tab2", "tab3"}, b.closed, "exit closes every tab")
}
