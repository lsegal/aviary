package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
)

// scriptedToolModel is an OpenAI-compatible chat endpoint that answers each
// user turn by calling one tool, then replies with the tool's result.
type scriptedToolModel struct {
	mu   sync.Mutex
	tool string
	args map[string]any
}

func (m *scriptedToolModel) set(tool string, args map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tool, m.args = tool, args
}

func (m *scriptedToolModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &req)
	w.Header().Set("Content-Type", "text/event-stream")
	chunk := func(delta map[string]any, finish any) {
		data, _ := json.Marshal(map[string]any{
			"id": "c", "object": "chat.completion.chunk", "created": 0, "model": "m",
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	}
	var result any
	for _, message := range req.Messages {
		switch message.Role {
		case "user":
			result = nil
		case "tool":
			result = message.Content
		}
	}
	if result != nil {
		chunk(map[string]any{"role": "assistant", "content": fmt.Sprint(result)}, nil)
		chunk(map[string]any{}, "stop")
	} else {
		m.mu.Lock()
		args, _ := json.Marshal(m.args)
		tool := m.tool
		m.mu.Unlock()
		chunk(map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
			"index": 0, "id": "call_1", "type": "function",
			"function": map[string]any{"name": tool, "arguments": string(args)},
		}}}, nil)
		chunk(map[string]any{}, "tool_calls")
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

func setupScriptedAgent(t *testing.T) (*Dispatcher, *scriptedToolModel) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	require.NoError(t, store.EnsureDirs())

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })
	agent.SetToolClientFactory(NewAgentToolClient)
	t.Cleanup(func() { agent.SetToolClientFactory(nil) })

	model := &scriptedToolModel{}
	srv := httptest.NewServer(model)
	t.Cleanup(srv.Close)
	factory := llm.NewFactory(func(string) (string, error) { return "", nil }).
		WithProviderOptionsResolver(func(string) (llm.ProviderOptions, bool) {
			return llm.ProviderOptions{BaseURI: srv.URL + "/v1"}, true
		})
	mgr := agent.NewManager(factory)
	t.Cleanup(mgr.Stop)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{
		Name:  "bot",
		Model: "vllm/test",
		// File tools need a preset that enables them; the grant replaces only
		// the static allowedPaths.
		Permissions: &config.PermissionsConfig{Preset: config.PermissionsPresetFull},
	}}})
	SetDeps(&Deps{Agents: mgr})
	return NewDispatcher("https://localhost:16677", ""), model
}

func TestAgentRun_WorkdirGrantsTheRunItsCloneAndStreamsFileChanges(t *testing.T) {
	d, model := setupScriptedAgent(t)
	clone := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(clone, "main.go"), []byte("package main\n"), 0o644))
	resolved, err := filepath.EvalSymlinks(clone)
	require.NoError(t, err)

	model.set("file_edit", map[string]any{"path": "main.go", "old_string": "main", "new_string": "app"})
	var progress []string
	var mu sync.Mutex
	_, err = d.CallToolProgress(context.Background(), "agent_run", map[string]any{
		"name": "bot", "message": "rename the package", "workdir": clone,
		"include_tool_progress": true, "include_file_progress": true,
	}, func(message string) {
		mu.Lock()
		defer mu.Unlock()
		progress = append(progress, message)
	})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(clone, "main.go"))
	require.NoError(t, err)
	assert.Equal(t, "package app\n", string(data), "a relative path edits the granted clone")

	mu.Lock()
	defer mu.Unlock()
	var file agent.FileChangeEvent
	var sawTool bool
	for _, message := range progress {
		if payload, ok := strings.CutPrefix(message, "[file]"); ok {
			require.NoError(t, json.Unmarshal([]byte(payload), &file))
		}
		sawTool = sawTool || strings.HasPrefix(message, `[tool]{"args"`) || strings.HasPrefix(message, `[tool]{"name"`)
	}
	assert.True(t, sawTool, "tool progress: %v", progress)
	assert.Equal(t, "file_edit", file.Tool)
	assert.Equal(t, filepath.Join(resolved, "main.go"), file.Path)
	assert.Contains(t, file.Diff, "+package app")

	// The same agent's next run carries no grant, so the clone is out of reach.
	model.set("file_read", map[string]any{"path": filepath.Join(clone, "main.go")})
	out, err := d.CallTool(context.Background(), "agent_run", map[string]any{"name": "bot", "message": "read it"})
	require.NoError(t, err)
	assert.Contains(t, out, "no filesystem allowedPaths configured")
	assert.NotContains(t, out, "package app")
}

func TestAgentRun_RejectsInvalidWorkdir(t *testing.T) {
	d, _ := setupScriptedAgent(t)
	file := filepath.Join(t.TempDir(), "file.txt")
	require.NoError(t, os.WriteFile(file, nil, 0o644))

	for workdir, want := range map[string]string{
		"relative/clone":                      "must be an absolute path",
		file:                                  "is not a directory",
		filepath.Join(t.TempDir(), "missing"): "no such file",
	} {
		_, err := d.CallToolProgress(context.Background(), "agent_run", map[string]any{
			"name": "bot", "message": "hi", "workdir": workdir,
		}, func(string) {})
		require.Error(t, err, workdir)
		assert.Contains(t, err.Error(), want, workdir)
	}
}
