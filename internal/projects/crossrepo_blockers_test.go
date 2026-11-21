package projects

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/lsegal/aviary/internal/projects/core"
)

// repoStates answers OriginatingWorkState per repository, so a lookup in the
// wrong repository reads the wrong issue's state.
type repoStates struct {
	*fakeSource
	mu     sync.Mutex
	states map[string]OriginatingWorkState
	reads  []string
}

func (r *repoStates) OriginatingWorkState(_ context.Context, repo string, number int) (OriginatingWorkState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := fmt.Sprintf("%s#%d", strings.ToLower(repo), number)
	r.reads = append(r.reads, key)
	return r.states[key], nil
}

func (r *repoStates) StacksEnabled(context.Context, string) (bool, error) { return true, nil }

// o/r#50 has an open pull request, but the issue's only blocker is
// other/lib#50: it is not stacked on o/r#50's pull request, and its paused
// stacked work reads other/lib#50 (issue #855).
func TestEngineResolvesCrossRepoBlockerInItsOwnRepository(t *testing.T) {
	issue := Issue{Number: 7, Repository: "o/r", State: "open", DependsOn: []IssueDependency{{Number: 50, State: "open", Repository: "other/lib"}}}
	checker := &repoStates{fakeSource: &fakeSource{}, states: map[string]OriginatingWorkState{
		"o/r#7":        {IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 15, State: "open", Stacked: true}}},
		"o/r#50":       {IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 60, State: "open"}}},
		"other/lib#50": {IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 61, State: "closed", Merged: true}}},
	}}
	w := &Engine{Repo: "o/r", Issues: checker}
	if pullRequest, ok := w.stackableOnDependency(context.Background(), checker, issue); ok {
		t.Fatalf("stackableOnDependency() = #%d, want a cross-repo blocker never stacked on", pullRequest)
	}
	wake, reason, _, _ := w.pausedWake(context.Background(), checker, core.PausedWork{Kind: core.PausedStacked}, issue)
	if wake != core.PausedResume || !strings.Contains(reason, "other/lib#50") {
		t.Fatalf("pausedWake() = %v (%q), want it resumed on other/lib#50's merged pull request", wake, reason)
	}
	for _, read := range checker.reads {
		if read == "o/r#50" {
			t.Fatalf("blocker state was read from o/r#50: %q", checker.reads)
		}
	}
}
