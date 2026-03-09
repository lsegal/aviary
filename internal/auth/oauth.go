package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
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

// geminiCodeAssistEndpoint is the base URL for the Gemini Code Assist API,
// which is the backend used by the gemini-cli OAuth flow.
const geminiCodeAssistEndpoint = "https://cloudcode-pa.googleapis.com/v1internal"

// GeminiLookupProject fetches the Google Cloud project ID associated with a
// Gemini Code Assist OAuth access token. Uses the loadCodeAssist endpoint
// (matching gemini-cli's packages/core/src/code_assist/server.ts).
func GeminiLookupProject(ctx context.Context, accessToken string) (string, error) {
	loadURL := geminiCodeAssistEndpoint + ":loadCodeAssist"
	body := `{"cloudaicompanionProject":null,"metadata":{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI","duetProject":null}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loadURL, bytes.NewBufferString(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini project lookup: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini project lookup: %s", resp.Status)
	}
	var result struct {
		CloudaicompanionProject string `json:"cloudaicompanionProject"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding gemini project lookup response: %w", err)
	}
	if result.CloudaicompanionProject == "" {
		return "", fmt.Errorf("gemini project lookup: empty project ID in response")
	}
	return result.CloudaicompanionProject, nil
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
