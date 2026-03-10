package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/browser"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/memory"
	"github.com/lsegal/aviary/internal/scheduler"
	"github.com/lsegal/aviary/internal/store"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func setupDispatcherWithScheduler(t *testing.T) (*Dispatcher, *scheduler.Scheduler) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	cfg := &config.Config{
		Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}},
	}
	if err := config.Save("", cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	mgr := agent.NewManager(nil)
	mgr.Reconcile(cfg)
	s, err := scheduler.New(mgr, 1)
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	t.Cleanup(s.Stop)
	s.Reconcile(cfg)
	SetDeps(&Deps{Agents: mgr, Scheduler: s})

	return NewDispatcher("https://localhost:16677", ""), s
}

// ── Agent tools ───────────────────────────────────────────────────────────────

func TestAgentTools_NilDeps(t *testing.T) {
	old := GetDeps()
	SetDeps(&Deps{Agents: nil})
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	d := NewDispatcher("https://localhost:16677", "")

	// agent_list takes no args
	toolCallContains(t, d, "agent_list", map[string]any{}, "not initialized")
	// agent_stop takes name
	toolCallContains(t, d, "agent_stop", map[string]any{"name": "x"}, "not initialized")
	// agent_run takes name and message
	toolCallContains(t, d, "agent_run", map[string]any{"name": "x", "message": "hi"}, "not initialized")
}

func TestAgentStop_FoundAndStopped(t *testing.T) {
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

	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}})
	SetDeps(&Deps{Agents: mgr})

	d := NewDispatcher("https://localhost:16677", "")

	// stop known agent
	out, err := d.CallTool(context.Background(), "agent_stop", map[string]any{"name": "bot"})
	if err != nil {
		t.Fatalf("agent_stop: %v", err)
	}
	if !strings.Contains(out, "stopped") {
		t.Fatalf("expected 'stopped' in output, got %q", out)
	}

	// stop unknown agent
	toolCallContains(t, d, "agent_stop", map[string]any{"name": "unknown-agent"}, "not found")
}

func TestAgentGet_Tool(t *testing.T) {
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
		Agents: []config.AgentConfig{{Name: "alpha", Model: "claude-3"}},
	}
	if err := config.Save("", cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	mgr := agent.NewManager(nil)
	mgr.Reconcile(cfg)
	SetDeps(&Deps{Agents: mgr})

	d := NewDispatcher("https://localhost:16677", "")

	// agent_get for known agent
	out, err := d.CallTool(context.Background(), "agent_get", map[string]any{"name": "alpha"})
	if err != nil {
		t.Fatalf("agent_get: %v", err)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "claude-3") {
		t.Fatalf("expected agent details in output, got %q", out)
	}

	// agent_get for unknown agent
	toolCallContains(t, d, "agent_get", map[string]any{"name": "unknown"}, "not found")
}

func TestAgentAdd_Update_Delete(t *testing.T) {
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
		Agents: []config.AgentConfig{{Name: "existing", Model: "x"}},
	}
	if err := config.Save("", cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	mgr := agent.NewManager(nil)
	mgr.Reconcile(cfg)
	SetDeps(&Deps{Agents: mgr})

	d := NewDispatcher("https://localhost:16677", "")

	// agent_add with empty name
	toolCallContains(t, d, "agent_add", map[string]any{"name": ""}, "required")

	// agent_add new agent
	out, err := d.CallTool(context.Background(), "agent_add", map[string]any{
		"name":  "newbot",
		"model": "claude-3",
	})
	if err != nil {
		t.Fatalf("agent_add: %v", err)
	}
	if !strings.Contains(out, "added") {
		t.Fatalf("expected 'added' in output, got %q", out)
	}

	// agent_add duplicate
	toolCallContains(t, d, "agent_add", map[string]any{"name": "newbot", "model": "x"}, "already exists")

	// agent_update with empty name
	toolCallContains(t, d, "agent_update", map[string]any{"name": ""}, "required")

	// agent_update known agent
	out, err = d.CallTool(context.Background(), "agent_update", map[string]any{
		"name":  "newbot",
		"model": "claude-4",
	})
	if err != nil {
		t.Fatalf("agent_update: %v", err)
	}
	if !strings.Contains(out, "updated") {
		t.Fatalf("expected 'updated' in output, got %q", out)
	}

	// agent_update unknown agent
	toolCallContains(t, d, "agent_update", map[string]any{"name": "ghost"}, "not found")

	// agent_delete unknown agent
	toolCallContains(t, d, "agent_delete", map[string]any{"name": "ghost"}, "not found")

	// agent_delete known agent
	out, err = d.CallTool(context.Background(), "agent_delete", map[string]any{"name": "newbot"})
	if err != nil {
		t.Fatalf("agent_delete: %v", err)
	}
	if !strings.Contains(out, "deleted") {
		t.Fatalf("expected 'deleted' in output, got %q", out)
	}
}

