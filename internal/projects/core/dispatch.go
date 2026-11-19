package core

import (
	"fmt"
	"strings"
)

// The predicates here answer what a target and an issue say about whether the
// issue is work: they are shared with the browser driver, which reads the same
// project statuses off a rendered board that the API path reads from GraphQL.

// IssueBlocked reports whether an issue is not work yet, and why: it either
// has sub-issues of its own, which makes it a tracking issue, or it depends on
// an issue that is not closed.
func IssueBlocked(issue Issue) (bool, string) {
	if issue.HasSubIssues {
		return true, "has sub-issues"
	}
	blocked := make([]string, 0)
	for _, dependency := range issue.DependsOn {
		if !strings.EqualFold(dependency.State, "closed") {
			if dependency.State == "" {
				blocked = append(blocked, fmt.Sprintf("depends on #%d", dependency.Number))
			} else {
				blocked = append(blocked, fmt.Sprintf("depends on #%d (%s)", dependency.Number, strings.ToLower(dependency.State)))
			}
		}
	}
	if len(blocked) == 0 {
		return false, ""
	}
	return true, strings.Join(blocked, ", ")
}

// StackableDependency returns the one open dependency an issue could be
// stacked on instead of waiting for it to close (issue #657): with GitHub
// stacked pull requests, the dependent issue's pull request is built on the
// blocker's branch. A tracking issue with sub-issues is never stackable, and
// neither is an issue blocked by more than one open issue, because a stack is
// a single chain of branches.
func StackableDependency(issue Issue) (IssueDependency, bool) {
	if issue.HasSubIssues {
		return IssueDependency{}, false
	}
	var open []IssueDependency
	for _, dependency := range issue.DependsOn {
		if !strings.EqualFold(dependency.State, "closed") {
			open = append(open, dependency)
		}
	}
	if len(open) != 1 {
		return IssueDependency{}, false
	}
	return open[0], true
}

// ShouldDispatchIssue decides whether a repository or project issue that is
// not already active locally is a dispatch candidate. Remote ownership can
// no longer be read synchronously for repository issues (no label survives
// to check), so a never-seen issue is always a candidate, and one this
// instance has already seen is a candidate again only if it wasn't already
// completed; negotiateContestedIssues is what asks, via comments, whether
// another instance already owns a reappearing candidate.
func ShouldDispatchIssue(repo string, issue Issue, isActive, wasActive, wasCompleted, seen bool, readyState string) bool {
	if isActive {
		return false
	}
	if wasActive {
		return true
	}
	if IsProjectTarget(repo) {
		if ProjectStatusAllowsDispatch(issue.ProjectStatus, readyState) {
			return true
		}
		// An item parked at "In Progress" is claimed work: either this
		// instance's own reappearing item, or work stranded in that column by
		// an instance that died mid-run and left this one no local record to
		// recognize it by. Both are dispatch candidates;
		// negotiateContestedIssues is what asks, through the comment protocol,
		// whether another live instance still owns it before it is reclaimed.
		return ProjectItemInProgress(repo, issue)
	}
	if !seen {
		return true
	}
	return !wasCompleted
}

// ProjectItemInProgress reports whether issue is a project item currently
// parked in the "In Progress" column, which is how a glorp instance marks
// work it has claimed.
func ProjectItemInProgress(target string, issue Issue) bool {
	return IsProjectTarget(target) && strings.EqualFold(strings.TrimSpace(issue.ProjectStatus), "In Progress")
}

func RemoteIssueAllowsDispatch(target string, issue Issue, readyState string) bool {
	if !IsProjectTarget(target) {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(issue.ProjectStatus), "In Progress") ||
		ProjectStatusAllowsDispatch(issue.ProjectStatus, readyState)
}

func ProjectStatusAllowsDispatch(status, readyState string) bool {
	status = strings.TrimSpace(status)
	readyState = strings.TrimSpace(readyState)
	if readyState != "" {
		return strings.EqualFold(status, readyState)
	}
	return strings.EqualFold(status, "Todo") || strings.EqualFold(status, "Ready")
}

func ProjectReadyState(configured, current string) string {
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured
	}
	if current = strings.TrimSpace(current); ProjectStatusAllowsDispatch(current, "") {
		return current
	}
	return "Todo"
}
