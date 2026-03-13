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
	"github.com/lsegal/aviary/internal/memory"
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

type recordingToolClient struct {
	tools   []ToolInfo
	mu      sync.Mutex
	calls   []toolCall
	results map[string]string
}

func (r *recordingToolClient) ListTools(_ context.Context) ([]ToolInfo, error) { return r.tools, nil }

func (r *recordingToolClient) CallToolText(_ context.Context, name string, args map[string]any) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, toolCall{Tool: name, Arguments: args})
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
			assert.True(t, ok)
			assert.Equal(t, tc.tool, parsed.Tool)

		})
	}
}

func TestParseInlineToolCalls(t *testing.T) {
	input := `[tool] {"name":"browser_click","arguments":{"selector":"a[href=\"/chat\"]","tab_id":"tab123"}}[tool] {"name":"browser_screenshot","arguments":{"tab_id":"tab123"}} Sent the Chat section screenshot.`

	calls, trailing, ok := parseInlineToolCalls(input)
	assert.True(t, ok)
	assert.Len(t, calls, 2)
	assert.Equal(t, "browser_click", calls[0].Tool)
	assert.Equal(t, "browser_screenshot", calls[1].Tool)
	assert.Equal(t, "Sent the Chat section screenshot.", trailing)
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
		&config.AgentConfig{
			Name:  "assistant",
			Model: "anthropic/claude",
			Permissions: &config.PermissionsConfig{
				Preset: config.PermissionsPresetFull,
			},
		},
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
		assert.FailNow(t, "timeout")
	}
	assert.GreaterOrEqual(t, provider.callCount(), 2)
	assert.False(t, strings.Contains(strings.ToLower(gotText.String()), "don't have direct access"))

}

func TestAgentRunner_ExecutesInlineToolBlocks(t *testing.T) {
	toolClient := &recordingToolClient{
		tools: []ToolInfo{
			{Name: "browser_click", Description: "Click in the browser"},
			{Name: "browser_screenshot", Description: "Take a screenshot"},
			{Name: "channel_send_file", Description: "Send a file to the channel"},
		},
		results: map[string]string{
			"browser_click":      "clicked",
			"browser_screenshot": `{"file_path":"C:\\tmp\\chat.png"}`,
			"channel_send_file":  "sent",
		},
	}
	SetToolClientFactory(func(_ context.Context) (ToolClient, error) {
		return toolClient, nil
	})
	t.Cleanup(func() { SetToolClientFactory(nil) })

	provider := &sequenceProvider{responses: [][]llm.Event{
		{{
			Type: llm.EventTypeText,
			Text: `[tool] {"name":"browser_click","arguments":{"selector":"a[href=\"/chat\"]","tab_id":"tab123"}}[tool] {"name":"browser_screenshot","arguments":{"tab_id":"tab123"}}[tool] {"name":"channel_send_file","arguments":{"caption":"Chat Section","file_path":"C:\\Users\\Loren\\.config\\aviary\\screenshots\\chat.png"}}Sent the Chat section screenshot.`,
		}, {Type: llm.EventTypeDone}},
		{{Type: llm.EventTypeText, Text: "Which one next?"}, {Type: llm.EventTypeDone}},
	}}

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_inline_tools", Name: "inline-tools", Model: "test/model"},
		&config.AgentConfig{Name: "inline-tools", Model: "test/model"},
		provider,
		nil,
		nil,
	)

	var gotText strings.Builder
	done := make(chan struct{}, 1)
	runner.Prompt(context.Background(), "take the screenshot", func(e StreamEvent) {
		if e.Type == StreamEventText {
			gotText.WriteString(e.Text)
		}
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

	assert.GreaterOrEqual(t, provider.callCount(), 2)
	assert.Len(t, toolClient.calls, 3)
	assert.Equal(t, "browser_click", toolClient.calls[0].Tool)
	assert.Equal(t, "browser_screenshot", toolClient.calls[1].Tool)
	assert.Equal(t, "channel_send_file", toolClient.calls[2].Tool)
	assert.NotContains(t, gotText.String(), `"[name":"browser_click"`)
	assert.Contains(t, gotText.String(), "Which one next?")
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
	assert.True(t, IsSessionProcessing("sess-test"))
	stopped := StopSession("sess-test")
	assert.Equal(t, 1, stopped)

	select {
	case <-ctx.Done():
	case <-time.After(1 * time.Second):
		assert.FailNow(t, "timeout")
	}
	assert.False(t, IsSessionProcessing("sess-test"))

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
		assert.FailNow(t, "timeout")
	}
	assert.GreaterOrEqual(t, len(got), 2)
	assert.Equal(t, StreamEventText, got[0].Type)
	assert.Equal(t, StreamEventDone, got[len(got)-1].Type)

}

