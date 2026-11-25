package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withoutBrowser makes OpenBrowser fail by leaving no browser launcher on PATH.
func withoutBrowser(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// withFakeBrowser installs a browser launcher on PATH that records the URL it
// was asked to open, and returns the file it records into.
func withFakeBrowser(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake browser launcher is a shell script")
	}
	dir := t.TempDir()
	out := filepath.Join(t.TempDir(), "opened-url")
	script := "#!/bin/sh\nprintf '%s' \"$1\" > \"$AVIARY_TEST_BROWSER_OUT\"\n"
	for _, name := range []string{"open", "xdg-open"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755))
	}
	t.Setenv("PATH", dir)
	t.Setenv("AVIARY_TEST_BROWSER_OUT", out)
	return out
}

func readOpenedURL(t *testing.T, out string) string {
	t.Helper()
	var opened string
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(out)
		opened = string(data)
		return err == nil && opened != ""
	}, 5*time.Second, 10*time.Millisecond)
	return opened
}

// requireFreePort skips when a fixed OAuth callback port is already taken on
// this machine (for example by a running Aviary or Codex login).
func requireFreePort(t *testing.T, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Skipf("port %d is in use: %v", port, err)
	}
	_ = ln.Close()
}

// releaseBrowserLoginOnCleanup cancels any pending flow for provider and waits
// for its callback listener to be torn down so later tests can bind the port.
func releaseBrowserLoginOnCleanup(t *testing.T, provider string, port int) {
	t.Helper()
	t.Cleanup(func() {
		cancelPendingBrowserLogin(provider)
		assert.Eventually(t, func() bool {
			ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				return false
			}
			_ = ln.Close()
			return true
		}, 5*time.Second, 10*time.Millisecond)
	})
}

