package mcp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/scheduler"
	"github.com/lsegal/aviary/internal/store"
)

// setupProjectTaskCompile saves cfg as the running config and stubs the
// compiler and the tool catalog. compile answers each compile; the returned
// slice records the prompts compiled, and tools is the catalog compiles see.
func setupProjectTaskCompile(t *testing.T, cfg *config.Config, compile func(prompt string) *compiledTaskResult) (*[]string, *[]agent.ToolInfo) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(filepath.Join(base, "aviary"))
	t.Cleanup(func() { store.SetDataDir("") })
	t.Setenv("AVIARY_CONFIG_BASE_DIR", filepath.Join(base, "aviary"))
	require.NoError(t, store.EnsureDirs())

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	// Background compiles read the deps, so they finish before the restore.
	t.Cleanup(backgroundCompiles.Wait)
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	var prompts []string
	prevCompiler := tryCompileTaskPromptFunc
	t.Cleanup(func() { tryCompileTaskPromptFunc = prevCompiler })
	tryCompileTaskPromptFunc = func(_ context.Context, agentName, prompt, _ string, _ bool) (*compiledTaskResult, error) {
		assert.Equal(t, "bot", agentName)
		prompts = append(prompts, prompt)
		return compile(prompt), nil
	}
	tools := []agent.ToolInfo{{Name: "exec"}, {Name: "task_run"}}
	prevTools := compileToolsFunc
	t.Cleanup(func() { compileToolsFunc = prevTools })
	compileToolsFunc = func(context.Context, string) ([]agent.ToolInfo, error) { return tools, nil }

	require.NoError(t, config.Save("", cfg))
	mgr := agent.NewManager(nil)
	mgr.Reconcile(cfg)
	s, err := scheduler.New(mgr, 1)
	require.NoError(t, err)
	t.Cleanup(s.Stop)
	s.Reconcile(cfg)
	SetDeps(&Deps{Agents: mgr, Scheduler: s})
	return &prompts, &tools
}

func compiledScript(script string) func(string) *compiledTaskResult {
	return func(string) *compiledTaskResult {
		return &compiledTaskResult{Type: "script", Script: script, Steps: []compiledTaskStep{{Kind: "deterministic", Deterministic: true, Tool: "exec"}}}
	}
}

func loadedTask(t *testing.T, name string) config.TaskConfig {
	t.Helper()
	cfg, err := config.Load("")
	require.NoError(t, err)
	for _, task := range cfg.Agents[0].Tasks {
		if task.Name == name {
			return task
		}
	}
	t.Fatalf("task %q not found", name)
	return config.TaskConfig{}
}

func TestTaskScheduleSavesPrecompiledProjectTask(t *testing.T) {
	prompts, _ := setupProjectTaskCompile(t, &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}}, compiledScript(`print("relabeled")`))

	out, err := NewDispatcher("https://localhost:16677", "").CallTool(context.Background(), "task_schedule", map[string]any{
		"agent": "bot", "name": "stale", "type": "project", "project": "acme/app",
		"content": "Label stale issues", "trigger": "daily", "at": "09:00",
	})
	require.NoError(t, err)
	assert.Contains(t, out, "Precompiled")
	assert.Equal(t, []string{projectTaskCompilePrompt("Label stale issues", "acme/app")}, *prompts)

	// The setting and the script survive a reload, and the task stays a
	// project task whose prompt the model can still fall back to.
	task := loadedTask(t, "stale")
	assert.Equal(t, "project", task.Type)
	assert.Equal(t, "Label stale issues", task.Prompt)
	require.NotNil(t, task.Precompile)
	assert.True(t, *task.Precompile)
	assert.Equal(t, `print("relabeled")`, task.Compiled)
	assert.NotEmpty(t, task.CompiledKey)

	_, err = NewDispatcher("https://localhost:16677", "").CallTool(context.Background(), "task_schedule", map[string]any{
		"agent": "bot", "name": "triage", "type": "project", "project": "acme/app",
		"content": "Triage new issues", "trigger": "daily", "at": "09:00", "precompile": false,
	})
	require.NoError(t, err)
	off := loadedTask(t, "triage")
	require.NotNil(t, off.Precompile)
	assert.False(t, *off.Precompile)
	assert.Empty(t, off.Compiled)
	assert.Len(t, *prompts, 1, "a project task with precompile off is not compiled")
}

func TestEnsureProjectTaskCompiledRecompilesOnChange(t *testing.T) {
	on := true
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x", Tasks: []config.TaskConfig{{
		Name: "stale", Type: "project", Project: "acme/app", Prompt: "Label stale issues", Trigger: "daily", At: "09:00", Precompile: &on,
	}}}}}
	script := `print("v1")`
	prompts, tools := setupProjectTaskCompile(t, cfg, func(string) *compiledTaskResult { return compiledScript(script)("") })
	ctx := context.Background()

	got, err := EnsureProjectTaskCompiled(ctx, "bot", "stale", "run")
	require.NoError(t, err)
	assert.Equal(t, `print("v1")`, got)
	assert.Equal(t, `print("v1")`, loadedTask(t, "stale").Compiled, "an aviary.yaml task keeps its script there")

	// Nothing changed: the saved script is reused without compiling.
	script = `print("v2")`
	got, err = EnsureProjectTaskCompiled(ctx, "bot", "stale", "run")
	require.NoError(t, err)
	assert.Equal(t, `print("v1")`, got)
	assert.Len(t, *prompts, 1)

	// A new tool recompiles.
	*tools = append(*tools, agent.ToolInfo{Name: "web_get"})
	got, err = EnsureProjectTaskCompiled(ctx, "bot", "stale", "run")
	require.NoError(t, err)
	assert.Equal(t, `print("v2")`, got)
	assert.Len(t, *prompts, 2)

	// So do a new prompt and a new project.
	saved, err := config.Load("")
	require.NoError(t, err)
	saved.Agents[0].Tasks[0].Prompt = "Label issues stale for 30 days"
	require.NoError(t, config.Save("", saved))
	_, err = EnsureProjectTaskCompiled(ctx, "bot", "stale", "run")
	require.NoError(t, err)
	saved.Agents[0].Tasks[0] = loadedTask(t, "stale")
	saved.Agents[0].Tasks[0].Project = "acme/web"
	require.NoError(t, config.Save("", saved))
	_, err = EnsureProjectTaskCompiled(ctx, "bot", "stale", "run")
	require.NoError(t, err)
	assert.Equal(t, []string{
		projectTaskCompilePrompt("Label stale issues", "acme/app"),
		projectTaskCompilePrompt("Label stale issues", "acme/app"),
		projectTaskCompilePrompt("Label issues stale for 30 days", "acme/app"),
		projectTaskCompilePrompt("Label issues stale for 30 days", "acme/web"),
	}, *prompts)
}

func TestEnsureProjectTaskCompiledKeepsNonDeterministicPromptOnModel(t *testing.T) {
	on := true
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x", Tasks: []config.TaskConfig{{
		Name: "fix", Type: "project", Project: "acme/app", Prompt: "Fix the flakiest test", Trigger: "daily", At: "09:00", Precompile: &on,
	}}}}}
	prompts, _ := setupProjectTaskCompile(t, cfg, func(string) *compiledTaskResult {
		return &compiledTaskResult{Type: "prompt", Reason: "editing code needs the model"}
	})

	for range 2 {
		got, err := EnsureProjectTaskCompiled(context.Background(), "bot", "fix", "run")
		require.NoError(t, err)
		assert.Empty(t, got)
	}
	assert.Len(t, *prompts, 1, "the verdict is kept until the prompt, project or tools change")
}
