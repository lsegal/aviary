package core

import "testing"

func TestCurrentPullRequest(t *testing.T) {
	tests := []struct {
		name  string
		state OriginatingWorkState
		want  int
	}{
		{name: "none", state: OriginatingWorkState{}, want: 0},
		{name: "open", state: OriginatingWorkState{PullRequests: []PullRequestWorkState{{Number: 9, State: "OPEN", IsDraft: true}}}, want: 9},
		{name: "newer open supersedes older", state: OriginatingWorkState{PullRequests: []PullRequestWorkState{{Number: 11, State: "OPEN"}, {Number: 9, State: "OPEN"}}}, want: 11},
		{name: "open beats merged", state: OriginatingWorkState{PullRequests: []PullRequestWorkState{{Number: 12, State: "CLOSED", Merged: true}, {Number: 9, State: "OPEN"}}}, want: 9},
		{name: "merged", state: OriginatingWorkState{PullRequests: []PullRequestWorkState{{Number: 9, State: "CLOSED", Merged: true}}}, want: 9},
		{name: "rejected is never current", state: OriginatingWorkState{PullRequests: []PullRequestWorkState{{Number: 12, State: "CLOSED"}, {Number: 9, State: "OPEN"}}}, want: 9},
		{name: "only rejected", state: OriginatingWorkState{PullRequests: []PullRequestWorkState{{Number: 9, State: "CLOSED"}}}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := CurrentPullRequest(tt.state)
			if ok != (tt.want != 0) || got.Number != tt.want {
				t.Fatalf("CurrentPullRequest() = #%d, %v; want #%d", got.Number, ok, tt.want)
			}
		})
	}
}

func TestJobSnapshotPullRequestLabel(t *testing.T) {
	var job JobSnapshot
	if label := job.PullRequestLabel(); label != "" {
		t.Fatalf("job without a pull request label = %q, want empty", label)
	}
	if !job.SetPullRequest(OriginatingWorkState{PullRequests: []PullRequestWorkState{{Repository: "o/r", Number: 750, State: "OPEN"}}}, "O/R") {
		t.Fatal("setting the first pull request reported no change")
	}
	if label := job.PullRequestLabel(); label != "PR #750" {
		t.Fatalf("same-repository label = %q, want %q", label, "PR #750")
	}
	if job.SetPullRequest(OriginatingWorkState{PullRequests: []PullRequestWorkState{{Number: 750, State: "OPEN"}}}, "o/r") {
		t.Fatal("re-reading the same pull request reported a change")
	}
	job.SetPullRequest(OriginatingWorkState{PullRequests: []PullRequestWorkState{{Repository: "fork/r", Number: 12, State: "OPEN"}}}, "o/r")
	if label := job.PullRequestLabel(); label != "PR fork/r#12" {
		t.Fatalf("cross-repository label = %q, want %q", label, "PR fork/r#12")
	}
	job.SetPullRequest(OriginatingWorkState{}, "o/r")
	if label := job.PullRequestLabel(); label != "" {
		t.Fatalf("label after the pull request went away = %q, want empty", label)
	}
}

func TestJobSnapshotURLs(t *testing.T) {
	job := JobSnapshot{Number: 24, Target: "lsegal/nupp", PullRequest: 190}
	if got, want := job.IssueURL(), "https://github.com/lsegal/nupp/issues/24"; got != want {
		t.Fatalf("IssueURL() = %q, want %q", got, want)
	}
	if got, want := job.PullRequestURL(), "https://github.com/lsegal/nupp/pull/190"; got != want {
		t.Fatalf("PullRequestURL() = %q, want %q", got, want)
	}
	job.PullRequestRepository = "fork/nupp"
	if got, want := job.PullRequestURL(), "https://github.com/fork/nupp/pull/190"; got != want {
		t.Fatalf("cross-repository PullRequestURL() = %q, want %q", got, want)
	}
	project := JobSnapshot{Number: 5, Target: "https://github.com/users/o/projects/1", Repository: "o/r"}
	if got, want := project.IssueURL(), "https://github.com/o/r/issues/5"; got != want {
		t.Fatalf("project IssueURL() = %q, want %q", got, want)
	}
	if got := project.PullRequestURL(); got != "" {
		t.Fatalf("PullRequestURL() without a pull request = %q, want empty", got)
	}
	unknown := JobSnapshot{Number: 5, Target: "https://github.com/users/o/projects/1", PullRequest: 6}
	if unknown.IssueURL() != "" || unknown.PullRequestURL() != "" {
		t.Fatalf("job with an unknown repository linked to %q and %q", unknown.IssueURL(), unknown.PullRequestURL())
	}
}
