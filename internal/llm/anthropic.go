package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicProvider streams responses from Anthropic's Claude models.
type AnthropicProvider struct {
	client anthropic.Client
	model  string
	oauth  bool // true when using Bearer token auth (Claude Pro/Max)
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

// NewAnthropicOAuthProvider creates a provider using an OAuth Bearer token (Claude Pro/Max).
func NewAnthropicOAuthProvider(accessToken, model string) *AnthropicProvider {
	// WithAuthToken sets Authorization: Bearer without injecting x-api-key.
	opts := []option.RequestOption{
		option.WithAuthToken(accessToken),
		option.WithHTTPClient(newOAuthClient()),
		option.WithHeader("anthropic-beta", "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05,effort-2025-11-24"),
		option.WithHeader("anthropic-dangerous-direct-browser-access", "true"),
		option.WithHeader("anthropic-version", "2023-06-01"),
		option.WithHeader("user-agent", "claude-cli/2.1.78 (external, cli)"),
		option.WithHeader("x-app", "cli"),
	}
	return &AnthropicProvider{
		client: anthropic.NewClient(opts...),
		model:  model,
		oauth:  true,
	}
}

// Ping validates Anthropic credentials. For API key auth it uses GET /v1/models
// (token-free). For OAuth, /v1/models is not accessible with bearer tokens so
// it falls back to a minimal 1-token message.
func (p *AnthropicProvider) Ping(ctx context.Context) error {
	if !p.oauth {
		_, err := p.client.Models.List(ctx, anthropic.ModelListParams{})
		return err
	}
	ch, err := p.Stream(ctx, Request{
		Model:    p.model,
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
		MaxToks:  1,
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

// Stream sends a request to Anthropic and returns a streaming event channel.
func (p *AnthropicProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	messages := make([]anthropic.MessageParam, 0, len(req.Messages))
	appendMsg := func(msg anthropic.MessageParam) {
		// Merge consecutive messages of the same role to satisfy Anthropic's
		// strict alternation requirement (user/assistant/user/...).
		if len(messages) > 0 && messages[len(messages)-1].Role == msg.Role {
			messages[len(messages)-1].Content = append(messages[len(messages)-1].Content, msg.Content...)
			return
		}
		messages = append(messages, msg)
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			if strings.TrimSpace(m.MediaURL) != "" {
				blocks := make([]anthropic.ContentBlockParamUnion, 0, 2)
				if strings.TrimSpace(m.Content) != "" {
					blocks = append(blocks, anthropic.NewTextBlock(m.Content))
				}
				if strings.HasPrefix(m.MediaURL, "data:") {
					// data:<mediatype>;base64,<data>
					parts := strings.SplitN(m.MediaURL, ",", 2)
					if len(parts) == 2 {
						header := strings.TrimPrefix(parts[0], "data:")
						mediaType := strings.TrimSuffix(header, ";base64")
						blocks = append(blocks, anthropic.NewImageBlockBase64(mediaType, parts[1]))
					}
				} else {
					blocks = append(blocks, anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: m.MediaURL}))
				}
				appendMsg(anthropic.NewUserMessage(blocks...))
				continue
			}
			appendMsg(anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case RoleAssistant:
			appendMsg(anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content)))
		case RoleSystem:
			// Anthropic does not support a "system" role inside the messages array;
			// inject as a user turn. The appendMsg helper merges it with any
			// adjacent user messages to avoid alternation violations.
			appendMsg(anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
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
	if p.oauth {
		// Prepend the Claude Code billing header that grants OAuth tokens access
		// to sonnet/opus models. cc_version and cc_entrypoint are required;
		// the cch value is not validated server-side.
		billingHdr := anthropic.TextBlockParam{
			Text: "x-anthropic-billing-header: cc_version=2.1.78.13b; cc_entrypoint=cli; cch=269ee;",
		}
		if req.System != "" {
			params.System = []anthropic.TextBlockParam{billingHdr, {Text: req.System}}
		} else {
			params.System = []anthropic.TextBlockParam{billingHdr}
		}
	} else if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}

	stream := p.client.Messages.NewStreaming(ctx, params)

	ch := make(chan Event, 32)
	go func() {
		defer close(ch)
		var usageData *Usage
		for stream.Next() {
			event := stream.Current()
			switch e := event.AsAny().(type) {
			case anthropic.ContentBlockDeltaEvent:
				if delta, ok := e.Delta.AsAny().(anthropic.TextDelta); ok {
					ch <- Event{Type: EventTypeText, Text: delta.Text}
				}
			case anthropic.MessageDeltaEvent:
				// Usage totals are reported in the MessageDeltaEvent.
				usageData = &Usage{
					InputTokens:      int(e.Usage.InputTokens),
					OutputTokens:     int(e.Usage.OutputTokens),
					CacheReadTokens:  int(e.Usage.CacheReadInputTokens),
					CacheWriteTokens: int(e.Usage.CacheCreationInputTokens),
				}
			}
		}
		if err := stream.Err(); err != nil {
			ch <- Event{Type: EventTypeError, Error: fmt.Errorf("anthropic stream: %w", err)}
			return
		}
		if usageData != nil {
			ch <- Event{Type: EventTypeUsage, Usage: usageData}
		}
		ch <- Event{Type: EventTypeDone}
	}()

	return ch, nil
}
