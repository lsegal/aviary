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
	AnthropicClientID    = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	AnthropicAuthorizeURL = "https://claude.ai/oauth/authorize"
	AnthropicTokenURL    = "https://platform.claude.com/v1/oauth/token"
	AnthropicRedirectURI = "https://platform.claude.com/oauth/code/callback"
	AnthropicScope       = "user:profile user:inference"
)

// OpenAI / Codex OAuth constants (from opencode codex plugin).
const (
	OpenAIClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	OpenAIAuthorizeURL = "https://auth.openai.com/oauth/authorize"
	OpenAITokenURL     = "https://auth.openai.com/oauth/token"
	OpenAIScope        = "openid profile email offline_access"
	OpenAICallbackPort = 1455
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
	defer resp.Body.Close()
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
	defer resp.Body.Close()
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
func OpenAILogin(ctx context.Context) (*OAuthToken, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}

	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", OpenAICallbackPort)

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", OpenAICallbackPort))
	if err != nil {
		return nil, fmt.Errorf("starting OAuth callback server on port %d: %w", OpenAICallbackPort, err)
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
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
		fmt.Fprintln(w, `<html><body><h2>Authorization successful!</h2><p>You may close this tab.</p><script>window.close()</script></body></html>`)
		select {
		case codeCh <- code:
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

	// Build authorize URL.
	u, _ := url.Parse(OpenAIAuthorizeURL)
	q := u.Query()
	q.Set("client_id", OpenAIClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", OpenAIScope)
	q.Set("code_challenge", pkce.Challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", pkce.Verifier)
	q.Set("codex_cli_simplified_flow", "true")
	q.Set("originator", "aviary")
	u.RawQuery = q.Encode()

	if err := OpenBrowser(u.String()); err != nil {
		return nil, fmt.Errorf("opening browser: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case code := <-codeCh:
		return openAIExchange(ctx, code, pkce.Verifier, redirectURI)
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
	defer resp.Body.Close()
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
