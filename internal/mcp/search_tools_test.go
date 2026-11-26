package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/browser"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
)

// fakeSearchBrowser is a searchBrowser that records calls and returns canned
// results or errors at each step.
type fakeSearchBrowser struct {
	openErr     error
	navigateErr error
	evalErr     error
	evalResult  string

	opened    []string
	navigated []string
	evaluated []string
	closed    []string
}

func (f *fakeSearchBrowser) Open(_ context.Context, url string) (string, error) {
	f.opened = append(f.opened, url)
	if f.openErr != nil {
		return "", f.openErr
	}
	return "tab-1", nil
}

func (f *fakeSearchBrowser) Navigate(_ context.Context, tabID, url string) error {
	f.navigated = append(f.navigated, tabID+" "+url)
	return f.navigateErr
}

func (f *fakeSearchBrowser) EvalJS(_ context.Context, _ string, expr string) (string, error) {
	f.evaluated = append(f.evaluated, expr)
	return f.evalResult, f.evalErr
}

func (f *fakeSearchBrowser) CloseTab(tabID string) error {
	f.closed = append(f.closed, tabID)
	return nil
}

func TestBrowserSearch_ReturnsParsedResults(t *testing.T) {
	br := &fakeSearchBrowser{evalResult: `[{"title":"Go","url":"https://go.dev","description":"The Go language"}]`}

	results, err := browserSearch(context.Background(), br, "go & testing", 7)
	require.NoError(t, err)
	assert.Equal(t, []searchResult{{Title: "Go", URL: "https://go.dev", Description: "The Go language"}}, results)

	assert.Equal(t, []string{"about:blank"}, br.opened)
	assert.Equal(t, []string{"tab-1 https://www.google.com/search?q=go+%26+testing"}, br.navigated)
	require.Len(t, br.evaluated, 1)
	assert.Contains(t, br.evaluated[0], "results.length >= 7")
	assert.Equal(t, []string{"tab-1"}, br.closed)
}

func TestBrowserSearch_Errors(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name      string
		br        *fakeSearchBrowser
		wantErr   string
		wantClose bool
	}{
		{name: "open", br: &fakeSearchBrowser{openErr: boom}, wantErr: "opening tab: boom"},
		{name: "navigate", br: &fakeSearchBrowser{navigateErr: boom}, wantErr: "navigating to google: boom", wantClose: true},
		{name: "eval", br: &fakeSearchBrowser{evalErr: boom}, wantErr: "extracting results: boom", wantClose: true},
		{name: "malformed JSON", br: &fakeSearchBrowser{evalResult: "not json"}, wantErr: `parsing search results (raw="not json")`, wantClose: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := browserSearch(context.Background(), tt.br, "q", 3)
			require.Error(t, err)
			assert.Nil(t, results)
			assert.Contains(t, err.Error(), tt.wantErr)
			if tt.wantClose {
				assert.Equal(t, []string{"tab-1"}, tt.br.closed, "tab must be closed after a failure")
			} else {
				assert.Empty(t, tt.br.closed, "no tab was opened, so none should be closed")
			}
		})
	}
}

func TestWebSearchBrowser_UsesDepsBrowser(t *testing.T) {
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })

	SetDeps(&Deps{})
	assert.Nil(t, webSearchBrowser())

	mgr := browser.NewManager("", 0, "", true)
	SetDeps(&Deps{Browser: mgr})
	assert.Same(t, mgr, webSearchBrowser())
}

// setupWebSearch isolates config and auth in a temp dir, stores the given
// Brave credential (when non-empty), writes the config, and installs br as the
// fallback browser.
func setupWebSearch(t *testing.T, braveRef, braveKey string, br searchBrowser) *Dispatcher {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	require.NoError(t, store.EnsureDirs())

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })
	prevBrowser := webSearchBrowser
	t.Cleanup(func() { webSearchBrowser = prevBrowser })
	webSearchBrowser = func() searchBrowser { return br }

	as, err := auth.NewFileStore(filepath.Join(base, "aviary", "auth", "credentials.json"))
	require.NoError(t, err)
	if braveKey != "" {
		require.NoError(t, as.Set("brave_api_key", braveKey))
	}
	require.NoError(t, config.Save("", &config.Config{
		Search: config.SearchConfig{Web: config.WebSearchConfig{BraveAPIKey: braveRef}},
	}))
	SetDeps(&Deps{Auth: as})
	return NewDispatcher("https://localhost:16677", "")
}

// stubBrave routes Brave API requests to a local server that responds with
// status and body.
func stubBrave(t *testing.T, status int, body string) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	origClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = origClient })
	http.DefaultClient = &http.Client{Transport: &redirectTransport{from: "https://api.search.brave.com", to: ts.URL}}
}

func decodeSearchResults(t *testing.T, out string) []searchResult {
	t.Helper()
	var results []searchResult
	require.NoError(t, json.Unmarshal([]byte(out), &results), out)
	return results
}

