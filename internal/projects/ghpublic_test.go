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
	var requestedURL string
	ranGH := false
	gh := GHCLI{
		publicAPI: func(_ context.Context, requestURL string) ([]byte, http.Header, int, error) {
			requestedURL = requestURL
			return []byte(`{"state":"open"}`), nil, http.StatusOK, nil
		},
		runCommand: func(context.Context, ...string) ([]byte, error) {
			ranGH = true
			return nil, nil
		},
	}
	body, err := gh.apiGETObject(context.Background(), true, "repos/owner/repo/issues/7")
	if err != nil || string(body) != `{"state":"open"}` {
		t.Fatalf("apiGETObject() = (%q, %v)", body, err)
	}
	if ranGH {
		t.Fatal("apiGETObject() ran the gh CLI even though the public request succeeded")
	}
	if requestedURL != "https://api.github.com/repos/owner/repo/issues/7" {
		t.Fatalf("requested URL = %q", requestedURL)
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
	calls := 0
	gh := GHCLI{
		publicAPI: func(_ context.Context, requestURL string) ([]byte, http.Header, int, error) {
			calls++
			if calls == 1 {
				if requestURL != "https://api.github.com/repos/owner/repo/issues/7/comments" {
					t.Fatalf("first request URL = %q", requestURL)
				}
				return []byte(`[{"body":"first"}]`), newPublicHeader(`<https://api.github.com/repos/owner/repo/issues/7/comments?page=2>; rel="next"`), http.StatusOK, nil
			}
			if requestURL != "https://api.github.com/repos/owner/repo/issues/7/comments?page=2" {
				t.Fatalf("second request URL = %q", requestURL)
			}
			return []byte(`[{"body":"second"}]`), newPublicHeader(""), http.StatusOK, nil
		},
	}
	body, err := gh.apiGETPaginated(context.Background(), true, "repos/owner/repo/issues/7/comments")
	if err != nil {
		t.Fatalf("apiGETPaginated() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("public API calls = %d, want 2", calls)
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
		{name: "default filter substitutes self login without repeating qualifiers", repo: "owner/repo", filter: defaultIssueFilter, selfLogin: "lsegal", want: "repo:owner/repo is:issue state:open assignee:lsegal author:lsegal"},
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
	var searchURL string
	gh := GHCLI{
		selfLoginCache: &sync.Map{},
		runCommand: func(context.Context, ...string) ([]byte, error) {
			return []byte("lsegal\n"), nil
		},
		publicAPI: func(_ context.Context, requestURL string) ([]byte, http.Header, int, error) {
			searchURL = requestURL
			return []byte(`{"items":[{"number":7,"title":"bug","body":"details","state":"open","created_at":"2026-07-20T17:38:43Z","labels":[{"name":"bug"}]},{"number":8,"title":"a pr","pull_request":{}}]}`), nil, http.StatusOK, nil
		},
	}
	issues, ok := gh.listPublicIssues(context.Background(), "owner/repo", defaultIssueFilter, false)
	if !ok {
		t.Fatal("listPublicIssues() ok = false, want true")
	}
	if len(issues) != 1 || issues[0].Number != 7 || issues[0].Title != "bug" || len(issues[0].Labels) != 1 || issues[0].Labels[0].Name != "bug" {
		t.Fatalf("issues = %#v", issues)
	}
	if !strings.Contains(searchURL, "repo%3Aowner%2Frepo") || !strings.Contains(searchURL, "assignee%3Alsegal") {
		t.Fatalf("search URL = %q, want it to encode repo and resolved assignee qualifiers", searchURL)
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
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		runCommand: func(context.Context, ...string) ([]byte, error) {
			t.Fatal("gh CLI should not run for a public repo's issue list")
			return nil, nil
		},
		publicAPI: func(_ context.Context, requestURL string) ([]byte, http.Header, int, error) {
			switch requestURL {
			case "https://api.github.com/repos/owner/repo":
				return []byte(`{"private":false}`), nil, http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/7/dependencies/blocked_by":
				return []byte(`[]`), nil, http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/7/sub_issues":
				return []byte(`[]`), nil, http.StatusOK, nil
			}
			return []byte(`{"items":[{"number":7,"title":"bug","state":"open","created_at":"2026-07-20T17:38:43Z"}]}`), nil, http.StatusOK, nil
		},
	}
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
	ranGH := false
	responses := []string{
		`{"private":false}`,
		`{"state":"open"}`,
		`[]`,
	}
	call := 0
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		runCommand: func(context.Context, ...string) ([]byte, error) {
			ranGH = true
			return nil, errors.New("should not run gh")
		},
		publicAPI: func(context.Context, string) ([]byte, http.Header, int, error) {
			body := responses[call]
			call++
			return []byte(body), nil, http.StatusOK, nil
		},
	}
	state, err := gh.OriginatingWorkState(context.Background(), "owner/repo", 7)
	if err != nil || state.IssueState != "open" {
		t.Fatalf("OriginatingWorkState() = (%#v, %v)", state, err)
	}
	if ranGH {
		t.Fatal("OriginatingWorkState() ran the gh CLI for a public repo")
	}
}

func TestLoadDependenciesUsesPublicAPIForPublicRepo(t *testing.T) {
	ranGH := false
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		runCommand: func(context.Context, ...string) ([]byte, error) {
			ranGH = true
			return nil, errors.New("should not run gh")
		},
		publicAPI: func(_ context.Context, requestURL string) ([]byte, http.Header, int, error) {
			switch requestURL {
			case "https://api.github.com/repos/owner/repo":
				return []byte(`{"private":false}`), nil, http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/4":
				return []byte(`{"state":"open"}`), nil, http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/9/dependencies/blocked_by":
				return []byte(`[]`), nil, http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/9/sub_issues":
				return []byte(`[{"number":10,"state":"open"}]`), nil, http.StatusOK, nil
			}
			t.Fatalf("unexpected request URL: %s", requestURL)
			return nil, nil, 0, nil
		},
	}
	issue := &Issue{Number: 9, Body: "depends on #4"}
	if err := gh.loadDependencies(context.Background(), "owner/repo", issue); err != nil {
		t.Fatalf("loadDependencies() error = %v", err)
	}
	if len(issue.DependsOn) != 1 || issue.DependsOn[0].Number != 4 || issue.DependsOn[0].State != "open" {
		t.Fatalf("DependsOn = %#v", issue.DependsOn)
	}
	if !issue.HasSubIssues {
		t.Fatal("HasSubIssues = false, want true")
	}
	if ranGH {
		t.Fatal("loadDependencies() ran the gh CLI for a public repo")
	}
}

