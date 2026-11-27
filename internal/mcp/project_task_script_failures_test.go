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
		require.Contains(t, user, "gh api repos/acme/app/releases/latest --jq .tag_name", "the prompt gives the endpoint for the latest release")
		require.NotContains(t, user, "gh release view")
		return `local main = tool.exec({command = "gh api repos/acme/app/commits/main --jq .sha"})
local tag = tool.exec({command = "gh api repos/acme/app/releases/latest --jq .tag_name"})
print(main.stdout, tag.stdout)`, nil
	}

	got, err := EnsureTaskCompiled(context.Background(), "bot", "release", "run")
	require.NoError(t, err)
	assert.Contains(t, got, "repos/acme/app/commits/main")
	assert.Contains(t, got, "repos/acme/app/releases/latest")
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

var errRateLimited = errors.New("<string>:12: gh: API rate limit exceeded for user ID 1. (HTTP 403)")

func TestScriptFailingItsFirstRunStaysCompiled(t *testing.T) {
	key := setupCompiledReleaseTask(t, `print("checked")`)

	result, err := RecordTaskScriptOutcome("bot", "release", errGh404)
	require.NoError(t, err)
	assert.True(t, result.StreakStarted)
	task := loadedTask(t, "release")
	assert.Equal(t, `print("checked")`, task.Compiled, "the script is kept")
	assert.Equal(t, key, task.CompiledKey)
	require.NotNil(t, task.Precompile)
	assert.True(t, *task.Precompile)
	assert.Equal(t, 1, task.ScriptFailures)
	assert.Equal(t, "gh 404", task.ScriptLastError)
	assert.Empty(t, task.PrecompileNote)

	// The next run still runs the script.
	got, err := EnsureTaskCompiled(context.Background(), "bot", "release", "run")
	require.NoError(t, err)
	assert.Equal(t, `print("checked")`, got)
}

