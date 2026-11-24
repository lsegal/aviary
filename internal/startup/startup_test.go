package startup

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/store"
)

// fakeClock advances only when told to.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func newTestTrace(t *testing.T) (*Trace, *fakeClock) {
	t.Helper()
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
	clock := &fakeClock{now: time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)}
	return newTrace(clock.Now), clock
}

func TestTraceRecordsPhasesAndMCPReady(t *testing.T) {
	tr, clock := newTestTrace(t)
	end := tr.Phase("config load")
	clock.Advance(30 * time.Millisecond)
	end()
	tr.Time("MCP tool registration", func() { clock.Advance(20 * time.Millisecond) })
	tr.MarkMCPReady()
	clock.Advance(time.Second)
	tr.MarkMCPReady() // a listener rotation does not move it

	r := tr.Snapshot()
	assert.Equal(t, int64(50), r.MCPReadyMS)
	require.Len(t, r.Phases, 2)
	assert.Equal(t, Phase{Name: "config load", StartMS: 0, DurationMS: 30}, r.Phases[0])
	assert.Equal(t, Phase{Name: "MCP tool registration", StartMS: 30, DurationMS: 20}, r.Phases[1])
	assert.True(t, r.Ready)

	saved, err := Load()
	require.NoError(t, err)
	assert.Equal(t, int64(50), saved.MCPReadyMS, "MCP ready is saved for aviary doctor")
	assert.Positive(t, saved.PID)
}

func TestTraceGoTracksSubsystemReadiness(t *testing.T) {
	tr, clock := newTestTrace(t)
	tr.MarkMCPReady()
	release := make(chan struct{})
	done := make(chan struct{})
	tr.Go("channels", "channels connect", func() error {
		<-release
		clock.Advance(3 * time.Second)
		return nil
	}, func() { close(done) })
	failed := make(chan struct{})
	tr.Go("projects", "Code Loop discovery", func() error { return errors.New("GitHub unreachable") }, func() { close(failed) })
	<-failed

	r := tr.Snapshot()
	assert.False(t, r.Ready, "channels are still connecting")
	s, ok := tr.State("channels")
	require.True(t, ok)
	assert.Equal(t, StatePending, s.State)
	s, _ = tr.State("projects")
	assert.Equal(t, StateFailed, s.State)
	assert.Equal(t, "GitHub unreachable", s.Error)

	close(release)
	<-done
	r = tr.Snapshot()
	assert.True(t, r.Ready)
	assert.Contains(t, r.Summary(), "slowest: channels connect 3.00s")
	saved, err := Load()
	require.NoError(t, err)
	assert.True(t, saved.Ready, "the finished boot is saved")
}

func TestCheckReadyReturnsInitializingError(t *testing.T) {
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir(""); Begin() })
	tr := Begin()
	assert.NoError(t, CheckReady("channels"), "an untracked subsystem is not waited on")
	tr.Pending("channels")
	err := CheckReady("channels")
	var initErr *InitializingError
	require.ErrorAs(t, err, &initErr)
	assert.Equal(t, "aviary is still initializing channels; retry in 2s", err.Error())

	done := make(chan struct{})
	tr.Go("channels", "channels connect", func() error { return nil }, func() { close(done) })
	<-done
	assert.NoError(t, CheckReady("channels"))
}

func TestReportSummaryAndDetail(t *testing.T) {
	r := Report{
		MCPReadyMS: 420,
		Phases: []Phase{
			{Name: "config load", StartMS: 0, DurationMS: 10},
			{Name: "channels connect", StartMS: 420, DurationMS: 3000, Background: true},
			{Name: "Code Loop discovery", StartMS: 420, DurationMS: 1100, Background: true, Error: "boom"},
		},
		Subsystems: []Subsystem{{Name: "skills", State: StatePending, SinceMS: 420}},
	}
	for _, p := range r.Phases {
		r.TotalMS = max(r.TotalMS, p.StartMS+p.DurationMS)
	}
	assert.Equal(t, "boot: 3.42s, MCP ready at 0.42s, still initializing: skills; slowest: channels connect 3.00s, Code Loop discovery 1.10s, config load 0.01s", r.Summary())

	detail := r.Detail()
	lines := strings.Split(strings.TrimSpace(detail), "\n")
	require.Len(t, lines, 5)
	assert.Contains(t, lines[1], "critical")
	assert.Contains(t, lines[1], "config load")
	assert.Contains(t, lines[3], "background")
	assert.Contains(t, lines[3], "(error: boom)")
	assert.Contains(t, lines[4], "subsystem skills")

	red := r.Redacted()
	assert.Empty(t, red.Phases[2].Error)
	assert.Equal(t, "boom", r.Phases[2].Error, "redacting leaves the original alone")
}

func TestSummaryBeforeMCPReady(t *testing.T) {
	assert.Equal(t, "boot: 0.00s, MCP not ready yet", Report{}.Summary())
}
