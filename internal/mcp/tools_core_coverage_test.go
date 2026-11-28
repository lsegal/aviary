package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/saferestart"
	"github.com/lsegal/aviary/internal/store"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

// tcSetup saves cfg as the running config in a fresh data directory, wires
// an agent manager and scheduler for it, and returns a dispatcher.
func tcSetup(t *testing.T, cfg *config.Config) *Dispatcher {
	t.Helper()
	setupSchedulerDeps(t, cfg)
	return NewDispatcher("https://localhost:16677", "")
}

// tcCall calls tool through d and returns its text, which holds the error
// message when the tool fails.
func tcCall(t *testing.T, d *Dispatcher, tool string, args map[string]any) string {
	t.Helper()
	return tcCallCtx(context.Background(), t, d, tool, args)
}

// tcCallCtx returns a tool's text, including the message of a result the
// tool marked as an error.
func tcCallCtx(ctx context.Context, t *testing.T, d *Dispatcher, tool string, args map[string]any) string {
	t.Helper()
	return toolText(t, d, ctx, tool, args)
}

// tcCorruptConfig replaces aviary.yaml with text that does not parse.
func tcCorruptConfig(t *testing.T) {
	t.Helper()
	require.NoError(t, os.WriteFile(config.DefaultPath(), []byte("agents: [unclosed\n"), 0o600))
}

// tcModel is an OpenAI-compatible chat endpoint whose behavior is set by
// mode: "text" streams a reply, "fail" rejects the request, "block" holds
// the request open until the client goes away, and "tool" defers to a
// scriptedToolModel.
type tcModel struct {
	mu      sync.Mutex
	mode    string
	started chan struct{}
	release chan struct{}
	tool    scriptedToolModel
}

func (m *tcModel) setMode(mode string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mode = mode
}

func (m *tcModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	mode := m.mode
	m.mu.Unlock()
	switch mode {
	case "fail":
		http.Error(w, "model exploded", http.StatusBadRequest)
	case "block":
		select {
		case m.started <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-m.release:
		}
	case "tool":
		m.tool.ServeHTTP(w, r)
	default:
		w.Header().Set("Content-Type", "text/event-stream")
		for _, part := range []string{"hello ", "world"} {
			data, _ := json.Marshal(map[string]any{
				"id": "c", "object": "chat.completion.chunk", "created": 0, "model": "m",
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": part}}},
			})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		}
		data, _ := json.Marshal(map[string]any{
			"id": "c", "object": "chat.completion.chunk", "created": 0, "model": "m",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}
}

// tcSetupModelAgent wires the agent "bot" to a tcModel endpoint.
func tcSetupModelAgent(t *testing.T) (*Dispatcher, *tcModel) {
	t.Helper()
	cfg := &config.Config{Agents: []config.AgentConfig{{
		Name:        "bot",
		Model:       "vllm/test",
		Permissions: &config.PermissionsConfig{Preset: config.PermissionsPresetFull},
	}}}
	s := setupSchedulerDeps(t, cfg)
	agent.SetToolClientFactory(NewAgentToolClient)
	t.Cleanup(func() { agent.SetToolClientFactory(nil) })

	model := &tcModel{mode: "text", started: make(chan struct{}, 1), release: make(chan struct{})}
	srv := httptest.NewServer(model)
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(model.release) })
	factory := llm.NewFactory(func(string) (string, error) { return "", nil }).
		WithProviderOptionsResolver(func(string) (llm.ProviderOptions, bool) {
			return llm.ProviderOptions{BaseURI: srv.URL + "/v1"}, true
		})
	mgr := agent.NewManager(factory)
	t.Cleanup(mgr.Stop)
	mgr.Reconcile(cfg)
	SetDeps(&Deps{Agents: mgr, Scheduler: s})
	return NewDispatcher("https://localhost:16677", ""), model
}

// tcWaitStarted waits for the model to receive a blocking request.
func tcWaitStarted(t *testing.T, m *tcModel) {
	t.Helper()
	select {
	case <-m.started:
	case <-time.After(15 * time.Second):
		t.Fatal("model never received the request")
	}
}

// ── agent_run ─────────────────────────────────────────────────────────────────

func TestToolsCoreAgentRunStreamsTextAndToolProgress(t *testing.T) {
	d, model := tcSetupModelAgent(t)

	var progress []string
	out, isError, err := d.CallToolWithProgress(context.Background(), "agent_run", map[string]any{
		"name": "bot", "message": "say hi",
	}, func(message string) bool {
		progress = append(progress, message)
		return false
	})
	require.NoError(t, err)
	require.False(t, isError, out)
	assert.Equal(t, "hello world", out)
	assert.Equal(t, []string{"hello ", "world"}, progress, "each text delta is a progress message")

	model.setMode("tool")
	require.NoError(t, os.WriteFile(store.AgentRulesPath("bot"), []byte("rules-marker"), 0o600))
	model.tool.set("agent_rules_get", map[string]any{"name": "bot"})
	progress = nil
	out, isError, err = d.CallToolWithProgress(context.Background(), "agent_run", map[string]any{
		"name": "bot", "session": "tools", "message": "read your rules", "include_tool_progress": true,
	}, func(message string) bool {
		progress = append(progress, message)
		return false
	})
	require.NoError(t, err)
	require.False(t, isError, out)
	assert.Contains(t, out, "rules-marker")
	var toolMessages int
	for _, message := range progress {
		if strings.HasPrefix(message, "[tool]") {
			toolMessages++
			assert.Contains(t, message, "agent_rules_get")
		}
	}
	assert.Positive(t, toolMessages, "tool calls are reported with include_tool_progress: %v", progress)
}

