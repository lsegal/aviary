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
	client    openai.Client
	model     string
	baseURL   string
	fileInput bool
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
		// OpenAI-compatible endpoints often reject file content parts, so
		// only the OpenAI API itself gets PDFs as files.
		fileInput: baseURL == "" && openAIModelSupportsFileInput(model),
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
	fileInput   bool
}

// NewOpenAICodexProvider creates a provider using an OAuth Bearer token for
// ChatGPT Pro/Plus accounts. The access token is obtained via auth_login_openai.
func NewOpenAICodexProvider(accessToken, model string) *OpenAICodexProvider {
	return &OpenAICodexProvider{
		accessToken: accessToken,
		accountID:   extractChatGPTAccountID(accessToken),
		model:       model,
		httpClient:  newDebugClient(nil),
		fileInput:   openAIModelSupportsFileInput(model),
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
	var input []any
	if strings.TrimSpace(req.System) != "" {
		input = append(input, codexInputMessage{Role: "system", Content: req.System})
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
			if len(m.MediaURLs) > 0 {
				input = append(input, codexInputMessage{Role: "user", Content: p.userContentParts(m)})
				continue
			}
			input = append(input, codexInputMessage{Role: "user", Content: m.Content})
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
			input = append(input, codexInputMessage{
				Role:    "assistant",
				Content: []codexInputTextContent{{Type: "output_text", Text: m.Content}},
			})
		case RoleSystem:
			input = append(input, codexInputMessage{Role: "system", Content: m.Content})
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
				Parameters:  strictSchemaObject(t.InputSchema),
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

		scanner := newSSEScanner(resp.Body)
		var lastUsage *Usage
		var lastResponseID string
		pendingCalls := pendingToolCalls{}
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "" || data == "[DONE]" {
				continue
			}
			var ev codexResponsesEvent
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
					pendingCalls[int64(ev.OutputIndex)] = &pendingToolCall{
						ID:   ev.Item.CallID,
						Name: ev.Item.Name,
					}
				}
			case "response.function_call_arguments.delta":
				if call := pendingCalls[int64(ev.OutputIndex)]; call != nil {
					call.Arguments.WriteString(ev.Delta)
				}
			case "response.function_call_arguments.done":
				// Arguments are already accumulated via deltas; nothing extra needed.
			case "response.completed":
				if ev.Response == nil {
					continue
				}
				if ev.Response.ID != "" {
					lastResponseID = ev.Response.ID
				}
				if usage := ev.Response.usage(); usage != nil {
					lastUsage = usage
				}
			case "response.failed":
				ch <- Event{Type: EventTypeError, Error: fmt.Errorf("openai codex: response.failed: %s", ev.failureMessage())}
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- Event{Type: EventTypeError, Error: fmt.Errorf("openai codex stream: %w", err)}
			return
		}
		if !pendingCalls.emit(ch, "openai codex") {
			return
		}
		if lastUsage != nil {
			ch <- Event{Type: EventTypeUsage, Usage: lastUsage}
		}
		ch <- Event{Type: EventTypeDone, ResponseID: lastResponseID}
	}()

	return ch, nil
}

type codexInputTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type codexInputImageContent struct {
	Type     string `json:"type"`
	ImageURL string `json:"image_url"`
}

type codexInputFileContent struct {
	Type     string `json:"type"`
	Filename string `json:"filename"`
	FileData string `json:"file_data"`
}

type codexInputMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// userContentParts converts a user message with media into Responses API
// input parts: the text (if any), then images, then PDFs when the model
// accepts file input.
func (p *OpenAICodexProvider) userContentParts(m Message) []any {
	parts := make([]any, 0, 1+len(m.MediaURLs))
	if strings.TrimSpace(m.Content) != "" {
		parts = append(parts, codexInputTextContent{Type: "input_text", Text: m.Content})
	}
	for _, mediaURL := range m.MediaURLs {
		if isImageMediaURL(mediaURL) {
			parts = append(parts, codexInputImageContent{Type: "input_image", ImageURL: mediaURL})
			continue
		}
		if filename, fileData, ok := openAIFileInput(mediaURL); ok && p.fileInput {
			parts = append(parts, codexInputFileContent{Type: "input_file", Filename: filename, FileData: fileData})
		}
	}
	return parts
}

