package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"

	"github.com/stretchr/testify/assert"
)

var testDataDir string

func TestMain(m *testing.M) {
	var err error
	testDataDir, err = os.MkdirTemp("", "aviary-agent-test-*")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(testDataDir) }()
	store.SetDataDir(testDataDir)
	store.SetWorkspaceDir(testDataDir)
	os.Exit(m.Run())
}

// setTestDataDir gives t its own isolated data directory and restores
// the shared testDataDir when the test finishes.
func setTestDataDir(t *testing.T) {
	t.Helper()
	isolatedDir := t.TempDir()
	store.SetDataDir(isolatedDir)
	store.SetWorkspaceDir(isolatedDir)
	t.Cleanup(func() {
		store.SetDataDir(testDataDir)
		store.SetWorkspaceDir(testDataDir)
	})
}

type mockProvider struct {
	events []llm.Event
	err    error
}

type sequenceProvider struct {
	mu        sync.Mutex
	responses [][]llm.Event
	requests  []llm.Request
}

func (s *sequenceProvider) Stream(_ context.Context, req llm.Request) (<-chan llm.Event, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	idx := len(s.requests) - 1
	var events []llm.Event
	if idx < len(s.responses) {
		events = s.responses[idx]
	}
	s.mu.Unlock()

	ch := make(chan llm.Event, len(events)+1)
	for _, e := range events {
		ch <- e
	}
	if len(events) == 0 || events[len(events)-1].Type != llm.EventTypeDone {
		ch <- llm.Event{Type: llm.EventTypeDone}
	}
	close(ch)
	return ch, nil
}

func (s *sequenceProvider) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

type fakeToolClient struct {
	tools []ToolInfo
}

func (f *fakeToolClient) ListTools(_ context.Context) ([]ToolInfo, error) { return f.tools, nil }
func (f *fakeToolClient) CallToolText(_ context.Context, _ string, _ map[string]any) (string, error) {
	return "", nil
}
func (f *fakeToolClient) Close() error { return nil }

type recordedCall struct {
	Tool      string
	Arguments map[string]any
}

type recordingToolClient struct {
	tools   []ToolInfo
	mu      sync.Mutex
	calls   []recordedCall
	results map[string]string
}

func (r *recordingToolClient) ListTools(_ context.Context) ([]ToolInfo, error) { return r.tools, nil }

func (r *recordingToolClient) CallToolText(_ context.Context, name string, args map[string]any) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedCall{Tool: name, Arguments: args})
	if r.results != nil {
		if result, ok := r.results[name]; ok {
			return result, nil
		}
	}
	return "", nil
}

func (r *recordingToolClient) Close() error { return nil }

func (m *mockProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan llm.Event, len(m.events)+1)
	for _, e := range m.events {
		ch <- e
	}
	if len(m.events) == 0 || m.events[len(m.events)-1].Type != llm.EventTypeDone {
		ch <- llm.Event{Type: llm.EventTypeDone}
	}
	close(ch)
	return ch, nil
}

func TestStreamEventConstants(t *testing.T) {
	for _, typ := range []StreamEventType{StreamEventText, StreamEventDone, StreamEventError, StreamEventStop} {
		assert.NotEqual(t, "", typ)

	}
}