func TestToolsCoreAgentRunModelErrorIsToolError(t *testing.T) {
	d, model := tcSetupModelAgent(t)
	model.setMode("fail")

	c, err := d.Resolve(context.Background())
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck
	res, err := c.CallTool(context.Background(), "agent_run", map[string]any{"name": "bot", "message": "hi"})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Contains(t, extractText(res), "400 Bad Request")
}

func TestToolsCoreAgentRunSessionIDResumesThatSession(t *testing.T) {
	d, _ := tcSetupModelAgent(t)
	sess, err := agent.NewSessionManager().Create("bot")
	require.NoError(t, err)

	assert.Contains(t, tcCall(t, d, "agent_run", map[string]any{"session_id": sess.ID, "message": "hi"}),
		"name is required when session_id is provided")
	assert.Contains(t, tcCall(t, d, "agent_run", map[string]any{"name": "bot", "session_id": "sess_missing", "message": "hi"}),
		"not found")

	out := tcCall(t, d, "agent_run", map[string]any{"name": "bot", "session_id": sess.ID, "message": "hi"})
	assert.Equal(t, "hello world", out)
	msgs, err := store.ReadJSONL[domain.Message](store.SessionPath("bot", sess.ID))
	require.NoError(t, err)
	var contents []string
	for _, msg := range msgs {
		if msg.Role != "" {
			contents = append(contents, msg.Content)
		}
	}
	assert.Equal(t, []string{"hi", "hello world"}, contents, "the turn is saved in the given session")
}

func TestToolsCoreStopCommandsCancelARunningTurn(t *testing.T) {
	d, model := tcSetupModelAgent(t)
	model.setMode("block")
	sess, err := agent.NewSessionManager().GetOrCreateNamed("bot", "work")
	require.NoError(t, err)

	// With nothing running, a stop message is answered without the model.
	assert.Contains(t, tcCall(t, d, "agent_run", map[string]any{"name": "bot", "session": "work", "message": "/stop"}),
		"has no active work")

	run := func() <-chan string {
		done := make(chan string, 1)
		go func() {
			out, err := d.CallTool(context.Background(), "agent_run", map[string]any{"name": "bot", "session": "work", "message": "long job"})
			if err != nil {
				out = "transport error: " + err.Error()
			}
			done <- out
		}()
		return done
	}
	waitDone := func(done <-chan string) string {
		select {
		case out := <-done:
			return out
		case <-time.After(15 * time.Second):
			t.Fatal("agent_run did not return after the stop")
			return ""
		}
	}

	done := run()
	tcWaitStarted(t, model)
	require.Eventually(t, func() bool { return agent.IsSessionProcessing("bot", sess.ID) }, 10*time.Second, 10*time.Millisecond)

	var turn agent.ActiveTurn
	require.NoError(t, json.Unmarshal([]byte(tcCall(t, d, "session_turn", map[string]any{"agent": "bot", "session_id": sess.ID})), &turn))
	assert.True(t, turn.Processing)
	assert.Equal(t, sess.ID, turn.SessionID)

	assert.Contains(t, tcCall(t, d, "agent_run", map[string]any{"name": "bot", "session": "work", "message": "Stop"}),
		fmt.Sprintf("stopped session %q", sess.ID))
	out := waitDone(done)
	assert.NotContains(t, out, "transport error")
	assert.NotContains(t, out, "context canceled", "a stopped run is not reported as a failure")

	done = run()
	tcWaitStarted(t, model)
	require.Eventually(t, func() bool { return agent.IsSessionProcessing("bot", sess.ID) }, 10*time.Second, 10*time.Millisecond)
	assert.Contains(t, tcCall(t, d, "session_stop", map[string]any{"agent": "bot", "session_id": sess.ID}),
		fmt.Sprintf("stopped 1 active run(s) in session %q", sess.ID))
	waitDone(done)
	assert.False(t, agent.IsSessionProcessing("bot", sess.ID))
}

// ── agent_stop ────────────────────────────────────────────────────────────────

func TestToolsCoreAgentStopDeletesSessionCheckpoints(t *testing.T) {
	d := tcSetup(t, botConfig())
	sess, err := agent.NewSessionManager().GetOrCreateNamed("bot", "work")
	require.NoError(t, err)

	// Recovery of interrupted runs, which drops stale checkpoints, has
	// finished before these are written.
	GetDeps().Agents.WaitRecovery()
	dir := store.CheckpointDir("bot")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "nested.json"), 0o700))
	require.NoError(t, store.WriteJSON(filepath.Join(dir, "mine.json"), agent.RunCheckpoint{AgentName: "bot", SessionID: sess.ID}))
	require.NoError(t, store.WriteJSON(filepath.Join(dir, "other.json"), agent.RunCheckpoint{AgentName: "bot", SessionID: "sess_other"}))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{not json"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("keep"), 0o600))

	out := tcCall(t, d, "agent_stop", map[string]any{"name": "bot", "session": "work"})
	assert.Equal(t, fmt.Sprintf("agent %q stopped (session %s)", "bot", sess.ID), out)
	assert.NoFileExists(t, filepath.Join(dir, "mine.json"))
	assert.NoFileExists(t, filepath.Join(dir, "corrupt.json"), "an unreadable checkpoint is removed")
	assert.FileExists(t, filepath.Join(dir, "other.json"), "another session's checkpoint is kept")
	assert.FileExists(t, filepath.Join(dir, "notes.txt"))
	assert.DirExists(t, filepath.Join(dir, "nested.json"))

	out = tcCall(t, d, "agent_stop", map[string]any{"name": "bot"})
	assert.Equal(t, `agent "bot" stopped`, out)
	assert.NoFileExists(t, filepath.Join(dir, "other.json"), "stopping the whole agent drops every checkpoint")
	assert.FileExists(t, filepath.Join(dir, "notes.txt"))
	assert.DirExists(t, filepath.Join(dir, "nested.json"))
}

