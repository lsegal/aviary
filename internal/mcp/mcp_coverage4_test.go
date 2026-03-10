package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/scheduler"
	"github.com/lsegal/aviary/internal/store"
)

// ── session_stop via context session ID ──────────────────────────────────────

func TestSessionStop_ViaContextSessionID(t *testing.T) {
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

	// Create a session and inject its ID into context
	agentID := "agent_bot"
	sess, err := agent.NewSessionManager().GetOrCreateNamed(agentID, "main")
	if err != nil {
		t.Fatalf("get or create session: %v", err)
	}

	// Inject the session ID into the context (exercises the ctx path in session_stop)
	ctx := agent.WithSessionID(context.Background(), sess.ID)

	// Use the Dispatcher's CallTool which passes the context through properly
	d := NewDispatcher("https://localhost:16677", "")
	out, err := d.CallTool(ctx, "session_stop", map[string]any{})
	if err != nil {
		t.Fatalf("session_stop via context: %v", err)
	}
	if !strings.Contains(out, "no active") {
		t.Fatalf("expected 'no active' in session_stop via context, got %q", out)
	}
}

// ── job_query date filter exclusion paths ────────────────────────────────────

func TestJobQuery_DateFilterExclusion(t *testing.T) {
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

	// Enqueue a job
	job, err := s.Queue().Enqueue("bot/daily", "agent_bot", "bot", "run", "", 1, "", "")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	d := NewDispatcher("https://localhost:16677", "")

	// Query with a start date in the future — should exclude the job
	futureStart := time.Now().Add(24 * time.Hour).Format("2006-01-02")
	out, err := d.CallTool(context.Background(), "job_query", map[string]any{"start": futureStart})
	if err != nil {
		t.Fatalf("job_query future start: %v", err)
	}
	if strings.Contains(out, job.ID) {
		t.Fatalf("job should be excluded by future start date, got %q", out)
	}

	// Query with an end date in the past — should exclude the job
	pastEnd := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
	out, err = d.CallTool(context.Background(), "job_query", map[string]any{"end": pastEnd})
	if err != nil {
		t.Fatalf("job_query past end: %v", err)
	}
	if strings.Contains(out, job.ID) {
		t.Fatalf("job should be excluded by past end date, got %q", out)
	}

	// Query with status filter that doesn't match
	out, err = d.CallTool(context.Background(), "job_query", map[string]any{"status": "completed"})
	if err != nil {
		t.Fatalf("job_query status filter: %v", err)
	}
	if strings.Contains(out, job.ID) {
		t.Fatalf("pending job should be excluded by status=completed filter, got %q", out)
	}

	// Query with agent filter that doesn't match
	out, err = d.CallTool(context.Background(), "job_query", map[string]any{"agent": "other-bot"})
	if err != nil {
		t.Fatalf("job_query agent filter: %v", err)
	}
	if strings.Contains(out, job.ID) {
		t.Fatalf("job should be excluded by agent=other-bot filter, got %q", out)
	}
}

// ── task_schedule: agent in agents but not in config ─────────────────────────

func TestTaskSchedule_AgentNotInConfig(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	// Save a config without the "ghost" agent
	cfg := &config.Config{
		Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}},
	}
	if err := config.Save("", cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	// Create a manager that has "ghost" but config doesn't
	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{
		{Name: "bot", Model: "test/x"},
		{Name: "ghost", Model: "test/x"},
	}})
	s, err := scheduler.New(mgr, 1)
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	t.Cleanup(s.Stop)
	SetDeps(&Deps{Agents: mgr, Scheduler: s})

	d := NewDispatcher("https://localhost:16677", "")

	// Schedule with "ghost" agent + cron schedule — agent is in manager but not in saved config
	toolCallContains(t, d, "task_schedule", map[string]any{
		"agent":    "ghost",
		"prompt":   "run this",
		"schedule": "0 0 10 * * *",
	}, "not found in config")
}

// ── task_schedule recurring update ───────────────────────────────────────────