func TestLoadDependenciesIgnoresClosedSubIssues(t *testing.T) {
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		runCommand: func(context.Context, ...string) ([]byte, error) {
			return nil, errors.New("should not run gh")
		},
		publicAPI: func(_ context.Context, requestURL string) ([]byte, http.Header, int, error) {
			switch requestURL {
			case "https://api.github.com/repos/owner/repo":
				return []byte(`{"private":false}`), nil, http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/9/dependencies/blocked_by":
				return []byte(`[]`), nil, http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/9/sub_issues":
				return []byte(`[{"number":10,"state":"closed"},{"number":11,"state":"MERGED"}]`), nil, http.StatusOK, nil
			}
			t.Fatalf("unexpected request URL: %s", requestURL)
			return nil, nil, 0, nil
		},
	}
	issue := &Issue{Number: 9}
	if err := gh.loadDependencies(context.Background(), "owner/repo", issue); err != nil {
		t.Fatalf("loadDependencies() error = %v", err)
	}
	if issue.HasSubIssues {
		t.Fatal("HasSubIssues = true, want false for only closed/merged sub-issues")
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

// TestLoadDependenciesReadsEveryPublicPage keeps a blocker or open sub-issue
// on a later page from being dropped, which would let aviary dispatch an issue
// GitHub still considers blocked.
func TestLoadDependenciesReadsEveryPublicPage(t *testing.T) {
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		runCommand: func(context.Context, ...string) ([]byte, error) {
			return nil, errors.New("should not run gh")
		},
		publicAPI: func(_ context.Context, requestURL string) ([]byte, http.Header, int, error) {
			switch requestURL {
			case "https://api.github.com/repos/owner/repo":
				return []byte(`{"private":false}`), nil, http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/9/dependencies/blocked_by":
				return []byte(`[{"number":3,"state":"closed"}]`), newPublicHeader(`<https://api.github.com/repos/owner/repo/issues/9/dependencies/blocked_by?page=2>; rel="next"`), http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/9/dependencies/blocked_by?page=2":
				return []byte(`[{"number":5,"state":"open"}]`), newPublicHeader(""), http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/9/sub_issues":
				return []byte(`[{"number":10,"state":"closed"}]`), newPublicHeader(`<https://api.github.com/repos/owner/repo/issues/9/sub_issues?page=2>; rel="next"`), http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/9/sub_issues?page=2":
				return []byte(`[{"number":11,"state":"open"}]`), newPublicHeader(""), http.StatusOK, nil
			}
			t.Fatalf("unexpected request URL: %s", requestURL)
			return nil, nil, 0, nil
		},
	}
	issue := &Issue{Number: 9}
	if err := gh.loadDependencies(context.Background(), "owner/repo", issue); err != nil {
		t.Fatalf("loadDependencies() error = %v", err)
	}
	want := []IssueDependency{{Number: 3, State: "closed"}, {Number: 5, State: "open"}}
	if !reflect.DeepEqual(issue.DependsOn, want) {
		t.Fatalf("DependsOn = %#v, want %#v", issue.DependsOn, want)
	}
	if !issue.HasSubIssues {
		t.Fatal("HasSubIssues = false, want true for an open sub-issue on the second page")
	}
}

