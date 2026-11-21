package projects

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

func TestRecentMergeRecorderCountsPullRequestsMergedInTheLastDay(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	merges := newRecentMerges("", now)
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
	jobs, ok := m.Status(t.Context(), 0, nil)["jobs"].(map[string]int)
	if !ok {
		t.Fatalf("status jobs = %T", m.Status(t.Context(), 0, nil)["jobs"])
	}
	if jobs["merged_24h"] != 3 || jobs["completed"] != 5 {
		t.Fatalf("status jobs = %v, want merged_24h 3 and completed 5", jobs)
	}
}

func TestRecentMergesSurviveEngineRestart(t *testing.T) {
	now := time.Now()
	statePath := filepath.Join(t.TempDir(), "state.json")
	before := newRecentMerges(mergesPathFor(statePath), now)
	before.record("o/r", OriginatingWorkState{IssueState: "closed", PullRequests: []PullRequestWorkState{
		{Number: 8, State: "closed", Merged: true, MergedAt: now.Add(-3 * time.Hour)},
		{Number: 9, State: "closed", Merged: true, MergedAt: now.Add(-25 * time.Hour)},
	}})

	saved, err := os.ReadFile(mergesPathFor(statePath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "o/r#9") {
		t.Fatalf("saved merges kept a merge older than a day: %s", saved)
	}

	reporter := &snapshotReporter{}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: statePath,
		Issues: &fakeSource{batches: [][]Issue{{}}}, Runner: &fakeRunner{}, UI: reporter,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	if len(reporter.snapshots) == 0 {
		t.Fatal("engine published no snapshots")
	}
	if got := reporter.snapshots[len(reporter.snapshots)-1].Merged24h; got != 1 {
		t.Fatalf("Merged24h after restart = %d, want 1 (o/r#8 only)", got)
	}
}

func TestRecentMergesRestoreOnlyTheLastDay(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "state.merges.json")
	data := `{"o/r#8": "` + now.Add(-time.Hour).Format(time.RFC3339) + `", "o/r#9": "` + now.Add(-25*time.Hour).Format(time.RFC3339) + `"}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	merges := newRecentMerges(path, now)
	if _, ok := merges.at["o/r#9"]; ok {
		t.Fatal("restored a merge older than a day")
	}
	if got := merges.count(now); got != 1 {
		t.Fatalf("restored merges = %d, want 1", got)
	}
}