// hitCallback sends a browser-style redirect to a local OAuth callback server,
// bypassing the patched http.DefaultClient.
func hitCallback(t *testing.T, rawURL string) (int, string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	var resp *http.Response
	require.Eventually(t, func() bool {
		var err error
		resp, err = client.Get(rawURL)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// pkceTokenServer serves a token endpoint that only issues a token when the
// posted code and verifier match the authorize URL's code_challenge.
type pkceTokenServer struct {
	mu        sync.Mutex
	challenge string
	form      url.Values
}

func (s *pkceTokenServer) setChallenge(c string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.challenge = c
}

func (s *pkceTokenServer) postedForm() url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.form
}

func (s *pkceTokenServer) handler(code string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		s.mu.Lock()
		s.form = r.PostForm
		challenge := s.challenge
		s.mu.Unlock()
		if r.PostForm.Get("code") != code || s256(r.PostForm.Get("code_verifier")) != challenge {
			http.Error(w, "bad verifier", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-" + code,
			"refresh_token": "refresh-" + code,
			"expires_in":    3600,
		})
	}
}

func TestStartOpenAILogin_CompletesThroughCallback(t *testing.T) {
	requireFreePort(t, OpenAICallbackPort)
	opened := withFakeBrowser(t)
	tokens := &pkceTokenServer{}
	srv := httptest.NewServer(tokens.handler("openai-code"))
	defer srv.Close()
	patchDefaultClient(t, srv)
	releaseBrowserLoginOnCleanup(t, "openai", OpenAICallbackPort)

	start, err := StartOpenAILogin()
	require.NoError(t, err)
	assert.True(t, start.BrowserOpened)
	assert.Empty(t, start.BrowserOpenErr)
	assert.Equal(t, "http://localhost:1455/auth/callback", start.CallbackURL)
	assert.WithinDuration(t, time.Now().Add(BrowserLoginTimeout), start.ExpiresAt, 5*time.Second)
	assert.Equal(t, start.AuthorizeURL, readOpenedURL(t, opened))

	u, err := url.Parse(start.AuthorizeURL)
	require.NoError(t, err)
	q := u.Query()
	assert.Equal(t, "auth.openai.com", u.Host)
	assert.Equal(t, OpenAIClientID, q.Get("client_id"))
	assert.Equal(t, start.CallbackURL, q.Get("redirect_uri"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.Equal(t, "true", q.Get("codex_cli_simplified_flow"))
	assert.Equal(t, "codex_cli_rs", q.Get("originator"))
	state := q.Get("state")
	require.NotEmpty(t, state)
	tokens.setChallenge(q.Get("code_challenge"))

	status, body := hitCallback(t, fmt.Sprintf("http://127.0.0.1:%d/auth/callback?code=openai-code&state=%s", OpenAICallbackPort, url.QueryEscape(state)))
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, "Authorization successful")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tok, err := CompleteOpenAILogin(ctx)
	require.NoError(t, err)
	assert.Equal(t, "access-openai-code", tok.AccessToken)
	assert.Equal(t, "refresh-openai-code", tok.RefreshToken)
	form := tokens.postedForm()
	assert.Equal(t, "authorization_code", form.Get("grant_type"))
	assert.Equal(t, start.CallbackURL, form.Get("redirect_uri"))

	_, err = CompleteOpenAILogin(ctx)
	assert.ErrorContains(t, err, "no pending OpenAI login")
}

func TestStartOpenAILogin_ProviderErrorFailsFlow(t *testing.T) {
	requireFreePort(t, OpenAICallbackPort)
	withoutBrowser(t)
	releaseBrowserLoginOnCleanup(t, "openai", OpenAICallbackPort)

	start, err := StartOpenAILogin()
	require.NoError(t, err)
	assert.False(t, start.BrowserOpened)
	assert.NotEmpty(t, start.BrowserOpenErr)

	u, err := url.Parse(start.AuthorizeURL)
	require.NoError(t, err)
	state := u.Query().Get("state")
	status, _ := hitCallback(t, fmt.Sprintf("http://127.0.0.1:%d/auth/callback?error=access_denied&error_description=nope&state=%s", OpenAICallbackPort, url.QueryEscape(state)))
	assert.Equal(t, http.StatusBadRequest, status)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = CompleteOpenAILogin(ctx)
	assert.EqualError(t, err, "OAuth error access_denied: nope")
}

func TestStartOpenAILogin_NewAttemptReplacesPendingFlow(t *testing.T) {
	requireFreePort(t, OpenAICallbackPort)
	withoutBrowser(t)
	releaseBrowserLoginOnCleanup(t, "openai", OpenAICallbackPort)

	first, err := StartOpenAILogin()
	require.NoError(t, err)
	firstFlow, ok := getPendingBrowserLogin("openai")
	require.True(t, ok)

	// The second attempt must tear down the first listener to rebind the port.
	second, err := StartOpenAILogin()
	require.NoError(t, err)
	assert.NotEqual(t, first.AuthorizeURL, second.AuthorizeURL)

	_, err = firstFlow.wait(context.Background())
	assert.EqualError(t, err, "openai login was replaced by a new attempt")

	secondFlow, ok := getPendingBrowserLogin("openai")
	require.True(t, ok)
	assert.NotSame(t, firstFlow, secondFlow)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = secondFlow.wait(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestStartGeminiLogin_CompletesThroughCallback(t *testing.T) {
	requireFreePort(t, GeminiCallbackPort)
	withoutBrowser(t)
	tokens := &pkceTokenServer{}
	srv := httptest.NewServer(tokens.handler("gemini-code"))
	defer srv.Close()
	patchDefaultClient(t, srv)
	releaseBrowserLoginOnCleanup(t, "gemini", GeminiCallbackPort)

	start, err := StartGeminiLogin()
	require.NoError(t, err)
	assert.False(t, start.BrowserOpened)
	assert.NotEmpty(t, start.BrowserOpenErr)
	assert.Equal(t, "http://localhost:45289", start.CallbackURL)

	u, err := url.Parse(start.AuthorizeURL)
	require.NoError(t, err)
	q := u.Query()
	assert.Equal(t, "accounts.google.com", u.Host)
	assert.Equal(t, GeminiClientID, q.Get("client_id"))
	assert.Equal(t, "offline", q.Get("access_type"))
	assert.Equal(t, "consent", q.Get("prompt"))
	tokens.setChallenge(q.Get("code_challenge"))

	status, body := hitCallback(t, fmt.Sprintf("http://127.0.0.1:%d/?code=gemini-code&state=%s", GeminiCallbackPort, url.QueryEscape(q.Get("state"))))
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, "Authorization successful")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tok, err := CompleteGeminiLogin(ctx)
	require.NoError(t, err)
	assert.Equal(t, "access-gemini-code", tok.AccessToken)
	assert.Equal(t, GeminiClientSecret, tokens.postedForm().Get("client_secret"))

	_, err = CompleteGeminiLogin(ctx)
	assert.ErrorContains(t, err, "no pending Gemini login")
}

func TestStartGeminiLogin_FailedExchangeFailsFlow(t *testing.T) {
	requireFreePort(t, GeminiCallbackPort)
	withoutBrowser(t)
	tokens := &pkceTokenServer{}
	srv := httptest.NewServer(tokens.handler("expected-code"))
	defer srv.Close()
	patchDefaultClient(t, srv)
	releaseBrowserLoginOnCleanup(t, "gemini", GeminiCallbackPort)

	start, err := StartGeminiLogin()
	require.NoError(t, err)
	u, err := url.Parse(start.AuthorizeURL)
	require.NoError(t, err)
	tokens.setChallenge(u.Query().Get("code_challenge"))

	hitCallback(t, fmt.Sprintf("http://127.0.0.1:%d/?code=other-code&state=%s", GeminiCallbackPort, url.QueryEscape(u.Query().Get("state"))))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = CompleteGeminiLogin(ctx)
	assert.ErrorContains(t, err, "gemini token exchange failed: 400")
}

func TestBrowserLoginListenHost(t *testing.T) {
	t.Setenv("AVIARY_SANDBOX", "")
	assert.Equal(t, "127.0.0.1", browserLoginListenHost())
	t.Setenv("AVIARY_SANDBOX", "true")
	assert.Equal(t, "0.0.0.0", browserLoginListenHost())
}

func TestPendingBrowserLogin_ResultLifecycle(t *testing.T) {
	t.Run("first result wins", func(t *testing.T) {
		flow := &pendingBrowserLogin{done: make(chan struct{})}
		flow.setResult(&OAuthToken{AccessToken: "first"}, nil)
		flow.setResult(nil, errors.New("late failure"))
		tok, err := flow.wait(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "first", tok.AccessToken)
	})

	t.Run("finished without token", func(t *testing.T) {
		flow := &pendingBrowserLogin{done: make(chan struct{})}
		flow.setResult(nil, nil)
		_, err := flow.wait(context.Background())
		assert.EqualError(t, err, "login finished without a token")
	})

	t.Run("shutdown stops once and tolerates nil", func(t *testing.T) {
		var nilFlow *pendingBrowserLogin
		nilFlow.shutdown()
		(&pendingBrowserLogin{}).shutdown()

		stops := 0
		flow := &pendingBrowserLogin{stop: func() { stops++ }}
		flow.shutdown()
		flow.shutdown()
		assert.Equal(t, 1, stops)
	})

	t.Run("storing a new flow fails the old one", func(t *testing.T) {
		const provider = "test-provider"
		old := &pendingBrowserLogin{done: make(chan struct{})}
		next := &pendingBrowserLogin{done: make(chan struct{})}
		storePendingBrowserLogin(provider, old)
		storePendingBrowserLogin(provider, next)
		t.Cleanup(func() { clearPendingBrowserLogin(provider, next) })

		_, err := old.wait(context.Background())
		assert.EqualError(t, err, "test-provider login was replaced by a new attempt")
		got, ok := getPendingBrowserLogin(provider)
		require.True(t, ok)
		assert.Same(t, next, got)

		// Clearing a stale flow must not remove the current one.
		clearPendingBrowserLogin(provider, old)
		got, ok = getPendingBrowserLogin(provider)
		assert.True(t, ok)
		assert.Same(t, next, got)
		clearPendingBrowserLogin(provider, next)
		_, ok = getPendingBrowserLogin(provider)
		assert.False(t, ok)
	})
}

func TestOpenBrowser_PassesURLVerbatim(t *testing.T) {
	out := withFakeBrowser(t)
	target := "https://example.com/oauth?client_id=a&state=b c"
	require.NoError(t, OpenBrowser(target))
	assert.Equal(t, target, readOpenedURL(t, out))
}

func TestOpenBrowser_MissingLauncher(t *testing.T) {
	withoutBrowser(t)
	assert.Error(t, OpenBrowser("https://example.com"))
}

func writeGHHosts(t *testing.T, home, content string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "gh")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hosts.yml"), []byte(content), 0o600))
}

func TestReadGHHostsToken(t *testing.T) {
	tests := []struct {
		name    string
		hosts   string
		want    string
		wantErr string
	}{
		{
			name:  "quoted token in github.com section",
			hosts: "github.com:\n    user: octo\n    oauth_token: \"gho_quoted\"\n",
			want:  "gho_quoted",
		},
		{
			name:  "skips other hosts",
			hosts: "ghe.example.com:\n    oauth_token: ghe_token\ngithub.com:\n    oauth_token: gho_real\n",
			want:  "gho_real",
		},
		{
			name:    "blank line ends the github.com section",
			hosts:   "github.com:\n    user: octo\n\n    oauth_token: gho_orphan\n",
			wantErr: "no github.com oauth_token found",
		},
		{
			name:    "top-level key ends the github.com section",
			hosts:   "github.com:\n    user: octo\nother:\n    oauth_token: gho_other\n",
			wantErr: "no github.com oauth_token found",
		},
		{
			name:    "empty token is ignored",
			hosts:   "github.com:\n    oauth_token: ''\n",
			wantErr: "no github.com oauth_token found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			writeGHHosts(t, home, tt.hosts)
			tok, err := readGHHostsToken()
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, tok.AccessToken)
			assert.False(t, tok.IsExpired())
		})
	}

	t.Run("falls back to USERPROFILE", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", "")
		t.Setenv("USERPROFILE", home)
		writeGHHosts(t, home, "github.com:\n  oauth_token: gho_windows\n")
		tok, err := readGHHostsToken()
		require.NoError(t, err)
		assert.Equal(t, "gho_windows", tok.AccessToken)
	})

	t.Run("no home directory", func(t *testing.T) {
		t.Setenv("HOME", "")
		t.Setenv("USERPROFILE", "")
		_, err := readGHHostsToken()
		assert.EqualError(t, err, "cannot determine home directory")
	})

	t.Run("missing hosts file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		_, err := readGHHostsToken()
		assert.True(t, errors.Is(err, os.ErrNotExist))
	})
}

