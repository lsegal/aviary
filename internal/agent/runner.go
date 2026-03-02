package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/memory"
	"github.com/lsegal/aviary/internal/store"
)

// AgentRunner manages an agent's active prompts and lifecycle.
type AgentRunner struct {
	agent    *domain.Agent
	cfg      *config.AgentConfig
	provider llm.Provider // nil until Phase 5 wiring; falls back to stub
	memory   *memory.Manager
	stopCh   chan struct{}
	mu       sync.Mutex
	active   sync.WaitGroup
	canceled bool
}

// NewAgentRunner creates an AgentRunner for the given agent.
func NewAgentRunner(a *domain.Agent, cfg *config.AgentConfig, provider llm.Provider, mem *memory.Manager) *AgentRunner {
	return &AgentRunner{
		agent:    a,
		cfg:      cfg,
		provider: provider,
		memory:   mem,
		stopCh:   make(chan struct{}),
	}
}

// Prompt sends a message to the agent and fans out stream events to consumers.
// Each call runs in its own goroutine; multiple concurrent calls are supported.
func (r *AgentRunner) Prompt(ctx context.Context, message string, consumers ...StreamConsumer) {
	r.mu.Lock()
	if r.canceled {
		r.mu.Unlock()
		for _, c := range consumers {
			c(StreamEvent{Type: StreamEventStop, AgentID: r.agent.ID})
		}
		return
	}
	r.active.Add(1)
	r.mu.Unlock()

	go func() {
		defer r.active.Done()

		promptCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		sessionID := r.resolveSessionID(promptCtx)
		untrack := trackSessionRun(sessionID, cancel)
		defer untrack()
		r.appendSessionMessage(sessionID, domain.MessageRoleUser, message)
		r.appendMemoryMessage(sessionID, domain.MessageRoleUser, message)

		slog.Info("agent: prompt started", "agent", r.agent.Name, "model", r.agent.Model)

		// Stop if stopCh is closed.
		go func() {
			select {
			case <-r.stopCh:
				cancel()
			case <-promptCtx.Done():
			}
		}()

		emit := func(e StreamEvent) {
			e.AgentID = r.agent.ID
			for _, c := range consumers {
				c(e)
			}
		}

		emitCanceled := func() {
			emit(StreamEvent{Type: StreamEventStop})
		}

		if r.provider == nil {
			if promptCtx.Err() != nil {
				emitCanceled()
				return
			}
			// Stub: no LLM provider configured.
			slog.Warn("agent: no provider", "agent", r.agent.Name, "model", r.agent.Model)
			emit(StreamEvent{Type: StreamEventText, Text: "[no LLM provider configured for " + r.agent.Model + "]"})
			emit(StreamEvent{Type: StreamEventDone})
			return
		}

		toolClient, err := newToolClientFactory(promptCtx)
		if err != nil {
			emit(StreamEvent{Type: StreamEventError, Err: err})
			return
		}
		if toolClient != nil {
			defer toolClient.Close() //nolint:errcheck
		}

		tools, _ := listToolsSafe(promptCtx, toolClient)
		systemPrompt := buildToolSystemPrompt(tools)
		if memContext := r.loadMemoryContext(sessionID, 1200); memContext != "" {
			systemPrompt += "\n\n" + memContext
		}

		conversation := []llm.Message{{Role: llm.RoleUser, Content: message}}
		if history := r.loadSessionConversation(sessionID, 24); len(history) > 0 {
			conversation = history
		}
		toolNames := make(map[string]struct{}, len(tools))
		for _, t := range tools {
			toolNames[t.Name] = struct{}{}
		}

		const maxToolRounds = 8
		for round := 0; round < maxToolRounds; round++ {
			if promptCtx.Err() != nil {
				emitCanceled()
				return
			}
			req := llm.Request{
				Model:    r.agent.Model,
				Messages: conversation,
				System:   systemPrompt,
				Stream:   true,
			}

			ch, err := r.provider.Stream(promptCtx, req)
			if err != nil {
				if errors.Is(err, context.Canceled) || promptCtx.Err() != nil {
					emitCanceled()
					return
				}
				slog.Error("agent: stream error", "agent", r.agent.Name, "err", err)
				emit(StreamEvent{Type: StreamEventError, Err: err})
				return
			}

			var modelOut strings.Builder
			for event := range ch {
				switch event.Type {
				case llm.EventTypeText:
					modelOut.WriteString(event.Text)
				case llm.EventTypeError:
					if errors.Is(event.Error, context.Canceled) || promptCtx.Err() != nil {
						emitCanceled()
						return
					}
					emit(StreamEvent{Type: StreamEventError, Err: event.Error})
					return
				case llm.EventTypeDone:
				}
			}
			if promptCtx.Err() != nil {
				emitCanceled()
				return
			}

			answer := strings.TrimSpace(modelOut.String())
			call, ok := parseToolCall(answer)
			if !ok || toolClient == nil {
				emit(StreamEvent{Type: StreamEventText, Text: answer})
				r.appendSessionMessage(sessionID, domain.MessageRoleAssistant, answer)
				r.appendMemoryMessage(sessionID, domain.MessageRoleAssistant, answer)
				slog.Info("agent: prompt done", "agent", r.agent.Name)
				emit(StreamEvent{Type: StreamEventDone})
				return
			}

			if _, exists := toolNames[call.Tool]; !exists {
				conversation = append(conversation,
					llm.Message{Role: llm.RoleAssistant, Content: answer},
					llm.Message{Role: llm.RoleUser, Content: fmt.Sprintf("Tool %q is not available. Choose one of the available tools.", call.Tool)},
				)
				continue
			}

			emit(StreamEvent{Type: StreamEventText, Text: fmt.Sprintf("[tool] %s", call.Tool)})
			resultText, callErr := toolClient.CallToolText(promptCtx, call.Tool, call.Arguments)
			if callErr != nil {
				if errors.Is(callErr, context.Canceled) || promptCtx.Err() != nil {
					emitCanceled()
					return
				}
				resultText = "error: " + callErr.Error()
			}

			conversation = append(conversation,
				llm.Message{Role: llm.RoleAssistant, Content: answer},
				llm.Message{Role: llm.RoleUser, Content: fmt.Sprintf("Tool result for %s:\n%s\n\nIf the task is complete, answer normally. If you need another tool call, respond with only JSON.", call.Tool, resultText)},
			)
		}

		r.appendSessionMessage(sessionID, domain.MessageRoleAssistant, fmt.Sprintf("Error: tool loop exceeded %d rounds", maxToolRounds))
		r.appendMemoryMessage(sessionID, domain.MessageRoleAssistant, fmt.Sprintf("Error: tool loop exceeded %d rounds", maxToolRounds))
		emit(StreamEvent{Type: StreamEventError, Err: fmt.Errorf("tool loop exceeded %d rounds", maxToolRounds)})
	}()
}

