package projects

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/lsegal/aviary/internal/projects/core"
)

// Issue #1661: a poll after one job changes reads that job's entry, not the
// whole list.

func deltaJobs(n int, changed map[int]string) []core.JobSnapshot {
	jobs := make([]core.JobSnapshot, 0, n)
	for i := 1; i <= n; i++ {
		status := "complete"
		if s, ok := changed[i]; ok {
			status = s
		}
		jobs = append(jobs, core.JobSnapshot{Number: i, Target: "o/r", Kind: core.JobKindIssue, Status: status, Title: fmt.Sprintf("Job %d", i)})
	}
	return jobs
}

func listKeys(list map[string]any) []string {
	var keys []string
	for _, job := range list["jobs"].([]map[string]any) {
		keys = append(keys, job["key"].(string))
	}
	return keys
}

func TestBoardListDeltaCarriesOnlyChangedEntries(t *testing.T) {
	m := newTestManager(t)
	m.snapshot.Store(&core.Snapshot{Jobs: deltaJobs(200, nil)})
	q := JobListQuery{Limit: boardListLimit, Revision: NewRevision, Delta: true}
	first := m.ListJobsPage(q)
	if first["delta"] != nil || len(first["jobs"].([]map[string]any)) != 200 {
		t.Fatalf("first list delta=%v jobs=%d, want the full list", first["delta"], len(first["jobs"].([]map[string]any)))
	}
	full, _ := json.Marshal(first)

	m.snapshot.Store(&core.Snapshot{Jobs: deltaJobs(200, map[int]string{150: "failed"})})
	q.Revision = first["revision"].(string)
	delta := m.ListJobsPage(q)
	if delta["delta"] != true {
		t.Fatalf("poll after one change = %v, want a delta", delta)
	}
	if got := listKeys(delta); !slices.Equal(got, []string{"o/r#150"}) {
		t.Errorf("delta jobs = %v, want only o/r#150", got)
	}
	if removed := delta["removed"].([]string); len(removed) != 0 {
		t.Errorf("removed = %v, want none", removed)
	}
	if delta["total"] != 200 || delta["revision"] == q.Revision || delta["revision"] == "" {
		t.Errorf("delta total=%v revision=%v, want 200 and a new revision", delta["total"], delta["revision"])
	}
	small, _ := json.Marshal(delta)
	if len(small)*2 > len(full) {
		t.Errorf("delta is %d bytes, full list %d; want well under half", len(small), len(full))
	}

	// The next poll diffs against the delta's revision.
	q.Revision = delta["revision"].(string)
	if again := m.ListJobsPage(q); again["unchanged"] != true {
		t.Errorf("poll after the delta = %v, want unchanged", again)
	}
}

func TestBoardListDeltaReportsRemovedAndAddedKeys(t *testing.T) {
	m := newTestManager(t)
	m.snapshot.Store(&core.Snapshot{Jobs: deltaJobs(3, nil)})
	q := JobListQuery{Revision: NewRevision, Delta: true}
	first := m.ListJobsPage(q)
	jobs := deltaJobs(4, nil)[1:] // #1 gone, #4 added
	m.snapshot.Store(&core.Snapshot{Jobs: jobs})
	q.Revision = first["revision"].(string)
	delta := m.ListJobsPage(q)
	if got := listKeys(delta); !slices.Equal(got, []string{"o/r#4"}) {
		t.Errorf("delta jobs = %v, want only the added o/r#4", got)
	}
	if removed := delta["removed"].([]string); !slices.Equal(removed, []string{"o/r#1"}) {
		t.Errorf("removed = %v, want o/r#1", removed)
	}
	keys, _ := delta["keys"].([]string)
	if len(keys) != 3 || slices.Contains(keys, "o/r#1") || !slices.Contains(keys, "o/r#4") {
		t.Errorf("keys = %v, want the three current entries", keys)
	}
}

func TestBoardListDeltaKeepsOrderWhenOnlyContentChanges(t *testing.T) {
	m := newTestManager(t)
	m.snapshot.Store(&core.Snapshot{Jobs: deltaJobs(3, nil)})
	q := JobListQuery{Revision: NewRevision, Delta: true}
	first := m.ListJobsPage(q)
	jobs := deltaJobs(3, nil)
	jobs[1].Title = "Renamed"
	m.snapshot.Store(&core.Snapshot{Jobs: jobs})
	q.Revision = first["revision"].(string)
	delta := m.ListJobsPage(q)
	if got := listKeys(delta); !slices.Equal(got, []string{"o/r#2"}) {
		t.Errorf("delta jobs = %v, want only o/r#2", got)
	}
	if _, ok := delta["keys"]; ok {
		t.Errorf("delta carries keys %v though the order is the same", delta["keys"])
	}
}

func TestBoardListDeltaFallsBackToFullList(t *testing.T) {
	m := newTestManager(t)
	m.snapshot.Store(&core.Snapshot{Jobs: deltaJobs(3, nil)})
	full := m.ListJobsPage(JobListQuery{Revision: "too-old", Delta: true})
	if full["delta"] != nil || len(full["jobs"].([]map[string]any)) != 3 || full["revision"] == "" {
		t.Errorf("unknown revision = %v, want the full list with a revision", full)
	}

	// Without delta, a changed list is always the full list.
	q := JobListQuery{Revision: full["revision"].(string)}
	m.snapshot.Store(&core.Snapshot{Jobs: deltaJobs(3, map[int]string{2: "failed"})})
	if list := m.ListJobsPage(q); list["delta"] != nil || len(list["jobs"].([]map[string]any)) != 3 {
		t.Errorf("changed list without delta = %v, want the full list", list)
	}

	// A revision pushed out of the cache can't be diffed.
	m2 := newTestManager(t)
	m2.snapshot.Store(&core.Snapshot{Jobs: deltaJobs(1, nil)})
	old := m2.ListJobsPage(JobListQuery{Revision: NewRevision, Delta: true})["revision"].(string)
	for i := range listDeltaRevisions {
		m2.snapshot.Store(&core.Snapshot{Jobs: deltaJobs(1, map[int]string{1: fmt.Sprintf("s%d", i)})})
		m2.ListJobsPage(JobListQuery{Revision: NewRevision})
	}
	m2.snapshot.Store(&core.Snapshot{Jobs: deltaJobs(1, map[int]string{1: "failed"})})
	if list := m2.ListJobsPage(JobListQuery{Revision: old, Delta: true}); list["delta"] != nil {
		t.Errorf("evicted revision = %v, want the full list", list)
	}
}
