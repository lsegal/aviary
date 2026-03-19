package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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

	// Create a session and inject its ID into context
	agentID := "bot"
	sess, err := agent.NewSessionManager().GetOrCreateNamed(agentID, "main")
	assert.NoError(t, err)

	// Inject the session ID into the context (exercises the ctx path in session_stop)
	ctx := agent.WithSessionAgentID(agent.WithSessionID(context.Background(), sess.ID), agentID)

	// Use the Dispatcher's CallTool which passes the context through properly
	d := NewDispatcher("https://localhost:16677", "")
	out, err := d.CallTool(ctx, "session_stop", map[string]any{})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "no active"))

}

// ── job_query date filter exclusion paths ────────────────────────────────────

func TestJobQuery_DateFilterExclusion(t *testing.T) {
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

	// Enqueue a job
	job, err := s.Queue().Enqueue("bot/daily", "bot", "run", "", 1, "", "")
	assert.NoError(t, err)

	d := NewDispatcher("https://localhost:16677", "")

	// Query with a start date in the future — should exclude the job
	futureStart := time.Now().UTC().Add(48 * time.Hour).Format("2006-01-02")
	out, err := d.CallTool(context.Background(), "job_query", map[string]any{"start": futureStart})
	assert.NoError(t, err)
	assert.False(t, strings.Contains(out, job.ID))

	// Query with an end date in the past — should exclude the job
	pastEnd := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02")
	out, err = d.CallTool(context.Background(), "job_query", map[string]any{"end": pastEnd})
	assert.NoError(t, err)
	assert.False(t, strings.Contains(out, job.ID))

	// Query with status filter that doesn't match
	out, err = d.CallTool(context.Background(), "job_query", map[string]any{"status": "completed"})
	assert.NoError(t, err)
	assert.False(t, strings.Contains(out, job.ID))

	// Query with agent filter that doesn't match
	out, err = d.CallTool(context.Background(), "job_query", map[string]any{"agent": "other-bot"})
	assert.NoError(t, err)
	assert.False(t, strings.Contains(out, job.ID))

	// Query with id filter that doesn't match
	out, err = d.CallTool(context.Background(), "job_query", map[string]any{"id": "job_missing"})
	assert.NoError(t, err)
	assert.False(t, strings.Contains(out, job.ID))

}

// ── task_schedule: agent in agents but not in config ─────────────────────────

func TestTaskSchedule_AgentNotInConfig(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	err := store.EnsureDirs()
	assert.NoError(t, err)

	// Save a config without the "ghost" agent
	cfg := &config.Config{
		Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}},
	}
	err = config.Save("", cfg)
	assert.NoError(t, err)

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
	assert.NoError(t, err)

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
	err := store.EnsureDirs()
	assert.NoError(t, err)

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
	err = config.Save("", cfg)
	assert.NoError(t, err)

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	mgr := agent.NewManager(nil)
	mgr.Reconcile(cfg)
	s, err := scheduler.New(mgr, 1)
	assert.NoError(t, err)

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
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "updated"))

}

// ── job_query with matching jobs ──────────────────────────────────────────────

func TestJobQuery_MatchingJobs(t *testing.T) {
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

	// Enqueue a job
	job, err := s.Queue().Enqueue("bot/daily", "bot", "run", "", 1, "", "")
	assert.NoError(t, err)

	d := NewDispatcher("https://localhost:16677", "")

	// Query with matching status
	out, err := d.CallTool(context.Background(), "job_query", map[string]any{"status": string(domain.JobStatusPending)})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, job.ID))

	// Query with matching agent
	out, err = d.CallTool(context.Background(), "job_query", map[string]any{"agent": "bot"})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, job.ID))

	// Query with matching id
	out, err = d.CallTool(context.Background(), "job_query", map[string]any{"id": job.ID})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, job.ID))

}

// ── task_stop with job_id ─────────────────────────────────────────────────────

func TestTaskStop_ByJobID(t *testing.T) {
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

	job, err := s.Queue().Enqueue("bot/task", "bot", "run", "", 1, "", "")
	assert.NoError(t, err)

	d := NewDispatcher("https://localhost:16677", "")

	// Stop by job_id
	out, err := d.CallTool(context.Background(), "task_stop", map[string]any{"job_id": job.ID})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "stopped"))

}

// ── task_stop all jobs (multiple stopped) ────────────────────────────────────

func TestTaskStop_AllJobs(t *testing.T) {
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

	// Enqueue two jobs
	_, err = s.Queue().Enqueue("bot/task1", "bot", "run 1", "", 1, "", "")
	assert.NoError(t, err)

	_, err = s.Queue().Enqueue("bot/task2", "bot", "run 2", "", 1, "", "")
	assert.NoError(t, err)

	d := NewDispatcher("https://localhost:16677", "")

	// Stop all jobs (no name or job_id)
	out, err := d.CallTool(context.Background(), "task_stop", map[string]any{})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "stopped"))

}

