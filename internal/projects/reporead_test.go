package projects

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/core"
)

// fakeGH records gh calls and answers them from responses, keyed by the
// joined arguments.
type fakeGH struct {
	mu        sync.Mutex
	calls     [][]string
	responses map[string]string
	failures  map[string]string
}

func (f *fakeGH) run(_ context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, args)
	key := strings.Join(args, " ")
	if detail, ok := f.failures[key]; ok {
		return []byte(detail), errors.New("exit status 1")
	}
	if output, ok := f.responses[key]; ok {
		return []byte(output), nil
	}
	return []byte("gh: Not Found (HTTP 404)"), errors.New("exit status 1")
}

// readTestManager watches o/r, with a job in o/board, a repository of a
// watched project board, and answers gh from fake.
func readTestManager(t *testing.T, fake *fakeGH) *Manager {
	t.Helper()
	m := newTestManager(t)
	m.list = []config.ProjectConfig{{Target: "o/r"}}
	m.snapshot.Store(&core.Snapshot{
		Targets: []string{"o/r"},
		Jobs:    []core.JobSnapshot{{Number: 3, Target: "o/r", Repository: "o/board", Status: "complete", Kind: core.JobKindIssue}},
	})
	m.ghRunCommand = fake.run
	recordLogs(t, m)
	return m
}

func logLines(m *Manager) []string {
	lines, _ := m.logsSince(0, 0)
	return lines
}

func TestGitHubReadCommitByURL(t *testing.T) {
	fake := &fakeGH{responses: map[string]string{
		"api --method GET repos/o/r/commits/39cfe1e": `{"sha": "39cfe1e", "commit": {"message": "Fix parser\n\nCloses #12"}, "files": [{"filename": "parser.go"}]}`,
	}}
	m := readTestManager(t, fake)

	result, err := m.GitHubRead(context.Background(), "", "", "https://github.com/o/r/commit/39cfe1e", "")
	require.NoError(t, err)
	assert.Equal(t, "o/r", result["repository"])
	assert.Equal(t, "commits/39cfe1e", result["path"])
	data := result["data"].(map[string]any)
	assert.Equal(t, "39cfe1e", data["sha"])
	assert.Contains(t, logLines(m), "Code Loop agent read GitHub repos/o/r/commits/39cfe1e")
}

func TestGitHubReadDiffAndRawMedia(t *testing.T) {
	fake := &fakeGH{responses: map[string]string{
		"api --method GET repos/o/r/compare/v1...main -H Accept: application/vnd.github.diff":          "diff --git a/x b/x\n",
		"api --method GET repos/o/r/contents/docs/a.md?ref=main -H Accept: application/vnd.github.raw": "# Docs\n",
	}}
	m := readTestManager(t, fake)

	result, err := m.GitHubRead(context.Background(), "o/r", "compare/v1...main", "", "diff")
	require.NoError(t, err)
	assert.Equal(t, "diff --git a/x b/x\n", result["text"])

	// A contents read returns the file's text by default.
	result, err = m.GitHubRead(context.Background(), "", "", "https://github.com/o/r/blob/main/docs/a.md", "")
	require.NoError(t, err)
	assert.Equal(t, "# Docs\n", result["text"])
}

func TestGitHubReadCommentAndRunURLs(t *testing.T) {
	fake := &fakeGH{responses: map[string]string{
		"api --method GET repos/o/board/issues/comments/987": `{"id": 987, "body": "please retry"}`,
		"api --method GET repos/o/r/actions/jobs/55":         `{"id": 55, "conclusion": "success"}`,
	}}
	m := readTestManager(t, fake)

	// A job's repository is readable even when only its board is watched.
	result, err := m.GitHubRead(context.Background(), "", "", "https://github.com/o/board/issues/935#issuecomment-987", "")
	require.NoError(t, err)
	assert.Equal(t, "please retry", result["data"].(map[string]any)["body"])

	result, err = m.GitHubRead(context.Background(), "", "", "https://github.com/o/r/actions/runs/44/job/55", "")
	require.NoError(t, err)
	assert.Equal(t, "success", result["data"].(map[string]any)["conclusion"])
}

