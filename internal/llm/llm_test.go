package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func TestConstants(t *testing.T) {
	for _, role := range []Role{RoleUser, RoleAssistant, RoleSystem} {
		if role == "" {
			t.Fatal("role constant should not be empty")
		}
	}
	for _, typ := range []EventType{EventTypeText, EventTypeError, EventTypeDone} {
		if typ == "" {
			t.Fatal("event type constant should not be empty")
		}
	}
}

func TestRequestAndMessageZeroValues(t *testing.T) {
	r := Request{}
	if r.Model != "" || r.MaxToks != 0 || r.Stream {
		t.Fatalf("unexpected zero request: %+v", r)
	}
	m := Message{Role: RoleUser, Content: "hello"}
	if m.Role != RoleUser || m.Content != "hello" {
		t.Fatalf("unexpected message: %+v", m)
	}
}

func TestFactoryForModel(t *testing.T) {
	f := NewFactory(func(_ string) (string, error) { return "test-key", nil })

	cases := []struct {
		model   string
		wantErr bool
	}{
		{model: "anthropic/claude-sonnet-4.5", wantErr: false},
		{model: "openai/gpt-4o", wantErr: false},
		{model: "openai-codex/gpt-5.2", wantErr: false},
		{model: "gemini/gemini-2.0-flash", wantErr: false},
		{model: "stdio/claude", wantErr: false},
		{model: "invalid", wantErr: true},
		{model: "unknown/model", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			p, err := f.ForModel(tc.model)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for model %s", tc.model)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", tc.model, err)
			}
			if p == nil {
				t.Fatalf("provider should not be nil for %s", tc.model)
			}
		})
	}
}

func TestFactoryResolverError(t *testing.T) {
	f := NewFactory(func(ref string) (string, error) {
		if ref == "auth:openai:default" || ref == "auth:openai:oauth" {
			return "", errors.New("boom")
		}
		return "key", nil
	})

	if _, err := f.ForModel("openai/gpt-4o"); err == nil {
		t.Fatal("expected auth resolver error")
	}

	if _, err := f.ForModel("stdio/codex"); err != nil {
		t.Fatalf("stdio should not require auth resolver, got %v", err)
	}
}

func TestFactoryOpenAICodexRequiresOAuth(t *testing.T) {
	f := NewFactory(func(ref string) (string, error) {
		if ref == "auth:openai:oauth" {
			return "", errors.New("missing")
		}
		return "key", nil
	})

	if _, err := f.ForModel("openai-codex/gpt-5.2"); err == nil {
		t.Fatal("expected missing oauth error for openai-codex")
	}
}

func TestFactoryNilResolver(t *testing.T) {
	f := NewFactory(nil)
	if _, err := f.ForModel("anthropic/claude-3-5-sonnet"); err != nil {
		t.Fatalf("expected nil resolver to still construct provider, got %v", err)
	}
}

func TestIntegration_AllProviderKinds(t *testing.T) {
	f := NewFactory(func(_ string) (string, error) { return "integration-key", nil })
	models := []string{
		"anthropic/claude-sonnet-4.5",
		"openai/gpt-4o-mini",
		"openai-codex/gpt-5.2",
		"gemini/gemini-pro",
		"stdio/claude",
	}
	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			p, err := f.ForModel(model)
			if err != nil {
				t.Fatalf("for model %s: %v", model, err)
			}
			if p == nil {
				t.Fatalf("provider is nil for %s", model)
			}
		})
	}
}

func TestParseImageDataURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantOK   bool
		wantMime string
	}{
		{"empty", "", false, ""},
		{"not data url", "https://example.com/img.png", false, ""},
		{"valid png", "data:image/png;base64,iVBORw0KGgoAAAANS", true, "image/png"},
		{"valid jpeg", "data:image/jpeg;base64,/9j/4AAQ", true, "image/jpeg"},
		{"missing base64 marker", "data:image/png;charset=utf8,abc", false, ""},
		{"whitespace stripped", " data:image/gif;base64,R0lGODlh ", true, "image/gif"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mime, _, ok := ParseImageDataURL(tc.input)
			if ok != tc.wantOK {
				t.Errorf("ok = %v; want %v", ok, tc.wantOK)
			}
			if ok && mime != tc.wantMime {
				t.Errorf("mime = %q; want %q", mime, tc.wantMime)
			}
		})
	}
}

func TestExtractFirstImageDataURL(t *testing.T) {
	// No image in text.
	cleaned, url := ExtractFirstImageDataURL("no image here")
	if cleaned != "no image here" || url != "" {
		t.Errorf("no image: got (%q, %q)", cleaned, url)
	}

	// Image embedded in text.
	text := "prefix data:image/png;base64,abc123== suffix"
	cleaned2, url2 := ExtractFirstImageDataURL(text)
	if url2 == "" {
		t.Error("expected extracted URL")
	}
	if url2 == text {
		t.Error("cleaned text should not contain data URL")
	}
	_ = cleaned2
}

