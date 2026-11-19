package core

import "testing"

func TestWakePaused(t *testing.T) {
	blockedIssue := Issue{Number: 7, State: "open", DependsOn: []IssueDependency{{Number: 12, State: "open"}}}
	unblockedIssue := Issue{Number: 7, State: "open", DependsOn: []IssueDependency{{Number: 12, State: "closed"}}}
	held := &OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 15, State: "open"}}}
	stacked := &OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 15, State: "open", Stacked: true}}}
	merged := &OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 15, State: "closed", Merged: true}}}
	rejected := &OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 15, State: "closed"}}}
	closedIssue := &OriginatingWorkState{IssueState: "closed", PullRequests: []PullRequestWorkState{{Number: 15, State: "closed"}}}
	blockerOpen := map[int]OriginatingWorkState{12: {IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 14, State: "open"}}}}
	blockerMerged := map[int]OriginatingWorkState{12: {IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 14, State: "closed", Merged: true}}}}
	blockerClosed := map[int]OriginatingWorkState{12: {IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 14, State: "closed"}}}}

	blocked := PausedWork{Kind: PausedBlocked, WaitingOn: []int{12}}
	stack := PausedWork{Kind: PausedStacked, WaitingOn: []int{12}, PullRequests: []int{15}}
	pullRequest := PausedWork{Kind: PausedPullRequest, PullRequests: []int{15}}
	for _, test := range []struct {
		name   string
		paused PausedWork
		check  PausedCheck
		want   PausedWake
	}{
		{"blocked waits on an open blocker", blocked, PausedCheck{Issue: blockedIssue}, PausedStay},
		{"blocked wakes when its blocker closes", blocked, PausedCheck{Issue: unblockedIssue}, PausedResume},
		{"blocked finishes when its issue closes", blocked, PausedCheck{Issue: Issue{Number: 7, State: "closed"}}, PausedComplete},
		{"stacked waits while its blocker pull request is open", stack, PausedCheck{Issue: blockedIssue, Own: stacked, Blockers: blockerOpen}, PausedStay},
		{"stacked wakes when its blocker issue closes", stack, PausedCheck{Issue: unblockedIssue, Own: stacked}, PausedResume},
		{"stacked wakes when its blocker pull request merges", stack, PausedCheck{Issue: blockedIssue, Own: stacked, Blockers: blockerMerged}, PausedResume},
		{"stacked wakes when its blocker pull request closes", stack, PausedCheck{Issue: blockedIssue, Own: stacked, Blockers: blockerClosed}, PausedResume},
		{"stacked restarts when its own pull request closes unmerged", stack, PausedCheck{Issue: blockedIssue, Own: rejected, Blockers: blockerOpen}, PausedRestart},
		{"stacked without a readable blocker stays", stack, PausedCheck{Issue: blockedIssue}, PausedStay},
		{"legacy record is stacked", PausedWork{}, PausedCheck{Issue: unblockedIssue}, PausedResume},
		{"held pull request waits while open", pullRequest, PausedCheck{Issue: Issue{Number: 7}, Own: held}, PausedStay},
		{"held pull request completes when it merges", pullRequest, PausedCheck{Issue: Issue{Number: 7}, Own: merged}, PausedComplete},
		{"held pull request completes when its issue closes", pullRequest, PausedCheck{Issue: Issue{Number: 7}, Own: closedIssue}, PausedComplete},
		{"held pull request restarts when closed unmerged", pullRequest, PausedCheck{Issue: Issue{Number: 7}, Own: rejected}, PausedRestart},
		{"held pull request never wakes on unreadable state", pullRequest, PausedCheck{Issue: Issue{Number: 7}}, PausedStay},
		{"manual stays when its pull request merges", PausedWork{Kind: PausedManual}, PausedCheck{Issue: Issue{Number: 7}, Own: merged}, PausedStay},
		{"manual stays when its blockers close", PausedWork{Kind: PausedManual}, PausedCheck{Issue: unblockedIssue, Own: rejected}, PausedStay},
		{"manual stays while its issue is open", PausedWork{Kind: PausedManual}, PausedCheck{Issue: Issue{Number: 7, State: "open"}, Own: held}, PausedStay},
		{"manual completes when its issue closes", PausedWork{Kind: PausedManual}, PausedCheck{Issue: Issue{Number: 7, State: "closed"}}, PausedComplete},
		{"manual completes when its own state shows its issue closed", PausedWork{Kind: PausedManual}, PausedCheck{Issue: Issue{Number: 7}, Own: closedIssue}, PausedComplete},
	} {
		got, reason := WakePaused(test.paused, test.check)
		if got != test.want {
			t.Errorf("%s: WakePaused() = %v (%q), want %v", test.name, got, reason, test.want)
		}
		if reason == "" {
			t.Errorf("%s: WakePaused() gave no reason", test.name)
		}
	}
}