// ── Browser tools (nil deps) ──────────────────────────────────────────────────

func TestBrowserTools_NilDepsExtended(t *testing.T) {
	old := GetDeps()
	SetDeps(&Deps{Browser: nil})
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	d := NewDispatcher("https://localhost:16677", "")

	toolCallContains(t, d, "browser_tabs", nil, "browser manager not initialized")
	toolCallContains(t, d, "browser_eval", map[string]any{"tab_id": "x", "javascript": "1"}, "browser manager not initialized")
	toolCallContains(t, d, "browser_screenshot", map[string]any{"tab_id": "x"}, "browser manager not initialized")
}

func TestBrowserTools_WithManager_TabsAndErrors(t *testing.T) {
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	mgr := browser.NewManager("", 0, t.TempDir(), false)
	SetDeps(&Deps{Browser: mgr})

	d := NewDispatcher("https://localhost:16677", "")

	// browser_tabs succeeds (returns empty list when no Chrome running)
	out, err := d.CallTool(context.Background(), "browser_tabs", nil)
	if err != nil {
		t.Fatalf("browser_tabs: %v", err)
	}
	// Should return JSON array or error
	_ = out

	// browser_eval missing tab_id
	toolCallContains(t, d, "browser_eval", map[string]any{"javascript": "1"}, "tab_id")
	// browser_screenshot missing tab_id
	toolCallContains(t, d, "browser_screenshot", map[string]any{}, "tab_id")
}

// ── Job tools ─────────────────────────────────────────────────────────────────

func TestJobLogsTool(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	mgr := agent.NewManager(nil)
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}}
	mgr.Reconcile(cfg)
	s, err := scheduler.New(mgr, 1)
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	t.Cleanup(s.Stop)
	SetDeps(&Deps{Agents: mgr, Scheduler: s})

	d := NewDispatcher("https://localhost:16677", "")

	// job_logs for a non-existent job
	toolCallContains(t, d, "job_logs", map[string]any{"id": "nonexistent-job-xyz"}, "not found")

	// Create a real job, then fetch its logs
	job, err := s.Queue().Enqueue("bot/daily", "agent_bot", "bot", "run", "", 1, "", "")
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	// Write output to the job
	job.Output = "hello from job"
	if err := store.WriteJSON(store.JobPath(job.AgentID, job.ID), job); err != nil {
		t.Fatalf("write job: %v", err)
	}

	out, err := d.CallTool(context.Background(), "job_logs", map[string]any{"id": job.ID})
	if err != nil {
		t.Fatalf("job_logs: %v", err)
	}
	if !strings.Contains(out, "hello from job") {
		t.Fatalf("expected job output in logs, got %q", out)
	}
}

func TestJobLogsNoOutput(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	mgr := agent.NewManager(nil)
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}}
	mgr.Reconcile(cfg)
	s, err := scheduler.New(mgr, 1)
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	t.Cleanup(s.Stop)
	SetDeps(&Deps{Agents: mgr, Scheduler: s})

	d := NewDispatcher("https://localhost:16677", "")

	job, err := s.Queue().Enqueue("bot/daily", "agent_bot", "bot", "run", "", 1, "", "")
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}

	out, err := d.CallTool(context.Background(), "job_logs", map[string]any{"id": job.ID})
	if err != nil {
		t.Fatalf("job_logs no output: %v", err)
	}
	if !strings.Contains(out, "no output") {
		t.Fatalf("expected 'no output' message, got %q", out)
	}
}

