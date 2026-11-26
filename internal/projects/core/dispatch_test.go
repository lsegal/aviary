package core

import "testing"

const (
	dispatchRepoTarget    = "o/r"
	dispatchProjectTarget = "https://github.com/orgs/acme/projects/5"
)

func TestIssueBlocked(t *testing.T) {
	tests := []struct {
		name        string
		issue       Issue
		wantBlocked bool
		wantReason  string
	}{
		{name: "no dependencies", issue: Issue{Number: 1, Repository: "o/r"}},
		{
			name:        "tracking issue",
			issue:       Issue{Number: 1, Repository: "o/r", HasSubIssues: true, DependsOn: []IssueDependency{{Number: 2, State: "open"}}},
			wantBlocked: true,
			wantReason:  "has sub-issues",
		},
		{
			name:  "closed dependencies in any case",
			issue: Issue{Number: 1, Repository: "o/r", DependsOn: []IssueDependency{{Number: 2, State: "closed"}, {Number: 3, State: "CLOSED"}}},
		},
		{
			name: "open, unknown, and cross-repository dependencies",
			issue: Issue{Number: 1, Repository: "o/r", DependsOn: []IssueDependency{
				{Number: 2, State: "OPEN"},
				{Number: 3, State: "closed"},
				{Number: 4},
				{Number: 5, State: "open", Repository: "other/lib"},
			}},
			wantBlocked: true,
			wantReason:  "depends on #2 (open), depends on #4, depends on other/lib#5 (open)",
		},
		{
			name:        "repository resolved from the target",
			issue:       Issue{Number: 1, Target: "https://github.com/o/r", DependsOn: []IssueDependency{{Number: 2, State: "open", Repository: "o/r"}}},
			wantBlocked: true,
			wantReason:  "depends on #2 (open)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked, reason := IssueBlocked(tt.issue)
			if blocked != tt.wantBlocked || reason != tt.wantReason {
				t.Errorf("IssueBlocked() = (%v, %q), want (%v, %q)", blocked, reason, tt.wantBlocked, tt.wantReason)
			}
		})
	}
}

