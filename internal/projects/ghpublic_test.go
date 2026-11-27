package projects

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func newPublicHeader(link string) http.Header {
	header := http.Header{}
	if link != "" {
		header.Set("Link", link)
	}
	return header
}

// publicResponse is one canned public REST API reply: its JSON body and the
// Link header that points at the next page, if any.
type publicResponse struct {
	body string
	link string
}

// publicRepoResponse answers the visibility probe for owner/repo as public.
var publicRepoResponse = publicResponse{body: `{"private":false}`}

// publicOnlyGH returns a GHCLI for a public owner/repo that serves every
// public API request from responses, and fails the test on any other URL or
// on any attempt to run the gh CLI.
func publicOnlyGH(t *testing.T, responses map[string]publicResponse) GHCLI {
	t.Helper()
	return GHCLI{
		publicRepoCache: &sync.Map{},
		runCommand: func(_ context.Context, args ...string) ([]byte, error) {
			t.Errorf("ran gh %q, want only public API requests", args)
			return nil, errors.New("should not run gh")
		},
		publicAPI: func(_ context.Context, requestURL string) ([]byte, http.Header, int, error) {
			response, ok := responses[requestURL]
			if !ok {
				t.Fatalf("unexpected public API request: %s", requestURL)
			}
			return []byte(response.body), newPublicHeader(response.link), http.StatusOK, nil
		},
	}
}

func TestIsPublicRepoWithoutCacheNeverCallsNetwork(t *testing.T) {
	gh := GHCLI{publicAPI: func(context.Context, string) ([]byte, http.Header, int, error) {
		t.Fatal("publicAPI should not be called when publicRepoCache is nil")
		return nil, nil, 0, nil
	}}
	if gh.isPublicRepo(context.Background(), "owner/repo") {
		t.Fatal("isPublicRepo() = true, want false without a cache")
	}
}

func TestIsPublicRepoCachesResult(t *testing.T) {
	calls := 0
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		publicAPI: func(context.Context, string) ([]byte, http.Header, int, error) {
			calls++
			return []byte(`{"private":false}`), nil, http.StatusOK, nil
		},
	}
	if !gh.isPublicRepo(context.Background(), "owner/repo") {
		t.Fatal("isPublicRepo() = false, want true")
	}
	if !gh.isPublicRepo(context.Background(), "owner/repo") {
		t.Fatal("isPublicRepo() = false on second call, want true")
	}
	if calls != 1 {
		t.Fatalf("publicAPI calls = %d, want 1 (result should be cached)", calls)
	}
}

func TestIsPublicRepoFalseForPrivateRepo(t *testing.T) {
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		publicAPI: func(context.Context, string) ([]byte, http.Header, int, error) {
			return []byte(`{"private":true}`), nil, http.StatusOK, nil
		},
	}
	if gh.isPublicRepo(context.Background(), "owner/repo") {
		t.Fatal("isPublicRepo() = true, want false for a private repo")
	}
}

func TestIsPublicRepoFalseOnRequestFailure(t *testing.T) {
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		publicAPI: func(context.Context, string) ([]byte, http.Header, int, error) {
			return nil, nil, 0, errors.New("network unreachable")
		},
	}
	if gh.isPublicRepo(context.Background(), "owner/repo") {
		t.Fatal("isPublicRepo() = true, want false when the probe request fails")
	}
}

func TestIsPublicRepoFalseOn404(t *testing.T) {
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		publicAPI: func(context.Context, string) ([]byte, http.Header, int, error) {
			return []byte(`{"message":"Not Found"}`), nil, http.StatusNotFound, nil
		},
	}
	if gh.isPublicRepo(context.Background(), "owner/repo") {
		t.Fatal("isPublicRepo() = true, want false on a 404")
	}
}

