package projects

import (
	"context"
	"fmt"
	"sync"

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
}

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

// cloneUsage gathers what deciding whether a clone is in use reads.
func (w *Engine) cloneUsage() cloneUsage {
	usage := cloneUsage{roots: w.CloneRoots(), live: w.Active.checkouts()}
	w.clones.mu.Lock()
	view := w.clones.view
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
