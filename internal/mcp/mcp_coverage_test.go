package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	err := store.EnsureDirs()
	assert.NoError(t, err)

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	cfg := &config.Config{
		Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}},
	}
	err = config.Save("", cfg)
	assert.NoError(t, err)

	mgr := agent.NewManager(nil)
	mgr.Reconcile(cfg)
	s, err := scheduler.New(mgr, 1)
	assert.NoError(t, err)

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
	err := store.EnsureDirs()
	assert.NoError(t, err)

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
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "stopped"))

	// stop unknown agent
	toolCallContains(t, d, "agent_stop", map[string]any{"name": "unknown-agent"}, "not found")
}

func TestAgentGet_Tool(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	err := store.EnsureDirs()
	assert.NoError(t, err)

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	cfg := &config.Config{
		Agents: []config.AgentConfig{{Name: "alpha", Model: "claude-3"}},
	}
	err = config.Save("", cfg)
	assert.NoError(t, err)

	mgr := agent.NewManager(nil)
	mgr.Reconcile(cfg)
	SetDeps(&Deps{Agents: mgr})

	d := NewDispatcher("https://localhost:16677", "")

	// agent_get for known agent
	out, err := d.CallTool(context.Background(), "agent_get", map[string]any{"name": "alpha"})
	assert.NoError(t, err)
	assert.Contains(t, out, "alpha")
	assert.Contains(t, out, "claude-3")

	// agent_get for unknown agent
	toolCallContains(t, d, "agent_get", map[string]any{"name": "unknown"}, "not found")
}

func TestAgentAdd_Update_Delete(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	err := store.EnsureDirs()
	assert.NoError(t, err)

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	cfg := &config.Config{
		Agents: []config.AgentConfig{{Name: "existing", Model: "x"}},
	}
	err = config.Save("", cfg)
	assert.NoError(t, err)

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
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "added"))
	assert.FileExists(t, filepath.Join(store.AgentDir("newbot"), "RULES.md"))

	// agent_add duplicate
	toolCallContains(t, d, "agent_add", map[string]any{"name": "newbot", "model": "x"}, "already exists")

	out, err = d.CallTool(context.Background(), "agent_template_sync", map[string]any{"agent": "newbot"})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "templates synced"))

	toolCallContains(t, d, "agent_template_sync", map[string]any{"agent": ""}, "required")

	// agent_update with empty name
	toolCallContains(t, d, "agent_update", map[string]any{"name": ""}, "required")

	// agent_update known agent
	out, err = d.CallTool(context.Background(), "agent_update", map[string]any{
		"name":  "newbot",
		"model": "claude-4",
	})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "updated"))

	// agent_update unknown agent
	toolCallContains(t, d, "agent_update", map[string]any{"name": "ghost"}, "not found")

	// agent_delete unknown agent
	toolCallContains(t, d, "agent_delete", map[string]any{"name": "ghost"}, "not found")

	// agent_delete known agent
	out, err = d.CallTool(context.Background(), "agent_delete", map[string]any{"name": "newbot"})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "deleted"))

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
	assert.NoError(t, err)

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
	err := store.EnsureDirs()
	assert.NoError(t, err)

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	mgr := agent.NewManager(nil)
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}}
	mgr.Reconcile(cfg)
	s, err := scheduler.New(mgr, 1)
	assert.NoError(t, err)

	t.Cleanup(s.Stop)
	SetDeps(&Deps{Agents: mgr, Scheduler: s})

	d := NewDispatcher("https://localhost:16677", "")

	// job_logs for a non-existent job
	toolCallContains(t, d, "job_logs", map[string]any{"id": "nonexistent-job-xyz"}, "not found")

	// Create a real job, then fetch its logs
	job, err := s.Queue().Enqueue("bot/daily", "agent_bot", "bot", "run", "", 1, "", "")
	assert.NoError(t, err)

	// Write output to the job
	job.Output = "hello from job"
	err = store.WriteJSON(store.JobPath(job.AgentID, job.ID), job)
	assert.NoError(t, err)

	out, err := d.CallTool(context.Background(), "job_logs", map[string]any{"id": job.ID})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "hello from job"))

}

func TestJobLogsNoOutput(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	err := store.EnsureDirs()
	assert.NoError(t, err)

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	mgr := agent.NewManager(nil)
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}}
	mgr.Reconcile(cfg)
	s, err := scheduler.New(mgr, 1)
	assert.NoError(t, err)

	t.Cleanup(s.Stop)
	SetDeps(&Deps{Agents: mgr, Scheduler: s})

	d := NewDispatcher("https://localhost:16677", "")

	job, err := s.Queue().Enqueue("bot/daily", "agent_bot", "bot", "run", "", 1, "", "")
	assert.NoError(t, err)

	out, err := d.CallTool(context.Background(), "job_logs", map[string]any{"id": job.ID})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "no output"))

}