func TestApiGETUsesPublicAPIWhenPublic(t *testing.T) {
	gh := publicOnlyGH(t, map[string]publicResponse{
		"https://api.github.com/repos/owner/repo/issues/7": {body: `{"state":"open"}`},
	})
	body, err := gh.apiGETObject(context.Background(), true, "repos/owner/repo/issues/7")
	if err != nil || string(body) != `{"state":"open"}` {
		t.Fatalf("apiGETObject() = (%q, %v)", body, err)
	}
}

func TestApiGETFallsBackToGHOnPublicFailure(t *testing.T) {
	var ghArgs []string
	gh := GHCLI{
		publicAPI: func(context.Context, string) ([]byte, http.Header, int, error) {
			return nil, nil, http.StatusForbidden, nil
		},
		runCommand: func(_ context.Context, args ...string) ([]byte, error) {
			ghArgs = args
			return []byte(`{"state":"open"}`), nil
		},
	}
	body, err := gh.apiGETObject(context.Background(), true, "repos/owner/repo/issues/7")
	if err != nil || string(body) != `{"state":"open"}` {
		t.Fatalf("apiGETObject() = (%q, %v)", body, err)
	}
	want := []string{"api", "repos/owner/repo/issues/7"}
	if !reflect.DeepEqual(ghArgs, want) {
		t.Fatalf("gh args = %#v, want %#v", ghArgs, want)
	}
}

func TestApiGETSkipsPublicAPIWhenNotPublic(t *testing.T) {
	gh := GHCLI{
		publicAPI: func(context.Context, string) ([]byte, http.Header, int, error) {
			t.Fatal("publicAPI should not be called when public is false")
			return nil, nil, 0, nil
		},
		runCommand: func(context.Context, ...string) ([]byte, error) {
			return []byte(`{"state":"open"}`), nil
		},
	}
	if _, err := gh.apiGETObject(context.Background(), false, "repos/owner/repo/issues/7"); err != nil {
		t.Fatalf("apiGETObject() error = %v", err)
	}
}

func TestApiGETPassesExtraGHArgsOnFallback(t *testing.T) {
	var ghArgs []string
	gh := GHCLI{
		runCommand: func(_ context.Context, args ...string) ([]byte, error) {
			ghArgs = args
			return []byte(`{"state":"open"}`), nil
		},
	}
	if _, err := gh.apiGETObject(context.Background(), false, "repos/owner/repo/issues/7", "--header", "X-GitHub-Api-Version: 2022-11-28"); err != nil {
		t.Fatalf("apiGETObject() error = %v", err)
	}
	want := []string{"api", "repos/owner/repo/issues/7", "--header", "X-GitHub-Api-Version: 2022-11-28"}
	if !reflect.DeepEqual(ghArgs, want) {
		t.Fatalf("gh args = %#v, want %#v", ghArgs, want)
	}
}

func TestApiGETPaginatedFollowsLinkHeaderWhenPublic(t *testing.T) {
	gh := publicOnlyGH(t, map[string]publicResponse{
		"https://api.github.com/repos/owner/repo/issues/7/comments":        {body: `[{"body":"first"}]`, link: `<https://api.github.com/repos/owner/repo/issues/7/comments?page=2>; rel="next"`},
		"https://api.github.com/repos/owner/repo/issues/7/comments?page=2": {body: `[{"body":"second"}]`},
	})
	body, err := gh.apiGETPaginated(context.Background(), true, "repos/owner/repo/issues/7/comments")
	if err != nil {
		t.Fatalf("apiGETPaginated() error = %v", err)
	}
	want := `[{"body":"first"},{"body":"second"}]`
	if string(body) != want {
		t.Fatalf("apiGETPaginated() = %s, want %s", body, want)
	}
}

