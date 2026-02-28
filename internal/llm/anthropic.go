package llm

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicProvider streams responses from Anthropic's Claude models.
type AnthropicProvider struct {
	client anthropic.Client
	model  string
}

// NewAnthropicProvider creates a provider for the given Claude model.
func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	opts := []option.RequestOption{}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	return &AnthropicProvider{
		client: anthropic.NewClient(opts...),
		model:  model,
	}
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
