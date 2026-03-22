package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	// Tool results are top-level function_call_output items; function calls from
	// the assistant are top-level function_call items. Other messages use role/content.
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
	var input []any
	if strings.TrimSpace(req.System) != "" {
		input = append(input, inputMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			if m.Result != nil {
				input = append(input, map[string]any{
					"type":    "function_call_output",
					"call_id": m.Result.ToolCallID,
					"output":  m.Result.Content,
				})
				continue
			}
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
			if m.ToolCall != nil {
				input = append(input, map[string]any{
					"type":      "function_call",
					"call_id":   m.ToolCall.ID,
					"name":      m.ToolCall.Name,
					"arguments": mustJSONMap(m.ToolCall.Arguments),
				})
				continue
			}
			input = append(input, inputMessage{
				Role:    "assistant",
				Content: []inputTextContent{{Type: "output_text", Text: m.Content}},
			})
		case RoleSystem:
			input = append(input, inputMessage{Role: "system", Content: m.Content})
		}
	}

	reqBody := map[string]any{
		"model":        p.model,
		"input":        input,
		"stream":       true,
		"instructions": "",
		"store":        false, // chatgpt.com codex rejects streaming requests unless store is false
	}
	if len(req.Tools) > 0 {
		type toolParam struct {
			Type        string `json:"type"`
			Name        string `json:"name"`
			Description string `json:"description,omitempty"`
			Parameters  any    `json:"parameters,omitempty"`
			Strict      bool   `json:"strict"`
		}
		tools := make([]toolParam, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, toolParam{
				Type:        "function",
				Name:        t.Name,
				Description: t.Description,
				Parameters:  schemaObject(t.InputSchema),
				Strict:      true,
			})
		}
		reqBody["tools"] = tools
	}
	// The ChatGPT backend does not accept a `previous_response_id` parameter here.
	// We intentionally do not send it to avoid 400 Bad Request responses.

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
		//   response.output_text.delta              {"delta":"text"}
		//   response.output_item.added              {"output_index":N,"item":{"type":"function_call","call_id":"...","name":"..."}}
		//   response.function_call_arguments.delta  {"output_index":N,"delta":"..."}
		//   response.function_call_arguments.done   {"output_index":N,"arguments":"..."}
		//   response.completed                      {"response":{"id":"...","usage":{...}}}
		//   response.failed                         {"response":{"error":{...}}}
		type responsesEvent struct {
			Type        string `json:"type"`
			Delta       string `json:"delta"`
			Arguments   string `json:"arguments"`
			OutputIndex int    `json:"output_index"`
			Response    *struct {
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
			Item *struct {
				Type   string `json:"type"`
				CallID string `json:"call_id"`
				Name   string `json:"name"`
			} `json:"item"`
		}
		type pendingToolCall struct {
			ID        string
			Name      string
			Arguments strings.Builder
		}

		scanner := newSSEScanner(resp.Body)
		var lastUsage *Usage
		var lastResponseID string
		pendingCalls := map[int]*pendingToolCall{}
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
			case "response.output_item.added":
				if ev.Item != nil && ev.Item.Type == "function_call" {
					pendingCalls[ev.OutputIndex] = &pendingToolCall{
						ID:   ev.Item.CallID,
						Name: ev.Item.Name,
					}
				}
			case "response.function_call_arguments.delta":
				if call := pendingCalls[ev.OutputIndex]; call != nil {
					call.Arguments.WriteString(ev.Delta)
				}
			case "response.function_call_arguments.done":
				// Arguments are already accumulated via deltas; nothing extra needed.
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
		if len(pendingCalls) > 0 {
			indexes := make([]int, 0, len(pendingCalls))
			for idx := range pendingCalls {
				indexes = append(indexes, idx)
			}
			sortInts(indexes)
			for _, idx := range indexes {
				call := pendingCalls[idx]
				if strings.TrimSpace(call.Name) == "" {
					continue
				}
				args, err := parseToolArguments(call.Arguments.String())
				if err != nil {
					ch <- Event{Type: EventTypeError, Error: fmt.Errorf("openai codex tool arguments for %q: %w", call.Name, err)}
					return
				}
				ch <- Event{Type: EventTypeToolCall, ToolCall: &ToolCall{
					ID:        call.ID,
					Name:      call.Name,
					Arguments: args,
				}}
			}
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
	for i := 0; i < len(req.Messages); i++ {
		m := req.Messages[i]
		switch m.Role {
		case RoleUser:
			if m.Result != nil {
				if strings.TrimSpace(m.Result.Content) == "" || strings.TrimSpace(m.Result.ToolCallID) == "" {
					continue
				}
				messages = append(messages, openai.ToolMessage(m.Result.Content, m.Result.ToolCallID))
				continue
			}
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
			assistantText := m.Content
			toolCalls := make([]openai.ChatCompletionMessageToolCallParam, 0, 1)
			for {
				if m.ToolCall != nil {
					argsJSON := mustJSONMap(m.ToolCall.Arguments)
					toolID := strings.TrimSpace(m.ToolCall.ID)
					if toolID == "" {
						toolID = "call_" + strconv.Itoa(i)
					}
					toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallParam{
						ID: toolID,
						Function: openai.ChatCompletionMessageToolCallFunctionParam{
							Name:      m.ToolCall.Name,
							Arguments: argsJSON,
						},
					})
				}
				if i+1 >= len(req.Messages) || req.Messages[i+1].Role != RoleAssistant {
					break
				}
				next := req.Messages[i+1]
				if next.Content != "" {
					if assistantText != "" {
						assistantText += "\n"
					}
					assistantText += next.Content
				}
				if next.ToolCall == nil {
					i++
					continue
				}
				i++
				m = next
			}
			if len(toolCalls) > 0 {
				msg := openai.ChatCompletionAssistantMessageParam{ToolCalls: toolCalls}
				if assistantText != "" {
					msg.Content.OfString = openai.String(assistantText)
				}
				messages = append(messages, openai.ChatCompletionMessageParamUnion{OfAssistant: &msg})
				continue
			}
			messages = append(messages, openai.AssistantMessage(assistantText))
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
	if len(req.Tools) > 0 {
		params.Tools = make([]openai.ChatCompletionToolParam, 0, len(req.Tools))
		for _, tool := range req.Tools {
			desc := tool.Description
			if ex := formatToolExamples(tool.Examples); ex != "" {
				if desc != "" {
					desc += "\n\n"
				}
				desc += ex
			}
			fn := shared.FunctionDefinitionParam{
				Name:        tool.Name,
				Parameters:  shared.FunctionParameters(schemaObject(tool.InputSchema)),
				Description: openai.String(desc),
				Strict:      openai.Bool(true),
			}
			params.Tools = append(params.Tools, openai.ChatCompletionToolParam{Function: fn})
		}
	}

	stream := p.client.Chat.Completions.NewStreaming(ctx, params)

	ch := make(chan Event, 32)
	go func() {
		defer close(ch)
		var lastUsage *Usage
		type pendingToolCall struct {
			ID        string
			Name      string
			Arguments strings.Builder
		}
		pendingCalls := map[int64]*pendingToolCall{}
		for stream.Next() {
			chunk := stream.Current()
			for _, choice := range chunk.Choices {
				if choice.Delta.Content != "" {
					ch <- Event{Type: EventTypeText, Text: choice.Delta.Content}
				}
				for _, tc := range choice.Delta.ToolCalls {
					call := pendingCalls[tc.Index]
					if call == nil {
						call = &pendingToolCall{}
						pendingCalls[tc.Index] = call
					}
					if tc.ID != "" {
						call.ID = tc.ID
					}
					if tc.Function.Name != "" {
						call.Name = tc.Function.Name
					}
					if tc.Function.Arguments != "" {
						call.Arguments.WriteString(tc.Function.Arguments)
					}
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
		if len(pendingCalls) > 0 {
			indexes := make([]int, 0, len(pendingCalls))
			for idx := range pendingCalls {
				indexes = append(indexes, int(idx))
			}
			sortInts(indexes)
			for _, idx := range indexes {
				call := pendingCalls[int64(idx)]
				if call == nil || strings.TrimSpace(call.Name) == "" {
					continue
				}
				args, err := parseToolArguments(call.Arguments.String())
				if err != nil {
					ch <- Event{Type: EventTypeError, Error: fmt.Errorf("openai tool arguments for %q: %w", call.Name, err)}
					return
				}
				ch <- Event{Type: EventTypeToolCall, ToolCall: &ToolCall{
					ID:        call.ID,
					Name:      call.Name,
					Arguments: args,
				}}
			}
		}
		if lastUsage != nil {
			ch <- Event{Type: EventTypeUsage, Usage: lastUsage}
		}
		ch <- Event{Type: EventTypeDone}
	}()

	return ch, nil
}

func mustJSONMap(v map[string]any) string {
	if v == nil {
		return "{}"
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func schemaObject(schema any) map[string]any {
	empty := map[string]any{"type": "object", "properties": map[string]any{}}
	if schema == nil {
		return empty
	}
	var obj map[string]any
	if m, ok := schema.(map[string]any); ok {
		obj = m
	} else {
		data, err := json.Marshal(schema)
		if err != nil {
			return empty
		}
		if err := json.Unmarshal(data, &obj); err != nil {
			return empty
		}
	}
	if len(obj) == 0 {
		return empty
	}
	// Ensure object schemas always have a properties key; required by strict-mode APIs.
	if t, _ := obj["type"].(string); t == "object" || t == "" {
		if _, ok := obj["properties"]; !ok {
			obj["properties"] = map[string]any{}
		}
	}
	// Strip combiners (oneOf/anyOf/allOf/not) from the top level — strict-mode APIs
	// (Copilot, OpenAI) reject schemas that have these at the root.
	delete(obj, "oneOf")
	delete(obj, "anyOf")
	delete(obj, "allOf")
	delete(obj, "not")
	return obj
}

func formatToolExamples(examples []map[string]any) string {
	if len(examples) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Example arguments:\n")
	limit := len(examples)
	if limit > 3 {
		limit = 3
	}
	for i := 0; i < limit; i++ {
		sb.WriteString("- ")
		sb.WriteString(mustJSONMap(examples[i]))
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func parseToolArguments(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, err
	}
	if args == nil {
		args = map[string]any{}
	}
	return args, nil
}

func sortInts(values []int) {
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}
