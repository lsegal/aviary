package core

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// DefaultIssueFilter is the search filter a run uses when none is configured:
// the issues the authenticated user opened or was assigned.
const DefaultIssueFilter = "assignee:@me author:@me"

// Target is a parsed watch target: an OWNER/REPO repository, a Projects (v2)
// board, or a repository's Discussions.
type Target struct {
	Repo, Owner, ProjectID string
	ProjectOwnerType       string
	IsProject              bool
	IsDiscussion           bool
	// DiscussionCategory is the slug of the single Discussions category a
	// discussion target watches. Empty means every category.
	DiscussionCategory string
}

// ValidRepo reports whether repo is a bare OWNER/REPO pair.
func ValidRepo(repo string) bool {
	parts := strings.Split(repo, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != "" && !strings.ContainsAny(repo, " \t\r\n")
}

// ParseTarget reads a watch target written as OWNER/REPO or as a GitHub
// repository, project, or discussions URL.
func ParseTarget(value string) (Target, error) {
	if ValidRepo(value) {
		return Target{Repo: value}, nil
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.RawQuery != "" || u.Fragment != "" {
		return Target{}, fmt.Errorf("target must be OWNER/REPO or a GitHub repository/project URL")
	}
	parts := strings.Split(strings.Trim(path.Clean(u.Path), "/"), "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return Target{Repo: parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")}, nil
	}
	if len(parts) == 4 && (parts[0] == "users" || parts[0] == "orgs") && parts[2] == "projects" && parts[1] != "" && parts[3] != "" {
		return Target{Owner: parts[1], ProjectID: parts[3], ProjectOwnerType: parts[0], IsProject: true}, nil
	}
	if len(parts) == 4 && parts[2] == "projects" && parts[0] != "" && parts[1] != "" && parts[3] != "" {
		return Target{Repo: parts[0] + "/" + parts[1], Owner: parts[0], ProjectID: parts[3], IsProject: true}, nil
	}
	if len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] == "discussions" {
		return Target{Repo: parts[0] + "/" + parts[1], IsDiscussion: true}, nil
	}
	if len(parts) == 5 && parts[0] != "" && parts[1] != "" && parts[2] == "discussions" && parts[3] == "categories" && parts[4] != "" {
		return Target{Repo: parts[0] + "/" + parts[1], IsDiscussion: true, DiscussionCategory: parts[4]}, nil
	}
	return Target{}, fmt.Errorf("target must be OWNER/REPO or a GitHub repository/project URL")
}

// IsProjectTarget reports whether a target names a Projects (v2) board.
func IsProjectTarget(repo string) bool {
	target, err := ParseTarget(repo)
	return err == nil && target.IsProject
}

// IsDiscussionTarget reports whether a target names a repository's Discussions.
func IsDiscussionTarget(repo string) bool {
	target, err := ParseTarget(repo)
	return err == nil && target.IsDiscussion
}

// IssueFilterTerms splits a --filter into the terms a query is actually built
// from, dropping every qualifier that names the kind or the state of what to
// look for.
//
// glorp only ever dispatches an open issue: a closed one has nothing left to
// do and a pull request is not work it can pick up, so "is:issue" and the open
// state are appended to every query rather than being defaults a --filter can
// displace. They are therefore not part of the filter a user is shown or
// overrides, and a filter that names its own kind or state has that term
// dropped here instead of contradicting the qualifiers glorp adds.
//
// It is shared with ProjectItemQuery, which appends the same two qualifiers in
// a project board's own narrower vocabulary.
func IssueFilterTerms(filter string) []string {
	var terms []string
	for _, term := range strings.Fields(filter) {
		switch {
		case strings.HasPrefix(term, "state:"), strings.HasPrefix(term, "is:"), strings.HasPrefix(term, "type:"):
			continue
		}
		terms = append(terms, term)
	}
	return terms
}

// IssueSearchTerms builds the search terms for filter, the query body shared by
// the API path and the browser mode's page URL. "is:issue" and "state:open"
// always open the query; see IssueFilterTerms.
//
// allIssues drops the filter entirely, leaving the bare qualifiers.
func IssueSearchTerms(filter string, allIssues bool) []string {
	if allIssues {
		filter = ""
	}
	return append([]string{"is:issue", "state:open"}, IssueFilterTerms(filter)...)
}

// ProjectItemQuery builds the item filter a project board is asked for, both
// as the board page's ?filterQuery= and as the GraphQL/`gh project item-list`
// item query.
//
// The kind and state qualifiers always open the query, exactly as
// IssueSearchTerms does for the issues page, and a filter naming its own is
// stripped of it by IssueFilterTerms rather than contradicting them.
//
// A board speaks a narrower search vocabulary than the issues page: it knows
// "is:open"/"is:closed" but not "state:open", so the open state is named the
// board's own way here.
func ProjectItemQuery(filter string, allIssues bool) string {
	if allIssues || filter == DefaultIssueFilter {
		filter = ""
	}
	return strings.Join(append([]string{"is:issue", "is:open"}, IssueFilterTerms(filter)...), " ")
}