func (r *AgentRunner) resolveSessionID(ctx context.Context) string {
	// session in context takes precedence when set by caller (e.g. MCP agent_run)
	// fallback to the agent's main session for background/channel prompts.
	if sid, ok := SessionIDFromContext(ctx); ok {
		return sid
	}
	sess, err := NewSessionManager().GetOrCreateNamed(r.agent.ID, "main")
	if err != nil || sess == nil || sess.ID == "" {
		return r.agent.ID + "-main"
	}
	return sess.ID
}

func (r *AgentRunner) appendSessionMessage(sessionID string, role domain.MessageRole, content string) {
	if strings.TrimSpace(content) == "" || sessionID == "" {
		return
	}
	msg := domain.Message{
		ID:        fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	}
	if err := store.AppendJSONL(store.SessionPath(sessionID), msg); err != nil {
		slog.Warn("agent: failed to persist session message", "agent", r.agent.Name, "session", sessionID, "err", err)
		return
	}
	notifySessionMessage(sessionID, string(role))
}

func (r *AgentRunner) appendMemoryMessage(sessionID string, role domain.MessageRole, content string) {
	if r.memory == nil || strings.TrimSpace(content) == "" {
		return
	}
	poolID := r.memoryPoolID()
	if err := r.memory.Append(poolID, sessionID, string(role), content); err != nil {
		slog.Warn("agent: failed to append memory", "agent", r.agent.Name, "pool", poolID, "err", err)
	}
}

