// Package llm provides adapters for language model providers.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/auth"
)

// ProviderOptions carries provider-specific configuration loaded from config.
type ProviderOptions struct {
	Auth    string
	BaseURI string
}

// Role identifies who authored a message.
type Role string

// Role values.
const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// Message is a single message in a conversation.
type Message struct {
	Role     Role
	Content  string
	MediaURL string
	ToolCall *ToolCall
	Result   *ToolResult
}

// Request is the input to an LLM provider.
type Request struct {
	Model              string
	Messages           []Message
	System             string // optional system prompt
	MaxToks            int    // 0 = provider default
	Stream             bool   // whether to stream
	PreviousResponseID string // provider-native conversation ID; when set, Messages should contain only the new user turn
	Tools              []ToolDefinition
}

// ToolDefinition describes a callable function/tool for provider-native tool APIs.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema any
	Examples    []map[string]any
}

// ToolCall is a provider-native structured tool invocation emitted by the model.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// ToolResult is a provider-native tool result message supplied back to the model.
type ToolResult struct {
	ToolCallID string
	Name       string
	Content    string
	IsError    bool
}

// Usage holds token-count metrics from an LLM call.
type Usage struct {
	InputTokens      int // prompt tokens
	OutputTokens     int // completion tokens
	CacheReadTokens  int // (Anthropic) prompt-cache read tokens
	CacheWriteTokens int // (Anthropic) prompt-cache creation tokens
}

// Event is a single streaming event from an LLM provider.
type Event struct {
	Type       EventType
	Text       string // partial text (EventTypeText)
	MediaURL   string // image data URL (EventTypeMedia)
	Error      error  // (EventTypeError)
	Usage      *Usage // token counts (EventTypeUsage)
	ResponseID string // provider-native response/conversation ID (EventTypeDone); non-empty only when supported
	ToolCall   *ToolCall
}

// EventType identifies a streaming event.
type EventType string

// EventType values.
const (
	EventTypeText     EventType = "text"
	EventTypeMedia    EventType = "media"
	EventTypeError    EventType = "error"
	EventTypeUsage    EventType = "usage" // emitted once before EventTypeDone
	EventTypeToolCall EventType = "tool_call"
	EventTypeDone     EventType = "done"
)

// Provider is the interface all LLM backends implement.
type Provider interface {
	// Stream sends req to the model and returns a channel of events.
	// The channel is closed when the stream ends (EventTypeDone or EventTypeError).
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}

// Factory creates a Provider from a model string of the form "<provider>/<name>".
type Factory struct {
	authResolver          func(ref string) (string, error)
	providerOptionsLookup func(provider string) (ProviderOptions, bool)
	tokenSetter           func(key, value string) error // optional: persists refreshed tokens
}

// NewFactory creates a Factory. authResolver resolves "auth:<x>:<y>" references.
func NewFactory(authResolver func(string) (string, error)) *Factory {
	return &Factory{authResolver: authResolver}
}

// WithTokenSetter sets a callback that persists a refreshed OAuth token.
// key is the bare credential key without the "auth:" prefix (e.g. "anthropic:oauth").
func (f *Factory) WithTokenSetter(setter func(key, value string) error) *Factory {
	f.tokenSetter = setter
	return f
}

// WithProviderOptionsResolver sets a callback that returns provider-specific
// config for a provider name such as "vllm".
func (f *Factory) WithProviderOptionsResolver(lookup func(provider string) (ProviderOptions, bool)) *Factory {
	f.providerOptionsLookup = lookup
	return f
}

