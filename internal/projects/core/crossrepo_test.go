package core

import (
	"slices"
	"testing"
)

// A blocker in another repository can share its number with an issue in the
// dependent issue's own repository; nothing keyed by dependency mixes the two
// up (issue #855).
func TestOpenDependenciesResolveTheirRepository(t *testing.T) {
	issue := Issue{Number: 7, Repository: "o/r", DependsOn: []IssueDependency{
		{Number: 50, State: "open"},
		{Number: 50, State: "open", Repository: "other/lib"},
		{Number: 51, State: "closed"},
	}}
	open := OpenDependencies(issue)
	var keys []string
	for _, dependency := range open {
		keys = append(keys, dependency.Key())
	}
	if want := []string{"o/r#50", "other/lib#50"}; !slices.Equal(keys, want) {
		t.Fatalf("OpenDependencies() keys = %q, want %q", keys, want)
	}
	if got, want := DependencyReferences("o/r", open), []string{"#50", "other/lib#50"}; !slices.Equal(got, want) {
		t.Fatalf("DependencyReferences() = %q, want %q", got, want)
	}
}

func TestStackableDependencyRejectsCrossRepoBlocker(t *testing.T) {
	for _, test := range []struct {
		name  string
		issue Issue
		want  bool
	}{
		{"blocker in another repository", Issue{Repository: "o/r", DependsOn: []IssueDependency{{Number: 50, State: "open", Repository: "other/lib"}}}, false},
		{"blocker in the same repository named explicitly", Issue{Repository: "o/r", DependsOn: []IssueDependency{{Number: 50, State: "open", Repository: "O/R"}}}, true},
		{"cross-repo blocker beside a closed local one", Issue{Repository: "o/r", DependsOn: []IssueDependency{{Number: 50, State: "closed"}, {Number: 50, State: "open", Repository: "other/lib"}}}, false},
	} {
		if _, ok := StackableDependency(test.issue); ok != test.want {
			t.Errorf("%s: StackableDependency() ok = %v, want %v", test.name, ok, test.want)
		}
	}
}

func TestWakePausedReadsCrossRepoBlockerByItsOwnRepository(t *testing.T) {
	issue := Issue{Number: 7, Repository: "o/r", State: "open", DependsOn: []IssueDependency{{Number: 50, State: "open", Repository: "other/lib"}}}
	own := &OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 15, State: "open", Stacked: true}}}
	merged := OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 14, State: "closed", Merged: true}}}
	stack := PausedWork{Kind: PausedStacked}
	// o/r#50's merged pull request says nothing about other/lib#50.
	if got, reason := WakePaused(stack, PausedCheck{Issue: issue, Own: own, Blockers: map[string]OriginatingWorkState{"o/r#50": merged}}); got != PausedStay {
		t.Fatalf("WakePaused() = %v (%q), want it to stay on other/lib#50", got, reason)
	}
	got, reason := WakePaused(stack, PausedCheck{Issue: issue, Own: own, Blockers: map[string]OriginatingWorkState{"other/lib#50": merged}})
	if got != PausedResume || reason != "its blocker other/lib#50 no longer has an open pull request" {
		t.Fatalf("WakePaused() = %v (%q), want it resumed on other/lib#50", got, reason)
	}
}
