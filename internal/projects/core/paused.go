package core

import (
	"fmt"
	"strings"
)

// The kinds of paused work (issue #732). Paused work is out of the dispatch
// queue and holds no agent slot. It waits on one GitHub terminal event (a
// merge or a close) and goes back into the queue through the normal claim path
// when that event happens. Review activity never wakes it.
const (
	// PausedBlocked is an issue waiting for the open issues it depends on to
	// close.
	PausedBlocked = "blocked"
	// PausedStacked is a ready pull request stacked on its blocker's branch,
	// waiting for the blocker's pull request to merge or close (issue #659).
	PausedStacked = "stacked"
	// PausedPullRequest is a finished run whose ready pull request is held
	// for a human to merge (a DONOTMERGE directive). It waits for that pull
	// request to merge, which completes the work, or to close unmerged, which
	// restarts it fresh (issues #690, #729).
	PausedPullRequest = "pull-request"
	// PausedManual is work the owner paused from a dashboard (issue #771). No
	// GitHub event wakes it; it waits until the owner retries it. Only its
	// issue closing finishes it, so it does not linger once the work is gone
	// (issue #775).
	PausedManual = "manual"
)

// ManualPauseReason is what the dashboards say a job the owner paused waits
// on.
const ManualPauseReason = "paused by owner"

// PausedWork is the one record every kind of paused work shares: what kind it
// is, the issues it waits on, and the pull requests it left behind. The record
// only saves work; whether it wakes is always read back from GitHub.
type PausedWork struct {
	Kind string `json:"kind"`
	// WaitingOn lists the open issues the work is blocked by.
	WaitingOn []int `json:"waitingOn,omitempty"`
	// PullRequests lists the work's own open pull requests.
	PullRequests []int `json:"pullRequests,omitempty"`
}

// PausedWake is what a paused record's wake check decided.
type PausedWake int

const (
	// PausedStay leaves the work paused.
	PausedStay PausedWake = iota
	// PausedResume puts the work back in the queue to pick it back up.
	PausedResume
	// PausedRestart puts the work back in the queue to start over, because
	// its pull request was closed without merging.
	PausedRestart
	// PausedComplete ends the work: its pull request merged or its issue
	// closed.
	PausedComplete
)

// PausedCheck is the GitHub state a wake check reads. Issue is the issue as
// the poll listed it. Own is the state of the issue's own pull requests, and
// Blockers holds the state of each issue it waits on. A state that could not
// be read is left out, and a check never wakes on missing state.
type PausedCheck struct {
	Issue    Issue
	Own      *OriginatingWorkState
	Blockers map[int]OriginatingWorkState
}

// OpenDependencies returns the numbers of the issues issue depends on that
// are not closed.
func OpenDependencies(issue Issue) []int {
	var open []int
	for _, dependency := range issue.DependsOn {
		if !strings.EqualFold(dependency.State, "closed") {
			open = append(open, dependency.Number)
		}
	}
	return open
}

// OpenPullRequests returns the numbers of the open, unmerged pull requests in
// state.
func OpenPullRequests(state OriginatingWorkState) []int {
	var open []int
	for _, pullRequest := range state.PullRequests {
		if strings.EqualFold(pullRequest.State, "open") && !pullRequest.Merged {
			open = append(open, pullRequest.Number)
		}
	}
	return open
}

// WakePaused decides whether paused work wakes, and why. It only reads GitHub
// terminal state: issue and pull request open, closed, and merged. Reviews,
// review comments, and approvals are not part of the input, so they can never
// wake paused work. Every instance that reads the same GitHub state decides
// the same thing.
func WakePaused(paused PausedWork, check PausedCheck) (PausedWake, string) {
	// A closed issue finishes every kind of paused work, a manual pause
	// included, so a job the owner paused does not stay paused forever once
	// its issue is gone (issue #775).
	if strings.EqualFold(check.Issue.State, "closed") || (check.Own != nil && strings.EqualFold(check.Own.IssueState, "closed")) {
		return PausedComplete, fmt.Sprintf("issue #%d was closed", check.Issue.Number)
	}
	// Only the owner resumes a manual pause, by retrying the job, so nothing
	// else read from GitHub wakes or finishes it (issue #771).
	if paused.Kind == PausedManual {
		return PausedStay, ManualPauseReason
	}
	if check.Own != nil {
		for _, pullRequest := range check.Own.PullRequests {
			if pullRequest.Merged {
				return PausedComplete, fmt.Sprintf("pull request #%d merged", pullRequest.Number)
			}
		}
	}
	switch paused.Kind {
	case PausedBlocked:
		if blocked, reason := IssueBlocked(check.Issue); blocked {
			return PausedStay, reason
		}
		return PausedResume, "its blockers are closed"
	case PausedPullRequest:
		if check.Own == nil {
			return PausedStay, "its pull request state could not be read"
		}
		if number, reason := RejectedPullRequest(*check.Own); number > 0 {
			return PausedRestart, reason
		}
		if open := OpenPullRequests(*check.Own); len(open) > 0 {
			return PausedStay, fmt.Sprintf("its pull request %s waits to merge", formatNumbers(open))
		}
		// No pull request is left to merge, so waiting on one would never
		// end. The work falls back to its blockers instead (issue #762).
		if blocked, reason := IssueBlocked(check.Issue); blocked {
			return PausedStay, reason + "; it has no open pull request"
		}
		return PausedResume, "it has no open pull request and nothing blocks it"
	default:
		// Stacked, including records written before paused work had a kind.
		blocked, reason := IssueBlocked(check.Issue)
		if !blocked {
			return PausedResume, "its blocker is no longer open"
		}
		if check.Own != nil {
			if number, rejected := RejectedPullRequest(*check.Own); number > 0 {
				return PausedRestart, rejected
			}
		}
		for _, number := range OpenDependencies(check.Issue) {
			state, ok := check.Blockers[number]
			if ok && len(state.PullRequests) > 0 && len(OpenPullRequests(state)) == 0 {
				return PausedResume, fmt.Sprintf("its blocker #%d no longer has an open pull request", number)
			}
		}
		return PausedStay, reason + "; its stacked pull request waits for that to merge"
	}
}

// RejectedPullRequest reports the pull request closed without merging that
// rejects the work on an open issue: none of its pull requests is open or
// merged, and at least one was closed unmerged. A closed issue or a merged
// pull request means the work is done and is never restarted.
func RejectedPullRequest(state OriginatingWorkState) (int, string) {
	if strings.EqualFold(state.IssueState, "closed") {
		return 0, ""
	}
	rejected := 0
	for _, pullRequest := range state.PullRequests {
		if pullRequest.Merged || strings.EqualFold(pullRequest.State, "open") {
			return 0, ""
		}
		if strings.EqualFold(pullRequest.State, "closed") && pullRequest.Number > rejected {
			rejected = pullRequest.Number
		}
	}
	if rejected == 0 {
		return 0, ""
	}
	return rejected, fmt.Sprintf("pull request #%d was closed without merging", rejected)
}

func formatNumbers(numbers []int) string {
	refs := make([]string, len(numbers))
	for i, number := range numbers {
		refs[i] = fmt.Sprintf("#%d", number)
	}
	return strings.Join(refs, ", ")
}