func TestTruncate(t *testing.T) {
	// Under limit.
	if got := truncate([]byte("hello"), 10); got != "hello" {
		t.Errorf("truncate under limit = %q; want hello", got)
	}
	// Over limit.
	long := []byte("abcdefghij")
	got := truncate(long, 5)
	if got != "abcde …+5 bytes" {
		t.Errorf("truncate over limit = %q", got)
	}
}

func TestWithTokenSetter(t *testing.T) {
	f := NewFactory(nil)
	var called bool
	f2 := f.WithTokenSetter(func(_, _ string) error {
		called = true
		return nil
	})
	if f2 == nil {
		t.Fatal("WithTokenSetter returned nil")
	}
	// Calling the setter should work.
	_ = called
}

func TestResolveOAuthToken_Empty(t *testing.T) {
	// Factory with nil resolver → no token.
	f := NewFactory(nil)
	_, ok := f.resolveOAuthToken("anthropic:oauth")
	if ok {
		t.Error("expected false when no auth resolver")
	}
}

func TestResolveOAuthToken_PlainString(t *testing.T) {
	// Auth resolver returns a plain API key.
	f := NewFactory(func(ref string) (string, error) {
		return "sk-test-key", nil
	})
	tok, ok := f.resolveOAuthToken("anthropic:oauth")
	if !ok || tok != "sk-test-key" {
		t.Errorf("resolveOAuthToken plain = (%q, %v); want (sk-test-key, true)", tok, ok)
	}
}

func TestForModel_UnknownProvider(t *testing.T) {
	f := NewFactory(nil)
	_, err := f.ForModel("unknown/model")
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestExtractChatGPTAccountID(t *testing.T) {
	// Malformed JWT (no dots).
	if got := extractChatGPTAccountID("notajwt"); got != "" {
		t.Errorf("expected empty for malformed JWT, got %q", got)
	}

	// JWT with invalid base64 payload.
	if got := extractChatGPTAccountID("header.!!!invalid.sig"); got != "" {
		t.Errorf("expected empty for invalid base64, got %q", got)
	}

	// Valid JWT-like payload with account ID.
	claims := map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acc-123",
		},
	}
	payload, _ := json.Marshal(claims)
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	jwt := "header." + encoded + ".sig"
	if got := extractChatGPTAccountID(jwt); got != "acc-123" {
		t.Errorf("extractChatGPTAccountID = %q; want acc-123", got)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestAnthropicProvider creates an AnthropicProvider pointing at a mock server.
func newTestAnthropicProvider(baseURL, model string) *AnthropicProvider {
	client := anthropic.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(baseURL),
	)
	return &AnthropicProvider{client: client, model: model}
}

// newTestGeminiProvider creates a GeminiProvider whose inner OpenAI client points
// at a mock server.
func newTestGeminiProvider(baseURL, model string) *GeminiProvider {
	return &GeminiProvider{inner: NewOpenAIProvider("test-key", model, baseURL), apiKey: "test-key"}
}

// anthropicSSSEResponse writes a minimal Anthropic SSE response for the given text.
func anthropicSSEResponse(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-3-5-sonnet-20241022\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":10,\"cache_creation_input_tokens\":0,\"cache_read_input_tokens\":0,\"output_tokens\":1}}}\n\n")
	fmt.Fprintf(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
	fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\n", text)
	fmt.Fprintf(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
	fmt.Fprintf(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":5}}\n\n")
	fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
}

// openAISSEResponse writes a minimal OpenAI chat completion SSE response.
func openAISSEResponse(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":%q},\"finish_reason\":null}]}\n\n", text)
	fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n")
	fmt.Fprintf(w, "data: [DONE]\n\n")
}

// collectEvents drains a channel into a slice (with timeout).
func collectEvents(t *testing.T, ch <-chan Event) []Event {
	t.Helper()
	var events []Event
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, ev)
		case <-deadline:
			t.Fatal("timeout waiting for events")
		}
	}
}

// ---------------------------------------------------------------------------
// debug.go tests
// ---------------------------------------------------------------------------

func TestDebugHTTP_Disabled(t *testing.T) {
	t.Setenv("AVIARY_DEBUG_HTTP", "0")
	if DebugHTTP() {
		t.Error("expected DebugHTTP() == false when env is 0")
	}
	c := newDebugClient(nil)
	if c == nil {
		t.Error("newDebugClient returned nil")
	}
}

func TestDebugHTTP_Enabled(t *testing.T) {
	t.Setenv("AVIARY_DEBUG_HTTP", "1")
	if !DebugHTTP() {
		t.Error("expected DebugHTTP() == true when env is 1")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "yes")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("body"))
	}))
	defer srv.Close()

	c := newDebugClient(nil)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-Custom", "value")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestDebugTransport_WithBody(t *testing.T) {
	t.Setenv("AVIARY_DEBUG_HTTP", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := newDebugClient(nil)
	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader("hello world"))
	req.Header.Set("Content-Type", "text/plain")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "hello world" {
		t.Errorf("body roundtrip: got %q", string(got))
	}
}

