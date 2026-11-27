package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/auth"
)

const (
	copilotBaseURL       = "https://api.githubcopilot.com"
	copilotEditorVersion = "vscode/1.99.0"
	copilotIntegrationID = "vscode-chat"
)

// CopilotProvider calls the GitHub Copilot Chat Completions API.
// It automatically exchanges a GitHub user token (PAT or OAuth) for a
// short-lived Copilot API token and re-exchanges when it expires.
type CopilotProvider struct {
	ghToken    string // GitHub user token (source of truth for renewal)
	model      string
	httpClient *http.Client
	baseURL    string
	// exchangeToken trades the GitHub user token for a Copilot API token.
	// It is a field so tests can avoid the real token exchange.
	exchangeToken func(ctx context.Context, ghToken string) (string, time.Time, error)

	mu         sync.Mutex
	copilotTok string
	tokExpiry  time.Time
}

// NewCopilotProvider creates a provider backed by a GitHub user token.
// The token is exchanged for a Copilot API token on first use.
func NewCopilotProvider(ghToken, model string) *CopilotProvider {
	return &CopilotProvider{
		ghToken:       ghToken,
		model:         model,
		httpClient:    newDebugClient(nil),
		baseURL:       copilotBaseURL,
		exchangeToken: auth.CopilotTokenExchange,
	}
}

// NewCopilotHTTPProvider creates a CopilotProvider with a fixed API token and
// optional custom base URL. Intended for tests; skips GitHub token exchange.
func NewCopilotHTTPProvider(token, model, baseURL string) *CopilotProvider {
	if baseURL == "" {
		baseURL = copilotBaseURL
	}
	p := &CopilotProvider{
		model:      model,
		httpClient: newDebugClient(nil),
		baseURL:    strings.TrimRight(baseURL, "/"),
	}
	// Use the supplied token directly — no exchange needed.
	p.copilotTok = token
	p.tokExpiry = time.Now().Add(365 * 24 * time.Hour)
	return p
}

// getToken returns a valid Copilot API token, exchanging or refreshing as needed.
func (p *CopilotProvider) getToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.copilotTok != "" && time.Now().Before(p.tokExpiry.Add(-30*time.Second)) {
		return p.copilotTok, nil
	}
	if p.ghToken == "" {
		return p.copilotTok, nil
	}
	tok, expiry, err := p.exchangeToken(ctx, p.ghToken)
	if err != nil {
		return "", err
	}
	p.copilotTok = tok
	p.tokExpiry = expiry
	return tok, nil
}

func (p *CopilotProvider) setHeaders(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Editor-Version", copilotEditorVersion)
	req.Header.Set("Copilot-Integration-Id", copilotIntegrationID)
}

