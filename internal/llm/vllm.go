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

// VLLMProvider speaks to a vLLM server via its OpenAI-compatible API.
type VLLMProvider struct {
	inner      *OpenAIProvider
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// NewVLLMProvider creates a provider backed by a vLLM OpenAI-compatible endpoint.
// If baseURI omits the API path, "/v1" is added automatically.
func NewVLLMProvider(apiKey, model, baseURI string) *VLLMProvider {
	baseURL := normalizeVLLMBaseURI(baseURI)
	return &VLLMProvider{
		inner:      NewOpenAIProvider(apiKey, model, baseURL),
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: newDebugClient(nil),
		baseURL:    baseURL,
	}
}

func normalizeVLLMBaseURI(baseURI string) string {
	baseURI = strings.TrimSpace(baseURI)
	if baseURI == "" {
		return ""
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

// Stream delegates to the OpenAI-compatible implementation.
func (p *VLLMProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	return p.inner.Stream(ctx, req)
}

// Ping validates reachability and auth by listing models.
func (p *VLLMProvider) Ping(ctx context.Context) error {
	_, err := p.ListModels(ctx)
	return err
}

// ListModels introspects the vLLM endpoint's OpenAI-compatible model list.
func (p *VLLMProvider) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("vllm models: building request: %w", err)
	}
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vllm models: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vllm models: GET %q: %s: %s", req.URL.String(), resp.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("vllm models: decoding response: %w", err)
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