// ── agent_rules_get with existing rules ──────────────────────────────────────

func TestAgentRulesGet_ExistingRules(t *testing.T) {
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

	// Set rules first
	_, err = d.CallTool(context.Background(), "agent_rules_set", map[string]any{
		"agent":   "bot",
		"content": "be helpful and kind",
	})
	assert.NoError(t, err)

	// Get rules - should return the content
	out, err := d.CallTool(context.Background(), "agent_rules_get", map[string]any{"name": "bot"})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "helpful"))

}

// ── auth_get for a key not found (vs. error) ─────────────────────────────────

func TestAuthGet_Found(t *testing.T) {
	d, _ := setupMCPWithAuth(t)

	// Set and then get
	_, err := d.CallTool(context.Background(), "auth_set", map[string]any{"name": "test:key", "value": "abcdefgh"})
	assert.NoError(t, err)

	out, err := d.CallTool(context.Background(), "auth_get", map[string]any{"name": "test:key"})
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, `"set": true`))
	assert.False(t, strings.Contains(out, "abcdefgh"))
	assert.True(t, // Should show first 4 chars: "abcd***..."
		strings.Contains(out, "abcd"))

}

// ── session_messages with actual session data ─────────────────────────────────

func TestSessionMessages_WithData(t *testing.T) {
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

	// Create a session and list its messages
	agentID := "bot"
	sess, err := agent.NewSessionManager().GetOrCreateNamed(agentID, "main")
	assert.NoError(t, err)

	// Messages should be empty initially
	out, err := d.CallTool(context.Background(), "session_messages", map[string]any{"agent": "bot", "session_id": sess.ID})
	assert.NoError(t, err)
	assert.False(t, // Should be a valid JSON array
		strings.TrimSpace(out) != "[]" && strings.TrimSpace(out) != "null")

}

func TestSessionHistory_ReversePaging(t *testing.T) {
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
	agentID := "bot"
	sess, err := agent.NewSessionManager().GetOrCreateNamed(agentID, "main")
	assert.NoError(t, err)

	appendMessage := func(id string, role domain.MessageRole, content string, sender *domain.MessageSender) {
		err = store.AppendJSONL(store.SessionPath(agentID, sess.ID), domain.Message{
			ID:        id,
			Role:      role,
			Sender:    sender,
			Content:   content,
			Timestamp: time.Now().UTC(),
		})
		assert.NoError(t, err)
	}

	appendMessage("m1", domain.MessageRoleUser, "first", domain.NewMessageSender("u1", "Alice", true))
	appendMessage("m2", domain.MessageRoleAssistant, "second", nil)
	appendMessage("m3", domain.MessageRoleUser, "third", domain.NewMessageSender("u2", "Bob", false))

	raw, err := d.CallTool(context.Background(), "session_history", map[string]any{
		"agent":      "bot",
		"session_id": sess.ID,
		"order":      "desc",
		"limit":      2,
		"skip":       1,
	})
	assert.NoError(t, err)

	var messages []struct {
		ID      string `json:"id"`
		Content string `json:"content"`
		Sender  *struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Participant bool   `json:"participant"`
		} `json:"sender"`
	}
	err = json.Unmarshal([]byte(raw), &messages)
	assert.NoError(t, err)
	assert.Len(t, messages, 2)
	assert.Equal(t, "m2", messages[0].ID)
	assert.Equal(t, "second", messages[0].Content)
	assert.Nil(t, messages[0].Sender)
	assert.Equal(t, "m1", messages[1].ID)
	assert.Equal(t, "first", messages[1].Content)
	assert.NotNil(t, messages[1].Sender)
	assert.Equal(t, "u1", messages[1].Sender.ID)
	assert.Equal(t, "Alice", messages[1].Sender.Name)
	assert.True(t, messages[1].Sender.Participant)
}

// ── server tools: config_validate with provider models ───────────────────────

func TestConfigValidate_WithProviders(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	err := store.EnsureDirs()
	assert.NoError(t, err)

	cfg := &config.Config{
		Agents: []config.AgentConfig{{
			Name:  "bot",
			Model: "anthropic/claude-3-haiku",
		}},
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

	out, err := d.CallTool(context.Background(), "config_validate", map[string]any{})
	assert.NoError(t, err)
	assert.False(t, // Returns array of issues
		!strings.HasPrefix(strings.TrimSpace(out), "[") && strings.TrimSpace(out) != "null")

}