func TestJobQueryWithDateRange(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	mgr := agent.NewManager(nil)
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}}
	mgr.Reconcile(cfg)
	s, err := scheduler.New(mgr, 1)
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	t.Cleanup(s.Stop)
	SetDeps(&Deps{Agents: mgr, Scheduler: s})

	// Enqueue a job so there is something to query.
	_, err = s.Queue().Enqueue("bot/daily", "agent_bot", "bot", "run", "", 1, "", "")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	d := NewDispatcher("https://localhost:16677", "")

	// Query with start/end date filter
	start := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
	end := time.Now().Add(24 * time.Hour).Format("2006-01-02")
	out, err := d.CallTool(context.Background(), "job_query", map[string]any{"start": start, "end": end})
	if err != nil {
		t.Fatalf("job_query with date range: %v", err)
	}
	// Should return an array containing our job
	if strings.TrimSpace(out) == "null" {
		t.Fatalf("expected non-null result when job is in range, got %q", out)
	}
}

func TestJobListQueryRunNow_NilScheduler(t *testing.T) {
	old := GetDeps()
	SetDeps(&Deps{Scheduler: nil})
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	d := NewDispatcher("https://localhost:16677", "")
	toolCallContains(t, d, "job_query", map[string]any{}, "scheduler not initialized")
}

// ── Auth tools ────────────────────────────────────────────────────────────────

func TestAuthLoginAnthropicComplete_NoPendingPKCE(t *testing.T) {
	d, _ := setupMCPWithAuth(t)

	// Ensure no lingering PKCE state from other tests
	auth.LoadPendingPKCE("anthropic") // consume any stored state

	// Calling complete without first calling auth_login_anthropic should error
	toolCallContains(t, d, "auth_login_anthropic_complete", map[string]any{"code": "someCode"}, "no pending")
}

func TestAuthLoginAnthropicComplete_EmptyCode(t *testing.T) {
	d, _ := setupMCPWithAuth(t)

	toolCallContains(t, d, "auth_login_anthropic_complete", map[string]any{"code": ""}, "required")
}

func TestAuthGetUnknownKey(t *testing.T) {
	d, _ := setupMCPWithAuth(t)

	// auth_get for a key that doesn't exist should return an error result
	toolCallContains(t, d, "auth_get", map[string]any{"name": "nonexistent:key"}, "")
}

func TestAuthDeleteUnknownKey(t *testing.T) {
	d, _ := setupMCPWithAuth(t)

	// auth_delete for unknown key — may succeed (no-op) or error
	// Either way, the tool should not panic
	_, _ = d.CallTool(context.Background(), "auth_delete", map[string]any{"name": "nonexistent:key"})
}

// ── reconcileAgents ───────────────────────────────────────────────────────────

func TestReconcileAgents_NilDeps(t *testing.T) {
	old := GetDeps()
	SetDeps(nil)
	t.Cleanup(func() { SetDeps(old) })

	// Should not panic
	reconcileAgents()
}

func TestReconcileAgents_NilAgentManager(t *testing.T) {
	old := GetDeps()
	SetDeps(&Deps{Agents: nil})
	t.Cleanup(func() { SetDeps(old) })

	// Should not panic
	reconcileAgents()
}

func TestReconcileAgents_WithManager(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}}
	if err := config.Save("", cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	old := GetDeps()
	mgr := agent.NewManager(nil)
	mgr.Reconcile(cfg)
	SetDeps(&Deps{Agents: mgr})
	t.Cleanup(func() { SetDeps(old) })

	// Should not panic and should reconcile successfully
	reconcileAgents()
}

// ── generatedTaskName ─────────────────────────────────────────────────────────

func TestGeneratedTaskName(t *testing.T) {
	name := generatedTaskName("Send the daily report")
	if name == "" {
		t.Fatal("expected non-empty name")
	}
	if strings.Contains(name, " ") {
		t.Fatalf("generated task name should not contain spaces, got %q", name)
	}
	// Should contain slug characters and a unix timestamp suffix
	if !strings.Contains(name, "-") {
		t.Fatalf("expected dash separator in generated name, got %q", name)
	}

	// Empty / symbol-only prompt falls back to "scheduled"
	fallbackName := generatedTaskName("!!!???")
	if !strings.HasPrefix(fallbackName, "scheduled") {
		t.Fatalf("expected 'scheduled' prefix for empty prompt, got %q", fallbackName)
	}

	// Long prompt gets truncated at 24 characters (base part)
	longName := generatedTaskName("averylongnamewithoutspacessoitdoesnotget truncated early")
	base := strings.Split(longName, "-")
	if len(base[0]) > 24 {
		t.Fatalf("expected base to be at most 24 chars, got %d", len(base[0]))
	}
}

// ── cdpPortOrDefault ─────────────────────────────────────────────────────────