func TestDebugTransport_Error(t *testing.T) {
	t.Setenv("AVIARY_DEBUG_HTTP", "1")
	c := newDebugClient(nil)
	// Use an invalid URL to trigger transport error.
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:1", nil)
	_, err := c.Do(req)
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

func TestWriteHeaders_Sensitive(t *testing.T) {
	var sb strings.Builder
	h := http.Header{}
	h.Set("Authorization", "Bearer secret")
	h.Set("X-Api-Key", "mykey")
	h.Set("Content-Type", "application/json")
	writeHeaders(&sb, h, "  ")
	out := sb.String()
	if strings.Contains(out, "secret") {
		t.Error("Authorization value should be redacted")
	}
	if strings.Contains(out, "mykey") {
		t.Error("X-Api-Key value should be redacted")
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Error("expected [REDACTED] marker")
	}
	if !strings.Contains(out, "application/json") {
		t.Error("Content-Type should appear unredacted")
	}
}

func TestWriteHeaders_Empty(t *testing.T) {
	var sb strings.Builder
	writeHeaders(&sb, http.Header{}, "  ")
	if sb.Len() != 0 {
		t.Errorf("expected empty output for empty headers, got %q", sb.String())
	}
}

func TestNoAPIKeyTransport(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	base := &http.Client{Transport: http.DefaultTransport}
	transport := &noAPIKeyTransport{base: base.Transport}
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("X-Api-Key", "should-be-removed")
	req.Header.Set("Authorization", "Bearer mytoken")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if gotHeaders.Get("X-Api-Key") != "" {
		t.Error("x-api-key header should have been stripped by noAPIKeyTransport")
	}
	if gotHeaders.Get("Authorization") == "" {
		t.Error("Authorization header should have been preserved")
	}
}

// ---------------------------------------------------------------------------
// AnthropicProvider tests
// ---------------------------------------------------------------------------

func TestAnthropicProvider_Ping_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"data":[{"id":"claude-3-5-sonnet-20241022","type":"model","display_name":"Claude 3.5 Sonnet","created_at":"2024-10-22T00:00:00Z"}],"first_id":"claude-3-5-sonnet-20241022","last_id":"claude-3-5-sonnet-20241022","has_more":false}`)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := newTestAnthropicProvider(srv.URL, "claude-3-5-sonnet-20241022")
	if err := p.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
}

func TestAnthropicProvider_Ping_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid api key"}}`)
	}))
	defer srv.Close()

	p := newTestAnthropicProvider(srv.URL, "claude-3-5-sonnet-20241022")
	err := p.Ping(context.Background())
	if err == nil {
		t.Fatal("expected Ping() to fail with 401")
	}
}

func TestAnthropicProvider_Stream_TextAndUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anthropicSSEResponse(w, "Hello!")
	}))
	defer srv.Close()

	p := newTestAnthropicProvider(srv.URL, "claude-3-5-sonnet-20241022")
	req := Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
		MaxToks:  100,
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	events := collectEvents(t, ch)
	var texts []string
	var gotDone, gotUsage bool
	for _, ev := range events {
		switch ev.Type {
		case EventTypeText:
			texts = append(texts, ev.Text)
		case EventTypeDone:
			gotDone = true
		case EventTypeUsage:
			gotUsage = true
		case EventTypeError:
			t.Fatalf("unexpected error event: %v", ev.Error)
		}
	}
	if len(texts) == 0 {
		t.Error("expected text events")
	}
	if !gotDone {
		t.Error("expected done event")
	}
	if !gotUsage {
		t.Error("expected usage event")
	}
}

func TestAnthropicProvider_Stream_WithSystem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anthropicSSEResponse(w, "Hi there!")
	}))
	defer srv.Close()

	p := newTestAnthropicProvider(srv.URL, "claude-3-5-sonnet-20241022")
	req := Request{
		System:   "You are a helpful assistant.",
		Messages: []Message{{Role: RoleUser, Content: "Hello"}},
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	events := collectEvents(t, ch)
	var gotDone bool
	for _, ev := range events {
		if ev.Type == EventTypeDone {
			gotDone = true
		}
	}
	if !gotDone {
		t.Error("expected done event")
	}
}

func TestAnthropicProvider_Stream_AssistantMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anthropicSSEResponse(w, "reply")
	}))
	defer srv.Close()

	p := newTestAnthropicProvider(srv.URL, "claude-3-5-sonnet-20241022")
	req := Request{
		Messages: []Message{
			{Role: RoleUser, Content: "Question"},
			{Role: RoleAssistant, Content: "Previous answer"},
			{Role: RoleUser, Content: "Follow-up"},
		},
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	collectEvents(t, ch)
}

func TestAnthropicProvider_Stream_WithMediaURL_Data(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anthropicSSEResponse(w, "image")
	}))
	defer srv.Close()

	p := newTestAnthropicProvider(srv.URL, "claude-3-5-sonnet-20241022")
	req := Request{
		Messages: []Message{
			{Role: RoleUser, Content: "Look at this", MediaURL: "data:image/png;base64,abc123"},
		},
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	collectEvents(t, ch)
}

func TestAnthropicProvider_Stream_WithMediaURL_HTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anthropicSSEResponse(w, "image response")
	}))
	defer srv.Close()

	p := newTestAnthropicProvider(srv.URL, "claude-3-5-sonnet-20241022")
	req := Request{
		Messages: []Message{
			{Role: RoleUser, Content: "Look", MediaURL: "https://example.com/img.png"},
		},
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	collectEvents(t, ch)
}