func TestStackableDependency(t *testing.T) {
	tests := []struct {
		name       string
		issue      Issue
		wantNumber int
		wantOK     bool
	}{
		{name: "no dependencies", issue: Issue{Repository: "o/r"}},
		{
			name:   "tracking issue",
			issue:  Issue{Repository: "o/r", HasSubIssues: true, DependsOn: []IssueDependency{{Number: 2, State: "open"}}},
			wantOK: false,
		},
		{
			name:       "one open blocker among closed ones",
			issue:      Issue{Repository: "o/r", DependsOn: []IssueDependency{{Number: 2, State: "open"}, {Number: 3, State: "closed"}}},
			wantNumber: 2,
			wantOK:     true,
		},
		{
			name:  "two open blockers",
			issue: Issue{Repository: "o/r", DependsOn: []IssueDependency{{Number: 2, State: "open"}, {Number: 3, State: "open"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dependency, ok := StackableDependency(tt.issue)
			if ok != tt.wantOK || dependency.Number != tt.wantNumber {
				t.Errorf("StackableDependency() = (#%d, %v), want (#%d, %v)", dependency.Number, ok, tt.wantNumber, tt.wantOK)
			}
		})
	}
}

func TestShouldDispatchIssue(t *testing.T) {
	tests := []struct {
		name         string
		repo         string
		status       string
		isActive     bool
		wasActive    bool
		wasCompleted bool
		seen         bool
		readyState   string
		want         bool
	}{
		// Repository targets.
		{name: "repo: active locally", repo: dispatchRepoTarget, isActive: true, wasActive: true, want: false},
		{name: "repo: was active", repo: dispatchRepoTarget, wasActive: true, wasCompleted: true, seen: true, want: true},
		{name: "repo: never seen", repo: dispatchRepoTarget, wasCompleted: true, want: true},
		{name: "repo: seen, not completed", repo: dispatchRepoTarget, seen: true, want: true},
		{name: "repo: seen and completed", repo: dispatchRepoTarget, seen: true, wasCompleted: true, want: false},
		{name: "repo: status ignored", repo: dispatchRepoTarget, status: "Done", seen: true, want: true},

		// Project targets.
		{name: "project: active locally", repo: dispatchProjectTarget, status: "Todo", isActive: true, want: false},
		{name: "project: was active outside ready column", repo: dispatchProjectTarget, status: "Done", wasActive: true, want: true},
		{name: "project: default Todo", repo: dispatchProjectTarget, status: "Todo", want: true},
		{name: "project: default Ready, case and space", repo: dispatchProjectTarget, status: "  rEaDy ", want: true},
		{name: "project: Todo seen and completed", repo: dispatchProjectTarget, status: "Todo", seen: true, wasCompleted: true, want: true},
		{name: "project: Done not ready", repo: dispatchProjectTarget, status: "Done", want: false},
		{name: "project: never seen, not ready", repo: dispatchProjectTarget, status: "Backlog", want: false},
		{name: "project: empty status", repo: dispatchProjectTarget, want: false},
		{name: "project: configured ready state", repo: dispatchProjectTarget, status: "queued", readyState: " Queued ", want: true},
		{name: "project: configured ready state rejects Todo", repo: dispatchProjectTarget, status: "Todo", readyState: "Queued", want: false},
		{name: "project: In Progress reclaimable", repo: dispatchProjectTarget, status: " in progress ", seen: true, wasCompleted: true, want: true},
		{name: "project: In Progress with configured ready state", repo: dispatchProjectTarget, status: "In Progress", readyState: "Queued", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := Issue{Number: 1, Repository: "o/r", ProjectStatus: tt.status}
			got := ShouldDispatchIssue(tt.repo, issue, tt.isActive, tt.wasActive, tt.wasCompleted, tt.seen, tt.readyState)
			if got != tt.want {
				t.Errorf("ShouldDispatchIssue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProjectItemInProgress(t *testing.T) {
	tests := []struct {
		target string
		status string
		want   bool
	}{
		{target: dispatchProjectTarget, status: "In Progress", want: true},
		{target: dispatchProjectTarget, status: "  in PROGRESS\t", want: true},
		{target: dispatchProjectTarget, status: "InProgress", want: false},
		{target: dispatchProjectTarget, status: "Todo", want: false},
		{target: dispatchProjectTarget, status: "", want: false},
		{target: dispatchRepoTarget, status: "In Progress", want: false},
		{target: "not a target", status: "In Progress", want: false},
	}
	for _, tt := range tests {
		if got := ProjectItemInProgress(tt.target, Issue{ProjectStatus: tt.status}); got != tt.want {
			t.Errorf("ProjectItemInProgress(%q, %q) = %v, want %v", tt.target, tt.status, got, tt.want)
		}
	}
}

func TestRemoteIssueAllowsDispatch(t *testing.T) {
	tests := []struct {
		target     string
		status     string
		readyState string
		want       bool
	}{
		{target: dispatchRepoTarget, status: "", want: true},
		{target: dispatchRepoTarget, status: "Done", readyState: "Queued", want: true},
		{target: dispatchProjectTarget, status: "Todo", want: true},
		{target: dispatchProjectTarget, status: "ready", want: true},
		{target: dispatchProjectTarget, status: " In Progress ", want: true},
		{target: dispatchProjectTarget, status: "In Progress", readyState: "Queued", want: true},
		{target: dispatchProjectTarget, status: "QUEUED", readyState: "Queued", want: true},
		{target: dispatchProjectTarget, status: "Todo", readyState: "Queued", want: false},
		{target: dispatchProjectTarget, status: "Done", want: false},
		{target: dispatchProjectTarget, status: "", want: false},
	}
	for _, tt := range tests {
		if got := RemoteIssueAllowsDispatch(tt.target, Issue{ProjectStatus: tt.status}, tt.readyState); got != tt.want {
			t.Errorf("RemoteIssueAllowsDispatch(%q, %q, %q) = %v, want %v", tt.target, tt.status, tt.readyState, got, tt.want)
		}
	}
}

func TestProjectStatusAllowsDispatch(t *testing.T) {
	tests := []struct {
		status     string
		readyState string
		want       bool
	}{
		{status: "Todo", want: true},
		{status: "todo", want: true},
		{status: " Ready\n", want: true},
		{status: "READY", readyState: "  ", want: true},
		{status: "In Progress", want: false},
		{status: "Done", want: false},
		{status: "", want: false},
		{status: "Next up", readyState: "next UP", want: true},
		{status: " Next up ", readyState: "Next up", want: true},
		{status: "Todo", readyState: "Next up", want: false},
		{status: "Ready", readyState: "Next up", want: false},
		{status: "", readyState: "Next up", want: false},
	}
	for _, tt := range tests {
		if got := ProjectStatusAllowsDispatch(tt.status, tt.readyState); got != tt.want {
			t.Errorf("ProjectStatusAllowsDispatch(%q, %q) = %v, want %v", tt.status, tt.readyState, got, tt.want)
		}
	}
}

func TestProjectReadyState(t *testing.T) {
	tests := []struct {
		configured string
		current    string
		want       string
	}{
		{configured: "Queued", current: "Todo", want: "Queued"},
		{configured: "  Next up ", current: "Ready", want: "Next up"},
		{configured: "", current: "Ready", want: "Ready"},
		{configured: " ", current: " todo ", want: "todo"},
		{configured: "", current: "In Progress", want: "Todo"},
		{configured: "", current: "Done", want: "Todo"},
		{configured: "", current: "", want: "Todo"},
	}
	for _, tt := range tests {
		if got := ProjectReadyState(tt.configured, tt.current); got != tt.want {
			t.Errorf("ProjectReadyState(%q, %q) = %q, want %q", tt.configured, tt.current, got, tt.want)
		}
	}
}