func TestCDPPortOrDefault(t *testing.T) {
	if got := cdpPortOrDefault(0); got != config.DefaultCDPPort {
		t.Fatalf("expected default port %d, got %d", config.DefaultCDPPort, got)
	}
	if got := cdpPortOrDefault(9999); got != 9999 {
		t.Fatalf("expected 9999, got %d", got)
	}
}

// ── agent_tools.go ────────────────────────────────────────────────────────────

func TestNewAgentToolClient(t *testing.T) {
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	SetDeps(&Deps{Agents: agent.NewManager(nil)})

	tc, err := NewAgentToolClient(context.Background())
	if err != nil {
		t.Fatalf("NewAgentToolClient: %v", err)
	}
	defer tc.Close() //nolint:errcheck

	// ListTools returns a non-empty list
	tools, err := tc.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("expected non-empty tool list from agent tool client")
	}

	// CallToolText returns text
	out, err := tc.CallToolText(context.Background(), "ping", map[string]any{})
	if err != nil {
		t.Fatalf("CallToolText: %v", err)
	}
	if out != "pong" {
		t.Fatalf("expected pong, got %q", out)
	}
}

// ── config_get / config_save / config_validate tools ─────────────────────────

func TestConfigGetSaveValidateTools(t *testing.T) {
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

	// config_get returns current config
	out, err := d.CallTool(context.Background(), "config_get", map[string]any{})
	if err != nil {
		t.Fatalf("config_get: %v", err)
	}
	if !strings.Contains(out, "bot") {
		t.Fatalf("expected agent name in config_get output, got %q", out)
	}

	// config_validate returns issues array
	out, err = d.CallTool(context.Background(), "config_validate", map[string]any{})
	if err != nil {
		t.Fatalf("config_validate: %v", err)
	}
	// Should return a JSON array
	if !strings.HasPrefix(strings.TrimSpace(out), "[") && strings.TrimSpace(out) != "null" {
		t.Fatalf("expected JSON array from config_validate, got %q", out)
	}

	// config_save with valid JSON config
	cfgJSON := `{"agents":[{"name":"bot","model":"anthropic/claude-3-haiku"}]}`
	out, err = d.CallTool(context.Background(), "config_save", map[string]any{"config": cfgJSON})
	if err != nil {
		t.Fatalf("config_save: %v", err)
	}
	if !strings.Contains(out, "saved") {
		t.Fatalf("expected 'saved' in config_save output, got %q", out)
	}

	// config_save with invalid JSON
	toolCallContains(t, d, "config_save", map[string]any{"config": "not-json"}, "invalid config")
}

// ── session_create tool ───────────────────────────────────────────────────────

func TestSessionCreateTool(t *testing.T) {
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

	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}})
	SetDeps(&Deps{Agents: mgr})

	d := NewDispatcher("https://localhost:16677", "")

	// Missing agent name
	toolCallContains(t, d, "session_create", map[string]any{}, "required")

	// Valid create
	out, err := d.CallTool(context.Background(), "session_create", map[string]any{"agent": "bot"})
	if err != nil {
		t.Fatalf("session_create: %v", err)
	}
	if !strings.Contains(out, "agent_bot") {
		t.Fatalf("expected agent_id in session_create output, got %q", out)
	}
}

// ── session_stop with agent param ─────────────────────────────────────────────

func TestSessionStop_ByAgentParam(t *testing.T) {
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

	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}})
	SetDeps(&Deps{Agents: mgr})

	d := NewDispatcher("https://localhost:16677", "")

	// Stop by agent name (no active session, returns "no active work")
	out, err := d.CallTool(context.Background(), "session_stop", map[string]any{
		"agent":   "bot",
		"session": "main",
	})
	if err != nil {
		t.Fatalf("session_stop by agent: %v", err)
	}
	if !strings.Contains(out, "no active") {
		t.Fatalf("expected 'no active' message, got %q", out)
	}
}

// ── local file data URL + channel_send_file error paths ──────────────────────

func TestLocalFileToDataURL_Errors(t *testing.T) {
	// File not found
	_, err := localFileToDataURL("/nonexistent/path/file.png")
	if err == nil {
		t.Fatal("expected error for missing file")
	}

	// Empty file
	emptyFile := filepath.Join(t.TempDir(), "empty.txt")
	if err := os.WriteFile(emptyFile, []byte{}, 0o600); err != nil {
		t.Fatalf("write empty file: %v", err)
	}
	_, err = localFileToDataURL(emptyFile)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected 'empty' error, got %v", err)
	}
}