// ── agent_run_script ──────────────────────────────────────────────────────────

func TestToolsCoreAgentRunScriptSessions(t *testing.T) {
	d := tcSetup(t, botConfig())

	assert.Contains(t, tcCall(t, d, "agent_run_script", map[string]any{"script": "print(1)"}), "agent is required")

	// Without an agent arg, the agent comes from the calling session.
	ctx := agent.WithSessionAgentID(context.Background(), "bot")
	ctx = agent.WithTaskID(ctx, "bot/daily")
	ctx = agent.WithJobID(ctx, "job_1")
	out := tcCallCtx(ctx, t, d, "agent_run_script", map[string]any{"script": "print(environment.agent_id)"})
	assert.Equal(t, "bot", strings.TrimSpace(out))

	sess, err := agent.NewSessionManager().Create("bot")
	require.NoError(t, err)
	out = tcCall(t, d, "agent_run_script", map[string]any{"agent": "bot", "session_id": sess.ID, "script": "print(environment.session_id)"})
	assert.Equal(t, sess.ID, strings.TrimSpace(out))

	assert.Contains(t, tcCall(t, d, "agent_run_script", map[string]any{"agent": "bot", "session_id": "sess_nope", "script": "print(1)"}),
		"not found")

	c, err := d.Resolve(context.Background())
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck
	res, err := c.CallTool(context.Background(), "agent_run_script", map[string]any{"agent": "bot", "script": `error("boom")`})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Contains(t, extractText(res), "boom")
}

func TestToolsCoreAgentRunScriptNeedsAgentManager(t *testing.T) {
	tcSetup(t, botConfig())
	SetDeps(&Deps{})
	d := NewDispatcher("https://localhost:16677", "")
	assert.Contains(t, tcCall(t, d, "agent_run_script", map[string]any{"agent": "bot", "script": "print(1)"}), "agent manager not initialized")
}

// ── agent config tools ────────────────────────────────────────────────────────

func TestToolsCoreAgentUpdateFallbacksPersist(t *testing.T) {
	d := tcSetup(t, botConfig())

	out := tcCall(t, d, "agent_update", map[string]any{"name": "bot", "fallbacks": []string{"test/y", "test/z"}})
	assert.Equal(t, `agent "bot" updated`, out)
	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Len(t, cfg.Agents, 1)
	assert.Equal(t, "test/x", cfg.Agents[0].Model, "an empty model leaves the model alone")
	assert.Equal(t, []string{"test/y", "test/z"}, cfg.Agents[0].Fallbacks)
}

func TestToolsCoreAgentToolsReportUnreadableConfig(t *testing.T) {
	d := tcSetup(t, botConfig())
	tcCorruptConfig(t)

	for tool, args := range map[string]map[string]any{
		"agent_get":                {"name": "bot"},
		"agent_add":                {"name": "new"},
		"agent_update":             {"name": "bot", "model": "test/y"},
		"agent_delete":             {"name": "bot"},
		"config_get":               {},
		"config_task_move_to_file": {"agent": "bot", "task": "t"},
		"config_task_rename":       {"agent": "bot", "task": "t", "new_name": "u"},
		"task_schedule":            {"agent": "bot", "content": "hi"},
	} {
		assert.Contains(t, tcCall(t, d, tool, args), "parsing config", tool)
	}
}

// ── rules and agent files ─────────────────────────────────────────────────────

func TestToolsCoreAgentRulesRoundTripAndErrors(t *testing.T) {
	d := tcSetup(t, botConfig())

	assert.Equal(t, `RULES.md written for agent "bot"`, tcCall(t, d, "agent_rules_set", map[string]any{"agent": "bot", "content": "Be brief."}))
	assert.Equal(t, "Be brief.", tcCall(t, d, "agent_rules_get", map[string]any{"name": "bot"}))
	assert.Equal(t, "", tcCall(t, d, "agent_rules_get", map[string]any{"name": "norules"}), "an agent without RULES.md has empty rules")
	assert.Contains(t, tcCall(t, d, "agent_rules_set", map[string]any{"agent": " ", "content": "x"}), "agent is required")

	// RULES.md that is a directory can be neither read nor written.
	path := store.AgentRulesPath("odd")
	require.NoError(t, os.MkdirAll(path, 0o700))
	assert.Contains(t, tcCall(t, d, "agent_rules_get", map[string]any{"name": "odd"}), "reading rules")
	assert.Contains(t, tcCall(t, d, "agent_rules_set", map[string]any{"agent": "odd", "content": "x"}), "writing rules")

	// An agent directory blocked by a file cannot be created.
	require.NoError(t, os.WriteFile(store.AgentDir("blocked"), []byte("file"), 0o600))
	assert.Contains(t, tcCall(t, d, "agent_rules_set", map[string]any{"agent": "blocked", "content": "x"}), "creating agent dir")
}

