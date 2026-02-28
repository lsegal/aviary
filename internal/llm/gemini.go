package llm

import "context"

const geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai/"

// GeminiProvider uses Google Gemini via the OpenAI-compatible endpoint.
type GeminiProvider struct {
	inner *OpenAIProvider
}

// NewGeminiProvider creates a Gemini provider reusing the OpenAI adapter.
func NewGeminiProvider(apiKey, model string) *GeminiProvider {
	return &GeminiProvider{
		inner: NewOpenAIProvider(apiKey, model, geminiBaseURL),
	}
}

// Stream forwards to the underlying OpenAI-compatible provider.
func (p *GeminiProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	return p.inner.Stream(ctx, req)
}
