package llm

import (
	"context"
	"fmt"
	"net/http"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicProvider streams responses from Anthropic's Claude models.
type AnthropicProvider struct {
	client anthropic.Client
	model  string
}

// NewAnthropicProvider creates a provider for the given Claude model using an API key.
func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	opts := []option.RequestOption{}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	opts = append(opts, option.WithHTTPClient(newDebugClient(nil)))
	return &AnthropicProvider{
		client: anthropic.NewClient(opts...),
		model:  model,
	}
}

// noAPIKeyTransport wraps a RoundTripper and removes the x-api-key header so
// that OAuth Bearer auth is the only credential sent.
type noAPIKeyTransport struct{ base http.RoundTripper }

func (t *noAPIKeyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Del("x-api-key")
	return t.base.RoundTrip(req)
}

// NewAnthropicOAuthProvider creates a provider using an OAuth Bearer token (Claude Pro/Max).
// The token is obtained via the Anthropic OAuth PKCE flow (auth_login_anthropic).
func NewAnthropicOAuthProvider(accessToken, model string) *AnthropicProvider {
	// Use a custom transport to strip x-api-key, which the SDK injects from
	// the env var ANTHROPIC_API_KEY even when WithAPIKey("") is passed.
	httpClient := newDebugClient(&noAPIKeyTransport{base: http.DefaultTransport})
	opts := []option.RequestOption{
		option.WithAPIKey(""),
		option.WithAuthToken(accessToken),
		option.WithHTTPClient(httpClient),
		// Required headers for Anthropic OAuth requests.
		option.WithHeader("anthropic-beta", "oauth-2025-04-20,interleaved-thinking-2025-05-14"),
		option.WithHeader("user-agent", "claude-cli/2.1.2 (external, cli)"),
	}
	return &AnthropicProvider{
		client: anthropic.NewClient(opts...),
		model:  model,
	}
}

// Ping validates Anthropic credentials by listing models (GET /v1/models).
// This costs no tokens and is fast.
func (p *AnthropicProvider) Ping(ctx context.Context) error {
	_, err := p.client.Models.List(ctx, anthropic.ModelListParams{})
	return err
}

// Stream sends a request to Anthropic and returns a streaming event channel.
func (p *AnthropicProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	messages := make([]anthropic.MessageParam, 0, len(req.Messages))
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case RoleAssistant:
			messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content)))
		}
	}

	maxToks := int64(4096)
	if req.MaxToks > 0 {
		maxToks = int64(req.MaxToks)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(p.model),
		Messages:  messages,
		MaxTokens: maxToks,
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}

	stream := p.client.Messages.NewStreaming(ctx, params)

	ch := make(chan Event, 32)
	go func() {
		defer close(ch)
		for stream.Next() {
			event := stream.Current()
			switch e := event.AsAny().(type) {
			case anthropic.ContentBlockDeltaEvent:
				if delta, ok := e.Delta.AsAny().(anthropic.TextDelta); ok {
					ch <- Event{Type: EventTypeText, Text: delta.Text}
				}
			}
		}
		if err := stream.Err(); err != nil {
			ch <- Event{Type: EventTypeError, Error: fmt.Errorf("anthropic stream: %w", err)}
			return
		}
		ch <- Event{Type: EventTypeDone}
	}()

	return ch, nil
}
