package projects

import (
	"slices"
	"testing"

	"github.com/lsegal/aviary/internal/projects/core"
)

func TestListJobsSearchesEveryField(t *testing.T) {
	m := newTestManager(t)
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{
		{Number: 351, Target: "o/r", Title: "Fix parser", Status: "active", Kind: core.JobKindIssue, Agent: "claude"},
		{Number: 12, Target: "o/r", Title: "Docs", Status: "complete", Kind: core.JobKindIssue, PullRequest: 351},
		{Number: 13, Target: "o/r", Title: "Other", Status: "complete", Kind: core.JobKindIssue, SessionID: "7f3ca91b"},
		{Number: 14, Target: "o/r", Title: "Logs", Status: "failed", Kind: core.JobKindDiscussion, Log: "panic: Widget exploded"},
		{Number: 15, Target: "x/y", Title: "Elsewhere", Status: "active", Kind: core.JobKindIssue, Log: "351"},
	}})
	numbers := func(list map[string]any) []int {
		var got []int
		for _, job := range list["jobs"].([]map[string]any) {
			got = append(got, job["number"].(int))
		}
		return got
	}
	for _, tc := range []struct {
		query  string
		status string
		want   []int
	}{
		{"351", "", []int{351, 12}},
		{"PARSER", "", []int{351}},
		{"7F3C", "", []int{13}},
		{"widget", "", []int{14}},
		{"gh-discuss", "", []int{14}},
		{"351", "complete", []int{12}},
		{"zzzzz", "", nil},
	} {
		list := m.ListJobs([]string{"o/r"}, tc.status, tc.query, 0, 0)
		if got := numbers(list); !slices.Equal(got, tc.want) {
			t.Errorf("query %q status %q = %v, want %v", tc.query, tc.status, got, tc.want)
		}
		if list["total"] != len(tc.want) {
			t.Errorf("query %q total = %v, want %d", tc.query, list["total"], len(tc.want))
		}
	}
	if got := m.ListJobs([]string{"o/r"}, "", "351", 0, 0)["unfiltered_total"]; got != 4 {
		t.Errorf("unfiltered_total = %v, want 4", got)
	}
	if _, ok := m.ListJobs([]string{"o/r"}, "", "", 0, 0)["unfiltered_total"]; ok {
		t.Error("unfiltered_total set without a query")
	}
}

func TestListJobsCoversSeveralProjects(t *testing.T) {
	m := newTestManager(t)
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{
		{Number: 1, Target: "o/r", Status: "active"},
		{Number: 2, Target: "x/y", Status: "active"},
		{Number: 3, Target: "z/z", Status: "active"},
	}})
	count := func(targets []string) any { return m.ListJobs(targets, "", "", 0, 0)["total"] }
	if got := count([]string{"o/r", "x/y"}); got != 2 {
		t.Errorf("two projects total = %v, want 2", got)
	}
	if got := count(nil); got != 3 {
		t.Errorf("no projects total = %v, want every job (3)", got)
	}
}
