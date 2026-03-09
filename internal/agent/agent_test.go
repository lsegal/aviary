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

var testDataDir string

func TestMain(m *testing.M) {
	var err error
	testDataDir, err = os.MkdirTemp("", "aviary-agent-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(testDataDir)
	store.SetDataDir(testDataDir)
	os.Exit(m.Run())
}

// setTestDataDir gives t its own isolated data directory and restores
// the shared testDataDir when the test finishes.
func setTestDataDir(t *testing.T) {
	t.Helper()
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir(testDataDir) })
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
	runner := NewAgentRunner(&domain.Agent{ID: "a1", Model: "anthropic/test"}, &config.AgentConfig{Name: "bot"}, nil, nil, nil)

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
	runner := NewAgentRunner(&domain.Agent{ID: "a1", Model: "anthropic/test"}, &config.AgentConfig{Name: "bot"}, provider, nil, nil)

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
		runner := NewAgentRunner(&domain.Agent{ID: "a1", Model: "anthropic/test"}, &config.AgentConfig{Name: "bot"}, &mockProvider{err: errors.New("boom")}, nil, nil)
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
		runner := NewAgentRunner(&domain.Agent{ID: "a1", Model: "anthropic/test"}, &config.AgentConfig{Name: "bot"}, &mockProvider{events: []llm.Event{{Type: llm.EventTypeError, Error: errors.New("event boom")}}}, nil, nil)
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
	runner := NewAgentRunner(a, cfg, nil, nil, nil)

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
	setTestDataDir(t)
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

func TestSessionManager_List(t *testing.T) {
	setTestDataDir(t)
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

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
	if err := store.AppendJSONL(store.SessionPath(agentID, s1.ID), s1); err != nil {
		t.Fatalf("setup s1: %v", err)
	}

	// Create another session with new AgentID format (agent_assistant)
	s2 := &domain.Session{
		ID:        "agent_assistant-other",
		AgentID:   "agent_assistant",
		Name:      "other",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.AppendJSONL(store.SessionPath(agentID, s2.ID), s2); err != nil {
		t.Fatalf("setup s2: %v", err)
	}

	list, err := sm.List(agentID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(list))
	}

	// Verify "main" is first
	if list[0].Name != "main" {
		t.Errorf("expected main session first, got %q", list[0].Name)
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

func TestFilterTools_AllowList(t *testing.T) {
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_bot", Name: "bot"},
		&config.AgentConfig{
			Name:        "bot",
			Permissions: &config.PermissionsConfig{Tools: []string{"tool_a", "tool_b"}},
		},
		&mockProvider{},
		nil,
		nil,
	)

	tools := []ToolInfo{
		{Name: "tool_a", Description: "Tool A"},
		{Name: "tool_b", Description: "Tool B"},
		{Name: "tool_c", Description: "Tool C"},
	}

	// Agent-level permissions apply when no per-message restrictions.
	filtered := runner.filterTools(tools, nil)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered tools, got %d", len(filtered))
	}
	for _, tool := range filtered {
		if tool.Name != "tool_a" && tool.Name != "tool_b" {
			t.Errorf("unexpected tool: %s", tool.Name)
		}
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
		nil,
	)

	tools := []ToolInfo{
		{Name: "tool_a"},
		{Name: "tool_b"},
		{Name: "tool_c"},
	}

	// Per-message override restricts to only tool_c.
	filtered := runner.filterTools(tools, []string{"tool_c"})
	if len(filtered) != 1 || filtered[0].Name != "tool_c" {
		t.Fatalf("expected 1 filtered tool (tool_c), got %v", filtered)
	}
}

func TestFilterTools_NoRestrictions(t *testing.T) {
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_bot", Name: "bot"},
		&config.AgentConfig{Name: "bot"}, // no permissions
		&mockProvider{},
		nil,
		nil,
	)

	tools := []ToolInfo{{Name: "tool_a"}, {Name: "tool_b"}}
	filtered := runner.filterTools(tools, nil)
	if len(filtered) != 2 {
		t.Fatalf("expected all 2 tools, got %d", len(filtered))
	}
}

func TestLoadRules_InlineText(t *testing.T) {
	setTestDataDir(t)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_bot", Name: "bot"},
		&config.AgentConfig{Name: "bot", Rules: "Be helpful."},
		&mockProvider{},
		nil,
		nil,
	)

	rules := runner.loadRules()
	if rules != "Be helpful." {
		t.Errorf("expected inline rules, got %q", rules)
	}
}

func TestLoadRules_FilePath(t *testing.T) {
	setTestDataDir(t)

	dir := t.TempDir()
	rulesFile := filepath.Join(dir, "RULES.md")
	if err := os.WriteFile(rulesFile, []byte("# Rules\nBe safe."), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_bot", Name: "bot"},
		&config.AgentConfig{Name: "bot", Rules: rulesFile},
		&mockProvider{},
		nil,
		nil,
	)

	rules := runner.loadRules()
	if !strings.Contains(rules, "Be safe.") {
		t.Errorf("expected file rules, got %q", rules)
	}
}

