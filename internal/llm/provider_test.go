package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// oauthTokenJSON returns a stored OAuth token that expires at the given time.
func oauthTokenJSON(access, refresh string, expiresAt time.Time) string {
	return fmt.Sprintf(`{"access":%q,"refresh":%q,"expires_at":%d}`, access, refresh, expiresAt.UnixMilli())
}

// refreshServer stands in for an OAuth token endpoint and counts the refresh
// requests it receives.
func refreshServer(t *testing.T, status int, access string) *int {
	t.Helper()
	calls := 0
	mockDefaultClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if status != http.StatusOK {
			http.Error(w, "refresh rejected", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  access,
			"refresh_token": "rt-new",
			"expires_in":    3600,
		})
	})
	return &calls
}

func TestForModelForceRefresh_RefreshesUnexpiredGeminiToken(t *testing.T) {
	calls := refreshServer(t, http.StatusOK, "at-forced")

	var refs []string
	persisted := map[string]string{}
	f := NewFactory(func(ref string) (string, error) {
		refs = append(refs, ref)
		return oauthTokenJSON("at-current", "rt-old", time.Now().Add(time.Hour)), nil
	}).WithTokenSetter(func(key, value string) error {
		persisted[key] = value
		return nil
	})

	p, err := f.ForModelForceRefresh("google-gemini/gemini-2.5-pro")
	require.NoError(t, err)
	gp, ok := p.(*GeminiCodeAssistProvider)
	require.True(t, ok, "got %T", p)
	assert.Equal(t, "at-forced", gp.accessToken)
	assert.Equal(t, "gemini-2.5-pro", gp.model)
	assert.Equal(t, 1, *calls, "a valid token must still be refreshed when forced")
	assert.Equal(t, []string{"auth:gemini:oauth"}, refs)
	require.Contains(t, persisted, "gemini:oauth")
	assert.Contains(t, persisted["gemini:oauth"], "at-forced")
}

func TestForModel_DoesNotRefreshUnexpiredToken(t *testing.T) {
	calls := refreshServer(t, http.StatusOK, "at-forced")
	f := NewFactory(func(string) (string, error) {
		return oauthTokenJSON("at-current", "rt-old", time.Now().Add(time.Hour)), nil
	})

	p, err := f.ForModel("gemini-code-assist/gemini-2.5-pro")
	require.NoError(t, err)
	assert.Equal(t, "at-current", p.(*GeminiCodeAssistProvider).accessToken)
	assert.Zero(t, *calls)
}

func TestForModelForceRefresh_AnthropicPersistsRefreshedToken(t *testing.T) {
	calls := refreshServer(t, http.StatusOK, "at-anthropic")

	var persistedKey, persistedValue string
	f := NewFactory(func(ref string) (string, error) {
		require.Equal(t, "auth:anthropic:oauth", ref)
		return oauthTokenJSON("at-current", "rt-old", time.Now().Add(time.Hour)), nil
	}).WithTokenSetter(func(key, value string) error {
		persistedKey, persistedValue = key, value
		return nil
	})

	p, err := f.ForModelForceRefresh("anthropic/claude-sonnet-5-5")
	require.NoError(t, err)
	ap, ok := p.(*AnthropicProvider)
	require.True(t, ok, "got %T", p)
	assert.True(t, ap.oauth)
	assert.Equal(t, 1, *calls)
	assert.Equal(t, "anthropic:oauth", persistedKey)
	assert.Contains(t, persistedValue, "at-anthropic")
}

func TestForModelForceRefresh_RefreshFailureKeepsStaleToken(t *testing.T) {
	calls := refreshServer(t, http.StatusBadRequest, "")
	setterCalled := false
	f := NewFactory(func(string) (string, error) {
		return oauthTokenJSON("at-stale", "rt-old", time.Now().Add(time.Hour)), nil
	}).WithTokenSetter(func(string, string) error {
		setterCalled = true
		return nil
	})

	p, err := f.ForModelForceRefresh("google-gemini/gemini-2.5-pro")
	require.NoError(t, err)
	assert.Equal(t, "at-stale", p.(*GeminiCodeAssistProvider).accessToken)
	assert.Equal(t, 1, *calls)
	assert.False(t, setterCalled, "a failed refresh must not be persisted")
}

