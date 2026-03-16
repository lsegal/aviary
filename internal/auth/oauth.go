package auth

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Anthropic OAuth constants for the claude.ai consumer OAuth flow (Pro/Max plans).
// Authorize goes to claude.ai; token exchange and redirect URI go to platform.claude.com.
// Client ID, URLs, and scope sourced from the official claude-code CLI (v2.1.63).
const (
	AnthropicClientID     = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	AnthropicAuthorizeURL = "https://claude.ai/oauth/authorize"
	AnthropicTokenURL     = "https://platform.claude.com/v1/oauth/token"
	AnthropicRedirectURI  = "https://platform.claude.com/oauth/code/callback"
	AnthropicScope        = "user:profile user:inference"
)

// OpenAI / Codex OAuth constants.
// Sourced from the official openai/codex CLI (codex-rs/login/src/server.rs and
// codex-rs/core/src/auth.rs). CLIENT_ID and issuer are stable public values.
const (
	OpenAIClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	OpenAIIssuer       = "https://auth.openai.com"
	OpenAIAuthorizeURL = "https://auth.openai.com/oauth/authorize"
	OpenAITokenURL     = "https://auth.openai.com/oauth/token"
	OpenAIScope        = "openid profile email offline_access"
	OpenAICallbackPort = 1455
	// openAIOriginator is the originator header value used by the official CLI.
	openAIOriginator = "codex_cli_rs"
)

// GitHub OAuth endpoints used for PKCE login. The client ID must be supplied
// by setting the GITHUB_OAUTH_CLIENT_ID environment variable.
const (
	GitHubAuthorizeURL = "https://github.com/login/oauth/authorize"
	GitHubTokenURL     = "https://github.com/login/oauth/access_token"
	GitHubCallbackPort = 1466
)

// PKCEParams holds the PKCE code verifier and its SHA-256 challenge.
type PKCEParams struct {
	Verifier  string
	Challenge string
}

// OAuthToken holds the result of a successful OAuth token exchange.
type OAuthToken struct {
	AccessToken  string `json:"access"`
	RefreshToken string `json:"refresh"`
	ExpiresAt    int64  `json:"expires_at"` // Unix milliseconds
}

// IsExpired reports whether the token has expired (within a 30-second buffer).
func (t *OAuthToken) IsExpired() bool {
	return time.Now().UnixMilli() >= t.ExpiresAt-30_000
}

// pendingPKCE stores in-progress PKCE verifiers keyed by provider name.
var (
	pendingMu   sync.Mutex
	pendingPKCE = map[string]PKCEParams{}
)

// StorePendingPKCE saves a PKCE state for a named login flow.
func StorePendingPKCE(provider string, p PKCEParams) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	pendingPKCE[provider] = p
}

// LoadPendingPKCE retrieves and removes the stored PKCE state.
// Returns false if none is found.
func LoadPendingPKCE(provider string) (PKCEParams, bool) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	p, ok := pendingPKCE[provider]
	if ok {
		delete(pendingPKCE, provider)
	}
	return p, ok
}