func TestConsecutiveScriptFailuresNeverTurnPrecompileOff(t *testing.T) {
	setupCompiledReleaseTask(t, `print("checked")`)

	require.NoError(t, RecordTaskScriptResult("bot", "release", nil))
	task := loadedTask(t, "release")
	assert.True(t, task.ScriptVerified)

	started := 0
	for i := 1; i <= 10; i++ {
		result, err := RecordTaskScriptOutcome("bot", "release", errGh404)
		require.NoError(t, err)
		if result.StreakStarted {
			started++
		}
		task = loadedTask(t, "release")
		assert.Equal(t, i, task.ScriptFailures)
		assert.Equal(t, `print("checked")`, task.Compiled)
		assert.True(t, EffectiveTaskPrecompile(task), "precompile stays on")
		assert.Empty(t, task.PrecompileNote)
	}
	assert.Equal(t, 1, started, "a run of failures starts one streak")

	// The failure count and last error reach the UI with the task's config.
	data, err := json.Marshal(task)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"script_failures":10`)
	assert.Contains(t, string(data), `"script_last_error":"gh 404"`)

	// A success ends the streak, so the next failure starts a new one.
	require.NoError(t, RecordTaskScriptResult("bot", "release", nil))
	task = loadedTask(t, "release")
	assert.Zero(t, task.ScriptFailures)
	assert.Empty(t, task.ScriptLastError)
	result, err := RecordTaskScriptOutcome("bot", "release", errGh404)
	require.NoError(t, err)
	assert.True(t, result.StreakStarted)
}

func TestTransientScriptFailuresAreNotCounted(t *testing.T) {
	setupCompiledReleaseTask(t, `print("checked")`)
	for _, runErr := range []error{
		errRateLimited,
		errors.New("gh: You have exceeded a secondary rate limit (HTTP 403)"),
		errors.New("Post \"https://api.github.com/graphql\": dial tcp: lookup api.github.com: no such host"),
		errors.New("gh: Server Error (HTTP 502)"),
	} {
		result, err := RecordTaskScriptOutcome("bot", "release", runErr)
		require.NoError(t, err)
		assert.False(t, result.StreakStarted, runErr.Error())
	}
	task := loadedTask(t, "release")
	assert.Zero(t, task.ScriptFailures)
	assert.Empty(t, task.ScriptLastError)
	assert.Equal(t, `print("checked")`, task.Compiled)
}

func TestScriptStateSurvivesMarkdownTaskFile(t *testing.T) {
	dir := t.TempDir()
	task := config.TaskConfig{Name: "release", Type: "project", Project: "acme/app", Prompt: releaseShaPrompt, Every: "5m",
		Precompile: new(true), Compiled: `print(1)`, CompiledKey: "k", ScriptVerified: true, ScriptFailures: 2, ScriptLastError: "gh 404"}
	path, err := config.SaveMarkdownTask(dir, task)
	require.NoError(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	loaded, err := config.ParseMarkdownTask("release", raw)
	require.NoError(t, err)
	assert.True(t, loaded.ScriptVerified)
	assert.Equal(t, 2, loaded.ScriptFailures)
	assert.Equal(t, "gh 404", loaded.ScriptLastError)
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

func TestCompilerVersionBumpRecompilesFailingScripts(t *testing.T) {
	key := setupCompiledReleaseTask(t, `print("old compiler")`)
	require.NoError(t, RecordTaskScriptResult("bot", "release", errGh404))
	require.Equal(t, key, loadedTask(t, "release").CompiledKey)

	prev := taskCompilerVersion
	t.Cleanup(func() { taskCompilerVersion = prev })
	taskCompilerVersion++

	got, err := EnsureTaskCompiled(context.Background(), "bot", "release", "run")
	require.NoError(t, err)
	assert.Equal(t, `print("old compiler")`, got, "the new compiler compiles the task again")
	task := loadedTask(t, "release")
	assert.NotEqual(t, key, task.CompiledKey)
	assert.Zero(t, task.ScriptFailures, "the failures are cleared")
	assert.Empty(t, task.ScriptLastError)
	assert.False(t, task.ScriptVerified)
}

func TestRecompileRestartsFailingScript(t *testing.T) {
	setupCompiledReleaseTask(t, `print("checked")`)
	require.NoError(t, RecordTaskScriptResult("bot", "release", nil))
	for range 3 {
		require.NoError(t, RecordTaskScriptResult("bot", "release", errGh404))
	}
	require.Equal(t, 3, loadedTask(t, "release").ScriptFailures)

	out, err := NewDispatcher("https://localhost:16677", "").CallTool(context.Background(), "config_task_convert_to_script", map[string]any{
		"agent": "bot", "task": "release",
	})
	require.NoError(t, err)
	assert.Contains(t, out, "recompiling")
	backgroundCompiles.Wait()

	task := loadedTask(t, "release")
	assert.True(t, EffectiveTaskPrecompile(task))
	assert.Equal(t, `print("checked")`, task.Compiled)
	assert.NotEmpty(t, task.CompiledKey)
	assert.Zero(t, task.ScriptFailures)
	assert.Empty(t, task.ScriptLastError)
	assert.False(t, task.ScriptVerified)
}

func TestMigrateRestoresTasksChangedAfterScriptFailures(t *testing.T) {
	off := false
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x", Tasks: []config.TaskConfig{
		{Name: "release", Type: "project", Project: "acme/app", Prompt: releaseShaPrompt, Every: "5m", Precompile: &off, ScriptVerified: true, ScriptFailures: 3,
			PrecompileNote: "Precompile was turned off after the compiled script failed 3 runs in a row (last: gh 403), so the task runs on the model. Use Recompile on the task page to compile it again."},
		{Name: "aside", Type: "project", Project: "acme/app", Prompt: releaseShaPrompt, Every: "5m", Precompile: new(true), CompiledKey: "k", ScriptFailures: 1,
			PrecompileNote: "The compiled script failed on its first run (gh 404), so it was set aside and the task runs on the model. Use Recompile on the task page to compile it again."},
		{Name: "manual", Type: "project", Project: "acme/app", Prompt: releaseShaPrompt, Every: "5m", Precompile: &off},
	}}}}
	setupProjectTaskCompile(t, cfg, compiledScript(`print(1)`))

	restored, err := MigrateAutoDisabledPrecompile()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"bot/release", "bot/aside"}, restored)

	release := loadedTask(t, "release")
	assert.True(t, EffectiveTaskPrecompile(release), "precompile is back on")
	assert.Empty(t, release.PrecompileNote)
	assert.Zero(t, release.ScriptFailures)
	aside := loadedTask(t, "aside")
	assert.True(t, EffectiveTaskPrecompile(aside))
	assert.Empty(t, aside.CompiledKey, "the set-aside script is compiled again")
	assert.Empty(t, aside.PrecompileNote)
	manual := loadedTask(t, "manual")
	require.NotNil(t, manual.Precompile)
	assert.False(t, *manual.Precompile, "a task the user turned off is left alone")

	// It runs once: a second pass finds nothing to restore.
	restored, err = MigrateAutoDisabledPrecompile()
	require.NoError(t, err)
	assert.Empty(t, restored)
}
