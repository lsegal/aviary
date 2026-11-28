package projects

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// Issue #1657: a large job store must not slow the Code Loop boards down.
// seedBoard fills the board with boardSeedJobs jobs as a busy instance has
// them: five schedules with 300 runs each and 500 finished issue jobs, each
// carrying the job store's full 32 KiB of scrollback.
const (
	boardSeedJobs      = 2000
	boardSeedSchedules = 5
	boardSeedIssues    = 500
	// boardListLimit is the board's page size when it reads every job.
	boardListLimit = 1000
	// boardListMaxBytes is the most a full board list of the seeded store
	// may serialize to. Card summaries take about 450 bytes each; any
	// scrollback, transcript or raw output would add 32 KiB per job.
	boardListMaxBytes = 512 * 1024
	// boardLoadTarget is the stated target for building a board's first
	// load, its status and full job list, from the seeded store.
	boardLoadTarget = time.Second
)

func seedBoard(t testing.TB, m *Manager) {
	t.Helper()
	now := time.Now()
	log := strings.Repeat("some scrollback output line\n", jobStoreLogBytes/28)
	jobs := make([]core.JobSnapshot, 0, boardSeedJobs)
	n := 1
	runs := (boardSeedJobs - boardSeedIssues) / boardSeedSchedules
	for s := range boardSeedSchedules {
		for range runs {
			started := now.Add(-time.Duration(n) * time.Minute)
			jobs = append(jobs, core.JobSnapshot{
				Number: n, Target: "o/r", Kind: core.JobKindSchedule, ScheduleID: fmt.Sprintf("task-%d", s),
				Status: "complete", Title: "scheduled", Started: started, Finished: started.Add(time.Second),
				Summary: "ok", Log: log,
			})
			n++
		}
	}
	for range boardSeedIssues {
		jobs = append(jobs, core.JobSnapshot{
			Number: n, Target: "o/r", Kind: core.JobKindIssue, Status: "complete", Title: "issue",
			PullRequest: n + 10000, PullRequestState: "MERGED", Started: now, Finished: now, Log: log,
		})
		n++
	}
	m.snapshot.Store(&core.Snapshot{Targets: []string{"o/r"}, LastPoll: now, Jobs: jobs})
}

func TestBoardListCarriesCardSummariesOnly(t *testing.T) {
	m := newTestManager(t)
	seedBoard(t, m)
	list := m.ListJobs(nil, "", nil, nil, "", 0, boardListLimit)
	data, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > boardListMaxBytes {
		t.Errorf("board list of %d jobs is %d bytes, want at most %d", boardSeedJobs, len(data), boardListMaxBytes)
	}
	jobs := list["jobs"].([]map[string]any)
	if want := boardSeedSchedules + boardSeedIssues; len(jobs) != want || list["total"] != want {
		t.Fatalf("board lists %d entries (total %v), want %d: one per schedule plus the issues", len(jobs), list["total"], want)
	}
	for _, job := range jobs {
		for _, key := range []string{"scrollback", "raw_output", "events", "log"} {
			if _, ok := job[key]; ok {
				t.Fatalf("%v carries %s; the board list holds card summaries only", job["key"], key)
			}
		}
		if runs, ok := job["recent_runs"].([]map[string]any); ok && len(runs) > recentScheduleRuns {
			t.Errorf("%v lists %d recent runs, want at most %d", job["key"], len(runs), recentScheduleRuns)
		}
	}
}

func TestBoardLoadMeetsTarget(t *testing.T) {
	m := newTestManager(t)
	seedBoard(t, m)
	started := time.Now()
	for _, v := range []any{
		m.Status(t.Context(), 0, []string{"o/r"}),
		m.ListJobs([]string{"o/r"}, "", core.BoardStates, nil, "", 0, boardListLimit),
		m.ListJobs([]string{"o/r"}, "", nil, []string{core.JobKindSchedule}, "", 0, boardListLimit),
	} {
		if _, err := json.Marshal(v); err != nil {
			t.Fatal(err)
		}
	}
	if took := time.Since(started); took > boardLoadTarget {
		t.Errorf("board load of %d jobs took %v, want under %v", boardSeedJobs, took, boardLoadTarget)
	}
}

func TestBoardListAddsCardOutputInOneRequest(t *testing.T) {
	m := newTestManager(t)
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{
		{Number: 1, Target: "o/r", Kind: core.JobKindIssue, Status: "active", Log: "a\nb\nc\nd\n"},
		{Number: 2, Target: "o/r", Kind: core.JobKindIssue, Status: "complete"},
		{Number: 3, Target: "o/r", Kind: core.JobKindSchedule, ScheduleID: "t", Status: "complete", Log: "old run"},
	}})
	list := m.ListJobsPage(JobListQuery{Targets: []string{"o/r"}, OutputLines: 2, OutputEvents: 5})
	got := map[int]any{}
	for _, job := range list["jobs"].([]map[string]any) {
		got[job["number"].(int)] = job["scrollback"]
	}
	if got[1] != "c\nd" {
		t.Errorf("#1 scrollback = %q, want its last two lines", got[1])
	}
	if got[2] != "" {
		t.Errorf("#2 scrollback = %q, want none for a job without output", got[2])
	}
	if got[3] != nil {
		t.Errorf("schedule between runs got scrollback %q; only a running schedule's entry carries output", got[3])
	}
}

