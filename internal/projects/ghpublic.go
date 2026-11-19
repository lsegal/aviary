package projects

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// publicAPIDoer performs a single unauthenticated GET against the public
// GitHub API. It is overridable in tests via GHCLI.publicAPI.
type publicAPIDoer func(ctx context.Context, requestURL string) (body []byte, header http.Header, status int, err error)

// publicHTTPClient bounds each public API request so an unreachable network
// can never stall polling indefinitely.
var publicHTTPClient = &http.Client{Timeout: 15 * time.Second}

// doPublicGitHubRequest is the real, network-hitting implementation of
// publicAPIDoer.
func doPublicGitHubRequest(ctx context.Context, requestURL string) ([]byte, http.Header, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := publicHTTPClient.Do(req)
	if err != nil {
		return nil, nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, 0, err
	}
	return body, resp.Header, resp.StatusCode, nil
}

// publicGET issues one request through g.publicAPI, falling back to the real
// network implementation when no override is configured.
func (g GHCLI) publicGET(ctx context.Context, requestURL string) ([]byte, http.Header, int, error) {
	if g.publicAPI != nil {
		return g.publicAPI(ctx, requestURL)
	}
	return doPublicGitHubRequest(ctx, requestURL)
}

// isPublicRepo reports whether repo is a public GitHub repository, using the
// unauthenticated public API so the check itself never spends the
// authenticated token's rate limit. The result is cached for the lifetime of
// g's cache (shared across copies of GHCLI via the pointer field) since a
// repository's visibility rarely changes during a single aviary run.
//
// The whole public-API path is gated on publicRepoCache being non-nil: only
// main() sets it, so this never probes the network (or changes behavior) for
// a GHCLI built directly, such as in tests that supply their own runCommand
// mock and never opt into the public API.
func (g GHCLI) isPublicRepo(ctx context.Context, repo string) bool {
	if repo == "" || g.publicRepoCache == nil {
		return false
	}
	if cached, ok := g.publicRepoCache.Load(repo); ok {
		return cached.(bool)
	}
	public := g.probePublicRepo(ctx, repo)
	g.publicRepoCache.Store(repo, public)
	return public
}

