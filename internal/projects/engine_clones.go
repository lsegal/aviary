package projects

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// cloneState is a Engine's temp-directory setting and clone bookkeeping (issue
// #830).
type cloneState struct {
	mu sync.Mutex
	// tempDirs is the configured, ordered list of temp directories. Empty
	// means the system temp directory.
	tempDirs []string
	registry *cloneRegistry
	// view reads the run loop's live work state and jobs; nil before the
	// loop starts, when the state file is read instead.
	view func() (map[string]workState, map[string]JobSnapshot)
	// leased holds the clones handed to a job until its work state records
	// them, keyed by cloneKey, with the job's key (issue #622).
	leased map[string]string
	// leaseMu serializes choosing a clone to reuse, so two jobs are never
	// handed the same one, and removing an unused clone, so none is removed
	// while it is being handed to a job.
	leaseMu sync.Mutex
	// reapInterval overrides cloneReapInterval in tests, and reaping keeps
	// two sweeps from overlapping.
	reapInterval time.Duration
	reaping      atomic.Bool
	// released, when set, is called with each job key once releaseJobClone
	// has finished, so tests wait for it instead of sleeping (issue #806).
	released func(key string)
}

// cloneReapInterval is how often unused clones are removed in the
// background, as a backstop for clones a finished job's own cleanup missed
// (issue #752).
const cloneReapInterval = 15 * time.Minute

// SetTempDirs replaces the ordered temp directories new clones are placed in.
func (w *Engine) SetTempDirs(dirs []string) {
	w.clones.mu.Lock()
	w.clones.tempDirs = append([]string(nil), dirs...)
	w.clones.mu.Unlock()
}

// TempDirs reports the configured temp directories, empty for the system
// temp directory.
func (w *Engine) TempDirs() []string {
	w.clones.mu.Lock()
	defer w.clones.mu.Unlock()
	return append([]string{}, w.clones.tempDirs...)
}

// CloneRoots reports the sanctioned directories, in the order new clones try
// them.
func (w *Engine) CloneRoots() []string {
	return cloneRoots(w.TempDirs())
}

// CloneRoot picks the directory a new run's agent starts in and clones under:
// the first sanctioned root with room for a clone.
func (w *Engine) CloneRoot() (string, error) {
	return pickCloneRoot(w.CloneRoots())
}

func (w *Engine) setCloneView(view func() (map[string]workState, map[string]JobSnapshot)) {
	w.clones.mu.Lock()
	w.clones.view = view
	w.clones.mu.Unlock()
}

// cloneRegistry loads the registry beside the state file on first use.
func (w *Engine) cloneRegistry() (*cloneRegistry, error) {
	w.clones.mu.Lock()
	defer w.clones.mu.Unlock()
	if w.clones.registry != nil {
		return w.clones.registry, nil
	}
	reg, err := loadCloneRegistry(clonesPathFor(w.StatePath))
	if err != nil {
		return nil, err
	}
	w.clones.registry = reg
	return reg, nil
}

// recordClone books the checkout a run announced.
func (w *Engine) recordClone(key, target string, issue Issue, checkout string) {
	reg, err := w.cloneRegistry()
	if err != nil {
		w.logf("issue #%d could not record clone %s: %v", issue.Number, checkout, err)
		return
	}
	if !underRoots(checkout, w.CloneRoots()) {
		w.logf("issue #%d checkout %s is outside the configured temp directories", issue.Number, checkout)
	}
	if err := reg.record(cloneRecord{Path: checkout, Repository: issueRepository(target, issue), Issue: issue.Number, Job: key}); err != nil {
		w.logf("issue #%d could not save clone record: %v", issue.Number, err)
	}
}

// leaseReusableClone hands job key a free clone of repo, reserving it until
// releaseCloneLease, or returns "" when no clone of repo is free. The caller
// records the clone in the job's work state before releasing the lease.
func (w *Engine) leaseReusableClone(key, repo string) string {
	reg, err := w.cloneRegistry()
	if err != nil {
		return ""
	}
	w.clones.leaseMu.Lock()
	defer w.clones.leaseMu.Unlock()
	// Only booked clones are candidates; scanning the temp directories for
	// leftovers is left to listing, off the dispatch path.
	usage := w.cloneUsage()
	rec, ok := pickReusableClone(reg, repo, usage)
	if !ok {
		return ""
	}
	w.clones.mu.Lock()
	if w.clones.leased == nil {
		w.clones.leased = map[string]string{}
	}
	w.clones.leased[cloneKey(rec.Path)] = key
	w.clones.mu.Unlock()
	return rec.Path
}