func TestApiGETPaginatedFallsBackToGHOnFailure(t *testing.T) {
	var ghArgs []string
	gh := GHCLI{
		publicAPI: func(context.Context, string) ([]byte, http.Header, int, error) {
			return nil, nil, http.StatusForbidden, nil
		},
		runCommand: func(_ context.Context, args ...string) ([]byte, error) {
			ghArgs = args
			return []byte(`[{"body":"from gh"}]`), nil
		},
	}
	body, err := gh.apiGETPaginated(context.Background(), true, "repos/owner/repo/issues/7/comments")
	if err != nil || string(body) != `[{"body":"from gh"}]` {
		t.Fatalf("apiGETPaginated() = (%s, %v)", body, err)
	}
	want := []string{"api", "repos/owner/repo/issues/7/comments", "--paginate"}
	if !reflect.DeepEqual(ghArgs, want) {
		t.Fatalf("gh args = %#v, want %#v", ghArgs, want)
	}
}

func TestNextPageURL(t *testing.T) {
	for _, test := range []struct {
		name string
		link string
		want string
	}{
		{name: "no link header", link: "", want: ""},
		{name: "next only", link: `<https://api.github.com/x?page=2>; rel="next"`, want: "https://api.github.com/x?page=2"},
		{name: "prev and next", link: `<https://api.github.com/x?page=1>; rel="prev", <https://api.github.com/x?page=3>; rel="next"`, want: "https://api.github.com/x?page=3"},
		{name: "last only, no next", link: `<https://api.github.com/x?page=9>; rel="last"`, want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := nextPageURL(test.link); got != test.want {
				t.Fatalf("nextPageURL(%q) = %q, want %q", test.link, got, test.want)
			}
		})
	}
}