func TestAgentRunner_WithProvider(t *testing.T) {
	provider := &mockProvider{events: []llm.Event{{Type: llm.EventTypeText, Text: "hello "}, {Type: llm.EventTypeText, Text: "world"}}}
	runner := NewAgentRunner(&domain.Agent{ID: "a1", Model: "anthropic/test"}, &config.AgentConfig{Name: "bot"}, provider, nil, nil)

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
		runner := NewAgentRunner(&domain.Agent{ID: "a1", Model: "anthropic/test"}, &config.AgentConfig{Name: "bot"}, &mockProvider{err: errors.New("boom")}, nil, nil)
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
			assert.Error(t, err)
		case <-time.After(2 * time.Second):
			assert.FailNow(t, "timeout")
		}
	})

	t.Run("usage records zero-token throttles", func(t *testing.T) {
		setTestDataDir(t)
		err := store.EnsureDirs()
		assert.NoError(t, err)

		runner := NewAgentRunner(
			&domain.Agent{ID: "a1", Model: "google/gemini-3-flash-preview"},
			&config.AgentConfig{Name: "bot"},
			&mockProvider{events: []llm.Event{{Type: llm.EventTypeError, Error: errors.New("429 rate limit")}}},
			nil,
			nil,
		)

		errCh := make(chan error, 1)
		runner.Prompt(context.Background(), "hi", func(e StreamEvent) {
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

			if len(records) == 1 {
				assert.True(t, records[0].HasThrottle)
				assert.False(t, records[0].HasError)
				assert.Equal(t, 0, records[0].InputTokens)
				assert.Equal(t, 0, records[0].OutputTokens)

				break
			}
			assert.False(t, time.Now().After(deadline))

			time.Sleep(10 * time.Millisecond)
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
		nil,
	)

	rules := runner.loadRules()
	assert.Equal(t, "Be helpful.", rules)

}

func TestLoadRules_FilePath(t *testing.T) {
	setTestDataDir(t)

	dir := t.TempDir()
	rulesFile := filepath.Join(dir, "RULES.md")
	err := os.WriteFile(rulesFile, []byte("# Rules\nBe safe."), 0o600)
	assert.NoError(t, err)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_bot", Name: "bot"},
		&config.AgentConfig{Name: "bot", Rules: rulesFile},
		&mockProvider{},
		nil,
		nil,
	)

	rules := runner.loadRules()
	assert.True(t, strings.Contains(rules, "Be safe."))

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
		nil,
	)

	runner.appendSessionMessage("sess2", domain.MessageRoleUser, "Hello, world!", "", "")

	p := store.SessionPath("agent_persist", "sess2")
	data, err := os.ReadFile(p)
	assert.NoError(t, err)
	assert.True(t, strings.Contains(string(data), "Hello, world!"))

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
	assert.Equal(t, "explicit-session-id", sessionID)

}

func TestSetSessionMessageObserver(t *testing.T) {
	var notified string
	SetSessionMessageObserver(func(sessionID, role string) {
		notified = sessionID + "/" + role
	})
	t.Cleanup(func() { SetSessionMessageObserver(nil) })

	notifySessionMessage("sess123", "user")
	assert.Equal(t, "sess123/user", notified)

}

func TestRegisterSessionDelivery(t *testing.T) {
	var received string
	RegisterSessionDelivery("test-sess", "signal", "+1", func(text string) { received = text })

	deliverToSession("test-sess", "hello delivery")
	assert.Equal(t, "hello delivery", received)

	// Empty text should not call delivery function.
	received = ""
	deliverToSession("test-sess", "")
	assert.Equal(t, "", received)

	// Unknown session should not panic.
	deliverToSession("unknown-sess", "no delivery")
}

func TestRegisterSessionDelivery_Idempotent(t *testing.T) {
	var calls int
	RegisterSessionDelivery("sess-idem", "slack", "C1", func(_ string) { calls++ })
	RegisterSessionDelivery("sess-idem", "slack", "C1", func(_ string) { calls += 10 })

	// Second registration overwrites the first.
	deliverToSession("sess-idem", "msg")
	assert.Equal(t, 10, calls)

}

func TestRegisterSessionMediaDelivery(t *testing.T) {
	var captionGot, pathGot string
	RegisterSessionMediaDelivery("media-sess", "signal", "+2", func(caption, path string) {
		captionGot = caption
		pathGot = path
	})

	DeliverMediaToSession("media-sess", "my caption", "/path/to/file.jpg")
	assert.Equal(t, "my caption", captionGot)
	assert.Equal(t, "/path/to/file.jpg", pathGot)

	// Empty path should not call delivery function.
	captionGot = ""
	DeliverMediaToSession("media-sess", "ignored", "")
	assert.Equal(t, "", captionGot)

}