func TestGitHubLogin_WithoutClientIDUsesGHToken(t *testing.T) {
	t.Setenv("GITHUB_OAUTH_CLIENT_ID", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := GitHubLogin(context.Background())
	assert.ErrorContains(t, err, "GITHUB_OAUTH_CLIENT_ID not set")

	writeGHHosts(t, home, "github.com:\n    oauth_token: gho_from_gh\n")
	tok, err := GitHubLogin(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "gho_from_gh", tok.AccessToken)
}

func TestGitHubLogin_PKCEFlow(t *testing.T) {
	requireFreePort(t, GitHubCallbackPort)
	t.Setenv("GITHUB_OAUTH_CLIENT_ID", "test-client")

	var mu sync.Mutex
	var posted url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		posted = r.PostForm
		mu.Unlock()
		assert.Equal(t, "/login/oauth/access_token", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "gho_pkce", "token_type": "bearer"})
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)

	run := func(t *testing.T, ctx context.Context, query string) (*OAuthToken, error) {
		t.Helper()
		type result struct {
			tok *OAuthToken
			err error
		}
		done := make(chan result, 1)
		go func() {
			tok, err := GitHubLogin(ctx)
			done <- result{tok, err}
		}()
		if query != "" {
			hitCallback(t, fmt.Sprintf("http://127.0.0.1:%d/auth/callback%s", GitHubCallbackPort, query))
		}
		select {
		case r := <-done:
			return r.tok, r.err
		case <-time.After(10 * time.Second):
			t.Fatal("GitHubLogin did not return")
			return nil, nil
		}
	}

	t.Run("exchanges the callback code", func(t *testing.T) {
		opened := withFakeBrowser(t)
		tok, err := run(t, context.Background(), "?code=gh-code")
		require.NoError(t, err)
		assert.Equal(t, "gho_pkce", tok.AccessToken)
		assert.Empty(t, tok.RefreshToken)

		u, err := url.Parse(readOpenedURL(t, opened))
		require.NoError(t, err)
		q := u.Query()
		assert.Equal(t, "test-client", q.Get("client_id"))
		assert.Equal(t, "read:user user:email", q.Get("scope"))

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, "gh-code", posted.Get("code"))
		assert.Equal(t, "test-client", posted.Get("client_id"))
		assert.Equal(t, q.Get("code_challenge"), s256(posted.Get("code_verifier")))
		assert.Equal(t, q.Get("redirect_uri"), posted.Get("redirect_uri"))
	})

	t.Run("provider error", func(t *testing.T) {
		withFakeBrowser(t)
		_, err := run(t, context.Background(), "?error=access_denied")
		assert.EqualError(t, err, "oauth error: access_denied")
	})

	t.Run("callback without code", func(t *testing.T) {
		withFakeBrowser(t)
		_, err := run(t, context.Background(), "?state=x")
		assert.EqualError(t, err, "no code in callback")
	})

	t.Run("context cancelled while waiting", func(t *testing.T) {
		withFakeBrowser(t)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, err := run(t, ctx, "")
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("callback port in use", func(t *testing.T) {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", GitHubCallbackPort))
		require.NoError(t, err)
		defer ln.Close() //nolint:errcheck
		_, err = run(t, context.Background(), "")
		assert.ErrorContains(t, err, "starting OAuth callback server on port 1466")
	})

	t.Run("browser cannot be opened", func(t *testing.T) {
		// Runs last: GitHubLogin can return before its server starts tracking
		// the listener, so the callback port may stay bound afterwards.
		withoutBrowser(t)
		_, err := run(t, context.Background(), "")
		assert.ErrorContains(t, err, "opening browser for GitHub OAuth")
	})
}

func TestGitHubExchange_Errors(t *testing.T) {
	t.Run("http error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "nope", http.StatusUnauthorized)
		}))
		defer srv.Close()
		patchDefaultClient(t, srv)
		_, err := GitHubExchange(context.Background(), "c", "v", "http://localhost/cb", "id")
		assert.ErrorContains(t, err, "github token exchange failed: 401")
	})
}

