package projects

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

func seedBoard(b testing.TB, m *Manager) {
	now := time.Now()
	log := strings.Repeat("some scrollback output line\n", 1100)
	var jobs []core.JobSnapshot
	n := 1
	for s := range 5 {
		for range 300 {
			jobs = append(jobs, core.JobSnapshot{Number: n, Target: "o/r", Kind: core.JobKindSchedule, ScheduleID: fmt.Sprintf("task-%d", s), Status: "complete", Title: "sched", Started: now.Add(-time.Duration(n) * time.Minute), Finished: now.Add(-time.Duration(n)*time.Minute + time.Second), Summary: "ok", Log: log})
			n++
		}
	}
	for range 500 {
		jobs = append(jobs, core.JobSnapshot{Number: n, Target: "o/r", Kind: core.JobKindIssue, Status: "complete", Title: "issue", PullRequest: n + 10000, PullRequestState: "MERGED", Started: now, Finished: now, Log: log})
		n++
	}
	m.snapshot.Store(&core.Snapshot{Targets: []string{"o/r"}, LastPoll: now, Jobs: jobs})
}

func BenchmarkBoardStatus(b *testing.B) {
	m := newTestManager(b)
	seedBoard(b, m)
	for b.Loop() {
		json.Marshal(m.Status(b.Context(), 0, []string{"o/r"}))
	}
}

func BenchmarkBoardList1000(b *testing.B) {
	m := newTestManager(b)
	seedBoard(b, m)
	for b.Loop() {
		out, _ := json.Marshal(m.ListJobs(nil, "", nil, nil, "", 0, 1000))
		b.ReportMetric(float64(len(out)), "bytes")
	}
}

func BenchmarkBoardDisplay(b *testing.B) {
	m := newTestManager(b)
	seedBoard(b, m)
	for b.Loop() {
		m.display()
	}
}

func BenchmarkBoardGetJob(b *testing.B) {
	m := newTestManager(b)
	seedBoard(b, m)
	for b.Loop() {
		json.Marshal(must(m.GetJob("o/r", 1700, 12, 40)))
	}
}

func must(v map[string]any, _ error) map[string]any { return v }