func TestChannelSendFile_NoSession(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })
	SetDeps(&Deps{})

	d := NewDispatcher("https://localhost:16677", "")

	// Missing file_path
	toolCallContains(t, d, "channel_send_file", map[string]any{}, "required")

	// No session context
	toolCallContains(t, d, "channel_send_file", map[string]any{"file_path": "/tmp/foo.png"}, "no active channel session")
}

// ── skills_list tool ──────────────────────────────────────────────────────────

func TestSkillsListTool(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	cfg := &config.Config{}
	if err := config.Save("", cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })
	SetDeps(&Deps{})

	d := NewDispatcher("https://localhost:16677", "")
	out, err := d.CallTool(context.Background(), "skills_list", map[string]any{})
	if err != nil {
		t.Fatalf("skills_list: %v", err)
	}
	// Should return a JSON array (possibly empty)
	if !strings.HasPrefix(strings.TrimSpace(out), "[") && strings.TrimSpace(out) != "null" {
		t.Fatalf("expected JSON array from skills_list, got %q", out)
	}
}

// ── memory tools (nil deps) ───────────────────────────────────────────────────

func TestMemoryToolsQuery_NilDeps(t *testing.T) {
	old := GetDeps()
	SetDeps(&Deps{Memory: nil})
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	d := NewDispatcher("https://localhost:16677", "")

	// Just verify the query tool errors properly (the others are covered in mcp_test.go)
	toolCallContains(t, d, "memory_search", map[string]any{"agent": "bot", "query": "test"}, "not initialized")
}

// ── memory_search (no query / empty result) ───────────────────────────────────

func TestMemorySearch_EmptyQuery(t *testing.T) {
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
	SetDeps(&Deps{Memory: mem})

	d := NewDispatcher("https://localhost:16677", "")

	// memory_search with empty query returns all notes (empty)
	out, err := d.CallTool(context.Background(), "memory_search", map[string]any{"agent": "bot", "query": ""})
	if err != nil {
		t.Fatalf("memory_search empty query: %v", err)
	}
	_ = out // may be empty string, that's fine
}

// ── task_schedule error paths ─────────────────────────────────────────────────

func TestTaskSchedule_NilScheduler(t *testing.T) {
	old := GetDeps()
	SetDeps(&Deps{Scheduler: nil})
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	d := NewDispatcher("https://localhost:16677", "")
	toolCallContains(t, d, "task_schedule", map[string]any{"agent": "bot", "prompt": "hi"}, "scheduler not initialized")
	toolCallContains(t, d, "task_list", map[string]any{}, "scheduler not initialized")
	toolCallContains(t, d, "task_run", map[string]any{"name": "bot/daily"}, "scheduler not initialized")
	toolCallContains(t, d, "task_stop", map[string]any{}, "scheduler not initialized")
}

func TestTaskSchedule_InvalidInDuration(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	toolCallContains(t, d, "task_schedule", map[string]any{
		"agent":  "bot",
		"prompt": "run this",
		"in":     "not-a-duration",
	}, "invalid duration")
}

func TestTaskSchedule_InvalidCronSchedule(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	toolCallContains(t, d, "task_schedule", map[string]any{
		"agent":    "bot",
		"prompt":   "run this",
		"schedule": "not a cron",
	}, "invalid schedule")
}

func TestTaskSchedule_AgentNotFound(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	toolCallContains(t, d, "task_schedule", map[string]any{
		"agent":  "nonexistent",
		"prompt": "run this",
	}, "not found")
}

func TestTaskSchedule_PromptRequired(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	toolCallContains(t, d, "task_schedule", map[string]any{"agent": "bot", "prompt": ""}, "required")
}

func TestTaskSchedule_AgentRequired(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)
	toolCallContains(t, d, "task_schedule", map[string]any{"agent": "", "prompt": "run"}, "required")
}

func TestTaskSchedule_ImmediateTask(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)

	out, err := d.CallTool(context.Background(), "task_schedule", map[string]any{
		"agent":  "bot",
		"prompt": "run now",
	})
	if err != nil {
		t.Fatalf("task_schedule immediate: %v", err)
	}
	if !strings.Contains(out, "immediately") {
		t.Fatalf("expected 'immediately' in output, got %q", out)
	}
}