// releaseCloneLease ends the reservation leaseReusableClone made.
func (w *Engine) releaseCloneLease(path string) {
	w.clones.mu.Lock()
	delete(w.clones.leased, cloneKey(path))
	w.clones.mu.Unlock()
}

// prepareReusedClone resets a reused clone for its new job within
// cloneReuseBudget.
func (w *Engine) prepareReusedClone(ctx context.Context, dir string) error {
	git := w.checkpointGit
	if git == nil {
		git = runCheckpointGit
	}
	ctx, cancel := context.WithTimeout(ctx, cloneReuseBudget)
	defer cancel()
	return prepareReusedClone(ctx, git, dir)
}

// reassignClone books a reused clone to its new job, dropping what its
// earlier job recorded about it.
func (w *Engine) reassignClone(key, target string, issue Issue, checkout string) {
	if reg, err := w.cloneRegistry(); err == nil {
		_ = reg.remove(checkout)
	}
	w.recordClone(key, target, issue, checkout)
}

// cloneUsage gathers what deciding whether a clone is in use reads.
func (w *Engine) cloneUsage() cloneUsage {
	usage := cloneUsage{roots: w.CloneRoots(), live: w.Active.checkouts()}
	w.clones.mu.Lock()
	view := w.clones.view
	if len(w.clones.leased) > 0 {
		usage.leased = make(map[string]string, len(w.clones.leased))
		for path, key := range w.clones.leased {
			usage.leased[path] = key
		}
	}
	w.clones.mu.Unlock()
	if view != nil {
		usage.work, usage.jobs = view()
		return usage
	}
	usage.work, usage.workErr = loadScopedWorkState(w.StatePath, w.Targets)
	return usage
}

// ListClones enumerates every recorded clone with its size, adopting
// leftovers found on disk first.
func (w *Engine) ListClones(ctx context.Context) (core.ClonesSnapshot, error) {
	reg, err := w.cloneRegistry()
	if err != nil {
		return core.ClonesSnapshot{}, err
	}
	usage := w.cloneUsage()
	reg.adopt(usage.roots, usage.work)
	snapshot, err := listClones(reg, usage)
	if err != nil {
		return core.ClonesSnapshot{}, err
	}
	measureCloneSizes(ctx, &snapshot, cloneSizeTimeout, dirSize)
	return snapshot, nil
}

// ClearClones removes every clone nothing can still need.
func (w *Engine) ClearClones(context.Context) (core.ClearClonesResult, error) {
	reg, err := w.cloneRegistry()
	if err != nil {
		return core.ClearClonesResult{}, err
	}
	usage := w.cloneUsage()
	reg.adopt(usage.roots, usage.work)
	result, err := clearClones(reg, w.cloneUsage)
	if err != nil {
		return core.ClearClonesResult{}, err
	}
	for _, entry := range result.Removed {
		w.logf("removed unused clone %s (%s)", entry.Path, formatBytes(*entry.Size))
	}
	for _, failure := range result.Errors {
		w.logf("could not remove clone %s", failure)
	}
	if len(result.Removed) > 0 {
		w.logf("%s", clearClonesTotal(result))
	}
	return result, nil
}

// clearClonesTotal summarizes a clear in one log line.
func clearClonesTotal(result core.ClearClonesResult) string {
	noun := "clones"
	if len(result.Removed) == 1 {
		noun = "clone"
	}
	return fmt.Sprintf("cleared %d unused %s, reclaimed %s", len(result.Removed), noun, formatBytes(result.Reclaimed))
}

// reapClonesEvery removes unused clones every cloneReapInterval until ctx
// ends.
func (w *Engine) reapClonesEvery(ctx context.Context) {
	interval := w.clones.reapInterval
	if interval <= 0 {
		interval = cloneReapInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.reapClones()
		}
	}
}