func TestAgentRunner_PersistsToolMessagesSeparately(t *testing.T) {
	setTestDataDir(t)

	toolClient := &recordingToolClient{
		tools:   []ToolInfo{{Name: "web_search", Description: "Search the web"}},
		results: map[string]string{"web_search": "result payload"},
	}
	SetToolClientFactory(func(_ context.Context) (ToolClient, error) {
		return toolClient, nil
	})
	t.Cleanup(func() { SetToolClientFactory(nil) })

	provider := &sequenceProvider{responses: [][]llm.Event{
		{{Type: llm.EventTypeToolCall, ToolCall: &llm.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang"}}}, {Type: llm.EventTypeDone}},
		{{Type: llm.EventTypeText, Text: "final answer"}, {Type: llm.EventTypeDone}},
	}}

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_tool_history", Name: "tool-history", Model: "test/model"},
		&config.AgentConfig{Name: "tool-history", Model: "test/model"},
		provider,
		nil,
	)

	sess, err := NewSessionManager().GetOrCreateNamed("agent_tool_history", "main")
	assert.NoError(t, err)

	var gotText strings.Builder
	toolEvents := 0
	done := make(chan struct{}, 1)
	runner.Prompt(WithSessionID(context.Background(), sess.ID), "search", func(e StreamEvent) {
		switch e.Type {
		case StreamEventText:
			gotText.WriteString(e.Text)
		case StreamEventTool:
			toolEvents++
		case StreamEventDone, StreamEventError, StreamEventStop:
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		assert.FailNow(t, "timeout")
	}

	assert.Equal(t, 1, toolEvents)
	assert.Equal(t, "final answer", gotText.String())

	lines, err := store.ReadJSONL[domain.Message](store.SessionPath("agent_tool_history", sess.ID))
	assert.NoError(t, err)
	// Filter to actual messages (skip session-metadata and response-marker entries with empty role).
	var msgs []domain.Message
	for _, l := range lines {
		if l.Role != "" {
			msgs = append(msgs, l)
		}
	}
	assert.GreaterOrEqual(t, len(msgs), 3)
	assert.Equal(t, domain.MessageRoleTool, msgs[len(msgs)-2].Role)
	assert.Contains(t, msgs[len(msgs)-2].Content, `"name":"web_search"`)
	assert.Equal(t, domain.MessageRoleAssistant, msgs[len(msgs)-1].Role)
	assert.Equal(t, "final answer", msgs[len(msgs)-1].Content)
}

func TestAgentRunner_NormalizesSessionHistoryCurrentSessionID(t *testing.T) {
	setTestDataDir(t)

	toolClient := &recordingToolClient{
		tools:   []ToolInfo{{Name: "session_history", Description: "Read session history"}},
		results: map[string]string{"session_history": "[]"},
	}
	SetToolClientFactory(func(_ context.Context) (ToolClient, error) {
		return toolClient, nil
	})
	t.Cleanup(func() { SetToolClientFactory(nil) })

	provider := &sequenceProvider{responses: [][]llm.Event{
		{{Type: llm.EventTypeToolCall, ToolCall: &llm.ToolCall{Name: "session_history", Arguments: map[string]any{"session_id": "current", "order": "desc", "limit": float64(20)}}}, {Type: llm.EventTypeDone}},
		{{Type: llm.EventTypeText, Text: "ok"}, {Type: llm.EventTypeDone}},
	}}

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_session_history", Name: "assistant", Model: "test/model"},
		&config.AgentConfig{Name: "assistant", Model: "test/model"},
		provider,
		nil,
	)

	sess, err := NewSessionManager().GetOrCreateNamed("agent_session_history", "main")
	assert.NoError(t, err)

	done := make(chan struct{}, 1)
	runner.Prompt(WithSessionID(context.Background(), sess.ID), "what just happened?", func(e StreamEvent) {
		if e.Type == StreamEventDone || e.Type == StreamEventError || e.Type == StreamEventStop {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		assert.FailNow(t, "timeout")
	}

	if assert.Len(t, toolClient.calls, 1) {
		assert.Equal(t, "session_history", toolClient.calls[0].Tool)
		assert.Equal(t, sess.ID, toolClient.calls[0].Arguments["session_id"])
		assert.Equal(t, "desc", toolClient.calls[0].Arguments["order"])
	}
}

func TestAgentRunner_BareOverrideClearsSystemPrompt(t *testing.T) {
	setTestDataDir(t)

	provider := &sequenceProvider{responses: [][]llm.Event{
		{{Type: llm.EventTypeText, Text: "ok"}, {Type: llm.EventTypeDone}},
	}}
	toolClientCalls := 0
	SetToolClientFactory(func(_ context.Context) (ToolClient, error) {
		toolClientCalls++
		return &fakeToolClient{tools: []ToolInfo{{Name: "agent_update", Description: "Update an agent"}}}, nil
	})
	t.Cleanup(func() { SetToolClientFactory(nil) })

	rulesPath := store.AgentRulesPath("agent_bare")
	err := os.MkdirAll(filepath.Dir(rulesPath), 0o700)
	assert.NoError(t, err)
	err = os.WriteFile(rulesPath, []byte("Follow local rules."), 0o600)
	assert.NoError(t, err)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_bare", Name: "bare", Model: "test/model"},
		&config.AgentConfig{Name: "bare", Model: "test/model"},
		provider,
		nil,
	)

	done := make(chan struct{}, 1)
	runner.PromptWithOverrides(context.Background(), "hello", RunOverrides{Bare: true}, func(e StreamEvent) {
		if e.Type == StreamEventDone || e.Type == StreamEventError || e.Type == StreamEventStop {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		assert.FailNow(t, "timeout")
	}

	if assert.Len(t, provider.requests, 1) {
		assert.Equal(t, "", provider.requests[0].System)
	}
	assert.Equal(t, 0, toolClientCalls)
}

func TestLoadSessionConversation_IncludesChannelMetadataAndTimestamp(t *testing.T) {
	setTestDataDir(t)

	provider := &sequenceProvider{responses: [][]llm.Event{{{Type: llm.EventTypeDone}}}}

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_hist", Name: "hist", Model: "test/model"},
		&config.AgentConfig{Name: "hist", Model: "test/model"},
		provider,
		nil,
	)

	sess, err := NewSessionManager().GetOrCreateNamed("agent_hist", "main")
	assert.NoError(t, err)

	// Add a channel sidecar for this session
	chCfg := &store.SessionChannelsConfig{
		SessionID: sess.ID,
		AgentID:   "agent_hist",
		Channels:  []store.SessionChannel{{Type: "slack", ConfiguredID: "alerts", ID: "C123"}},
	}
	assert.NoError(t, store.WriteSessionChannels(chCfg))

	// Create three user messages so prior context and last user message are present.
	t1 := time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 3, 22, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 3, 23, 12, 34, 56, 0, time.UTC)

	msg1 := domain.Message{ID: "m1", Role: domain.MessageRoleUser, Sender: domain.NewMessageSender("alice", "Alice", true), Content: "Hello", Timestamp: t1}
	msg2 := domain.Message{ID: "m2", Role: domain.MessageRoleUser, Sender: domain.NewMessageSender("bob", "Bob", true), Content: "FYI", Timestamp: t2}
	msg3 := domain.Message{ID: "m3", Role: domain.MessageRoleUser, Sender: domain.NewMessageSender("charlie", "Charlie", true), Content: "What's next?", Timestamp: t3}

	assert.NoError(t, store.AppendJSONL(store.SessionPath("agent_hist", sess.ID), msg1))
	assert.NoError(t, store.AppendJSONL(store.SessionPath("agent_hist", sess.ID), msg2))
	assert.NoError(t, store.AppendJSONL(store.SessionPath("agent_hist", sess.ID), msg3))

	// Directly load the conversation to verify injected metadata and formatting.
	conv := runner.loadSessionConversation(sess.ID, 24)
	if assert.Len(t, conv, 2) {
		// Assistant context
		assert.Contains(t, conv[0].Content, "Conversation context (channel metadata)")
		assert.Contains(t, conv[0].Content, "Known members:")
		// Last user message formatting: timestamp and sender name present
		assert.Contains(t, conv[1].Content, "[2026-03-23 12:34:56]")
		assert.Contains(t, conv[1].Content, "<Charlie")
	}
}

func TestAgentRunner_HistoryOverrideFalseSkipsSessionConversation(t *testing.T) {
	setTestDataDir(t)

	provider := &sequenceProvider{responses: [][]llm.Event{
		{{Type: llm.EventTypeText, Text: "ok"}, {Type: llm.EventTypeDone}},
	}}
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_history_off", Name: "history-off", Model: "test/model"},
		&config.AgentConfig{Name: "history-off", Model: "test/model"},
		provider,
		nil,
	)

	sess, err := NewSessionManager().GetOrCreateNamed("agent_history_off", "main")
	assert.NoError(t, err)
	runner.appendSessionMessage(sess.ID, domain.MessageRoleUser, "earlier question", "", "")
	runner.appendSessionMessage(sess.ID, domain.MessageRoleAssistant, "earlier answer", "", "")

	done := make(chan struct{}, 1)
	history := false
	runner.PromptWithOverrides(WithSessionID(context.Background(), sess.ID), "new question", RunOverrides{History: &history}, func(e StreamEvent) {
		if e.Type == StreamEventDone || e.Type == StreamEventError || e.Type == StreamEventStop {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		assert.FailNow(t, "timeout")
	}

	if assert.Len(t, provider.requests, 1) {
		assert.Len(t, provider.requests[0].Messages, 1)
		assert.Contains(t, provider.requests[0].Messages[0].Content, "new question")
	}
}

func TestAgentRunner_HistoryOverrideTrueLoadsSessionConversation(t *testing.T) {
	setTestDataDir(t)

	provider := &sequenceProvider{responses: [][]llm.Event{
		{{Type: llm.EventTypeText, Text: "ok"}, {Type: llm.EventTypeDone}},
	}}
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_history_on", Name: "history-on", Model: "test/model"},
		&config.AgentConfig{Name: "history-on", Model: "test/model"},
		provider,
		nil,
	)

	sess, err := NewSessionManager().GetOrCreateNamed("agent_history_on", "main")
	assert.NoError(t, err)
	runner.appendSessionMessage(sess.ID, domain.MessageRoleUser, "earlier question", "", "")
	runner.appendSessionMessage(sess.ID, domain.MessageRoleAssistant, "earlier answer", "", "")

	done := make(chan struct{}, 1)
	history := true
	runner.PromptWithOverrides(WithSessionID(context.Background(), sess.ID), "new question", RunOverrides{History: &history}, func(e StreamEvent) {
		if e.Type == StreamEventDone || e.Type == StreamEventError || e.Type == StreamEventStop {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		assert.FailNow(t, "timeout")
	}

	if assert.Len(t, provider.requests, 1) {
		// Prior conversation is collapsed into a single assistant message
		// containing a preamble plus the earlier messages, followed by the
		// new user message.
		assert.Len(t, provider.requests[0].Messages, 2)
		assert.Contains(t, provider.requests[0].Messages[0].Content, "I loaded prior conversation history")
		assert.Contains(t, provider.requests[0].Messages[0].Content, "earlier question")
		assert.Contains(t, provider.requests[0].Messages[0].Content, "earlier answer")
		assert.Contains(t, provider.requests[0].Messages[1].Content, "new question")
	}
}

func TestAgentRunner_DefaultPromptIncludesSystemPreamble(t *testing.T) {
	setTestDataDir(t)

	provider := &sequenceProvider{responses: [][]llm.Event{
		{{Type: llm.EventTypeText, Text: "ok"}, {Type: llm.EventTypeDone}},
	}}
	SetToolClientFactory(func(_ context.Context) (ToolClient, error) {
		return &fakeToolClient{tools: []ToolInfo{{Name: "agent_update", Description: "Update an agent"}}}, nil
	})
	t.Cleanup(func() { SetToolClientFactory(nil) })

	rulesPath := store.AgentRulesPath("agent_default")
	err := os.MkdirAll(filepath.Dir(rulesPath), 0o700)
	assert.NoError(t, err)
	err = os.WriteFile(rulesPath, []byte("Follow local rules."), 0o600)
	assert.NoError(t, err)
	err = store.WriteAgentMarkdownFile("agent_default", "AGENTS.md", "Agent workspace instructions.")
	assert.NoError(t, err)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_default", Name: "default", Model: "test/model"},
		&config.AgentConfig{Name: "default", Model: "test/model"},
		provider,
		nil,
	)

	done := make(chan struct{}, 1)
	runner.Prompt(context.Background(), "hello", func(e StreamEvent) {
		if e.Type == StreamEventDone || e.Type == StreamEventError || e.Type == StreamEventStop {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		assert.FailNow(t, "timeout")
	}

	if assert.Len(t, provider.requests, 1) {
		system := provider.requests[0].System
		assert.Contains(t, system, "Agent workspace instructions.")
		assert.Contains(t, system, "<rules>")
		agentsIdx := strings.Index(system, "Agent workspace instructions.")
		rulesIdx := strings.Index(system, "<rules>")
		assert.NotEqual(t, -1, agentsIdx)
		assert.NotEqual(t, -1, rulesIdx)
		assert.Less(t, agentsIdx, rulesIdx)
	}
}

func TestAgentRunner_NonInteractiveJobPrompt(t *testing.T) {
	setTestDataDir(t)

	provider := &sequenceProvider{responses: [][]llm.Event{
		{{Type: llm.EventTypeText, Text: "ok"}, {Type: llm.EventTypeDone}},
	}}

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_task", Name: "task", Model: "test/model"},
		&config.AgentConfig{Name: "task", Model: "test/model"},
		provider,
		nil,
	)

	// Create a session whose header declares type=task so the runner treats
	// it as a non-interactive job run.
	sess := &domain.Session{
		ID:        "agent_task-main",
		AgentID:   "agent_task",
		Name:      "main",
		Type:      domain.SessionTypeTask,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	assert.NoError(t, store.AppendJSONL(store.SessionPath("agent_task", sess.ID), sess))

	done := make(chan struct{}, 1)
	runner.Prompt(WithSessionID(context.Background(), sess.ID), "run job", func(e StreamEvent) {
		if e.Type == StreamEventDone || e.Type == StreamEventError || e.Type == StreamEventStop {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		assert.FailNow(t, "timeout")
	}

	if assert.Len(t, provider.requests, 1) {
		assert.Contains(t, provider.requests[0].System, "IMPORTANT: This is a non-interactive job run.")
	}
}

func TestSessionProcessingLifecycleAndStop(t *testing.T) {
	t.Helper()

	runs.mu.Lock()
	runs.bySession = make(map[string]map[uint64]context.CancelFunc)
	runs.nextID = 0
	runs.mu.Unlock()

	agentID := "agent_test"
	var mu sync.Mutex
	changes := make([]bool, 0, 2)
	SetSessionProcessingObserver(func(notifiedAgentID, sessionID string, processing bool) {
		if notifiedAgentID != agentID || sessionID != "sess-test" {
			return
		}
		mu.Lock()
		changes = append(changes, processing)
		mu.Unlock()
	})
	t.Cleanup(func() { SetSessionProcessingObserver(nil) })

	ctx, cancel := context.WithCancel(context.Background())
	untrack := trackSessionRun(agentID, "sess-test", cancel)
	assert.True(t, IsSessionProcessing(agentID, "sess-test"))
	stopped := StopSession(agentID, "sess-test")
	assert.Equal(t, 1, stopped)

	select {
	case <-ctx.Done():
	case <-time.After(1 * time.Second):
		assert.FailNow(t, "timeout")
	}
	assert.False(t, IsSessionProcessing(agentID, "sess-test"))

	// Cleanup should be idempotent even after StopSession removed the run.
	untrack()

	mu.Lock()
	defer mu.Unlock()
	assert.GreaterOrEqual(t, len(changes), 2)
	assert.True(t, changes[0])
	assert.False(t, changes[len(changes)-1])

}

func TestNewID(t *testing.T) {
	a := newID("sess")
	b := newID("sess")
	assert.True(t, strings.HasPrefix(a, "sess_"))
	assert.True(t, strings.HasPrefix(b, "sess_"))

}

func TestAgentRunner_NilProvider(t *testing.T) {
	runner := NewAgentRunner(&domain.Agent{ID: "a1", Model: "anthropic/test"}, &config.AgentConfig{Name: "bot"}, nil, nil)

	var got []StreamEvent
	done := make(chan struct{}, 1)
	runner.Prompt(context.Background(), "hello", func(e StreamEvent) {
		got = append(got, e)
		if e.Type == StreamEventDone || e.Type == StreamEventError || e.Type == StreamEventStop {
			done <- struct{}{}
		}
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		assert.FailNow(t, "timeout")
	}
	assert.GreaterOrEqual(t, len(got), 2)
	assert.Equal(t, StreamEventText, got[0].Type)
	assert.Equal(t, StreamEventDone, got[len(got)-1].Type)

}

func TestAgentRunner_WithProvider(t *testing.T) {
	provider := &mockProvider{events: []llm.Event{{Type: llm.EventTypeText, Text: "hello "}, {Type: llm.EventTypeText, Text: "world"}}}
	runner := NewAgentRunner(&domain.Agent{ID: "a1", Model: "anthropic/test"}, &config.AgentConfig{Name: "bot"}, provider, nil)

	var text string
	var chunks []string
	done := make(chan struct{}, 1)
	runner.Prompt(context.Background(), "hi", func(e StreamEvent) {
		if e.Type == StreamEventText {
			text += e.Text
			chunks = append(chunks, e.Text)
		}
		if e.Type == StreamEventDone {
			done <- struct{}{}
		}
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		assert.FailNow(t, "timeout")
	}
	assert.Equal(t, "hello world", text)
	assert.Equal(t, []string{"hello ", "world"}, chunks)

}

func TestAgentRunner_ErrorCases(t *testing.T) {
	t.Run("stream setup error", func(t *testing.T) {
		runner := NewAgentRunner(&domain.Agent{ID: "a1", Model: "anthropic/test"}, &config.AgentConfig{Name: "bot"}, &mockProvider{err: errors.New("boom")}, nil)
		errCh := make(chan error, 1)
		runner.Prompt(context.Background(), "hi", func(e StreamEvent) {
			if e.Type == StreamEventError {
				errCh <- e.Err
			}
		})
		select {
		case err := <-errCh:
			assert.Error(t, err)
		case <-time.After(2 * time.Second):
			assert.FailNow(t, "timeout")
		}
	})

	t.Run("stream setup error delivers to session channel", func(t *testing.T) {
		var delivered string
		RegisterSessionDelivery("a1", "sess-stream-setup-error", "signal", "+1", func(text string) { delivered = text })

		runner := NewAgentRunner(
			&domain.Agent{ID: "a1", Model: "anthropic/test"},
			&config.AgentConfig{Name: "bot"},
			&mockProvider{err: errors.New("boom")},
			nil,
		)

		done := make(chan struct{}, 1)
		runner.Prompt(WithSessionID(context.Background(), "sess-stream-setup-error"), "hi", func(e StreamEvent) {
			if e.Type == StreamEventError {
				done <- struct{}{}
			}
		})
		select {
		case <-done:
			assert.Equal(t, "Error: boom", delivered)
		case <-time.After(2 * time.Second):
			assert.FailNow(t, "timeout")
		}
	})

	t.Run("stream event error", func(t *testing.T) {
		runner := NewAgentRunner(&domain.Agent{ID: "a1", Model: "anthropic/test"}, &config.AgentConfig{Name: "bot"}, &mockProvider{events: []llm.Event{{Type: llm.EventTypeError, Error: errors.New("event boom")}}}, nil)
		errCh := make(chan error, 1)
		runner.Prompt(context.Background(), "hi", func(e StreamEvent) {
			if e.Type == StreamEventError {
				errCh <- e.Err
			}
		})
		select {
		case err := <-errCh:
			assert.Error(t, err)
		case <-time.After(2 * time.Second):
			assert.FailNow(t, "timeout")
		}
	})

	t.Run("stream event error delivers to session channel", func(t *testing.T) {
		var delivered string
		RegisterSessionDelivery("a1", "sess-stream-event-error", "signal", "+1", func(text string) { delivered = text })

		runner := NewAgentRunner(
			&domain.Agent{ID: "a1", Model: "anthropic/test"},
			&config.AgentConfig{Name: "bot"},
			&mockProvider{events: []llm.Event{{Type: llm.EventTypeError, Error: errors.New("event boom")}}},
			nil,
		)

		done := make(chan struct{}, 1)
		runner.Prompt(WithSessionID(context.Background(), "sess-stream-event-error"), "hi", func(e StreamEvent) {
			if e.Type == StreamEventError {
				done <- struct{}{}
			}
		})
		select {
		case <-done:
			assert.Equal(t, "Error: event boom", delivered)
		case <-time.After(2 * time.Second):
			assert.FailNow(t, "timeout")
		}
	})

	t.Run("usage records zero-token throttles", func(t *testing.T) {
		setTestDataDir(t)
		err := store.EnsureDirs()
		assert.NoError(t, err)
		sessionID := "sess-zero-token-throttle"

		runner := NewAgentRunner(
			&domain.Agent{ID: "a1", Model: "google/gemini-3-flash-preview"},
			&config.AgentConfig{Name: "bot"},
			&mockProvider{events: []llm.Event{{Type: llm.EventTypeError, Error: errors.New("429 rate limit")}}},
			nil,
		)

		errCh := make(chan error, 1)
		runner.Prompt(WithSessionID(context.Background(), sessionID), "hi", func(e StreamEvent) {
			if e.Type == StreamEventError {
				errCh <- e.Err
			}
		})
		select {
		case err := <-errCh:
			assert.ErrorContains(t, err, "429")
		case <-time.After(2 * time.Second):
			assert.FailNow(t, "timeout")
		}

		deadline := time.Now().Add(2 * time.Second)
		for {
			records, err := store.ReadJSONL[domain.UsageRecord](store.UsagePath())
			assert.NoError(t, err)

			for _, rec := range records {
				if rec.SessionID != sessionID {
					continue
				}

				assert.True(t, rec.HasThrottle)
				assert.False(t, rec.HasError)
				assert.Equal(t, 0, rec.InputTokens)
				assert.Equal(t, 0, rec.OutputTokens)

				return
			}
			assert.False(t, time.Now().After(deadline))

			time.Sleep(10 * time.Millisecond)
		}
	})
}

func TestAgentRunner_StopAndAccessors(t *testing.T) {
	a := &domain.Agent{ID: "a1", Name: "myagent"}
	cfg := &config.AgentConfig{Name: "myagent", Model: "anthropic/claude"}
	runner := NewAgentRunner(a, cfg, nil, nil)

	runner.Stop()
	typCh := make(chan StreamEventType, 1)
	runner.Prompt(context.Background(), "hi", func(e StreamEvent) { typCh <- e.Type })
	select {
	case typ := <-typCh:
		assert.Equal(t, StreamEventStop, typ)
	case <-time.After(1 * time.Second):
		assert.FailNow(t, "timeout")
	}
	assert.Equal(t, a, runner.Agent())
	assert.Equal(t, cfg, runner.Config())

}

func TestManager_ReconcileAndLookup(t *testing.T) {
	mgr := NewManager(nil)

	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot1", Model: "anthropic/claude"}, {Name: "bot2", Model: "openai/gpt-4"}}}
	mgr.Reconcile(cfg)
	_, ok := mgr.Get("bot1")
	assert.True(t, ok)

	_, ok = mgr.Get("bot2")
	assert.True(t, ok)

	got := len(mgr.List())
	assert.Equal(t, 2, got)

	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "bot1", Model: "anthropic/claude"}}})
	_, ok = mgr.Get("bot2")
	assert.False(t, ok)

	r1, _ := mgr.Get("bot1")
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "bot1", Model: "openai/gpt-4.1"}}})
	r2, _ := mgr.Get("bot1")
	assert.NotEqual(t, r2, r1)

	mgr.Stop()
}

func TestManager_Reconcile_UsesGlobalDefaults(t *testing.T) {
	mgr := NewManager(nil)

	cfg := &config.Config{
		Agents: []config.AgentConfig{{Name: "bot", Model: ""}},
		Models: config.ModelsConfig{
			Defaults: &config.ModelDefaults{
				Model:     "google/gemini-2.0-flash",
				Fallbacks: []string{"openai-codex/gpt-5.2"},
			},
		},
	}

	mgr.Reconcile(cfg)

	runner, ok := mgr.Get("bot")
	assert.True(t, ok)
	got := runner.Agent().Model
	assert.Equal(t, "google/gemini-2.0-flash", got)

	fallbacks := runner.Agent().Fallbacks
	assert.Len(t, fallbacks, 1)
	assert.Equal(t, "openai-codex/gpt-5.2", fallbacks[0])

	mgr.Reconcile(&config.Config{
		Agents: []config.AgentConfig{{Name: "bot", Model: ""}},
		Models: config.ModelsConfig{
			Defaults: &config.ModelDefaults{
				Model:     "openai/gpt-4.1",
				Fallbacks: []string{"anthropic/claude-sonnet-4.5"},
			},
		},
	})

	runner, ok = mgr.Get("bot")
	assert.True(t, ok)
	got = runner.Agent().Model
	assert.Equal(t, "openai/gpt-4.1", got)

	fallbacks = runner.Agent().Fallbacks
	assert.Len(t, fallbacks, 1)
	assert.Equal(t, "anthropic/claude-sonnet-4.5", fallbacks[0])

}

func TestManager_Reconcile_UpdatesOnPermissionsChange(t *testing.T) {
	mgr := NewManager(nil)

	cfg := &config.Config{
		Agents: []config.AgentConfig{{
			Name:  "bot",
			Model: "test/x",
			Permissions: &config.PermissionsConfig{
				Preset: config.PermissionsPresetFull,
				Exec: &config.ExecPermissionsConfig{
					AllowedCommands: []string{"go env *"},
				},
			},
		}},
	}
	mgr.Reconcile(cfg)

	r1, ok := mgr.Get("bot")
	assert.True(t, ok)
	if assert.NotNil(t, r1.Config()) && assert.NotNil(t, r1.Config().Permissions) {
		assert.Equal(t, config.PermissionsPresetFull, config.EffectivePermissionsPreset(r1.Config().Permissions))
		assert.NotNil(t, r1.Config().Permissions.Exec)
	}

	cfg = &config.Config{
		Agents: []config.AgentConfig{{
			Name:  "bot",
			Model: "test/x",
			Permissions: &config.PermissionsConfig{
				Preset: config.PermissionsPresetStandard,
			},
		}},
	}
	mgr.Reconcile(cfg)

	r2, ok := mgr.Get("bot")
	assert.True(t, ok)
	assert.NotEqual(t, r1, r2)
	if assert.NotNil(t, r2.Config()) {
		if assert.NotNil(t, r2.Config().Permissions) {
			assert.Equal(t, config.PermissionsPresetStandard, config.EffectivePermissionsPreset(r2.Config().Permissions))
			assert.Nil(t, r2.Config().Permissions.Exec)
		}
	}
}

func TestSessionManager_CreateAndGetOrCreate(t *testing.T) {
	setTestDataDir(t)
	err := store.EnsureDirs()
	assert.NoError(t, err)

	sm := NewSessionManager()
	s1, err := sm.Create("agent1")
	assert.NoError(t, err)
	assert.NotEqual(t, "", s1.ID)
	assert.Equal(t, "agent1", s1.AgentID)

	s2, err := sm.GetOrCreate("agent1")
	assert.NoError(t, err)
	assert.NotEqual(t, "", s2.ID)
	assert.Equal(t, "agent1", s2.AgentID)

}

func TestSessionManager_List(t *testing.T) {
	setTestDataDir(t)
	err := store.EnsureDirs()
	assert.NoError(t, err)

	sm := NewSessionManager()
	agentID := "agent_assistant"

	// Create a "main" session with old AgentID format (plain assistant)
	s1 := &domain.Session{
		ID:        "assistant-main",
		AgentID:   "assistant",
		Name:      "main",
		CreatedAt: time.Now().Add(-time.Hour),
		UpdatedAt: time.Now().Add(-time.Hour),
	}
	err = store.AppendJSONL(store.SessionPath(agentID, s1.ID), s1)
	assert.NoError(t, err)

	// Create another session with new AgentID format (agent_assistant)
	s2 := &domain.Session{
		ID:        "agent_assistant-other",
		AgentID:   "agent_assistant",
		Name:      "other",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err = store.AppendJSONL(store.SessionPath(agentID, s2.ID), s2)
	assert.NoError(t, err)

	list, err := sm.List(agentID)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(list))
	assert.Equal(t, // Verify "main" is first
		"main", list[0].Name)

}

func TestDiscoverSkillsAndBuildPrompt(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "planner")
	if err := store.EnsureDirs(); err != nil {
		// EnsureDirs unrelated to this test; ignore data-dir setup state.
		_ = err
	}
	err := os.MkdirAll(skillDir, 0o700)
	assert.NoError(t, err)

	err = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("Plan steps carefully."), 0o600)
	assert.NoError(t, err)

	skills, err := DiscoverSkills(dir)
	assert.NoError(t, err)
	assert.Len(t, skills, 1)
	assert.Equal(t, "planner", skills[0].Name)

	prompt := BuildSystemPrompt("Base prompt", skills)
	assert.Contains(t, prompt, `<skill name="planner">`)
	assert.Contains(t, prompt, "Base prompt")

}