func (g GHCLI) probePublicRepo(ctx context.Context, repo string) bool {
	body, _, status, err := g.publicGET(ctx, "https://api.github.com/repos/"+repo)
	if err != nil || status != http.StatusOK {
		return false
	}
	var data struct {
		Private bool `json:"private"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return false
	}
	return !data.Private
}

// errListReadOnePage reports a list endpoint read through a single-page
// helper. GitHub returns only the first page (30 items by default) of a list,
// so such a read silently drops everything after it; a list endpoint has to go
// through apiGETPaginated instead.
var errListReadOnePage = errors.New("list endpoint read through a single-page helper; use apiGETPaginated")

// rejectListBody returns errListReadOnePage when body is a JSON array.
func rejectListBody(path string, body []byte) error {
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '[' {
		return fmt.Errorf("%s: %w", path, errListReadOnePage)
	}
	return nil
}

// apiGETObject fetches a single-object GitHub REST API path (relative, e.g.
// "repos/owner/repo/issues/1"), preferring the unauthenticated public API
// when public is true so polling a public repository doesn't spend the
// authenticated token's rate limit. It falls back to `gh api` whenever the
// public request itself fails, so a transient network blip or the public
// API's own (much lower) rate limit never breaks polling outright.
//
// It reads one page only, so it refuses a list (JSON array) response with
// errListReadOnePage rather than return a truncated one: list endpoints go
// through apiGETPaginated.
func (g GHCLI) apiGETObject(ctx context.Context, public bool, path string, extraGHArgs ...string) ([]byte, error) {
	if public {
		body, _, status, err := g.publicGET(ctx, "https://api.github.com/"+path)
		if err == nil && status >= 200 && status < 300 {
			return body, rejectListBody(path, body)
		}
	}
	output, err := g.run(ctx, append([]string{"api", path}, extraGHArgs...)...)
	if err != nil {
		return output, err
	}
	return output, rejectListBody(path, output)
}

// apiReachable reports whether a GET of path succeeds, without returning its
// body. Probes such as "are stacks enabled" only need the status, so they may
// name a list endpoint without paginating it. A failure's error carries the
// response text, such as "HTTP 404".
func (g GHCLI) apiReachable(ctx context.Context, public bool, path string) error {
	if public {
		_, _, status, err := g.publicGET(ctx, "https://api.github.com/"+path)
		if err == nil && status >= 200 && status < 300 {
			return nil
		}
	}
	output, err := g.run(ctx, "api", path)
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// dependencyIssueView returns the `{"state": ...}` JSON for a dependency
// issue, preferring the unauthenticated public REST API when public is true
// and falling back to the authenticated REST issue endpoint (via `gh api`,
// never `gh issue view`, which queries GraphQL) otherwise.
func (g GHCLI) dependencyIssueView(ctx context.Context, public bool, repo string, number int) ([]byte, error) {
	if public {
		body, _, status, err := g.publicGET(ctx, "https://api.github.com/repos/"+repo+"/issues/"+strconv.Itoa(number))
		if err == nil && status >= 200 && status < 300 {
			return body, nil
		}
	}
	return g.run(ctx, "api", "repos/"+repo+"/issues/"+strconv.Itoa(number))
}

// apiGETPaginated mirrors `gh api PATH --paginate`: it follows RFC 5988
// Link: rel="next" headers and concatenates each page's JSON array into one.
// extraGHArgs are passed only to the `gh api` fallback, as with apiGETObject.
func (g GHCLI) apiGETPaginated(ctx context.Context, public bool, path string, extraGHArgs ...string) ([]byte, error) {
	ghArgs := append([]string{"api", path, "--paginate"}, extraGHArgs...)
	if !public {
		return g.run(ctx, ghArgs...)
	}
	requestURL := "https://api.github.com/" + path
	var all []json.RawMessage
	for requestURL != "" {
		body, header, status, err := g.publicGET(ctx, requestURL)
		if err != nil || status < 200 || status >= 300 {
			return g.run(ctx, ghArgs...)
		}
		var page []json.RawMessage
		if err := json.Unmarshal(body, &page); err != nil {
			return g.run(ctx, ghArgs...)
		}
		all = append(all, page...)
		requestURL = nextPageURL(header.Get("Link"))
	}
	return json.Marshal(all)
}

// nextPageURL extracts the rel="next" target from a GitHub Link header, or
// "" once there are no further pages.
func nextPageURL(link string) string {
	for _, part := range strings.Split(link, ",") {
		segments := strings.Split(strings.TrimSpace(part), ";")
		if len(segments) < 2 {
			continue
		}
		if strings.TrimSpace(segments[1]) != `rel="next"` {
			continue
		}
		return strings.Trim(strings.TrimSpace(segments[0]), "<>")
	}
	return ""
}

// resolveSelfLogin returns the authenticated gh user's login, caching it for
// the lifetime of g's cache. It is only needed to translate the "@me"
// search qualifier, which has no meaning to an unauthenticated request.
func (g GHCLI) resolveSelfLogin(ctx context.Context) (string, error) {
	if g.selfLoginCache != nil {
		if cached, ok := g.selfLoginCache.Load("login"); ok {
			return cached.(string), nil
		}
	}
	output, err := g.run(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return "", fmt.Errorf("resolve authenticated user: %w: %s", err, strings.TrimSpace(string(output)))
	}
	login := strings.TrimSpace(string(output))
	if g.selfLoginCache != nil {
		g.selfLoginCache.Store("login", login)
	}
	return login, nil
}

func issueFilterTerms(filter string) []string { return core.IssueFilterTerms(filter) }

func issueSearchTerms(filter string, allIssues bool) []string {
	return core.IssueSearchTerms(filter, allIssues)
}

// publicIssueSearchQuery builds the GitHub search-syntax query equivalent to
// issueListArgs, substituting the "@me" search qualifier (meaningless to an
// unauthenticated request) with the resolved login.
func publicIssueSearchQuery(repo, filter string, allIssues bool, selfLogin string) string {
	if !allIssues && selfLogin != "" {
		filter = strings.ReplaceAll(filter, "author:@me", "author:"+selfLogin)
		filter = strings.ReplaceAll(filter, "assignee:@me", "assignee:"+selfLogin)
	}
	terms := append([]string{"repo:" + repo}, issueSearchTerms(filter, allIssues)...)
	return strings.Join(terms, " ")
}

type searchIssueResult struct {
	Number      int          `json:"number"`
	Title       string       `json:"title"`
	Body        string       `json:"body"`
	State       string       `json:"state"`
	CreatedAt   time.Time    `json:"created_at"`
	Labels      []IssueLabel `json:"labels"`
	PullRequest *struct{}    `json:"pull_request,omitempty"`
}

type searchIssuesPage struct {
	Items             []searchIssueResult `json:"items"`
	IncompleteResults bool                `json:"incomplete_results"`
}

// issueFromSearchResult converts one Search API result into an Issue. ok is
// false for a pull request, since the search endpoint returns both.
func issueFromSearchResult(item searchIssueResult) (issue Issue, ok bool) {
	if item.PullRequest != nil {
		return Issue{}, false
	}
	return Issue{
		Number:    item.Number,
		Title:     item.Title,
		Body:      item.Body,
		State:     item.State,
		CreatedAt: item.CreatedAt,
		Labels:    item.Labels,
	}, true
}

// listPublicIssues lists open issues in repo via the unauthenticated Search
// API. It returns ok=false whenever the public request didn't succeed, so
// the caller can fall back to listAuthenticatedIssues.
func (g GHCLI) listPublicIssues(ctx context.Context, repo, filter string, allIssues bool) (issues []Issue, ok bool) {
	selfLogin := ""
	if !allIssues && (strings.Contains(filter, "author:@me") || strings.Contains(filter, "assignee:@me")) {
		login, err := g.resolveSelfLogin(ctx)
		if err != nil {
			return nil, false
		}
		selfLogin = login
	}
	query := publicIssueSearchQuery(repo, filter, allIssues, selfLogin)
	requestURL := "https://api.github.com/search/issues?" + url.Values{"q": {query}, "per_page": {"100"}}.Encode()
	for requestURL != "" {
		body, header, status, err := g.publicGET(ctx, requestURL)
		if err != nil || status < 200 || status >= 300 {
			return nil, false
		}
		var page searchIssuesPage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, false
		}
		if page.IncompleteResults {
			// GitHub's search backend returns 200 with incomplete_results:true
			// when its own index lookup times out, silently omitting issues
			// rather than erroring. Treat that the same as a failed request.
			return nil, false
		}
		for _, item := range page.Items {
			issue, ok := issueFromSearchResult(item)
			if !ok {
				continue
			}
			issues = append(issues, issue)
			if len(issues) == 1000 {
				return issues, true
			}
		}
		requestURL = nextPageURL(header.Get("Link"))
	}
	return issues, true
}

// listAuthenticatedIssues lists open issues in repo via the REST Search API,
// authenticated through `gh api` rather than `gh issue list` (which queries
// GraphQL). It is the private-repo/public-API-unavailable counterpart to
// listPublicIssues, and the only path aviary's core polling loop takes for a
// private repository.
func (g GHCLI) listAuthenticatedIssues(ctx context.Context, repo, filter string, allIssues bool) ([]Issue, error) {
	query := publicIssueSearchQuery(repo, filter, allIssues, "")
	output, err := g.run(ctx, "api", "search/issues", "-X", "GET", "-f", "q="+query, "-f", "per_page=100", "--paginate")
	if err != nil {
		return nil, fmt.Errorf("list issues: %w: %s", err, strings.TrimSpace(string(output)))
	}
	var issues []Issue
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var page searchIssuesPage
		if err := decoder.Decode(&page); err != nil {
			if err == io.EOF {
				return issues, nil
			}
			return nil, fmt.Errorf("decode issues: %w", err)
		}
		if page.IncompleteResults {
			return nil, fmt.Errorf("list issues: search API returned incomplete results")
		}
		for _, item := range page.Items {
			issue, ok := issueFromSearchResult(item)
			if !ok {
				continue
			}
			issues = append(issues, issue)
			if len(issues) == 1000 {
				return issues, nil
			}
		}
	}
}
