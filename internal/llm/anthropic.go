package llm

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const anthropicStrictToolLimit = 20

// The OAuth provider impersonates the Claude CLI. These values were captured
// from claude-cli 2.1.312 (interactive, OAuth) by pointing ANTHROPIC_BASE_URL
// at a localhost proxy and recording its POST /v1/messages requests. The API
// rejects OAuth requests from CLI versions it considers too old for a model
// (claude_code_version_too_old), so refresh these from a new capture when
// that error appears.
const (
	claudeCLIVersion   = "2.1.312"
	claudeCLIUserAgent = "claude-cli/" + claudeCLIVersion + " (external, cli)"
	claudeCLIBetas     = "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,per-turn-control-2026-07-01,mid-conversation-tool-changes-2026-07-01,inline-tools-2026-09-15,advisor-tool-2026-03-01,thinking-token-count-2026-05-13,effort-2025-11-24"
	// claudeCLIBillingHeader is the first system block of every CLI request.
	// cc_version and cc_entrypoint are required for OAuth tokens to reach
	// sonnet/opus models; the CLI no longer sends cch.
	claudeCLIBillingHeader = "x-anthropic-billing-header: cc_version=" + claudeCLIVersion + ".7fc; cc_entrypoint=cli;"
)

// AnthropicProvider streams responses from Anthropic's Claude models.
type AnthropicProvider struct {
	client anthropic.Client
	model  string
	oauth  bool // true when using Bearer token auth (Claude Pro/Max)
}

// NewAnthropicProvider creates a provider for the given Claude model using an API key.
func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	opts := []option.RequestOption{}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	opts = append(opts, option.WithHTTPClient(newDebugClient(nil)))
	return &AnthropicProvider{
		client: anthropic.NewClient(opts...),
		model:  model,
	}
}

// NewAnthropicOAuthProvider creates a provider using an OAuth Bearer token (Claude Pro/Max).
func NewAnthropicOAuthProvider(accessToken, model string) *AnthropicProvider {
	// WithAuthToken sets Authorization: Bearer without injecting x-api-key.
	opts := []option.RequestOption{
		option.WithAuthToken(accessToken),
		option.WithHTTPClient(newOAuthClient()),
		option.WithQuery("beta", "true"),
		option.WithHeader("anthropic-beta", claudeCLIBetas),
		option.WithHeader("anthropic-dangerous-direct-browser-access", "true"),
		option.WithHeader("anthropic-version", "2023-06-01"),
		option.WithHeader("user-agent", claudeCLIUserAgent),
		option.WithHeader("x-app", "cli"),
		option.WithHeader("x-claude-code-session-id", newSessionID()),
	}
	return &AnthropicProvider{
		client: anthropic.NewClient(opts...),
		model:  model,
		oauth:  true,
	}
}

// Ping validates Anthropic credentials. For API key auth it uses GET /v1/models
// (token-free). For OAuth, /v1/models is not accessible with bearer tokens so
// it falls back to a minimal 1-token message.
func (p *AnthropicProvider) Ping(ctx context.Context) error {
	if !p.oauth {
		_, err := p.client.Models.List(ctx, anthropic.ModelListParams{})
		return err
	}
	ch, err := p.Stream(ctx, Request{
		Model:    p.model,
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
		MaxToks:  1,
	})
	if err != nil {
		return err
	}
	for ev := range ch {
		if ev.Type == EventTypeError {
			return ev.Error
		}
	}
	return nil
}

