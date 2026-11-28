package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// openAICompatLocalProvider is the shared implementation behind self-hosted servers
// (Ollama, vLLM) that expose an OpenAI-compatible API.
type openAICompatLocalProvider struct {
	inner      *OpenAIProvider
	apiKey     string
	httpClient *http.Client
	baseURL    string
	// label prefixes error messages, e.g. "ollama".
	label string
}

// newOpenAICompatLocalProvider builds the shared provider. baseURL must already be
// normalized (see normalizeLocalOpenAIBaseURI).
func newOpenAICompatLocalProvider(label, apiKey, model, baseURL string) openAICompatLocalProvider {
	return openAICompatLocalProvider{
		inner:      NewOpenAIProvider(apiKey, model, baseURL),
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: newDebugClient(nil),
		baseURL:    baseURL,
		label:      label,
	}
}

// normalizeLocalOpenAIBaseURI falls back to defaultBaseURI when baseURI is
// empty and appends "/v1" when the URI omits an API path.
func normalizeLocalOpenAIBaseURI(baseURI, defaultBaseURI string) string {
	baseURI = strings.TrimSpace(baseURI)
	if baseURI == "" {
		baseURI = defaultBaseURI
	}
	parsed, err := url.Parse(baseURI)
	if err != nil {
		return strings.TrimRight(baseURI, "/")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if path == "" {
		path = "/v1"
	}
	parsed.Path = path
	parsed.RawPath = path
	return strings.TrimRight(parsed.String(), "/")
}

// Stream sends a chat request to the OpenAI-compatible endpoint.
func (p *openAICompatLocalProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	return p.inner.Stream(ctx, req)
}

// Ping validates reachability and auth by listing models.
func (p *openAICompatLocalProvider) Ping(ctx context.Context) error {
	_, err := p.ListModels(ctx)
	return err
}

// ListModels introspects the endpoint's OpenAI-compatible model list.
func (p *openAICompatLocalProvider) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("%s models: building request: %w", p.label, err)
	}
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s models: %w", p.label, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s models: GET %q: %s: %s", p.label, req.URL.String(), resp.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("%s models: decoding response: %w", p.label, err)
	}

	models := make([]string, 0, len(payload.Data))
	for _, model := range payload.Data {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		models = append(models, id)
	}
	return models, nil
}