func TestForModelForceRefresh_TokenSetterErrorStillUsesRefreshedToken(t *testing.T) {
	refreshServer(t, http.StatusOK, "at-forced")
	f := NewFactory(func(string) (string, error) {
		return oauthTokenJSON("at-current", "rt-old", time.Now().Add(time.Hour)), nil
	}).WithTokenSetter(func(string, string) error {
		return errors.New("credential store is read-only")
	})

	p, err := f.ForModelForceRefresh("google-gemini/gemini-2.5-pro")
	require.NoError(t, err)
	assert.Equal(t, "at-forced", p.(*GeminiCodeAssistProvider).accessToken)
}

func TestForModelForceRefresh_ProviderWithoutRefreshFlow(t *testing.T) {
	// GitHub Copilot tokens have no refresh flow here, so a forced refresh
	// must not reach any token endpoint and keeps the stored token.
	calls := refreshServer(t, http.StatusOK, "at-forced")
	f := NewFactory(func(ref string) (string, error) {
		if ref == "auth:github-copilot:oauth" {
			return oauthTokenJSON("gh-token", "rt-old", time.Now().Add(time.Hour)), nil
		}
		return "", nil
	})

	p, err := f.ForModelForceRefresh("github-copilot/gpt-5")
	require.NoError(t, err)
	cp, ok := p.(*CopilotProvider)
	require.True(t, ok, "got %T", p)
	assert.Equal(t, "gh-token", cp.ghToken)
	assert.Zero(t, *calls)
}

func TestForModelForceRefresh_InvalidModel(t *testing.T) {
	f := NewFactory(func(string) (string, error) {
		t.Fatal("an invalid model string must not resolve credentials")
		return "", nil
	})
	for _, model := range []string{"", "anthropic", "no-slash-model"} {
		_, err := f.ForModelForceRefresh(model)
		require.Error(t, err, model)
		assert.Contains(t, err.Error(), "expected <provider>/<name>")
	}
}

func TestForModel_AuthResolverErrors(t *testing.T) {
	resolverErr := errors.New("keychain locked")
	tests := []struct {
		model   string
		opts    ProviderOptions
		wantErr string
	}{
		{model: "anthropic/claude-sonnet-5-5", wantErr: "anthropic auth: keychain locked"},
		{model: "github-copilot/gpt-5", wantErr: "github-copilot auth: keychain locked"},
		{model: "meta/muse-spark-1.3", wantErr: "meta auth: keychain locked"},
		{model: "vllm/llama", opts: ProviderOptions{Auth: "auth:vllm:default"}, wantErr: "vllm auth: keychain locked"},
		{model: "ollama/llama", opts: ProviderOptions{Auth: "auth:ollama:default"}, wantErr: "ollama auth: keychain locked"},
	}
	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			f := NewFactory(func(string) (string, error) { return "", resolverErr }).
				WithProviderOptionsResolver(func(string) (ProviderOptions, bool) { return tc.opts, true })
			_, err := f.ForModel(tc.model)
			require.Error(t, err)
			assert.ErrorIs(t, err, resolverErr)
			assert.Equal(t, tc.wantErr, err.Error())
		})
	}
}

func TestForModel_GeminiCodeAssistNeedsOAuth(t *testing.T) {
	f := NewFactory(func(string) (string, error) { return "", nil })
	_, err := f.ForModel("gemini-code-assist/gemini-2.5-pro")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "aviary auth login gemini")
}