// Stream sends a request to Anthropic and returns a streaming event channel.
func (p *AnthropicProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	messages := make([]anthropic.MessageParam, 0, len(req.Messages))
	appendMsg := func(msg anthropic.MessageParam) {
		// Merge consecutive messages of the same role to satisfy Anthropic's
		// strict alternation requirement (user/assistant/user/...).
		if len(messages) > 0 && messages[len(messages)-1].Role == msg.Role {
			messages[len(messages)-1].Content = append(messages[len(messages)-1].Content, msg.Content...)
			return
		}
		messages = append(messages, msg)
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			if m.Result != nil {
				appendMsg(anthropic.NewUserMessage(anthropic.NewToolResultBlock(
					m.Result.ToolCallID,
					m.Result.Content,
					m.Result.IsError,
				)))
				continue
			}
			if len(m.MediaURLs) > 0 {
				appendMsg(anthropic.NewUserMessage(anthropicUserBlocks(m)...))
				continue
			}
			appendMsg(anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case RoleAssistant:
			if m.ToolCall != nil {
				appendMsg(anthropic.NewAssistantMessage(anthropic.NewToolUseBlock(
					m.ToolCall.ID,
					m.ToolCall.Arguments,
					m.ToolCall.Name,
				)))
				continue
			}
			appendMsg(anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content)))
		case RoleSystem:
			// Anthropic does not support a "system" role inside the messages array;
			// inject as a user turn. The appendMsg helper merges it with any
			// adjacent user messages to avoid alternation violations.
			appendMsg(anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		}
	}

	maxToks := int64(4096)
	if req.MaxToks > 0 {
		maxToks = int64(req.MaxToks)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(p.model),
		Messages:  messages,
		MaxTokens: maxToks,
	}
	if len(req.Tools) > 0 {
		useStrictTools := len(req.Tools) <= anthropicStrictToolLimit
		params.Tools = make([]anthropic.ToolUnionParam, 0, len(req.Tools))
		for _, tool := range req.Tools {
			schema := anthropicToolSchema(tool.InputSchema)
			params.Tools = append(params.Tools, anthropic.ToolUnionParamOfTool(schema, tool.Name))
			params.Tools[len(params.Tools)-1].OfTool.Description = anthropic.String(tool.Description)
			params.Tools[len(params.Tools)-1].OfTool.InputExamples = tool.Examples
			if useStrictTools {
				params.Tools[len(params.Tools)-1].OfTool.Strict = anthropic.Bool(true)
			}
		}
	}
	if p.oauth {
		// Prepend the Claude Code billing header that grants OAuth tokens access
		// to sonnet/opus models.
		billingHdr := anthropic.TextBlockParam{Text: claudeCLIBillingHeader}
		if req.System != "" {
			params.System = []anthropic.TextBlockParam{billingHdr, {Text: req.System}}
		} else {
			params.System = []anthropic.TextBlockParam{billingHdr}
		}
	} else if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	if req.CacheControl != nil && strings.EqualFold(strings.TrimSpace(req.CacheControl.Type), "ephemeral") {
		setAnthropicCacheBreakpoints(&params)
	}

	stream := p.client.Messages.NewStreaming(ctx, params)

	ch := make(chan Event, 32)
	go func() {
		defer close(ch)
		var usageData *Usage
		pendingCalls := pendingToolCalls{}
		for stream.Next() {
			event := stream.Current()
			switch e := event.AsAny().(type) {
			case anthropic.MessageStartEvent:
				u := e.Message.Usage
				usageData = &Usage{
					InputTokens:      int(u.InputTokens),
					OutputTokens:     int(u.OutputTokens),
					CacheReadTokens:  int(u.CacheReadInputTokens),
					CacheWriteTokens: int(u.CacheCreationInputTokens),
				}
			case anthropic.ContentBlockStartEvent:
				if block, ok := e.ContentBlock.AsAny().(anthropic.ToolUseBlock); ok {
					call := &pendingToolCall{ID: block.ID, Name: block.Name}
					if len(block.Input) > 0 {
						call.Arguments.Write(block.Input)
					}
					pendingCalls[e.Index] = call
				}
			case anthropic.ContentBlockDeltaEvent:
				if delta, ok := e.Delta.AsAny().(anthropic.TextDelta); ok {
					ch <- Event{Type: EventTypeText, Text: delta.Text}
					continue
				}
				if delta, ok := e.Delta.AsAny().(anthropic.InputJSONDelta); ok {
					pendingCalls.at(e.Index).appendStreamedArguments(delta.PartialJSON)
				}
			case anthropic.MessageDeltaEvent:
				// MessageDeltaEvent carries cumulative totals; input and cache
				// counts may be omitted, so keep message_start's values then.
				if usageData == nil {
					usageData = &Usage{}
				}
				usageData.OutputTokens = int(e.Usage.OutputTokens)
				if e.Usage.InputTokens > 0 {
					usageData.InputTokens = int(e.Usage.InputTokens)
				}
				if e.Usage.CacheReadInputTokens > 0 {
					usageData.CacheReadTokens = int(e.Usage.CacheReadInputTokens)
				}
				if e.Usage.CacheCreationInputTokens > 0 {
					usageData.CacheWriteTokens = int(e.Usage.CacheCreationInputTokens)
				}
			}
		}
		if err := stream.Err(); err != nil {
			ch <- Event{Type: EventTypeError, Error: fmt.Errorf("anthropic stream: %w", err)}
			return
		}
		if !pendingCalls.emit(ch, "anthropic") {
			return
		}
		if usageData != nil {
			ch <- Event{Type: EventTypeUsage, Usage: usageData}
		}
		ch <- Event{Type: EventTypeDone}
	}()

	return ch, nil
}

