package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestErrNotFoundAndIsNotFound(t *testing.T) {
	err := &ErrNotFound{Key: "k1"}
	if err.Error() != `credential "k1" not found` {
		t.Fatalf("unexpected error string: %s", err.Error())
	}
	if !IsNotFound(err) {
		t.Fatal("IsNotFound should return true for ErrNotFound")
	}
	if IsNotFound(errors.New("other")) {
		t.Fatal("IsNotFound should return false for non-ErrNotFound")
	}
}

type mockStore struct {
	data map[string]string
}

func (m *mockStore) Set(key, value string) error {
	m.data[key] = value
	return nil
}

func (m *mockStore) Get(key string) (string, error) {
	v, ok := m.data[key]
	if !ok {
		return "", &ErrNotFound{Key: key}
	}
	return v, nil
}

func (m *mockStore) Delete(key string) error {
	delete(m.data, key)
	return nil
}

func (m *mockStore) List() ([]string, error) {
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys, nil
}

func TestResolve(t *testing.T) {
	store := &mockStore{data: map[string]string{"anthropic:default": "sk-abc"}}

	got, err := Resolve(store, "literal-token")
	if err != nil || got != "literal-token" {
		t.Fatalf("resolve literal got %q err %v", got, err)
	}

	got, err = Resolve(store, "auth:anthropic:default")
	if err != nil || got != "sk-abc" {
		t.Fatalf("resolve ref got %q err %v", got, err)
	}

	_, err = Resolve(store, "auth:")
	if err == nil {
		t.Fatal("expected invalid auth ref error")
	}

	_, err = Resolve(store, "auth:missing:key")
	if err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestParseRef(t *testing.T) {
	provider, name, ok := ParseRef("auth:anthropic:default")
	if !ok || provider != "anthropic" || name != "default" {
		t.Fatalf("unexpected parse: %q %q %v", provider, name, ok)
	}

	provider, name, ok = ParseRef("auth:openai")
	if !ok || provider != "openai" || name != "" {
		t.Fatalf("unexpected parse short: %q %q %v", provider, name, ok)
	}

	provider, name, ok = ParseRef("literal")
	if ok || provider != "" || name != "" {
		t.Fatalf("unexpected non-auth parse: %q %q %v", provider, name, ok)
	}
}

func TestFileStoreCRUDAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new filestore: %v", err)
	}

	if err := s.Set("a", "1"); err != nil {
		t.Fatalf("set a: %v", err)
	}
	if err := s.Set("b", "2"); err != nil {
		t.Fatalf("set b: %v", err)
	}

	v, err := s.Get("a")
	if err != nil || v != "1" {
		t.Fatalf("get a got %q err %v", v, err)
	}

	keys, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !reflect.DeepEqual(keys, []string{"a", "b"}) {
		t.Fatalf("keys = %v", keys)
	}

	if err := s.Delete("a"); err != nil {
		t.Fatalf("delete a: %v", err)
	}
	if _, err := s.Get("a"); !IsNotFound(err) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
	if err := s.Delete("a"); !IsNotFound(err) {
		t.Fatalf("expected delete missing not found, got %v", err)
	}

	s2, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new filestore #2: %v", err)
	}
	v, err = s2.Get("b")
	if err != nil || v != "2" {
		t.Fatalf("reloaded get b got %q err %v", v, err)
	}
}

func TestGeneratePKCE(t *testing.T) {
	p, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}
	if p.Verifier == "" {
		t.Fatal("Verifier is empty")
	}
	if p.Challenge == "" {
		t.Fatal("Challenge is empty")
	}
	// Two calls should produce different values.
	p2, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE #2: %v", err)
	}
	if p.Verifier == p2.Verifier {
		t.Error("expected different verifiers from two calls")
	}
}