// GeneratePKCE creates new random PKCE parameters.
func GeneratePKCE() (PKCEParams, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return PKCEParams{}, fmt.Errorf("generating PKCE verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return PKCEParams{Verifier: verifier, Challenge: challenge}, nil
}

// AnthropicBuildAuthorizeURL builds the claude.ai OAuth authorization URL.
// The redirect_uri points to platform.claude.com/oauth/code/callback, which
// displays the authorization code on-screen so the user can copy-paste it back.
func AnthropicBuildAuthorizeURL(pkce PKCEParams, mode string) string {
	_ = mode // reserved for future differentiation
	u, _ := url.Parse(AnthropicAuthorizeURL)
	q := u.Query()
	q.Set("client_id", AnthropicClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", AnthropicRedirectURI)
	q.Set("scope", AnthropicScope)
	q.Set("code_challenge", pkce.Challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", pkce.Verifier) // Anthropic uses state = verifier
	u.RawQuery = q.Encode()
	return u.String()
}

// AnthropicExchange exchanges an authorization code for OAuth tokens.
// verifier must match the one used to build the authorize URL.
//
// The code returned by Anthropic's callback page may be in "code#state" format;
// both parts are forwarded to the token endpoint as required.
func AnthropicExchange(ctx context.Context, code, verifier string) (*OAuthToken, error) {
	// Anthropic may return the code as "CODE#STATESUFFIX".
	codePart, statePart, _ := splitOnce(code, '#')

	body := map[string]any{
		"code":          codePart,
		"grant_type":    "authorization_code",
		"client_id":     AnthropicClientID,
		"redirect_uri":  AnthropicRedirectURI,
		"code_verifier": verifier,
	}
	if statePart != "" {
		body["state"] = statePart
	}

	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, AnthropicTokenURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic token exchange: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic token exchange failed: %s", resp.Status)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding anthropic token response: %w", err)
	}
	return &OAuthToken{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    time.Now().UnixMilli() + int64(result.ExpiresIn)*1000,
	}, nil
}

// AnthropicRefresh refreshes an expired Anthropic OAuth access token.
func AnthropicRefresh(ctx context.Context, refreshToken string) (*OAuthToken, error) {
	body := map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     AnthropicClientID,
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, AnthropicTokenURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic token refresh: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic token refresh failed: %s", resp.Status)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding anthropic refresh response: %w", err)
	}
	return &OAuthToken{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    time.Now().UnixMilli() + int64(result.ExpiresIn)*1000,
	}, nil
}

// OpenAILogin starts a local HTTP callback server on port 1455, opens the browser
// to the OpenAI/Codex OAuth consent screen, and returns tokens after the user
// approves. Blocks until ctx is cancelled or the callback is received.
//
// The flow matches the official openai/codex CLI (codex-rs/login/src/server.rs):
//   - redirect_uri uses "localhost" (not 127.0.0.1) and path "/auth/callback"
//   - authorize URL includes id_token_add_organizations, codex_cli_simplified_flow, originator
//   - state is a separate random value independent of the PKCE verifier
//   - after code exchange the id_token is swapped for a real OpenAI API key
func OpenAILogin(ctx context.Context) (*OAuthToken, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}

	// Generate an independent random state (Codex does not reuse the PKCE verifier as state).
	stateBuf := make([]byte, 16)
	if _, err := rand.Read(stateBuf); err != nil {
		return nil, fmt.Errorf("generating OAuth state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBuf)

	// Codex uses "localhost" (not 127.0.0.1) and the path "/auth/callback".
	redirectURI := fmt.Sprintf("http://localhost:%d/auth/callback", OpenAICallbackPort)

	// Listen on all loopback interfaces so both 127.0.0.1 and ::1 work, but
	// bind only to the loopback to avoid exposing the callback server.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", OpenAICallbackPort))
	if err != nil {
		return nil, fmt.Errorf("starting OAuth callback server on port %d: %w", OpenAICallbackPort, err)
	}

	type callbackResult struct {
		code  string
		state string
	}
	codeCh := make(chan callbackResult, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}
	// Codex registers the callback at /auth/callback.
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			desc := r.URL.Query().Get("error_description")
			http.Error(w, "OAuth error: "+errParam, http.StatusBadRequest)
			select {
			case errCh <- fmt.Errorf("OAuth error %s: %s", errParam, desc):
			default:
			}
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			select {
			case errCh <- fmt.Errorf("no code in OAuth callback"):
			default:
			}
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintln(w, `<html><body><h2>Authorization successful!</h2><p>You may close this tab.</p><script>window.close()</script></body></html>`)
		select {
		case codeCh <- callbackResult{code: code, state: r.URL.Query().Get("state")}:
		default:
		}
	})

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			select {
			case errCh <- err:
			default:
			}
		}
	}()
	defer srv.Shutdown(context.Background()) //nolint:errcheck

	// Build the authorize URL exactly as Codex does (server.rs build_authorize_url).
	u, _ := url.Parse(OpenAIAuthorizeURL)
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", OpenAIClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", OpenAIScope)
	q.Set("code_challenge", pkce.Challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("id_token_add_organizations", "true")
	q.Set("codex_cli_simplified_flow", "true")
	q.Set("state", state)
	q.Set("originator", openAIOriginator)
	u.RawQuery = q.Encode()

	if err := OpenBrowser(u.String()); err != nil {
		return nil, fmt.Errorf("opening browser: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case cb := <-codeCh:
		return openAIExchange(ctx, cb.code, pkce.Verifier, redirectURI)
	}
}

func openAIExchange(ctx context.Context, code, verifier, redirectURI string) (*OAuthToken, error) {
	body := url.Values{}
	body.Set("grant_type", "authorization_code")
	body.Set("client_id", OpenAIClientID)
	body.Set("code", code)
	body.Set("code_verifier", verifier)
	body.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OpenAITokenURL, bytes.NewBufferString(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai token exchange: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai token exchange failed: %s", resp.Status)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding openai token response: %w", err)
	}

	return &OAuthToken{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    time.Now().UnixMilli() + int64(result.ExpiresIn)*1000,
	}, nil
}