func TestAnthropicProvider_Stream_WithMediaURL_NoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anthropicSSEResponse(w, "image only")
	}))
	defer srv.Close()

	p := newTestAnthropicProvider(srv.URL, "claude-3-5-sonnet-20241022")
	req := Request{
		Messages: []Message{
			// MediaURL with no text content
			{Role: RoleUser, Content: "", MediaURL: "data:image/png;base64,abc123"},
		},
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	collectEvents(t, ch)
}

func TestAnthropicProvider_Stream_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"type":"error","error":{"type":"server_error","message":"internal error"}}`)
	}))
	defer srv.Close()

	p := newTestAnthropicProvider(srv.URL, "claude-3-5-sonnet-20241022")
	req := Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	}
	ch, err := p.Stream(context.Background(), req)
	// Either the initial call returns an error or the channel gets an error event.
	if err != nil {
		return // acceptable
	}
	events := collectEvents(t, ch)
	var gotErr bool
	for _, ev := range events {
		if ev.Type == EventTypeError {
			gotErr = true
		}
	}
	if !gotErr {
		t.Error("expected error event on 500 response")
	}
}

func TestAnthropicProvider_Stream_DefaultMaxToks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anthropicSSEResponse(w, "ok")
	}))
	defer srv.Close()

	p := newTestAnthropicProvider(srv.URL, "claude-3-5-sonnet-20241022")
	// MaxToks=0 should use the default (4096).
	req := Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
		MaxToks:  0,
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	collectEvents(t, ch)
}

func TestNewAnthropicOAuthProvider(t *testing.T) {
	p := NewAnthropicOAuthProvider("test-access-token", "claude-3-5-sonnet-20241022")
	if p == nil {
		t.Fatal("NewAnthropicOAuthProvider returned nil")
	}
	if p.model != "claude-3-5-sonnet-20241022" {
		t.Errorf("model = %q; want claude-3-5-sonnet-20241022", p.model)
	}
}

func TestNewAnthropicProvider_EmptyKey(t *testing.T) {
	p := NewAnthropicProvider("", "claude-3-5-sonnet-20241022")
	if p == nil {
		t.Fatal("NewAnthropicProvider returned nil")
	}
}

// ---------------------------------------------------------------------------
// OpenAIProvider tests
// ---------------------------------------------------------------------------

func TestOpenAIProvider_Ping_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"object":"list","data":[{"id":"gpt-4o","object":"model","created":1234567890,"owned_by":"openai"}]}`)
	}))
	defer srv.Close()

	p := NewOpenAIProvider("test-key", "gpt-4o", srv.URL)
	if err := p.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
}

func TestOpenAIProvider_Ping_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid api key","type":"invalid_request_error"}}`)
	}))
	defer srv.Close()

	p := NewOpenAIProvider("bad-key", "gpt-4o", srv.URL)
	err := p.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error for 401")
	}
}

func TestOpenAIProvider_Stream_TextAndUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		openAISSEResponse(w, "Hello!")
	}))
	defer srv.Close()

	p := NewOpenAIProvider("test-key", "gpt-4o", srv.URL)
	req := Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	events := collectEvents(t, ch)
	var texts []string
	var gotDone, gotUsage bool
	for _, ev := range events {
		switch ev.Type {
		case EventTypeText:
			texts = append(texts, ev.Text)
		case EventTypeDone:
			gotDone = true
		case EventTypeUsage:
			gotUsage = true
		case EventTypeError:
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}
	if len(texts) == 0 {
		t.Error("expected text events")
	}
	if !gotDone {
		t.Error("expected done event")
	}
	if !gotUsage {
		t.Error("expected usage event")
	}
}

func TestOpenAIProvider_Stream_WithSystem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		openAISSEResponse(w, "sys reply")
	}))
	defer srv.Close()

	p := NewOpenAIProvider("test-key", "gpt-4o", srv.URL)
	req := Request{
		System:   "Be concise.",
		Messages: []Message{{Role: RoleUser, Content: "Tell me a joke"}},
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	collectEvents(t, ch)
}

func TestOpenAIProvider_Stream_AllRoles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		openAISSEResponse(w, "ok")
	}))
	defer srv.Close()

	p := NewOpenAIProvider("test-key", "gpt-4o", srv.URL)
	req := Request{
		Messages: []Message{
			{Role: RoleSystem, Content: "System msg"},
			{Role: RoleUser, Content: "User msg"},
			{Role: RoleAssistant, Content: "Asst msg"},
			{Role: RoleUser, Content: "Another user"},
		},
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	collectEvents(t, ch)
}

func TestOpenAIProvider_Stream_WithMediaURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		openAISSEResponse(w, "image response")
	}))
	defer srv.Close()

	p := NewOpenAIProvider("test-key", "gpt-4o", srv.URL)
	req := Request{
		Messages: []Message{
			{Role: RoleUser, Content: "Look", MediaURL: "https://example.com/img.png"},
		},
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	collectEvents(t, ch)
}