func TestSetMemoryCompactionObserver(t *testing.T) {
	var notifiedAgent string
	SetMemoryCompactionObserver(func(agentID, _ string, started bool) {
		_ = started
		notifiedAgent = agentID
	})
	t.Cleanup(func() { SetMemoryCompactionObserver(nil) })

	notifyMemoryCompaction("agent_test", "pool1", true)
	assert.Equal(t, "agent_test", notifiedAgent)

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
	RegisterSessionDelivery(sessionID, "signal", "+1555", func(text string) {
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
	assert.Equal(t, "shared", poolID)

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
	assert.Equal(t, "private:mpid2", poolID)

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
		v := runner.compactKeep()
		assert.Equal(t, 50, v)

	})

	t.Run("default value", func(t *testing.T) {
		runner := NewAgentRunner(
			&domain.Agent{ID: "agent_ck2", Name: "ck2"},
			&config.AgentConfig{Name: "ck2"},
			&mockProvider{},
			nil,
			nil,
		)
		v := runner.compactKeep()
		assert.Greater(t, v, 0)

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
	ctx6 := WithChannelSession(ctx, "slack", "C123")
	chType, chID, ok := ChannelSessionFromContext(ctx6)
	assert.True(t, ok)
	assert.Equal(t, "slack", chType)
	assert.Equal(t, "C123", chID)

	// WithChannelSession empty type is no-op
	ctx7 := WithChannelSession(ctx, "", "C123")
	_, _, ok = ChannelSessionFromContext(ctx7)
	assert.False(t, ok)

}

func TestPickMap(t *testing.T) {
	// map value
	inner := map[string]any{"a": 1}
	obj := map[string]any{"key": inner}
	got := pickMap(obj, "key")
	assert.Equal(t, 1, got["a"])

	// string value (JSON)
	obj2 := map[string]any{"key": `{"b":2}`}
	got2 := pickMap(obj2, "key")
	assert.Equal(t, float64(2), got2["b"])

	// missing key falls through to next
	obj3 := map[string]any{"other": inner}
	got3 := pickMap(obj3, "missing", "other")
	assert.Equal(t, 1, got3["a"])

	// no keys match returns empty map
	got4 := pickMap(obj3, "nope")
	assert.Equal(t, 0, len(got4))

}

func TestResolveSessionID_ChannelContext(t *testing.T) {
	setTestDataDir(t)

	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_rch", Name: "rch"},
		&config.AgentConfig{Name: "rch"},
		&mockProvider{}, nil, nil,
	)
	ctx := WithChannelSession(context.Background(), "slack", "C999")
	sid := runner.resolveSessionID(ctx)
	assert.
		// Should be non-empty (either a created session ID or fallback)
		NotEqual(t, "", sid)

}

func TestMemoryTokens(t *testing.T) {
	setTestDataDir(t)

	t.Run("explicit", func(t *testing.T) {
		runner := NewAgentRunner(
			&domain.Agent{ID: "agent_mt", Name: "mt"},
			&config.AgentConfig{Name: "mt", MemoryTokens: 512},
			&mockProvider{}, nil, nil,
		)
		v := runner.memoryTokens()
		assert.Equal(t, 512, v)

	})

	t.Run("default", func(t *testing.T) {
		runner := NewAgentRunner(
			&domain.Agent{ID: "agent_mt2", Name: "mt2"},
			&config.AgentConfig{Name: "mt2"},
			&mockProvider{}, nil, nil,
		)
		v := runner.memoryTokens()
		assert.Greater(t, v, 0)

	})
}

func TestLoadMemoryContext_NilMemory(t *testing.T) {
	setTestDataDir(t)
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_lmc", Name: "lmc"},
		&config.AgentConfig{Name: "lmc"},
		&mockProvider{}, nil, nil,
	)
	got := runner.loadMemoryContext("sess1", 1000)
	assert.Equal(t, "", got)

}

func TestLoadMemoryContext_WithMemory(t *testing.T) {
	setTestDataDir(t)
	mem := memory.New()
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_lmc2", Name: "lmc2"},
		&config.AgentConfig{Name: "lmc2"},
		&mockProvider{}, nil, mem,
	)

	// Append a memory entry then call loadMemoryContext.
	poolID := runner.memoryPoolID()
	err := mem.Append(poolID, "sess1", "user", "test memory content")
	assert.NoError(t, err)

	got := runner.loadMemoryContext("sess1", 10000)
	assert.True(t, strings.Contains(got, "test memory content"))

}