// OpenAIRefresh refreshes an expired OpenAI/Codex OAuth access token.
func OpenAIRefresh(ctx context.Context, refreshToken string) (*OAuthToken, error) {
	body := url.Values{}
	body.Set("grant_type", "refresh_token")
	body.Set("client_id", OpenAIClientID)
	body.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OpenAITokenURL, bytes.NewBufferString(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai token refresh: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai token refresh failed: %s", resp.Status)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding openai refresh response: %w", err)
	}
	if result.RefreshToken == "" {
		result.RefreshToken = refreshToken
	}

	return &OAuthToken{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    time.Now().UnixMilli() + int64(result.ExpiresIn)*1000,
	}, nil
}

// GitHubLogin performs a GitHub OAuth PKCE login. Requires GITHUB_OAUTH_CLIENT_ID
// to be set; falls back to reading the gh CLI token from hosts.yml if not set.
func GitHubLogin(ctx context.Context) (*OAuthToken, error) {
	clientID := os.Getenv("GITHUB_OAUTH_CLIENT_ID")
	if clientID == "" {
		// Fall back to reading the GitHub CLI/gh token from hosts.yml so users
		// who authenticated with the official `gh` tool (or extensions) don't
		// need to provide a client id. This mirrors typical Copilot/CLI
		// behavior which reuses existing gh credentials when available.
		if t, err := readGHHostsToken(); err == nil {
			return t, nil
		}
		return nil, fmt.Errorf("GITHUB_OAUTH_CLIENT_ID not set; cannot perform GitHub OAuth PKCE login")
	}

	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}

	// Listen on loopback only.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", GitHubCallbackPort))
	if err != nil {
		return nil, fmt.Errorf("starting OAuth callback server on port %d: %w", GitHubCallbackPort, err)
	}

	type cb struct{ code string }
	codeCh := make(chan cb, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			http.Error(w, "OAuth error: "+errParam, http.StatusBadRequest)
			select {
			case errCh <- fmt.Errorf("oauth error: %s", errParam):
			default:
			}
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			select {
			case errCh <- fmt.Errorf("no code in callback"):
			default:
			}
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintln(w, `<html><body><h2>Authorization successful!</h2><p>You may close this tab.</p><script>window.close()</script></body></html>`)
		select {
		case codeCh <- cb{code: code}:
		default:
		}
	})

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			select {
			case errCh <- err:
			default:
			}
		}
	}()
	defer srv.Shutdown(context.Background()) //nolint:errcheck

	// Build authorize URL
	u, _ := url.Parse(GitHubAuthorizeURL)
	q := u.Query()
	q.Set("client_id", clientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", fmt.Sprintf("http://localhost:%d/auth/callback", GitHubCallbackPort))
	q.Set("scope", "read:user user:email")
	q.Set("code_challenge", pkce.Challenge)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()

	if err := OpenBrowser(u.String()); err != nil {
		return nil, fmt.Errorf("opening browser for GitHub OAuth: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case e := <-errCh:
		return nil, e
	case cbres := <-codeCh:
		// Exchange code for token
		return GitHubExchange(ctx, cbres.code, pkce.Verifier, fmt.Sprintf("http://localhost:%d/auth/callback", GitHubCallbackPort), clientID)
	}
}

// readGHHostsToken attempts to parse the GitHub CLI hosts.yml file for a
// `github.com` entry with an `oauth_token` value and returns it as an
// OAuthToken with a long expiry. This implements a minimal, robust parser
// that avoids adding a YAML dependency.
func readGHHostsToken() (*OAuthToken, error) {
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	if home == "" {
		return nil, fmt.Errorf("cannot determine home directory")
	}
	path := filepath.Join(home, ".config", "gh", "hosts.yml")
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck

	scanner := bufio.NewScanner(f)
	inGitHubSection := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "github.com:" {
			inGitHubSection = true
			continue
		}
		if inGitHubSection {
			if line == "" {
				// blank line ends section
				inGitHubSection = false
				continue
			}
			// look for `oauth_token: <token>`
			if strings.HasPrefix(line, "oauth_token:") {
				tok := strings.TrimSpace(strings.TrimPrefix(line, "oauth_token:"))
				tok = strings.Trim(tok, "'\"")
				if tok != "" {
					return &OAuthToken{AccessToken: tok, RefreshToken: "", ExpiresAt: time.Now().Add(10 * 365 * 24 * time.Hour).UnixMilli()}, nil
				}
			}
			// If we encounter a non-indented line it's probably a new top-level key.
			if !strings.HasPrefix(scanner.Text(), " ") && !strings.HasPrefix(scanner.Text(), "\t") {
				inGitHubSection = false
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no github.com oauth_token found in %s", path)
}

// GitHubExchange exchanges an authorization code for a GitHub OAuth token.
func GitHubExchange(ctx context.Context, code, verifier, redirectURI, clientID string) (*OAuthToken, error) {
	// GitHub expects form-encoded POST
	body := url.Values{}
	body.Set("client_id", clientID)
	body.Set("code", code)
	body.Set("code_verifier", verifier)
	body.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GitHubTokenURL, bytes.NewBufferString(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github token exchange: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github token exchange failed: %s", resp.Status)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding github token response: %w", err)
	}

	// GitHub tokens do not have a refresh token in this flow; set a long expiry.
	return &OAuthToken{AccessToken: result.AccessToken, RefreshToken: "", ExpiresAt: time.Now().Add(10 * 365 * 24 * time.Hour).UnixMilli()}, nil
}

// Gemini OAuth constants for the gemini-cli OAuth flow.
// Client ID, secret, and redirect port sourced from the official google-gemini/gemini-cli
// (packages/core/src/code_assist/oauth2.ts).
const (
	GeminiClientID     = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
	GeminiClientSecret = "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"
	GeminiAuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	GeminiTokenURL     = "https://oauth2.googleapis.com/token"
	GeminiScope        = "https://www.googleapis.com/auth/cloud-platform openid email profile"
	GeminiCallbackPort = 45289
)

// GeminiLogin starts a local HTTP callback server on port 45289, opens the browser
// to Google's OAuth consent screen, and returns tokens after the user approves.
// Matches the google-gemini/gemini-cli authentication flow.
func GeminiLogin(ctx context.Context) (*OAuthToken, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}

	stateBuf := make([]byte, 16)
	if _, err := rand.Read(stateBuf); err != nil {
		return nil, fmt.Errorf("generating OAuth state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBuf)

	redirectURI := fmt.Sprintf("http://localhost:%d", GeminiCallbackPort)

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", GeminiCallbackPort))
	if err != nil {
		return nil, fmt.Errorf("starting OAuth callback server on port %d: %w", GeminiCallbackPort, err)
	}

	type callbackResult struct {
		code  string
		state string
	}
	codeCh := make(chan callbackResult, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			desc := r.URL.Query().Get("error_description")
			http.Error(w, "OAuth error: "+errParam, http.StatusBadRequest)
			select {
			case errCh <- fmt.Errorf("OAuth error %s: %s", errParam, desc):
			default:
			}
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			select {
			case errCh <- fmt.Errorf("no code in OAuth callback"):
			default:
			}
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintln(w, `<html><body><h2>Authorization successful!</h2><p>You may close this tab.</p><script>window.close()</script></body></html>`)
		select {
		case codeCh <- callbackResult{code: code, state: r.URL.Query().Get("state")}:
		default:
		}
	})

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			select {
			case errCh <- err:
			default:
			}
		}
	}()
	defer srv.Shutdown(context.Background()) //nolint:errcheck

	u, _ := url.Parse(GeminiAuthorizeURL)
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", GeminiClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", GeminiScope)
	q.Set("code_challenge", pkce.Challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("access_type", "offline") // request refresh token
	q.Set("prompt", "consent")      // force consent to always get refresh token
	u.RawQuery = q.Encode()

	if err := OpenBrowser(u.String()); err != nil {
		return nil, fmt.Errorf("opening browser: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case cb := <-codeCh:
		return geminiExchange(ctx, cb.code, pkce.Verifier, redirectURI)
	}
}

func geminiExchange(ctx context.Context, code, verifier, redirectURI string) (*OAuthToken, error) {
	body := url.Values{}
	body.Set("grant_type", "authorization_code")
	body.Set("client_id", GeminiClientID)
	body.Set("client_secret", GeminiClientSecret)
	body.Set("code", code)
	body.Set("code_verifier", verifier)
	body.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GeminiTokenURL, bytes.NewBufferString(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini token exchange: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini token exchange failed: %s", resp.Status)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding gemini token response: %w", err)
	}
	return &OAuthToken{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    time.Now().UnixMilli() + int64(result.ExpiresIn)*1000,
	}, nil
}