func TestLoadRules_FallbackToDataDir(t *testing.T) {
	setTestDataDir(t)

	// Write RULES.md to the agent's data directory.
	agentID := "agent_ruletest"
	rulesPath := store.AgentRulesPath(agentID)
	if err := os.MkdirAll(filepath.Dir(rulesPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rulesPath, []byte("Follow safety guidelines."), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := NewAgentRunner(
		&domain.Agent{ID: agentID, Name: "ruletest"},
		&config.AgentConfig{Name: "ruletest"}, // no inline rules
		&mockProvider{},
		nil,
		nil,
	)

	rules := runner.loadRules()
	if !strings.Contains(rules, "Follow safety guidelines.") {
		t.Errorf("expected fallback rules from data dir, got %q", rules)
	}
}

func TestLoadRules_Empty(t *testing.T) {
	setTestDataDir(t)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_norules", Name: "norules"},
		&config.AgentConfig{Name: "norules"}, // no rules
		&mockProvider{},
		nil,
		nil,
	)

	rules := runner.loadRules()
	if rules != "" {
		t.Errorf("expected empty rules, got %q", rules)
	}
}

func TestAppendSessionMessage_SkipsEmptyContent(t *testing.T) {
	setTestDataDir(t)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_msg", Name: "msgtest"},
		&config.AgentConfig{Name: "msgtest"},
		&mockProvider{},
		nil,
		nil,
	)

	// Empty content should not create a file.
	runner.appendSessionMessage("sess1", domain.MessageRoleUser, "", "", "")
	p := store.SessionPath("agent_msg", "sess1")
	if _, err := os.Stat(p); err == nil {
		t.Error("expected no session file for empty content")
	}
}

func TestAppendSessionMessage_PersistsMessage(t *testing.T) {
	setTestDataDir(t)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_persist", Name: "persist"},
		&config.AgentConfig{Name: "persist"},
		&mockProvider{},
		nil,
		nil,
	)

	runner.appendSessionMessage("sess2", domain.MessageRoleUser, "Hello, world!", "", "")

	p := store.SessionPath("agent_persist", "sess2")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("expected session file: %v", err)
	}
	if !strings.Contains(string(data), "Hello, world!") {
		t.Errorf("expected message in session file, got: %s", string(data))
	}
}

func TestResolveSessionID_FromContext(t *testing.T) {
	setTestDataDir(t)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_sess", Name: "sesstest"},
		&config.AgentConfig{Name: "sesstest"},
		&mockProvider{},
		nil,
		nil,
	)

	ctx := WithSessionID(context.Background(), "explicit-session-id")
	sessionID := runner.resolveSessionID(ctx)
	if sessionID != "explicit-session-id" {
		t.Errorf("expected explicit session ID, got %q", sessionID)
	}
}

func TestSetSessionMessageObserver(t *testing.T) {
	var notified string
	SetSessionMessageObserver(func(sessionID, role string) {
		notified = sessionID + "/" + role
	})
	t.Cleanup(func() { SetSessionMessageObserver(nil) })

	notifySessionMessage("sess123", "user")
	if notified != "sess123/user" {
		t.Errorf("expected sess123/user, got %q", notified)
	}
}

func TestRegisterSessionDelivery(t *testing.T) {
	var received string
	RegisterSessionDelivery("test-sess", "signal", "+1", func(text string) { received = text })

	deliverToSession("test-sess", "hello delivery")
	if received != "hello delivery" {
		t.Errorf("expected 'hello delivery', got %q", received)
	}

	// Empty text should not call delivery function.
	received = ""
	deliverToSession("test-sess", "")
	if received != "" {
		t.Error("expected no delivery for empty text")
	}

	// Unknown session should not panic.
	deliverToSession("unknown-sess", "no delivery")
}

func TestRegisterSessionDelivery_Idempotent(t *testing.T) {
	var calls int
	RegisterSessionDelivery("sess-idem", "slack", "C1", func(_ string) { calls++ })
	RegisterSessionDelivery("sess-idem", "slack", "C1", func(_ string) { calls += 10 })

	// Second registration overwrites the first.
	deliverToSession("sess-idem", "msg")
	if calls != 10 {
		t.Errorf("expected calls=10 (second fn overwrites), got %d", calls)
	}
}

