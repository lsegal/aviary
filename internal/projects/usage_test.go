package projects

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/domain"
)

type usageLog struct {
	mu      sync.Mutex
	records []domain.UsageRecord
}

func (l *usageLog) record(rec domain.UsageRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, rec)
}

func (l *usageLog) snapshot() []domain.UsageRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]domain.UsageRecord(nil), l.records...)
}

// failingRunner fails every run after taking a moment, so its wall time is
// measurable.
type failingRunner struct{}

func (failingRunner) Run(context.Context, Issue) error {
	time.Sleep(20 * time.Millisecond)
	return errors.New("agent crashed")
}

func TestEngineRecordsProjectRunWallTimeAndFailure(t *testing.T) {
	usage := &usageLog{}
	w := &Engine{
		Repo: "O/R", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(t.TempDir(), "state.json"),
		Issues: &fakeSource{batches: [][]Issue{{{Number: 7}}}}, Runner: failingRunner{}, RecordUsage: usage.record,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(usage.snapshot()) < 2 {
		time.Sleep(time.Millisecond)
	}
	records := usage.snapshot()
	if len(records) != 2 {
		t.Fatalf("usage records = %+v, want a run and a failure", records)
	}
	run, failure := records[0], records[1]
	for _, rec := range records {
		if rec.AgentID != UsageAgentID || rec.SessionID != "o/r#7" || rec.Project == nil || rec.Project.Issue != "o/r#7" {
			t.Fatalf("project usage record = %+v, want agent %q and issue o/r#7", rec, UsageAgentID)
		}
		if rec.InputTokens != 0 || rec.OutputTokens != 0 {
			t.Fatalf("project usage record carries tokens: %+v", rec)
		}
	}
	if run.Project.Outcome != "" || run.Project.DurationMS < 20 {
		t.Fatalf("run record = %+v, want at least 20ms of wall time and no outcome", *run.Project)
	}
	if failure.Project.Outcome != domain.ProjectOutcomeFailed || failure.Project.DurationMS != 0 {
		t.Fatalf("failure record = %+v, want a failed outcome", *failure.Project)
	}
}

func TestRecentMergesReportEachNewRecentMergeOnce(t *testing.T) {
	now := time.Now()
	path := mergesPathFor(filepath.Join(t.TempDir(), "state.json"))
	merges := newRecentMerges(path, now)
	type merge struct {
		repo      string
		number    int
		mergedAt  time.Time
		additions int
		deletions int
	}
	var reported []merge
	merges.onMerge = func(repo string, number int, pullRequest PullRequestWorkState) {
		reported = append(reported, merge{repo, number, pullRequest.MergedAt, pullRequest.Additions, pullRequest.Deletions})
	}
	state := OriginatingWorkState{IssueState: "closed", PullRequests: []PullRequestWorkState{
		{Number: 8, State: "closed", Merged: true, MergedAt: now.Add(-time.Hour), Additions: 30, Deletions: 4},
		{Number: 9, State: "closed", Merged: true, MergedAt: now.Add(-25 * time.Hour)},
		{Number: 10, State: "open"},
	}}
	merges.record("o/r", 7, state)
	merges.record("o/r", 7, state)
	if len(reported) != 1 || reported[0] != (merge{"o/r", 7, now.Add(-time.Hour), 30, 4}) {
		t.Fatalf("reported merges = %+v, want only o/r#8's merge and diff size, once, for issue o/r#7", reported)
	}

	// A restarted engine restores the merges it already reported, so reading
	// the same state again does not count them twice.
	restarted := newRecentMerges(path, now)
	restarted.onMerge = merges.onMerge
	restarted.record("o/r", 7, state)
	if len(reported) != 1 {
		t.Fatalf("reported merges after a restart = %+v, want no new report", reported)
	}
}
