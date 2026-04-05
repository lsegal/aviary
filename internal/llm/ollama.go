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

const defaultOllamaBaseURI = "http://127.0.0.1:11434"

// OllamaProvider speaks to an Ollama server via its OpenAI-compatible API.
type OllamaProvider struct {
	inner      *OpenAIProvider
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// NewOllamaProvider creates a provider backed by an Ollama OpenAI-compatible endpoint.
// If baseURI omits the API path, "/v1" is added automatically.
func NewOllamaProvider(apiKey, model, baseURI string) *OllamaProvider {
	baseURL := normalizeOllamaBaseURI(baseURI)
	return &OllamaProvider{
		inner:      NewOpenAIProvider(apiKey, model, baseURL),
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: newDebugClient(nil),
		baseURL:    baseURL,
	}
}

func normalizeOllamaBaseURI(baseURI string) string {
	baseURI = strings.TrimSpace(baseURI)
	if baseURI == "" {
		baseURI = defaultOllamaBaseURI
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

// Stream sends a chat request to the Ollama OpenAI-compatible endpoint.
func (p *OllamaProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	return p.inner.Stream(ctx, req)
}

// Ping verifies that the Ollama endpoint is reachable by listing models.
func (p *OllamaProvider) Ping(ctx context.Context) error {
	_, err := p.ListModels(ctx)
	return err
}

// ListModels introspects the Ollama endpoint's OpenAI-compatible model list.
func (p *OllamaProvider) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("ollama models: building request: %w", err)
	}
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama models: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama models: GET %q: %s: %s", req.URL.String(), resp.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("ollama models: decoding response: %w", err)
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