func TestGitHubReadKeepsLogTail(t *testing.T) {
	log := strings.Repeat("noise\n", readBodyLimit/6+100) + "Error: tests failed\n"
	fake := &fakeGH{responses: map[string]string{"api --method GET repos/o/r/actions/jobs/55/logs": log}}
	m := readTestManager(t, fake)

	result, err := m.GitHubRead(context.Background(), "o/r", "actions/jobs/55/logs", "", "")
	require.NoError(t, err)
	assert.Equal(t, true, result["truncated"])
	assert.True(t, strings.HasSuffix(result["text"].(string), "Error: tests failed\n"))
}

func TestGitHubReadStaysInWatchedRepositories(t *testing.T) {
	fake := &fakeGH{}
	m := readTestManager(t, fake)
	ctx := context.Background()

	_, err := m.GitHubRead(ctx, "other/repo", "commits/abc", "", "")
	assert.ErrorContains(t, err, "other/repo is not a watched repository")
	_, err = m.GitHubRead(ctx, "", "", "https://github.com/other/repo/commit/abc", "")
	assert.ErrorContains(t, err, "not a watched repository")
	for _, path := range []string{"../../user", "contents/../../../users/x", "%2e%2e/x", "commits/a b"} {
		_, err = m.GitHubRead(ctx, "o/r", path, "", "")
		assert.ErrorContains(t, err, "invalid path", path)
	}
	_, err = m.GitHubRead(ctx, "o/r", "commits/abc", "", "html")
	assert.ErrorContains(t, err, "unknown media")
	_, err = m.GitHubRead(ctx, "", "commits/abc", "", "")
	assert.ErrorContains(t, err, "name the repository")
	assert.Empty(t, fake.calls, "nothing outside the watched repositories reaches gh")
}

func TestGitHubReadReportsRateLimit(t *testing.T) {
	fake := &fakeGH{failures: map[string]string{
		"api --method GET repos/o/r/pulls/1": "gh: API rate limit exceeded for user (HTTP 403)",
	}}
	m := readTestManager(t, fake)

	_, err := m.GitHubRead(context.Background(), "o/r", "pulls/1", "", "")
	assert.ErrorContains(t, err, "rate limit is used up")
	assert.Contains(t, logLines(m), "Code Loop agent read GitHub repos/o/r/pulls/1: rate limited")
}

func TestGitHubURLPath(t *testing.T) {
	for link, want := range map[string]string{
		"https://github.com/o/r":                                       "",
		"https://github.com/o/r/commit/abc#diff-0123":                  "commits/abc",
		"https://github.com/o/r/commits/main":                          "commits?sha=main",
		"https://github.com/o/r/compare/v1.0...main":                   "compare/v1.0...main",
		"https://github.com/o/r/blob/0123abc/internal/config/agent.go": "contents/internal/config/agent.go?ref=0123abc",
		"https://github.com/o/r/tree/main/docs":                        "contents/docs?ref=main",
		"https://github.com/o/r/pull/12/":                              "pulls/12",
		"https://github.com/o/r/pull/12/files":                         "pulls/12/files",
		"https://github.com/o/r/pull/12#discussion_r77":                "pulls/comments/77",
		"https://github.com/o/r/pull/12#pullrequestreview-8":           "pulls/12/reviews/8",
		"https://github.com/o/r/pull/12#issuecomment-9":                "issues/comments/9",
		"https://www.github.com/o/r/issues/4#issuecomment-10":          "issues/comments/10",
		"https://github.com/o/r/issues/4#event-5":                      "issues/4",
		"https://github.com/o/r/actions/runs/44":                       "actions/runs/44",
		"https://github.com/o/r/actions/runs/44/job/55?pr=3#step:4:1":  "actions/jobs/55",
		"https://github.com/o/r/actions/workflows/pages.yml":           "actions/workflows/pages.yml/runs",
		"https://github.com/o/r/releases/tag/v1.2":                     "releases/tags/v1.2",
		"https://github.com/o/r/releases/latest":                       "releases/latest",
		"https://api.github.com/repos/o/r/actions/runs?branch=main":    "actions/runs?branch=main",
	} {
		repository, path, err := githubURLPath(link)
		if assert.NoError(t, err, link) {
			assert.Equal(t, "o/r", repository, link)
			assert.Equal(t, want, path, link)
		}
	}
	for _, link := range []string{"https://gitlab.com/o/r/commit/abc", "https://github.com/o", "https://github.com/o/r/wiki", "https://api.github.com/user", "not a url\x7f"} {
		_, _, err := githubURLPath(link)
		assert.Error(t, err, link)
	}
}