func TestGeneratePKCE_ChallengeIsSHA256(t *testing.T) {
	p, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}
	// Verify challenge = base64url(sha256(verifier))
	sum := sha256.Sum256([]byte(p.Verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if p.Challenge != want {
		t.Errorf("challenge mismatch: got %q want %q", p.Challenge, want)
	}
}

func TestOAuthToken_IsExpired(t *testing.T) {
	t.Run("expired", func(t *testing.T) {
		tok := &OAuthToken{ExpiresAt: 1000} // long past
		if !tok.IsExpired() {
			t.Error("expected expired token to be IsExpired=true")
		}
	})

	t.Run("valid future", func(t *testing.T) {
		far := time.Now().Add(10 * time.Minute).UnixMilli()
		tok := &OAuthToken{ExpiresAt: far}
		if tok.IsExpired() {
			t.Error("expected future token to be IsExpired=false")
		}
	})

	t.Run("within 30s buffer", func(t *testing.T) {
		// Expires in 15 seconds — within the 30s buffer, so should be expired.
		soon := time.Now().Add(15 * time.Second).UnixMilli()
		tok := &OAuthToken{ExpiresAt: soon}
		if !tok.IsExpired() {
			t.Error("expected token expiring within buffer to be IsExpired=true")
		}
	})
}

func TestStorePendingPKCE_LoadPendingPKCE(t *testing.T) {
	p := PKCEParams{Verifier: "verif123", Challenge: "chal456"}
	StorePendingPKCE("testprovider", p)

	got, ok := LoadPendingPKCE("testprovider")
	if !ok {
		t.Fatal("expected LoadPendingPKCE to return true")
	}
	if got.Verifier != p.Verifier || got.Challenge != p.Challenge {
		t.Errorf("loaded PKCE mismatch: got %+v want %+v", got, p)
	}

	// Load again should return false (removed after first load).
	_, ok2 := LoadPendingPKCE("testprovider")
	if ok2 {
		t.Error("expected second LoadPendingPKCE to return false")
	}
}

func TestStorePendingPKCE_MissingKey(t *testing.T) {
	_, ok := LoadPendingPKCE("nonexistent-provider-xyz")
	if ok {
		t.Error("expected missing provider to return ok=false")
	}
}

func TestAnthropicBuildAuthorizeURL(t *testing.T) {
	p := PKCEParams{Verifier: "test-verifier", Challenge: "test-challenge"}
	u := AnthropicBuildAuthorizeURL(p, "")

	if !strings.Contains(u, "client_id=") {
		t.Errorf("expected client_id in URL: %s", u)
	}
	if !strings.Contains(u, "code_challenge=test-challenge") {
		t.Errorf("expected code_challenge in URL: %s", u)
	}
	if !strings.Contains(u, "response_type=code") {
		t.Errorf("expected response_type in URL: %s", u)
	}
	if !strings.Contains(u, "claude.ai") {
		t.Errorf("expected claude.ai in URL: %s", u)
	}
}

// patchDefaultClient temporarily replaces http.DefaultClient with a client
// whose transport redirects all requests to srv.URL.
func patchDefaultClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := *http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: &redirectTransport{targetBase: srv.URL, rt: http.DefaultTransport},
	}
	t.Cleanup(func() { *http.DefaultClient = orig })
}

// redirectTransport rewrites the request host to targetBase.
type redirectTransport struct {
	targetBase string
	rt         http.RoundTripper
}

func (r *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request and rewrite the URL.
	clone := req.Clone(req.Context())
	clone.URL.Scheme = "http"
	// Parse targetBase to get host.
	var host string
	if len(r.targetBase) > 7 {
		host = r.targetBase[7:] // strip "http://"
	}
	clone.URL.Host = host
	return r.rt.RoundTrip(clone)
}

func TestAnthropicExchange_MockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "acc-tok-123",
			"refresh_token": "ref-tok-456",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)

	tok, err := AnthropicExchange(context.Background(), "mycode", "myverifier")
	if err != nil {
		t.Fatalf("AnthropicExchange: %v", err)
	}
	if tok.AccessToken != "acc-tok-123" {
		t.Errorf("access token = %q; want acc-tok-123", tok.AccessToken)
	}
	if tok.RefreshToken != "ref-tok-456" {
		t.Errorf("refresh token = %q; want ref-tok-456", tok.RefreshToken)
	}
	if tok.ExpiresAt == 0 {
		t.Error("expected non-zero ExpiresAt")
	}
}