// codexResponsesEvent is one Responses API SSE event:
//
//	response.output_text.delta              {"delta":"text"}
//	response.output_item.added              {"output_index":N,"item":{"type":"function_call","call_id":"...","name":"..."}}
//	response.function_call_arguments.delta  {"output_index":N,"delta":"..."}
//	response.function_call_arguments.done   {"output_index":N,"arguments":"..."}
//	response.completed                      {"response":{"id":"...","usage":{...}}}
//	response.failed                         {"response":{"error":{...}}}
type codexResponsesEvent struct {
	Type        string                  `json:"type"`
	Delta       string                  `json:"delta"`
	Arguments   string                  `json:"arguments"`
	OutputIndex int                     `json:"output_index"`
	Response    *codexResponsesResponse `json:"response"`
	Item        *struct {
		Type   string `json:"type"`
		CallID string `json:"call_id"`
		Name   string `json:"name"`
	} `json:"item"`
}

type codexResponsesResponse struct {
	ID    string `json:"id"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// usage returns the response's token usage, or nil when it reports none.
func (r *codexResponsesResponse) usage() *Usage {
	u := r.Usage
	if u == nil {
		return nil
	}
	hasTokens := u.InputTokens > 0 || u.OutputTokens > 0
	if !hasTokens {
		return nil
	}
	return &Usage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens}
}

// failureMessage describes a response.failed event's error.
func (ev codexResponsesEvent) failureMessage() string {
	if ev.Response == nil || ev.Response.Error == nil {
		return "unknown error"
	}
	if ev.Response.Error.Code == "" {
		return ev.Response.Error.Message
	}
	return ev.Response.Error.Code + ": " + ev.Response.Error.Message
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
			if len(m.MediaURLs) > 0 {
				messages = append(messages, openai.UserMessage(p.userContentParts(m)))
				continue
			}
			messages = append(messages, openai.UserMessage(m.Content))
		case RoleAssistant:
			var msg openai.ChatCompletionMessageParamUnion
			msg, i = openAIAssistantTurn(req.Messages, i)
			messages = append(messages, msg)
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
				Parameters:  shared.FunctionParameters(strictSchemaObject(tool.InputSchema)),
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
		pendingCalls := pendingToolCalls{}
		for stream.Next() {
			chunk := stream.Current()
			for _, choice := range chunk.Choices {
				if choice.Delta.Content != "" {
					ch <- Event{Type: EventTypeText, Text: choice.Delta.Content}
				}
				for _, tc := range choice.Delta.ToolCalls {
					pendingCalls.applyDelta(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments)
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
		if !pendingCalls.emit(ch, "openai") {
			return
		}
		if lastUsage != nil {
			ch <- Event{Type: EventTypeUsage, Usage: lastUsage}
		}
		ch <- Event{Type: EventTypeDone}
	}()

	return ch, nil
}

// userContentParts converts a user message with media into chat content
// parts: the text (if any), then images, then PDFs when the model accepts
// file input.
func (p *OpenAIProvider) userContentParts(m Message) []openai.ChatCompletionContentPartUnionParam {
	parts := make([]openai.ChatCompletionContentPartUnionParam, 0, 1+len(m.MediaURLs))
	if strings.TrimSpace(m.Content) != "" {
		parts = append(parts, openai.TextContentPart(m.Content))
	}
	for _, mediaURL := range m.MediaURLs {
		if isImageMediaURL(mediaURL) {
			parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
				URL: mediaURL,
			}))
			continue
		}
		if filename, fileData, ok := openAIFileInput(mediaURL); ok && p.fileInput {
			parts = append(parts, openai.FileContentPart(openai.ChatCompletionContentPartFileFileParam{
				Filename: openai.String(filename),
				FileData: openai.String(fileData),
			}))
		}
	}
	return parts
}

// openAIAssistantTurn folds the assistant message at msgs[i] and the
// assistant messages directly after it into one chat message, joining their
// text and collecting their tool calls. It returns the message and the index
// of the last message it consumed.
func openAIAssistantTurn(msgs []Message, i int) (openai.ChatCompletionMessageParamUnion, int) {
	assistantText := msgs[i].Content
	toolCalls := make([]openai.ChatCompletionMessageToolCallParam, 0, 1)
	if call := msgs[i].ToolCall; call != nil {
		toolCalls = append(toolCalls, openAIToolCallParam(call, i))
	}
	for i+1 < len(msgs) && msgs[i+1].Role == RoleAssistant {
		i++
		next := msgs[i]
		assistantText = appendLine(assistantText, next.Content)
		if next.ToolCall != nil {
			toolCalls = append(toolCalls, openAIToolCallParam(next.ToolCall, i))
		}
	}
	if len(toolCalls) == 0 {
		return openai.AssistantMessage(assistantText), i
	}
	msg := openai.ChatCompletionAssistantMessageParam{ToolCalls: toolCalls}
	if assistantText != "" {
		msg.Content.OfString = openai.String(assistantText)
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &msg}, i
}

// openAIToolCallParam converts a tool call from message index i, naming it
// "call_<i>" when it has no ID.
func openAIToolCallParam(call *ToolCall, i int) openai.ChatCompletionMessageToolCallParam {
	return openai.ChatCompletionMessageToolCallParam{
		ID: toolCallIDOrDefault(call, i),
		Function: openai.ChatCompletionMessageToolCallFunctionParam{
			Name:      call.Name,
			Arguments: mustJSONMap(call.Arguments),
		},
	}
}

// toolCallIDOrDefault returns the call's trimmed ID, or "call_<i>" when empty.
func toolCallIDOrDefault(call *ToolCall, i int) string {
	if id := strings.TrimSpace(call.ID); id != "" {
		return id
	}
	return "call_" + strconv.Itoa(i)
}

// openAIFileInput returns the filename and file data OpenAI file input takes
// for a PDF data URL. Unnamed PDFs are called "document.pdf".
func openAIFileInput(mediaURL string) (filename string, fileData string, ok bool) {
	mimeType, data, ok := ParseDocumentDataURL(mediaURL)
	if !ok {
		return "", "", false
	}
	filename = DataURLName(mediaURL)
	if filename == "" {
		filename = "document.pdf"
	}
	return filename, "data:" + mimeType + ";base64," + data, true
}

// openAIModelSupportsFileInput reports whether an OpenAI model reads PDF
// file input. Models older than GPT-4o and the text-only mini reasoning
// models do not.
func openAIModelSupportsFileInput(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "gpt-4" {
		return false
	}
	for _, prefix := range []string{"gpt-4-", "gpt-3", "o1-mini", "o1-preview", "o3-mini"} {
		if strings.HasPrefix(m, prefix) {
			return false
		}
	}
	for _, kind := range []string{"audio", "realtime", "transcribe", "tts"} {
		if strings.Contains(m, kind) {
			return false
		}
	}
	return true
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
		data, err := json.Marshal(m)
		if err != nil {
			return empty
		}
		if err := json.Unmarshal(data, &obj); err != nil {
			return empty
		}
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

func strictSchemaObject(schema any) map[string]any {
	obj := schemaObject(schema)
	applyStrictSchemaRules(obj)
	return obj
}

func applyStrictSchemaRules(value any) {
	switch v := value.(type) {
	case map[string]any:
		delete(v, "oneOf")
		delete(v, "anyOf")
		delete(v, "allOf")
		delete(v, "not")

		props, hasProps := v["properties"].(map[string]any)
		if t, _ := v["type"].(string); t == "object" || hasProps {
			if !hasProps {
				props = map[string]any{}
				v["properties"] = props
			}
			names := make([]string, 0, len(props))
			for name, prop := range props {
				names = append(names, name)
				applyStrictSchemaRules(prop)
			}
			sortStrings(names)
			required := make([]any, 0, len(names))
			for _, name := range names {
				required = append(required, name)
			}
			v["required"] = required
			v["additionalProperties"] = false
		}
		if items, ok := v["items"]; ok {
			applyStrictSchemaRules(items)
		}
	case []any:
		for _, item := range v {
			applyStrictSchemaRules(item)
		}
	}
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

func sortStrings(values []string) {
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}
