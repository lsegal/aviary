package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

// OpenAIProvider streams responses from OpenAI-compatible endpoints.
type OpenAIProvider struct {
	client  openai.Client
	model   string
	baseURL string
}

// NewOpenAIProvider creates a provider for an OpenAI-compatible API.
// baseURL is empty for the default OpenAI API.
func NewOpenAIProvider(apiKey, model, baseURL string) *OpenAIProvider {
	opts := []option.RequestOption{}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	opts = append(opts, option.WithHTTPClient(newDebugClient(nil)))
	return &OpenAIProvider{
		client:  openai.NewClient(opts...),
		model:   model,
		baseURL: baseURL,
	}
}

// OpenAICodexProvider wraps OpenAIProvider for ChatGPT Pro/Plus OAuth tokens.
// It intentionally does NOT implement Pinger so that PingModel falls back to
// a minimal 1-token chat completion, avoiding GET /v1/models which requires
// api.model.read scope that ChatGPT OAuth tokens do not have.
type OpenAICodexProvider struct {
	inner *OpenAIProvider
}

// NewOpenAICodexProvider creates a provider using an OAuth Bearer token for
// ChatGPT Pro/Plus accounts. The access token is obtained via auth_login_openai.
func NewOpenAICodexProvider(accessToken, model string) *OpenAICodexProvider {
	// The token audience is https://api.openai.com/v1, so requests must go to
	// the default api.openai.com endpoint (empty baseURL = SDK default).
	inner := NewOpenAIProvider(accessToken, model, "")
	// api.openai.com/v1 requires a ChatGPT-Account-ID header when using OAuth tokens —
	// this is derived from the JWT access token's https://api.openai.com/auth.chatgpt_account_id claim.
	if accountID := extractChatGPTAccountID(accessToken); accountID != "" {
		inner.client = openai.NewClient(
			option.WithAPIKey(accessToken),
			option.WithHTTPClient(newDebugClient(nil)),
			option.WithHeader("ChatGPT-Account-ID", accountID),
		)
	}
	return &OpenAICodexProvider{inner: inner}
}

// extractChatGPTAccountID parses the JWT payload of an OpenAI OAuth access token
// and returns the chatgpt_account_id claim. This must be sent as ChatGPT-Account-ID
// header to api.openai.com when using OAuth tokens so requests are associated with
// the user's ChatGPT subscription.
func extractChatGPTAccountID(jwtToken string) string {
	parts := strings.Split(jwtToken, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Auth struct {
			ChatGPTAccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Auth.ChatGPTAccountID
}

// Stream forwards to the underlying OpenAI provider.
func (p *OpenAICodexProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	return p.inner.Stream(ctx, req)
}

// Ping validates OpenAI credentials by listing models (GET /v1/models).
// This costs no tokens and is fast.
func (p *OpenAIProvider) Ping(ctx context.Context) error {
	_, err := p.client.Models.List(ctx)
	return err
}

// Stream sends a request to the OpenAI API and returns a streaming event channel.
func (p *OpenAIProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, openai.SystemMessage(req.System))
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			if strings.TrimSpace(m.MediaURL) != "" {
				parts := make([]openai.ChatCompletionContentPartUnionParam, 0, 2)
				if strings.TrimSpace(m.Content) != "" {
					parts = append(parts, openai.TextContentPart(m.Content))
				}
				parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
					URL: m.MediaURL,
				}))
				messages = append(messages, openai.UserMessage(parts))
				continue
			}
			messages = append(messages, openai.UserMessage(m.Content))
		case RoleAssistant:
			messages = append(messages, openai.AssistantMessage(m.Content))
		case RoleSystem:
			messages = append(messages, openai.SystemMessage(m.Content))
		}
	}

	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(p.model),
		Messages: messages,
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		},
	}

	stream := p.client.Chat.Completions.NewStreaming(ctx, params)

	ch := make(chan Event, 32)
	go func() {
		defer close(ch)
		var lastUsage *Usage
		for stream.Next() {
			chunk := stream.Current()
			for _, choice := range chunk.Choices {
				if choice.Delta.Content != "" {
					ch <- Event{Type: EventTypeText, Text: choice.Delta.Content}
				}
			}
			// Capture usage from the final chunk (populated when include_usage=true).
			if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
				lastUsage = &Usage{
					InputTokens:  int(chunk.Usage.PromptTokens),
					OutputTokens: int(chunk.Usage.CompletionTokens),
				}
			}
		}
		if err := stream.Err(); err != nil {
			ch <- Event{Type: EventTypeError, Error: fmt.Errorf("openai stream: %w", err)}
			return
		}
		if lastUsage != nil {
			ch <- Event{Type: EventTypeUsage, Usage: lastUsage}
		}
		ch <- Event{Type: EventTypeDone}
	}()

	return ch, nil
}