// TestLoadDependenciesIgnoresClosedSubIssuesAcrossPages keeps closed
// sub-issues spread over several pages from marking the issue as split.
func TestLoadDependenciesIgnoresClosedSubIssuesAcrossPages(t *testing.T) {
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		runCommand: func(context.Context, ...string) ([]byte, error) {
			return nil, errors.New("should not run gh")
		},
		publicAPI: func(_ context.Context, requestURL string) ([]byte, http.Header, int, error) {
			switch requestURL {
			case "https://api.github.com/repos/owner/repo":
				return []byte(`{"private":false}`), nil, http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/9/dependencies/blocked_by":
				return []byte(`[]`), newPublicHeader(""), http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/9/sub_issues":
				return []byte(`[{"number":10,"state":"closed"}]`), newPublicHeader(`<https://api.github.com/repos/owner/repo/issues/9/sub_issues?page=2>; rel="next"`), http.StatusOK, nil
			case "https://api.github.com/repos/owner/repo/issues/9/sub_issues?page=2":
				return []byte(`[{"number":11,"state":"closed"}]`), newPublicHeader(""), http.StatusOK, nil
			}
			t.Fatalf("unexpected request URL: %s", requestURL)
			return nil, nil, 0, nil
		},
	}
	issue := &Issue{Number: 9}
	if err := gh.loadDependencies(context.Background(), "owner/repo", issue); err != nil {
		t.Fatalf("loadDependencies() error = %v", err)
	}
	if issue.HasSubIssues {
		t.Fatal("HasSubIssues = true, want false when every sub-issue on every page is closed")
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
	ranGH := false
	gh := GHCLI{
		publicRepoCache: &sync.Map{},
		runCommand: func(context.Context, ...string) ([]byte, error) {
			ranGH = true
			return nil, errors.New("should not run gh")
		},
		publicAPI: func(_ context.Context, requestURL string) ([]byte, http.Header, int, error) {
			if requestURL == "https://api.github.com/repos/owner/repo" {
				return []byte(`{"private":false}`), nil, http.StatusOK, nil
			}
			return []byte(`[{"body":"hi","created_at":"2026-07-20T12:00:00Z"}]`), newPublicHeader(""), http.StatusOK, nil
		},
	}
	comments, err := gh.ListComments(context.Background(), "owner/repo", 7)
	if err != nil {
		t.Fatalf("ListComments() error = %v", err)
	}
	if len(comments) != 1 || comments[0].Body != "hi" {
		t.Fatalf("comments = %#v", comments)
	}
	if ranGH {
		t.Fatal("ListComments() ran the gh CLI for a public repo")
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

// TestDefaultIssueFilterRequiresBothAssigneeAndAuthor pins the default filter to
// requiring that the authenticated user both authored and is assigned to an
// issue. Matching either qualifier alone would let anyone dispatch an agent by
// assigning the authenticated user an issue they filed themselves.
func TestDefaultIssueFilterRequiresBothAssigneeAndAuthor(t *testing.T) {
	if defaultIssueFilter != "assignee:@me author:@me" {
		t.Fatalf("defaultIssueFilter = %q, want both assignee:@me and author:@me", defaultIssueFilter)
	}
	query := publicIssueSearchQuery("owner/repo", defaultIssueFilter, false, "lsegal")
	for _, want := range []string{"assignee:lsegal", "author:lsegal"} {
		if !strings.Contains(query, want) {
			t.Fatalf("publicIssueSearchQuery() = %q, want it to contain %q", query, want)
		}
	}
	if strings.Contains(query, "@me") {
		t.Fatalf("publicIssueSearchQuery() = %q, want every @me qualifier substituted", query)
	}
}

// TestListIssuesSearchesOnceWithBothQualifiers guards against reintroducing the
// two-search union that treated the default filter as an OR of assignee and
// author (see issue #371): a single search must carry both qualifiers.
func TestListIssuesSearchesOnceWithBothQualifiers(t *testing.T) {
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
	gh.Filter = defaultIssueFilter
	if _, err := gh.ListIssues(context.Background(), "owner/repo"); err != nil {
		t.Fatalf("ListIssues() error = %v", err)
	}
	if len(queries) != 1 {
		t.Fatalf("issue searches = %#v, want exactly one search carrying both qualifiers", queries)
	}
	if !strings.Contains(queries[0], "assignee:@me") || !strings.Contains(queries[0], "author:@me") {
		t.Fatalf("search query = %q, want both assignee:@me and author:@me", queries[0])
	}
}
