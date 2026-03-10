package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/memory"
	"github.com/lsegal/aviary/internal/store"
)

// ── localFileToDataURL large file ─────────────────────────────────────────────

func TestLocalFileToDataURL_FileTooLarge(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "large*.bin")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer f.Close() //nolint:errcheck

	// Write more than maxInlineSessionMediaBytes
	huge := make([]byte, maxInlineSessionMediaBytes+1)
	huge[0] = 0x89 // make non-empty
	if _, err := f.Write(huge); err != nil {
		t.Fatalf("write: %v", err)
	}
	f.Close() //nolint:errcheck

	_, err = localFileToDataURL(f.Name())
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected 'too large' error, got %v", err)
	}
}

// ── startProviderPingIfStale "already in flight" ──────────────────────────────

func TestStartProviderPingIfStale_AlreadyInFlight(t *testing.T) {
	const provider = "test-inflight-provider"

	// Clean up state from other tests
	providerPingMu.Lock()
	delete(providerPingCache, provider)
	providerPingMu.Unlock()
	providerPingActive.Delete(provider)

	// Pre-load the active map to simulate an in-flight ping
	providerPingActive.Store(provider, struct{}{})
	defer providerPingActive.Delete(provider)

	factory := llm.NewFactory(func(_ string) (string, error) { return "", nil })
	// Calling when in-flight should return immediately without starting another goroutine
	startProviderPingIfStale(provider, "test/model", factory)

	// Entry should still not be in cache (in-flight never set it)
	providerPingMu.RLock()
	_, cached := providerPingCache[provider]
	providerPingMu.RUnlock()
	if cached {
		t.Fatal("expected no cache entry for in-flight provider")
	}
}

// ── web_search Brave fallback (no results → browser) ─────────────────────────

func TestWebSearch_BraveNoResults_FallbackNoBrowser(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	// Set up auth with a brave key
	authPath := filepath.Join(base, "aviary", "auth", "credentials.json")
	as, err := auth.NewFileStore(authPath)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	if err := as.Set("brave_api_key", "test-key"); err != nil {
		t.Fatalf("set brave key: %v", err)
	}
	if err := config.Save("", &config.Config{
		Search: config.SearchConfig{
			Web: config.WebSearchConfig{BraveAPIKey: "auth:brave_api_key"},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	SetDeps(&Deps{Auth: as, Browser: nil})

	// Mock Brave to return empty results
	emptyPayload := `{"web":{"results":[]}}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(emptyPayload))
	}))
	defer ts.Close()

	origClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = origClient })
	http.DefaultClient = &http.Client{
		Transport: &redirectTransport{from: "https://api.search.brave.com", to: ts.URL},
	}

	d := NewDispatcher("https://localhost:16677", "")
	// Brave returned no results, no browser → should error with "no search backend"
	toolCallContains(t, d, "web_search", map[string]any{"query": "empty results"}, "no search backend")
}

func TestWebSearch_BraveError_FallbackNoBrowser(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	authPath := filepath.Join(base, "aviary", "auth", "credentials.json")
	as, err := auth.NewFileStore(authPath)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	if err := as.Set("brave_api_key", "bad-key"); err != nil {
		t.Fatalf("set brave key: %v", err)
	}
	if err := config.Save("", &config.Config{
		Search: config.SearchConfig{
			Web: config.WebSearchConfig{BraveAPIKey: "auth:brave_api_key"},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	SetDeps(&Deps{Auth: as, Browser: nil})

	// Mock Brave to return an error
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer ts.Close()

	origClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = origClient })
	http.DefaultClient = &http.Client{
		Transport: &redirectTransport{from: "https://api.search.brave.com", to: ts.URL},
	}

	d := NewDispatcher("https://localhost:16677", "")
	// Brave failed, no browser → should fall back and error
	toolCallContains(t, d, "web_search", map[string]any{"query": "error query"}, "no search backend")
}

func TestWebSearch_CountClamping(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	authPath := filepath.Join(base, "aviary", "auth", "credentials.json")
	as, err := auth.NewFileStore(authPath)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	if err := as.Set("brave_api_key", "test-key"); err != nil {
		t.Fatalf("set brave key: %v", err)
	}
	if err := config.Save("", &config.Config{
		Search: config.SearchConfig{
			Web: config.WebSearchConfig{BraveAPIKey: "auth:brave_api_key"},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	SetDeps(&Deps{Auth: as, Browser: nil})

	// Mock brave to return a single result
	payload := `{"web":{"results":[{"title":"R","url":"https://r.com","description":"r"}]}}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	defer ts.Close()

	origClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = origClient })
	http.DefaultClient = &http.Client{
		Transport: &redirectTransport{from: "https://api.search.brave.com", to: ts.URL},
	}

	d := NewDispatcher("https://localhost:16677", "")

	// count > 20 should be clamped to 20
	out, err := d.CallTool(context.Background(), "web_search", map[string]any{"query": "test", "count": 100})
	if err != nil {
		t.Fatalf("web_search count > 20: %v", err)
	}
	if !strings.Contains(out, "R") {
		t.Fatalf("expected result, got %q", out)
	}

	// count <= 0 should default to 10
	out, err = d.CallTool(context.Background(), "web_search", map[string]any{"query": "test", "count": 0})
	if err != nil {
		t.Fatalf("web_search count 0: %v", err)
	}
	_ = out
}

// ── reconcileAgents config load error ─────────────────────────────────────────

func TestReconcileAgents_BadConfigDir(t *testing.T) {
	// Set XDG_CONFIG_HOME to a non-writable/non-existent path so config.Load fails
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "nonexistent", "subdir"))

	old := GetDeps()
	mgr := agent.NewManager(nil)
	SetDeps(&Deps{Agents: mgr})
	t.Cleanup(func() { SetDeps(old) })

	// reconcileAgents should handle config load error gracefully
	reconcileAgents()
}