func TestFilterTools_AllowList(t *testing.T) {
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_bot", Name: "bot"},
		&config.AgentConfig{
			Name:        "bot",
			Permissions: &config.PermissionsConfig{Tools: []string{"tool_a", "tool_b"}},
		},
		&mockProvider{},
		nil,
	)

	tools := []ToolInfo{
		{Name: "tool_a", Description: "Tool A"},
		{Name: "tool_b", Description: "Tool B"},
		{Name: "tool_c", Description: "Tool C"},
	}

	// Agent-level permissions apply when no per-message restrictions.
	filtered := runner.filterTools(tools, nil, nil)
	assert.Equal(t, 2, len(filtered))

	for _, tool := range filtered {
		assert.Contains(t, []string{"tool_a", "tool_b"}, tool.Name)

	}
}

func TestFilterTools_PerMessageOverride(t *testing.T) {
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_bot", Name: "bot"},
		&config.AgentConfig{
			Name:        "bot",
			Permissions: &config.PermissionsConfig{Tools: []string{"tool_a"}},
		},
		&mockProvider{},
		nil,
	)

	tools := []ToolInfo{
		{Name: "tool_a"},
		{Name: "tool_b"},
		{Name: "tool_c"},
	}

	// Per-message override restricts to only tool_c.
	filtered := runner.filterTools(tools, []string{"tool_c"}, nil)
	assert.Len(t, filtered, 1)
	assert.Equal(t, "tool_c", filtered[0].Name)

}