// GeminiRefresh refreshes an expired Gemini OAuth access token.
func GeminiRefresh(ctx context.Context, refreshToken string) (*OAuthToken, error) {
	body := url.Values{}
	body.Set("grant_type", "refresh_token")
	body.Set("client_id", GeminiClientID)
	body.Set("client_secret", GeminiClientSecret)
	body.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GeminiTokenURL, bytes.NewBufferString(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini token refresh: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini token refresh failed: %s", resp.Status)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding gemini refresh response: %w", err)
	}
	// Google may not return a new refresh token; reuse the old one.
	if result.RefreshToken == "" {
		result.RefreshToken = refreshToken
	}
	return &OAuthToken{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    time.Now().UnixMilli() + int64(result.ExpiresIn)*1000,
	}, nil
}

// GitHub Copilot OAuth constants. The client ID is the public GitHub Copilot
// VS Code extension OAuth app, widely used by third-party Copilot integrations.
const (
	CopilotClientID       = "Iv1.b507a08c87ecfe98"
	CopilotDeviceCodeURL  = "https://github.com/login/device/code"
	CopilotDeviceTokenURL = "https://github.com/login/oauth/access_token"
	CopilotDeviceScope    = "read:user"
)

// CopilotDeviceState holds the in-flight state for a GitHub Copilot device flow.
type CopilotDeviceState struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        int
	ExpiresIn       int
}