func TestTaskSchedule_CapturesReplySessionContext(t *testing.T) {
	d, s := setupDispatcherWithScheduler(t)

	const (
		agentID   = "agent_bot"
		sessionID = "agent_bot-signal:+15551234567"
	)
	ctx := agent.WithSessionAgentID(agent.WithSessionID(context.Background(), sessionID), agentID)

	if _, err := d.CallTool(ctx, "task_schedule", map[string]any{
		"agent":  "bot",
		"prompt": "run now",
		"in":     "30s",
	}); err != nil {
		t.Fatalf("task_schedule with session context: %v", err)
	}

	jobs, err := s.Queue().List("")
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected one queued job, got %d", len(jobs))
	}
	if jobs[0].ReplyAgentID != agentID || jobs[0].ReplySessionID != sessionID {
		t.Fatalf("expected reply context %q/%q, got %q/%q", agentID, sessionID, jobs[0].ReplyAgentID, jobs[0].ReplySessionID)
	}
}

func TestTaskSchedule_WithDelay(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)

	out, err := d.CallTool(context.Background(), "task_schedule", map[string]any{
		"agent":  "bot",
		"prompt": "run later",
		"in":     "5m",
	})
	if err != nil {
		t.Fatalf("task_schedule with delay: %v", err)
	}
	if !strings.Contains(out, "job ID") {
		t.Fatalf("expected job ID in output, got %q", out)
	}
}

func TestTaskStopNoJobs(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)

	out, err := d.CallTool(context.Background(), "task_stop", map[string]any{})
	if err != nil {
		t.Fatalf("task_stop no jobs: %v", err)
	}
	if !strings.Contains(out, "no pending") {
		t.Fatalf("expected 'no pending' message, got %q", out)
	}
}

func TestTaskStopByNameNoMatch(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)

	out, err := d.CallTool(context.Background(), "task_stop", map[string]any{"name": "nonexistent-task"})
	if err != nil {
		t.Fatalf("task_stop no match: %v", err)
	}
	if !strings.Contains(out, "no pending") {
		t.Fatalf("expected 'no pending' message, got %q", out)
	}
}

// ── validateTaskSchedule ──────────────────────────────────────────────────────

func TestValidateTaskSchedule(t *testing.T) {
	if err := validateTaskSchedule("0 0 10 * * *"); err != nil {
		t.Fatalf("valid schedule should not error: %v", err)
	}
	if err := validateTaskSchedule("not-a-cron"); err == nil {
		t.Fatal("expected error for invalid cron")
	}
}

// ── agent_run error paths ─────────────────────────────────────────────────────

func TestAgentRun_AgentNotFound(t *testing.T) {
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}})
	SetDeps(&Deps{Agents: mgr})

	d := NewDispatcher("https://localhost:16677", "")
	toolCallContains(t, d, "agent_run", map[string]any{"name": "unknown", "message": "hello"}, "not found")
}

// ── job_run_now with nil scheduler ────────────────────────────────────────────

func TestJobRunNow_NilScheduler(t *testing.T) {
	old := GetDeps()
	SetDeps(&Deps{Scheduler: nil})
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	d := NewDispatcher("https://localhost:16677", "")
	toolCallContains(t, d, "job_run_now", map[string]any{"id": "x"}, "scheduler not initialized")
}

// ── usage_query with RFC3339 timestamps ──────────────────────────────────────

func TestUsageQueryTool_RFC3339Filter(t *testing.T) {
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
	SetDeps(&Deps{Agents: agent.NewManager(nil)})

	rec := domain.UsageRecord{
		Timestamp:    time.Now().Add(-1 * time.Hour),
		AgentName:    "rfc-bot",
		Model:        "claude-3",
		Provider:     "anthropic",
		InputTokens:  10,
		OutputTokens: 5,
	}
	usagePath := store.UsagePath()
	if err := store.AppendJSONL(usagePath, rec); err != nil {
		t.Fatalf("write usage: %v", err)
	}

	d := NewDispatcher("https://localhost:16677", "")

	// RFC3339 timestamps
	start := time.Now().Add(-2 * time.Hour).Format(time.RFC3339)
	end := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	out, err := d.CallTool(context.Background(), "usage_query", map[string]any{"start": start, "end": end})
	if err != nil {
		t.Fatalf("usage_query RFC3339: %v", err)
	}
	if !strings.Contains(out, "rfc-bot") {
		t.Fatalf("expected rfc-bot in RFC3339 filtered query, got %q", out)
	}
}