// TestTokenEndpoints_RejectMalformedResponses checks that every token endpoint
// reports undecodable bodies and unreachable servers instead of returning an
// empty token.
func TestTokenEndpoints_RejectMalformedResponses(t *testing.T) {
	ctx := context.Background()
	calls := map[string]func() error{
		"anthropic exchange": func() error { _, err := AnthropicExchange(ctx, "c", "v"); return err },
		"anthropic refresh":  func() error { _, err := AnthropicRefresh(ctx, "r"); return err },
		"openai exchange":    func() error { _, err := openAIExchange(ctx, "c", "v", "http://localhost/cb"); return err },
		"openai refresh":     func() error { _, err := OpenAIRefresh(ctx, "r"); return err },
		"gemini exchange":    func() error { _, err := geminiExchange(ctx, "c", "v", "http://localhost/cb"); return err },
		"gemini refresh":     func() error { _, err := GeminiRefresh(ctx, "r"); return err },
		"github exchange":    func() error { _, err := GitHubExchange(ctx, "c", "v", "http://localhost/cb", "id"); return err },
		"copilot device":     func() error { _, err := CopilotDeviceCode(ctx); return err },
		"copilot token": func() error {
			_, _, err := CopilotTokenExchange(ctx, "gho")
			return err
		},
	}

	t.Run("malformed JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "{not json")
		}))
		defer srv.Close()
		patchDefaultClient(t, srv)
		for name, call := range calls {
			assert.ErrorContains(t, call(), "decoding", name)
		}
	})

	t.Run("unreachable server", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		patchDefaultClient(t, srv)
		srv.Close()
		for name, call := range calls {
			assert.ErrorContains(t, call(), "connect", name)
		}
	})
}