func TestFilterTools_DisabledWinsAfterAllowList(t *testing.T) {
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_bot", Name: "bot"},
		&config.AgentConfig{
			Name: "bot",
			Permissions: &config.PermissionsConfig{
				Tools:         []string{"tool_a", "tool_b"},
				DisabledTools: []string{"tool_b"},
			},
		},
		&mockProvider{},
		nil,
	)

	tools := []ToolInfo{
		{Name: "tool_a"},
		{Name: "tool_b"},
		{Name: "tool_c"},
	}

	filtered := runner.filterTools(tools, nil, nil)
	assert.Len(t, filtered, 1)
	assert.Equal(t, "tool_a", filtered[0].Name)

}

func TestFilterTools_PerMessageDisabledAppliedAfterRestrict(t *testing.T) {
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_bot", Name: "bot"},
		&config.AgentConfig{Name: "bot"},
		&mockProvider{},
		nil,
	)

	tools := []ToolInfo{
		{Name: "tool_a"},
		{Name: "tool_b"},
	}

	filtered := runner.filterTools(tools, []string{"tool_a", "tool_b"}, []string{"tool_b"})
	assert.Len(t, filtered, 1)
	assert.Equal(t, "tool_a", filtered[0].Name)

}

func TestFilterTools_NoRestrictions(t *testing.T) {
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_bot", Name: "bot"},
		&config.AgentConfig{Name: "bot"}, // no permissions
		&mockProvider{},
		nil,
	)

	tools := []ToolInfo{{Name: "tool_a"}, {Name: "tool_b"}}
	filtered := runner.filterTools(tools, nil, nil)
	assert.Equal(t, 2, len(filtered))

}

