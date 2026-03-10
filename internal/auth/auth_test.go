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

	"github.com/stretchr/testify/assert"
)

func TestErrNotFoundAndIsNotFound(t *testing.T) {
	err := &ErrNotFound{Key: "k1"}
	assert.Equal(t, `credential "k1" not found`, err.Error())
	assert.True(t, IsNotFound(err))
	assert.False(t, IsNotFound(errors.New("other")))

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
	assert.NoError(t, err)
	assert.Equal(t, "literal-token", got)

	got, err = Resolve(store, "auth:anthropic:default")
	assert.NoError(t, err)
	assert.Equal(t, "sk-abc", got)

	_, err = Resolve(store, "auth:")
	assert.Error(t, err)

	_, err = Resolve(store, "auth:missing:key")
	assert.Error(t, err)

}

func TestParseRef(t *testing.T) {
	provider, name, ok := ParseRef("auth:anthropic:default")
	assert.True(t, ok)
	assert.Equal(t, "anthropic", provider)
	assert.Equal(t, "default", name)

	provider, name, ok = ParseRef("auth:openai")
	assert.True(t, ok)
	assert.Equal(t, "openai", provider)
	assert.Equal(t, "", name)

	provider, name, ok = ParseRef("literal")
	assert.False(t, ok)
	assert.Equal(t, "", provider)
	assert.Equal(t, "", name)

}

func TestFileStoreCRUDAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	s, err := NewFileStore(path)
	assert.NoError(t, err)
	err = s.Set("a", "1")
	assert.NoError(t, err)

	err = s.Set("b", "2")
	assert.NoError(t, err)

	v, err := s.Get("a")
	assert.NoError(t, err)
	assert.Equal(t, "1", v)

	keys, err := s.List()
	assert.NoError(t, err)
	assert.True(t, reflect.DeepEqual(keys, []string{"a", "b"}))
	err = s.Delete("a")
	assert.NoError(t, err)

	_, err = s.Get("a")
	assert.True(t, IsNotFound(err))

	err = s.Delete("a")
	assert.True(t, IsNotFound(err))

	s2, err := NewFileStore(path)
	assert.NoError(t, err)

	v, err = s2.Get("b")
	assert.NoError(t, err)
	assert.Equal(t, "2", v)

}

func TestGeneratePKCE(t *testing.T) {
	p, err := GeneratePKCE()
	assert.NoError(t, err)
	assert.NotEqual(t, "", p.Verifier)
	assert.NotEqual(t, "", p.Challenge)

	// Two calls should produce different values.
	p2, err := GeneratePKCE()
	assert.NoError(t, err)
	assert.NotEqual(t, p2.Verifier, p.Verifier)

}

func TestGeneratePKCE_ChallengeIsSHA256(t *testing.T) {
	p, err := GeneratePKCE()
	assert.NoError(t, err)

	// Verify challenge = base64url(sha256(verifier))
	sum := sha256.Sum256([]byte(p.Verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	assert.Equal(t, want, p.Challenge)

}

func TestOAuthToken_IsExpired(t *testing.T) {
	t.Run("expired", func(t *testing.T) {
		tok := &OAuthToken{ExpiresAt: 1000}
		assert. // long past
			True(t, tok.IsExpired())

	})

	t.Run("valid future", func(t *testing.T) {
		far := time.Now().Add(10 * time.Minute).UnixMilli()
		tok := &OAuthToken{ExpiresAt: far}
		assert.False(t, tok.IsExpired())

	})

	t.Run("within 30s buffer", func(t *testing.T) {
		// Expires in 15 seconds — within the 30s buffer, so should be expired.
		soon := time.Now().Add(15 * time.Second).UnixMilli()
		tok := &OAuthToken{ExpiresAt: soon}
		assert.True(t, tok.IsExpired())

	})
}

func TestStorePendingPKCE_LoadPendingPKCE(t *testing.T) {
	p := PKCEParams{Verifier: "verif123", Challenge: "chal456"}
	StorePendingPKCE("testprovider", p)

	got, ok := LoadPendingPKCE("testprovider")
	assert.True(t, ok)
	assert.Equal(t, p.Verifier, got.Verifier)
	assert.Equal(t, p.Challenge, got.Challenge)

	// Load again should return false (removed after first load).
	_, ok2 := LoadPendingPKCE("testprovider")
	assert.False(t, ok2)

}

func TestStorePendingPKCE_MissingKey(t *testing.T) {
	_, ok := LoadPendingPKCE("nonexistent-provider-xyz")
	assert.False(t, ok)

}

func TestAnthropicBuildAuthorizeURL(t *testing.T) {
	p := PKCEParams{Verifier: "test-verifier", Challenge: "test-challenge"}
	u := AnthropicBuildAuthorizeURL(p, "")
	assert.True(t, strings.Contains(u, "client_id="))
	assert.True(t, strings.Contains(u, "code_challenge=test-challenge"))
	assert.True(t, strings.Contains(u, "response_type=code"))
	assert.True(t, strings.Contains(u, "claude.ai"))

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
	assert.NoError(t, err)
	assert.Equal(t, "acc-tok-123", tok.AccessToken)
	assert.Equal(t, "ref-tok-456", tok.RefreshToken)
	assert.NotEqual(t, 0, tok.ExpiresAt)

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
	assert.NoError(t, err)
	assert.True(t, strings.Contains(tok.AccessToken, "code=MYCODE"))
	assert.True(t, strings.Contains(tok.AccessToken, "state=MYSTATE"))

}

func TestAnthropicExchange_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)

	_, err := AnthropicExchange(context.Background(), "code", "verifier")
	assert.Error(t, err)

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
	assert.NoError(t, err)
	assert.Equal(t, "new-acc-tok", tok.AccessToken)

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
	assert.NoError(t, err)
	assert.Equal(t, "gemini-new-tok", tok.AccessToken)
	assert.Equal(t, // Should reuse old refresh token when none returned.
		"old-gemini-refresh", tok.RefreshToken)

}

