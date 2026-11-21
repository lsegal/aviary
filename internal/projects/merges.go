package projects

import (
	"context"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/store"
)

// recentMergeWindow is how far back the dashboards count merged pull requests.
const recentMergeWindow = 24 * time.Hour

// recentMerges remembers when the pull requests of this run's jobs merged, so
// status can report how many merged in the last recentMergeWindow. It learns
// them from the issue states aviary already reads for active, paused, and
// completed work, and keeps them in a file beside the engine's work state so
// the count survives a restart.
type recentMerges struct {
	mu sync.Mutex
	// at maps a pull request's lowercase "repo#number" key to when it merged.
	at map[string]time.Time
	// path is where the merges are saved; empty keeps them in memory only.
	path string
	// onMerge is told of each pull request merged within recentMergeWindow
	// the first time it is recorded, with its issue's repo and number. Nil
	// tells nothing.
	onMerge func(repo string, number int, mergedAt time.Time)
}

// newRecentMerges restores the merges saved at path within recentMergeWindow
// of now. A missing or unreadable file starts empty, since the count is only
// a dashboard figure.
func newRecentMerges(path string, now time.Time) *recentMerges {
	m := &recentMerges{at: make(map[string]time.Time), path: path}
	if path == "" {
		return m
	}
	saved, err := store.ReadJSON[map[string]time.Time](path)
	if err != nil {
		return m
	}
	cutoff := now.Add(-recentMergeWindow)
	for key, mergedAt := range saved {
		if !mergedAt.Before(cutoff) {
			m.at[key] = mergedAt
		}
	}
	return m
}

// mergesPathFor names the merges file kept beside statePath, the way
// clonesPathFor names the clone registry.
func mergesPathFor(statePath string) string {
	if statePath == "" {
		return ""
	}
	return strings.TrimSuffix(statePath, filepath.Ext(statePath)) + ".merges.json"
}

// record notes every merged pull request in state, which belongs to issue
// repo#number. A pull request whose repository was not recorded is hosted in
// the issue's own repository.
func (m *recentMerges) record(repo string, number int, state OriginatingWorkState) {
	if m == nil {
		return
	}
	now := time.Now()
	cutoff := now.Add(-recentMergeWindow)
	var merged []time.Time
	m.mu.Lock()
	changed := false
	for _, pullRequest := range state.PullRequests {
		if !pullRequest.Merged || pullRequest.MergedAt.IsZero() || pullRequest.Number <= 0 {
			continue
		}
		key := strings.ToLower(pullRequest.RepoOr(repo)) + "#" + strconv.Itoa(pullRequest.Number)
		previous, seen := m.at[key]
		if seen && previous.Equal(pullRequest.MergedAt) {
			continue
		}
		// A merge older than the window may already have been reported and
		// forgotten since, so only newly seen recent merges are reported.
		if !seen && !pullRequest.MergedAt.Before(cutoff) {
			merged = append(merged, pullRequest.MergedAt)
		}
		m.at[key] = pullRequest.MergedAt
		changed = true
	}
	if changed {
		m.save(now)
	}
	onMerge := m.onMerge
	m.mu.Unlock()
	if onMerge != nil {
		for _, mergedAt := range merged {
			onMerge(repo, number, mergedAt)
		}
	}
}

// save writes the merges within recentMergeWindow of now to m.path. Callers
// hold m.mu. A failed write only costs the count after the next restart, so
// it is logged rather than returned.
func (m *recentMerges) save(now time.Time) {
	if m.path == "" {
		return
	}
	cutoff := now.Add(-recentMergeWindow)
	recent := make(map[string]time.Time, len(m.at))
	for key, mergedAt := range m.at {
		if !mergedAt.Before(cutoff) {
			recent[key] = mergedAt
		}
	}
	if err := store.WriteJSON(m.path, recent); err != nil {
		slog.Warn("projects: saving recent merges failed", "component", "projects", "path", m.path, "err", err)
	}
}

// count returns how many recorded pull requests merged within
// recentMergeWindow before now, forgetting older ones.
func (m *recentMerges) count(now time.Time) int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := now.Add(-recentMergeWindow)
	count := 0
	for key, mergedAt := range m.at {
		if mergedAt.Before(cutoff) {
			delete(m.at, key)
			continue
		}
		count++
	}
	return count
}

// recentMergeRecorder is a WorkClosureChecker that records the merged pull
// requests of every issue state it reads.
type recentMergeRecorder struct {
	WorkClosureChecker
	merges *recentMerges
}

func (r recentMergeRecorder) OriginatingWorkState(ctx context.Context, repo string, number int) (OriginatingWorkState, error) {
	state, err := r.WorkClosureChecker.OriginatingWorkState(ctx, repo, number)
	if err == nil {
		r.merges.record(repo, number, state)
	}
	return state, err
}

// recordRecentMerges wraps checker so the states it reads feed merges. A nil
// checker stays nil.
func recordRecentMerges(checker WorkClosureChecker, merges *recentMerges) WorkClosureChecker {
	if checker == nil {
		return nil
	}
	return recentMergeRecorder{WorkClosureChecker: checker, merges: merges}
}