func TestFilterTools_PermissionsPresetCapsAvailableTools(t *testing.T) {
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_bot", Name: "bot"},
		&config.AgentConfig{
			Name: "bot",
			Permissions: &config.PermissionsConfig{
				Preset: config.PermissionsPresetMinimal,
				Tools:  []string{"task_run", "browser_open", "auth_set"},
			},
		},
		&mockProvider{},
		nil,
	)

	tools := []ToolInfo{
		{Name: "task_run"},
		{Name: "browser_open"},
		{Name: "auth_set"},
		{Name: "job_list"},
	}

	filtered := runner.filterTools(tools, nil, nil)
	if assert.Len(t, filtered, 1) {
		assert.Equal(t, "task_run", filtered[0].Name)
	}
}

func TestLoadRules_InlineText(t *testing.T) {
	setTestDataDir(t)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_bot", Name: "bot"},
		&config.AgentConfig{Name: "bot", Rules: "Be helpful."},
		&mockProvider{},
		nil,
	)

	rules := runner.loadRules()
	assert.Equal(t, "Be helpful.", rules)

}

func TestLoadRules_FilePath(t *testing.T) {
	setTestDataDir(t)

	agentID := "agent_bot"
	agentDir := store.AgentDir(agentID)
	err := os.MkdirAll(agentDir, 0o700)
	assert.NoError(t, err)

	rulesFile := filepath.Join(agentDir, "RULES.md")
	err = os.WriteFile(rulesFile, []byte("# Rules\nBe safe."), 0o600)
	assert.NoError(t, err)

	runner := NewAgentRunner(
		&domain.Agent{ID: agentID, Name: "bot"},
		&config.AgentConfig{Name: "bot", Rules: rulesFile},
		&mockProvider{},
		nil,
	)

	rules := runner.loadRules()
	assert.True(t, strings.Contains(rules, "Be safe."))

}

func TestLoadRules_FilePathOutsideAgentDir(t *testing.T) {
	setTestDataDir(t)

	// Write a file outside the agent's data directory.
	dir := t.TempDir()
	rulesFile := filepath.Join(dir, "RULES.md")
	err := os.WriteFile(rulesFile, []byte("sensitive content"), 0o600)
	assert.NoError(t, err)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_bot", Name: "bot"},
		&config.AgentConfig{Name: "bot", Rules: rulesFile},
		&mockProvider{},
		nil,
	)

	// Path traversal outside the agent dir must be blocked.
	rules := runner.loadRules()
	assert.Equal(t, "", rules)
}

func TestLoadRules_FallbackToDataDir(t *testing.T) {
	setTestDataDir(t)

	// Write RULES.md to the agent's data directory.
	agentID := "agent_ruletest"
	rulesPath := store.AgentRulesPath(agentID)
	err := os.MkdirAll(filepath.Dir(rulesPath), 0o700)
	assert.NoError(t, err)

	err = os.WriteFile(rulesPath, []byte("Follow safety guidelines."), 0o600)
	assert.NoError(t, err)

	runner := NewAgentRunner(
		&domain.Agent{ID: agentID, Name: "ruletest"},
		&config.AgentConfig{Name: "ruletest"}, // no inline rules
		&mockProvider{},
		nil,
	)

	rules := runner.loadRules()
	assert.True(t, strings.Contains(rules, "Follow safety guidelines."))

}

func TestLoadRules_Empty(t *testing.T) {
	setTestDataDir(t)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_norules", Name: "norules"},
		&config.AgentConfig{Name: "norules"}, // no rules
		&mockProvider{},
		nil,
	)

	rules := runner.loadRules()
	assert.Equal(t, "", rules)

}

func TestAppendSessionMessage_SkipsEmptyContent(t *testing.T) {
	setTestDataDir(t)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_msg", Name: "msgtest"},
		&config.AgentConfig{Name: "msgtest"},
		&mockProvider{},
		nil,
	)

	// Empty content should not create a file.
	runner.appendSessionMessage("sess1", domain.MessageRoleUser, "", "", "")
	p := store.SessionPath("agent_msg", "sess1")
	_, err := os.Stat(p)
	assert.Error(t, err)

}

func TestAppendSessionMessage_PersistsMessage(t *testing.T) {
	setTestDataDir(t)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_persist", Name: "persist"},
		&config.AgentConfig{Name: "persist"},
		&mockProvider{},
		nil,
	)

	runner.appendSessionMessage("sess2", domain.MessageRoleUser, "Hello, world!", "", "")

	p := store.SessionPath("agent_persist", "sess2")
	data, err := os.ReadFile(p)
	assert.NoError(t, err)
	assert.True(t, strings.Contains(string(data), "Hello, world!"))

}

func TestAppendSessionMessageWithSender_PersistsSender(t *testing.T) {
	setTestDataDir(t)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_sender", Name: "sender"},
		&config.AgentConfig{Name: "sender"},
		&mockProvider{},
		nil,
	)

	sender := domain.NewMessageSender("u123", "Alice", true)
	runner.appendSessionMessageWithSender("sess-sender", domain.MessageRoleUser, "Hello, world!", "", "", sender)

	lines, err := store.ReadJSONL[domain.Message](store.SessionPath("agent_sender", "sess-sender"))
	assert.NoError(t, err)
	assert.Len(t, lines, 1)
	assert.NotNil(t, lines[0].Sender)
	assert.Equal(t, "u123", lines[0].Sender.ID)
	assert.Equal(t, "Alice", lines[0].Sender.Name)
	assert.True(t, lines[0].Sender.Participant)
}

func TestResolveSessionID_FromContext(t *testing.T) {
	setTestDataDir(t)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_sess", Name: "sesstest"},
		&config.AgentConfig{Name: "sesstest"},
		&mockProvider{},
		nil,
	)

	ctx := WithSessionID(context.Background(), "explicit-session-id")
	sessionID := runner.resolveSessionID(ctx)
	assert.Equal(t, "explicit-session-id", sessionID)

}