func TestAnthropicExchange_WithCodeHashState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		// Verify code is split correctly.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("code=%v,state=%v", body["code"], body["state"]),
			"expires_in":   3600,
		})
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)

	tok, err := AnthropicExchange(context.Background(), "MYCODE#MYSTATE", "verifier")
	if err != nil {
		t.Fatalf("AnthropicExchange: %v", err)
	}
	if !strings.Contains(tok.AccessToken, "code=MYCODE") {
		t.Errorf("expected code split correctly, got: %q", tok.AccessToken)
	}
	if !strings.Contains(tok.AccessToken, "state=MYSTATE") {
		t.Errorf("expected state split correctly, got: %q", tok.AccessToken)
	}
}

func TestAnthropicExchange_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)

	_, err := AnthropicExchange(context.Background(), "code", "verifier")
	if err == nil {
		t.Fatal("expected error for HTTP 401")
	}
}

func TestAnthropicRefresh_MockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["grant_type"] != "refresh_token" {
			http.Error(w, "bad grant type", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-acc-tok",
			"refresh_token": "new-ref-tok",
			"expires_in":    7200,
		})
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)

	tok, err := AnthropicRefresh(context.Background(), "old-refresh-token")
	if err != nil {
		t.Fatalf("AnthropicRefresh: %v", err)
	}
	if tok.AccessToken != "new-acc-tok" {
		t.Errorf("access token = %q; want new-acc-tok", tok.AccessToken)
	}
}

func TestGeminiRefresh_MockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "gemini-new-tok",
			"expires_in":   3600,
			// No refresh_token in response — should reuse old.
		})
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)

	tok, err := GeminiRefresh(context.Background(), "old-gemini-refresh")
	if err != nil {
		t.Fatalf("GeminiRefresh: %v", err)
	}
	if tok.AccessToken != "gemini-new-tok" {
		t.Errorf("access token = %q; want gemini-new-tok", tok.AccessToken)
	}
	// Should reuse old refresh token when none returned.
	if tok.RefreshToken != "old-gemini-refresh" {
		t.Errorf("refresh token should be reused, got %q", tok.RefreshToken)
	}
}

func TestGeminiRefresh_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "expired", http.StatusUnauthorized)
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)

	_, err := GeminiRefresh(context.Background(), "bad-token")
	if err == nil {
		t.Fatal("expected error for HTTP 401")
	}
}

func TestOpenAIExchange_MockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "openai-access-tok",
			"refresh_token": "openai-refresh-tok",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)

	tok, err := openAIExchange(context.Background(), "code123", "verifier456", "http://localhost:1455")
	if err != nil {
		t.Fatalf("openAIExchange: %v", err)
	}
	if tok.AccessToken != "openai-access-tok" {
		t.Errorf("access token = %q; want openai-access-tok", tok.AccessToken)
	}
}

func TestOpenAIExchange_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)

	_, err := openAIExchange(context.Background(), "code", "verifier", "http://localhost:1455")
	if err == nil {
		t.Fatal("expected error for HTTP 400")
	}
}

func TestKeychainStore_CRUD(t *testing.T) {
	s := NewKeychainStore()

	// Test Set (may fail on CI without a keychain).
	err := s.Set("test-aviary-key", "test-value")
	if err != nil {
		t.Skipf("keychain not available (Set failed): %v", err)
	}
	t.Cleanup(func() { _ = s.Delete("test-aviary-key") })

	val, err := s.Get("test-aviary-key")
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if val != "test-value" {
		t.Errorf("Get = %q; want %q", val, "test-value")
	}

	keys, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, k := range keys {
		if k == "test-aviary-key" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected key in List, got %v", keys)
	}

	if err := s.Delete("test-aviary-key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get("test-aviary-key"); !IsNotFound(err) {
		t.Errorf("expected not found after delete, got %v", err)
	}
}