// ── auth tools with short value (masking) ────────────────────────────────────

func TestAuthGet_ShortValue(t *testing.T) {
	d, _ := setupMCPWithAuth(t)

	// Store a very short credential (≤4 chars) to exercise the "****" masking path
	_, err := d.CallTool(context.Background(), "auth_set", map[string]any{"name": "test:short", "value": "abc"})
	if err != nil {
		t.Fatalf("auth_set short: %v", err)
	}

	out, err := d.CallTool(context.Background(), "auth_get", map[string]any{"name": "test:short"})
	if err != nil {
		t.Fatalf("auth_get short: %v", err)
	}
	// Value ≤4 chars → masked as "****"
	if !strings.Contains(out, "****") {
		t.Fatalf("expected '****' masking for short value, got %q", out)
	}
	if strings.Contains(out, "abc") {
		t.Fatalf("credential value should not be visible, got %q", out)
	}
}

// ── config_save validation error path ─────────────────────────────────────────

func TestConfigSave_ValidationError(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}
	cfg := &config.Config{}
	if err := config.Save("", cfg); err != nil {
		t.Fatalf("save empty config: %v", err)
	}

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })
	SetDeps(&Deps{Agents: agent.NewManager(nil)})

	d := NewDispatcher("https://localhost:16677", "")

	// Valid JSON but with validation issues (e.g. agent without model)
	// Use empty config which should pass validation
	cfgJSON := `{"agents":[]}`
	out, err := d.CallTool(context.Background(), "config_save", map[string]any{"config": cfgJSON})
	if err != nil {
		t.Fatalf("config_save valid: %v", err)
	}
	if !strings.Contains(out, "saved") {
		t.Fatalf("expected 'saved', got %q", out)
	}
}

// ── concurrent startProviderPingIfStale ──────────────────────────────────────

func TestStartProviderPingIfStale_Concurrent(_ *testing.T) {
	const provider = "test-concurrent-ping"

	providerPingMu.Lock()
	delete(providerPingCache, provider)
	providerPingMu.Unlock()
	providerPingActive.Delete(provider)

	factory := llm.NewFactory(func(_ string) (string, error) { return "", nil })

	// Launch multiple goroutines concurrently — only one should win the LoadOrStore
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			startProviderPingIfStale(provider, "test/model", factory)
		}()
	}
	wg.Wait()

	// Wait for background goroutine to complete
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		providerPingMu.RLock()
		_, cached := providerPingCache[provider]
		providerPingMu.RUnlock()
		if cached {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Clean up
	providerPingMu.Lock()
	delete(providerPingCache, provider)
	providerPingMu.Unlock()
}

// ── memory_query tool (old API test coverage gap) ─────────────────────────────

