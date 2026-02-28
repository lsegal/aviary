// Package llm provides adapters for language model providers.
package llm

import (
	"context"
	"fmt"
	"strings"
)

// Role identifies who authored a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// Message is a single message in a conversation.
type Message struct {
	Role    Role
	Content string
}

// Request is the input to an LLM provider.
type Request struct {
	Model    string
	Messages []Message
	System   string  // optional system prompt
	MaxToks  int     // 0 = provider default
	Stream   bool    // whether to stream
}

// Event is a single streaming event from an LLM provider.
type Event struct {
	Type  EventType
	Text  string // partial text (EventTypeText)
	Error error  // (EventTypeError)
}

// EventType identifies a streaming event.
type EventType string

const (
	EventTypeText  EventType = "text"
	EventTypeError EventType = "error"
	EventTypeDone  EventType = "done"
)

// Provider is the interface all LLM backends implement.
type Provider interface {
	// Stream sends req to the model and returns a channel of events.
	// The channel is closed when the stream ends (EventTypeDone or EventTypeError).
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}

// Factory creates a Provider from a model string of the form "<provider>/<name>".
type Factory struct {
	authResolver func(ref string) (string, error)
}

// NewFactory creates a Factory. authResolver resolves "auth:<x>:<y>" references.
func NewFactory(authResolver func(string) (string, error)) *Factory {
	return &Factory{authResolver: authResolver}
}

// ForModel returns a Provider for the given model string.
// model format: "anthropic/claude-sonnet-4.5", "openai/gpt-4o", "gemini/gemini-pro",
// "stdio/claude" (subprocess), etc.
func (f *Factory) ForModel(model string) (Provider, error) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid model %q: expected <provider>/<name>", model)
	}
	provider, name := parts[0], parts[1]

	switch provider {
	case "anthropic":
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

	case "gemini":
		apiKey, err := f.resolveAuth("auth:gemini:default")
		if err != nil {
			return nil, fmt.Errorf("gemini auth: %w", err)
		}
		return NewGeminiProvider(apiKey, name), nil

	case "stdio":
		return NewStdioProvider(name), nil

	default:
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
}

func (f *Factory) resolveAuth(ref string) (string, error) {
	if f.authResolver == nil {
		return "", nil
	}
	return f.authResolver(ref)
}