var (
	copilotDeviceMu      sync.Mutex
	copilotDevicePending *CopilotDeviceState
)

// StoreCopilotDeviceState saves in-flight device state for a two-step flow.
func StoreCopilotDeviceState(s *CopilotDeviceState) {
	copilotDeviceMu.Lock()
	defer copilotDeviceMu.Unlock()
	copilotDevicePending = s
}

// LoadCopilotDeviceState retrieves and clears the stored device state.
// Returns false if none is stored.
func LoadCopilotDeviceState() (*CopilotDeviceState, bool) {
	copilotDeviceMu.Lock()
	defer copilotDeviceMu.Unlock()
	s := copilotDevicePending
	copilotDevicePending = nil
	return s, s != nil
}

// CopilotDeviceCode requests a device code from GitHub and returns the state
// that should be displayed to the user (user_code + verification_uri).
func CopilotDeviceCode(ctx context.Context) (*CopilotDeviceState, error) {
	body := url.Values{}
	body.Set("client_id", CopilotClientID)
	body.Set("scope", CopilotDeviceScope)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, CopilotDeviceCodeURL, bytes.NewBufferString(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilot device code: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	var dc struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&dc); err != nil {
		return nil, fmt.Errorf("copilot device code: decoding response: %w", err)
	}
	if dc.DeviceCode == "" {
		return nil, fmt.Errorf("copilot device code: empty device_code in response")
	}
	if dc.Interval <= 0 {
		dc.Interval = 5
	}
	return &CopilotDeviceState{
		DeviceCode:      dc.DeviceCode,
		UserCode:        dc.UserCode,
		VerificationURI: dc.VerificationURI,
		Interval:        dc.Interval,
		ExpiresIn:       dc.ExpiresIn,
	}, nil
}