// resolveOAuthToken tries to resolve a stored OAuth token for a provider.
// If the token is expired (or forceRefresh is true) it is automatically
// refreshed via the provider's refresh-token flow and the new token is
// persisted via tokenSetter.
// Returns (accessToken, true) if a usable token is found.
func (f *Factory) resolveOAuthToken(providerKey string, forceRefresh bool) (string, bool) {
	raw, err := f.resolveAuth(providerKey)
	if err != nil || raw == "" {
		return "", false
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed != "" && !strings.HasPrefix(trimmed, "{") {
		return trimmed, true
	}

	var tok auth.OAuthToken
	if err := json.Unmarshal([]byte(raw), &tok); err != nil || tok.AccessToken == "" {
		return "", false
	}
	// Auto-refresh when the token is within 30 s of expiry, or when forced.
	if (tok.IsExpired() || forceRefresh) && tok.RefreshToken != "" {
		if refreshed := f.refreshOAuthToken(providerKey, &tok); refreshed != nil {
			slog.Info("llm: OAuth token refreshed", "provider", providerKey, "forced", forceRefresh)
			return refreshed.AccessToken, true
		}
		// Refresh failed; fall through and try the stale token — the API
		// will return a 401 anyway, which gives a clearer error message.
	}
	return tok.AccessToken, true
}

// refreshOAuthToken performs the provider-specific token refresh, persists the
// new token via tokenSetter, and returns the refreshed OAuthToken.
func (f *Factory) refreshOAuthToken(providerKey string, tok *auth.OAuthToken) *auth.OAuthToken {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var newTok *auth.OAuthToken
	var err error
	switch {
	case strings.Contains(providerKey, "anthropic"):
		newTok, err = auth.AnthropicRefresh(ctx, tok.RefreshToken)
	case strings.Contains(providerKey, "openai"):
		newTok, err = auth.OpenAIRefresh(ctx, tok.RefreshToken)
	case strings.Contains(providerKey, "google"), strings.Contains(providerKey, "gemini"):
		newTok, err = auth.GeminiRefresh(ctx, tok.RefreshToken)
	default:
		return nil
	}
	if err != nil {
		slog.Warn("llm: OAuth token refresh failed", "provider", providerKey, "err", err)
		return nil
	}
	// Persist under the bare key (strip the "auth:" resolver prefix).
	if f.tokenSetter != nil {
		key := strings.TrimPrefix(providerKey, "auth:")
		if data, marshalErr := json.Marshal(newTok); marshalErr == nil {
			if setErr := f.tokenSetter(key, string(data)); setErr != nil {
				slog.Warn("llm: failed to persist refreshed token", "provider", providerKey, "err", setErr)
			}
		}
	}
	return newTok
}

// ForModel returns a Provider for the given model string.
// model format: "anthropic/claude-sonnet-4.5", "openai/gpt-4o", "google-gemini/gemini-2.5-pro",
// "google/gemini-2.5-pro" (API key), "stdio/claude" (subprocess), etc.
func (f *Factory) ForModel(model string) (Provider, error) {
	return f.forModel(model, false)
}

// ForModelForceRefresh is like ForModel but forces any OAuth token to be
// refreshed before creating the provider. Use this after receiving a 401 to
// obtain a fresh token without waiting for the local expiry heuristic.
func (f *Factory) ForModelForceRefresh(model string) (Provider, error) {
	return f.forModel(model, true)
}

func (f *Factory) forModel(model string, forceRefresh bool) (Provider, error) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid model %q: expected <provider>/<name>", model)
	}
	provider, name := parts[0], parts[1]

	switch provider {
	case "anthropic":
		if accessToken, ok := f.resolveOAuthToken("auth:anthropic:oauth", forceRefresh); ok {
			return NewAnthropicOAuthProvider(accessToken, name), nil
		}
		apiKey, err := f.resolveAuth("auth:anthropic:default")
		if err != nil {
			return nil, fmt.Errorf("anthropic auth: %w", err)
		}
		return NewAnthropicProvider(apiKey, name), nil

	case "openai":
		apiKey, err := f.resolveAuth("auth:openai:default")
		if err != nil {
			return nil, fmt.Errorf("openai auth: %w", err)
		}
		return NewOpenAIProvider(apiKey, name, ""), nil

	case "openai-codex":
		if accessToken, ok := f.resolveOAuthToken("auth:openai:oauth", forceRefresh); ok {
			return NewOpenAICodexProvider(accessToken, name), nil
		}
		return nil, fmt.Errorf("openai-codex auth: missing OAuth token; run 'aviary auth login openai'")

	case "google-gemini":
		if accessToken, ok := f.resolveOAuthToken("auth:gemini:oauth", forceRefresh); ok {
			return NewGeminiCodeAssistProvider(accessToken, name), nil
		}
		return nil, fmt.Errorf("google-gemini auth: missing Google (Gemini) OAuth token; run 'aviary auth login gemini'")

	case "github-copilot":
		if accessToken, ok := f.resolveOAuthToken("auth:github-copilot:oauth", forceRefresh); ok {
			return NewCopilotProvider(accessToken, name), nil
		}
		apiKey, err := f.resolveAuth("auth:github-copilot:default")
		if err != nil {
			return nil, fmt.Errorf("github-copilot auth: %w", err)
		}
		if apiKey == "" {
			return nil, fmt.Errorf("github-copilot auth: missing credentials; set GH_TOKEN/GITHUB_TOKEN or run 'aviary auth login github-copilot'")
		}
		return NewCopilotProvider(apiKey, name), nil

	case "google":
		apiKey, err := f.resolveAuth("auth:gemini:default")
		if err != nil {
			return nil, fmt.Errorf("google auth: %w", err)
		}
		return NewGeminiProvider(apiKey, name), nil

	case "gemini-code-assist":
		if accessToken, ok := f.resolveOAuthToken("auth:gemini:oauth", forceRefresh); ok {
			return NewGeminiCodeAssistProvider(accessToken, name), nil
		}
		return nil, fmt.Errorf("gemini-code-assist: missing Google (Gemini) OAuth token; run 'aviary auth login gemini'")

	case "vllm":
		opts, _ := f.providerOptions("vllm")
		if strings.TrimSpace(opts.BaseURI) == "" {
			return nil, fmt.Errorf("vllm: missing models.providers.vllm.base_uri")
		}
		apiKey, err := f.resolveOptionalAuth(opts.Auth)
		if err != nil {
			return nil, fmt.Errorf("vllm auth: %w", err)
		}
		return NewVLLMProvider(apiKey, name, opts.BaseURI), nil

	default:
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
}

