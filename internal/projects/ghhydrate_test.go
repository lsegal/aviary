package projects

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// The hydration these cover is what browser mode fills the fields a rendered
// page cannot show with, but it is the root package's own `gh` client that
// does it, so it is tested here rather than alongside the page readers.

// TestGHCLIHydrateIssueUsesTargetedRESTCalls pins the per-issue cost of a
// hydration on a private repository: one issue read plus the two dependency
// lookups, all REST, none of them GraphQL.
func TestGHCLIHydrateIssueUsesTargetedRESTCalls(t *testing.T) {
	var calls [][]string
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		publicAPI: func(context.Context, string) ([]byte, http.Header, int, error) {
			return []byte(`{"private":true}`), nil, http.StatusOK, nil
		},
		runCommand: func(_ context.Context, args ...string) ([]byte, error) {
			calls = append(calls, append([]string(nil), args...))
			switch {
			case len(args) >= 2 && args[0] == "api" && args[1] == "repos/owner/repo/issues/9":
				return []byte(`{"title":"Real title","body":"the body","state":"OPEN","created_at":"2026-08-30T12:00:00Z"}`), nil
			case len(args) >= 2 && args[0] == "api" && strings.Contains(args[1], "dependencies/blocked_by"):
				return []byte(`[]`), nil
			case len(args) >= 2 && args[0] == "api" && strings.Contains(args[1], "sub_issues"):
				return []byte(`[{"number":10,"state":"open"}]`), nil
			}
			return nil, fmt.Errorf("unexpected gh call: %#v", args)
		},
	}
	issue := &Issue{Number: 9, Title: "scraped title", State: "open"}
	if err := gh.HydrateIssue(context.Background(), "owner/repo", issue); err != nil {
		t.Fatalf("HydrateIssue() error = %v", err)
	}
	if issue.Body != "the body" || issue.Title != "Real title" || issue.State != "open" || issue.CreatedAt.IsZero() {
		t.Fatalf("hydrated issue = %#v", issue)
	}
	if !issue.HasSubIssues {
		t.Fatal("HasSubIssues = false, want true for an open sub-issue")
	}
	if len(calls) != 3 {
		t.Fatalf("gh calls = %#v, want exactly the issue read and the two dependency lookups", calls)
	}
	for _, call := range calls {
		if len(call) == 0 || call[0] != "api" {
			t.Fatalf("gh calls = %#v, want only `gh api` REST calls", calls)
		}
		for _, arg := range call {
			if strings.Contains(arg, "graphql") || strings.HasPrefix(arg, "query") {
				t.Fatalf("gh calls = %#v, want no GraphQL query", calls)
			}
		}
	}
}

// TestGHCLIHydrateIssuePrefersPublicAPI keeps a public repository's hydration
// off the authenticated token's rate limit.
func TestGHCLIHydrateIssuePrefersPublicAPI(t *testing.T) {
	var requested []string
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		runCommand: func(context.Context, ...string) ([]byte, error) {
			return nil, errors.New("should not run gh")
		},
		publicAPI: func(_ context.Context, requestURL string) ([]byte, http.Header, int, error) {
			requested = append(requested, requestURL)
			switch requestURL {
			case "https://api.github.com/repos/owner/repo":
				return []byte(`{"private":false}`), nil, http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/9":
				return []byte(`{"body":"body text"}`), nil, http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/9/dependencies/blocked_by":
				return []byte(`[]`), nil, http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/9/sub_issues":
				return []byte(`[]`), nil, http.StatusOK, nil
			}
			t.Fatalf("unexpected request URL: %s", requestURL)
			return nil, nil, 0, nil
		},
	}
	issue := &Issue{Number: 9}
	if err := gh.HydrateIssue(context.Background(), "owner/repo", issue); err != nil {
		t.Fatalf("HydrateIssue() error = %v", err)
	}
	if issue.Body != "body text" {
		t.Fatalf("Body = %q", issue.Body)
	}
	for _, requestURL := range requested {
		if strings.Contains(requestURL, "graphql") {
			t.Fatalf("requested %v, want no GraphQL", requested)
		}
	}
}

// TestGlorpIssueHandledSnapshot covers the predicate browser mode diffs
// against: in-flight and completed work is handled, failed work is not,
// because it gets dispatched again and still needs its metadata.
func TestGlorpIssueHandledSnapshot(t *testing.T) {
	w := &Glorp{}
	issue := func(number int) Issue {
		return Issue{Number: number, Target: "lsegal/glorp", Repository: "lsegal/glorp"}
	}
	if w.issueHandled(issue(1)) {
		t.Fatal("issueHandled() = true before any snapshot was published")
	}
	w.publishHandledIssues(map[string]workState{
		"lsegal/glorp#1": {Status: "completed"},
		"lsegal/glorp#2": {Status: "failed"},
		"lsegal/glorp#3": {Status: "active"},
	}, map[string]string{"lsegal/glorp#4": "claude"})
	for number, want := range map[int]bool{1: true, 2: false, 3: true, 4: true, 5: false} {
		if got := w.issueHandled(issue(number)); got != want {
			t.Fatalf("issueHandled(#%d) = %v, want %v", number, got, want)
		}
	}
}