func (r *AgentRunner) loadMemoryContext(sessionID string, maxTokens int) string {
	if r.memory == nil {
		return ""
	}
	entries, err := r.memory.LoadContext(r.memoryPoolID(), maxTokens)
	if err != nil || len(entries) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Memory context (persisted across conversations):\n")
	for _, e := range entries {
		if strings.TrimSpace(e.Content) == "" {
			continue
		}
		role := strings.TrimSpace(e.Role)
		if role == "" {
			role = "note"
		}
		b.WriteString("- ")
		b.WriteString(role)
		if e.SessionID != "" && e.SessionID != sessionID {
			b.WriteString(" (session ")
			b.WriteString(e.SessionID)
			b.WriteString(")")
		}
		b.WriteString(": ")
		b.WriteString(e.Content)
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}

func (r *AgentRunner) loadSessionConversation(sessionID string, maxMessages int) []llm.Message {
	if sessionID == "" {
		return nil
	}

	lines, err := store.ReadJSONL[map[string]any](store.SessionPath(sessionID))
	if err != nil || len(lines) == 0 {
		return nil
	}

	messages := make([]llm.Message, 0, len(lines))
	for _, line := range lines {
		role, _ := line["role"].(string)
		content, _ := line["content"].(string)
		if strings.TrimSpace(role) == "" || strings.TrimSpace(content) == "" {
			continue
		}

		switch domain.MessageRole(role) {
		case domain.MessageRoleUser:
			messages = append(messages, llm.Message{Role: llm.RoleUser, Content: content})
		case domain.MessageRoleAssistant:
			messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: content})
		case domain.MessageRoleSystem:
			messages = append(messages, llm.Message{Role: llm.RoleSystem, Content: content})
		}
	}

	if maxMessages > 0 && len(messages) > maxMessages {
		messages = messages[len(messages)-maxMessages:]
	}
	return messages
}

func (r *AgentRunner) memoryPoolID() string {
	memoryName := strings.TrimSpace(r.cfg.Memory)
	switch memoryName {
	case "", "private":
		return "private:" + r.agent.Name
	case "shared":
		return "shared"
	default:
		return memoryName
	}
}

func listToolsSafe(ctx context.Context, toolClient ToolClient) ([]ToolInfo, error) {
	if toolClient == nil {
		return nil, nil
	}
	tools, err := toolClient.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	return tools, nil
}

type toolCall struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}

func parseToolCall(s string) (toolCall, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return toolCall{}, false
	}
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)

	var tc toolCall
	if err := json.Unmarshal([]byte(trimmed), &tc); err == nil && tc.Tool != "" {
		if tc.Arguments == nil {
			tc.Arguments = map[string]any{}
		}
		return tc, true
	}

	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		fragment := strings.TrimSpace(trimmed[start : end+1])
		if err := json.Unmarshal([]byte(fragment), &tc); err == nil && tc.Tool != "" {
			if tc.Arguments == nil {
				tc.Arguments = map[string]any{}
			}
			return tc, true
		}
	}

	return toolCall{}, false
}

func buildToolSystemPrompt(tools []ToolInfo) string {
	var sb strings.Builder
	sb.WriteString("You are an autonomous local assistant. Use tools when needed.\n")
	sb.WriteString("If you decide to call a tool, respond with ONLY valid JSON in this exact shape: {\"tool\":\"<name>\",\"arguments\":{...}}\n")
	sb.WriteString("Do not include markdown when calling a tool.\n")
	sb.WriteString("After receiving tool results, either call another tool with JSON or provide the final user-facing answer as plain text.\n\n")

	if skills, err := DiscoverSkills("."); err == nil && len(skills) > 0 {
		sb.WriteString("Available skills:\n")
		for _, sk := range skills {
			sb.WriteString("- ")
			sb.WriteString(sk.Name)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
		sb.WriteString(BuildSystemPrompt("", skills))
		sb.WriteString("\n\n")
	}

	sb.WriteString("Available tools:\n")
	for _, t := range tools {
		sb.WriteString("- ")
		sb.WriteString(t.Name)
		if t.Description != "" {
			sb.WriteString(": ")
			sb.WriteString(t.Description)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// Stop cancels all in-flight prompts for this agent.
func (r *AgentRunner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.canceled {
		r.canceled = true
		close(r.stopCh)
	}
}

// Wait blocks until all active prompts finish.
func (r *AgentRunner) Wait() { r.active.Wait() }

// Agent returns the domain agent.
func (r *AgentRunner) Agent() *domain.Agent { return r.agent }

// Config returns the agent's config snapshot.
func (r *AgentRunner) Config() *config.AgentConfig { return r.cfg }