// reapClones removes every recorded clone nothing uses, adopting leftovers
// found on disk first. A sweep still running when the next one is due is
// not overlapped.
func (w *Engine) reapClones() {
	if !w.clones.reaping.CompareAndSwap(false, true) {
		return
	}
	defer w.clones.reaping.Store(false)
	reg, err := w.cloneRegistry()
	if err != nil {
		w.logf("could not reap clones: %v", err)
		return
	}
	usage := w.cloneUsage()
	if usage.workErr != nil {
		w.logf("could not reap clones: %v: %v", errWorkStateUnreadable, usage.workErr)
		return
	}
	reg.adopt(usage.roots, usage.work)
	result := w.removeUnusedClones(reg, func(cloneRecord) bool { return true })
	if len(result.Removed) > 0 {
		w.logf("reaper %s", clearClonesTotal(result))
	}
}

// releaseJobClone removes the clone job key used once the job has finished,
// unless another job or agent still uses that directory. checkout is the
// job's last recorded checkout, which may be empty.
func (w *Engine) releaseJobClone(key, checkout string) {
	if released := w.clones.released; released != nil {
		defer released(key)
	}
	reg, err := w.cloneRegistry()
	if err != nil {
		return
	}
	if checkout != "" {
		if _, known := reg.get(checkout); !known && isDir(checkout) {
			_ = reg.record(cloneRecord{Path: checkout, Job: key})
		}
	}
	w.removeUnusedClones(reg, func(rec cloneRecord) bool {
		return rec.Job == key || (checkout != "" && cloneKey(rec.Path) == cloneKey(checkout))
	})
}

// removeUnusedClones removes each recorded clone match selects that no live
// job or agent of this instance uses and that belongs to this instance,
// logging each removal and the space it freed. Each clone is classified
// right before it is removed, under the lease lock, so a clone being handed
// to a job is never removed.
func (w *Engine) removeUnusedClones(reg *cloneRegistry, match func(cloneRecord) bool) core.ClearClonesResult {
	result := core.ClearClonesResult{Removed: []core.CloneEntry{}}
	for _, rec := range reg.list() {
		if !match(rec) {
			continue
		}
		if entry, ok := w.removeCloneIfUnused(reg, rec.Path); ok {
			result.Removed = append(result.Removed, entry)
			result.Reclaimed += *entry.Size
		}
	}
	return result
}

func (w *Engine) removeCloneIfUnused(reg *cloneRegistry, path string) (core.CloneEntry, bool) {
	w.clones.leaseMu.Lock()
	defer w.clones.leaseMu.Unlock()
	rec, ok := reg.get(path)
	if !ok {
		return core.CloneEntry{}, false
	}
	usage := w.cloneUsage()
	if usage.workErr != nil {
		return core.CloneEntry{}, false
	}
	verdict, reason := classifyClone(rec, usage)
	if verdict != cloneUnused || !ownedHere(rec, usage) || !w.safeToRemoveAutomatically(rec.Path) {
		return core.CloneEntry{}, false
	}
	size, _ := dirSize(context.Background(), rec.Path)
	if err := removeTree(rec.Path); err != nil {
		w.logf("could not remove unused clone %s: %v", rec.Path, err)
		return core.CloneEntry{}, false
	}
	_ = reg.remove(rec.Path)
	w.logf("removed unused clone %s (%s freed): %s", rec.Path, formatBytes(size), reason)
	entry := cloneEntry(rec, verdict, reason)
	entry.Size = &size
	return entry, true
}

// ownedHere reports whether rec belongs to this instance: recorded by one of
// its runs, or adopted from disk for a job its work state tracks. Removing
// clones automatically leaves clones another instance may own to a manual
// clear.
func ownedHere(rec cloneRecord, usage cloneUsage) bool {
	if !rec.Adopted {
		return true
	}
	state, ok := usage.work[rec.Job]
	return ok && state.Status != ""
}

// safeToRemoveAutomatically reports whether path looks like a clone that can
// be removed without anyone asking: a git checkout that does not hold this
// instance's state. A checkout recorded for a directory that is not a clone
// is left to a manual clear.
func (w *Engine) safeToRemoveAutomatically(path string) bool {
	if !isDir(filepath.Join(path, ".git")) {
		return false
	}
	return w.StatePath == "" || !underRoots(w.StatePath, []string{path})
}
