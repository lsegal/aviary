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
)

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
		if typ == "" {
			t.Fatal("stream event type should not be empty")
		}
	}
}

func TestParseToolCall_Variants(t *testing.T) {
	cases := []struct {
		name string
		in   string
		tool string
	}{
		{
			name: "mcp style name+arguments",
			in:   `{"name":"agent_update","arguments":{"name":"assistant","model":"openai/gpt-5.2"}}`,
			tool: "agent_update",
		},
		{
			name: "nested tool_call",
			in:   `{"tool_call":{"name":"agent_update","args":{"name":"assistant"}}}`,
			tool: "agent_update",
		},
		{
			name: "json fence with input",
			in:   "```json\n{\"tool\":\"agent_update\",\"input\":{\"name\":\"assistant\"}}\n```",
			tool: "agent_update",
		},
		{
			name: "array first element",
			in:   `[{"tool":"agent_update","arguments":{"name":"assistant"}}]`,
			tool: "agent_update",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, ok := parseToolCall(tc.in)
			if !ok {
				t.Fatalf("parseToolCall(%q) failed", tc.in)
			}
			if parsed.Tool != tc.tool {
				t.Fatalf("expected tool %q, got %q", tc.tool, parsed.Tool)
			}
		})
	}
}

func TestAgentRunner_RetryToollessRefusalOnce(t *testing.T) {
	SetToolClientFactory(func(_ context.Context) (ToolClient, error) {
		return &fakeToolClient{tools: []ToolInfo{{Name: "agent_update", Description: "Update an agent"}}}, nil
	})
	t.Cleanup(func() { SetToolClientFactory(nil) })

	provider := &sequenceProvider{responses: [][]llm.Event{
		{{Type: llm.EventTypeText, Text: "I don't have direct access to modify your model configuration from here."}, {Type: llm.EventTypeDone}},
		{{Type: llm.EventTypeText, Text: `{"tool":"agent_update","arguments":{"name":"assistant","model":"openai/gpt-5.2"}}`}, {Type: llm.EventTypeDone}},
	}}

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_assistant", Name: "assistant", Model: "anthropic/claude"},
		&config.AgentConfig{Name: "assistant", Model: "anthropic/claude"},
		provider,
		nil,
	)

	var gotText strings.Builder
	done := make(chan struct{}, 1)
	runner.Prompt(context.Background(), "set my model to openai/gpt-5.2", func(e StreamEvent) {
		if e.Type == StreamEventText {
			gotText.WriteString(e.Text)
		}
		if e.Type == StreamEventDone || e.Type == StreamEventError {
			done <- struct{}{}
		}
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runner")
	}

	if provider.callCount() < 2 {
		t.Fatalf("expected at least 2 provider calls (includes one retry), got %d", provider.callCount())
	}

	if strings.Contains(strings.ToLower(gotText.String()), "don't have direct access") {
		t.Fatalf("unexpected toolless refusal in final output: %q", gotText.String())
	}
}

func TestSessionProcessingLifecycleAndStop(t *testing.T) {
	t.Helper()

	runs.mu.Lock()
	runs.bySession = make(map[string]map[uint64]context.CancelFunc)
	runs.nextID = 0
	runs.mu.Unlock()

	var mu sync.Mutex
	changes := make([]bool, 0, 2)
	SetSessionProcessingObserver(func(sessionID string, processing bool) {
		if sessionID != "sess-test" {
			return
		}
		mu.Lock()
		changes = append(changes, processing)
		mu.Unlock()
	})
	t.Cleanup(func() { SetSessionProcessingObserver(nil) })

	ctx, cancel := context.WithCancel(context.Background())
	untrack := trackSessionRun("sess-test", cancel)

	if !IsSessionProcessing("sess-test") {
		t.Fatal("expected session to be processing after trackSessionRun")
	}

	if stopped := StopSession("sess-test"); stopped != 1 {
		t.Fatalf("expected exactly one stopped run, got %d", stopped)
	}

	select {
	case <-ctx.Done():
	case <-time.After(1 * time.Second):
		t.Fatal("expected StopSession to cancel tracked context")
	}

	if IsSessionProcessing("sess-test") {
		t.Fatal("expected session to be idle after StopSession")
	}

	// Cleanup should be idempotent even after StopSession removed the run.
	untrack()

	mu.Lock()
	defer mu.Unlock()
	if len(changes) < 2 || !changes[0] || changes[len(changes)-1] {
		t.Fatalf("expected processing transitions [true ... false], got %+v", changes)
	}
}