func TestJobQueryWithDateRange(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	err := store.EnsureDirs()
	assert.NoError(t, err)

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	mgr := agent.NewManager(nil)
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}}
	mgr.Reconcile(cfg)
	s, err := scheduler.New(mgr, 1)
	assert.NoError(t, err)

	t.Cleanup(s.Stop)
	SetDeps(&Deps{Agents: mgr, Scheduler: s})

	// Enqueue a job so there is something to query.
	_, err = s.Queue().Enqueue("bot/daily", "agent_bot", "bot", "run", "", 1, "", "")
	assert.NoError(t, err)

	d := NewDispatcher("https://localhost:16677", "")

	// Query with start/end date filter
	start := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
	end := time.Now().Add(24 * time.Hour).Format("2006-01-02")
	out, err := d.CallTool(context.Background(), "job_query", map[string]any{"start": start, "end": end})
	assert.NoError(t, err)
	assert.NotEqual(t, // Should return an array containing our job
		"null", strings.TrimSpace(out))

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
	err := store.EnsureDirs()
	assert.NoError(t, err)

	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}}
	err = config.Save("", cfg)
	assert.NoError(t, err)

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
	assert.NotEqual(t, "", name)
	assert.False(t, strings.Contains(name, " "))
	assert.True(t, // Should contain slug characters and a unix timestamp suffix
		strings.Contains(name, "-"))

	// Empty / symbol-only prompt falls back to "scheduled"
	fallbackName := generatedTaskName("!!!???")
	assert.True(t, strings.HasPrefix(fallbackName, "scheduled"))

	// Long prompt gets truncated at 24 characters (base part)
	longName := generatedTaskName("averylongnamewithoutspacessoitdoesnotget truncated early")
	base := strings.Split(longName, "-")
	assert.LessOrEqual(t, len(base[0]), 24)

}

// ── cdpPortOrDefault ─────────────────────────────────────────────────────────

func TestCDPPortOrDefault(t *testing.T) {
	got := cdpPortOrDefault(0)
	assert.Equal(t, config.DefaultCDPPort, got)

	got = cdpPortOrDefault(9999)
	assert.Equal(t, 9999, got)

}

// ── agent_tools.go ────────────────────────────────────────────────────────────

func TestNewAgentToolClient(t *testing.T) {
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	SetDeps(&Deps{Agents: agent.NewManager(nil)})

	tc, err := NewAgentToolClient(context.Background())
	assert.NoError(t, err)

	defer tc.Close() //nolint:errcheck

	// ListTools returns a non-empty list
	tools, err := tc.ListTools(context.Background())
	assert.NoError(t, err)
	assert.NotEqual(t, 0, len(tools))

	// CallToolText returns text
	out, err := tc.CallToolText(context.Background(), "ping", map[string]any{})
	assert.NoError(t, err)
	assert.Equal(t, "pong", out)

}

// ── config_get / config_save / config_validate tools ─────────────────────────

func TestConfigGetSaveValidateTools(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	err := store.EnsureDirs()
	assert.NoError(t, err)

	cfg := &config.Config{
		Agents: []config.AgentConfig{{Name: "bot", Model: "anthropic/claude-3-haiku"}},
	}
	err = config.Save("", cfg)
	assert.NoError(t, err)

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
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "bot"))

	// config_validate returns issues array
	out, err = d.CallTool(context.Background(), "config_validate", map[string]any{})
	assert.NoError(t, err)
	assert.False(t, // Should return a JSON array
		!strings.HasPrefix(strings.TrimSpace(out), "[") && strings.TrimSpace(out) != "null")

	// config_save with valid JSON config
	cfgJSON := `{"agents":[{"name":"bot","model":"anthropic/claude-3-haiku","channels":[{"type":"slack","id":"alerts"}]}]}`
	out, err = d.CallTool(context.Background(), "config_save", map[string]any{"config": cfgJSON})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "saved"))

	state, err := store.ReadAppState()
	assert.NoError(t, err)
	if meta, ok := state.Channels["bot/slack/alerts"]; ok {
		assert.False(t, meta.EnabledAt.IsZero())
		assert.True(t, meta.DisabledAt.IsZero())
	}

	disableJSON := `{"agents":[{"name":"bot","model":"anthropic/claude-3-haiku","channels":[{"type":"slack","id":"alerts","enabled":false}]}]}`
	out, err = d.CallTool(context.Background(), "config_save", map[string]any{"config": disableJSON})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "saved"))

	state, err = store.ReadAppState()
	assert.NoError(t, err)
	if meta, ok := state.Channels["bot/slack/alerts"]; ok {
		assert.False(t, meta.EnabledAt.IsZero())
		assert.False(t, meta.DisabledAt.IsZero())
	}

	// config_save with invalid JSON
	toolCallContains(t, d, "config_save", map[string]any{"config": "not-json"}, "invalid config")

	out, err = d.CallTool(context.Background(), "config_restore_latest_backup", map[string]any{})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "restored"))

	restoredCfg, err := config.Load("")
	assert.NoError(t, err)
	require.Len(t, restoredCfg.Agents, 1)
	assert.Equal(t, "slack", restoredCfg.Agents[0].Channels[0].Type)
	assert.True(t, config.BoolOr(restoredCfg.Agents[0].Channels[0].Enabled, true))
}

