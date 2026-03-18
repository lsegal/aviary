package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

// OpenAIProvider streams responses from OpenAI-compatible endpoints.
type OpenAIProvider struct {
	client  openai.Client
	model   string
	baseURL string
}

// NewOpenAIProvider creates a provider for an OpenAI-compatible API.
// baseURL is empty for the default OpenAI API.
func NewOpenAIProvider(apiKey, model, baseURL string) *OpenAIProvider {
	opts := []option.RequestOption{}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	opts = append(opts, option.WithHTTPClient(newDebugClient(nil)))
	return &OpenAIProvider{
		client:  openai.NewClient(opts...),
		model:   model,
		baseURL: baseURL,
	}
}

// openAICodexBaseURL is the ChatGPT backend endpoint for OAuth-authenticated Codex requests.
// ChatGPT OAuth tokens (ChatGPT Plus/Pro subscriptions) must be sent here — not to
// api.openai.com which always bills from platform API credits.
// The path uses the OpenAI Responses API format (not chat completions).
const openAICodexBaseURL = "https://chatgpt.com/backend-api/codex/responses"

// OpenAICodexProvider makes raw HTTP requests to the ChatGPT backend for
// ChatGPT Pro/Plus OAuth tokens. It bypasses the openai-go SDK so that only
// the exact headers the ChatGPT backend expects are sent:
//
//	Authorization: Bearer <access_token>
//	ChatGPT-Account-ID: <account_id from JWT>
//	Content-Type: application/json
//
// It intentionally does NOT implement Pinger so PingModel falls back to a
// 1-token stream, avoiding GET /v1/models which requires api.model.read scope.
type OpenAICodexProvider struct {
	accessToken string
	accountID   string
	model       string
	httpClient  *http.Client
}

// NewOpenAICodexProvider creates a provider using an OAuth Bearer token for
// ChatGPT Pro/Plus accounts. The access token is obtained via auth_login_openai.
func NewOpenAICodexProvider(accessToken, model string) *OpenAICodexProvider {
	return &OpenAICodexProvider{
		accessToken: accessToken,
		accountID:   extractChatGPTAccountID(accessToken),
		model:       model,
		httpClient:  newDebugClient(nil),
	}
}