func TestBoardListRevisionSkipsUnchangedPolls(t *testing.T) {
	m := newTestManager(t)
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{{Number: 1, Target: "o/r", Kind: core.JobKindIssue, Status: "active"}}})
	if _, ok := m.ListJobsPage(JobListQuery{})["revision"]; ok {
		t.Error("list without a revision request carries one")
	}
	first := m.ListJobsPage(JobListQuery{Revision: NewRevision})
	revision, _ := first["revision"].(string)
	if revision == "" {
		t.Fatal("list carries no revision")
	}
	again := m.ListJobsPage(JobListQuery{Revision: revision})
	if again["unchanged"] != true || again["jobs"] != nil {
		t.Errorf("unchanged poll = %v, want only unchanged and the revision", again)
	}
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{{Number: 1, Target: "o/r", Kind: core.JobKindIssue, Status: "complete"}}})
	changed := m.ListJobsPage(JobListQuery{Revision: revision})
	if changed["unchanged"] != nil || changed["revision"] == revision || len(changed["jobs"].([]map[string]any)) != 1 {
		t.Errorf("changed poll = %v, want the whole list with a new revision", changed)
	}
}

func TestScheduleEntryKeepsNewestRunsAndLastFinished(t *testing.T) {
	var jobs []core.JobSnapshot
	// The newest finished run, #5, is older than the 20 runs in flight.
	jobs = append(jobs, core.JobSnapshot{Number: 5, Target: "o/r", Kind: core.JobKindSchedule, ScheduleID: "t", Status: "failed"})
	for n := 1; n <= 4; n++ {
		jobs = append(jobs, core.JobSnapshot{Number: n, Target: "o/r", Kind: core.JobKindSchedule, ScheduleID: "t", Status: "complete"})
	}
	for n := 30; n > 10; n-- {
		jobs = append(jobs, core.JobSnapshot{Number: n, Target: "o/r", Kind: core.JobKindSchedule, ScheduleID: "t", Status: "active"})
	}
	entries := boardEntries(jobs)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want one for the schedule", len(entries))
	}
	entry := entries[0]
	if len(entry.runs) != recentScheduleRuns || entry.runs[0].Number != 30 || entry.runs[recentScheduleRuns-1].Number != 11 {
		t.Errorf("runs = %d from #%d, want the newest %d, #30 to #11", len(entry.runs), entry.runs[0].Number, recentScheduleRuns)
	}
	if entry.lastFinished == nil || entry.lastFinished.Number != 5 {
		t.Errorf("lastFinished = %v, want #5", entry.lastFinished)
	}
	if entry.job.Number != 30 {
		t.Errorf("entry job = #%d, want the latest run #30", entry.job.Number)
	}
}

func TestStatusCountsEachProjectsRunningAndWaitingJobs(t *testing.T) {
	m := newTestManager(t)
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{
		{Number: 1, Target: "o/r", Kind: core.JobKindIssue, Status: "active", CheckoutDirectory: "/tmp/r-1"},
		{Number: 2, Target: "o/r", Kind: core.JobKindIssue, Status: "queued"},
		{Number: 3, Target: "o/r", Kind: core.JobKindIssue, Status: "complete"},
		{Number: 4, Target: "x/y", Kind: core.JobKindIssue, Status: "paused"},
	}})
	got := m.Status(t.Context(), 0, []string{"o/r"})["target_jobs"].(map[string]TargetJobCounts)
	if got["o/r"] != (TargetJobCounts{Running: 1, Waiting: 1}) || got["x/y"] != (TargetJobCounts{Waiting: 1}) {
		t.Errorf("target_jobs = %v", got)
	}
}

func TestGetJobCapsRawOutputToRequestedLines(t *testing.T) {
	m := newTestManager(t)
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{{Number: 1, Target: "o/r", Kind: core.JobKindIssue, Status: "active"}}})
	recorder := m.transcripts.For("o/r#1").Recorder(nil)
	recorder.Write([]byte(strings.Repeat("line\n", 500) + "last\n"))
	recorder.Flush()
	capped, err := m.GetJob("o/r", 1, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if capped["raw_output"] != "line\nlast" {
		t.Errorf("raw_output = %q, want the last two lines", capped["raw_output"])
	}
	full, _ := m.GetJob("o/r", 1, -1, 0)
	if raw := full["raw_output"].(string); strings.Count(raw, "\n") < 500 {
		t.Errorf("uncapped raw_output has %d lines, want all of them", strings.Count(raw, "\n"))
	}
}

// BenchmarkBoardLoad measures the server side of a board's first load from
// the seeded store: its status and its job and schedule lists, serialized.
func BenchmarkBoardLoad(b *testing.B) {
	m := newTestManager(b)
	seedBoard(b, m)
	for b.Loop() {
		size := 0
		for _, v := range []any{
			m.Status(b.Context(), 0, []string{"o/r"}),
			m.ListJobs([]string{"o/r"}, "", core.BoardStates, nil, "", 0, boardListLimit),
			m.ListJobs([]string{"o/r"}, "", nil, []string{core.JobKindSchedule}, "", 0, boardListLimit),
		} {
			data, _ := json.Marshal(v)
			size += len(data)
		}
		b.ReportMetric(float64(size), "bytes")
	}
}
