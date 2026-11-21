package projects

import (
	"reflect"
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
		list := m.ListJobs([]string{"o/r"}, tc.status, "", tc.query, 0, 0)
		if got := numbers(list); !slices.Equal(got, tc.want) {
			t.Errorf("query %q status %q = %v, want %v", tc.query, tc.status, got, tc.want)
		}
		if list["total"] != len(tc.want) {
			t.Errorf("query %q total = %v, want %d", tc.query, list["total"], len(tc.want))
		}
	}
	if got := m.ListJobs([]string{"o/r"}, "", "", "351", 0, 0)["unfiltered_total"]; got != 4 {
		t.Errorf("unfiltered_total = %v, want 4", got)
	}
	if _, ok := m.ListJobs([]string{"o/r"}, "", "", "", 0, 0)["unfiltered_total"]; ok {
		t.Error("unfiltered_total set without a query")
	}
}

func TestListJobsFiltersByBoardState(t *testing.T) {
	m := newTestManager(t)
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{
		{Number: 1, Target: "o/r", Status: "queued", Kind: core.JobKindIssue},
		{Number: 2, Target: "o/r", Status: "active", Kind: core.JobKindIssue},
		{Number: 3, Target: "o/r", Status: "active", Kind: core.JobKindIssue, CheckoutDirectory: "/tmp/r-3"},
		{Number: 4, Target: "o/r", Status: "active", Kind: core.JobKindDiscussion},
		{Number: 5, Target: "o/r", Status: "stopping", Kind: core.JobKindIssue},
		{Number: 6, Target: "o/r", Status: "waiting", Kind: core.JobKindIssue, WaitReason: "waiting for CI"},
		{Number: 7, Target: "o/r", Status: "paused", Kind: core.JobKindIssue, WaitReason: "blocked by #2"},
		{Number: 8, Target: "o/r", Status: "complete", Kind: core.JobKindIssue},
		{Number: 9, Target: "o/r", Status: "failed", Kind: core.JobKindIssue},
	}})
	want := map[string][]int{
		core.BoardStarting: {1, 2},
		core.BoardRunning:  {3, 4, 5},
		core.BoardPaused:   {6, 7},
		core.BoardDone:     {8},
		core.BoardFailed:   {9},
	}
	seen := 0
	for _, state := range core.BoardStates {
		var got []int
		for _, job := range m.ListJobs([]string{"o/r"}, "", state, "", 0, 0)["jobs"].([]map[string]any) {
			got = append(got, job["number"].(int))
			if job["state"] != state {
				t.Errorf("job #%v state = %v, want %s", job["number"], job["state"], state)
			}
		}
		if !slices.Equal(got, want[state]) {
			t.Errorf("state %s = %v, want %v", state, got, want[state])
		}
		seen += len(got)
	}
	if seen != 9 {
		t.Errorf("the five states hold %d jobs, want every one of 9", seen)
	}
}

func TestStatusCountsBoardStatesLikeTheFilters(t *testing.T) {
	m := newTestManager(t)
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{
		{Number: 1, Target: "o/r", Status: "queued", Kind: core.JobKindIssue},
		{Number: 2, Target: "o/r", Status: "active", Kind: core.JobKindIssue},
		{Number: 3, Target: "o/r", Status: "active", Kind: core.JobKindIssue, CheckoutDirectory: "/tmp/r-3"},
		{Number: 4, Target: "o/r", Status: "waiting", Kind: core.JobKindIssue, WaitReason: "waiting for CI"},
		{Number: 5, Target: "o/r", Status: "paused", Kind: core.JobKindIssue},
		{Number: 6, Target: "o/r", Status: "complete", Kind: core.JobKindIssue},
		{Number: 7, Target: "x/y", Status: "failed", Kind: core.JobKindIssue},
	}})
	for _, targets := range [][]string{nil, {"o/r"}, {"x/y"}, {"o/r", "x/y"}} {
		states := m.Status(t.Context(), 0, targets)["states"].(map[string]int)
		if len(states) != len(core.BoardStates) {
			t.Errorf("targets %v: states = %v, want one count per board state", targets, states)
		}
		for _, state := range core.BoardStates {
			if want := m.ListJobs(targets, "", state, "", 0, 0)["total"]; states[state] != want {
				t.Errorf("targets %v: %s = %d, the filter shows %v", targets, state, states[state], want)
			}
		}
	}
	want := map[string]int{core.BoardStarting: 2, core.BoardPaused: 2, core.BoardRunning: 1, core.BoardDone: 1, core.BoardFailed: 0}
	if got := m.Status(t.Context(), 0, []string{"o/r"})["states"]; !reflect.DeepEqual(got, want) {
		t.Errorf("o/r states = %v, want %v", got, want)
	}
}

func TestJobSummaryPullRequestState(t *testing.T) {
	for _, tc := range []struct {
		pr   core.PullRequestWorkState
		want string
	}{
		{core.PullRequestWorkState{Number: 5, State: "OPEN", IsDraft: true}, core.PullRequestDraft},
		{core.PullRequestWorkState{Number: 5, State: "OPEN"}, core.PullRequestOpen},
		{core.PullRequestWorkState{Number: 5, State: "MERGED", Merged: true}, core.PullRequestMerged},
	} {
		job := core.JobSnapshot{Number: 1, Target: "o/r", Status: "active", Kind: core.JobKindIssue}
		if !job.SetPullRequest(core.OriginatingWorkState{PullRequests: []core.PullRequestWorkState{tc.pr}}, "o/r") {
			t.Errorf("%+v: SetPullRequest reported no change", tc.pr)
		}
		if got := jobSummary(job)["pull_request_state"]; got != tc.want {
			t.Errorf("%+v: pull_request_state = %v, want %s", tc.pr, got, tc.want)
		}
	}
	job := core.JobSnapshot{Number: 1, Target: "o/r"}
	open := core.OriginatingWorkState{PullRequests: []core.PullRequestWorkState{{Number: 5, State: "OPEN", IsDraft: true}}}
	job.SetPullRequest(open, "o/r")
	open.PullRequests[0].IsDraft = false
	if !job.SetPullRequest(open, "o/r") || job.PullRequestState != core.PullRequestOpen {
		t.Errorf("a draft marked ready did not republish: state %q", job.PullRequestState)
	}
}

func TestListJobsCoversSeveralProjects(t *testing.T) {
	m := newTestManager(t)
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{
		{Number: 1, Target: "o/r", Status: "active"},
		{Number: 2, Target: "x/y", Status: "active"},
		{Number: 3, Target: "z/z", Status: "active"},
	}})
	count := func(targets []string) any { return m.ListJobs(targets, "", "", "", 0, 0)["total"] }
	if got := count([]string{"o/r", "x/y"}); got != 2 {
		t.Errorf("two projects total = %v, want 2", got)
	}
	if got := count(nil); got != 3 {
		t.Errorf("no projects total = %v, want every job (3)", got)
	}
}
