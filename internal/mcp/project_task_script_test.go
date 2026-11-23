package mcp

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/scriptruntime"
)

// releaseCheckPrompt is the task from #952: it can only be done with shell
// commands in the clone.
const releaseCheckPrompt = "If main is different from last release, trigger a github action run of release.yml"

func toolNames(tools []agent.ToolInfo) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

// projectScriptRuntimeContext is the context the scheduler runs a project
// task's script with (see scheduler.runCompiledProjectTask).
func projectScriptRuntimeContext(agentName, checkout string) context.Context {
	ctx := agent.WithSessionAgentID(agent.WithSessionID(context.Background(), "task-session"), agentName)
	return agent.WithProjectTaskScript(agent.WithFilesystemGrant(ctx, checkout))
}

// setupProjectScriptAgent configures bot, a standard-preset agent with no
// exec permissions, as the owner of a precompiled project task, with the
// real tool catalog instead of a stub.
func setupProjectScriptAgent(t *testing.T, model string) {
	t.Helper()
	on := true
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: model, Tasks: []config.TaskConfig{{
		Name: "release", Type: "project", Project: "acme/app", Prompt: releaseCheckPrompt, Trigger: "daily", At: "09:00", Precompile: &on,
	}}}}}
	setupProjectTaskCompile(t, cfg, compiledScript(`print("unused")`))
	compileToolsFunc = nil
}

func TestProjectTaskScriptToolsMatchAcrossCompileAnalyzeAndRuntime(t *testing.T) {
	setupProjectScriptAgent(t, "test/x")
	ctx := context.Background()

	plain, err := compileTools(ctx, "bot")
	require.NoError(t, err)
	assert.NotContains(t, toolNames(plain), "exec", "a standard agent without exec permissions has no exec")

	compiled, err := compileTools(agent.WithProjectTaskScript(ctx), "bot")
	require.NoError(t, err)
	assert.Contains(t, toolNames(compiled), "exec", "a project task's script is compiled with exec")

	// task_analyze analyzes a project task against the same tools.
	stubTaskAnalyzer(t, nil)
	var analyzed []string
	taskAnalyzerFunc = func(ctx context.Context, agentName, _, _ string) (*taskAnalyzerResult, error) {
		tools, err := compileTools(ctx, agentName)
		require.NoError(t, err)
		analyzed = toolNames(tools)
		return &taskAnalyzerResult{Analysis: &compileTaskAnalysis{Steps: []compiledTaskStep{{Kind: "deterministic", Deterministic: true, Tool: "exec"}}}, Tools: tools}, nil
	}
	_, err = analyzeTaskForEditor(ctx, taskAnalyzeArgs{Agent: "bot", Type: "project", Project: "acme/app", Content: releaseCheckPrompt})
	require.NoError(t, err)

	// The script runs with the same tool table.
	runCtx := projectScriptRuntimeContext("bot", t.TempDir())
	client, err := NewAgentToolClient(runCtx)
	require.NoError(t, err)
	defer client.Close() //nolint:errcheck
	runtime, err := client.ListTools(runCtx)
	require.NoError(t, err)

	assert.Equal(t, toolNames(compiled), analyzed)
	assert.Equal(t, toolNames(compiled), toolNames(runtime))
}

func TestProjectTaskNeedingShellCompilesToExecScriptThatRunsInClone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	setupProjectScriptAgent(t, "test/x")
	script := `local r = tool.exec({command = "git rev-parse --show-toplevel"})
print(r.stdout)`
	tryCompileTaskPromptFunc = func(ctx context.Context, agentName, prompt, _ string, _ bool) (*compiledTaskResult, error) {
		// The compiler sees exec, which the prompt says runs in the clone.
		tools, err := compileTools(ctx, agentName)
		require.NoError(t, err)
		require.Contains(t, toolNames(tools), "exec")
		assert.Contains(t, prompt, "exec runs commands in that clone")
		return &compiledTaskResult{Type: "script", Script: script, Steps: []compiledTaskStep{{Kind: "deterministic", Deterministic: true, Tool: "exec"}}}, nil
	}
	got, err := EnsureProjectTaskCompiled(context.Background(), "bot", "release", "run")
	require.NoError(t, err)
	require.Equal(t, script, got)

	checkout := t.TempDir()
	require.NoError(t, exec.Command("git", "init", "-q", checkout).Run())
	ctx := projectScriptRuntimeContext("bot", checkout)
	client, err := NewAgentToolClient(ctx)
	require.NoError(t, err)
	defer client.Close() //nolint:errcheck
	out, err := scriptruntime.RunLua(ctx, got, scriptruntime.Options{ToolClient: client})
	require.NoError(t, err)
	want, err := filepath.EvalSymlinks(checkout)
	require.NoError(t, err)
	gotDir, err := filepath.EvalSymlinks(strings.TrimSpace(out))
	require.NoError(t, err)
	assert.Equal(t, want, gotDir)

	// exec stays in the clone.
	refused, err := client.CallToolText(ctx, "exec", map[string]any{"command": "git status", "cwd": t.TempDir()})
	require.NoError(t, err)
	assert.Contains(t, refused, "inside its clone")

	// Outside a project task's script, the agent still has no exec.
	plainCtx := agent.WithFilesystemGrant(agent.WithSessionAgentID(context.Background(), "bot"), checkout)
	plain, err := NewAgentToolClient(plainCtx)
	require.NoError(t, err)
	defer plain.Close() //nolint:errcheck
	_, err = plain.CallToolText(plainCtx, "exec", map[string]any{"command": "git status"})
	assert.Error(t, err)
}