func TestResolveOptionalAuth(t *testing.T) {
	resolverErr := errors.New("no such credential")
	var resolved []string
	f := NewFactory(func(ref string) (string, error) {
		resolved = append(resolved, ref)
		if ref == "auth:missing:key" {
			return "", resolverErr
		}
		return "secret-for-" + ref, nil
	})

	tests := []struct {
		name    string
		value   string
		want    string
		wantErr error
	}{
		{name: "empty", value: "", want: ""},
		{name: "whitespace only", value: "   ", want: ""},
		{name: "plain key", value: " sk-plain ", want: "sk-plain"},
		{name: "auth reference", value: "auth:vllm:default", want: "secret-for-auth:vllm:default"},
		{name: "resolver error", value: "auth:missing:key", wantErr: resolverErr},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := f.resolveOptionalAuth(tc.value)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
	assert.Equal(t, []string{"auth:vllm:default", "auth:missing:key"}, resolved,
		"only auth: references reach the resolver")

	got, err := NewFactory(nil).resolveOptionalAuth("auth:vllm:default")
	require.NoError(t, err)
	assert.Empty(t, got, "a factory without a resolver resolves references to nothing")
}

// localModelsServer serves an OpenAI-compatible /models endpoint and records
// the Authorization header it receives.
func localModelsServer(t *testing.T, status int, gotAuth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/models", r.URL.Path)
		*gotAuth = r.Header.Get("Authorization")
		if status != http.StatusOK {
			http.Error(w, "unauthorized", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"llama"}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPingModel_UsesPinger(t *testing.T) {
	var gotAuth string
	srv := localModelsServer(t, http.StatusOK, &gotAuth)
	f := NewFactory(func(ref string) (string, error) {
		require.Equal(t, "auth:vllm:default", ref)
		return "vllm-secret", nil
	}).WithProviderOptionsResolver(func(provider string) (ProviderOptions, bool) {
		require.Equal(t, "vllm", provider)
		return ProviderOptions{Auth: "auth:vllm:default", BaseURI: srv.URL + "/v1"}, true
	})

	require.NoError(t, f.PingModel(context.Background(), "vllm/llama"))
	assert.Equal(t, "Bearer vllm-secret", gotAuth)
}

func TestPingModel_PingerError(t *testing.T) {
	var gotAuth string
	srv := localModelsServer(t, http.StatusUnauthorized, &gotAuth)
	f := NewFactory(nil).WithProviderOptionsResolver(func(string) (ProviderOptions, bool) {
		return ProviderOptions{Auth: "plain-key", BaseURI: srv.URL + "/v1"}, true
	})

	err := f.PingModel(context.Background(), "ollama/llama")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.Equal(t, "Bearer plain-key", gotAuth)
}

func TestPingModel_ForModelError(t *testing.T) {
	f := NewFactory(func(string) (string, error) { return "", nil })
	err := f.PingModel(context.Background(), "meta/muse-spark-1.3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "meta auth: missing API key")
}

// metaFactory returns a factory whose Meta provider (which has no Pinger)
// talks to srv.
func metaFactory(srv *httptest.Server) *Factory {
	return NewFactory(func(string) (string, error) { return "meta-key", nil }).
		WithProviderOptionsResolver(func(string) (ProviderOptions, bool) {
			return ProviderOptions{BaseURI: srv.URL + "/v1"}, true
		})
}

func TestPingModel_FallsBackToOneTokenStream(t *testing.T) {
	var gotReq struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		assert.NoError(t, json.Unmarshal(body, &gotReq))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"model\":\"muse-spark-1.3\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"H\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	require.NoError(t, metaFactory(srv).PingModel(context.Background(), "meta/muse-spark-1.3"))
	assert.Equal(t, "/v1/chat/completions", gotPath)
	assert.Equal(t, "muse-spark-1.3", gotReq.Model)
	require.NotEmpty(t, gotReq.Messages)
	last := gotReq.Messages[len(gotReq.Messages)-1]
	assert.Equal(t, "user", last.Role)
	assert.Contains(t, fmt.Sprint(last.Content), "Hi")
}

func TestPingModel_FallbackStreamErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid api key","type":"invalid_request_error"}}`)
	}))
	defer srv.Close()

	err := metaFactory(srv).PingModel(context.Background(), "meta/muse-spark-1.3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}