func TestPublicIssueSearchQuery(t *testing.T) {
	for _, test := range []struct {
		name      string
		repo      string
		filter    string
		allIssues bool
		selfLogin string
		want      string
	}{
		{name: "default filter substitutes self login without repeating qualifiers", repo: "owner/repo", filter: defaultIssueFilter, selfLogin: "lsegal", want: "repo:owner/repo is:issue state:open assignee:lsegal"},
		{name: "all issues ignores filter", repo: "owner/repo", filter: defaultIssueFilter, allIssues: true, selfLogin: "lsegal", want: "repo:owner/repo is:issue state:open"},
		{name: "custom filter without author", repo: "owner/repo", filter: "label:bug", want: "repo:owner/repo is:issue state:open label:bug"},
		{name: "author filter still substitutes self login", repo: "owner/repo", filter: "author:@me", selfLogin: "lsegal", want: "repo:owner/repo is:issue state:open author:lsegal"},
		{name: "filter naming its own kind cannot ask for pull requests", repo: "owner/repo", filter: "is:pr author:@me", selfLogin: "lsegal", want: "repo:owner/repo is:issue state:open author:lsegal"},
		{name: "filter naming its own state cannot ask for closed issues", repo: "owner/repo", filter: "state:closed label:bug", want: "repo:owner/repo is:issue state:open label:bug"},
		{name: "filter naming kind and state keeps neither", repo: "owner/repo", filter: "type:issue is:closed", want: "repo:owner/repo is:issue state:open"},
		{name: "qualifiers are never repeated", repo: "owner/repo", filter: "is:issue state:open label:bug", want: "repo:owner/repo is:issue state:open label:bug"},
		{name: "empty filter falls back to the defaults", repo: "owner/repo", want: "repo:owner/repo is:issue state:open"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := publicIssueSearchQuery(test.repo, test.filter, test.allIssues, test.selfLogin); got != test.want {
				t.Fatalf("publicIssueSearchQuery() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveSelfLoginCaches(t *testing.T) {
	calls := 0
	gh := GHCLI{
		selfLoginCache: &sync.Map{},
		runCommand: func(context.Context, ...string) ([]byte, error) {
			calls++
			return []byte("lsegal\n"), nil
		},
	}
	login, err := gh.resolveSelfLogin(context.Background())
	if err != nil || login != "lsegal" {
		t.Fatalf("resolveSelfLogin() = (%q, %v)", login, err)
	}
	if _, err := gh.resolveSelfLogin(context.Background()); err != nil {
		t.Fatalf("resolveSelfLogin() second call error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("gh calls = %d, want 1 (login should be cached)", calls)
	}
}

func TestListPublicIssuesResolvesSelfLoginAndFiltersPullRequests(t *testing.T) {
	gh := publicOnlyGH(t, map[string]publicResponse{
		"https://api.github.com/search/issues?per_page=100&q=repo%3Aowner%2Frepo+is%3Aissue+state%3Aopen+assignee%3Alsegal": {body: `{"items":[{"number":7,"title":"bug","body":"details","state":"open","created_at":"2026-07-20T17:38:43Z","labels":[{"name":"bug"}]},{"number":8,"title":"a pr","pull_request":{}}]}`},
	})
	gh.selfLoginCache = &sync.Map{}
	gh.runCommand = func(context.Context, ...string) ([]byte, error) {
		return []byte("lsegal\n"), nil
	}
	issues, ok := gh.listPublicIssues(context.Background(), "owner/repo", defaultIssueFilter, false)
	if !ok {
		t.Fatal("listPublicIssues() ok = false, want true")
	}
	if len(issues) != 1 || issues[0].Number != 7 || issues[0].Title != "bug" || len(issues[0].Labels) != 1 || issues[0].Labels[0].Name != "bug" {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestListPublicIssuesFallsBackOnFailure(t *testing.T) {
	gh := GHCLI{
		publicAPI: func(context.Context, string) ([]byte, http.Header, int, error) {
			return nil, nil, http.StatusForbidden, nil
		},
	}
	if _, ok := gh.listPublicIssues(context.Background(), "owner/repo", "", true); ok {
		t.Fatal("listPublicIssues() ok = true, want false on request failure")
	}
}

func TestListPublicIssuesFallsBackOnIncompleteResults(t *testing.T) {
	gh := GHCLI{
		publicAPI: func(context.Context, string) ([]byte, http.Header, int, error) {
			return []byte(`{"items":[{"number":7,"title":"bug","state":"open"}],"incomplete_results":true}`), nil, http.StatusOK, nil
		},
	}
	issues, ok := gh.listPublicIssues(context.Background(), "owner/repo", "", true)
	if ok || issues != nil {
		t.Fatalf("listPublicIssues() = (%#v, %v), want (nil, false) on incomplete_results:true", issues, ok)
	}
}

func TestListIssuesUsesPublicSearchAPIForPublicRepo(t *testing.T) {
	gh := publicOnlyGH(t, map[string]publicResponse{
		"https://api.github.com/repos/owner/repo":                                                         publicRepoResponse,
		"https://api.github.com/search/issues?per_page=100&q=repo%3Aowner%2Frepo+is%3Aissue+state%3Aopen": {body: `{"items":[{"number":7,"title":"bug","state":"open","created_at":"2026-07-20T17:38:43Z"}]}`},
		"https://api.github.com/repos/owner/repo/issues/7/dependencies/blocked_by":                        {body: `[]`},
		"https://api.github.com/repos/owner/repo/issues/7/sub_issues":                                     {body: `[]`},
	})
	gh.AllIssues = true
	issues, err := gh.ListIssues(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("ListIssues() error = %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 7 {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestListIssuesFallsBackToAuthenticatedSearchOnIncompleteResults(t *testing.T) {
	var calls [][]string
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		publicAPI: func(_ context.Context, requestURL string) ([]byte, http.Header, int, error) {
			switch requestURL {
			case "https://api.github.com/repos/owner/repo":
				return []byte(`{"private":false}`), nil, http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/7/dependencies/blocked_by":
				return []byte(`[]`), nil, http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/7/sub_issues":
				return []byte(`[]`), nil, http.StatusOK, nil
			}
			return []byte(`{"items":[],"incomplete_results":true}`), nil, http.StatusOK, nil
		},
		runCommand: func(_ context.Context, args ...string) ([]byte, error) {
			calls = append(calls, append([]string(nil), args...))
			if len(args) > 1 && args[0] == "api" && args[1] == "search/issues" {
				return []byte(`{"items":[{"number":7,"title":"bug"}]}`), nil
			}
			return []byte(`[]`), nil
		},
	}
	gh.Filter = defaultIssueFilter
	issues, err := gh.ListIssues(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("ListIssues() error = %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 7 {
		t.Fatalf("issues = %#v, want the authenticated REST search fallback result", issues)
	}
	for _, call := range calls {
		for _, arg := range call {
			if strings.Contains(arg, "graphql") {
				t.Fatalf("gh calls = %#v, want no graphql call", calls)
			}
		}
	}
}

func TestListIssuesUsesAuthenticatedSearchWhenNotPublic(t *testing.T) {
	publicAPICalls := 0
	var calls [][]string
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		publicAPI: func(context.Context, string) ([]byte, http.Header, int, error) {
			publicAPICalls++
			return []byte(`{"private":true}`), nil, http.StatusOK, nil
		},
		runCommand: func(_ context.Context, args ...string) ([]byte, error) {
			calls = append(calls, append([]string(nil), args...))
			if len(args) > 1 && args[0] == "api" && args[1] == "search/issues" {
				return []byte(`{"items":[{"number":7,"title":"bug"}]}`), nil
			}
			return []byte(`[]`), nil
		},
	}
	gh.Filter = defaultIssueFilter
	issues, err := gh.ListIssues(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("ListIssues() error = %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 7 {
		t.Fatalf("issues = %#v", issues)
	}
	want := []string{"api", "search/issues", "-X", "GET", "-f", "q=" + publicIssueSearchQuery("owner/repo", defaultIssueFilter, false, ""), "-f", "per_page=100", "--paginate"}
	if len(calls) == 0 || !reflect.DeepEqual(calls[0], want) {
		t.Fatalf("gh calls = %#v, want first call %#v", calls, want)
	}
	if publicAPICalls != 1 {
		t.Fatalf("publicAPI calls = %d, want 1 (only the visibility probe, not an issue search)", publicAPICalls)
	}
}

func TestOriginatingWorkStateUsesPublicAPIForPublicRepo(t *testing.T) {
	gh := publicOnlyGH(t, map[string]publicResponse{
		"https://api.github.com/repos/owner/repo":                                publicRepoResponse,
		"https://api.github.com/repos/owner/repo/issues/7":                       {body: `{"state":"open"}`},
		"https://api.github.com/repos/owner/repo/issues/7/timeline?per_page=100": {body: `[]`},
	})
	state, err := gh.OriginatingWorkState(context.Background(), "owner/repo", 7)
	if err != nil || state.IssueState != "open" {
		t.Fatalf("OriginatingWorkState() = (%#v, %v)", state, err)
	}
}

// TestLoadDependenciesPublicAPI reads blockers and sub-issues of a public
// repository's issue through the public API only. The paged cases keep a
// blocker or open sub-issue on a later page from being dropped, which would let
// aviary dispatch an issue GitHub still considers blocked, and keep closed
// sub-issues spread over several pages from marking the issue as split.
func TestLoadDependenciesPublicAPI(t *testing.T) {
	const (
		blockedBy = "https://api.github.com/repos/owner/repo/issues/9/dependencies/blocked_by"
		subIssues = "https://api.github.com/repos/owner/repo/issues/9/sub_issues"
	)
	tests := []struct {
		name             string
		body             string
		responses        map[string]publicResponse
		wantDependsOn    []IssueDependency
		wantHasSubIssues bool
	}{
		{
			name: "body dependency and open sub-issue",
			body: "depends on #4",
			responses: map[string]publicResponse{
				"https://api.github.com/repos/owner/repo/issues/4": {body: `{"state":"open"}`},
				blockedBy: {body: `[]`},
				subIssues: {body: `[{"number":10,"state":"open"}]`},
			},
			wantDependsOn:    []IssueDependency{{Number: 4, State: "open", Repository: "owner/repo"}},
			wantHasSubIssues: true,
		},
		{
			name: "closed and merged sub-issues",
			responses: map[string]publicResponse{
				blockedBy: {body: `[]`},
				subIssues: {body: `[{"number":10,"state":"closed"},{"number":11,"state":"MERGED"}]`},
			},
		},
		{
			name: "blocker and open sub-issue on second pages",
			responses: map[string]publicResponse{
				blockedBy:             {body: `[{"number":3,"state":"closed"}]`, link: `<` + blockedBy + `?page=2>; rel="next"`},
				blockedBy + "?page=2": {body: `[{"number":5,"state":"open"}]`},
				subIssues:             {body: `[{"number":10,"state":"closed"}]`, link: `<` + subIssues + `?page=2>; rel="next"`},
				subIssues + "?page=2": {body: `[{"number":11,"state":"open"}]`},
			},
			wantDependsOn:    []IssueDependency{{Number: 3, State: "closed", Repository: "owner/repo"}, {Number: 5, State: "open", Repository: "owner/repo"}},
			wantHasSubIssues: true,
		},
		{
			name: "closed sub-issues across pages",
			responses: map[string]publicResponse{
				blockedBy:             {body: `[]`},
				subIssues:             {body: `[{"number":10,"state":"closed"}]`, link: `<` + subIssues + `?page=2>; rel="next"`},
				subIssues + "?page=2": {body: `[{"number":11,"state":"closed"}]`},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responses := map[string]publicResponse{"https://api.github.com/repos/owner/repo": publicRepoResponse}
			for requestURL, response := range tt.responses {
				responses[requestURL] = response
			}
			gh := publicOnlyGH(t, responses)
			issue := &Issue{Number: 9, Body: tt.body}
			if err := gh.loadDependencies(context.Background(), "owner/repo", issue); err != nil {
				t.Fatalf("loadDependencies() error = %v", err)
			}
			if (len(issue.DependsOn) != 0 || len(tt.wantDependsOn) != 0) && !reflect.DeepEqual(issue.DependsOn, tt.wantDependsOn) {
				t.Fatalf("DependsOn = %#v, want %#v", issue.DependsOn, tt.wantDependsOn)
			}
			if issue.HasSubIssues != tt.wantHasSubIssues {
				t.Fatalf("HasSubIssues = %v, want %v", issue.HasSubIssues, tt.wantHasSubIssues)
			}
		})
	}
}

func TestLoadDependenciesUsesAuthenticatedRESTForPrivateRepo(t *testing.T) {
	var calls [][]string
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		publicAPI: func(context.Context, string) ([]byte, http.Header, int, error) {
			return []byte(`{"private":true}`), nil, http.StatusOK, nil
		},
		runCommand: func(_ context.Context, args ...string) ([]byte, error) {
			calls = append(calls, append([]string(nil), args...))
			switch {
			case len(args) >= 2 && args[0] == "api" && args[1] == "repos/owner/repo/issues/4":
				return []byte(`{"state":"open"}`), nil
			case len(args) >= 2 && args[0] == "api" && strings.Contains(args[1], "dependencies/blocked_by"):
				return []byte(`[]`), nil
			case len(args) >= 2 && args[0] == "api" && strings.Contains(args[1], "sub_issues"):
				return []byte(`[]`), nil
			}
			return nil, fmt.Errorf("unexpected gh call: %#v", args)
		},
	}
	issue := &Issue{Number: 9, Body: "depends on #4"}
	if err := gh.loadDependencies(context.Background(), "owner/repo", issue); err != nil {
		t.Fatalf("loadDependencies() error = %v", err)
	}
	if len(issue.DependsOn) != 1 || issue.DependsOn[0].Number != 4 || issue.DependsOn[0].State != "open" {
		t.Fatalf("DependsOn = %#v", issue.DependsOn)
	}
	if issue.HasSubIssues {
		t.Fatal("HasSubIssues = true, want false")
	}
	for _, call := range calls {
		if len(call) > 0 && call[0] == "issue" {
			t.Fatalf("gh calls = %#v, want no `gh issue view` (queries GraphQL)", calls)
		}
	}
}

// TestLoadDependenciesPaginatesAuthenticatedREST makes the private-repository
// path ask `gh api` for every page of both relationship endpoints, and decode
// the merged array it returns.
func TestLoadDependenciesPaginatesAuthenticatedREST(t *testing.T) {
	var calls [][]string
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		publicAPI: func(context.Context, string) ([]byte, http.Header, int, error) {
			return []byte(`{"private":true}`), nil, http.StatusOK, nil
		},
		runCommand: func(_ context.Context, args ...string) ([]byte, error) {
			calls = append(calls, append([]string(nil), args...))
			switch {
			case len(args) >= 2 && args[0] == "api" && strings.Contains(args[1], "dependencies/blocked_by"):
				return []byte(`[{"number":3,"state":"closed"},{"number":5,"state":"open"}]`), nil
			case len(args) >= 2 && args[0] == "api" && strings.Contains(args[1], "sub_issues"):
				return []byte(`[{"number":10,"state":"closed"},{"number":11,"state":"open"}]`), nil
			}
			return nil, fmt.Errorf("unexpected gh call: %#v", args)
		},
	}
	issue := &Issue{Number: 9}
	if err := gh.loadDependencies(context.Background(), "owner/repo", issue); err != nil {
		t.Fatalf("loadDependencies() error = %v", err)
	}
	want := [][]string{
		{"api", "repos/owner/repo/issues/9/dependencies/blocked_by", "--paginate", "--header", "X-GitHub-Api-Version: 2022-11-28"},
		{"api", "repos/owner/repo/issues/9/sub_issues", "--paginate", "--header", "X-GitHub-Api-Version: 2022-11-28"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("gh calls = %#v, want %#v", calls, want)
	}
	if len(issue.DependsOn) != 2 || issue.DependsOn[1].Number != 5 {
		t.Fatalf("DependsOn = %#v, want both blockers", issue.DependsOn)
	}
	if !issue.HasSubIssues {
		t.Fatal("HasSubIssues = false, want true")
	}
}

func TestListCommentsUsesPublicAPIForPublicRepo(t *testing.T) {
	gh := publicOnlyGH(t, map[string]publicResponse{
		"https://api.github.com/repos/owner/repo":                   publicRepoResponse,
		"https://api.github.com/repos/owner/repo/issues/7/comments": {body: `[{"body":"hi","created_at":"2026-07-20T12:00:00Z"}]`},
	})
	comments, err := gh.ListComments(context.Background(), "owner/repo", 7)
	if err != nil {
		t.Fatalf("ListComments() error = %v", err)
	}
	if len(comments) != 1 || comments[0].Body != "hi" {
		t.Fatalf("comments = %#v", comments)
	}
}

// TestDefaultIssueFilterOmitsKindAndStateQualifiers pins the kind and state
// qualifiers out of the filter a user is shown and overrides. aviary only ever
// dispatches an open issue, so they are appended to every query it builds
// instead, and overriding --filter must not mean repeating them.
func TestDefaultIssueFilterOmitsKindAndStateQualifiers(t *testing.T) {
	for _, qualifier := range []string{"is:issue", "state:open"} {
		if strings.Contains(defaultIssueFilter, qualifier) {
			t.Fatalf("defaultIssueFilter = %q, want it not to name %q", defaultIssueFilter, qualifier)
		}
	}
	for _, filter := range []string{"", defaultIssueFilter, "label:ready", "is:pr", "state:closed"} {
		if got := strings.Join(issueSearchTerms(filter, false), " "); !strings.HasPrefix(got, "is:issue state:open") {
			t.Errorf("issueSearchTerms(%q) = %q, want it to open with is:issue state:open", filter, got)
		}
		if got := projectItemQuery(filter, false); !strings.HasPrefix(got, "is:issue is:open") {
			t.Errorf("projectItemQuery(%q) = %q, want it to open with is:issue is:open", filter, got)
		}
	}
}

// TestDefaultIssueFilterIsAssigneeOnly pins the default filter to the
// assignee qualifier alone (see issue #421). Assignment is the gate: only
// someone with triage access can assign an issue, while anyone can author one,
// so an author term protects nothing and only excludes issues someone else
// wrote that the authenticated user was assigned.
func TestDefaultIssueFilterIsAssigneeOnly(t *testing.T) {
	if defaultIssueFilter != "assignee:@me" {
		t.Fatalf("defaultIssueFilter = %q, want assignee:@me", defaultIssueFilter)
	}
	query := publicIssueSearchQuery("owner/repo", defaultIssueFilter, false, "lsegal")
	if !strings.Contains(query, "assignee:lsegal") {
		t.Fatalf("publicIssueSearchQuery() = %q, want it to contain assignee:lsegal", query)
	}
	if strings.Contains(query, "author:") {
		t.Fatalf("publicIssueSearchQuery() = %q, want no author qualifier", query)
	}
	if strings.Contains(query, "@me") {
		t.Fatalf("publicIssueSearchQuery() = %q, want every @me qualifier substituted", query)
	}
}

// TestListIssuesSearchesOnceWithFilter guards against reintroducing the
// two-search union that split the filter's qualifiers into an OR (see issue
// #371): a single search carries the filter exactly, whether it is the
// assignee-only default or a configured filter that still names an author.
func TestListIssuesSearchesOnceWithFilter(t *testing.T) {
	for _, tt := range []struct {
		filter      string
		want, avoid []string
	}{
		{filter: defaultIssueFilter, want: []string{"assignee:@me"}, avoid: []string{"author:"}},
		{filter: "assignee:@me author:@me", want: []string{"assignee:@me", "author:@me"}},
	} {
		var queries []string
		gh := GHCLI{
			publicRepoCache: &sync.Map{},
			publicAPI: func(context.Context, string) ([]byte, http.Header, int, error) {
				return []byte(`{"private":true}`), nil, http.StatusOK, nil
			},
			runCommand: func(_ context.Context, args ...string) ([]byte, error) {
				if len(args) > 1 && args[0] == "api" && args[1] == "search/issues" {
					for i, arg := range args {
						if arg == "-f" && i+1 < len(args) && strings.HasPrefix(args[i+1], "q=") {
							queries = append(queries, strings.TrimPrefix(args[i+1], "q="))
						}
					}
					return []byte(`{"items":[{"number":7,"title":"bug"}]}`), nil
				}
				return []byte(`[]`), nil
			},
		}
		gh.Filter = tt.filter
		if _, err := gh.ListIssues(context.Background(), "owner/repo"); err != nil {
			t.Fatalf("ListIssues(%q) error = %v", tt.filter, err)
		}
		if len(queries) != 1 {
			t.Fatalf("filter %q: issue searches = %#v, want exactly one search", tt.filter, queries)
		}
		for _, want := range tt.want {
			if !strings.Contains(queries[0], want) {
				t.Errorf("filter %q: search query = %q, want it to contain %q", tt.filter, queries[0], want)
			}
		}
		for _, avoid := range tt.avoid {
			if strings.Contains(queries[0], avoid) {
				t.Errorf("filter %q: search query = %q, want it not to contain %q", tt.filter, queries[0], avoid)
			}
		}
	}
}