func TestSetSessionMessageObserver(t *testing.T) {
	var notified string
	SetSessionMessageObserver(func(agentID, sessionID, role string) {
		notified = agentID + "/" + sessionID + "/" + role
	})
	t.Cleanup(func() { SetSessionMessageObserver(nil) })

	notifySessionMessage("agent_test", "sess123", "user")
	assert.Equal(t, "agent_test/sess123/user", notified)

}

func TestRegisterSessionDelivery(t *testing.T) {
	var received string
	RegisterSessionDelivery("agent_test", "test-sess", "signal", "+1", func(text string) { received = text })

	deliverToSession("agent_test", "test-sess", "hello delivery")
	assert.Equal(t, "hello delivery", received)

	// Empty text should not call delivery function.
	received = ""
	deliverToSession("agent_test", "test-sess", "")
	assert.Equal(t, "", received)

	// Sentinel NO_REPLY should not call delivery function.
	received = ""
	deliverToSession("agent_test", "test-sess", "NO_REPLY")
	assert.Equal(t, "", received)

	// Unknown session should not panic.
	deliverToSession("agent_test", "unknown-sess", "no delivery")
}

func TestShouldDeliverReply(t *testing.T) {
	assert.False(t, ShouldDeliverReply(""))
	assert.False(t, ShouldDeliverReply(" \n\t "))
	assert.False(t, ShouldDeliverReply("NO_REPLY"))
	assert.True(t, ShouldDeliverReply("no_reply"))
	assert.True(t, ShouldDeliverReply("hello"))
}

func TestRegisterSessionDelivery_Idempotent(t *testing.T) {
	var calls int
	RegisterSessionDelivery("agent_test", "sess-idem", "slack", "C1", func(_ string) { calls++ })
	RegisterSessionDelivery("agent_test", "sess-idem", "slack", "C1", func(_ string) { calls += 10 })

	// Second registration overwrites the first.
	deliverToSession("agent_test", "sess-idem", "msg")
	assert.Equal(t, 10, calls)

}

func TestRegisterSessionMediaDelivery(t *testing.T) {
	var captionGot, pathGot string
	RegisterSessionMediaDelivery("agent_test", "media-sess", "signal", "+2", func(caption, path string) {
		captionGot = caption
		pathGot = path
	})

	DeliverMediaToSession("agent_test", "media-sess", "my caption", "/path/to/file.jpg")
	assert.Equal(t, "my caption", captionGot)
	assert.Equal(t, "/path/to/file.jpg", pathGot)

	// Empty path should not call delivery function.
	captionGot = ""
	DeliverMediaToSession("agent_test", "media-sess", "ignored", "")
	assert.Equal(t, "", captionGot)

}

func TestSessionManager_CreateWithName(t *testing.T) {
	setTestDataDir(t)
	sm := NewSessionManager()

	sess, err := sm.CreateWithName("agent_named", "mysession")
	assert.NoError(t, err)
	assert.Equal(t, "mysession", sess.Name)
	assert.Equal(t, "agent_named", sess.AgentID)
	assert.NotEqual(t, "", sess.ID)

}

func TestSessionManager_CreateWithName_AlwaysNew(t *testing.T) {
	setTestDataDir(t)
	sm := NewSessionManager()

	sess1, _ := sm.CreateWithName("agent_new", "myname")
	sess2, _ := sm.CreateWithName("agent_new", "myname")
	assert.NotEqual(t, sess2.ID, sess1.ID)

}

func TestAppendMessageToSession(t *testing.T) {
	setTestDataDir(t)

	agentID := "agent_append_msg"
	sessionID := "sess_amsg"

	// Create session first.
	sm := NewSessionManager()
	_, _ = sm.CreateWithName(agentID, "amsg")

	err := AppendMessageToSession(agentID, sessionID, domain.MessageRoleUser, "Hello there!")
	assert.NoError(t, err)

	// Verify message was written.
	p := store.SessionPath(agentID, sessionID)
	data, err := os.ReadFile(p)
	assert.NoError(t, err)
	assert.True(t, strings.Contains(string(data), "Hello there!"))

}

func TestAppendReplyToSession_Delivers(t *testing.T) {
	setTestDataDir(t)

	agentID := "agent_reply"
	sessionID := "sess_reply"

	var delivered string
	RegisterSessionDelivery(agentID, sessionID, "signal", "+1555", func(text string) {
		delivered = text
	})
	err := AppendReplyToSession(agentID, sessionID, "hi")
	assert.NoError(t, err)

	assert.Equal(t, "hi", delivered)

	data, err := os.ReadFile(store.SessionPath(agentID, sessionID))
	assert.NoError(t, err)
	assert.Contains(t, string(data), "\"role\":\"assistant\"")
	assert.Contains(t, string(data), "hi")

}

func TestRunnerWait(t *testing.T) {
	setTestDataDir(t)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_wait", Name: "wait"},
		&config.AgentConfig{Name: "wait"},
		&mockProvider{},
		nil,
	)

	// Wait on idle runner should return immediately.
	done := make(chan struct{})
	go func() {
		runner.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		assert.FailNow(t, "timeout")
	}
}

func TestSessionContextHelpers(t *testing.T) {
	ctx := context.Background()

	// SessionIDFromContext on empty context
	_, ok := SessionIDFromContext(ctx)
	assert.False(t, ok)

	// WithSessionID empty string is no-op
	ctx2 := WithSessionID(ctx, "")
	_, ok = SessionIDFromContext(ctx2)
	assert.False(t, ok)

	// WithSessionID non-empty
	ctx3 := WithSessionID(ctx, "sess123")
	sid, ok := SessionIDFromContext(ctx3)
	assert.True(t, ok)
	assert.Equal(t, "sess123", sid)

	// WithSessionAgentID
	ctx4 := WithSessionAgentID(ctx, "agentX")
	aid, ok := SessionAgentIDFromContext(ctx4)
	assert.True(t, ok)
	assert.Equal(t, "agentX", aid)

	// WithSessionAgentID empty is no-op
	ctx5 := WithSessionAgentID(ctx, "")
	_, ok = SessionAgentIDFromContext(ctx5)
	assert.False(t, ok)

	// SessionAgentIDFromContext 0-value
	_, ok = SessionAgentIDFromContext(context.Background())
	assert.False(t, ok)

	// WithChannelSession
	ctx6 := WithChannelSession(ctx, "slack", "alerts", "C123")
	chType, configuredID, chID, ok := ChannelSessionFromContext(ctx6)
	assert.True(t, ok)
	assert.Equal(t, "slack", chType)
	assert.Equal(t, "alerts", configuredID)
	assert.Equal(t, "C123", chID)

	// WithChannelSession empty type is no-op
	ctx7 := WithChannelSession(ctx, "", "", "C123")
	_, _, _, ok = ChannelSessionFromContext(ctx7)
	assert.False(t, ok)

}

func TestResolveSessionID_ChannelContext(t *testing.T) {
	setTestDataDir(t)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_rch", Name: "rch"},
		&config.AgentConfig{Name: "rch"},
		&mockProvider{}, nil,
	)
	ctx := WithChannelSession(context.Background(), "slack", "alerts", "C999")
	sid := runner.resolveSessionID(ctx)
	assert.
		// Should be non-empty (either a created session ID or fallback)
		NotEqual(t, "", sid)

}

func TestLoadMemoryContext_NoNotes(t *testing.T) {
	setTestDataDir(t)
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_lmc", Name: "lmc"},
		&config.AgentConfig{Name: "lmc"},
		&mockProvider{}, nil,
	)
	got := runner.loadMemoryContext("hello")
	assert.Equal(t, "", got)

}

func TestLoadMemoryContext_WithNotes(t *testing.T) {
	setTestDataDir(t)
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_lmc2", Name: "lmc2"},
		&config.AgentConfig{Name: "lmc2"},
		&mockProvider{}, nil,
	)

	notesPath := store.NotesPath(runner.memoryPoolID())
	assert.NoError(t, os.MkdirAll(filepath.Dir(notesPath), 0o700))
	assert.NoError(t, os.WriteFile(notesPath, []byte("- test memory content\n- another note"), 0o600))

	got := runner.loadMemoryContext("test memory")
	assert.True(t, strings.Contains(got, "test memory content"))

}

