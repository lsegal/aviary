package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai/"

// geminiNativeModelsURL is the native Gemini REST endpoint for listing models.
// Only accepts API key auth (?key=); OAuth tokens from the Code Assist flow
// are scoped to cloudcode-pa.googleapis.com, not this endpoint.
const geminiNativeModelsURL = "https://generativelanguage.googleapis.com/v1beta/models"

// googleTokenInfoURL validates any Google OAuth access token.
const googleTokenInfoURL = "https://oauth2.googleapis.com/tokeninfo"

// geminiCodeAssistStreamURL is the streaming endpoint for the Code Assist API.
// The project ID and model name (without "models/" prefix) are passed in the request body, not the URL.
const geminiCodeAssistStreamURL = "https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse"

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

// GeminiCodeAssistProvider uses the Cloud Code Assist API
// (cloudcode-pa.googleapis.com) with a Google OAuth access token from the
// gemini-cli Code Assist flow. This uses the native Gemini streaming format,
// not the OpenAI-compatible endpoint, and does not require Vertex AI to be
// enabled in the project.
type GeminiCodeAssistProvider struct {
	accessToken string
	projectID   string
	model       string
}

// NewGeminiCodeAssistProvider creates a provider using the Code Assist endpoint.
// projectID is the Google Cloud project ID returned by auth.GeminiLookupProject.
func NewGeminiCodeAssistProvider(accessToken, projectID, model string) *GeminiCodeAssistProvider {
	return &GeminiCodeAssistProvider{
		accessToken: accessToken,
		projectID:   projectID,
		model:       model,
	}
}

// Ping validates the Google OAuth token via the tokeninfo endpoint.
func (p *GeminiCodeAssistProvider) Ping(ctx context.Context) error {
	return pingGoogleOAuthToken(ctx, p.accessToken)
}

// Stream calls the Code Assist streaming endpoint using native Gemini format.
func (p *GeminiCodeAssistProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Role  string `json:"role"`
		Parts []part `json:"parts"`
	}
	type systemInstruction struct {
		Parts []part `json:"parts"`
	}
	type generationConfig struct {
		MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
	}
	type innerRequest struct {
		Contents          []content          `json:"contents"`
		SystemInstruction *systemInstruction `json:"systemInstruction,omitempty"`
		GenerationConfig  *generationConfig  `json:"generationConfig,omitempty"`
	}

	var contents []content
	for _, msg := range req.Messages {
		switch msg.Role {
		case RoleUser:
			c := content{Role: "user", Parts: []part{{Text: msg.Content}}}
			contents = append(contents, c)
		case RoleAssistant:
			c := content{Role: "model", Parts: []part{{Text: msg.Content}}}
			contents = append(contents, c)
		}
	}

	inner := innerRequest{Contents: contents}
	if req.System != "" {
		inner.SystemInstruction = &systemInstruction{Parts: []part{{Text: req.System}}}
	}
	if req.MaxToks > 0 {
		inner.GenerationConfig = &generationConfig{MaxOutputTokens: req.MaxToks}
	}

	envelope := map[string]any{
		"model":   p.model,
		"project": p.projectID,
		"request": inner,
	}

	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("code assist: marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, geminiCodeAssistStreamURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("code assist: creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.accessToken)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("code assist stream: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("code assist stream: %s %s", resp.Status, strings.TrimSpace(string(errBody)))
	}

	ch := make(chan Event, 16)
	go func() {
		defer close(ch)
		defer func() { _ = resp.Body.Close() }()

		type candidateChunk struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		}
		type responseChunk struct {
			Response struct {
				Candidates    []candidateChunk `json:"candidates"`
				UsageMetadata struct {
					PromptTokenCount     int `json:"promptTokenCount"`
					CandidatesTokenCount int `json:"candidatesTokenCount"`
				} `json:"usageMetadata"`
			} `json:"response"`
		}

		scanner := bufio.NewScanner(resp.Body)
		var lastUsage *Usage
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var chunk responseChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			for _, cand := range chunk.Response.Candidates {
				for _, p := range cand.Content.Parts {
					if p.Text != "" {
						ch <- Event{Type: EventTypeText, Text: p.Text}
					}
				}
			}
			if chunk.Response.UsageMetadata.PromptTokenCount > 0 || chunk.Response.UsageMetadata.CandidatesTokenCount > 0 {
				lastUsage = &Usage{
					InputTokens:  chunk.Response.UsageMetadata.PromptTokenCount,
					OutputTokens: chunk.Response.UsageMetadata.CandidatesTokenCount,
				}
			}
		}

		if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
			ch <- Event{Type: EventTypeError, Error: fmt.Errorf("code assist stream: %w", err)}
			return
		}
		if lastUsage != nil {
			ch <- Event{Type: EventTypeUsage, Usage: lastUsage}
		}
		ch <- Event{Type: EventTypeDone}
	}()

	return ch, nil
}