func TestToolsCoreAgentFileToolsNeedAnAgent(t *testing.T) {
	d := tcSetup(t, botConfig())

	for tool, args := range map[string]map[string]any{
		"agent_file_list":   {},
		"agent_file_read":   {"file": "MEMORY.md"},
		"agent_file_write":  {"file": "notes/a.txt", "content": "x"},
		"agent_file_delete": {"file": "notes/a.txt"},
		"agent_file_rename": {"file": "notes/a.txt", "to": "notes/b.txt"},
	} {
		assert.Contains(t, tcCall(t, d, tool, args), tool+" requires an agent session context", tool)
	}

	assert.Contains(t, tcCall(t, d, "agent_file_read", map[string]any{"agent": "bot", "file": "notes/missing.txt"}), "reading agent file")
	assert.Contains(t, tcCall(t, d, "agent_file_write", map[string]any{"agent": "bot", "file": "../escape.txt", "content": "x"}), "writing agent file")
	assert.NoFileExists(t, filepath.Join(filepath.Dir(store.AgentDir("bot")), "escape.txt"))
}

// ── session tools ─────────────────────────────────────────────────────────────

func TestToolsCoreSessionHistoryValidatesArguments(t *testing.T) {
	d := tcSetup(t, botConfig())

	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"agent": " ", "session_id": "main"}, "agent is required"},
		{map[string]any{"agent": "bot", "session_id": ""}, "session_id is required"},
		{map[string]any{"agent": "bot", "session_id": "main", "limit": -1}, "limit must be >= 0"},
		{map[string]any{"agent": "bot", "session_id": "main", "skip": -1}, "skip must be >= 0"},
		{map[string]any{"agent": "bot", "session_id": "main", "order": "sideways"}, "order must be 'asc' or 'desc'"},
	} {
		assert.Contains(t, tcCall(t, d, "session_history", tc.args), tc.want)
	}
	assert.Equal(t, "[]", strings.TrimSpace(tcCall(t, d, "session_messages", map[string]any{"agent": "bot", "session_id": "sess_absent"})))
}