// stubCompileStages answers the compiler's CLI model by stage.
func stubCompileStages(t *testing.T, analysis, generation, validation string) *[]string {
	t.Helper()
	// The real compiler runs, on a stubbed CLI model.
	tryCompileTaskPromptFunc = nil
	prev := completeCLIText
	t.Cleanup(func() { completeCLIText = prev })
	var stages []string
	completeCLIText = func(_ context.Context, _, system, _ string) (string, error) {
		switch {
		case system == taskDeterminismSystemPrompt:
			stages = append(stages, "analysis")
			return analysis, nil
		case strings.HasPrefix(system, "Generate a single-file Lua script"):
			stages = append(stages, "generation")
			return generation, nil
		default:
			stages = append(stages, "validation")
			return validation, nil
		}
	}
	return &stages
}

const deterministicExecAnalysis = `{"reason":"fixed commands","steps":[{"kind":"deterministic","deterministic":true,"tool":"exec","description":"compare main with the last release"}]}`

func TestCompileRejectsScriptThatAlwaysErrors(t *testing.T) {
	setupProjectScriptAgent(t, "claude/opus")
	stubCompileStages(t, deterministicExecAnalysis,
		"error(\"release check aborted: the `exec` tool is not available in this script runtime\")",
		`{"valid":true,"reason":"ok"}`)

	got, err := EnsureProjectTaskCompiled(context.Background(), "bot", "release", "run")
	require.NoError(t, err)
	assert.Empty(t, got, "a stub is never promoted")
	task := loadedTask(t, "release")
	assert.Empty(t, task.Compiled)
	assert.Equal(t, "project", task.Type, "the task stays a prompt-run project task")
}

func TestCompileRejectsScriptWhoseReasonSaysStayAPromptTask(t *testing.T) {
	setupProjectScriptAgent(t, "claude/opus")
	stubCompileStages(t, deterministicExecAnalysis,
		`local r = tool.exec({command = "gh release view --json tagName"}) print(r.stdout)`,
		`{"valid":true,"reason":"The script never dispatches release.yml, so a faithful deterministic script cannot be built from these tools. The task should stay a prompt task and not be auto-promoted."}`)

	got, err := EnsureProjectTaskCompiled(context.Background(), "bot", "release", "run")
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Empty(t, loadedTask(t, "release").Compiled)
}

func TestCompileDeclinedWhenAnalysisReasonSaysStayAPromptTask(t *testing.T) {
	setupProjectScriptAgent(t, "claude/opus")
	stages := stubCompileStages(t,
		`{"reason":"exec is unavailable, so this should stay a prompt task","steps":[{"kind":"deterministic","deterministic":true}]}`,
		`print("never generated")`, `{"valid":true}`)

	got, err := EnsureProjectTaskCompiled(context.Background(), "bot", "release", "run")
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, []string{"analysis"}, *stages, "nothing is generated once the analysis declines")
}

func TestCompilePromotesWorkingExecScript(t *testing.T) {
	setupProjectScriptAgent(t, "claude/opus")
	script := `local r = tool.exec({command = "gh release view --json tagName"}) print(r.stdout)`
	stubCompileStages(t, deterministicExecAnalysis, script, `{"valid":true,"reason":"compares main with the release and dispatches the workflow"}`)

	got, err := EnsureProjectTaskCompiled(context.Background(), "bot", "release", "run")
	require.NoError(t, err)
	assert.Equal(t, script, got)
}
