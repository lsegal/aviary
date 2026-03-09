package llm

import (
	"context"
	"fmt"
	"net/http"

	"github.com/lsegal/aviary/internal/auth"
)

const geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai/"

// geminiNativeModelsURL is the native Gemini REST endpoint for listing models.
// Only accepts API key auth (?key=); OAuth tokens from the Code Assist flow
// are scoped to cloudcode-pa.googleapis.com, not this endpoint.
const geminiNativeModelsURL = "https://generativelanguage.googleapis.com/v1beta/models"

// googleTokenInfoURL validates any Google OAuth access token.
const googleTokenInfoURL = "https://oauth2.googleapis.com/tokeninfo"

// pingGoogleOAuthToken validates a Google OAuth access token via the tokeninfo endpoint.
func pingGoogleOAuthToken(ctx context.Context, accessToken string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleTokenInfoURL, nil)
	if err != nil {
		return err
	}
	q := req.URL.Query()
	q.Set("access_token", accessToken)
	req.URL.RawQuery = q.Encode()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("gemini oauth ping: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gemini oauth ping: invalid token (%s)", resp.Status)
	}
	return nil
}

// GeminiProvider uses Google Gemini via the OpenAI-compatible endpoint with an API key.
type GeminiProvider struct {
	inner  *OpenAIProvider
	apiKey string
}

// NewGeminiProvider creates a Gemini provider using an API key.
func NewGeminiProvider(apiKey, model string) *GeminiProvider {
	return &GeminiProvider{
		inner:  NewOpenAIProvider(apiKey, model, geminiBaseURL),
		apiKey: apiKey,
	}
}

// Ping validates the Gemini API key via the native models endpoint (?key=<apiKey>).
func (p *GeminiProvider) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, geminiNativeModelsURL, nil)
	if err != nil {
		return err
	}
	q := req.URL.Query()
	q.Set("key", p.apiKey)
	req.URL.RawQuery = q.Encode()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("gemini ping: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gemini ping: %s", resp.Status)
	}
	return nil
}

// Stream forwards to the underlying OpenAI-compatible provider.
func (p *GeminiProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	return p.inner.Stream(ctx, req)
}

// GeminiCodeAssistProvider uses the Vertex AI OpenAI-compatible endpoint with a
// Google OAuth access token from the gemini-cli Code Assist flow. The project ID
// is fetched at login time via GeminiLookupProject (loadCodeAssist) and used to
// construct the Vertex AI streaming endpoint URL.
type GeminiCodeAssistProvider struct {
	inner       *OpenAIProvider
	accessToken string
}

// NewGeminiCodeAssistProvider creates a provider using the Code Assist endpoint.
// projectID is the Google Cloud project ID returned by auth.GeminiLookupProject.
func NewGeminiCodeAssistProvider(accessToken, projectID, model string) *GeminiCodeAssistProvider {
	baseURL := auth.GeminiCodeAssistBaseURL(projectID)
	return &GeminiCodeAssistProvider{
		inner:       NewOpenAIProvider(accessToken, model, baseURL),
		accessToken: accessToken,
	}
}

// Ping validates the Google OAuth token via the tokeninfo endpoint.
func (p *GeminiCodeAssistProvider) Ping(ctx context.Context) error {
	return pingGoogleOAuthToken(ctx, p.accessToken)
}

// Stream forwards to the underlying OpenAI-compatible provider.
func (p *GeminiCodeAssistProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	return p.inner.Stream(ctx, req)
}