func TestToolsCoreSessionHistoryFiltersByIDAndJoinsResponseIDs(t *testing.T) {
	d := tcSetup(t, botConfig())
	sess, err := agent.NewSessionManager().GetOrCreateNamed("bot", "main")
	require.NoError(t, err)
	path := store.SessionPath("bot", sess.ID)
	now := time.Now().UTC()
	for _, msg := range []domain.Message{
		{ID: "u1", Role: domain.MessageRoleUser, Content: "question", Timestamp: now},
		{ID: "a1", Role: domain.MessageRoleAssistant, Content: "answer", Timestamp: now},
		// A role-less marker links a prompt to its response.
		{ID: "u1", ResponseID: "a1"},
	} {
		require.NoError(t, store.AppendJSONL(path, msg))
	}

	var got []struct {
		ID         string `json:"id"`
		Content    string `json:"content"`
		ResponseID string `json:"response_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(tcCall(t, d, "session_messages", map[string]any{"agent": "bot", "session_id": sess.ID, "id": "u1"})), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "question", got[0].Content)
	assert.Equal(t, "a1", got[0].ResponseID)

	require.NoError(t, json.Unmarshal([]byte(tcCall(t, d, "session_history", map[string]any{"agent": "bot", "session_id": sess.ID, "order": "desc", "limit": 1})), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "answer", got[0].Content, "limit keeps the newest message in desc order")

	assert.Equal(t, "[]", strings.TrimSpace(tcCall(t, d, "session_history", map[string]any{"agent": "bot", "session_id": sess.ID, "skip": 5})),
		"skipping past the end leaves nothing")
}

func TestToolsCoreSessionListOrdersMainFirst(t *testing.T) {
	d := tcSetup(t, botConfig())
	sm := agent.NewSessionManager()
	main, err := sm.GetOrCreateNamed("bot", "main")
	require.NoError(t, err)
	other, err := sm.CreateWithName("bot", "aaa-first")
	require.NoError(t, err)
	// A stray file in the agents directory is not an agent.
	require.NoError(t, os.WriteFile(filepath.Join(store.DataDir(), store.DirAgents, "0-stray.txt"), nil, 0o600))

	assert.Contains(t, tcCall(t, d, "session_list", map[string]any{"agent": ""}), "agent name is required")
	var list []struct {
		ID      string `json:"id"`
		AgentID string `json:"agent_id"`
		Name    string `json:"name"`
	}
	// The agent name matches its directory case-insensitively.
	require.NoError(t, json.Unmarshal([]byte(tcCall(t, d, "session_list", map[string]any{"agent": "BOT"})), &list))
	require.Len(t, list, 2)
	assert.Equal(t, main.ID, list[0].ID, "main is listed first")
	assert.Equal(t, "main", list[0].Name)
	assert.Equal(t, "bot", list[0].AgentID)
	assert.Equal(t, other.ID, list[1].ID)
}

func TestToolsCoreSessionToolsValidateArguments(t *testing.T) {
	d := tcSetup(t, botConfig())

	for _, tool := range []string{"session_remove", "session_clear", "session_rename", "session_turn"} {
		withName := func(args map[string]any) map[string]any {
			if tool == "session_rename" {
				args["name"] = "x"
			}
			return args
		}
		assert.Contains(t, tcCall(t, d, tool, withName(map[string]any{"agent": " ", "session_id": "sess_1"})), "agent is required", tool)
		assert.Contains(t, tcCall(t, d, tool, withName(map[string]any{"agent": "bot", "session_id": " "})), "session_id is required", tool)
	}
	assert.Contains(t, tcCall(t, d, "session_create", map[string]any{"agent": ""}), "agent name is required")
	assert.Contains(t, tcCall(t, d, "session_remove", map[string]any{"agent": "bot", "session_id": "sess_missing"}), "not found")
	assert.Contains(t, tcCall(t, d, "session_rename", map[string]any{"agent": "bot", "session_id": "sess_missing", "name": "x"}), "not found")
	assert.Contains(t, tcCall(t, d, "session_stop", map[string]any{"session_id": "sess_1"}), "agent is required")

	var turn agent.ActiveTurn
	require.NoError(t, json.Unmarshal([]byte(tcCall(t, d, "session_turn", map[string]any{"agent": "bot", "session_id": "sess_idle"})), &turn))
	assert.False(t, turn.Processing)
	assert.Empty(t, turn.Items)
}

// ── job tools ─────────────────────────────────────────────────────────────────

func TestToolsCoreTaskCompileQueryAndGet(t *testing.T) {
	d := tcSetup(t, botConfig())
	day := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	for _, rec := range []domain.TaskCompile{
		{ID: "tc_ok", AgentID: "bot", TaskName: "daily", Status: domain.TaskCompileStatusSucceeded, CreatedAt: day,
			Stages: []domain.TaskCompileStage{{}}},
		{ID: "tc_bad", AgentID: "bot", TaskName: "weekly", Status: domain.TaskCompileStatusFailed, CreatedAt: day.Add(48 * time.Hour)},
	} {
		require.NoError(t, store.WriteJSON(store.TaskCompilePath(rec.AgentID, rec.ID), rec))
	}

	ids := func(args map[string]any) []string {
		var recs []domain.TaskCompile
		require.NoError(t, json.Unmarshal([]byte(tcCall(t, d, "task_compile_query", args)), &recs))
		out := []string{}
		for _, r := range recs {
			assert.Nil(t, r.Stages, "query leaves out stages")
			out = append(out, r.ID)
		}
		return out
	}
	assert.Equal(t, []string{"tc_bad", "tc_ok"}, ids(map[string]any{}), "newest first")
	assert.Equal(t, []string{"tc_ok"}, ids(map[string]any{"id": "tc_ok"}))
	assert.Equal(t, []string{"tc_bad"}, ids(map[string]any{"status": "failed"}))
	assert.Equal(t, []string{"tc_ok"}, ids(map[string]any{"end": "2026-03-10"}))
	assert.Equal(t, []string{"tc_bad"}, ids(map[string]any{"start": "2026-03-11T00:00:00Z", "task": "WEEKLY", "agent": "Bot"}))

	assert.Contains(t, tcCall(t, d, "task_compile_query", map[string]any{"start": "yesterday"}), `start "yesterday" is neither a date`)
	assert.Contains(t, tcCall(t, d, "task_compile_query", map[string]any{"end": "soon"}), `end "soon" is neither a date`)

	var full domain.TaskCompile
	require.NoError(t, json.Unmarshal([]byte(tcCall(t, d, "task_compile_get", map[string]any{"id": "tc_ok"})), &full))
	assert.Len(t, full.Stages, 1, "get returns the full record")

	require.NoError(t, os.WriteFile(store.TaskCompilePath("bot", "tc_broken"), []byte("{"), 0o600))
	assert.Contains(t, tcCall(t, d, "task_compile_get", map[string]any{"id": "tc_broken"}), "reading task compile tc_broken")
	assert.Contains(t, tcCall(t, d, "task_compile_get", map[string]any{"id": "tc_none"}), "task compile tc_none not found")
	assert.Equal(t, []string{"tc_bad", "tc_ok"}, ids(map[string]any{}), "a corrupt record is skipped by the query")

	// A task_compiles entry that is not a directory cannot be listed. Windows
	// reports the same ReadDir failure differently, so only Unix checks it.
	if runtime.GOOS == "windows" {
		return
	}
	require.NoError(t, os.MkdirAll(store.AgentDir("zz"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(store.AgentDir("zz"), "task_compiles"), nil, 0o600))
	assert.Contains(t, tcCall(t, d, "task_compile_query", map[string]any{}), "reading directory")
}

func TestToolsCoreJobLogsFallsBackToSessionLog(t *testing.T) {
	d := tcSetup(t, botConfig())
	sess, err := agent.NewSessionManager().Create("bot")
	require.NoError(t, err)
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, msg := range []domain.Message{
		{ID: "m1", Role: domain.MessageRoleUser, Content: "check the logs", Timestamp: ts},
		{ID: "m1", ResponseID: "m3"},
		{ID: "m2", Role: domain.MessageRoleTool, Content: `{"ok":true}`, Timestamp: ts},
		{ID: "m3", Role: domain.MessageRoleAssistant, Content: "see chart", MediaURLs: []string{"file:///a.png", "file:///b.png"}},
	} {
		require.NoError(t, store.AppendJSONL(store.SessionPath("bot", sess.ID), msg))
	}
	job := domain.Job{ID: "job_sess", AgentID: "bot", TaskID: "bot/t", SessionID: sess.ID, Status: domain.JobStatusCompleted}
	require.NoError(t, store.WriteJSON(store.JobPath("bot", job.ID), job))

	out := tcCall(t, d, "job_logs", map[string]any{"id": job.ID})
	assert.Contains(t, out, "[2026-01-02T03:04:05Z] <user>: check the logs")
	assert.Contains(t, out, "<tool>: {\n  \"ok\": true\n}", "tool JSON is pretty-printed")
	assert.Contains(t, out, "[] <assistant>: see chart [media: file:///a.png] [media: file:///b.png]")

	// A job whose session log holds only markers has no output.
	empty, err := agent.NewSessionManager().Create("bot")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(store.SessionPath("bot", empty.ID), []byte(`{"id":"x","response_id":"y"}`+"\n"), 0o600))
	job2 := domain.Job{ID: "job_empty", AgentID: "bot", SessionID: empty.ID}
	require.NoError(t, store.WriteJSON(store.JobPath("bot", job2.ID), job2))
	assert.Equal(t, "(no output captured)", tcCall(t, d, "job_logs", map[string]any{"id": job2.ID}))

	job3 := domain.Job{ID: "job_script", AgentID: "bot", Output: "model output", ScriptError: "line 3: nil value"}
	require.NoError(t, store.WriteJSON(store.JobPath("bot", job3.ID), job3))
	assert.Equal(t, "Precompiled script failed, so the model ran the task:\nline 3: nil value\n\nmodel output",
		tcCall(t, d, "job_logs", map[string]any{"id": job3.ID}))

	require.NoError(t, os.WriteFile(store.JobPath("bot", "job_corrupt"), []byte("{"), 0o600))
	assert.Contains(t, tcCall(t, d, "job_logs", map[string]any{"id": "job_corrupt"}), "reading job job_corrupt")
}

func TestToolsCoreJobRunNowUnknownJob(t *testing.T) {
	d := tcSetup(t, botConfig())
	out := tcCall(t, d, "job_run_now", map[string]any{"id": "job_missing"})
	assert.Contains(t, out, "job_missing")
}

// ── server tools ──────────────────────────────────────────────────────────────

func TestToolsCoreModelsList(t *testing.T) {
	d := tcSetup(t, botConfig())

	var all []string
	require.NoError(t, json.Unmarshal([]byte(tcCall(t, d, "models_list", map[string]any{})), &all))
	assert.NotEmpty(t, all)

	var anthropic []string
	require.NoError(t, json.Unmarshal([]byte(tcCall(t, d, "models_list", map[string]any{"provider": "anthropic"})), &anthropic))
	require.NotEmpty(t, anthropic)
	for _, id := range anthropic {
		assert.True(t, strings.HasPrefix(id, "anthropic/"), id)
	}
	assert.Contains(t, tcCall(t, d, "models_list", map[string]any{"provider": "nosuch"}), `unknown provider "nosuch"`)
}

func TestToolsCoreModelsListIntrospectsEndpoints(t *testing.T) {
	var mu sync.Mutex
	var authHeaders []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		mu.Unlock()
		if r.URL.Path != "/v1/models" {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"llama-3"},{"id":"qwen"}]}`)
	}))
	t.Cleanup(srv.Close)

	d := tcSetup(t, &config.Config{
		Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}},
		Models: config.ModelsConfig{Providers: map[string]config.ProviderConfig{
			"ollama": {BaseURI: srv.URL + "/v1", Auth: "auth:ollama:key"},
		}},
	})
	st, err := auth.NewFileStore(filepath.Join(t.TempDir(), "credentials.json"))
	require.NoError(t, err)
	require.NoError(t, st.Set("ollama:key", "secret-token"))
	deps := *GetDeps()
	deps.Auth = st
	SetDeps(&deps)

	var list []string
	require.NoError(t, json.Unmarshal([]byte(tcCall(t, d, "models_list", map[string]any{"provider": "vllm", "base_uri": srv.URL, "auth": "plain"})), &list))
	assert.Equal(t, []string{"vllm/llama-3", "vllm/qwen"}, list)

	// The configured base URI and auth reference are used when none is given.
	require.NoError(t, json.Unmarshal([]byte(tcCall(t, d, "models_list", map[string]any{"provider": "ollama"})), &list))
	assert.Equal(t, []string{"ollama/llama-3", "ollama/qwen"}, list)
	mu.Lock()
	assert.Equal(t, []string{"Bearer plain", "Bearer secret-token"}, authHeaders)
	mu.Unlock()

	assert.Contains(t, tcCall(t, d, "models_list", map[string]any{"provider": "ollama", "auth": "auth:missing"}), "auth:missing")
	assert.Contains(t, tcCall(t, d, "models_list", map[string]any{"provider": "vllm", "base_uri": srv.URL + "/other"}), "404")
}