func TestWebSearchTool_FallsBackToBrowser(t *testing.T) {
	browserResults := `[{"title":"From browser","url":"https://example.com","description":"d"}]`
	tests := []struct {
		name     string
		braveRef string
		braveKey string
		status   int
		body     string
	}{
		{name: "no Brave key configured"},
		{name: "Brave error", braveRef: "auth:brave_api_key", braveKey: "k", status: http.StatusUnauthorized, body: "denied"},
		{name: "Brave no results", braveRef: "auth:brave_api_key", braveKey: "k", status: http.StatusOK, body: `{"web":{"results":[]}}`},
		{name: "Brave credential missing", braveRef: "auth:brave_api_key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			br := &fakeSearchBrowser{evalResult: browserResults}
			d := setupWebSearch(t, tt.braveRef, tt.braveKey, br)
			if tt.status != 0 {
				stubBrave(t, tt.status, tt.body)
			}

			out, err := d.CallTool(context.Background(), "web_search", map[string]any{"query": "fallback", "count": 50})
			require.NoError(t, err)
			assert.Equal(t, []searchResult{{Title: "From browser", URL: "https://example.com", Description: "d"}}, decodeSearchResults(t, out))
			require.Len(t, br.evaluated, 1)
			assert.Contains(t, br.evaluated[0], "results.length >= 20", "count is clamped to 20")
			assert.Equal(t, []string{"tab-1"}, br.closed)
		})
	}
}

func TestWebSearchTool_PrefersBraveOverBrowser(t *testing.T) {
	br := &fakeSearchBrowser{evalResult: `[]`}
	d := setupWebSearch(t, "auth:brave_api_key", "k", br)
	stubBrave(t, http.StatusOK, `{"web":{"results":[{"title":"From Brave","url":"https://brave.example","description":"b"}]}}`)

	out, err := d.CallTool(context.Background(), "web_search", map[string]any{"query": "brave"})
	require.NoError(t, err)
	assert.Equal(t, "From Brave", decodeSearchResults(t, out)[0].Title)
	assert.Empty(t, br.opened, "browser must not be used when Brave returns results")
}

func TestWebSearchTool_BrowserSearchError(t *testing.T) {
	br := &fakeSearchBrowser{navigateErr: errors.New("page load timed out")}
	d := setupWebSearch(t, "", "", br)

	toolCallContains(t, d, "web_search", map[string]any{"query": "q"}, "browser search: navigating to google: page load timed out")
	assert.Equal(t, []string{"tab-1"}, br.closed)
}

func TestConfiguredBraveAPIKey(t *testing.T) {
	t.Run("resolves auth reference", func(t *testing.T) {
		setupWebSearch(t, " auth:brave_api_key ", "secret", nil)
		key, ref, err := configuredBraveAPIKey()
		require.NoError(t, err)
		assert.Equal(t, "secret", key)
		assert.Equal(t, "auth:brave_api_key", ref)
	})

	t.Run("not configured", func(t *testing.T) {
		setupWebSearch(t, "", "secret", nil)
		key, ref, err := configuredBraveAPIKey()
		require.NoError(t, err)
		assert.Empty(t, key)
		assert.Empty(t, ref)
	})

	t.Run("missing credential", func(t *testing.T) {
		setupWebSearch(t, "auth:brave_api_key", "", nil)
		key, ref, err := configuredBraveAPIKey()
		require.Error(t, err)
		assert.Contains(t, err.Error(), `resolving "auth:brave_api_key"`)
		assert.Empty(t, key)
		assert.Equal(t, "auth:brave_api_key", ref)
	})

	t.Run("auth store error", func(t *testing.T) {
		setupWebSearch(t, "auth:brave_api_key", "", nil)
		SetDeps(&Deps{})
		// A directory where the credentials file belongs makes the store fail to open.
		credPath := store.SubDir(store.DirAuth) + "/credentials.json"
		require.NoError(t, os.MkdirAll(credPath, 0o700))
		key, ref, err := configuredBraveAPIKey()
		require.Error(t, err)
		assert.Empty(t, key)
		assert.Equal(t, "auth:brave_api_key", ref)
	})

	t.Run("config error", func(t *testing.T) {
		setupWebSearch(t, "", "", nil)
		require.NoError(t, os.WriteFile(config.DefaultPath(), []byte("{ bad: yaml: ["), 0o600))
		key, ref, err := configuredBraveAPIKey()
		require.Error(t, err)
		assert.True(t, strings.HasPrefix(err.Error(), "loading config:"), err.Error())
		assert.Empty(t, key)
		assert.Empty(t, ref)
	})
}

func TestWebSearchTool_ConfigErrorFallsBackToBrowser(t *testing.T) {
	br := &fakeSearchBrowser{evalResult: `[{"title":"T","url":"https://t.example","description":""}]`}
	d := setupWebSearch(t, "", "", br)
	require.NoError(t, os.WriteFile(config.DefaultPath(), []byte("{ bad: yaml: ["), 0o600))

	out, err := d.CallTool(context.Background(), "web_search", map[string]any{"query": "q"})
	require.NoError(t, err)
	assert.Equal(t, "T", decodeSearchResults(t, out)[0].Title)
}
