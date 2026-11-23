package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// releaseShaPrompt is the release-aviary task from #958, which reads main's
// SHA and the latest release from the project's repository.
const releaseShaPrompt = "If main sha is different from the last release sha then trigger a github action for release.yml"

func TestProjectTaskCompileUsesExplicitRepositoryForMainSHA(t *testing.T) {
	setupProjectScriptAgent(t, "claude/opus")
	// The real compiler runs on a stubbed model that writes its gh calls from
	// what the compile prompt tells it.
	stubCompileStages(t, deterministicExecAnalysis, "", `{"valid":true,"reason":"compares main with the release"}`)
	prevComplete := completeCLIText
	completeCLIText = func(ctx context.Context, model, system, user string) (string, error) {
		if !strings.HasPrefix(system, "Generate a single-file Lua script") {
			return prevComplete(ctx, model, system, user)
		}
		require.Contains(t, user, "acme/app")
		require.Contains(t, user, "gh api repos/acme/app/commits/main --jq .sha", "the prompt gives the endpoint for main's SHA")
		return `local main = tool.exec({command = "gh api repos/acme/app/commits/main --jq .sha"})
local tag = tool.exec({command = "gh release view --repo acme/app --json tagName --jq .tagName"})
print(main.stdout, tag.stdout)`, nil
	}

	got, err := EnsureTaskCompiled(context.Background(), "bot", "release", "run")
	require.NoError(t, err)
	assert.Contains(t, got, "repos/acme/app/commits/main")
	assert.NotContains(t, got, "{owner}")
	assert.NotContains(t, got, "$GH_REPO")
	assert.Empty(t, projectScriptPlaceholderProblem(got))
}

func TestCompileRejectsScriptThatNamesRepositoryThroughPlaceholders(t *testing.T) {
	for name, command := range map[string]string{
		"shell variable":   `gh api "repos/$GH_REPO/commits/main" --jq .sha`,
		"braced variable":  `gh api repos/${GH_REPO}/commits/main --jq .sha`,
		"gh placeholders":  `gh api repos/{owner}/{repo}/commits/main --jq .sha`,
		"repo placeholder": `gh api repos/acme/{repo}/git/ref/heads/main --jq .object.sha`,
	} {
		t.Run(name, func(t *testing.T) {
			setupProjectScriptAgent(t, "claude/opus")
			stubCompileStages(t, deterministicExecAnalysis,
				fmt.Sprintf("local r = tool.exec({command = %q})\nprint(r.stdout)", command),
				`{"valid":true,"reason":"reads main's SHA"}`)

			got, err := EnsureTaskCompiled(context.Background(), "bot", "release", "run")
			require.NoError(t, err)
			assert.Empty(t, got, "the script is not promoted")
			task := loadedTask(t, "release")
			assert.Empty(t, task.Compiled, "the task stays a prompt task")
			assert.NotEmpty(t, task.CompiledKey, "the same prompt is not compiled again every run")
			records, err := listTaskCompiles()
			require.NoError(t, err)
			require.NotEmpty(t, records)
			assert.Equal(t, domain.TaskCompileStatusSkipped, records[0].Status)
			assert.Contains(t, records[0].Reason, "Rejected")
		})
	}
	// Lua's own "$" anchors are not shell variables.
	assert.Empty(t, projectScriptPlaceholderProblem(`local s = ("x "):gsub("%s+$", "") if s:match("^%x+$") then print(s) end`))
}

// setupCompiledReleaseTask compiles the release task to script and returns
// its compile key.
func setupCompiledReleaseTask(t *testing.T, script string) string {
	t.Helper()
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x", Tasks: []config.TaskConfig{{
		Name: "release", Type: "project", Project: "acme/app", Prompt: releaseShaPrompt, Trigger: "interval", Every: "5m", Precompile: new(true),
	}}}}}
	setupProjectTaskCompile(t, cfg, compiledScript(script))
	got, err := EnsureTaskCompiled(context.Background(), "bot", "release", "run")
	require.NoError(t, err)
	require.Equal(t, script, got)
	return loadedTask(t, "release").CompiledKey
}

var errGh404 = errors.New("<string>:52: failed to read main SHA: gh: Not Found (HTTP 404) {\n  \"message\": \"Not Found\",\n  \"status\": \"404\"\n}\nstack traceback:\n    [G]: in function 'error'")