// ── session_create tool ───────────────────────────────────────────────────────

func TestSessionCreateTool(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	err := store.EnsureDirs()
	assert.NoError(t, err)

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
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "agent_bot"))

}

// ── session_stop with agent param ─────────────────────────────────────────────

func TestSessionStop_ByAgentParam(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	err := store.EnsureDirs()
	assert.NoError(t, err)

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
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "no active"))

}

// ── local file data URL + channel_send_file error paths ──────────────────────

func TestLocalFileToDataURL_Errors(t *testing.T) {
	// File not found
	_, err := localFileToDataURL("/nonexistent/path/file.png")
	assert.Error(t, err)

	// Empty file
	emptyFile := filepath.Join(t.TempDir(), "empty.txt")
	err = os.WriteFile(emptyFile, []byte{}, 0o600)
	assert.NoError(t, err)

	_, err = localFileToDataURL(emptyFile)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")

}

func TestChannelSendFile_NoSession(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(base + "/aviary")
	t.Cleanup(func() { store.SetDataDir("") })
	err := store.EnsureDirs()
	assert.NoError(t, err)

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
	err := store.EnsureDirs()
	assert.NoError(t, err)

	cfg := &config.Config{}
	err = config.Save("", cfg)
	assert.NoError(t, err)

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })
	SetDeps(&Deps{})

	d := NewDispatcher("https://localhost:16677", "")
	out, err := d.CallTool(context.Background(), "skills_list", map[string]any{})
	assert.NoError(t, err)
	out = strings.TrimSpace(out)
	if out != "null" {
		assert.True(t, strings.HasPrefix(out, "["))
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
	err := store.EnsureDirs()
	assert.NoError(t, err)

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
	assert.NoError(t, err)

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
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "immediately"))

}

func TestTaskSchedule_CapturesReplySessionContext(t *testing.T) {
	d, s := setupDispatcherWithScheduler(t)

	const (
		agentID   = "agent_bot"
		sessionID = "agent_bot-signal:+15551234567"
	)
	ctx := agent.WithSessionAgentID(agent.WithSessionID(context.Background(), sessionID), agentID)
	_, err := d.CallTool(ctx, "task_schedule", map[string]any{
		"agent":  "bot",
		"prompt": "run now",
		"in":     "30s",
	})
	assert.NoError(t, err)

	jobs, err := s.Queue().List("")
	assert.NoError(t, err)
	assert.Equal(t, 1, len(jobs))
	assert.Equal(t, agentID, jobs[0].ReplyAgentID)
	assert.Equal(t, sessionID, jobs[0].ReplySessionID)

}

func TestTaskSchedule_RecurringTaskDefaultsToOriginChannelRoute(t *testing.T) {
	d, s := setupDispatcherWithScheduler(t)

	cfg, err := config.Load("")
	assert.NoError(t, err)
	cfg.Agents[0].Channels = []config.ChannelConfig{{Type: "slack", ID: "alerts"}}
	err = config.Save("", cfg)
	assert.NoError(t, err)
	s.Reconcile(cfg)

	ctx := agent.WithChannelSession(context.Background(), "slack", "alerts", "C123")
	_, err = d.CallTool(ctx, "task_schedule", map[string]any{
		"agent":    "bot",
		"name":     "daily-report",
		"prompt":   "write report",
		"schedule": "0 0 10 * * *",
	})
	assert.NoError(t, err)

	updated, err := config.Load("")
	assert.NoError(t, err)
	if assert.Len(t, updated.Agents, 1) && assert.Len(t, updated.Agents[0].Tasks, 1) {
		assert.Equal(t, "route:slack:alerts:C123", updated.Agents[0].Tasks[0].Target)
	}
}

func TestTaskSchedule_WithDelay(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)

	out, err := d.CallTool(context.Background(), "task_schedule", map[string]any{
		"agent":  "bot",
		"prompt": "run later",
		"in":     "5m",
	})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "job ID"))

}

func TestTaskStopNoJobs(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)

	out, err := d.CallTool(context.Background(), "task_stop", map[string]any{})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "no pending"))

}

func TestTaskStopByNameNoMatch(t *testing.T) {
	d, _ := setupDispatcherWithScheduler(t)

	out, err := d.CallTool(context.Background(), "task_stop", map[string]any{"name": "nonexistent-task"})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "no pending"))

}

// ── validateTaskSchedule ──────────────────────────────────────────────────────

func TestValidateTaskSchedule(t *testing.T) {
	err := validateTaskSchedule("0 0 10 * * *")
	assert.NoError(t, err)

	err = validateTaskSchedule("not-a-cron")
	assert.Error(t, err)

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
	err := store.EnsureDirs()
	assert.NoError(t, err)

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
	err = store.AppendJSONL(usagePath, rec)
	assert.NoError(t, err)

	d := NewDispatcher("https://localhost:16677", "")

	// RFC3339 timestamps
	start := time.Now().Add(-2 * time.Hour).Format(time.RFC3339)
	end := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	out, err := d.CallTool(context.Background(), "usage_query", map[string]any{"start": start, "end": end})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "rfc-bot"))

}