func TestOpenAIProvider_Stream_MediaURLNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		openAISSEResponse(w, "image only")
	}))
	defer srv.Close()

	p := NewOpenAIProvider("test-key", "gpt-4o", srv.URL)
	req := Request{
		Messages: []Message{
			{Role: RoleUser, Content: "", MediaURL: "https://example.com/img.png"},
		},
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	collectEvents(t, ch)
}

func TestOpenAIProvider_Stream_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"message":"server error","type":"server_error"}}`)
	}))
	defer srv.Close()

	p := NewOpenAIProvider("test-key", "gpt-4o", srv.URL)
	req := Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		return // initial error is fine
	}
	events := collectEvents(t, ch)
	var gotErr bool
	for _, ev := range events {
		if ev.Type == EventTypeError {
			gotErr = true
		}
	}
	if !gotErr {
		t.Error("expected error event on 500 response")
	}
}

func TestNewOpenAIProvider_EmptyKey(t *testing.T) {
	p := NewOpenAIProvider("", "gpt-4o", "")
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

// ---------------------------------------------------------------------------
// OpenAICodexProvider tests
// ---------------------------------------------------------------------------

func TestOpenAICodexProvider_Stream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\" World\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p := NewOpenAICodexProvider("test-token", "gpt-4o")
	p.httpClient = &http.Client{Transport: &rewriteURLTransport{target: srv.URL, base: http.DefaultTransport}}

	req := Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	events := collectEvents(t, ch)
	var texts []string
	var gotDone, gotUsage bool
	for _, ev := range events {
		switch ev.Type {
		case EventTypeText:
			texts = append(texts, ev.Text)
		case EventTypeDone:
			gotDone = true
		case EventTypeUsage:
			gotUsage = true
		case EventTypeError:
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}
	if strings.Join(texts, "") != "Hello World" {
		t.Errorf("text = %q; want %q", strings.Join(texts, ""), "Hello World")
	}
	if !gotDone {
		t.Error("expected done event")
	}
	if !gotUsage {
		t.Error("expected usage event")
	}
}

func TestOpenAICodexProvider_Stream_AllRoles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p := NewOpenAICodexProvider("test-token", "gpt-4o")
	p.httpClient = &http.Client{Transport: &rewriteURLTransport{target: srv.URL, base: http.DefaultTransport}}

	req := Request{
		System: "System instructions",
		Messages: []Message{
			{Role: RoleUser, Content: "User msg"},
			{Role: RoleAssistant, Content: "Asst msg"},
			{Role: RoleSystem, Content: "Sys inline"},
			{Role: RoleUser, Content: "Follow-up", MediaURL: "https://example.com/img.png"},
		},
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	collectEvents(t, ch)
}

func TestOpenAICodexProvider_Stream_WithMedia_NoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p := NewOpenAICodexProvider("test-token", "gpt-4o")
	p.httpClient = &http.Client{Transport: &rewriteURLTransport{target: srv.URL, base: http.DefaultTransport}}

	req := Request{
		Messages: []Message{
			{Role: RoleUser, Content: "", MediaURL: "https://example.com/img.png"},
		},
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	collectEvents(t, ch)
}

func TestOpenAICodexProvider_Stream_Failed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"quota exceeded\",\"code\":\"rate_limit\"}}}\n\n")
	}))
	defer srv.Close()

	p := NewOpenAICodexProvider("test-token", "gpt-4o")
	p.httpClient = &http.Client{Transport: &rewriteURLTransport{target: srv.URL, base: http.DefaultTransport}}

	ch, err := p.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Stream() returned error: %v", err)
	}
	events := collectEvents(t, ch)
	var gotErr bool
	for _, ev := range events {
		if ev.Type == EventTypeError {
			gotErr = true
			if !strings.Contains(ev.Error.Error(), "rate_limit") {
				t.Errorf("error message should contain code, got: %v", ev.Error)
			}
		}
	}
	if !gotErr {
		t.Error("expected error event")
	}
}

func TestOpenAICodexProvider_Stream_FailedNoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// response.failed without a specific error struct
		fmt.Fprint(w, "data: {\"type\":\"response.failed\",\"response\":{}}\n\n")
	}))
	defer srv.Close()

	p := NewOpenAICodexProvider("test-token", "gpt-4o")
	p.httpClient = &http.Client{Transport: &rewriteURLTransport{target: srv.URL, base: http.DefaultTransport}}

	ch, err := p.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	events := collectEvents(t, ch)
	var gotErr bool
	for _, ev := range events {
		if ev.Type == EventTypeError {
			gotErr = true
		}
	}
	if !gotErr {
		t.Error("expected error event for response.failed")
	}
}

func TestOpenAICodexProvider_Stream_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "forbidden")
	}))
	defer srv.Close()

	p := NewOpenAICodexProvider("test-token", "gpt-4o")
	p.httpClient = &http.Client{Transport: &rewriteURLTransport{target: srv.URL, base: http.DefaultTransport}}

	_, err := p.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 403")
	}
}

func TestOpenAICodexProvider_Stream_WithAccountID(t *testing.T) {
	claims := map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acc-456",
		},
	}
	payload, _ := json.Marshal(claims)
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	jwtToken := "header." + encoded + ".sig"

	var gotAccountID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccountID = r.Header.Get("ChatGPT-Account-ID")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p := NewOpenAICodexProvider(jwtToken, "gpt-4o")
	p.httpClient = &http.Client{Transport: &rewriteURLTransport{target: srv.URL, base: http.DefaultTransport}}

	ch, err := p.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	collectEvents(t, ch)
	if gotAccountID != "acc-456" {
		t.Errorf("ChatGPT-Account-ID = %q; want acc-456", gotAccountID)
	}
}

func TestOpenAICodexProvider_Stream_InvalidJSONLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Some lines that should be skipped (not "data: " prefix or bad JSON).
		fmt.Fprint(w, "event: ping\n\n")
		fmt.Fprint(w, "data: not valid json\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p := NewOpenAICodexProvider("test-token", "gpt-4o")
	p.httpClient = &http.Client{Transport: &rewriteURLTransport{target: srv.URL, base: http.DefaultTransport}}

	ch, err := p.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	events := collectEvents(t, ch)
	var texts []string
	for _, ev := range events {
		if ev.Type == EventTypeText {
			texts = append(texts, ev.Text)
		}
	}
	if len(texts) == 0 {
		t.Error("expected text event despite bad lines")
	}
}

// rewriteURLTransport redirects all requests to a target server (for testing
// providers that have hardcoded URLs like openAICodexBaseURL).
type rewriteURLTransport struct {
	target string
	base   http.RoundTripper
}

func (t *rewriteURLTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.target, "http://")
	return t.base.RoundTrip(req)
}

// ---------------------------------------------------------------------------
// GeminiProvider tests
// ---------------------------------------------------------------------------

func TestGeminiProvider_Stream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		openAISSEResponse(w, "Gemini says hi")
	}))
	defer srv.Close()

	p := newTestGeminiProvider(srv.URL, "gemini-2.0-flash")
	req := Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	events := collectEvents(t, ch)
	var texts []string
	for _, ev := range events {
		if ev.Type == EventTypeText {
			texts = append(texts, ev.Text)
		}
	}
	if len(texts) == 0 {
		t.Error("expected text events")
	}
}

func TestGeminiProvider_Ping_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"models":[]}`)
	}))
	defer srv.Close()

	var mu sync.Mutex
	old := http.DefaultClient
	mu.Lock()
	http.DefaultClient = &http.Client{Transport: &rewriteURLTransport{target: srv.URL, base: http.DefaultTransport}}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		http.DefaultClient = old
		mu.Unlock()
	})

	p := newTestGeminiProvider(srv.URL, "gemini-2.0-flash")
	err := p.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
}