// anthropicUserBlocks converts a user message with media into content blocks:
// the text (if any), then each image, PDF, or remote image URL. Other local
// data and file URLs are dropped.
func anthropicUserBlocks(m Message) []anthropic.ContentBlockParamUnion {
	blocks := make([]anthropic.ContentBlockParamUnion, 0, 1+len(m.MediaURLs))
	if strings.TrimSpace(m.Content) != "" {
		blocks = append(blocks, anthropic.NewTextBlock(m.Content))
	}
	for _, mediaURL := range m.MediaURLs {
		if block, ok := anthropicMediaBlock(mediaURL); ok {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// anthropicMediaBlock converts one media URL into a content block.
func anthropicMediaBlock(mediaURL string) (anthropic.ContentBlockParamUnion, bool) {
	if mimeType, data, ok := ParseImageDataURL(mediaURL); ok {
		return anthropic.NewImageBlockBase64(mimeType, data), true
	}
	if _, data, ok := ParseDocumentDataURL(mediaURL); ok {
		return anthropic.NewDocumentBlock(anthropic.Base64PDFSourceParam{Data: data}), true
	}
	isLocal := strings.HasPrefix(mediaURL, "data:") || strings.HasPrefix(mediaURL, "file:")
	if isLocal {
		return anthropic.ContentBlockParamUnion{}, false
	}
	return anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: mediaURL}), true
}

// newSessionID returns a random UUIDv4 for the X-Claude-Code-Session-Id
// header, which the CLI sends once per session.
func newSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// setAnthropicCacheBreakpoints marks the end of the tool definitions, the
// system prompt, and the latest history message as prompt-cache breakpoints,
// so a replayed conversation is billed at the cache-read rate up to its newest
// turn. That uses three of the four breakpoints the API allows.
func setAnthropicCacheBreakpoints(params *anthropic.MessageNewParams) {
	if n := len(params.Tools); n > 0 && params.Tools[n-1].OfTool != nil {
		params.Tools[n-1].OfTool.CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	if n := len(params.System); n > 0 {
		params.System[n-1].CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	if n := len(params.Messages); n > 0 {
		content := params.Messages[n-1].Content
		if m := len(content); m > 0 {
			if cc := content[m-1].GetCacheControl(); cc != nil {
				*cc = anthropic.NewCacheControlEphemeralParam()
			}
		}
	}
}

func anthropicToolSchema(schema any) anthropic.ToolInputSchemaParam {
	obj := schemaObject(schema)
	properties := obj["properties"]
	required, _ := obj["required"].([]any)
	requiredStrings := make([]string, 0, len(required))
	for _, item := range required {
		if s, ok := item.(string); ok && s != "" {
			requiredStrings = append(requiredStrings, s)
		}
	}
	if alt, ok := obj["required"].([]string); ok {
		requiredStrings = append([]string{}, alt...)
	}
	extra := map[string]any{}
	for key, value := range obj {
		if key == "type" || key == "properties" || key == "required" {
			continue
		}
		extra[key] = value
	}
	return anthropic.ToolInputSchemaParam{
		Properties:  properties,
		Required:    requiredStrings,
		ExtraFields: extra,
	}
}