// Pinger is an optional interface that LLM providers can implement to
// validate credentials without consuming any tokens.
type Pinger interface {
	Ping(ctx context.Context) error
}

// PingModel verifies that the provider for the given model string is reachable
// and the credentials are valid. If the provider implements Pinger it uses a
// token-free check (e.g. GET /v1/models); otherwise it falls back to sending
// a minimal 1-token request. Returns nil on success.
func (f *Factory) PingModel(ctx context.Context, model string) error {
	provider, err := f.ForModel(model)
	if err != nil {
		return err
	}

	if p, ok := provider.(Pinger); ok {
		return p.Ping(ctx)
	}

	// Fallback: send a minimal 1-token request to verify auth.
	ch, err := provider.Stream(ctx, Request{
		Model:    model[strings.Index(model, "/")+1:],
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
		MaxToks:  1,
		Stream:   true,
	})
	if err != nil {
		return err
	}
	for ev := range ch {
		if ev.Type == EventTypeError {
			return ev.Error
		}
	}
	return nil
}

func (f *Factory) resolveAuth(ref string) (string, error) {
	if f.authResolver == nil {
		return "", nil
	}
	return f.authResolver(ref)
}

func (f *Factory) providerOptions(provider string) (ProviderOptions, bool) {
	if f.providerOptionsLookup == nil {
		return ProviderOptions{}, false
	}
	return f.providerOptionsLookup(strings.TrimSpace(provider))
}

func (f *Factory) resolveOptionalAuth(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "auth:") {
		return f.resolveAuth(value)
	}
	return value, nil
}
