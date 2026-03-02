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
	if !strings.Contains(prompt, "Skill: planner") || !strings.Contains(prompt, "Base prompt") {
		t.Fatalf("unexpected prompt: %s", prompt)
	}
}
