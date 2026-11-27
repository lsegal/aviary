package projects

import (
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/store"
)

// jobStoreVersion is the version of the Code Loop job store's format. A store
// written in any other version is ignored.
const jobStoreVersion = 2

// jobStoreLogBytes caps the scrollback the store keeps for each job, keeping
// the tail, so the store does not grow with every job's whole output (issue
// #1460). The job's full transcript lives in its transcript directory.
const jobStoreLogBytes = 32 * 1024

// clearedJobRetention is how long a cleared or expired job's tombstone is
// kept, so a job the engine or a poll still reports is not stored again as new.
const clearedJobRetention = 90 * 24 * time.Hour

// jobStoreWriteDelay batches the engine's frequent snapshots into one write.
var jobStoreWriteDelay = 5 * time.Second

// jobStoreFile is the Code Loop job store as it is saved.
type jobStoreFile struct {
	Version int `json:"version"`
	// CachedAt is when Snapshot was saved: the engine's last snapshot after
	// a complete poll, without its jobs.
	CachedAt time.Time     `json:"cached_at"`
	Snapshot core.Snapshot `json:"snapshot"`
	// Jobs holds every job Code Loop has seen, keyed by jobKey, and Cleared
	// the jobs cleared or expired since.
	Jobs    map[string]storedJob  `json:"jobs"`
	Cleared map[string]clearedJob `json:"cleared,omitempty"`
}