func TestAnthropicRefresh_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "expired", http.StatusBadRequest)
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)
	_, err := AnthropicRefresh(context.Background(), "old")
	assert.ErrorContains(t, err, "anthropic token refresh failed: 400")
}

func TestCopilotDeviceState_StoreAndLoad(t *testing.T) {
	_, ok := LoadCopilotDeviceState()
	assert.False(t, ok)

	want := &CopilotDeviceState{DeviceCode: "dev", UserCode: "ABCD-1234"}
	StoreCopilotDeviceState(want)
	got, ok := LoadCopilotDeviceState()
	require.True(t, ok)
	assert.Same(t, want, got)

	_, ok = LoadCopilotDeviceState()
	assert.False(t, ok, "load must clear the stored state")
}

func TestCopilotDeviceCode(t *testing.T) {
	t.Run("defaults missing interval", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			assert.Equal(t, "/login/device/code", r.URL.Path)
			assert.Equal(t, CopilotClientID, r.PostForm.Get("client_id"))
			assert.Equal(t, CopilotDeviceScope, r.PostForm.Get("scope"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "dev-1",
				"user_code":        "WXYZ-0000",
				"verification_uri": "https://github.com/login/device",
				"expires_in":       900,
			})
		}))
		defer srv.Close()
		patchDefaultClient(t, srv)

		state, err := CopilotDeviceCode(context.Background())
		require.NoError(t, err)
		assert.Equal(t, &CopilotDeviceState{
			DeviceCode:      "dev-1",
			UserCode:        "WXYZ-0000",
			VerificationURI: "https://github.com/login/device",
			Interval:        5,
			ExpiresIn:       900,
		}, state)
	})

	t.Run("empty device code", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"error":"unauthorized_client"}`)
		}))
		defer srv.Close()
		patchDefaultClient(t, srv)
		_, err := CopilotDeviceCode(context.Background())
		assert.EqualError(t, err, "copilot device code: empty device_code in response")
	})
}

func TestCopilotPollDevice(t *testing.T) {
	var mu sync.Mutex
	polls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		device := r.PostForm.Get("device_code")
		assert.Equal(t, "urn:ietf:params:oauth:grant-type:device_code", r.PostForm.Get("grant_type"))
		mu.Lock()
		polls[device]++
		n := polls[device]
		mu.Unlock()
		switch device {
		case "pending-then-ok":
			if n == 1 {
				_, _ = io.WriteString(w, `{"error":"authorization_pending"}`)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"gho_device"}`)
		case "expired":
			_, _ = io.WriteString(w, `{"error":"expired_token"}`)
		case "denied":
			_, _ = io.WriteString(w, `{"error":"access_denied"}`)
		case "empty":
			_, _ = io.WriteString(w, `{}`)
		case "malformed":
			_, _ = io.WriteString(w, `{`)
		}
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)

	poll := func(device string, expiresIn int) (*OAuthToken, error) {
		return CopilotPollDevice(context.Background(), &CopilotDeviceState{DeviceCode: device, Interval: 1, ExpiresIn: expiresIn})
	}

	t.Run("group", func(t *testing.T) {
		t.Run("pending then authorized", func(t *testing.T) {
			t.Parallel()
			tok, err := poll("pending-then-ok", 60)
			require.NoError(t, err)
			assert.Equal(t, "gho_device", tok.AccessToken)
			assert.False(t, tok.IsExpired())
			mu.Lock()
			defer mu.Unlock()
			assert.Equal(t, 2, polls["pending-then-ok"])
		})
		t.Run("expired code", func(t *testing.T) {
			t.Parallel()
			_, err := poll("expired", 60)
			assert.EqualError(t, err, "copilot device flow: device code expired; try again")
		})
		t.Run("other error", func(t *testing.T) {
			t.Parallel()
			_, err := poll("denied", 60)
			assert.EqualError(t, err, "copilot device flow: access_denied")
		})
		t.Run("empty token", func(t *testing.T) {
			t.Parallel()
			_, err := poll("empty", 60)
			assert.EqualError(t, err, "copilot device poll: empty access_token")
		})
		t.Run("malformed response", func(t *testing.T) {
			t.Parallel()
			_, err := poll("malformed", 60)
			assert.ErrorContains(t, err, "copilot device poll: decoding response")
		})
		t.Run("deadline passed", func(t *testing.T) {
			t.Parallel()
			_, err := poll("never-polled", 0)
			assert.EqualError(t, err, "copilot device flow: timed out waiting for authorization")
			mu.Lock()
			defer mu.Unlock()
			assert.Zero(t, polls["never-polled"])
		})
		t.Run("context cancelled", func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := CopilotPollDevice(ctx, &CopilotDeviceState{DeviceCode: "x", Interval: 1, ExpiresIn: 60})
			assert.ErrorIs(t, err, context.Canceled)
		})
	})
}

