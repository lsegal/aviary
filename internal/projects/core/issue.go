// Package core holds the types aviary's halves exchange: the issues and
// comments a source produces, the state a run reads back from GitHub, and the
// target syntax that names where either comes from. It is deliberately small
// and dependency free, so the browser driver in package browser and the root
// package can both speak them without importing each other.
package core

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Issue is a GitHub issue as aviary reads it, whichever source it came from.
type Issue struct {
	Number        int               `json:"number"`
	Repository    string            `json:"repository,omitempty"`
	Title         string            `json:"title,omitempty"`
	Body          string            `json:"body,omitempty"`
	State         string            `json:"state,omitempty"`
	CreatedAt     time.Time         `json:"createdAt,omitempty"`
	Labels        []IssueLabel      `json:"labels,omitempty"`
	DependsOn     []IssueDependency `json:"dependsOn,omitempty"`
	HasSubIssues  bool              `json:"hasSubIssues,omitempty"`
	ProjectStatus string            `json:"projectStatus,omitempty"`
	ProjectItemID string            `json:"-"`
	Target        string            `json:"-"`
	// Fork is the OWNER/REPO of the user's fork the fix is pushed to when the
	// user cannot write to Repository. Empty pushes to Repository.
	Fork string `json:"-"`
	// Prompt replaces the gh-fix invocation for a scheduled run, which has no
	// issue, and ScheduleID names the schedule it fired from.
	Prompt     string `json:"-"`
	ScheduleID string `json:"-"`
}

// IssueLabel is one label on an issue.
type IssueLabel struct {
	Name string `json:"name"`
}

// IssueDependency is an issue this one is blocked by.
type IssueDependency struct {
	Number int    `json:"number"`
	State  string `json:"state"`
}

// IssueSource lists the issues a watched target currently offers.
type IssueSource interface {
	ListIssues(context.Context, string) ([]Issue, error)
}

// IssueRepository resolves the OWNER/REPO an issue belongs to, falling back to
// the target it was listed from when the issue does not name one itself.
func IssueRepository(target string, issue Issue) string {
	if issue.Repository != "" {
		return issue.Repository
	}
	parsed, err := ParseTarget(target)
	if err == nil && parsed.Repo != "" {
		return parsed.Repo
	}
	return target
}

// PullRequestWorkState is one pull request linked to an issue an agent is
// working, and how far along it is.
type PullRequestWorkState struct {
	// Repository is the OWNER/REPO hosting the pull request. It differs from
	// the issue's repository when the fix was opened elsewhere, such as from
	// a fork, and is empty when unknown.
	Repository string
	Number     int
	State      string
	Merged     bool
	// MergedAt is when the pull request merged, zero while it has not.
	MergedAt time.Time
	// IsDraft reports whether the pull request is still a draft. A run that
	// finishes with an open, unmerged, non-draft pull request left behind has
	// deliberately marked its fix ready and is waiting on a human rather than
	// stalled.
	IsDraft bool
	// Stacked reports whether the pull request targets a branch other than
	// its repository's default branch, which is how a pull request stacked on
	// an open blocker's branch looks until that blocker merges. A ready one
	// is paused waiting on its base rather than held for a human.
	Stacked bool
	// TasksDone and TasksTotal count the checked and all task-list items in
	// the pull request's body, the run's plan checklist.
	TasksDone  int
	TasksTotal int
	// HeadSHA is the pull request's head commit, and Comments counts its
	// conversation and review comments, so a turn that pushed or commented
	// counts as progress.
	HeadSHA  string
	Comments int
}

// RepoOr returns the repository hosting the pull request, or fallback (the
// issue's repository) when it was not recorded.
func (p PullRequestWorkState) RepoOr(fallback string) string {
	if p.Repository == "" {
		return fallback
	}
	return p.Repository
}

// InRepo reports whether the pull request lives in repo, treating an
// unrecorded repository as the issue's own.
func (p PullRequestWorkState) InRepo(repo string) bool {
	return p.Repository == "" || strings.EqualFold(p.Repository, repo)
}

// CurrentPullRequest returns the pull request an issue's work is on now: the
// newest open one, or else the newest merged one. A pull request closed
// without merging was rejected or superseded, so it is never current, and a
// newer pull request replaces an older one.
func CurrentPullRequest(state OriginatingWorkState) (PullRequestWorkState, bool) {
	var current PullRequestWorkState
	found := false
	rank := func(pullRequest PullRequestWorkState) int {
		switch {
		case strings.EqualFold(pullRequest.State, "open") && !pullRequest.Merged:
			return 2
		case pullRequest.Merged:
			return 1
		}
		return 0
	}
	for _, pullRequest := range state.PullRequests {
		r := rank(pullRequest)
		if r == 0 || pullRequest.Number <= 0 {
			continue
		}
		if !found || r > rank(current) || (r == rank(current) && pullRequest.Number > current.Number) {
			current, found = pullRequest, true
		}
	}
	return current, found
}

// OriginatingWorkState is what a run reads back about the issue an agent is
// working, so a closure or an edit made while it runs can be relayed into the
// session.
type OriginatingWorkState struct {
	IssueState string
	// IssueBody is the issue's description, compared between checks so an
	// edit made while an agent is working is relayed into its session
	//.
	IssueBody string
	// IssueComments counts the comments on the issue.
	IssueComments int
	PullRequests  []PullRequestWorkState
}

// WorkProgress fingerprints what an agent's turn can visibly change on GitHub:
// the issue's comments, and each pull request's existence, state, head
// commit, comments, and plan checklist. Two equal fingerprints mean nothing
// new was committed, opened, or commented in between.
func WorkProgress(state OriginatingWorkState) string {
	pullRequests := slices.Clone(state.PullRequests)
	slices.SortFunc(pullRequests, func(a, b PullRequestWorkState) int {
		return cmp.Or(strings.Compare(a.Repository, b.Repository), cmp.Compare(a.Number, b.Number))
	})
	var b strings.Builder
	fmt.Fprintf(&b, "%s comments=%d", state.IssueState, state.IssueComments)
	for _, p := range pullRequests {
		fmt.Fprintf(&b, "; %s#%d %s merged=%t draft=%t head=%s comments=%d tasks=%d/%d", p.Repository, p.Number, p.State, p.Merged, p.IsDraft, p.HeadSHA, p.Comments, p.TasksDone, p.TasksTotal)
	}
	return b.String()
}

// WorkClosureChecker reports the current state of the issue behind active work.
type WorkClosureChecker interface {
	OriginatingWorkState(context.Context, string, int) (OriginatingWorkState, error)
}

// StackSource reports whether a repository has GitHub stacked pull requests
// enabled, so an issue blocked by an open issue can be built on the blocker's
// branch instead of waiting for it to close.
type StackSource interface {
	StacksEnabled(context.Context, string) (bool, error)
}

// ProjectStateSource returns a cheap fingerprint of a project board's
// dispatchable state. GitHub publishes no projects_v2 webhook for user-owned
// Projects, so board-only edits (dragging an issue onto the board, moving a
// card into the ready column) produce no delivery at all. Push mode probes
// this fingerprint on a short interval and only pays for a full poll when it
// actually changes, instead of waiting out the fallback interval.
type ProjectStateSource interface {
	ProjectState(context.Context, string) (string, error)
}

// ForkSource decides where the fix for a repository's issue is pushed (issue
// #712). It answers an empty string when the authenticated user can write to
// repo, and otherwise the OWNER/REPO of the user's fork of it, creating that
// fork when it does not exist yet.
type ForkSource interface {
	ForkFor(context.Context, string) (string, error)
}
