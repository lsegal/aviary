package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
)

func TestMigrateDropsUnknownPrecompileNote(t *testing.T) {
	off := false
	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "test/x", Tasks: []config.TaskConfig{
		{Name: "odd", Type: "project", Project: "acme/app", Prompt: releaseShaPrompt, Every: "5m", Precompile: &off, CompiledKey: "k", ScriptFailures: 2,
			PrecompileNote: "  Something a future version wrote  "},
		{Name: "plain", Type: "project", Project: "acme/app", Prompt: releaseShaPrompt, Every: "5m", Precompile: &off, CompiledKey: "p", ScriptFailures: 1},
	}}}}
	setupProjectTaskCompile(t, cfg, compiledScript(`print(1)`))

	restored, err := MigrateAutoDisabledPrecompile()
	require.NoError(t, err)
	assert.Empty(t, restored, "an unknown note restores nothing")

	odd := loadedTask(t, "odd")
	assert.Empty(t, odd.PrecompileNote, "the unknown note is dropped")
	require.NotNil(t, odd.Precompile)
	assert.False(t, *odd.Precompile, "the setting is left alone")
	assert.Equal(t, "k", odd.CompiledKey, "the script is kept")
	assert.Equal(t, 2, odd.ScriptFailures)

	plain := loadedTask(t, "plain")
	require.NotNil(t, plain.Precompile)
	assert.False(t, *plain.Precompile, "a task without a note is untouched")
	assert.Equal(t, "p", plain.CompiledKey)
	assert.Equal(t, 1, plain.ScriptFailures)
}

func TestMigrateReturnsConfigLoadError(t *testing.T) {
	setupProjectTaskCompile(t, botConfig(), compiledScript(`print(1)`))
	require.NoError(t, os.WriteFile(config.DefaultPath(), []byte("agents: [\n  - name: bot\n    tasks: {"), 0o600))

	restored, err := MigrateAutoDisabledPrecompile()
	require.Error(t, err)
	assert.Nil(t, restored)
}

// setupUnwritableFileTask stores the task "stuck" in a markdown file whose
// stem differs from its name, and puts a directory where saving it would
// write stuck.md, so saving the task fails on every platform.
func setupUnwritableFileTask(t *testing.T, note string) string {
	t.Helper()
	setupProjectTaskCompile(t, botConfig(), compiledScript(`print(1)`))
	agentCfg := config.AgentConfig{Name: "bot"}
	dir := config.AgentTasksDir(agentCfg)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "stuck.md"), 0o750))
	path := filepath.Join(dir, "stuck-file.md")
	content := "---\nname: stuck\ntype: project\nproject: acme/app\nevery: 5m\nprecompile: false\ncompiled_key: k\nprecompile_note: " + note + "\n---\n\n" + releaseShaPrompt + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	task := loadedTask(t, "stuck")
	require.True(t, task.FromFile)
	require.Equal(t, note, task.PrecompileNote)
	return path
}

func TestMigrateReturnsSaveErrorForUnknownNote(t *testing.T) {
	path := setupUnwritableFileTask(t, "An unknown note")
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	restored, err := MigrateAutoDisabledPrecompile()
	require.Error(t, err)
	assert.Empty(t, restored)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "the task file is unchanged")
	assert.Equal(t, "An unknown note", loadedTask(t, "stuck").PrecompileNote)
}

func TestMigrateReturnsSaveErrorForRestoredTask(t *testing.T) {
	setupUnwritableFileTask(t, autoDisabledPrecompileNote+" 3 runs in a row")

	restored, err := MigrateAutoDisabledPrecompile()
	require.Error(t, err)
	assert.Empty(t, restored, "a task that could not be saved is not reported restored")
	task := loadedTask(t, "stuck")
	require.NotNil(t, task.Precompile)
	assert.False(t, *task.Precompile, "the saved setting is unchanged")
	assert.Equal(t, "k", task.CompiledKey)
}