func TestCopilotLogin_RunsDeviceFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/device/code":
			_, _ = io.WriteString(w, `{"device_code":"dev","user_code":"CODE-1","verification_uri":"https://github.com/login/device","interval":1,"expires_in":60}`)
		case "/login/oauth/access_token":
			_ = r.ParseForm()
			assert.Equal(t, "dev", r.PostForm.Get("device_code"))
			_, _ = io.WriteString(w, `{"access_token":"gho_login"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)

	tok, err := CopilotLogin(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "gho_login", tok.AccessToken)

	t.Run("device code failure", func(t *testing.T) {
		bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{}`)
		}))
		defer bad.Close()
		patchDefaultClient(t, bad)
		_, err := CopilotLogin(context.Background())
		assert.ErrorContains(t, err, "empty device_code")
	})
}

func TestCopilotTokenExchange(t *testing.T) {
	expiry := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		name      string
		body      string
		wantToken string
		check     func(t *testing.T, got time.Time)
	}{
		{
			name:      "unix expiry",
			body:      fmt.Sprintf(`{"token":"cop-1","expires_at":%d}`, expiry.Unix()),
			wantToken: "cop-1",
			check:     func(t *testing.T, got time.Time) { assert.True(t, got.Equal(expiry)) },
		},
		{
			name:      "RFC3339 expiry",
			body:      `{"token":"cop-2","expires_at":"` + expiry.Format(time.RFC3339) + `"}`,
			wantToken: "cop-2",
			check:     func(t *testing.T, got time.Time) { assert.True(t, got.Equal(expiry)) },
		},
		{
			name:      "missing expiry defaults to thirty minutes",
			body:      `{"token":"cop-3"}`,
			wantToken: "cop-3",
			check: func(t *testing.T, got time.Time) {
				assert.WithinDuration(t, time.Now().Add(30*time.Minute), got, time.Minute)
			},
		},
		{
			name:      "unparseable expiry defaults to thirty minutes",
			body:      `{"token":"cop-4","expires_at":true}`,
			wantToken: "cop-4",
			check: func(t *testing.T, got time.Time) {
				assert.WithinDuration(t, time.Now().Add(30*time.Minute), got, time.Minute)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/copilot_internal/v2/token", r.URL.Path)
				assert.Equal(t, "Bearer gho_user", r.Header.Get("Authorization"))
				assert.Equal(t, "aviary", r.Header.Get("Copilot-Integration-Id"))
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()
			patchDefaultClient(t, srv)

			tok, exp, err := CopilotTokenExchange(context.Background(), "gho_user")
			require.NoError(t, err)
			assert.Equal(t, tt.wantToken, tok)
			tt.check(t, exp)
		})
	}

	t.Run("http error includes body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "  not entitled  ", http.StatusForbidden)
		}))
		defer srv.Close()
		patchDefaultClient(t, srv)
		_, _, err := CopilotTokenExchange(context.Background(), "gho_user")
		require.Error(t, err)
		assert.True(t, strings.HasSuffix(err.Error(), "403 Forbidden: not entitled"), err.Error())
	})
}