func TestAppendMemoryMessage_NilMemory(t *testing.T) {
	setTestDataDir(t)
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_amm", Name: "amm"},
		&config.AgentConfig{Name: "amm"},
		&mockProvider{}, nil, nil,
	)
	// Should not panic with nil memory
	runner.appendMemoryMessage("sess1", domain.MessageRoleUser, "hello")
}

func TestAppendMemoryMessage_WithMemory(t *testing.T) {
	setTestDataDir(t)
	mem := memory.New()
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_amm2", Name: "amm2"},
		&config.AgentConfig{Name: "amm2"},
		&mockProvider{}, nil, mem,
	)
	runner.appendMemoryMessage("sess1", domain.MessageRoleUser, "hello memory")

	poolID := runner.memoryPoolID()
	entries, err := mem.All(poolID)
	assert.NoError(t, err)
	assert.NotEqual(t, 0, len(entries))

	// Empty content should be skipped
	runner.appendMemoryMessage("sess1", domain.MessageRoleUser, "")
	entries2, _ := mem.All(poolID)
	assert.Equal(t, len(entries), len(entries2))

}

func TestMaybeCompactMemory_NilMemory(t *testing.T) {
	setTestDataDir(t)
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_mcm", Name: "mcm"},
		&config.AgentConfig{Name: "mcm"},
		&mockProvider{}, nil, nil,
	)
	// Should not panic
	runner.maybeCompactMemory()
}

func TestMaybeCompactMemory_BelowThreshold(t *testing.T) {
	setTestDataDir(t)
	mem := memory.New()
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_mcm2", Name: "mcm2"},
		&config.AgentConfig{Name: "mcm2"},
		&mockProvider{}, nil, mem,
	)
	// Add a single entry — well below compaction threshold, should return without launching goroutine.
	poolID := runner.memoryPoolID()
	_ = mem.Append(poolID, "sess1", "user", "hello")
	// Should not panic.
	runner.maybeCompactMemory()
}

func TestParseToolCall(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantOK   bool
		wantTool string
	}{
		{"empty", "", false, ""},
		{"plain JSON", `{"tool":"ping","arguments":{"x":1}}`, true, "ping"},
		{"markdown fence", "```json\n{\"tool\":\"foo\",\"arguments\":{}}\n```", true, "foo"},
		{"nil arguments", `{"tool":"bar"}`, true, "bar"},
		{"array first element", `[{"tool":"arr","arguments":{}}]`, true, "arr"},
		{"embedded in text", `some text {"tool":"embed","arguments":{}} more`, true, "embed"},
		{"invalid JSON", `not json`, false, ""},
		{"tool_call wrapper", `{"tool_call":{"tool":"nested","arguments":{"k":"v"}}}`, true, "nested"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseToolCall(tc.input)
			assert.Equal(t, tc.wantOK, ok)
			if ok {
				assert.Equal(t, tc.wantTool, got.Tool)
			}

		})
	}
}

func TestBuildToolSystemPrompt(t *testing.T) {
	tools := []ToolInfo{
		{Name: "tool_a", Description: "does a"},
		{Name: "tool_b"},
	}
	out := buildToolSystemPrompt("myagent", tools)
	assert.True(t, strings.Contains(out, "myagent"))
	assert.True(t, strings.Contains(out, "tool_a"))
	assert.True(t, strings.Contains(out, "does a"))
	assert.True(t, strings.Contains(out, "tool_b"))
	assert.True(t, strings.Contains(out, "note_write"))
	assert.True(t, strings.Contains(out, "memory_store only"))

	// Without agent name.
	out2 := buildToolSystemPrompt("", tools)
	assert.False(t, strings.Contains(out2, "agent name is"))

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
		&mockProvider{}, nil, nil,
	)
	sess, err := NewSessionManager().Create(runner.agent.ID)
	assert.NoError(t, err)

	// Empty content but non-empty mediaURL should persist.
	runner.appendSessionMessage(sess.ID, domain.MessageRoleUser, "", "http://example.com/img.png", "")

	data, err := os.ReadFile(store.SessionPath(runner.agent.ID, sess.ID))
	assert.NoError(t, err)
	assert.True(t, strings.Contains(string(data), "img.png"))

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

func TestLoadMemoryContext_WithNotes(t *testing.T) {
	setTestDataDir(t)
	mem := memory.New()
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_lmcn", Name: "lmcn"},
		&config.AgentConfig{Name: "lmcn"},
		&mockProvider{}, nil, mem,
	)

	poolID := runner.memoryPoolID()
	err := mem.SetNotes(poolID, "important note")
	assert.NoError(t, err)

	got := runner.loadMemoryContext("sess1", 10000)
	assert.True(t, strings.Contains(got, "important note"))
	assert.True(t, strings.Contains(got, "Persistent notes"))

}
