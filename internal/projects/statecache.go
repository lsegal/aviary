package projects

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/store"
)

// stateCacheVersion is the version of the Code Loop state cache's format. A
// cache written in any other version is ignored.
const stateCacheVersion = 1

// stateCacheLogLines is how many lines of each job's scrollback the cache
// keeps, enough for a board card without making the file grow with every job's
// whole output.
const stateCacheLogLines = 40

// stateCacheWriteDelay batches the engine's frequent snapshots into one write.
var stateCacheWriteDelay = 5 * time.Second

// stateCacheFile is the Code Loop state cache as it is saved: the last
// snapshot the engine published after a complete poll, and when that was.
type stateCacheFile struct {
	Version  int           `json:"version"`
	CachedAt time.Time     `json:"cached_at"`
	Snapshot core.Snapshot `json:"snapshot"`
}

// stateCache serves the last known Code Loop state while a newly started
// engine has not polled GitHub yet (issue #921). It is only ever read for
// display: the job actions, safe restarts and dispatch read the engine's
// live snapshot, so nothing is started, claimed or restarted from it.
type stateCache struct {
	path string

	mu       sync.Mutex
	cached   *core.Snapshot
	cachedAt time.Time
	// pending is the snapshot waiting for the next write, and timer the
	// write scheduled for it.
	pending  *stateCacheFile
	timer    *time.Timer
	disabled bool
}

// newStateCache loads the cache at path. A missing, unreadable, corrupt, or
// other-version cache is ignored, so startup behaves as if there were none.
func newStateCache(path string) *stateCache {
	c := &stateCache{path: path}
	saved, err := store.ReadJSON[stateCacheFile](path)
	switch {
	case err != nil:
	case saved.Version != stateCacheVersion:
		slog.Info(fmt.Sprintf("ignoring the Code Loop state cache: version %d, want %d", saved.Version, stateCacheVersion), "component", logComponent)
	case saved.CachedAt.IsZero():
	default:
		c.cached, c.cachedAt = &saved.Snapshot, saved.CachedAt
	}
	return c
}

// observe records a live snapshot. Once the engine has finished a poll, every
// target in it is live, so it becomes the cache served after the next
// restart; until then the cache already on disk is kept.
func (c *stateCache) observe(live core.Snapshot) {
	if live.LastPoll.IsZero() {
		return
	}
	saved := live
	saved.Jobs = make([]JobSnapshot, len(live.Jobs))
	for i, job := range live.Jobs {
		job.Log = lastLogLines(job.Log, stateCacheLogLines)
		saved.Jobs[i] = job
	}
	saved.IssueCounts = maps.Clone(live.IssueCounts)
	saved.Quotas = maps.Clone(live.Quotas)
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cached, c.cachedAt = &saved, now
	if c.disabled {
		return
	}
	c.pending = &stateCacheFile{Version: stateCacheVersion, CachedAt: now, Snapshot: saved}
	if c.timer == nil {
		c.timer = time.AfterFunc(stateCacheWriteDelay, c.flush)
	}
}

// flush writes the pending snapshot, if any.
func (c *stateCache) flush() {
	c.mu.Lock()
	pending := c.pending
	c.pending = nil
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.mu.Unlock()
	if pending == nil || c.path == "" {
		return
	}
	if err := store.WriteJSON(c.path, pending); err != nil {
		slog.Warn("could not save the Code Loop state cache", "component", logComponent, "err", err)
	}
}

// close writes what is pending and stops writing.
func (c *stateCache) close() {
	c.flush()
	c.mu.Lock()
	c.disabled = true
	c.mu.Unlock()
}

// load returns the cached snapshot and when it was saved, or nil.
func (c *stateCache) load() (*core.Snapshot, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cached, c.cachedAt
}

// displayState is what the status and job APIs show: the live snapshot with
// the cache filling in every target the engine has not polled yet.
type displayState struct {
	core.Snapshot
	// CachedAt is when the cache was saved, zero when nothing is from it.
	CachedAt time.Time
	// Stale lists the targets shown from the cache, and Loading the
	// targets that have neither live nor cached data yet.
	Stale   []string
	Loading []string
	// staleJobs holds the keys of the jobs shown from the cache.
	staleJobs map[string]bool
}

