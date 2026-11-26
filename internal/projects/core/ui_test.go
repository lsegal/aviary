package core

import "testing"

func TestQueued(t *testing.T) {
	for kind, want := range map[string]bool{
		JobKindMerge:      true,
		JobKindDiscuss:    true,
		JobKindIssue:      false,
		JobKindDiscussion: false,
		JobKindSchedule:   false,
		"":                false,
	} {
		if got := Queued(kind); got != want {
			t.Errorf("Queued(%q) = %v, want %v", kind, got, want)
		}
	}
}

func TestJobSnapshotBoardState(t *testing.T) {
	tests := []struct {
		name string
		job  JobSnapshot
		want string
	}{
		{name: "queued", job: JobSnapshot{Status: "queued", Kind: JobKindIssue}, want: BoardStarting},
		{name: "active issue without clone", job: JobSnapshot{Status: "active", Kind: JobKindIssue}, want: BoardStarting},
		{name: "active issue with clone", job: JobSnapshot{Status: "active", Kind: JobKindIssue, CheckoutDirectory: "/tmp/clone"}, want: BoardRunning},
		{name: "active discussion without clone", job: JobSnapshot{Status: "active", Kind: JobKindDiscussion}, want: BoardRunning},
		{name: "active schedule without clone", job: JobSnapshot{Status: "active", Kind: JobKindSchedule}, want: BoardRunning},
		{name: "waiting", job: JobSnapshot{Status: "waiting", Kind: JobKindIssue}, want: BoardPaused},
		{name: "paused", job: JobSnapshot{Status: "paused", Kind: JobKindIssue}, want: BoardPaused},
		{name: "complete", job: JobSnapshot{Status: "complete", Kind: JobKindIssue}, want: BoardDone},
		{name: "failed", job: JobSnapshot{Status: "failed", Kind: JobKindIssue}, want: BoardFailed},
		{name: "stopping", job: JobSnapshot{Status: "stopping", Kind: JobKindIssue}, want: BoardRunning},
		{name: "unknown", job: JobSnapshot{Status: "mystery"}, want: BoardRunning},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.job.BoardState(); got != tt.want {
				t.Fatalf("BoardState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJobSnapshotIssueURL(t *testing.T) {
	tests := []struct {
		name string
		job  JobSnapshot
		want string
	}{
		{name: "issue", job: JobSnapshot{Repository: "o/r", Number: 7, Kind: JobKindIssue}, want: "https://github.com/o/r/issues/7"},
		{name: "repository target", job: JobSnapshot{Target: "o/r", Number: 7}, want: "https://github.com/o/r/issues/7"},
		{name: "merge", job: JobSnapshot{Repository: "o/r", Number: 8, Kind: JobKindMerge}, want: "https://github.com/o/r/pull/8"},
		{name: "discuss", job: JobSnapshot{Repository: "o/r", Number: 9, Kind: JobKindDiscuss}, want: "https://github.com/o/r/discussions/9"},
		{name: "schedule", job: JobSnapshot{Repository: "o/r", Number: 1700000000, Kind: JobKindSchedule}, want: ""},
		{name: "no number", job: JobSnapshot{Repository: "o/r", Kind: JobKindIssue}, want: ""},
		{name: "project target", job: JobSnapshot{Target: "my-project", Number: 7, Kind: JobKindIssue}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.job.IssueURL(); got != tt.want {
				t.Fatalf("IssueURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestJobActionAvailability pins the contract the web dashboard's
// jobActionAvailability mirrors.
func TestJobActionAvailability(t *testing.T) {
	tests := []struct {
		status string
		want   JobActions
	}{
		{status: "queued", want: JobActions{RetryReason: "Already queued for a gh-fix run", Pause: true}},
		{status: "stopping", want: JobActions{RetryReason: "Waiting for the stopped run to exit"}},
		{status: "active", want: JobActions{Retry: true, Stop: true, Pause: true}},
		{status: "waiting", want: JobActions{Retry: true, Stop: true, Pause: true}},
		{status: "paused", want: JobActions{Retry: true, Stop: true}},
		{status: "complete", want: JobActions{Retry: true}},
		{status: "failed", want: JobActions{Retry: true}},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := JobActionAvailability(tt.status); got != tt.want {
				t.Fatalf("JobActionAvailability(%q) = %+v, want %+v", tt.status, got, tt.want)
			}
		})
	}
}

func TestJobActionsFor(t *testing.T) {
	const discussionReason = "The next poll answers the discussion again while it has no reply"
	const scheduleReason = "The scheduled run has not finished"
	tests := []struct {
		name string
		job  JobSnapshot
		want JobActions
	}{
		{name: "discussion active", job: JobSnapshot{Kind: JobKindDiscussion, Status: "active"}, want: JobActions{Stop: true, RetryReason: discussionReason}},
		{name: "discussion queued", job: JobSnapshot{Kind: JobKindDiscussion, Status: "queued"}, want: JobActions{Stop: true, RetryReason: discussionReason}},
		{name: "discussion complete", job: JobSnapshot{Kind: JobKindDiscussion, Status: "complete"}, want: JobActions{RetryReason: discussionReason}},
		{name: "merge queued", job: JobSnapshot{Kind: JobKindMerge, Status: "queued"}, want: JobActions{Pause: true, Stop: true, RetryReason: "Already queued"}},
		{name: "discuss queued", job: JobSnapshot{Kind: JobKindDiscuss, Status: "queued"}, want: JobActions{Pause: true, Stop: true, RetryReason: "Already queued"}},
		{name: "schedule active", job: JobSnapshot{Kind: JobKindSchedule, Status: "active"}, want: JobActions{Stop: true, RetryReason: scheduleReason}},
		{name: "schedule waiting", job: JobSnapshot{Kind: JobKindSchedule, Status: "waiting"}, want: JobActions{Stop: true, RetryReason: scheduleReason}},
		{name: "schedule stopping", job: JobSnapshot{Kind: JobKindSchedule, Status: "stopping"}, want: JobActions{RetryReason: scheduleReason}},
		{name: "schedule complete", job: JobSnapshot{Kind: JobKindSchedule, Status: "complete"}, want: JobActions{Retry: true}},
		{name: "schedule failed", job: JobSnapshot{Kind: JobKindSchedule, Status: "failed"}, want: JobActions{Retry: true}},
		{name: "issue active", job: JobSnapshot{Kind: JobKindIssue, Status: "active"}, want: JobActionAvailability("active")},
		{name: "issue queued", job: JobSnapshot{Kind: JobKindIssue, Status: "queued"}, want: JobActionAvailability("queued")},
		{name: "unset kind paused", job: JobSnapshot{Status: "paused"}, want: JobActionAvailability("paused")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := JobActionsFor(tt.job); got != tt.want {
				t.Fatalf("JobActionsFor(%+v) = %+v, want %+v", tt.job, got, tt.want)
			}
		})
	}
}

func TestQueuedJobActions(t *testing.T) {
	tests := []struct {
		status string
		want   JobActions
	}{
		{status: "queued", want: JobActions{Pause: true, Stop: true, RetryReason: "Already queued"}},
		{status: "active", want: JobActions{Pause: true, Stop: true, RetryReason: "Already running"}},
		{status: "waiting", want: JobActions{Pause: true, Stop: true, RetryReason: "Already running"}},
		{status: "stopping", want: JobActions{RetryReason: "Waiting for the stopped run to exit"}},
		{status: "paused", want: JobActions{Retry: true, Stop: true}},
		{status: "complete", want: JobActions{Retry: true}},
		{status: "failed", want: JobActions{Retry: true}},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := queuedJobActions(tt.status); got != tt.want {
				t.Fatalf("queuedJobActions(%q) = %+v, want %+v", tt.status, got, tt.want)
			}
		})
	}
}