func TestGeminiRefresh_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "expired", http.StatusUnauthorized)
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)

	_, err := GeminiRefresh(context.Background(), "bad-token")
	assert.Error(t, err)

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
	assert.NoError(t, err)
	assert.Equal(t, "openai-access-tok", tok.AccessToken)

}

func TestOpenAIExchange_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)

	_, err := openAIExchange(context.Background(), "code", "verifier", "http://localhost:1455")
	assert.Error(t, err)

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
	assert.NoError(t, err)
	assert.Equal(t, "test-value", val)

	keys, err := s.List()
	assert.NoError(t, err)

	found := false
	for _, k := range keys {
		if k == "test-aviary-key" {
			found = true
		}
	}
	assert.True(t, found)
	err = s.Delete("test-aviary-key")
	assert.NoError(t, err)

	_, err = s.Get("test-aviary-key")
	assert.True(t, IsNotFound(err))

}

func TestKeychainStore_GetNotFound(t *testing.T) {
	s := NewKeychainStore()
	_, err := s.Get("nonexistent-aviary-key-xyz")
	assert.Error(t, err)
	assert.True(t, IsNotFound(err))

}

func TestKeychainStore_ListEmpty(t *testing.T) {
	s := NewKeychainStore()
	keys, err := s.List()
	assert.NoError(t, err)
	assert.Equal(t, 0, len(keys))

}

func TestFileStore_CorruptedJSON(t *testing.T) {
	path := fmt.Sprintf("%s/corrupted.json", t.TempDir())

	// Write invalid JSON.
	err := writeFileContent(path, "{not valid json}")
	assert.NoError(t, err)

	_, err = NewFileStore(path)
	assert.Error(t, err)

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
		assert.Equal(t, tc.wantA, a)
		assert.Equal(t, tc.wantB, b)
		assert.Equal(t, tc.wantOK, ok)

	}
}

func TestIntegration_FileStoreResolve(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	s, err := NewFileStore(path)
	assert.NoError(t, err)
	err = s.Set("openai:default", "sk-test")
	assert.NoError(t, err)

	got, err := Resolve(s, "auth:openai:default")
	assert.NoError(t, err)
	assert.Equal(t, "sk-test", got)

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
	assert.NoError(t, err)
	assert.Equal(t, "gemini-access-tok", tok.AccessToken)
	assert.Equal(t, "gemini-refresh-tok", tok.RefreshToken)
	assert.NotEqual(t, 0, tok.ExpiresAt)

}

func TestGeminiExchange_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()
	patchDefaultClient(t, srv)

	_, err := geminiExchange(context.Background(), "code", "verifier", "http://localhost:45289/callback")
	assert.Error(t, err)

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
	assert.Error(t, err)

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
	assert.Error(t, err)

}