func TestTaskSchedule_RecurringUpdate(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	cfg := &config.Config{
		Agents: []config.AgentConfig{{
			Name:  "bot",
			Model: "test/x",
			Tasks: []config.TaskConfig{{
				Name:     "daily",
				Schedule: "0 0 10 * * *",
				Prompt:   "original prompt",
			}},
		}},
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
	s, err := scheduler.New(mgr, 1)
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	t.Cleanup(s.Stop)
	s.Reconcile(cfg)
	SetDeps(&Deps{Agents: mgr, Scheduler: s})

	d := NewDispatcher("https://localhost:16677", "")

	// Update existing task
	out, err := d.CallTool(context.Background(), "task_schedule", map[string]any{
		"agent":    "bot",
		"name":     "daily",
		"prompt":   "updated prompt",
		"schedule": "0 0 11 * * *",
	})
	if err != nil {
		t.Fatalf("task_schedule update: %v", err)
	}
	if !strings.Contains(out, "updated") {
		t.Fatalf("expected 'updated' in response, got %q", out)
	}
}

// ── job_query with matching jobs ──────────────────────────────────────────────

func TestJobQuery_MatchingJobs(t *testing.T) {
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

	// Enqueue a job
	job, err := s.Queue().Enqueue("bot/daily", "agent_bot", "bot", "run", "", 1, "", "")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	d := NewDispatcher("https://localhost:16677", "")

	// Query with matching status
	out, err := d.CallTool(context.Background(), "job_query", map[string]any{"status": string(domain.JobStatusPending)})
	if err != nil {
		t.Fatalf("job_query matching status: %v", err)
	}
	if !strings.Contains(out, job.ID) {
		t.Fatalf("expected job in pending status query, got %q", out)
	}

	// Query with matching agent
	out, err = d.CallTool(context.Background(), "job_query", map[string]any{"agent": "bot"})
	if err != nil {
		t.Fatalf("job_query matching agent: %v", err)
	}
	if !strings.Contains(out, job.ID) {
		t.Fatalf("expected job in agent query, got %q", out)
	}
}

// ── task_stop with job_id ─────────────────────────────────────────────────────

func TestTaskStop_ByJobID(t *testing.T) {
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

	job, err := s.Queue().Enqueue("bot/task", "agent_bot", "bot", "run", "", 1, "", "")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	d := NewDispatcher("https://localhost:16677", "")

	// Stop by job_id
	out, err := d.CallTool(context.Background(), "task_stop", map[string]any{"job_id": job.ID})
	if err != nil {
		t.Fatalf("task_stop by job_id: %v", err)
	}
	if !strings.Contains(out, "stopped") {
		t.Fatalf("expected 'stopped' in task_stop output, got %q", out)
	}
}

// ── task_stop all jobs (multiple stopped) ────────────────────────────────────

func TestTaskStop_AllJobs(t *testing.T) {
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

	// Enqueue two jobs
	_, err = s.Queue().Enqueue("bot/task1", "agent_bot", "bot", "run 1", "", 1, "", "")
	if err != nil {
		t.Fatalf("enqueue 1: %v", err)
	}
	_, err = s.Queue().Enqueue("bot/task2", "agent_bot", "bot", "run 2", "", 1, "", "")
	if err != nil {
		t.Fatalf("enqueue 2: %v", err)
	}

	d := NewDispatcher("https://localhost:16677", "")

	// Stop all jobs (no name or job_id)
	out, err := d.CallTool(context.Background(), "task_stop", map[string]any{})
	if err != nil {
		t.Fatalf("task_stop all: %v", err)
	}
	if !strings.Contains(out, "stopped") {
		t.Fatalf("expected 'stopped' in task_stop all output, got %q", out)
	}
}

// ── agent_rules_get with existing rules ──────────────────────────────────────

func TestAgentRulesGet_ExistingRules(t *testing.T) {
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

	// Set rules first
	_, err := d.CallTool(context.Background(), "agent_rules_set", map[string]any{
		"agent":   "bot",
		"content": "be helpful and kind",
	})
	if err != nil {
		t.Fatalf("agent_rules_set: %v", err)
	}

	// Get rules - should return the content
	out, err := d.CallTool(context.Background(), "agent_rules_get", map[string]any{"name": "bot"})
	if err != nil {
		t.Fatalf("agent_rules_get: %v", err)
	}
	if !strings.Contains(out, "helpful") {
		t.Fatalf("expected 'helpful' in rules, got %q", out)
	}
}

// ── auth_get for a key not found (vs. error) ─────────────────────────────────

func TestAuthGet_Found(t *testing.T) {
	d, _ := setupMCPWithAuth(t)

	// Set and then get
	_, err := d.CallTool(context.Background(), "auth_set", map[string]any{"name": "test:key", "value": "abcdefgh"})
	if err != nil {
		t.Fatalf("auth_set: %v", err)
	}

	out, err := d.CallTool(context.Background(), "auth_get", map[string]any{"name": "test:key"})
	if err != nil {
		t.Fatalf("auth_get found: %v", err)
	}
	if !strings.Contains(out, `"set": true`) {
		t.Fatalf("expected set:true, got %q", out)
	}
	if strings.Contains(out, "abcdefgh") {
		t.Fatalf("should not expose raw value, got %q", out)
	}
	// Should show first 4 chars: "abcd***..."
	if !strings.Contains(out, "abcd") {
		t.Fatalf("expected first 4 chars 'abcd' in preview, got %q", out)
	}
}

// ── session_messages with actual session data ─────────────────────────────────

func TestSessionMessages_WithData(t *testing.T) {
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

	// Create a session and list its messages
	agentID := "agent_bot"
	sess, err := agent.NewSessionManager().GetOrCreateNamed(agentID, "main")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Messages should be empty initially
	out, err := d.CallTool(context.Background(), "session_messages", map[string]any{"session_id": sess.ID})
	if err != nil {
		t.Fatalf("session_messages: %v", err)
	}
	// Should be a valid JSON array
	if strings.TrimSpace(out) != "[]" && strings.TrimSpace(out) != "null" {
		t.Fatalf("expected empty array or null, got %q", out)
	}
}

// ── server tools: config_validate with provider models ───────────────────────

func TestConfigValidate_WithProviders(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	cfg := &config.Config{
		Agents: []config.AgentConfig{{
			Name:  "bot",
			Model: "anthropic/claude-3-haiku",
		}},
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

	out, err := d.CallTool(context.Background(), "config_validate", map[string]any{})
	if err != nil {
		t.Fatalf("config_validate: %v", err)
	}
	// Returns array of issues
	if !strings.HasPrefix(strings.TrimSpace(out), "[") && strings.TrimSpace(out) != "null" {
		t.Fatalf("expected JSON array from config_validate, got %q", out)
	}
}