func TestKeychainStore_GetNotFound(t *testing.T) {
	s := NewKeychainStore()
	_, err := s.Get("nonexistent-aviary-key-xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent key")
	}
	if !IsNotFound(err) {
		t.Errorf("expected IsNotFound error, got %T: %v", err, err)
	}
}

func TestKeychainStore_ListEmpty(t *testing.T) {
	s := NewKeychainStore()
	keys, err := s.List()
	if err != nil {
		t.Fatalf("List on empty store: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected empty list, got %v", keys)
	}
}

func TestFileStore_CorruptedJSON(t *testing.T) {
	path := fmt.Sprintf("%s/corrupted.json", t.TempDir())
	// Write invalid JSON.
	if err := writeFileContent(path, "{not valid json}"); err != nil {
		t.Fatal(err)
	}
	_, err := NewFileStore(path)
	if err == nil {
		t.Fatal("expected error loading corrupted JSON")
	}
}

func writeFileContent(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func TestSplitOnce(t *testing.T) {
	tests := []struct {
		s      string
		sep    byte
		wantA  string
		wantB  string
		wantOK bool
	}{
		{"hello#world", '#', "hello", "world", true},
		{"CODE#STATE", '#', "CODE", "STATE", true},
		{"nosep", '#', "nosep", "", false},
		{"", '#', "", "", false},
		{"a#b#c", '#', "a", "b#c", true}, // only first occurrence
	}
	for _, tc := range tests {
		a, b, ok := splitOnce(tc.s, tc.sep)
		if a != tc.wantA || b != tc.wantB || ok != tc.wantOK {
			t.Errorf("splitOnce(%q, %q) = (%q, %q, %v); want (%q, %q, %v)",
				tc.s, string(tc.sep), a, b, ok, tc.wantA, tc.wantB, tc.wantOK)
		}
	}
}

func TestIntegration_FileStoreResolve(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new filestore: %v", err)
	}
	if err := s.Set("openai:default", "sk-test"); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := Resolve(s, "auth:openai:default")
	if err != nil || got != "sk-test" {
		t.Fatalf("resolve got %q err %v", got, err)
	}
}

func TestGeminiExchange_MockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "gemini-access-tok",
			"refresh_token": "gemini-refresh-tok",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)

	tok, err := geminiExchange(context.Background(), "code123", "verifier456", "http://localhost:45289/callback")
	if err != nil {
		t.Fatalf("geminiExchange: %v", err)
	}
	if tok.AccessToken != "gemini-access-tok" {
		t.Errorf("access token = %q; want gemini-access-tok", tok.AccessToken)
	}
	if tok.RefreshToken != "gemini-refresh-tok" {
		t.Errorf("refresh token = %q; want gemini-refresh-tok", tok.RefreshToken)
	}
	if tok.ExpiresAt == 0 {
		t.Error("expected non-zero ExpiresAt")
	}
}

func TestGeminiExchange_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)

	_, err := geminiExchange(context.Background(), "code", "verifier", "http://localhost:45289/callback")
	if err == nil {
		t.Fatal("expected error for HTTP 403")
	}
}

func TestOpenAILogin_PortInUse(t *testing.T) {
	// Bind port 1455 so OpenAILogin fails with "address already in use".
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", OpenAICallbackPort))
	if err != nil {
		t.Skipf("cannot bind port %d: %v", OpenAICallbackPort, err)
	}
	defer func() { _ = ln.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = OpenAILogin(ctx)
	if err == nil {
		t.Fatal("expected error when callback port is busy")
	}
}

func TestGeminiLogin_PortInUse(t *testing.T) {
	// Bind port 45289 so GeminiLogin fails with "address already in use".
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", GeminiCallbackPort))
	if err != nil {
		t.Skipf("cannot bind port %d: %v", GeminiCallbackPort, err)
	}
	defer func() { _ = ln.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = GeminiLogin(ctx)
	if err == nil {
		t.Fatal("expected error when callback port is busy")
	}
}
