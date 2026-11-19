package projects

import (
	"context"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

func TestRecentMergeRecorderCountsPullRequestsMergedInTheLastDay(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	merges := newRecentMerges()
	checker := recordRecentMerges(stateChecker{
		7: {IssueState: "closed", PullRequests: []PullRequestWorkState{
			{Number: 8, State: "closed", Merged: true, MergedAt: now.Add(-time.Hour)},
			{Number: 9, State: "closed"},
			{Number: 10, State: "open"},
		}},
		11: {IssueState: "closed", PullRequests: []PullRequestWorkState{
			{Repository: "me/r", Number: 8, State: "closed", Merged: true, MergedAt: now.Add(-23 * time.Hour)},
			{Number: 12, State: "closed", Merged: true, MergedAt: now.Add(-25 * time.Hour)},
		}},
	}, merges)
	for _, number := range []int{7, 11, 7} {
		if _, err := checker.OriginatingWorkState(context.Background(), "o/r", number); err != nil {
			t.Fatal(err)
		}
	}
	if got := merges.count(now); got != 2 {
		t.Fatalf("merges in the last day = %d, want 2 (o/r#8 and me/r#8)", got)
	}
	if got := merges.count(now.Add(2 * time.Hour)); got != 1 {
		t.Fatalf("merges two hours later = %d, want 1", got)
	}
}

func TestStatusReportsMergedInTheLastDay(t *testing.T) {
	m := newTestManager(t)
	m.snapshot.Store(&core.Snapshot{Completed: 5, Merged24h: 3})
	jobs, ok := m.Status(t.Context(), 0)["jobs"].(map[string]int)
	if !ok {
		t.Fatalf("status jobs = %T", m.Status(t.Context(), 0)["jobs"])
	}
	if jobs["merged_24h"] != 3 || jobs["completed"] != 5 {
		t.Fatalf("status jobs = %v, want merged_24h 3 and completed 5", jobs)
	}
}
