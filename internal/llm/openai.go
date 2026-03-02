package llm

import (
	"context"
	"fmt"

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

// NewOpenAICodexProvider creates a provider using an OAuth Bearer token for
// ChatGPT Pro/Plus accounts. The access token is obtained via auth_login_openai.
// OpenAI's API key and OAuth access tokens both use Authorization: Bearer,
// so this is a thin wrapper around NewOpenAIProvider.
func NewOpenAICodexProvider(accessToken, model string) *OpenAIProvider {
	return NewOpenAIProvider(accessToken, model, "")
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
	}

	stream := p.client.Chat.Completions.NewStreaming(ctx, params)

	ch := make(chan Event, 32)
	go func() {
		defer close(ch)
		for stream.Next() {
			chunk := stream.Current()
			for _, choice := range chunk.Choices {
				if choice.Delta.Content != "" {
					ch <- Event{Type: EventTypeText, Text: choice.Delta.Content}
				}
			}
		}
		if err := stream.Err(); err != nil {
			ch <- Event{Type: EventTypeError, Error: fmt.Errorf("openai stream: %w", err)}
			return
		}
		ch <- Event{Type: EventTypeDone}
	}()

	return ch, nil
}