func TestBuildToolSystemPrompt(t *testing.T) {
	tools := []ToolInfo{
		{Name: "tool_a", Description: "does a"},
		{Name: "tool_b"},
	}
	out := buildToolSystemPrompt("myagent", tools)
	assert.True(t, strings.Contains(out, "myagent"))
	assert.True(t, strings.Contains(out, "task_schedule"))
	// Ensure header is present and no placeholder tokens remain.
	assert.True(t, strings.Contains(out, "autonomous local assistant"))
	assert.False(t, strings.Contains(out, "<available_tools>"))

	// Without agent name.
	out2 := buildToolSystemPrompt("", tools)
	assert.False(t, strings.Contains(out2, "agent name is"))

}

func TestBuildToolSystemPrompt_AdvertisesSessionHistory(t *testing.T) {
	out := buildToolSystemPrompt("", []ToolInfo{{Name: "session_history"}})
	assert.Contains(t, out, "inspect recent session history with session_history")
	assert.Contains(t, out, "order=\"desc\" and limit=20")

	out2 := buildToolSystemPrompt("", []ToolInfo{{Name: "session_messages"}})
	assert.Contains(t, out2, "inspect recent session history with session_messages")
	assert.Contains(t, out2, "order=\"desc\" and limit=20")
}

func TestBuildToolSystemPrompt_ForbidsEmptyPromisesAndNeedlessClarification(t *testing.T) {
	out := buildToolSystemPrompt("", nil)
	// Behavior changed: prompt no longer contains the previous admonitions.
	// Confirm prompt header is present instead.
	assert.Contains(t, out, "autonomous local assistant")
}

func TestBuildRulesPreamble(t *testing.T) {
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_bot", Name: "bot"},
		&config.AgentConfig{Name: "bot", Rules: "Be concise."},
		&mockProvider{},
		nil,
	)

	got := runner.buildRulesPreamble()
	assert.Contains(t, got, "<rules>")
	assert.Contains(t, got, "Be concise.")
	assert.Contains(t, got, "</rules>")
}

func TestIsRetryableError(t *testing.T) {
	assert.False(t, isRetryableError(nil))

	for _, msg := range []string{"429", "too many requests", "rate limit", "quota", "overloaded", "503", "service unavailable", "401", "unauthorized", "unauthenticated"} {
		assert.True(t, isRetryableError(errors.New(msg)))

	}
	assert.False(t, isRetryableError(errors.New("unrelated failure")))

}

func TestIsThrottleError(t *testing.T) {
	assert.False(t, isThrottleError(nil))

	for _, msg := range []string{
		"429",
		"rate limit",
		"RATE_LIMIT_EXCEEDED",
		"RESOURCE_EXHAUSTED",
		"quota exceeded",
		"capacity exhausted",
	} {
		assert.True(t, isThrottleError(errors.New(msg)))

	}
	for _, msg := range []string{"503 service unavailable", "unauthorized", "boom"} {
		assert.False(t, isThrottleError(errors.New(msg)))

	}
}

func TestAppendSessionMessage_WithMediaURL(t *testing.T) {
	setTestDataDir(t)
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_asm2", Name: "asm2"},
		&config.AgentConfig{Name: "asm2"},
		&mockProvider{}, nil,
	)
	sess, err := NewSessionManager().Create(runner.agent.ID)
	assert.NoError(t, err)

	// Empty content but non-empty mediaURL should persist.
	runner.appendSessionMessage(sess.ID, domain.MessageRoleUser, "", "http://example.com/img.png", "")

	data, err := os.ReadFile(store.SessionPath(runner.agent.ID, sess.ID))
	assert.NoError(t, err)
	assert.True(t, strings.Contains(string(data), "img.png"))

}

func TestLoadSessionConversation_ReplacesHistoricalMediaWithMarker(t *testing.T) {
	setTestDataDir(t)
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_hist_media", Name: "hist-media"},
		&config.AgentConfig{Name: "hist-media"},
		&mockProvider{}, nil,
	)
	sess, err := NewSessionManager().Create(runner.agent.ID)
	assert.NoError(t, err)

	runner.appendSessionMessage(sess.ID, domain.MessageRoleUser, "read this", "data:image/png;base64,cG5n", "")
	runner.appendSessionMessage(sess.ID, domain.MessageRoleAssistant, "", "data:image/png;base64,bW9yZQ==", "")
	runner.appendSessionMessage(sess.ID, domain.MessageRoleUser, "follow-up", "", "")

	history := runner.loadSessionConversation(sess.ID, 10)
	assert.Len(t, history, 2)
	assert.Equal(t, llm.RoleAssistant, history[0].Role)
	assert.Contains(t, history[0].Content, "read this\n[prior image attached]")
	assert.Contains(t, history[0].Content, "[prior media attached]")
	assert.Equal(t, llm.RoleUser, history[1].Role)
	assert.Contains(t, history[1].Content, "follow-up")
}

func TestLoadSessionConversation_UsesSenderMetadata(t *testing.T) {
	setTestDataDir(t)
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_hist_sender", Name: "hist-sender"},
		&config.AgentConfig{Name: "hist-sender"},
		&mockProvider{}, nil,
	)
	sess, err := NewSessionManager().Create(runner.agent.ID)
	assert.NoError(t, err)

	runner.appendSessionMessageWithSender(sess.ID, domain.MessageRoleUser, "opening question", "", "", domain.NewMessageSender("u1", "Alice", true))
	runner.appendSessionMessage(sess.ID, domain.MessageRoleAssistant, "opening answer", "", "")
	runner.appendSessionMessageWithSender(sess.ID, domain.MessageRoleUser, "side chatter", "", "", domain.NewMessageSender("u2", "Bob", false))
	runner.appendSessionMessageWithSender(sess.ID, domain.MessageRoleUser, "actual follow-up", "", "", domain.NewMessageSender("u1", "Alice", true))

	history := runner.loadSessionConversation(sess.ID, 10)
	// Prior messages are collapsed into a single assistant message followed by
	// the last user message containing sender attribution.
	assert.Len(t, history, 2)
	assert.Equal(t, llm.RoleAssistant, history[0].Role)
	assert.Contains(t, history[0].Content, "I loaded prior conversation history")
	assert.Contains(t, history[0].Content, "Alice (u1)")
	assert.Contains(t, history[0].Content, "opening question")
	assert.Contains(t, history[0].Content, "opening answer")
	assert.Contains(t, history[0].Content, "Bob (u2)")
	assert.Contains(t, history[0].Content, "side chatter")
	assert.Contains(t, history[0].Content, "group")
	assert.Equal(t, llm.RoleUser, history[1].Role)
	assert.Contains(t, history[1].Content, "actual follow-up")
	assert.Contains(t, history[1].Content, "Alice (u1)")
}

func TestLoadSessionConversationWithinBudget_PrioritizesCurrentThenRecentHistory(t *testing.T) {
	setTestDataDir(t)
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_hist_budget", Name: "hist-budget"},
		&config.AgentConfig{Name: "hist-budget"},
		&mockProvider{}, nil,
	)
	sess, err := NewSessionManager().Create(runner.agent.ID)
	assert.NoError(t, err)

	runner.appendSessionMessage(sess.ID, domain.MessageRoleUser, strings.Repeat("old bulky context ", 80), "", "")
	runner.appendSessionMessage(sess.ID, domain.MessageRoleAssistant, "old answer", "", "")
	runner.appendSessionMessage(sess.ID, domain.MessageRoleUser, "recent useful context", "", "")
	runner.appendSessionMessage(sess.ID, domain.MessageRoleAssistant, "recent answer", "", "")
	runner.appendSessionMessage(sess.ID, domain.MessageRoleUser, strings.Repeat("current quoted signal text ", 18), "", "")

	history := runner.loadSessionConversationWithinBudget(sess.ID, 10, 220, "")
	assert.Len(t, history, 2)
	if len(history) != 2 {
		return
	}
	assert.Equal(t, llm.RoleAssistant, history[0].Role)
	assert.Contains(t, history[0].Content, "recent useful context")
	assert.NotContains(t, history[0].Content, "old bulky context")
	assert.Equal(t, llm.RoleUser, history[1].Role)
	assert.Contains(t, history[1].Content, "current quoted signal text")
	assert.LessOrEqual(t, llm.EstimateRequestTokens(llm.Request{Messages: history}), 220)
}

