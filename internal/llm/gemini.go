package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai/"

// geminiNativeModelsURL is the native Gemini REST endpoint for listing models.
// Only accepts API key auth (?key=); OAuth tokens from the Code Assist flow
// are scoped to cloudcode-pa.googleapis.com, not this endpoint.
const geminiNativeModelsURL = "https://generativelanguage.googleapis.com/v1beta/models"

// googleTokenInfoURL validates any Google OAuth access token.
const googleTokenInfoURL = "https://oauth2.googleapis.com/tokeninfo"

// codeAssistBaseURL is the Code Assist API base.
const codeAssistBaseURL = "https://cloudcode-pa.googleapis.com/v1internal"

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

// fetchCodeAssistProject calls the Code Assist loadCodeAssist endpoint to
// retrieve the GCP project ID associated with the user's Google account.
// Returns a plain project ID string (e.g. "my-project-123456").
func fetchCodeAssistProject(ctx context.Context, accessToken string) (string, error) {
	payload := map[string]any{
		"cloudaicompanionProject": nil,
		"metadata": map[string]any{
			"ideType":     "IDE_UNSPECIFIED",
			"platform":    "PLATFORM_UNSPECIFIED",
			"pluginType":  "GEMINI",
			"duetProject": nil,
		},
	}
	body, _ := json.Marshal(payload)

	url := codeAssistBaseURL + ":loadCodeAssist"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("code assist project: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("code assist project: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("code assist project: %s %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	var result struct {
		CloudAICompanionProject string `json:"cloudaicompanionProject"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil || result.CloudAICompanionProject == "" {
		return "", fmt.Errorf("code assist project: unexpected response: %s", strings.TrimSpace(string(respBody)))
	}
	slog.Debug("code assist: resolved project", "project", result.CloudAICompanionProject)
	return result.CloudAICompanionProject, nil
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
	model       string
	mu          sync.Mutex
	project     string // cached plain GCP project ID, e.g. "my-project-123456"
}

// NewGeminiCodeAssistProvider creates a provider using the Code Assist endpoint.
func NewGeminiCodeAssistProvider(accessToken, model string) *GeminiCodeAssistProvider {
	return &GeminiCodeAssistProvider{
		accessToken: accessToken,
		model:       model,
	}
}

// Ping validates the Google OAuth token via the tokeninfo endpoint.
func (p *GeminiCodeAssistProvider) Ping(ctx context.Context) error {
	return pingGoogleOAuthToken(ctx, p.accessToken)
}

// resolveProject returns the cached project ID, fetching it via loadCodeAssist if needed.
func (p *GeminiCodeAssistProvider) resolveProject(ctx context.Context) (string, error) {
	p.mu.Lock()
	cached := p.project
	p.mu.Unlock()
	if cached != "" {
		return cached, nil
	}

	proj, err := fetchCodeAssistProject(ctx, p.accessToken)
	if err != nil {
		return "", err
	}

	p.mu.Lock()
	p.project = proj
	p.mu.Unlock()
	return proj, nil
}

// doStreamRequest posts body to url and returns the response. It retries once
// after a short delay on HTTP 5xx errors, which are often transient on the
// Code Assist free tier.
func (p *GeminiCodeAssistProvider) doStreamRequest(ctx context.Context, url string, body []byte) (*http.Response, error) {
	const maxAttempts = 2
	for attempt := range maxAttempts {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("code assist: creating request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+p.accessToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("code assist stream: %w", err)
		}
		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}

		errBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		errText := strings.TrimSpace(string(errBody))

		// Retry once on 5xx (transient backend errors); give up immediately on
		// 4xx since those are not going to be fixed by retrying.
		if resp.StatusCode >= 500 && attempt < maxAttempts-1 {
			slog.Warn("code assist: transient error, retrying", "status", resp.StatusCode, "attempt", attempt+1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
			}
			continue
		}

		return nil, fmt.Errorf("code assist stream: %s %s", resp.Status, errText)
	}
	return nil, fmt.Errorf("code assist stream: all attempts failed")
}

// Stream calls the Code Assist streaming endpoint using native Gemini format.
func (p *GeminiCodeAssistProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	project, err := p.resolveProject(ctx)
	if err != nil {
		return nil, err
	}

	type inlineData struct {
		MimeType string `json:"mimeType"`
		Data     string `json:"data"`
	}
	type part struct {
		Text       string      `json:"text,omitempty"`
		InlineData *inlineData `json:"inlineData,omitempty"`
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
			parts := make([]part, 0, 2)
			if strings.TrimSpace(msg.Content) != "" {
				parts = append(parts, part{Text: msg.Content})
			}
			if mimeType, data, ok := ParseImageDataURL(msg.MediaURL); ok {
				parts = append(parts, part{
					InlineData: &inlineData{
						MimeType: mimeType,
						Data:     data,
					},
				})
			}
			if len(parts) == 0 {
				continue
			}
			c := content{Role: "user", Parts: parts}
			contents = append(contents, c)
		case RoleAssistant:
			c := content{Role: "model", Parts: []part{{Text: msg.Content}}}
			contents = append(contents, c)
		case RoleSystem:
			c := content{Role: "user", Parts: []part{{Text: msg.Content}}}
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

	// The Code Assist API (matching gemini-cli's CAGenerateContentRequest):
	// - "model": plain model name (e.g. "gemini-2.0-flash")
	// - "project": plain GCP project ID (separate top-level field)
	// - "request": standard Gemini GenerateContentRequest
	envelope := map[string]any{
		"model":   p.model,
		"project": project,
		"request": inner,
	}

	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("code assist: marshaling request: %w", err)
	}
	slog.Debug("code assist: stream request", "body", string(body))

	streamURL := codeAssistBaseURL + ":streamGenerateContent?alt=sse"
	resp, err := p.doStreamRequest(ctx, streamURL, body)
	if err != nil {
		return nil, err
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

		scanner := newSSEScanner(resp.Body)
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