// CopilotPollDevice polls GitHub until the user authorizes the device flow.
func CopilotPollDevice(ctx context.Context, state *CopilotDeviceState) (*OAuthToken, error) {
	pollBody := url.Values{}
	pollBody.Set("client_id", CopilotClientID)
	pollBody.Set("device_code", state.DeviceCode)
	pollBody.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	interval := state.Interval
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()
	deadline := time.Now().Add(time.Duration(state.ExpiresIn) * time.Second)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("copilot device flow: timed out waiting for authorization")
			}
			preq, err := http.NewRequestWithContext(ctx, http.MethodPost, CopilotDeviceTokenURL, bytes.NewBufferString(pollBody.Encode()))
			if err != nil {
				return nil, err
			}
			preq.Header.Set("Accept", "application/json")
			preq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			presp, err := http.DefaultClient.Do(preq)
			if err != nil {
				return nil, fmt.Errorf("copilot device poll: %w", err)
			}
			var result struct {
				AccessToken string `json:"access_token"`
				Error       string `json:"error"`
			}
			decodeErr := json.NewDecoder(presp.Body).Decode(&result)
			_ = presp.Body.Close()
			if decodeErr != nil {
				return nil, fmt.Errorf("copilot device poll: decoding response: %w", decodeErr)
			}
			switch result.Error {
			case "":
				if result.AccessToken == "" {
					return nil, fmt.Errorf("copilot device poll: empty access_token")
				}
				return &OAuthToken{
					AccessToken: result.AccessToken,
					ExpiresAt:   time.Now().Add(10 * 365 * 24 * time.Hour).UnixMilli(),
				}, nil
			case "authorization_pending":
				continue
			case "slow_down":
				interval += 5
				ticker.Reset(time.Duration(interval) * time.Second)
				continue
			case "expired_token":
				return nil, fmt.Errorf("copilot device flow: device code expired; try again")
			default:
				return nil, fmt.Errorf("copilot device flow: %s", result.Error)
			}
		}
	}
}

// CopilotLogin is the CLI convenience wrapper: runs the full device flow in one
// call, printing the user code and verification URL to stdout.
func CopilotLogin(ctx context.Context) (*OAuthToken, error) {
	state, err := CopilotDeviceCode(ctx)
	if err != nil {
		return nil, err
	}
	fmt.Printf("\nTo authorize GitHub Copilot, visit: %s\n", state.VerificationURI)
	fmt.Printf("Enter code: %s\n\n", state.UserCode)
	return CopilotPollDevice(ctx, state)
}

// CopilotTokenExchange exchanges a GitHub user token (PAT or OAuth) for a
// short-lived GitHub Copilot API token. The Copilot token is valid for ~30 min.
// Returns (copilotToken, expiresAt, error).
func CopilotTokenExchange(ctx context.Context, ghToken string) (string, time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/copilot_internal/v2/token", nil)
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Authorization", "Bearer "+ghToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Editor-Version", "aviary/1.0")
	req.Header.Set("Copilot-Integration-Id", "aviary")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("copilot token exchange: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", time.Time{}, fmt.Errorf("copilot token exchange: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var result struct {
		Token     string          `json:"token"`
		ExpiresAt json.RawMessage `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", time.Time{}, fmt.Errorf("copilot token exchange: decoding response: %w", err)
	}
	var expiry time.Time
	if len(result.ExpiresAt) > 0 {
		// API may return a Unix timestamp (number) or an RFC3339 string.
		var ts int64
		if err := json.Unmarshal(result.ExpiresAt, &ts); err == nil {
			expiry = time.Unix(ts, 0)
		} else {
			var s string
			if err := json.Unmarshal(result.ExpiresAt, &s); err == nil {
				expiry, _ = time.Parse(time.RFC3339, s)
			}
		}
	}
	if expiry.IsZero() {
		expiry = time.Now().Add(30 * time.Minute)
	}
	return result.Token, expiry, nil
}

// OpenBrowser opens rawURL in the system default browser.
func OpenBrowser(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// cmd.exe interprets & in OAuth URLs as a command separator, so
		// bypass the shell entirely via rundll32.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	case "darwin":
		cmd = exec.Command("open", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}

// splitOnce splits s on the first occurrence of sep, returning (before, after, true).
// If sep is not found it returns (s, "", false).
func splitOnce(s string, sep byte) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}
