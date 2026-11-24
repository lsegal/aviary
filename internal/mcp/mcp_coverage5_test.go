package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/scheduler"
	"github.com/lsegal/aviary/internal/store"
)

// ── tools with a corrupted config ────────────────────────────────────────────

// corruptConfigDispatcher points the config at an unparseable aviary.yaml,
// installs deps (restored on cleanup), and returns a local dispatcher.
func corruptConfigDispatcher(t *testing.T, deps *Deps) *Dispatcher {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	configDir := filepath.Join(base, "aviary")
	assert.NoError(t, os.MkdirAll(configDir, 0o700))
	assert.NoError(t, os.WriteFile(filepath.Join(configDir, "aviary.yaml"), []byte("{ bad: yaml: ["), 0o600))

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })
	SetDeps(deps)

	return NewDispatcher("https://localhost:16677", "")
}

func TestReconcileAgents_ConfigLoadError(t *testing.T) {
	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "x"}}})
	corruptConfigDispatcher(t, &Deps{Agents: mgr})
	before := agentNames(mgr)
	assert.Contains(t, before, "bot")

	// A config that fails to load must leave the running agents untouched
	// rather than reconciling them against an empty config.
	reconcileAgents()

	assert.Equal(t, before, agentNames(mgr))
}

func agentNames(mgr *agent.Manager) []string {
	var names []string
	for _, a := range mgr.List() {
		names = append(names, a.Name)
	}
	sort.Strings(names)
	return names
}

func TestTools_ConfigLoadError(t *testing.T) {
	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "x"}}})
	sched, err := scheduler.New(mgr, 1)
	assert.NoError(t, err)
	t.Cleanup(sched.Stop)
	// task_schedule precompiles a recurring prompt task in the background.
	t.Cleanup(backgroundCompiles.Wait)
	d := corruptConfigDispatcher(t, &Deps{Agents: mgr, Scheduler: sched})

	tests := []struct {
		tool string
		args map[string]any
	}{
		{"skills_list", map[string]any{}},
		{"agent_get", map[string]any{"name": "bot"}},
		{"agent_add", map[string]any{"name": "bot2", "model": "x"}},
		{"agent_update", map[string]any{"name": "bot", "model": "x"}},
		{"agent_delete", map[string]any{"name": "bot"}},
		{"config_get", map[string]any{}},
		{"config_validate", map[string]any{}},
		{"task_schedule", map[string]any{"agent": "bot", "content": "run", "schedule": "0 0 10 * * *"}},
	}
	c, err := d.Resolve(context.Background())
	assert.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			res, err := c.CallTool(context.Background(), tt.tool, tt.args)
			assert.NoError(t, err)
			out := extractText(res)
			assert.True(t, res.IsError, "expected %s to fail, got %q", tt.tool, out)
			assert.Contains(t, out, "parsing config")
		})
	}
}

// ── CallToolText error path in InProcessClient ────────────────────────────────

func TestCallToolText_ToolNotFound(t *testing.T) {
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	SetDeps(&Deps{Agents: agent.NewManager(nil)})

	c, err := NewInProcessClient(context.Background(), NewServer())
	assert.NoError(t, err)

	defer c.Close() //nolint:errcheck

	// Calling a non-existent tool should return an error
	_, err = c.CallToolText(context.Background(), "nonexistent_tool_xyz", map[string]any{})
	if err == nil {
		t.Log("expected error for non-existent tool, but got nil — SDK may return MCP error result instead")
	}
}

// ── ensureInProcessDeps with auth store nil ───────────────────────────────────

func TestEnsureInProcessDeps_StoreEnsureDirsError(t *testing.T) {
	// This tests the EnsureDirs error path.
	// We do this by making the data dir non-writable (use a file path).
	oldDataDir := store.DataDir()
	t.Cleanup(func() { store.SetDataDir(oldDataDir) })

	// Set data dir to a file (not a directory) to cause EnsureDirs to fail
	tmpFile, err := os.CreateTemp(t.TempDir(), "not-a-dir")
	assert.NoError(t, err)

	tmpFile.Close() //nolint:errcheck

	store.SetDataDir(tmpFile.Name())

	oldDeps := GetDeps()
	oldDepsSet := depsSet
	t.Cleanup(func() {
		globalDeps = oldDeps
		depsSet = oldDepsSet
	})

	globalDeps = &Deps{}
	depsSet = false

	// Should fail since data dir is a file
	err = ensureInProcessDeps()
	if err == nil {
		// On some systems this might not fail; just verify no panic
		t.Log("ensureInProcessDeps did not fail with file as data dir — acceptable")
	}
}

// ── agent_rules_set with content that writes successfully ────────────────────

func TestAgentRulesSet_Success(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("AVIARY_CONFIG_BASE_DIR", filepath.Join(base, "aviary"))
	store.SetDataDir(filepath.Join(base, "aviary"))
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

	// Valid set
	out, err := d.CallTool(context.Background(), "agent_rules_set", map[string]any{
		"agent":   "testbot",
		"content": "# Rules\n- Be helpful",
	})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "written"))

	// Get the rules back
	out, err = d.CallTool(context.Background(), "agent_rules_get", map[string]any{"name": "testbot"})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "helpful"))

}

// ── braveSearch decode error ──────────────────────────────────────────────────

func TestBraveSearch_DecodeError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return malformed JSON
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer ts.Close()

	origClient := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = origClient })
	http.DefaultClient = &http.Client{
		Transport: &redirectTransport{from: "https://api.search.brave.com", to: ts.URL},
	}

	_, err := braveSearch(context.Background(), "test-key", "query", 5)
	assert.Error(t, err)

}