func TestScriptFailingItsFirstRunIsSetAside(t *testing.T) {
	key := setupCompiledReleaseTask(t, `print("checked")`)

	require.NoError(t, RecordTaskScriptResult("bot", "release", errGh404))
	task := loadedTask(t, "release")
	assert.Empty(t, task.Compiled, "a script that never completed is dropped at once")
	assert.Equal(t, key, task.CompiledKey, "and not compiled again for the same prompt")
	require.NotNil(t, task.Precompile)
	assert.True(t, *task.Precompile)
	assert.Contains(t, task.PrecompileNote, "failed on its first run (gh 404)")

	// Later runs use the model without compiling again.
	got, err := EnsureTaskCompiled(context.Background(), "bot", "release", "run")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestConsecutiveScriptFailuresTurnPrecompileOff(t *testing.T) {
	setupCompiledReleaseTask(t, `print("checked")`)

	require.NoError(t, RecordTaskScriptResult("bot", "release", nil))
	task := loadedTask(t, "release")
	assert.True(t, task.ScriptVerified)

	for i := 1; i < projectScriptFailureLimit; i++ {
		require.NoError(t, RecordTaskScriptResult("bot", "release", errGh404))
		task = loadedTask(t, "release")
		assert.Equal(t, i, task.ScriptFailures)
		assert.NotEmpty(t, task.Compiled, "a script that has worked gets more than one chance")
		assert.Empty(t, task.PrecompileNote)
	}
	// A success in between starts the count over.
	require.NoError(t, RecordTaskScriptResult("bot", "release", nil))
	assert.Zero(t, loadedTask(t, "release").ScriptFailures)

	for range projectScriptFailureLimit {
		require.NoError(t, RecordTaskScriptResult("bot", "release", errGh404))
	}
	task = loadedTask(t, "release")
	require.NotNil(t, task.Precompile)
	assert.False(t, *task.Precompile, "precompile is turned off")
	assert.False(t, EffectiveTaskPrecompile(task))
	assert.Empty(t, task.Compiled)
	assert.Contains(t, task.PrecompileNote, fmt.Sprintf("failed %d runs in a row (last: gh 404)", projectScriptFailureLimit))

	// The note reaches the UI with the task's config.
	data, err := json.Marshal(task)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"precompile_note":"Precompile was turned off`)

	// Turning precompile back on clears the note and compiles a fresh script.
	_, err = NewDispatcher("https://localhost:16677", "").CallTool(context.Background(), "task_update", map[string]any{
		"name": "bot/release", "precompile": true,
	})
	require.NoError(t, err)
	task = loadedTask(t, "release")
	assert.Empty(t, task.PrecompileNote)
	assert.Zero(t, task.ScriptFailures)
	require.Eventually(t, func() bool { return loadedTask(t, "release").Compiled != "" }, 5*time.Second, 20*time.Millisecond)
}

func TestScriptStateSurvivesMarkdownTaskFile(t *testing.T) {
	dir := t.TempDir()
	task := config.TaskConfig{Name: "release", Type: "project", Project: "acme/app", Prompt: releaseShaPrompt, Every: "5m",
		Precompile: new(true), Compiled: `print(1)`, CompiledKey: "k", ScriptVerified: true, ScriptFailures: 2, PrecompileNote: "note"}
	path, err := config.SaveMarkdownTask(dir, task)
	require.NoError(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	loaded, err := config.ParseMarkdownTask("release", raw)
	require.NoError(t, err)
	assert.True(t, loaded.ScriptVerified)
	assert.Equal(t, 2, loaded.ScriptFailures)
	assert.Equal(t, "note", loaded.PrecompileNote)
}

func TestTaskCompileQueryHonorsFilters(t *testing.T) {
	setupProjectTaskCompile(t, &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x"}}}, compiledScript(`print(1)`))
	big := strings.Repeat("x", 100_000)
	write := func(id, agentName, task string, created time.Time) {
		dir := filepath.Join(store.AgentDir(agentName), "task_compiles")
		require.NoError(t, os.MkdirAll(dir, 0o700))
		require.NoError(t, store.WriteJSON(filepath.Join(dir, id+".json"), &domain.TaskCompile{
			ID: id, AgentID: agentName, TaskName: task, Status: domain.TaskCompileStatusSucceeded, CreatedAt: created,
			Stages: []domain.TaskCompileStage{{Name: "generation", Response: big}},
		}))
	}
	day := time.Date(2026, 11, 23, 8, 0, 0, 0, time.UTC)
	write("task_compile_old", "Aviary", "release-aviary", day.Add(-48*time.Hour))
	write("task_compile_new", "Aviary", "release-aviary", day)
	write("task_compile_other", "other", "triage", day)

	query := func(args map[string]any) []domain.TaskCompile {
		t.Helper()
		out, err := NewDispatcher("https://localhost:16677", "").CallTool(context.Background(), "task_compile_query", args)
		require.NoError(t, err)
		assert.Less(t, len(out), 10_000, "records leave out their large stages")
		var records []domain.TaskCompile
		require.NoError(t, json.Unmarshal([]byte(out), &records))
		for _, r := range records {
			assert.Empty(t, r.Stages)
		}
		return records
	}
	ids := func(records []domain.TaskCompile) []string {
		out := make([]string, 0, len(records))
		for _, r := range records {
			out = append(out, r.ID)
		}
		return out
	}

	assert.ElementsMatch(t, []string{"task_compile_new", "task_compile_old"}, ids(query(map[string]any{"agent": "aviary"})))
	assert.Equal(t, []string{"task_compile_new"}, ids(query(map[string]any{"agent": "Aviary", "start": "2026-11-23"})))
	assert.Equal(t, []string{"task_compile_new"}, ids(query(map[string]any{"agent": "Aviary", "start": "2026-11-23T07:00:00Z"})))
	assert.Equal(t, []string{"task_compile_old"}, ids(query(map[string]any{"end": "2026-11-21"})))
	assert.Equal(t, []string{"task_compile_other"}, ids(query(map[string]any{"task": "triage"})))

	toolCallContains(t, NewDispatcher("https://localhost:16677", ""), "task_compile_query", map[string]any{"start": "yesterday"}, "neither a date")

	// task_compile_get still returns the full record.
	full, err := NewDispatcher("https://localhost:16677", "").CallTool(context.Background(), "task_compile_get", map[string]any{"id": "task_compile_new"})
	require.NoError(t, err)
	assert.Contains(t, full, big)
}