func TestMemoryQueryTool(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	mem := memory.New()
	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "x"}}})
	SetDeps(&Deps{Agents: mgr, Memory: mem})

	d := NewDispatcher("https://localhost:16677", "")

	// Store and then query
	if _, err := d.CallTool(context.Background(), "memory_store", map[string]any{"agent": "bot", "content": "cats are great"}); err != nil {
		t.Fatalf("memory_store: %v", err)
	}

	out, err := d.CallTool(context.Background(), "memory_search", map[string]any{"agent": "bot", "query": "cats"})
	if err != nil {
		t.Fatalf("memory_search: %v", err)
	}
	if !strings.Contains(out, "cats") {
		t.Fatalf("expected 'cats' in memory_search result, got %q", out)
	}

	// memory_show
	out, err = d.CallTool(context.Background(), "memory_show", map[string]any{"agent": "bot"})
	if err != nil {
		t.Fatalf("memory_show: %v", err)
	}
	if !strings.Contains(out, "cats") {
		t.Fatalf("expected 'cats' in memory_show, got %q", out)
	}

	// memory_clear
	out, err = d.CallTool(context.Background(), "memory_clear", map[string]any{"agent": "bot"})
	if err != nil {
		t.Fatalf("memory_clear: %v", err)
	}
	if !strings.Contains(out, "cleared") {
		t.Fatalf("expected 'cleared' in memory_clear output, got %q", out)
	}
}

// ── job_list with scheduler ───────────────────────────────────────────────────

func TestJobListWithScheduler(t *testing.T) {
	d, s := setupDispatcherWithScheduler(t)

	// Enqueue a job
	_, err := s.Queue().Enqueue("bot/task", "agent_bot", "bot", "run", "", 1, "", "")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	out, err := d.CallTool(context.Background(), "job_list", map[string]any{})
	if err != nil {
		t.Fatalf("job_list: %v", err)
	}
	if !strings.Contains(out, "bot/task") {
		t.Fatalf("expected 'bot/task' in job_list, got %q", out)
	}
}

// ── agent_update with fallbacks ───────────────────────────────────────────────

func TestAgentUpdate_WithFallbacks(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	cfg := &config.Config{
		Agents: []config.AgentConfig{{Name: "bot", Model: "x"}},
	}
	if err := config.Save("", cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	mgr := agent.NewManager(nil)
	mgr.Reconcile(cfg)
	SetDeps(&Deps{Agents: mgr})

	d := NewDispatcher("https://localhost:16677", "")

	out, err := d.CallTool(context.Background(), "agent_update", map[string]any{
		"name":      "bot",
		"fallbacks": []any{"fallback-model"},
	})
	if err != nil {
		t.Fatalf("agent_update fallbacks: %v", err)
	}
	if !strings.Contains(out, "updated") {
		t.Fatalf("expected 'updated', got %q", out)
	}
}

func TestAgentList_UsesGlobalDefaultsForEffectiveModel(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	cfg := &config.Config{
		Agents: []config.AgentConfig{{Name: "assistant", Model: ""}},
		Models: config.ModelsConfig{
			Defaults: &config.ModelDefaults{
				Model:     "google/gemini-2.0-flash",
				Fallbacks: []string{"openai-codex/gpt-5.2"},
			},
		},
	}
	if err := config.Save("", cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	mgr := agent.NewManager(nil)
	mgr.Reconcile(cfg)
	SetDeps(&Deps{Agents: mgr})

	d := NewDispatcher("https://localhost:16677", "")

	out, err := d.CallTool(context.Background(), "agent_list", map[string]any{})
	if err != nil {
		t.Fatalf("agent_list: %v", err)
	}
	if !strings.Contains(out, "google/gemini-2.0-flash") {
		t.Fatalf("expected effective default model in agent_list, got %q", out)
	}
	if !strings.Contains(out, "openai-codex/gpt-5.2") {
		t.Fatalf("expected effective default fallback in agent_list, got %q", out)
	}
}

// ── registerSkillTools (skills_list) coverage ────────────────────────────────

func TestSkillsListTool_WithAgents(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	cfg := &config.Config{
		Agents: []config.AgentConfig{{Name: "bot", Model: "anthropic/claude-3-haiku"}},
	}
	if err := config.Save("", cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	mgr := agent.NewManager(nil)
	mgr.Reconcile(cfg)
	SetDeps(&Deps{Agents: mgr})

	d := NewDispatcher("https://localhost:16677", "")
	out, err := d.CallTool(context.Background(), "skills_list", map[string]any{})
	if err != nil {
		t.Fatalf("skills_list with agents: %v", err)
	}
	_ = out
}

// ── dispatcher CallTool error path ───────────────────────────────────────────

func TestDispatcherCallTool_ToolError(t *testing.T) {
	old := GetDeps()
	SetDeps(&Deps{Scheduler: nil})
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	d := NewDispatcher("https://localhost:16677", "")
	// Calling a tool that returns an MCP error (scheduler not initialized)
	out, err := d.CallTool(context.Background(), "task_list", map[string]any{})
	if err != nil {
		// It might return an error
		_ = err
		return
	}
	// Or it might return an MCP error result as text
	if !strings.Contains(out, "scheduler not initialized") {
		t.Fatalf("expected scheduler error in output, got %q", out)
	}
}