// storedJob is one job card as last seen.
type storedJob struct {
	Job JobSnapshot `json:"job"`
	// UpdatedAt is when live data last changed the job, and FinishedAt when
	// it was first seen done or failed, which its finished_ttl counts from.
	UpdatedAt  time.Time `json:"updated_at"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
}

// clearedJob is the tombstone of a cleared or expired job: the run it
// removed, so the same run is never stored or shown again while a later run
// of the same issue is.
type clearedJob struct {
	Started   time.Time `json:"started,omitzero"`
	ClearedAt time.Time `json:"cleared_at"`
}

// jobFinished reports whether job is done or failed on the board, the only
// jobs that can be cleared or expire. Running, queued, paused, waiting and
// retrying jobs never are.
func jobFinished(job JobSnapshot) bool {
	state := job.BoardState()
	return state == core.BoardDone || state == core.BoardFailed
}

// jobStore is Code Loop's durable record of every job card and run it has
// seen (issue #1460), kept in the projects state directory so every client,
// the web UI, the TUI and the MCP tools, reads the same history, and served
// stale-while-revalidate after a restart until live data confirms it. It is
// only ever read for display: the job actions, safe restarts and dispatch
// read the engine's live snapshot, so nothing is started, claimed or
// restarted from it.
type jobStore struct {
	path string
	now  func() time.Time

	mu       sync.Mutex
	meta     *core.Snapshot
	cachedAt time.Time
	jobs     map[string]storedJob
	cleared  map[string]clearedJob
	// dirty marks changes waiting for the write scheduled on timer.
	dirty    bool
	timer    *time.Timer
	disabled bool
	// onRemove learns the keys of jobs removed from the store, so their
	// transcripts go too.
	onRemove func(keys []string)
}

// newJobStore loads the store at path. A missing, unreadable, corrupt, or
// other-version store is ignored, so startup behaves as if there were none.
func newJobStore(path string) *jobStore {
	s := &jobStore{
		path:    path,
		now:     time.Now,
		jobs:    map[string]storedJob{},
		cleared: map[string]clearedJob{},
	}
	saved, err := store.ReadJSON[jobStoreFile](path)
	switch {
	case err != nil:
	case saved.Version != jobStoreVersion:
		slog.Info(fmt.Sprintf("ignoring the Code Loop job store: version %d, want %d", saved.Version, jobStoreVersion), "component", logComponent)
	default:
		if !saved.CachedAt.IsZero() {
			s.meta, s.cachedAt = &saved.Snapshot, saved.CachedAt
		}
		if saved.Jobs != nil {
			s.jobs = saved.Jobs
		}
		if saved.Cleared != nil {
			s.cleared = saved.Cleared
		}
	}
	return s
}

// clearedRun reports whether job is the run a tombstone removed: it started no
// later than the cleared run.
func clearedRun(tomb clearedJob, job JobSnapshot) bool {
	return job.Started.IsZero() || !job.Started.After(tomb.Started)
}

// observe records a live snapshot. Every live job is upserted in place by its
// key, except a run that was cleared or expired. Once the engine has finished
// a poll, the snapshot's totals become those served after the next restart,
// and a running, queued or paused job the poll no longer reports is dropped:
// the live data has replaced it. Done and failed jobs stay until they are
// cleared or expire.
func (s *jobStore) observe(live core.Snapshot) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	liveKeys := make(map[string]bool, len(live.Jobs))
	for _, job := range live.Jobs {
		key := jobKey(job)
		liveKeys[key] = true
		if tomb, ok := s.cleared[key]; ok {
			if clearedRun(tomb, job) {
				continue
			}
			// A later run of the same issue is new work.
			delete(s.cleared, key)
		}
		previous, ok := s.jobs[key]
		merged := mergeStoredJob(previous, job, now)
		if ok && reflect.DeepEqual(previous.Job, merged.Job) && previous.FinishedAt.Equal(merged.FinishedAt) {
			continue
		}
		s.jobs[key] = merged
		changed = true
	}
	if !live.LastPoll.IsZero() {
		meta := live
		meta.Jobs = nil
		meta.IssueCounts = maps.Clone(live.IssueCounts)
		meta.Quotas = maps.Clone(live.Quotas)
		s.meta, s.cachedAt = &meta, now
		for key, stored := range s.jobs {
			if !liveKeys[key] && !jobFinished(stored.Job) && targetLive(&live, stored.Job.Target) {
				delete(s.jobs, key)
			}
		}
		changed = true
	}
	if changed {
		s.scheduleWriteLocked()
	}
}

// mergeStoredJob updates a stored job in place with its live state. What the
// live job does not know yet, such as the start time and scrollback of a job
// the engine only recovered from its work state, is kept from the stored copy.
func mergeStoredJob(stored storedJob, job JobSnapshot, now time.Time) storedJob {
	sameRun := stored.Job.Started.IsZero() || job.Started.IsZero() || job.Started.Equal(stored.Job.Started)
	if sameRun {
		if job.Started.IsZero() {
			job.Started = stored.Job.Started
		}
		if job.Log == "" {
			job.Log = stored.Job.Log
		}
	}
	job.Log = tailBytes(job.Log, jobStoreLogBytes)
	finishedAt := time.Time{}
	if jobFinished(job) {
		switch {
		case sameRun && !stored.FinishedAt.IsZero():
			finishedAt = stored.FinishedAt
		case !job.Finished.IsZero():
			finishedAt = job.Finished
		default:
			finishedAt = now
		}
	}
	return storedJob{Job: job, UpdatedAt: now, FinishedAt: finishedAt}
}

// tailBytes keeps the last limit bytes of s, starting at a line when it can.
func tailBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	tail := s[len(s)-limit:]
	for i := 0; i < len(tail); i++ {
		if tail[i] == '\n' {
			return tail[i+1:]
		}
	}
	return tail
}

// finishedTTL returns how long a target's finished jobs are kept; zero keeps
// them until they are cleared.
type finishedTTL func(target string) time.Duration

// expired reports whether a finished job has outlived its project's
// finished_ttl.
func expired(ttl finishedTTL, job JobSnapshot, finishedAt, now time.Time) bool {
	if ttl == nil || !jobFinished(job) || finishedAt.IsZero() {
		return false
	}
	keep := ttl(job.Target)
	return keep > 0 && now.Sub(finishedAt) > keep
}

// sweep removes every finished job older than its project's finished_ttl,
// leaving a tombstone so the engine's copy is not stored again, and drops
// tombstones past clearedJobRetention. It returns the keys removed.
func (s *jobStore) sweep(ttl finishedTTL) []string {
	now := s.now()
	s.mu.Lock()
	var removed []string
	for key, stored := range s.jobs {
		if expired(ttl, stored.Job, stored.FinishedAt, now) {
			s.removeLocked(key, stored, now)
			removed = append(removed, key)
		}
	}
	for key, tomb := range s.cleared {
		if now.Sub(tomb.ClearedAt) > clearedJobRetention {
			delete(s.cleared, key)
			s.dirty = true
		}
	}
	if len(removed) > 0 || s.dirty {
		s.scheduleWriteLocked()
	}
	onRemove := s.onRemove
	s.mu.Unlock()
	if len(removed) > 0 && onRemove != nil {
		onRemove(removed)
	}
	return removed
}

// removeLocked deletes a stored job, leaving its tombstone.
func (s *jobStore) removeLocked(key string, stored storedJob, now time.Time) {
	delete(s.jobs, key)
	s.cleared[key] = clearedJob{Started: stored.Job.Started, ClearedAt: now}
	s.dirty = true
}

// clear removes the finished jobs among jobs from the store, leaving
// tombstones so neither the engine's copy nor a later poll brings them back,
// and returns the keys it cleared. Jobs that are not done or failed are never
// cleared.
func (s *jobStore) clear(jobs []JobSnapshot) []string {
	now := s.now()
	s.mu.Lock()
	var removed []string
	for _, job := range jobs {
		if !jobFinished(job) {
			continue
		}
		key := jobKey(job)
		stored, ok := s.jobs[key]
		if !ok {
			stored = storedJob{Job: job}
		}
		s.removeLocked(key, stored, now)
		removed = append(removed, key)
	}
	if len(removed) > 0 {
		s.scheduleWriteLocked()
	}
	onRemove := s.onRemove
	s.mu.Unlock()
	if len(removed) > 0 && onRemove != nil {
		onRemove(removed)
	}
	return removed
}

// scheduleWriteLocked marks the store changed and schedules its write.
func (s *jobStore) scheduleWriteLocked() {
	s.dirty = true
	if s.disabled || s.timer != nil {
		return
	}
	s.timer = time.AfterFunc(jobStoreWriteDelay, s.flush)
}

// flush writes the store when it has changed.
func (s *jobStore) flush() {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if !s.dirty || s.disabled || s.path == "" {
		s.mu.Unlock()
		return
	}
	s.dirty = false
	file := jobStoreFile{Version: jobStoreVersion, CachedAt: s.cachedAt, Jobs: maps.Clone(s.jobs), Cleared: maps.Clone(s.cleared)}
	if s.meta != nil {
		file.Snapshot = *s.meta
	}
	s.mu.Unlock()
	if err := store.WriteJSON(s.path, file); err != nil {
		slog.Warn("could not save the Code Loop job store", "component", logComponent, "err", err)
	}
}

// purgeTarget forgets every stored job and tombstone of target, for a
// project removed with its data.
func (s *jobStore) purgeTarget(target string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, stored := range s.jobs {
		if stored.Job.Target == target {
			delete(s.jobs, key)
		}
	}
	for key := range s.cleared {
		if isTargetJob(key, target) {
			delete(s.cleared, key)
		}
	}
	s.scheduleWriteLocked()
}

// close writes what is pending and stops writing.
func (s *jobStore) close() {
	s.flush()
	s.mu.Lock()
	s.disabled = true
	s.mu.Unlock()
}

// view is what display merges with the live snapshot: the saved totals and
// when they were saved, the stored jobs, and the live jobs to hide because
// they were cleared or expired.
type storeView struct {
	meta     *core.Snapshot
	cachedAt time.Time
	jobs     []JobSnapshot
	hidden   map[string]bool
}

// view returns the store's contents for display, without expired jobs, and
// which of live's jobs to hide.
func (s *jobStore) view(live []JobSnapshot, ttl finishedTTL) storeView {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	v := storeView{meta: s.meta, cachedAt: s.cachedAt, hidden: map[string]bool{}}
	for _, stored := range s.jobs {
		if !expired(ttl, stored.Job, stored.FinishedAt, now) {
			v.jobs = append(v.jobs, stored.Job)
		}
	}
	for _, job := range live {
		key := jobKey(job)
		if tomb, ok := s.cleared[key]; ok && clearedRun(tomb, job) {
			v.hidden[key] = true
			continue
		}
		finishedAt := job.Finished
		if stored, ok := s.jobs[key]; ok {
			finishedAt = stored.FinishedAt
		}
		if expired(ttl, job, finishedAt, now) {
			v.hidden[key] = true
		}
	}
	return v
}

// displayState is what the status and job APIs show: the live snapshot with
// the job store filling in every job the engine does not report.
type displayState struct {
	core.Snapshot
	// CachedAt is when the stored totals were saved, zero when nothing
	// stale is shown.
	CachedAt time.Time
	// Stale lists the targets shown from the store, and Loading the
	// targets that have neither live nor stored data yet.
	Stale   []string
	Loading []string
	// staleJobs holds the keys of the jobs shown from the store and not yet
	// confirmed by live data.
	staleJobs map[string]bool
}

// jobStale reports whether job is shown from the store and not yet confirmed.
func (d displayState) jobStale(job JobSnapshot) bool {
	return d.staleJobs[jobKey(job)]
}

// anyStale reports whether any of targets, or any target without targets, is
// shown from the store or still loading.
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
// target's issues, or finished a whole poll that covered it. A target added
// since the engine started is not live until the engine restarts onto it.
func targetLive(live *core.Snapshot, target string) bool {
	if live == nil {
		return false
	}
	if len(live.Targets) > 0 && !slices.Contains(live.Targets, target) {
		return false
	}
	if _, ok := live.IssueCounts[target]; ok {
		return true
	}
	return !live.LastPoll.IsZero()
}

// watchedTargets is the targets to show: the live engine's, in its order,
// less those no longer configured, then the configured targets the engine
// has not started on yet. Without configured targets it is the live ones.
func watchedTargets(live *core.Snapshot, configured []string) []string {
	if live == nil || len(live.Targets) == 0 {
		return slices.Clone(configured)
	}
	if len(configured) == 0 {
		return slices.Clone(live.Targets)
	}
	targets := make([]string, 0, len(configured))
	for _, target := range live.Targets {
		if slices.Contains(configured, target) {
			targets = append(targets, target)
		}
	}
	for _, target := range configured {
		if !slices.Contains(targets, target) {
			targets = append(targets, target)
		}
	}
	return targets
}

// mergeStored builds the displayed state from the live snapshot, which is nil
// before the engine publishes one, and the job store, for the configured
// targets. The live jobs show first, less those cleared or expired. Every
// stored job the engine does not report shows too: on a target the engine has
// not polled since it started, every stored job, marked stale; on a polled
// target, only done and failed jobs, the history the engine no longer holds,
// since live data has replaced the rest. A target the engine has not polled
// shows its stored issue count, and stored targets no longer configured are
// dropped. Targets with no live or stored data show as loading. The
// configured targets win over the live engine's, so a project added or
// removed shows at once, before the engine restarts onto the change; the live
// targets stand in only when no settings resolve.
func mergeStored(live *core.Snapshot, stored storeView, configured []string) displayState {
	var display displayState
	if live != nil {
		display.Snapshot = *live
	}
	targets := watchedTargets(live, configured)
	display.Targets = targets
	polled := live != nil && !live.LastPoll.IsZero()
	storedTargets := map[string]bool{}
	if stored.meta != nil {
		for _, target := range stored.meta.Targets {
			storedTargets[target] = true
		}
	}
	for _, job := range stored.jobs {
		storedTargets[job.Target] = true
	}
	var stale []string
	for _, target := range targets {
		switch {
		case targetLive(live, target):
		case polled:
			// The store is never newer than a finished poll, so a target
			// the engine does not watch yet is loading rather than stale.
			display.Loading = append(display.Loading, target)
		case storedTargets[target]:
			stale = append(stale, target)
		default:
			display.Loading = append(display.Loading, target)
		}
	}
	display.Stale = stale
	jobs := make([]JobSnapshot, 0, len(display.Jobs)+len(stored.jobs))
	liveJobs := make(map[string]bool, len(display.Jobs))
	for _, job := range display.Jobs {
		key := jobKey(job)
		liveJobs[key] = true
		if !stored.hidden[key] {
			jobs = append(jobs, job)
		}
	}
	display.staleJobs = map[string]bool{}
	for _, job := range stored.jobs {
		key := jobKey(job)
		if liveJobs[key] || !slices.Contains(targets, job.Target) {
			continue
		}
		switch {
		case slices.Contains(stale, job.Target):
			display.staleJobs[key] = true
		case targetLive(live, job.Target) && jobFinished(job):
		default:
			continue
		}
		jobs = append(jobs, job)
	}
	sortJobSnapshots(jobs)
	display.Jobs = jobs
	display.Paused = 0
	for _, job := range jobs {
		if job.Status == "paused" {
			display.Paused++
		}
	}
	if len(stale) == 0 || stored.meta == nil {
		return display
	}
	cached := stored.meta
	display.CachedAt = stored.cachedAt
	display.IssueCounts = maps.Clone(display.IssueCounts)
	if display.IssueCounts == nil {
		display.IssueCounts = map[string]int{}
	}
	for _, target := range stale {
		if count, ok := cached.IssueCounts[target]; ok {
			display.IssueCounts[target] = count
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
