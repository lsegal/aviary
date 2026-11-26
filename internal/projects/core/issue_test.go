package core

import (
	"slices"
	"testing"
)

func TestPullRequestWorkStateRepoOr(t *testing.T) {
	tests := []struct {
		name       string
		repository string
		fallback   string
		want       string
	}{
		{name: "unrecorded falls back", repository: "", fallback: "owner/repo", want: "owner/repo"},
		{name: "unrecorded with empty fallback", repository: "", fallback: "", want: ""},
		{name: "recorded wins", repository: "fork/repo", fallback: "owner/repo", want: "fork/repo"},
		{name: "recorded with empty fallback", repository: "fork/repo", fallback: "", want: "fork/repo"},
		{name: "recorded keeps its case", repository: "Owner/Repo", fallback: "owner/repo", want: "Owner/Repo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := PullRequestWorkState{Repository: tt.repository, Number: 1}
			if got := p.RepoOr(tt.fallback); got != tt.want {
				t.Fatalf("RepoOr(%q) = %q, want %q", tt.fallback, got, tt.want)
			}
		})
	}
}

func TestWorkProgressFormat(t *testing.T) {
	if got, want := WorkProgress(OriginatingWorkState{IssueState: "OPEN", IssueComments: 2}), "OPEN comments=2"; got != want {
		t.Fatalf("WorkProgress without pull requests = %q, want %q", got, want)
	}
	state := OriginatingWorkState{
		IssueState:    "OPEN",
		IssueComments: 1,
		PullRequests: []PullRequestWorkState{{
			Repository: "owner/repo", Number: 7, State: "OPEN", IsDraft: true,
			HeadSHA: "abc", Comments: 3, TasksDone: 1, TasksTotal: 4,
		}},
	}
	want := "OPEN comments=1; owner/repo#7 OPEN merged=false draft=true head=abc comments=3 tasks=1/4"
	if got := WorkProgress(state); got != want {
		t.Fatalf("WorkProgress = %q, want %q", got, want)
	}
}

func TestWorkProgressIgnoresPullRequestOrder(t *testing.T) {
	pullRequests := []PullRequestWorkState{
		{Repository: "b/repo", Number: 1, State: "OPEN", HeadSHA: "b1"},
		{Repository: "a/repo", Number: 9, State: "OPEN", HeadSHA: "a9"},
		{Repository: "a/repo", Number: 2, State: "CLOSED", Merged: true, HeadSHA: "a2"},
		{Repository: "", Number: 5, State: "OPEN", HeadSHA: "x5"},
	}
	original := slices.Clone(pullRequests)
	forward := WorkProgress(OriginatingWorkState{IssueState: "OPEN", PullRequests: pullRequests})
	reversed := slices.Clone(pullRequests)
	slices.Reverse(reversed)
	if got := WorkProgress(OriginatingWorkState{IssueState: "OPEN", PullRequests: reversed}); got != forward {
		t.Fatalf("WorkProgress depends on order:\n%q\n%q", forward, got)
	}
	want := "OPEN comments=0" +
		"; #5 OPEN merged=false draft=false head=x5 comments=0 tasks=0/0" +
		"; a/repo#2 CLOSED merged=true draft=false head=a2 comments=0 tasks=0/0" +
		"; a/repo#9 OPEN merged=false draft=false head=a9 comments=0 tasks=0/0" +
		"; b/repo#1 OPEN merged=false draft=false head=b1 comments=0 tasks=0/0"
	if forward != want {
		t.Fatalf("WorkProgress = %q, want sorted by repository then number %q", forward, want)
	}
	if !slices.Equal(pullRequests, original) {
		t.Fatalf("WorkProgress reordered its input: %+v", pullRequests)
	}
}

func TestWorkProgressChangesWithEachField(t *testing.T) {
	base := func() OriginatingWorkState {
		return OriginatingWorkState{
			IssueState:    "OPEN",
			IssueComments: 1,
			PullRequests: []PullRequestWorkState{{
				Repository: "owner/repo", Number: 7, State: "OPEN", IsDraft: true,
				HeadSHA: "abc", Comments: 2, TasksDone: 1, TasksTotal: 3,
			}},
		}
	}
	baseline := WorkProgress(base())
	tests := []struct {
		name   string
		mutate func(*OriginatingWorkState)
	}{
		{name: "issue state", mutate: func(s *OriginatingWorkState) { s.IssueState = "CLOSED" }},
		{name: "issue comments", mutate: func(s *OriginatingWorkState) { s.IssueComments++ }},
		{name: "pull request opened", mutate: func(s *OriginatingWorkState) {
			s.PullRequests = append(s.PullRequests, PullRequestWorkState{Repository: "owner/repo", Number: 8, State: "OPEN"})
		}},
		{name: "pull request repository", mutate: func(s *OriginatingWorkState) { s.PullRequests[0].Repository = "fork/repo" }},
		{name: "pull request number", mutate: func(s *OriginatingWorkState) { s.PullRequests[0].Number = 8 }},
		{name: "pull request state", mutate: func(s *OriginatingWorkState) { s.PullRequests[0].State = "CLOSED" }},
		{name: "merged", mutate: func(s *OriginatingWorkState) { s.PullRequests[0].Merged = true }},
		{name: "draft", mutate: func(s *OriginatingWorkState) { s.PullRequests[0].IsDraft = false }},
		{name: "head sha", mutate: func(s *OriginatingWorkState) { s.PullRequests[0].HeadSHA = "def" }},
		{name: "pull request comments", mutate: func(s *OriginatingWorkState) { s.PullRequests[0].Comments++ }},
		{name: "tasks done", mutate: func(s *OriginatingWorkState) { s.PullRequests[0].TasksDone++ }},
		{name: "tasks total", mutate: func(s *OriginatingWorkState) { s.PullRequests[0].TasksTotal++ }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := base()
			tt.mutate(&state)
			if got := WorkProgress(state); got == baseline {
				t.Fatalf("WorkProgress unchanged after changing %s: %q", tt.name, got)
			}
		})
	}
}

func TestWorkProgressIgnoresUntrackedFields(t *testing.T) {
	state := OriginatingWorkState{
		IssueState:   "OPEN",
		PullRequests: []PullRequestWorkState{{Repository: "owner/repo", Number: 7, State: "OPEN"}},
	}
	baseline := WorkProgress(state)
	state.IssueBody = "edited"
	state.PullRequests[0].Additions = 10
	state.PullRequests[0].Deletions = 4
	state.PullRequests[0].Stacked = true
	state.PullRequests[0].MergeCommitSHA = "fff"
	if got := WorkProgress(state); got != baseline {
		t.Fatalf("WorkProgress changed for untracked fields: %q, want %q", got, baseline)
	}
}