// Ping validates the token by listing available models.
func (p *CopilotProvider) Ping(ctx context.Context) error {
	tok, err := p.getToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	p.setHeaders(req, tok)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("copilot ping: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("copilot ping: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// Stream sends a Chat Completions request and returns a channel of events.
func (p *CopilotProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	tok, err := p.getToken(ctx)
	if err != nil {
		return nil, err
	}

	messages := make([]copilotMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, copilotMessage{Role: "system", Content: req.System})
	}
	var userMeta string
	for i := 0; i < len(req.Messages); i++ {
		m := req.Messages[i]
		switch m.Role {
		case RoleUser:
			if m.Result != nil {
				messages = append(messages, copilotMessage{
					Role:       "tool",
					Content:    m.Result.Content,
					ToolCallID: m.Result.ToolCallID,
				})
				continue
			}
			// Extract metadata if present (e.g., from m.Content or a new field)
			// For now, assume metadata is not in m.Content, but could be passed via a new field in Message in the future.
			// If you want to support metadata, parse it here and set userMeta accordingly.
			messages = append(messages, copilotMessage{Role: "user", Content: m.Content})
		case RoleAssistant:
			var msg copilotMessage
			msg, i = copilotAssistantTurn(req.Messages, i)
			messages = append(messages, msg)
		case RoleSystem:
			messages = append(messages, copilotMessage{Role: "system", Content: m.Content})
		}
	}
	// If userMeta is set, append a final assistant message with the metadata line
	if userMeta != "" {
		messages = append(messages, copilotMessage{Role: "assistant", Content: userMeta})
	}

	payload := map[string]any{
		"model":    p.model,
		"messages": messages,
		"stream":   true,
	}
	if req.MaxToks > 0 {
		payload["max_tokens"] = req.MaxToks
	}
	if len(req.Tools) > 0 {
		type fnDef struct {
			Name        string `json:"name"`
			Description string `json:"description,omitempty"`
			Parameters  any    `json:"parameters,omitempty"`
		}
		type toolDef struct {
			Type     string `json:"type"`
			Function fnDef  `json:"function"`
		}
		tools := make([]toolDef, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, toolDef{
				Type: "function",
				Function: fnDef{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  schemaObject(t.InputSchema),
				},
			})
		}
		payload["tools"] = tools
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("copilot: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("copilot: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	p.setHeaders(httpReq, tok)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("copilot: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("copilot: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	ch := make(chan Event, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close() //nolint:errcheck

		type tcDelta struct {
			Index    int64  `json:"index"`
			ID       string `json:"id"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		}
		type delta struct {
			Content   string    `json:"content"`
			ToolCalls []tcDelta `json:"tool_calls"`
		}
		type choice struct {
			Delta delta `json:"delta"`
		}
		type streamChunk struct {
			Choices []choice `json:"choices"`
			Usage   *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		scanner := newSSEScanner(resp.Body)
		var lastUsage *Usage
		pendingCalls := pendingToolCalls{}
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}
			var c streamChunk
			if err := json.Unmarshal([]byte(data), &c); err != nil {
				continue
			}
			for _, choice := range c.Choices {
				if choice.Delta.Content != "" {
					ch <- Event{Type: EventTypeText, Text: choice.Delta.Content}
				}
				for _, tc := range choice.Delta.ToolCalls {
					pendingCalls.applyDelta(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments)
				}
			}
			if c.Usage != nil && (c.Usage.PromptTokens > 0 || c.Usage.CompletionTokens > 0) {
				lastUsage = &Usage{
					InputTokens:  c.Usage.PromptTokens,
					OutputTokens: c.Usage.CompletionTokens,
				}
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- Event{Type: EventTypeError, Error: fmt.Errorf("copilot stream: %w", err)}
			return
		}
		if !pendingCalls.emit(ch, "copilot") {
			return
		}
		if lastUsage != nil {
			ch <- Event{Type: EventTypeUsage, Usage: lastUsage}
		}
		ch <- Event{Type: EventTypeDone}
	}()

	return ch, nil
}

type copilotToolCallFunc struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type copilotToolCall struct {
	Index    int64               `json:"index"`
	ID       string              `json:"id,omitempty"`
	Type     string              `json:"type,omitempty"`
	Function copilotToolCallFunc `json:"function,omitempty"`
}

type copilotMessage struct {
	Role       string            `json:"role"`
	Content    any               `json:"content,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	ToolCalls  []copilotToolCall `json:"tool_calls,omitempty"`
}

// copilotAssistantTurn folds the assistant message at msgs[i] and the
// assistant messages directly after it into one chat message, joining their
// text and collecting their tool calls. Messages with tool calls carry no
// content. It returns the message and the index of the last message it
// consumed.
func copilotAssistantTurn(msgs []Message, i int) (copilotMessage, int) {
	m := msgs[i]
	text := m.Content
	var toolCalls []copilotToolCall
	for {
		if m.ToolCall != nil {
			toolCalls = append(toolCalls, copilotToolCall{
				Index: int64(len(toolCalls)),
				ID:    toolCallIDOrDefault(m.ToolCall, i),
				Type:  "function",
				Function: copilotToolCallFunc{
					Name:      m.ToolCall.Name,
					Arguments: mustJSONMap(m.ToolCall.Arguments),
				},
			})
		}
		if i+1 >= len(msgs) || msgs[i+1].Role != RoleAssistant {
			break
		}
		i++
		m = msgs[i]
		if m.Content != "" {
			text += "\n" + m.Content
		}
	}
	if len(toolCalls) > 0 {
		return copilotMessage{Role: "assistant", ToolCalls: toolCalls}, i
	}
	return copilotMessage{Role: "assistant", Content: text}, i
}