func TestToolsCoreServerRestartSurfacesRestartErrors(t *testing.T) {
	tcSetup(t, botConfig())
	deps := *GetDeps()
	deps.Restart = func(context.Context, saferestart.Request) (saferestart.Status, error) {
		return saferestart.Status{}, fmt.Errorf("restart already in progress")
	}
	SetDeps(&deps)
	d := NewDispatcher("https://localhost:16677", "")
	assert.Contains(t, tcCall(t, d, "server_restart", map[string]any{}), "restart already in progress")
}

func TestToolsCoreConfigRestoreLatestBackupErrors(t *testing.T) {
	d := tcSetup(t, botConfig())
	backups := filepath.Join(filepath.Dir(config.DefaultPath()), "backups")
	require.NoError(t, os.RemoveAll(backups))
	assert.Contains(t, tcCall(t, d, "config_restore_latest_backup", map[string]any{}), "latest config backup not found")

	require.NoError(t, os.MkdirAll(backups, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(backups, "aviary.yml.bak.1"), []byte("agents: [unclosed\n"), 0o600))
	assert.Contains(t, tcCall(t, d, "config_restore_latest_backup", map[string]any{}), "loading restored config")
}

func TestToolsCoreConfigSaveKeepsRetainedFileTasks(t *testing.T) {
	d := tcSetup(t, &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "anthropic/claude-3-haiku"}}})
	cfg, err := config.Load("")
	require.NoError(t, err)
	dir := config.AgentTasksDir(cfg.Agents[0])
	_, err = config.SaveMarkdownTask(dir, config.TaskConfig{Name: "keep", Prompt: "kept", Schedule: "0 0 9 * * *"})
	require.NoError(t, err)
	dropPath, err := config.SaveMarkdownTask(dir, config.TaskConfig{Name: "drop", Prompt: "dropped", Schedule: "0 0 10 * * *"})
	require.NoError(t, err)
	brokenPath := filepath.Join(dir, "broken.md")
	require.NoError(t, os.WriteFile(brokenPath, []byte("---\nschedule: [unclosed\n---\nbody\n"), 0o600))

	next := config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "anthropic/claude-3-haiku", Tasks: []config.TaskConfig{
		{Name: "keep", Prompt: "kept", Schedule: "0 0 9 * * *", FromFile: true},
	}}}}
	raw, err := json.Marshal(next)
	require.NoError(t, err)
	assert.Equal(t, "config saved", tcCall(t, d, "config_save", map[string]any{"config": string(raw)}))

	assert.NoFileExists(t, dropPath, "a removed file task loses its file")
	assert.FileExists(t, filepath.Join(dir, "keep.md"))
	assert.FileExists(t, brokenPath, "an unparseable file is left alone")
}