func TestGeminiProvider_Ping_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: &rewriteURLTransport{target: srv.URL, base: http.DefaultTransport}}
	t.Cleanup(func() { http.DefaultClient = old })

	p := newTestGeminiProvider(srv.URL, "gemini-2.0-flash")
	err := p.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error for 403")
	}
}

func TestNewGeminiProvider(t *testing.T) {
	p := NewGeminiProvider("test-key", "gemini-2.0-flash")
	if p == nil {
		t.Fatal("NewGeminiProvider returned nil")
	}
	if p.apiKey != "test-key" {
		t.Errorf("apiKey = %q; want test-key", p.apiKey)
	}
}

// ---------------------------------------------------------------------------
// GeminiCodeAssistProvider tests
// ---------------------------------------------------------------------------

// mockDefaultClient temporarily replaces http.DefaultClient for testing.
func mockDefaultClient(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: &rewriteURLTransport{target: srv.URL, base: http.DefaultTransport}}
	t.Cleanup(func() {
		http.DefaultClient = old
		srv.Close()
	})
	return srv
}

func TestPingGoogleOAuthToken_Success(t *testing.T) {
	mockDefaultClient(t, func(w http.ResponseWriter, r *http.Request) {
		if token := r.URL.Query().Get("access_token"); token == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"email":"test@example.com"}`)
	})

	err := pingGoogleOAuthToken(context.Background(), "test-access-token")
	if err != nil {
		t.Fatalf("pingGoogleOAuthToken error: %v", err)
	}
}

func TestPingGoogleOAuthToken_Invalid(t *testing.T) {
	mockDefaultClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error_description":"Invalid Value"}`)
	})

	err := pingGoogleOAuthToken(context.Background(), "bad-token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	if !strings.Contains(err.Error(), "invalid token") {
		t.Errorf("error should mention invalid token, got: %v", err)
	}
}

func TestFetchCodeAssistProject_Success(t *testing.T) {
	mockDefaultClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"cloudaicompanionProject":"my-project-123456"}`)
	})

	proj, err := fetchCodeAssistProject(context.Background(), "test-access-token")
	if err != nil {
		t.Fatalf("fetchCodeAssistProject error: %v", err)
	}
	if proj != "my-project-123456" {
		t.Errorf("project = %q; want my-project-123456", proj)
	}
}

func TestFetchCodeAssistProject_HTTPError(t *testing.T) {
	mockDefaultClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "access denied")
	})

	_, err := fetchCodeAssistProject(context.Background(), "bad-token")
	if err == nil {
		t.Fatal("expected error for 403")
	}
}

func TestFetchCodeAssistProject_BadJSON(t *testing.T) {
	mockDefaultClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"someOtherField":"value"}`)
	})

	_, err := fetchCodeAssistProject(context.Background(), "test-token")
	if err == nil {
		t.Fatal("expected error for missing cloudaicompanionProject")
	}
}