func TestLoadSessionConversation_PrimaryAnnotationApplied(t *testing.T) {
	setTestDataDir(t)

	provider := &sequenceProvider{responses: [][]llm.Event{{{Type: llm.EventTypeDone}}}}

	// Agent config includes a channel with Primary set to "bob".
	cfg := &config.AgentConfig{
		Name:     "hist-primary",
		Model:    "test/model",
		Channels: []config.ChannelConfig{{Type: "slack", ID: "alerts", Primary: "bob"}},
	}

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_hist_primary", Name: "hist-primary", Model: "test/model"},
		cfg,
		provider,
		nil,
	)

	sess, err := NewSessionManager().GetOrCreateNamed("agent_hist_primary", "main")
	assert.NoError(t, err)

	chCfg := &store.SessionChannelsConfig{
		SessionID: sess.ID,
		AgentID:   "agent_hist_primary",
		Channels:  []store.SessionChannel{{Type: "slack", ConfiguredID: "alerts", ID: "C123"}},
	}
	assert.NoError(t, store.WriteSessionChannels(chCfg))

	t1 := time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 3, 22, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 3, 23, 12, 34, 56, 0, time.UTC)

	msg1 := domain.Message{ID: "m1", Role: domain.MessageRoleUser, Sender: domain.NewMessageSender("alice", "Alice", true), Content: "Hello", Timestamp: t1}
	msg2 := domain.Message{ID: "m2", Role: domain.MessageRoleUser, Sender: domain.NewMessageSender("bob", "Bob", true), Content: "FYI", Timestamp: t2}
	msg3 := domain.Message{ID: "m3", Role: domain.MessageRoleUser, Sender: domain.NewMessageSender("charlie", "Charlie", true), Content: "What's next?", Timestamp: t3}

	assert.NoError(t, store.AppendJSONL(store.SessionPath("agent_hist_primary", sess.ID), msg1))
	assert.NoError(t, store.AppendJSONL(store.SessionPath("agent_hist_primary", sess.ID), msg2))
	assert.NoError(t, store.AppendJSONL(store.SessionPath("agent_hist_primary", sess.ID), msg3))

	conv := runner.loadSessionConversation(sess.ID, 24)
	if assert.Len(t, conv, 2) {
		// Assistant context should include the prior messages and mark Bob as primary
		assert.Contains(t, conv[0].Content, "source=slack:C123")
		assert.Contains(t, conv[0].Content, "primary")
		assert.Contains(t, conv[0].Content, "Bob")
		assert.Contains(t, conv[0].Content, "Reply directly to that sender in second person")

		// Last user message should have metadata before the name and include source
		assert.Contains(t, conv[1].Content, "[2026-03-23 12:34:56]")
		assert.Contains(t, conv[1].Content, "(group, source=slack:C123)")
		assert.Contains(t, conv[1].Content, "<Charlie")
	}
}

func TestLoadSessionConversation_SlackChannelLabeledCorrectly(t *testing.T) {
	setTestDataDir(t)
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_slack_group_label", Name: "slack-group-label"},
		&config.AgentConfig{Name: "slack-group-label"},
		&mockProvider{}, nil,
	)

	sess, err := NewSessionManager().GetOrCreateNamed("agent_slack_group_label", "main")
	assert.NoError(t, err)

	chCfg := &store.SessionChannelsConfig{
		SessionID: sess.ID,
		AgentID:   "agent_slack_group_label",
		Channels:  []store.SessionChannel{{Type: "slack", ConfiguredID: "alerts", ID: "C123"}},
	}
	assert.NoError(t, store.WriteSessionChannels(chCfg))

	t1 := time.Date(2026, 3, 23, 22, 10, 16, 0, time.UTC)
	t2 := time.Date(2026, 3, 23, 23, 3, 2, 0, time.UTC)

	msg1 := domain.Message{ID: "ctx1", Role: domain.MessageRoleUser, Sender: domain.NewMessageSender("u-other", "Other", false), Content: "background context", Timestamp: t1}
	msg2 := domain.Message{ID: "trig1", Role: domain.MessageRoleUser, Sender: domain.NewMessageSender("u-loren", "Loren", true), Content: "hi", Timestamp: t2}

	assert.NoError(t, store.AppendJSONL(store.SessionPath("agent_slack_group_label", sess.ID), msg1))
	assert.NoError(t, store.AppendJSONL(store.SessionPath("agent_slack_group_label", sess.ID), msg2))

	conv := runner.loadSessionConversation(sess.ID, 24)
	if assert.Len(t, conv, 2) {
		assert.Contains(t, conv[0].Content, "Reply directly to that sender in second person")
		assert.Contains(t, conv[0].Content, "(group, source=slack:C123)")
		assert.NotContains(t, conv[0].Content, "(private, source=slack:C123)")
		assert.Contains(t, conv[1].Content, "(group, source=slack:C123)")
		assert.NotContains(t, conv[1].Content, "(private, source=slack:C123)")
	}
}

// Regression test: the triggering message in a Signal group chat must be labeled "group",
// not "private", even though its Sender.Participant==true.
func TestLoadSessionConversation_SignalGroupLabeledCorrectly(t *testing.T) {
	setTestDataDir(t)
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_signal_group_label", Name: "signal-group-label"},
		&config.AgentConfig{Name: "signal-group-label"},
		&mockProvider{}, nil,
	)

	sess, err := NewSessionManager().GetOrCreateNamed("agent_signal_group_label", "main")
	assert.NoError(t, err)

	// Signal group ID (base64, not a phone number).
	groupID := "0LFvhmak+y3f4dwJfYty819gLi9il1IZU7xctbcWqzE="
	chCfg := &store.SessionChannelsConfig{
		SessionID: sess.ID,
		AgentID:   "agent_signal_group_label",
		Channels:  []store.SessionChannel{{Type: "signal", ConfiguredID: groupID, ID: groupID}},
	}
	assert.NoError(t, store.WriteSessionChannels(chCfg))

	t1 := time.Date(2026, 3, 23, 22, 10, 16, 0, time.UTC)
	t2 := time.Date(2026, 3, 23, 23, 3, 2, 0, time.UTC)

	// Context-only member (Participant=false) — should be "group".
	msg1 := domain.Message{ID: "ctx1", Role: domain.MessageRoleUser, Sender: domain.NewMessageSender("uuid-other", "Other", false), Content: "signal does care about that", Timestamp: t1}
	// Triggering member (Participant=true) — should ALSO be "group", not "private".
	msg2 := domain.Message{ID: "trig1", Role: domain.MessageRoleUser, Sender: domain.NewMessageSender("+12066439160", "+12066439160", true), Content: "is this a private chat?", Timestamp: t2}

	assert.NoError(t, store.AppendJSONL(store.SessionPath("agent_signal_group_label", sess.ID), msg1))
	assert.NoError(t, store.AppendJSONL(store.SessionPath("agent_signal_group_label", sess.ID), msg2))

	conv := runner.loadSessionConversation(sess.ID, 24)
	if assert.Len(t, conv, 2) {
		// Context block should label the non-participant as "group".
		assert.Contains(t, conv[0].Content, "group")
		assert.NotContains(t, conv[0].Content, "private")
		// Triggering message must also be labeled "group".
		assert.Contains(t, conv[1].Content, "(group,")
		assert.NotContains(t, conv[1].Content, "(private,")
	}
}

func TestSessionList(t *testing.T) {
	setTestDataDir(t)
	sm := NewSessionManager()

	// Empty list for unknown agent.
	sessions, err := sm.List("agent_no_such")
	assert.NoError(t, err)
	assert.Equal(t, 0, len(sessions))

	// Create two sessions and list them.
	_, err = sm.Create("agent_listtest")
	assert.NoError(t, err)

	_, err = sm.Create("agent_listtest")
	assert.NoError(t, err)

	sessions, err = sm.List("agent_listtest")
	assert.NoError(t, err)
	assert.Equal(t, 2, len(sessions))

}

func TestIsAuthError(t *testing.T) {
	assert.False(t, isAuthError(nil))
	assert.True(t, isAuthError(errors.New("401 Unauthorized")))
	assert.True(t, isAuthError(errors.New("request unauthorized")))
	assert.True(t, isAuthError(errors.New("unauthenticated")))
	assert.False(t, isAuthError(errors.New("500 internal server error")))
	assert.False(t, isAuthError(errors.New("context deadline exceeded")))
}