func TestToolsCoreConfigTaskFileToolErrors(t *testing.T) {
	d := tcSetup(t, &config.Config{Agents: []config.AgentConfig{{
		Name: "bot", Model: "test/x",
		Tasks: []config.TaskConfig{{Name: "inline", Prompt: "hi", Schedule: "0 0 9 * * *"}},
	}}})
	cfg, err := config.Load("")
	require.NoError(t, err)
	tasksDir := config.AgentTasksDir(cfg.Agents[0])
	require.NoError(t, os.MkdirAll(filepath.Dir(tasksDir), 0o700))
	require.NoError(t, os.WriteFile(tasksDir, []byte("not a dir"), 0o600))

	assert.Contains(t, tcCall(t, d, "config_task_move_to_file", map[string]any{"agent": "bot", "task": "inline"}), "writing task file")
	after, err := config.Load("")
	require.NoError(t, err)
	require.Len(t, after.Agents[0].Tasks, 1, "the inline task stays when its file cannot be written")

	assert.Contains(t, tcCall(t, d, "config_task_convert_to_script", map[string]any{"agent": "ghost", "task": "inline"}), "not found")
}

// ── config_task_rename ────────────────────────────────────────────────────────

func TestToolsCoreRenameTaskValidation(t *testing.T) {
	d := tcSetup(t, &config.Config{Agents: []config.AgentConfig{{
		Name: "bot", Model: "test/x",
		Tasks: []config.TaskConfig{
			{Name: "a", Prompt: "a", Schedule: "0 0 9 * * *"},
			{Name: "b", Prompt: "b", Schedule: "0 0 9 * * *"},
		},
	}}})
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"agent": "bot", "task": "a", "new_name": "  "}, "new name must be provided"},
		{map[string]any{"agent": "ghost", "task": "a", "new_name": "c"}, `agent "ghost" not found`},
		{map[string]any{"agent": "bot", "task": "a", "new_name": "b"}, `task "b" already defined for agent "bot"`},
		{map[string]any{"agent": "bot", "task": "zzz", "new_name": "c"}, `task "zzz" not found in agent "bot"`},
	} {
		assert.Contains(t, tcCall(t, d, "config_task_rename", tc.args), tc.want)
	}
	cfg, err := config.Load("")
	require.NoError(t, err)
	var names []string
	for _, task := range cfg.Agents[0].Tasks {
		names = append(names, task.Name)
	}
	assert.Equal(t, []string{"a", "b"}, names, "failed renames change nothing")
}

func TestToolsCoreRenameTaskFindsPlaceholderTaskFiles(t *testing.T) {
	d := tcSetup(t, botConfig())
	cfg, err := config.Load("")
	require.NoError(t, err)
	dir := config.AgentTasksDir(cfg.Agents[0])
	require.NoError(t, os.MkdirAll(dir, 0o700))
	// Files starting with "_" are skipped when loading config, so this task
	// is only found by scanning the task files.
	placeholder := filepath.Join(dir, "_draft.md")
	require.NoError(t, os.WriteFile(placeholder, []byte("---\nname: draft-report\nschedule: 0 0 9 * * *\n---\nWrite the report.\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.md"), []byte("---\nschedule: [unclosed\n---\nbody\n"), 0o600))

	out := tcCall(t, d, "config_task_rename", map[string]any{"agent": "bot", "task": "draft-report", "new_name": "final-report"})
	assert.Equal(t, `task "draft-report" renamed to "final-report" (file: final-report.md)`, out)
	assert.NoFileExists(t, placeholder)
	renamed, err := config.LoadMarkdownTask(filepath.Join(dir, "final-report.md"))
	require.NoError(t, err)
	assert.Equal(t, "final-report", renamed.Name)
	assert.Equal(t, "Write the report.", renamed.Prompt)

	after, err := config.Load("")
	require.NoError(t, err)
	require.Len(t, after.Agents[0].Tasks, 1)
	assert.Equal(t, "final-report", after.Agents[0].Tasks[0].Name)
}

// ── task_schedule ─────────────────────────────────────────────────────────────