func TestSiteFetchReadsOnlyWatchedProjectSites(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/away":
			http.Redirect(w, r, "http://elsewhere.invalid/", http.StatusFound)
		default:
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Last-Modified", "Tue, 24 Nov 2026 20:00:00 GMT")
			_, _ = w.Write([]byte("<h1>v1.2 is live</h1>"))
		}
	}))
	t.Cleanup(site.Close)
	fake := &fakeGH{responses: map[string]string{
		"api repos/o/r --jq .homepage // \"\"":                           site.URL + "\n",
		"api repos/o/r/pages --jq (.html_url // \"\"), (.cname // \"\")": "http://docs.example.invalid/\ndocs.example.invalid\n",
		"api repos/o/board --jq .homepage // \"\"":                       "\n",
	}}
	m := readTestManager(t, fake)
	m.siteClient = site.Client()
	ctx := context.Background()

	result, err := m.SiteFetch(ctx, site.URL+"/changelog")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, result["status"])
	assert.Equal(t, "<h1>v1.2 is live</h1>", result["body"])
	assert.Equal(t, "Tue, 24 Nov 2026 20:00:00 GMT", result["last_modified"])
	assert.Contains(t, logLines(m), "Code Loop agent fetched "+site.URL+"/changelog")

	_, err = m.SiteFetch(ctx, "https://example.com/")
	assert.ErrorContains(t, err, "example.com is not a watched project's site")
	_, err = m.SiteFetch(ctx, site.URL+"/away")
	assert.ErrorContains(t, err, "redirected off the project's sites")
	_, err = m.SiteFetch(ctx, "file:///etc/passwd")
	assert.ErrorContains(t, err, "give an http or https URL")

	// The hosts are read once and reused.
	count := 0
	for _, args := range fake.calls {
		if strings.Join(args, " ") == "api repos/o/r --jq .homepage // \"\"" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestSiteHostAllowed(t *testing.T) {
	hosts := []string{"aviary.bot", "o.github.io"}
	assert.True(t, siteHostAllowed(hosts, "aviary.bot"))
	assert.True(t, siteHostAllowed(hosts, "WWW.aviary.bot"))
	assert.True(t, siteHostAllowed(hosts, "o.github.io"))
	assert.False(t, siteHostAllowed(hosts, "evil-aviary.bot"))
	assert.False(t, siteHostAllowed(hosts, "aviary.bot.evil.com"))
}

func TestGitHubReadResolvesTheRepositoryToRead(t *testing.T) {
	fake := &fakeGH{responses: map[string]string{
		"api --method GET repos/o/r/commits/abc": `{"sha": "abc"}`,
	}}
	m := readTestManager(t, fake)
	ctx := context.Background()

	_, err := m.GitHubRead(ctx, "", "commits/abc", "https://github.com/o/r/commit/abc", "")
	assert.ErrorContains(t, err, "pass a url or a path, not both")
	_, err = m.GitHubRead(ctx, "", "", "https://example.com/o/r/commit/abc", "")
	assert.Error(t, err)
	_, err = m.GitHubRead(ctx, "o/board", "", "https://github.com/o/r/commit/abc", "")
	assert.ErrorContains(t, err, "the url is in o/r, not o/board")
	assert.Empty(t, fake.calls, "a refused read reaches gh")

	// With one watched repository, an unnamed one is that repository.
	m.snapshot.Store(&core.Snapshot{Targets: []string{"o/r"}})
	result, err := m.GitHubRead(ctx, "", "commits/abc", "", "")
	require.NoError(t, err)
	assert.Equal(t, "o/r", result["repository"])

	m.list = nil
	m.snapshot.Store(&core.Snapshot{})
	_, err = m.GitHubRead(ctx, "", "commits/abc", "", "")
	assert.ErrorContains(t, err, "no repositories are watched")
}