// jobStale reports whether job is shown from the cache.
func (d displayState) jobStale(job JobSnapshot) bool {
	return d.staleJobs[jobKey(job)]
}

// anyStale reports whether any of targets, or any target without targets, is
// shown from the cache or still loading.
func (d displayState) anyStale(targets []string) bool {
	for _, target := range slices.Concat(d.Stale, d.Loading) {
		if len(targets) == 0 || slices.Contains(targets, target) {
			return true
		}
	}
	return false
}

func jobKey(job JobSnapshot) string { return fmt.Sprintf("%s#%d", job.Target, job.Number) }

// targetLive reports whether the engine has polled target: it has listed the
// target's issues, or finished a whole poll.
func targetLive(live *core.Snapshot, target string) bool {
	if live == nil {
		return false
	}
	if _, ok := live.IssueCounts[target]; ok {
		return true
	}
	return !live.LastPoll.IsZero()
}

// mergeCached builds the displayed state from the live snapshot, which is nil
// before the engine publishes one, and the cache, which is nil without one,
// for the configured targets. Each target the engine has not polled yet shows
// its cached issue count and jobs, except a job the engine already knows,
// which shows its live, recovered state. Targets the cache does not know show
// as loading, and cached targets no longer configured are dropped.
func mergeCached(live, cached *core.Snapshot, cachedAt time.Time, configured []string) displayState {
	var display displayState
	if live != nil {
		display.Snapshot = *live
	}
	targets := configured
	if live != nil && len(live.Targets) > 0 {
		targets = live.Targets
	}
	display.Targets = slices.Clone(targets)
	if live != nil && !live.LastPoll.IsZero() {
		return display
	}
	cachedTargets := []string{}
	if cached != nil {
		cachedTargets = cached.Targets
	}
	var stale []string
	for _, target := range targets {
		switch {
		case targetLive(live, target):
		case slices.Contains(cachedTargets, target):
			stale = append(stale, target)
		default:
			display.Loading = append(display.Loading, target)
		}
	}
	if len(stale) == 0 {
		return display
	}
	display.Stale = stale
	display.CachedAt = cachedAt
	display.IssueCounts = maps.Clone(display.IssueCounts)
	if display.IssueCounts == nil {
		display.IssueCounts = map[string]int{}
	}
	for _, target := range stale {
		if count, ok := cached.IssueCounts[target]; ok {
			display.IssueCounts[target] = count
		}
	}
	liveJobs := make(map[string]bool, len(display.Jobs))
	for _, job := range display.Jobs {
		liveJobs[jobKey(job)] = true
	}
	jobs := slices.Clone(display.Jobs)
	display.staleJobs = map[string]bool{}
	for _, job := range cached.Jobs {
		key := jobKey(job)
		if !slices.Contains(stale, job.Target) || liveJobs[key] {
			continue
		}
		jobs = append(jobs, job)
		display.staleJobs[key] = true
	}
	sortJobSnapshots(jobs)
	display.Jobs = jobs
	display.Paused = 0
	for _, job := range jobs {
		if job.Status == "paused" {
			display.Paused++
		}
	}
	// Before the first poll, the run's own totals have counted nothing yet.
	display.Completed = max(display.Completed, cached.Completed)
	display.Failed = max(display.Failed, cached.Failed)
	display.Merged24h = max(display.Merged24h, cached.Merged24h)
	display.LastPoll = cached.LastPoll
	quotas := maps.Clone(cached.Quotas)
	if quotas == nil {
		quotas = map[string]string{}
	}
	maps.Copy(quotas, display.Quotas)
	display.Quotas = quotas
	if display.Identity == "" {
		display.Identity = cached.Identity
	}
	if display.Concurrency == 0 {
		display.Concurrency = cached.Concurrency
	}
	return display
}