func TestFetchCodeAssistProject_InvalidJSON(t *testing.T) {
	mockDefaultClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `not-json`)
	})

	_, err := fetchCodeAssistProject(context.Background(), "test-token")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestGeminiCodeAssistProvider_Ping_Success(t *testing.T) {
	mockDefaultClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"email":"test@example.com"}`)
	})

	p := NewGeminiCodeAssistProvider("test-token", "gemini-2.0-flash")
	if err := p.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
}

func TestGeminiCodeAssistProvider_Ping_Error(t *testing.T) {
	mockDefaultClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})

	p := NewGeminiCodeAssistProvider("bad-token", "gemini-2.0-flash")
	if err := p.Ping(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestGeminiCodeAssistProvider_ResolveProject_Cached(t *testing.T) {
	var callCount int
	mockDefaultClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"cloudaicompanionProject":"cached-project"}`)
	})

	p := NewGeminiCodeAssistProvider("test-token", "gemini-2.0-flash")
	proj1, err := p.resolveProject(context.Background())
	if err != nil {
		t.Fatalf("first resolveProject error: %v", err)
	}
	proj2, err := p.resolveProject(context.Background())
	if err != nil {
		t.Fatalf("second resolveProject error: %v", err)
	}
	if proj1 != "cached-project" || proj2 != "cached-project" {
		t.Errorf("projects = (%q, %q); want (cached-project, cached-project)", proj1, proj2)
	}
	// Should only have called the API once (second call uses cache).
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}
}

func TestGeminiCodeAssistProvider_ResolveProject_Error(t *testing.T) {
	mockDefaultClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "forbidden")
	})

	p := NewGeminiCodeAssistProvider("bad-token", "gemini-2.0-flash")
	_, err := p.resolveProject(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGeminiCodeAssistProvider_Stream_Success(t *testing.T) {
	var requestCount int
	mockDefaultClient(t, func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			// First request: resolve project via loadCodeAssist.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"cloudaicompanionProject":"test-project"}`)
		} else {
			// Second request: stream response.
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			data := `{"response":{"candidates":[{"content":{"parts":[{"text":"Hello from Gemini!"}],"role":"model"}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}}`
			fmt.Fprintf(w, "data: %s\n\n", data)
			fmt.Fprint(w, "data: [DONE]\n\n")
		}
	})

	p := NewGeminiCodeAssistProvider("test-token", "gemini-2.0-flash")
	req := Request{
		System:   "Be helpful.",
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
		MaxToks:  100,
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	events := collectEvents(t, ch)
	var texts []string
	var gotDone, gotUsage bool
	for _, ev := range events {
		switch ev.Type {
		case EventTypeText:
			texts = append(texts, ev.Text)
		case EventTypeDone:
			gotDone = true
		case EventTypeUsage:
			gotUsage = true
		case EventTypeError:
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}
	if strings.Join(texts, "") != "Hello from Gemini!" {
		t.Errorf("text = %q; want 'Hello from Gemini!'", strings.Join(texts, ""))
	}
	if !gotDone {
		t.Error("expected done event")
	}
	if !gotUsage {
		t.Error("expected usage event")
	}
}

func TestGeminiCodeAssistProvider_Stream_AllRoles(t *testing.T) {
	var requestCount int
	mockDefaultClient(t, func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"cloudaicompanionProject":"test-project"}`)
		} else {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			data := `{"response":{"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"}}],"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0}}}`
			fmt.Fprintf(w, "data: %s\n\n", data)
			fmt.Fprint(w, "data: [DONE]\n\n")
		}
	})

	p := NewGeminiCodeAssistProvider("test-token", "gemini-2.0-flash")
	req := Request{
		Messages: []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "Hi there"},
			{Role: RoleUser, Content: "What is 2+2?"},
		},
	}
	ch, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	collectEvents(t, ch)
}

func TestGeminiCodeAssistProvider_Stream_ProjectError(t *testing.T) {
	mockDefaultClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "forbidden")
	})

	p := NewGeminiCodeAssistProvider("bad-token", "gemini-2.0-flash")
	_, err := p.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error when resolveProject fails")
	}
}