func TestNewID(t *testing.T) {
	a := newID("sess")
	b := newID("sess")
	if !strings.HasPrefix(a, "sess_") || !strings.HasPrefix(b, "sess_") {
		t.Fatalf("newID prefix mismatch: %s %s", a, b)
	}
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
		t.Fatal("timed out waiting for runner")
	}
	if len(got) < 2 {
		t.Fatalf("expected at least text+done events, got %d", len(got))
	}
	if got[0].Type != StreamEventText || got[len(got)-1].Type != StreamEventDone {
		t.Fatalf("unexpected events: %+v", got)
	}
}

func TestAgentRunner_WithProvider(t *testing.T) {
	provider := &mockProvider{events: []llm.Event{{Type: llm.EventTypeText, Text: "hello "}, {Type: llm.EventTypeText, Text: "world"}}}
	runner := NewAgentRunner(&domain.Agent{ID: "a1", Model: "anthropic/test"}, &config.AgentConfig{Name: "bot"}, provider, nil)

	var text string
	done := make(chan struct{}, 1)
	runner.Prompt(context.Background(), "hi", func(e StreamEvent) {
		if e.Type == StreamEventText {
			text += e.Text
		}
		if e.Type == StreamEventDone {
			done <- struct{}{}
		}
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for done")
	}
	if text != "hello world" {
		t.Fatalf("unexpected text: %q", text)
	}
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
			if err == nil {
				t.Fatal("expected non-nil err")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for stream setup error")
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
			if err == nil {
				t.Fatal("expected non-nil err")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for event error")
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
		if typ != StreamEventStop {
			t.Fatalf("expected stop event, got %s", typ)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for stop event")
	}

	if runner.Agent() != a {
		t.Fatal("Agent accessor mismatch")
	}
	if runner.Config() != cfg {
		t.Fatal("Config accessor mismatch")
	}
}

func TestManager_ReconcileAndLookup(t *testing.T) {
	mgr := NewManager(nil)

	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot1", Model: "anthropic/claude"}, {Name: "bot2", Model: "openai/gpt-4"}}}
	mgr.Reconcile(cfg)

	if _, ok := mgr.Get("bot1"); !ok {
		t.Fatal("bot1 should exist")
	}
	if _, ok := mgr.Get("bot2"); !ok {
		t.Fatal("bot2 should exist")
	}
	if got := len(mgr.List()); got != 2 {
		t.Fatalf("expected 2 agents, got %d", got)
	}

	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "bot1", Model: "anthropic/claude"}}})
	if _, ok := mgr.Get("bot2"); ok {
		t.Fatal("bot2 should have been removed")
	}

	r1, _ := mgr.Get("bot1")
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "bot1", Model: "openai/gpt-4.1"}}})
	r2, _ := mgr.Get("bot1")
	if r1 == r2 {
		t.Fatal("bot1 runner should be replaced when model changes")
	}

	mgr.Stop()
}

func TestSessionManager_CreateAndGetOrCreate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	sm := NewSessionManager()
	s1, err := sm.Create("agent1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if s1.ID == "" || s1.AgentID != "agent1" {
		t.Fatalf("unexpected session: %+v", s1)
	}

	s2, err := sm.GetOrCreate("agent1")
	if err != nil {
		t.Fatalf("getorcreate: %v", err)
	}
	if s2.ID == "" || s2.AgentID != "agent1" {
		t.Fatalf("unexpected session 2: %+v", s2)
	}
}

func TestDiscoverSkillsAndBuildPrompt(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "planner")
	if err := store.EnsureDirs(); err != nil {
		// EnsureDirs unrelated to this test; ignore data-dir setup state.
		_ = err
	}
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("Plan steps carefully."), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	skills, err := DiscoverSkills(dir)
	if err != nil {
		t.Fatalf("discover skills: %v", err)
	}
	if len(skills) != 1 || skills[0].Name != "planner" {
		t.Fatalf("unexpected skills: %+v", skills)
	}

	prompt := BuildSystemPrompt("Base prompt", skills)
	if !strings.Contains(prompt, `<skill name="planner">`) || !strings.Contains(prompt, "Base prompt") {
		t.Fatalf("unexpected prompt: %s", prompt)
	}
}