// Review activity changes nothing a wake check reads: an approved pull request
// with changes requested and new comments is still open and unmerged, so it
// leaves every kind of paused work exactly where it was.
func TestWakePausedIgnoresReviewActivity(t *testing.T) {
	reviewed := &OriginatingWorkState{IssueState: "open", IssueBody: "edited", PullRequests: []PullRequestWorkState{{Number: 15, State: "open", Stacked: true}}}
	issue := Issue{Number: 7, State: "open", DependsOn: []IssueDependency{{Number: 12, State: "open"}}}
	blockers := map[int]OriginatingWorkState{12: {IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 14, State: "open"}}}}
	for _, kind := range []string{PausedBlocked, PausedStacked, PausedPullRequest} {
		if got, reason := WakePaused(PausedWork{Kind: kind}, PausedCheck{Issue: issue, Own: reviewed, Blockers: blockers}); got != PausedStay {
			t.Errorf("%s: WakePaused() = %v (%q), want it to stay paused", kind, got, reason)
		}
	}
}

// Every kind of paused work wakes once nothing blocks its issue and it has no
// pull request left to wait on: the nupp#5 shape, an open issue whose
// dependencies all closed, with no sub-issues and no pull requests (issue
// #762). A pull-request record that holds no pull request must not wait for
// one to merge forever.
func TestWakePausedResumesWhenEveryBlockerClosed(t *testing.T) {
	issue := Issue{Number: 5, State: "open", DependsOn: []IssueDependency{{Number: 2, State: "closed"}, {Number: 3, State: "closed"}, {Number: 4, State: "closed"}}}
	none := &OriginatingWorkState{IssueState: "open"}
	for _, paused := range []PausedWork{
		{Kind: PausedBlocked, WaitingOn: []int{2, 3, 4}},
		{Kind: PausedStacked},
		{Kind: PausedPullRequest},
		{Kind: PausedPullRequest, PullRequests: []int{15}},
		{},
	} {
		if got, reason := WakePaused(paused, PausedCheck{Issue: issue, Own: none}); got != PausedResume {
			t.Errorf("%+v: WakePaused() = %v (%q), want it resumed", paused, got, reason)
		}
	}
}

// A pull-request record with no open pull request falls back to its blockers,
// and the reason it stays names what keeps it paused (issue #762).
func TestWakePausedPullRequestWithoutOneNamesItsBlocker(t *testing.T) {
	none := &OriginatingWorkState{IssueState: "open"}
	paused := PausedWork{Kind: PausedPullRequest}
	for _, test := range []struct {
		issue Issue
		want  string
	}{
		{Issue{Number: 5, State: "open", DependsOn: []IssueDependency{{Number: 2, State: "open"}}}, "depends on #2 (open); it has no open pull request"},
		{Issue{Number: 5, State: "open", HasSubIssues: true}, "has sub-issues; it has no open pull request"},
	} {
		if got, reason := WakePaused(paused, PausedCheck{Issue: test.issue, Own: none}); got != PausedStay || reason != test.want {
			t.Errorf("%+v: WakePaused() = %v (%q), want it to stay paused with %q", test.issue, got, reason, test.want)
		}
	}
}