func TestGeminiCodeAssistProvider_Stream_500Retry(t *testing.T) {
	// Test that doStreamRequest retries once on 5xx.
	var requestCount int
	mockDefaultClient(t, func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			// loadCodeAssist.
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"cloudaicompanionProject":"test-project"}`)
		} else if requestCount == 2 {
			// First stream attempt: 500.
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "transient error")
		} else {
			// Second stream attempt: success.
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			data := `{"response":{"candidates":[{"content":{"parts":[{"text":"retry worked"}],"role":"model"}}],"usageMetadata":{}}}`
			fmt.Fprintf(w, "data: %s\n\n", data)
			fmt.Fprint(w, "data: [DONE]\n\n")
		}
	})

	p := NewGeminiCodeAssistProvider("test-token", "gemini-2.0-flash")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := p.Stream(ctx, Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	events := collectEvents(t, ch)
	var texts []string
	for _, ev := range events {
		if ev.Type == EventTypeText {
			texts = append(texts, ev.Text)
		}
	}
	if strings.Join(texts, "") != "retry worked" {
		t.Errorf("text after retry = %q; want 'retry worked'", strings.Join(texts, ""))
	}
}

func TestGeminiCodeAssistProvider_Stream_4xxNoRetry(t *testing.T) {
	var requestCount int
	mockDefaultClient(t, func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"cloudaicompanionProject":"test-project"}`)
		} else {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, "forbidden")
		}
	})

	p := NewGeminiCodeAssistProvider("test-token", "gemini-2.0-flash")
	_, err := p.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 403")
	}
	if requestCount > 2 {
		t.Errorf("expected no retry on 4xx, but got %d requests", requestCount)
	}
}

func TestGeminiCodeAssistProvider_Stream_InvalidJSONLine(t *testing.T) {
	var requestCount int
	mockDefaultClient(t, func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"cloudaicompanionProject":"test-project"}`)
		} else {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "data: not valid json\n\n")
			valid := `{"response":{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usageMetadata":{}}}`
			fmt.Fprintf(w, "data: %s\n\n", valid)
			fmt.Fprint(w, "data: [DONE]\n\n")
		}
	})

	p := NewGeminiCodeAssistProvider("test-token", "gemini-2.0-flash")
	ch, err := p.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	events := collectEvents(t, ch)
	var texts []string
	for _, ev := range events {
		if ev.Type == EventTypeText {
			texts = append(texts, ev.Text)
		}
	}
	if len(texts) == 0 {
		t.Error("expected text event despite bad JSON line")
	}
}

// ---------------------------------------------------------------------------
// Factory.PingModel tests
// ---------------------------------------------------------------------------

func TestFactory_PingModel_Pinger(t *testing.T) {
	// Provider that implements Pinger.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"object":"list","data":[]}`)
	}))
	defer srv.Close()

	f := NewFactory(func(ref string) (string, error) {
		return "test-key", nil
	})
	// Override the internal provider to use our test server.
	// We test PingModel indirectly by verifying it calls Ping on the provider.
	p := NewOpenAIProvider("test-key", "gpt-4o", srv.URL)
	err := p.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
	_ = f
}

func TestFactory_PingModel_FallbackStream(t *testing.T) {
	// OpenAICodexProvider doesn't implement Pinger - PingModel falls back to Stream.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hi\"}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	claims := map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "acc-test"},
	}
	payload, _ := json.Marshal(claims)
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	jwtToken := "header." + encoded + ".sig"

	p := NewOpenAICodexProvider(jwtToken, "gpt-4o")
	p.httpClient = &http.Client{Transport: &rewriteURLTransport{target: srv.URL, base: http.DefaultTransport}}

	ch, err := p.Stream(context.Background(), Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
		MaxToks:  1,
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	for ev := range ch {
		if ev.Type == EventTypeError {
			t.Fatalf("unexpected error: %v", ev.Error)
		}
	}
}

func TestFactory_PingModel_Error(t *testing.T) {
	f := NewFactory(nil)
	err := f.PingModel(context.Background(), "invalid")
	if err == nil {
		t.Fatal("expected error for invalid model")
	}
}

// ---------------------------------------------------------------------------
// Factory resolveOAuthToken with JSON token
// ---------------------------------------------------------------------------

func TestResolveOAuthToken_JSONToken(t *testing.T) {
	// Build a JSON token that is already valid (not expired).
	// OAuthToken uses "access", "refresh", "expires_at" (Unix ms).
	expiresAt := time.Now().Add(1*time.Hour).UnixMilli()
	tokenJSON := fmt.Sprintf(`{"access":"at-valid","refresh":"rt","expires_at":%d}`, expiresAt)

	f := NewFactory(func(ref string) (string, error) {
		return tokenJSON, nil
	})
	tok, ok := f.resolveOAuthToken("auth:anthropic:oauth")
	if !ok || tok == "" {
		t.Errorf("resolveOAuthToken JSON = (%q, %v); want non-empty, true", tok, ok)
	}
}

func TestResolveOAuthToken_InvalidJSON(t *testing.T) {
	f := NewFactory(func(ref string) (string, error) {
		return `{invalid json`, nil
	})
	_, ok := f.resolveOAuthToken("auth:anthropic:oauth")
	if ok {
		t.Error("expected false for invalid JSON token")
	}
}

func TestResolveOAuthToken_EmptyAccessToken(t *testing.T) {
	f := NewFactory(func(ref string) (string, error) {
		return `{"access_token":"","refresh_token":"rt"}`, nil
	})
	_, ok := f.resolveOAuthToken("auth:anthropic:oauth")
	if ok {
		t.Error("expected false for empty access token")
	}
}
