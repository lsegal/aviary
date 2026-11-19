package projects

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

// recentMergeWindow is how far back the dashboards count merged pull requests.
const recentMergeWindow = 24 * time.Hour

// recentMerges remembers when the pull requests of this run's jobs merged, so
// status can report how many merged in the last recentMergeWindow. It learns
// them from the issue states aviary already reads for active, paused, and
// completed work.
type recentMerges struct {
	mu sync.Mutex
	// at maps a pull request's lowercase "repo#number" key to when it merged.
	at map[string]time.Time
}

func newRecentMerges() *recentMerges {
	return &recentMerges{at: make(map[string]time.Time)}
}

// record notes every merged pull request in state, which belongs to issue
// repo#number. A pull request whose repository was not recorded is hosted in
// the issue's own repository.
func (m *recentMerges) record(repo string, state OriginatingWorkState) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, pullRequest := range state.PullRequests {
		if !pullRequest.Merged || pullRequest.MergedAt.IsZero() || pullRequest.Number <= 0 {
			continue
		}
		key := strings.ToLower(pullRequest.RepoOr(repo)) + "#" + strconv.Itoa(pullRequest.Number)
		m.at[key] = pullRequest.MergedAt
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
		r.merges.record(repo, state)
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
