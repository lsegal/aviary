package projects

import (
	"slices"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/projects/core"
)

func scheduledRun(number int, status string) core.JobSnapshot {
	started := time.Unix(int64(number), 0)
	run := core.JobSnapshot{Number: number, Target: "o/r", Kind: core.JobKindSchedule, ScheduleID: "a/release", Title: "release", Status: status, Started: started}
	if status == "complete" || status == "failed" {
		run.Finished = started.Add(42 * time.Second)
		run.Summary = "summary of run " + time.Unix(int64(number), 0).UTC().Format("15:04:05")
	}
	return run
}

func TestListJobsCollapsesScheduledRuns(t *testing.T) {
	m := newTestManager(t)
	next := time.Date(2026, 11, 23, 10, 20, 0, 0, time.UTC)
	m.SetScheduleSource(func() []domain.ScheduledTask {
		return []domain.ScheduledTask{{ID: "a/release", Enabled: true, NextRun: &next}}
	})
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{
		{Number: 7, Target: "o/r", Status: "complete", Kind: core.JobKindIssue},
		scheduledRun(1000, "complete"),
		scheduledRun(1300, "complete"),
		scheduledRun(1600, "complete"),
	}})
	list := m.ListJobs([]string{"o/r"}, "", nil, nil, "", 0, 0)
	jobs := list["jobs"].([]map[string]any)
	if len(jobs) != 2 || list["total"] != 2 {
		t.Fatalf("jobs = %v, want the issue and one entry for the schedule", jobs)
	}
	entry := jobs[1]
	if entry["number"] != 1600 || entry["state"] != core.BoardScheduled {
		t.Errorf("schedule entry = #%v %v, want the latest run #1600, scheduled", entry["number"], entry["state"])
	}
	last := entry["last_run"].(map[string]any)
	if last["number"] != 1600 || last["result"] != "succeeded" || last["summary"] != scheduledRun(1600, "complete").Summary || last["duration_ms"] != int64(42000) {
		t.Errorf("last_run = %v", last)
	}
	if got := entry["next_run"].(map[string]any); got["at"] != next || got["paused"] != false || got["queued"] != false {
		t.Errorf("next_run = %v, want %v, not paused or queued", got, next)
	}
	var recent []int
	for _, run := range entry["recent_runs"].([]map[string]any) {
		recent = append(recent, run["number"].(int))
	}
	if !slices.Equal(recent, []int{1600, 1300, 1000}) {
		t.Errorf("recent_runs = %v, want newest first", recent)
	}
	// Finished scheduled runs never count or filter as done.
	if got := m.ListJobs([]string{"o/r"}, "", []string{core.BoardDone}, nil, "", 0, 0)["total"]; got != 1 {
		t.Errorf("done total = %v, want only the issue", got)
	}
	if got := m.Status(t.Context(), 0, []string{"o/r"})["states"].(map[string]int)[core.BoardDone]; got != 1 {
		t.Errorf("done count = %d, want only the issue", got)
	}
	if got := m.ListJobs([]string{"o/r"}, "", nil, []string{core.JobKindIssue}, "", 0, 0)["total"]; got != 1 {
		t.Errorf("issue kind total = %v, want 1", got)
	}
}

func TestScheduleEntryRunsInPlace(t *testing.T) {
	m := newTestManager(t)
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{
		scheduledRun(1000, "complete"),
		scheduledRun(1300, "active"),
	}})
	jobs := m.ListJobs(nil, "", nil, nil, "", 0, 0)["jobs"].([]map[string]any)
	if len(jobs) != 1 || jobs[0]["number"] != 1300 || jobs[0]["state"] != core.BoardRunning {
		t.Fatalf("jobs = %v, want the running run as the one entry", jobs)
	}
	if last := jobs[0]["last_run"].(map[string]any); last["number"] != 1000 {
		t.Errorf("last_run = %v, want the finished run while the next one runs", last)
	}
	if got := m.Status(t.Context(), 0, nil)["states"].(map[string]int)[core.BoardRunning]; got != 1 {
		t.Errorf("running count = %d, want the schedule once", got)
	}
}

func TestScheduleEntryFailedUntilSuccessOrDismissed(t *testing.T) {
	m := newTestManager(t)
	failed := scheduledRun(1300, "failed")
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{scheduledRun(1000, "complete"), failed}})
	state := func() any { return m.ListJobs(nil, "", nil, nil, "", 0, 0)["jobs"].([]map[string]any)[0]["state"] }
	if got := state(); got != core.BoardFailed {
		t.Errorf("state after a failed run = %v, want failed", got)
	}
	if got := m.Status(t.Context(), 0, nil)["states"].(map[string]int)[core.BoardFailed]; got != 1 {
		t.Errorf("failed count = %d, want 1", got)
	}
	failed.Dismissed = true
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{scheduledRun(1000, "complete"), failed}})
	if got := state(); got != core.BoardScheduled {
		t.Errorf("state after dismissing = %v, want scheduled", got)
	}
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{scheduledRun(1000, "failed"), scheduledRun(1300, "complete")}})
	if got := state(); got != core.BoardScheduled {
		t.Errorf("state after a later success = %v, want scheduled", got)
	}
}

func TestScheduleNextRun(t *testing.T) {
	next := time.Now()
	for _, tc := range []struct {
		name   string
		task   domain.ScheduledTask
		paused bool
		queued bool
		at     bool
	}{
		{"armed", domain.ScheduledTask{Enabled: true, NextRun: &next}, false, false, true},
		{"paused", domain.ScheduledTask{NextRun: &next}, true, false, false},
		{"running", domain.ScheduledTask{Enabled: true, History: []domain.TaskRun{{Status: domain.TaskRunRunning}}}, false, false, false},
		{"queued behind a run", domain.ScheduledTask{Enabled: true, History: []domain.TaskRun{{Status: domain.TaskRunRunning}, {Status: domain.TaskRunQueued}}}, false, true, false},
	} {
		got := scheduleNextRun(tc.task)
		_, at := got["at"]
		if got["paused"] != tc.paused || got["queued"] != tc.queued || at != tc.at {
			t.Errorf("%s: next_run = %v", tc.name, got)
		}
	}
}

func TestRunResult(t *testing.T) {
	for _, tc := range []struct {
		run  core.JobSnapshot
		want string
	}{
		{core.JobSnapshot{Status: "complete"}, "succeeded"},
		{core.JobSnapshot{Status: "complete", Fallback: true}, "fallback"},
		{core.JobSnapshot{Status: "failed", Fallback: true}, "failed"},
	} {
		if got := runResult(tc.run); got != tc.want {
			t.Errorf("runResult(%+v) = %s, want %s", tc.run, got, tc.want)
		}
	}
}