// extractChatGPTAccountID parses the JWT payload of an OpenAI OAuth access token
// and returns the chatgpt_account_id claim. This must be sent as ChatGPT-Account-ID
// to the chatgpt.com/backend-api endpoint so requests are billed against the
// ChatGPT subscription rather than platform API credits.
func extractChatGPTAccountID(jwtToken string) string {
	parts := strings.Split(jwtToken, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Auth struct {
			ChatGPTAccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Auth.ChatGPTAccountID
}

// Stream makes a raw streaming POST to the ChatGPT backend using the Responses API
// and returns events. The Responses API is what chatgpt.com/backend-api requires —
// not the Chat Completions format.
func (p *OpenAICodexProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	// Build input array in Responses API format.
	// User messages use plain string content; assistant messages use the output_text structure.
	type inputTextContent struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type inputImageContent struct {
		Type     string `json:"type"`
		ImageURL string `json:"image_url"`
	}
	type inputMessage struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}
	var input []inputMessage
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			if strings.TrimSpace(m.MediaURL) != "" {
				parts := make([]any, 0, 2)
				if strings.TrimSpace(m.Content) != "" {
					parts = append(parts, inputTextContent{Type: "input_text", Text: m.Content})
				}
				parts = append(parts, inputImageContent{Type: "input_image", ImageURL: m.MediaURL})
				input = append(input, inputMessage{Role: "user", Content: parts})
				continue
			}
			input = append(input, inputMessage{Role: "user", Content: m.Content})
		case RoleAssistant:
			input = append(input, inputMessage{
				Role:    "assistant",
				Content: []inputTextContent{{Type: "output_text", Text: m.Content}},
			})
		case RoleSystem:
			// System messages within the conversation are folded into user turns.
			input = append(input, inputMessage{Role: "user", Content: m.Content})
		}
	}

	reqBody := map[string]any{
		"model":        p.model,
		"input":        input,
		"stream":       true,
		"instructions": req.System, // required by the backend, even if empty
		"store":        true,       // must be true to get a response ID for conversation continuity
	}
	if req.PreviousResponseID != "" {
		reqBody["previous_response_id"] = req.PreviousResponseID
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("openai codex: marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, openAICodexBaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai codex: building request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.accessToken)
	if p.accountID != "" {
		httpReq.Header.Set("ChatGPT-Account-ID", p.accountID)
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai codex: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("openai stream: POST %q: %s: %s", openAICodexBaseURL, resp.Status, strings.TrimSpace(string(body)))
	}

	ch := make(chan Event, 32)
	go func() {
		defer resp.Body.Close() //nolint:errcheck
		defer close(ch)

		// Responses API SSE events:
		//   event: response.output_text.delta  data: {"type":"response.output_text.delta","delta":"text"}
		//   event: response.completed          data: {"type":"response.completed","response":{...,"usage":{...}}}
		//   event: response.failed             data: {"type":"response.failed","response":{"error":{...}}}
		type responsesEvent struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
			// For response.completed and response.failed
			Response *struct {
				ID    string `json:"id"`
				Error *struct {
					Message string `json:"message"`
					Code    string `json:"code"`
				} `json:"error"`
				Usage *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"response"`
			// For response.output_item.done (full item at end)
			Item *struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"item"`
		}

		scanner := bufio.NewScanner(resp.Body)
		var lastUsage *Usage
		var lastResponseID string
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "" || data == "[DONE]" {
				continue
			}
			var ev responsesEvent
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				continue
			}
			switch ev.Type {
			case "response.output_text.delta":
				if ev.Delta != "" {
					ch <- Event{Type: EventTypeText, Text: ev.Delta}
				}
			case "response.completed":
				if ev.Response != nil {
					if ev.Response.ID != "" {
						lastResponseID = ev.Response.ID
					}
					if ev.Response.Usage != nil {
						u := ev.Response.Usage
						if u.InputTokens > 0 || u.OutputTokens > 0 {
							lastUsage = &Usage{
								InputTokens:  u.InputTokens,
								OutputTokens: u.OutputTokens,
							}
						}
					}
				}
			case "response.failed":
				msg := "unknown error"
				if ev.Response != nil && ev.Response.Error != nil {
					msg = ev.Response.Error.Message
					if ev.Response.Error.Code != "" {
						msg = ev.Response.Error.Code + ": " + msg
					}
				}
				ch <- Event{Type: EventTypeError, Error: fmt.Errorf("openai codex: response.failed: %s", msg)}
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- Event{Type: EventTypeError, Error: fmt.Errorf("openai codex stream: %w", err)}
			return
		}
		if lastUsage != nil {
			ch <- Event{Type: EventTypeUsage, Usage: lastUsage}
		}
		ch <- Event{Type: EventTypeDone, ResponseID: lastResponseID}
	}()

	return ch, nil
}

// Ping validates OpenAI credentials by listing models (GET /v1/models).
// This costs no tokens and is fast.
func (p *OpenAIProvider) Ping(ctx context.Context) error {
	_, err := p.client.Models.List(ctx)
	return err
}

// Stream sends a request to the OpenAI API and returns a streaming event channel.
func (p *OpenAIProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, openai.SystemMessage(req.System))
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			if strings.TrimSpace(m.MediaURL) != "" {
				parts := make([]openai.ChatCompletionContentPartUnionParam, 0, 2)
				if strings.TrimSpace(m.Content) != "" {
					parts = append(parts, openai.TextContentPart(m.Content))
				}
				parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
					URL: m.MediaURL,
				}))
				messages = append(messages, openai.UserMessage(parts))
				continue
			}
			messages = append(messages, openai.UserMessage(m.Content))
		case RoleAssistant:
			messages = append(messages, openai.AssistantMessage(m.Content))
		case RoleSystem:
			messages = append(messages, openai.SystemMessage(m.Content))
		}
	}

	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(p.model),
		Messages: messages,
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		},
	}

	stream := p.client.Chat.Completions.NewStreaming(ctx, params)

	ch := make(chan Event, 32)
	go func() {
		defer close(ch)
		var lastUsage *Usage
		for stream.Next() {
			chunk := stream.Current()
			for _, choice := range chunk.Choices {
				if choice.Delta.Content != "" {
					ch <- Event{Type: EventTypeText, Text: choice.Delta.Content}
				}
			}
			// Capture usage from the final chunk (populated when include_usage=true).
			if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
				lastUsage = &Usage{
					InputTokens:  int(chunk.Usage.PromptTokens),
					OutputTokens: int(chunk.Usage.CompletionTokens),
				}
			}
		}
		if err := stream.Err(); err != nil {
			ch <- Event{Type: EventTypeError, Error: fmt.Errorf("openai stream: %w", err)}
			return
		}
		if lastUsage != nil {
			ch <- Event{Type: EventTypeUsage, Usage: lastUsage}
		}
		ch <- Event{Type: EventTypeDone}
	}()

	return ch, nil
}
