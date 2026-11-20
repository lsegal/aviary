package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturedAnthropicRequest struct {
	query  string
	header http.Header
	system []any
}

// streamAnthropicCapture sends one streamed request through p to a mock
// server pointed at by ANTHROPIC_BASE_URL and returns what the server saw.
func streamAnthropicCapture(t *testing.T, newProvider func() *AnthropicProvider) capturedAnthropicRequest {
	t.Helper()
	var got capturedAnthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		var payload map[string]any
		assert.NoError(t, json.Unmarshal(body, &payload))
		got.query = r.URL.RawQuery
		got.header = r.Header.Clone()
		got.system, _ = payload["system"].([]any)
		anthropicSSEResponse(w, "ok")
	}))
	defer srv.Close()
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)

	p := newProvider()
	ch, err := p.Stream(context.Background(), Request{
		System:   "Be terse.",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	})
	require.NoError(t, err)
	for ev := range ch {
		require.NotEqual(t, EventTypeError, ev.Type, "stream error: %v", ev.Error)
	}
	return got
}

func systemText(t *testing.T, block any) string {
	t.Helper()
	m, ok := block.(map[string]any)
	require.True(t, ok)
	text, _ := m["text"].(string)
	return text
}

func TestAnthropicOAuthProvider_SendsCapturedCLIRequestShape(t *testing.T) {
	got := streamAnthropicCapture(t, func() *AnthropicProvider {
		return NewAnthropicOAuthProvider("oauth-token", "claude-opus-5-5")
	})

	assert.Equal(t, "beta=true", got.query)
	assert.Equal(t, "Bearer oauth-token", got.header.Get("Authorization"))
	assert.Empty(t, got.header.Get("X-Api-Key"))
	assert.Equal(t, "claude-cli/2.1.312 (external, cli)", got.header.Get("User-Agent"))
	assert.Equal(t, "cli", got.header.Get("X-App"))
	assert.Equal(t, "2023-06-01", got.header.Get("Anthropic-Version"))
	assert.Equal(t, "true", got.header.Get("Anthropic-Dangerous-Direct-Browser-Access"))
	assert.Equal(t, claudeCLIBetas, got.header.Get("Anthropic-Beta"))
	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, got.header.Get("X-Claude-Code-Session-Id"))
	assert.Equal(t, "0.128.0", got.header.Get("X-Stainless-Package-Version"))

	require.Len(t, got.system, 2)
	assert.Equal(t, "x-anthropic-billing-header: cc_version=2.1.312.7fc; cc_entrypoint=cli;", systemText(t, got.system[0]))
	assert.Equal(t, "Be terse.", systemText(t, got.system[1]))
}

func TestAnthropicOAuthProvider_SessionIDStableAcrossRequests(t *testing.T) {
	var ids []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids = append(ids, r.Header.Get("X-Claude-Code-Session-Id"))
		anthropicSSEResponse(w, "ok")
	}))
	defer srv.Close()
	t.Setenv("ANTHROPIC_BASE_URL", srv.URL)

	p := NewAnthropicOAuthProvider("oauth-token", "claude-opus-5-5")
	for range 2 {
		ch, err := p.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "Hello"}}})
		require.NoError(t, err)
		collectEvents(t, ch)
	}
	require.Len(t, ids, 2)
	assert.NotEmpty(t, ids[0])
	assert.Equal(t, ids[0], ids[1])
	assert.NotEqual(t, ids[0], newSessionID())
}

func TestAnthropicAPIKeyProvider_DoesNotImpersonateCLI(t *testing.T) {
	got := streamAnthropicCapture(t, func() *AnthropicProvider {
		return NewAnthropicProvider("api-key", "claude-opus-5-5")
	})

	assert.Empty(t, got.query)
	assert.Equal(t, "api-key", got.header.Get("X-Api-Key"))
	assert.Empty(t, got.header.Get("Authorization"))
	assert.NotContains(t, got.header.Get("User-Agent"), "claude-cli")
	assert.Empty(t, got.header.Get("X-App"))
	assert.Empty(t, got.header.Get("Anthropic-Beta"))
	assert.Empty(t, got.header.Get("X-Claude-Code-Session-Id"))
	assert.Equal(t, "go", got.header.Get("X-Stainless-Lang"))

	require.Len(t, got.system, 1)
	assert.Equal(t, "Be terse.", systemText(t, got.system[0]))
}
