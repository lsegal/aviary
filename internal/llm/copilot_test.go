package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCopilotExchangeProvider returns a provider whose token exchange is the
// given fake, so tests never reach GitHub.
func newCopilotExchangeProvider(ghToken string, exchange func(context.Context, string) (string, time.Time, error)) *CopilotProvider {
	p := NewCopilotProvider(ghToken, "gpt-5")
	p.exchangeToken = exchange
	return p
}

func TestCopilotGetTokenUsesCachedToken(t *testing.T) {
	p := newCopilotExchangeProvider("ghp-user", func(context.Context, string) (string, time.Time, error) {
		t.Fatal("exchange called for a valid cached token")
		return "", time.Time{}, nil
	})
	p.copilotTok = "cached"
	p.tokExpiry = time.Now().Add(time.Hour)

	tok, err := p.getToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "cached", tok)
}

func TestCopilotGetTokenRefreshesExpiredToken(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	var calls int
	var gotGH string
	p := newCopilotExchangeProvider("ghp-user", func(_ context.Context, gh string) (string, time.Time, error) {
		calls++
		gotGH = gh
		return "fresh", expiry, nil
	})
	p.copilotTok = "stale"
	// Inside the 30-second renewal margin counts as expired.
	p.tokExpiry = time.Now().Add(10 * time.Second)

	tok, err := p.getToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "fresh", tok)
	assert.Equal(t, "ghp-user", gotGH)
	assert.Equal(t, "fresh", p.copilotTok)
	assert.True(t, expiry.Equal(p.tokExpiry))

	// The refreshed token is cached for the next call.
	tok, err = p.getToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "fresh", tok)
	assert.Equal(t, 1, calls)
}

func TestCopilotGetTokenExchangesOnFirstUse(t *testing.T) {
	p := newCopilotExchangeProvider("ghp-user", func(context.Context, string) (string, time.Time, error) {
		return "first", time.Now().Add(time.Hour), nil
	})

	tok, err := p.getToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "first", tok)
}

func TestCopilotGetTokenExchangeError(t *testing.T) {
	p := newCopilotExchangeProvider("ghp-user", func(context.Context, string) (string, time.Time, error) {
		return "", time.Time{}, errors.New("exchange denied")
	})
	p.copilotTok = "stale"
	p.tokExpiry = time.Now().Add(-time.Minute)

	tok, err := p.getToken(context.Background())
	require.EqualError(t, err, "exchange denied")
	assert.Empty(t, tok)
	// A failed exchange leaves the previous token in place.
	assert.Equal(t, "stale", p.copilotTok)
}

func TestCopilotGetTokenWithoutGitHubTokenSkipsExchange(t *testing.T) {
	p := newCopilotExchangeProvider("", func(context.Context, string) (string, time.Time, error) {
		t.Fatal("exchange called without a GitHub token")
		return "", time.Time{}, nil
	})
	p.copilotTok = "expired"
	p.tokExpiry = time.Now().Add(-time.Hour)

	tok, err := p.getToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "expired", tok)
}

func TestNewCopilotHTTPProviderDefaultsBaseURL(t *testing.T) {
	assert.Equal(t, copilotBaseURL, NewCopilotHTTPProvider("tok", "gpt-5", "").baseURL)
	assert.Equal(t, "http://example.test", NewCopilotHTTPProvider("tok", "gpt-5", "http://example.test/").baseURL)
}

func TestCopilotPingSendsHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/models", r.URL.Path)
		got = r.Header.Clone()
		_, _ = fmt.Fprint(w, `{"data":[]}`)
	}))
	defer srv.Close()

	p := NewCopilotHTTPProvider("api-token", "gpt-5", srv.URL)
	require.NoError(t, p.Ping(context.Background()))
	assert.Equal(t, "Bearer api-token", got.Get("Authorization"))
	assert.Equal(t, copilotEditorVersion, got.Get("Editor-Version"))
	assert.Equal(t, copilotIntegrationID, got.Get("Copilot-Integration-Id"))
}

func TestCopilotPingWithoutTokenOmitsAuthorization(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	defer srv.Close()

	p := NewCopilotHTTPProvider("", "gpt-5", srv.URL)
	require.NoError(t, p.Ping(context.Background()))
	assert.Empty(t, got.Get("Authorization"))
	assert.Equal(t, copilotEditorVersion, got.Get("Editor-Version"))
}

func TestCopilotPingNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, "  bad credentials \n")
	}))
	defer srv.Close()

	err := NewCopilotHTTPProvider("tok", "gpt-5", srv.URL).Ping(context.Background())
	require.EqualError(t, err, "copilot ping: 401 Unauthorized: bad credentials")
}

func TestCopilotPingTransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	err := NewCopilotHTTPProvider("tok", "gpt-5", url).Ping(context.Background())
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "copilot ping: "), err.Error())
}

func TestCopilotPingInvalidBaseURL(t *testing.T) {
	err := NewCopilotHTTPProvider("tok", "gpt-5", "://bad").Ping(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing protocol scheme")
}

func TestCopilotPingTokenError(t *testing.T) {
	p := newCopilotExchangeProvider("ghp-user", func(context.Context, string) (string, time.Time, error) {
		return "", time.Time{}, errors.New("no copilot seat")
	})
	require.EqualError(t, p.Ping(context.Background()), "no copilot seat")
}

// copilotSSEServer records each chat request body and replies with the given
// SSE lines.
func copilotSSEServer(t *testing.T, lines []string, body *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		if body != nil {
			raw, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.NoError(t, json.Unmarshal(raw, body))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range lines {
			_, _ = fmt.Fprint(w, line+"\n\n")
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCopilotStreamRequestBody(t *testing.T) {
	var body map[string]any
	srv := copilotSSEServer(t, []string{"data: [DONE]"}, &body)
	p := NewCopilotHTTPProvider("tok", "gpt-5", srv.URL)

	ch, err := p.Stream(context.Background(), Request{
		System:  "be brief",
		MaxToks: 256,
		Tools: []ToolDefinition{{
			Name:        "read_file",
			Description: "Read a file",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
		}},
		Messages: []Message{
			{Role: RoleSystem, Content: "extra rules"},
			{Role: RoleUser, Content: "read it"},
			// Consecutive assistant messages collapse into one with every tool
			// call; the missing ID is synthesized from the message index.
			{Role: RoleAssistant, Content: "on it", ToolCall: &ToolCall{ID: " call_a ", Name: "read_file", Arguments: map[string]any{"path": "a.txt"}}},
			{Role: RoleAssistant, Content: "and this", ToolCall: &ToolCall{Name: "read_file"}},
			{Role: RoleUser, Result: &ToolResult{ToolCallID: "call_a", Name: "read_file", Content: "A"}},
			{Role: RoleUser, Result: &ToolResult{ToolCallID: "call_3", Name: "read_file", Content: "B"}},
			// Text-only consecutive assistant messages join with a newline.
			{Role: RoleAssistant, Content: "first"},
			{Role: RoleAssistant, Content: ""},
			{Role: RoleAssistant, Content: "second"},
		},
	})
	require.NoError(t, err)
	events := collectEvents(t, ch)
	require.Len(t, events, 1)
	assert.Equal(t, EventTypeDone, events[0].Type)

	assert.Equal(t, "gpt-5", body["model"])
	assert.Equal(t, true, body["stream"])
	assert.EqualValues(t, 256, body["max_tokens"])

	want := []any{
		map[string]any{"role": "system", "content": "be brief"},
		map[string]any{"role": "system", "content": "extra rules"},
		map[string]any{"role": "user", "content": "read it"},
		map[string]any{"role": "assistant", "tool_calls": []any{
			map[string]any{"index": float64(0), "id": "call_a", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"a.txt"}`}},
			map[string]any{"index": float64(1), "id": "call_3", "type": "function", "function": map[string]any{"name": "read_file", "arguments": "{}"}},
		}},
		map[string]any{"role": "tool", "content": "A", "tool_call_id": "call_a"},
		map[string]any{"role": "tool", "content": "B", "tool_call_id": "call_3"},
		map[string]any{"role": "assistant", "content": "first\nsecond"},
	}
	assert.Equal(t, want, body["messages"])

	assert.Equal(t, []any{map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        "read_file",
			"description": "Read a file",
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
		},
	}}, body["tools"])
}

func TestCopilotStreamOmitsOptionalFields(t *testing.T) {
	var body map[string]any
	srv := copilotSSEServer(t, []string{"data: [DONE]"}, &body)
	ch, err := NewCopilotHTTPProvider("tok", "gpt-5", srv.URL).Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	require.NoError(t, err)
	collectEvents(t, ch)

	assert.NotContains(t, body, "max_tokens")
	assert.NotContains(t, body, "tools")
	assert.Equal(t, []any{map[string]any{"role": "user", "content": "hi"}}, body["messages"])
}

func TestCopilotStreamParsesTextAndToolCalls(t *testing.T) {
	srv := copilotSSEServer(t, []string{
		": keep-alive comment",
		"event: message",
		"data: not json",
		`data: {"choices":[{"delta":{"content":"Hel"}}]}`,
		`data: {"choices":[{"delta":{"content":"lo"}}]}`,
		// Tool calls arrive out of order and in fragments.
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","function":{"name":"list_dir","arguments":""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"read_file","arguments":"{\"pa"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"a.txt\"}"}}]}}]}`,
		// A call that never gets a name is dropped.
		`data: {"choices":[{"delta":{"tool_calls":[{"index":2,"id":"call_c","function":{"arguments":"{}"}}]}}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0}}`,
		`data: {"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3}}`,
		"data: [DONE]",
		`data: {"choices":[{"delta":{"content":"after done"}}]}`,
	}, nil)

	ch, err := NewCopilotHTTPProvider("tok", "gpt-5", srv.URL).Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	require.NoError(t, err)
	events := collectEvents(t, ch)

	require.Len(t, events, 6)
	assert.Equal(t, Event{Type: EventTypeText, Text: "Hel"}, events[0])
	assert.Equal(t, Event{Type: EventTypeText, Text: "lo"}, events[1])
	assert.Equal(t, EventTypeToolCall, events[2].Type)
	assert.Equal(t, &ToolCall{ID: "call_a", Name: "read_file", Arguments: map[string]any{"path": "a.txt"}}, events[2].ToolCall)
	assert.Equal(t, EventTypeToolCall, events[3].Type)
	assert.Equal(t, &ToolCall{ID: "call_b", Name: "list_dir", Arguments: map[string]any{}}, events[3].ToolCall)
	assert.Equal(t, Event{Type: EventTypeUsage, Usage: &Usage{InputTokens: 7, OutputTokens: 3}}, events[4])
	assert.Equal(t, EventTypeDone, events[5].Type)
}

func TestCopilotStreamInvalidToolArguments(t *testing.T) {
	srv := copilotSSEServer(t, []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"read_file","arguments":"{oops"}}]}}]}`,
		"data: [DONE]",
	}, nil)

	ch, err := NewCopilotHTTPProvider("tok", "gpt-5", srv.URL).Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	require.NoError(t, err)
	events := collectEvents(t, ch)

	require.Len(t, events, 1)
	assert.Equal(t, EventTypeError, events[0].Type)
	assert.Contains(t, events[0].Error.Error(), `copilot tool arguments for "read_file"`)
}

func TestCopilotStreamScannerError(t *testing.T) {
	srv := copilotSSEServer(t, []string{
		`data: {"choices":[{"delta":{"content":"partial"}}]}`,
		"data: " + strings.Repeat("x", maxSSELineBytes+1),
	}, nil)

	ch, err := NewCopilotHTTPProvider("tok", "gpt-5", srv.URL).Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	require.NoError(t, err)
	events := collectEvents(t, ch)

	require.Len(t, events, 2)
	assert.Equal(t, Event{Type: EventTypeText, Text: "partial"}, events[0])
	assert.Equal(t, EventTypeError, events[1].Type)
	assert.Contains(t, events[1].Error.Error(), "copilot stream: ")
}

func TestCopilotStreamNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, "\n rate limited \n")
	}))
	defer srv.Close()

	ch, err := NewCopilotHTTPProvider("tok", "gpt-5", srv.URL).Stream(context.Background(), Request{})
	assert.Nil(t, ch)
	require.EqualError(t, err, "copilot: 429 Too Many Requests: rate limited")
}

func TestCopilotStreamTransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	ch, err := NewCopilotHTTPProvider("tok", "gpt-5", url).Stream(context.Background(), Request{})
	assert.Nil(t, ch)
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "copilot: "), err.Error())
}

func TestCopilotStreamInvalidBaseURL(t *testing.T) {
	ch, err := NewCopilotHTTPProvider("tok", "gpt-5", "://bad").Stream(context.Background(), Request{})
	assert.Nil(t, ch)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing protocol scheme")
}

func TestCopilotStreamTokenError(t *testing.T) {
	p := newCopilotExchangeProvider("ghp-user", func(context.Context, string) (string, time.Time, error) {
		return "", time.Time{}, errors.New("no copilot seat")
	})
	ch, err := p.Stream(context.Background(), Request{})
	assert.Nil(t, ch)
	require.EqualError(t, err, "no copilot seat")
}