func TestRegisterSessionMediaDelivery(t *testing.T) {
	var captionGot, pathGot string
	RegisterSessionMediaDelivery("media-sess", "signal", "+2", func(caption, path string) {
		captionGot = caption
		pathGot = path
	})

	DeliverMediaToSession("media-sess", "my caption", "/path/to/file.jpg")
	if captionGot != "my caption" || pathGot != "/path/to/file.jpg" {
		t.Errorf("unexpected delivery: caption=%q path=%q", captionGot, pathGot)
	}

	// Empty path should not call delivery function.
	captionGot = ""
	DeliverMediaToSession("media-sess", "ignored", "")
	if captionGot != "" {
		t.Error("expected no media delivery for empty path")
	}
}

func TestSetMemoryCompactionObserver(t *testing.T) {
	var notifiedAgent string
	SetMemoryCompactionObserver(func(agentID, poolID string, started bool) {
		notifiedAgent = agentID
	})
	t.Cleanup(func() { SetMemoryCompactionObserver(nil) })

	notifyMemoryCompaction("agent_test", "pool1", true)
	if notifiedAgent != "agent_test" {
		t.Errorf("expected agent_test, got %q", notifiedAgent)
	}
}

func TestSessionManager_CreateWithName(t *testing.T) {
	setTestDataDir(t)
	sm := NewSessionManager()

	sess, err := sm.CreateWithName("agent_named", "mysession")
	if err != nil {
		t.Fatalf("CreateWithName: %v", err)
	}
	if sess.Name != "mysession" {
		t.Errorf("Name = %q; want %q", sess.Name, "mysession")
	}
	if sess.AgentID != "agent_named" {
		t.Errorf("AgentID = %q; want %q", sess.AgentID, "agent_named")
	}
	if sess.ID == "" {
		t.Error("expected non-empty session ID")
	}
}

func TestSessionManager_CreateWithName_AlwaysNew(t *testing.T) {
	setTestDataDir(t)
	sm := NewSessionManager()

	sess1, _ := sm.CreateWithName("agent_new", "myname")
	sess2, _ := sm.CreateWithName("agent_new", "myname")

	if sess1.ID == sess2.ID {
		t.Error("expected different IDs from two CreateWithName calls")
	}
}

func TestAppendMessageToSession(t *testing.T) {
	setTestDataDir(t)

	agentID := "agent_append_msg"
	sessionID := "sess_amsg"

	// Create session first.
	sm := NewSessionManager()
	_, _ = sm.CreateWithName(agentID, "amsg")

	err := AppendMessageToSession(agentID, sessionID, domain.MessageRoleUser, "Hello there!")
	if err != nil {
		t.Fatalf("AppendMessageToSession: %v", err)
	}

	// Verify message was written.
	p := store.SessionPath(agentID, sessionID)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	if !strings.Contains(string(data), "Hello there!") {
		t.Errorf("expected message in session, got: %s", string(data))
	}
}

func TestRunnerMemoryPoolID(t *testing.T) {
	setTestDataDir(t)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_mpid", Name: "mpid"},
		&config.AgentConfig{Name: "mpid", Memory: "shared"},
		&mockProvider{},
		nil,
		nil,
	)

	poolID := runner.memoryPoolID()
	if poolID != "shared" {
		t.Errorf("memoryPoolID = %q; want %q", poolID, "shared")
	}
}

func TestRunnerMemoryPoolID_Default(t *testing.T) {
	setTestDataDir(t)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_mpid2", Name: "mpid2"},
		&config.AgentConfig{Name: "mpid2"}, // no Memory field
		&mockProvider{},
		nil,
		nil,
	)

	poolID := runner.memoryPoolID()
	if poolID != "private:mpid2" {
		t.Errorf("memoryPoolID default = %q; want %q", poolID, "private:mpid2")
	}
}

func TestRunnerCompactKeep(t *testing.T) {
	setTestDataDir(t)

	t.Run("explicit value", func(t *testing.T) {
		runner := NewAgentRunner(
			&domain.Agent{ID: "agent_ck", Name: "ck"},
			&config.AgentConfig{Name: "ck", CompactKeep: 50},
			&mockProvider{},
			nil,
			nil,
		)
		if v := runner.compactKeep(); v != 50 {
			t.Errorf("compactKeep = %d; want 50", v)
		}
	})

	t.Run("default value", func(t *testing.T) {
		runner := NewAgentRunner(
			&domain.Agent{ID: "agent_ck2", Name: "ck2"},
			&config.AgentConfig{Name: "ck2"},
			&mockProvider{},
			nil,
			nil,
		)
		if v := runner.compactKeep(); v <= 0 {
			t.Errorf("compactKeep default should be positive, got %d", v)
		}
	})
}

func TestRunnerWait(t *testing.T) {
	setTestDataDir(t)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_wait", Name: "wait"},
		&config.AgentConfig{Name: "wait"},
		&mockProvider{},
		nil,
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
		t.Fatal("Wait() did not return promptly on idle runner")
	}
}