func TestToolsCoreScheduleTaskValidation(t *testing.T) {
	d := tcSetup(t, botConfig())
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"agent": "bot", "content": "x", "project": "o/r"}, "project and model are only supported for type=project tasks"},
		{map[string]any{"agent": "bot", "content": "x", "model": "test/y"}, "project and model are only supported for type=project tasks"},
		{map[string]any{"agent": "bot", "content": "this is not lua (", "type": "script"}, "script tasks require valid Aviary Lua source"},
		{map[string]any{"agent": "bot", "content": "x", "target": "pager:ops"}, `invalid target "pager:ops"`},
		{map[string]any{"agent": "bot", "content": "x", "channel": "general"}, "only supported for workflows"},
	} {
		assert.Contains(t, tcCall(t, d, "task_schedule", tc.args), tc.want)
	}
	// The tool schema rejects unknown types; scheduleTask, which
	// project_task_create also calls, checks them itself.
	_, _, err := scheduleTask(context.Background(), nil, taskScheduleArgs{Agent: "bot", Content: "x", Type: "binary"})
	assert.EqualError(t, err, `invalid task type "binary"`)

	tasks, err := config.LoadAgentTaskFiles(config.AgentConfig{Name: "bot"})
	require.NoError(t, err)
	assert.Empty(t, tasks, "rejected requests save no task")
}

func TestToolsCoreScheduleTaskScripts(t *testing.T) {
	d := tcSetup(t, botConfig())
	script := "print('hi')"

	out := tcCall(t, d, "task_schedule", map[string]any{"agent": "bot", "content": script, "type": "script"})
	require.Contains(t, out, "Task scheduled immediately")
	jobID := strings.TrimSuffix(strings.SplitN(strings.SplitN(out, "job ID: ", 2)[1], ")", 2)[0], ")")
	job, err := store.ReadJSON[domain.Job](store.FindJobPath(jobID))
	require.NoError(t, err)
	assert.Equal(t, "script", job.TaskType)
	assert.Equal(t, script, job.Script)
	assert.True(t, strings.HasPrefix(job.TaskID, "print-hi-"), job.TaskID)

	out = tcCall(t, d, "task_schedule", map[string]any{"agent": "bot", "content": script, "type": "script", "name": "greeter", "schedule": "every 5 minutes"})
	assert.Equal(t, `Recurring task "greeter" created for agent "bot" with schedule "0 */5 * * * *".`, out)
	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Len(t, cfg.Agents[0].Tasks, 1)
	task := cfg.Agents[0].Tasks[0]
	assert.Equal(t, "script", task.Type)
	assert.Equal(t, script, task.Prompt, "a script task stores its Lua source as the file body")
	assert.Equal(t, "0 */5 * * * *", task.Schedule)
}

func TestToolsCoreScheduleTaskGeneratedNames(t *testing.T) {
	d := tcSetup(t, &config.Config{Agents: []config.AgentConfig{{
		Name: "bot", Model: "test/x",
		Tasks: []config.TaskConfig{
			{Name: "other-schedule", Prompt: "x", Schedule: "0 0 8 * * *"},
			{Name: "scheduled-elsewhere", Prompt: "x", Schedule: "0 0 9 * * *", Target: "session:abc"},
			{Name: "unrelated-name", Prompt: "x", Schedule: "0 0 9 * * *"},
		},
	}}})

	out := tcCall(t, d, "task_schedule", map[string]any{"agent": "bot", "content": "!!!", "schedule": "0 0 9 * * *", "precompile": false})
	require.Contains(t, out, `Recurring task "scheduled-`, "content without letters or digits gets a generic name")
	assert.Contains(t, out, " created ")

	out = tcCall(t, d, "task_schedule", map[string]any{"agent": "bot", "content": "Run 42 now", "in": "1h"})
	require.Contains(t, out, "Task scheduled at ")
	jobID := strings.TrimSuffix(strings.SplitN(strings.SplitN(out, "job ID: ", 2)[1], ")", 2)[0], ")")
	job, err := store.ReadJSON[domain.Job](store.FindJobPath(jobID))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(job.TaskID, "run-42-now-"), job.TaskID)
}

func TestToolsCoreScheduleTaskDefaultRoutes(t *testing.T) {
	d := tcSetup(t, &config.Config{Agents: []config.AgentConfig{
		{Name: "helper", Model: "test/x"},
		{Name: "bot", Model: "test/x"},
	}})
	taskTarget := func(name string) string {
		cfg, err := config.Load("")
		require.NoError(t, err)
		for _, a := range cfg.Agents {
			for _, task := range a.Tasks {
				if task.Name == name {
					return task.Target
				}
			}
		}
		t.Fatalf("task %q not saved", name)
		return ""
	}
	schedule := func(ctx context.Context, name string) {
		out := tcCallCtx(ctx, t, d, "task_schedule", map[string]any{"agent": "bot", "name": name, "content": "report", "schedule": "0 0 9 * * *", "precompile": false})
		require.Contains(t, out, "created", out)
	}

	sessCtx := agent.WithSessionAgentID(agent.WithSessionID(context.Background(), "sess_abc"), "bot")
	schedule(sessCtx, "from-own-session")
	assert.Equal(t, "session:sess_abc", taskTarget("from-own-session"), "a task asked for in a chat reports back to it")

	otherCtx := agent.WithSessionAgentID(agent.WithSessionID(context.Background(), "sess_abc"), "helper")
	schedule(otherCtx, "from-other-agent")
	assert.Equal(t, "", taskTarget("from-other-agent"), "another agent's session is not a route")

	chanCtx := agent.WithChannelSession(context.Background(), "slack", "unknown", "C1")
	schedule(chanCtx, "from-unknown-channel")
	assert.Equal(t, "", taskTarget("from-unknown-channel"), "a channel the agent does not have is not a route")
}
