package llm

import (
	"context"
	"strings"
)

// metaBaseURL is Meta's Model API, which serves the Muse models over an
// OpenAI-compatible Chat Completions endpoint authenticated with a Bearer key.
const metaBaseURL = "https://api.meta.ai/v1"

// MetaProvider streams Muse models straight from Meta's Model API, so they are
// usable with an API key and without the muse CLI installed.
//
// It intentionally does not implement Pinger: Meta documents no model listing
// on the Model API, so PingModel falls back to a 1-token request.
type MetaProvider struct {
	inner *OpenAIProvider
}

// NewMetaProvider creates a provider for a Muse model on Meta's Model API.
// baseURI overrides the API base URL and is empty for the default endpoint.
func NewMetaProvider(apiKey, model, baseURI string) *MetaProvider {
	baseURL := strings.TrimRight(strings.TrimSpace(baseURI), "/")
	if baseURL == "" {
		baseURL = metaBaseURL
	}
	return &MetaProvider{inner: NewOpenAIProvider(strings.TrimSpace(apiKey), model, baseURL)}
}

// Stream delegates to the OpenAI-compatible implementation.
func (p *MetaProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	return p.inner.Stream(ctx, req)
}
