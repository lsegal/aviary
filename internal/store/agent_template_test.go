package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
)

func setupAgentTemplateDataDir(t *testing.T) {
	t.Helper()
	SetDataDir(filepath.Join(t.TempDir(), "aviary"))
	t.Cleanup(func() { SetDataDir("") })
}

func agentsConfig(names ...string) *config.Config {
	cfg := &config.Config{}
	for _, name := range names {
		cfg.Agents = append(cfg.Agents, config.AgentConfig{Name: name, Model: "m-" + name})
	}
	return cfg
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestEnsureNewAgentTemplates(t *testing.T) {
	t.Run("nil_next_is_noop", func(t *testing.T) {
		setupAgentTemplateDataDir(t)
		assert.NoError(t, EnsureNewAgentTemplates(agentsConfig("a"), nil))
		assert.NoDirExists(t, filepath.Join(DataDir(), DirAgents))
	})

	t.Run("nil_prev_scaffolds_every_agent", func(t *testing.T) {
		setupAgentTemplateDataDir(t)
		assert.NoError(t, EnsureNewAgentTemplates(nil, agentsConfig("a", "b")))
		assert.FileExists(t, filepath.Join(AgentDir("a"), "RULES.md"))
		assert.FileExists(t, filepath.Join(AgentDir("b"), "RULES.md"))
	})

	t.Run("only_new_agents_are_scaffolded", func(t *testing.T) {
		setupAgentTemplateDataDir(t)
		assert.NoError(t, EnsureNewAgentTemplates(agentsConfig("existing"), agentsConfig("existing", "fresh")))
		assert.NoDirExists(t, AgentDir("existing"))
		assert.FileExists(t, filepath.Join(AgentDir("fresh"), "AGENTS.md"))
		assert.DirExists(t, filepath.Join(AgentDir("fresh"), "sessions"))
	})

	t.Run("sync_error_names_agent", func(t *testing.T) {
		setupAgentTemplateDataDir(t)
		// A regular file where the agent directory should be makes MkdirAll fail.
		writeTestFile(t, AgentDir("blocked"), "not a dir")
		err := EnsureNewAgentTemplates(nil, agentsConfig("blocked"))
		assert.ErrorContains(t, err, `syncing template for agent "blocked"`)
	})
}

func TestIsTemplateOnlyDir(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string // relative path -> content; trailing "/" makes a dir
		want  bool
	}{
		{name: "empty_dir", files: nil, want: false},
		{name: "only_scaffold_files", files: map[string]string{"AGENTS.md": "a", "MEMORY.md": "m", "RULES.md": "r"}, want: true},
		{name: "scaffold_dirs_with_gitkeep", files: map[string]string{"jobs/.gitkeep": "", "sessions/.gitkeep": "", "memory/": "", "task_compiles/": ""}, want: true},
		{name: "only_empty_scaffold_dirs", files: map[string]string{"jobs/": "", "sessions/": ""}, want: false},
		{name: "unknown_file", files: map[string]string{"RULES.md": "r", "notes.txt": "x"}, want: false},
		{name: "unknown_dir", files: map[string]string{"RULES.md": "r", "custom/": ""}, want: false},
		{name: "nested_dir_in_scaffold_dir", files: map[string]string{"RULES.md": "r", "sessions/nested/": ""}, want: false},
		{name: "extra_file_in_scaffold_dir", files: map[string]string{"RULES.md": "r", "sessions/abc.jsonl": "{}"}, want: false},
		{name: "all_scaffold_files", files: map[string]string{"TOOLS.md": "t", "notes/.gitkeep": "", "tasks/_ADD_TASKS_HERE": "", "task_compiles/.gitkeep": ""}, want: true},
		{name: "extra_file_in_notes", files: map[string]string{"TOOLS.md": "t", "notes/todo.md": "x"}, want: false},
		{name: "task_file_in_tasks", files: map[string]string{"TOOLS.md": "t", "tasks/daily.md": "x"}, want: false},
		{name: "compiled_task_in_task_compiles", files: map[string]string{"TOOLS.md": "t", "task_compiles/abc.json": "{}"}, want: false},
		{name: "scaffold_file_as_dir", files: map[string]string{"TOOLS.md/": ""}, want: false},
		{name: "scaffold_dir_as_file", files: map[string]string{"RULES.md": "r", "notes": "x"}, want: false},
		{name: "gitkeep_at_root", files: map[string]string{".gitkeep": ""}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range tc.files {
				path := filepath.Join(dir, filepath.FromSlash(rel))
				if rel[len(rel)-1] == '/' {
					require.NoError(t, os.MkdirAll(path, 0o700))
					continue
				}
				writeTestFile(t, path, content)
			}
			got, err := isTemplateOnlyDir(dir)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("missing_dir_errors", func(t *testing.T) {
		got, err := isTemplateOnlyDir(filepath.Join(t.TempDir(), "missing"))
		assert.Error(t, err)
		assert.True(t, os.IsNotExist(err))
		assert.False(t, got)
	})

	t.Run("unreadable_scaffold_dir_errors", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("directory permissions are not enforced on Windows")
		}
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory permissions")
		}
		dir := t.TempDir()
		sub := filepath.Join(dir, "sessions")
		require.NoError(t, os.MkdirAll(sub, 0o700))
		require.NoError(t, os.Chmod(sub, 0o000))
		t.Cleanup(func() { _ = os.Chmod(sub, 0o700) })

		got, err := isTemplateOnlyDir(dir)
		assert.Error(t, err)
		assert.False(t, got)
	})
}

func TestRenameMatchingAgentDirs_NilConfigsAreNoop(t *testing.T) {
	setupAgentTemplateDataDir(t)
	oldDir := AgentDir("old")
	require.NoError(t, os.MkdirAll(oldDir, 0o700))

	assert.NoError(t, RenameMatchingAgentDirs(nil, agentsConfig("new")))
	assert.NoError(t, RenameMatchingAgentDirs(agentsConfig("old"), nil))
	assert.DirExists(t, oldDir)
}

func TestRenameMatchingAgentDirs_AmbiguousMatchIsIgnored(t *testing.T) {
	setupAgentTemplateDataDir(t)
	oldDir := AgentDir("old")
	writeTestFile(t, filepath.Join(oldDir, "MEMORY.md"), "keep")

	same := config.AgentConfig{Model: "m"}
	prev := &config.Config{Agents: []config.AgentConfig{{Name: "old", Model: same.Model}}}
	next := &config.Config{Agents: []config.AgentConfig{
		{Name: "new-a", Model: same.Model},
		{Name: "new-b", Model: same.Model},
	}}

	assert.NoError(t, RenameMatchingAgentDirs(prev, next))
	assert.DirExists(t, oldDir)
	assert.NoDirExists(t, AgentDir("new-a"))
	assert.NoDirExists(t, AgentDir("new-b"))
}

func TestRenameMatchingAgentDirs_DifferentConfigIsNotRenamed(t *testing.T) {
	setupAgentTemplateDataDir(t)
	oldDir := AgentDir("old")
	require.NoError(t, os.MkdirAll(oldDir, 0o700))

	prev := &config.Config{Agents: []config.AgentConfig{{Name: "old", Model: "m1"}}}
	next := &config.Config{Agents: []config.AgentConfig{{Name: "new", Model: "m2"}}}

	assert.NoError(t, RenameMatchingAgentDirs(prev, next))
	assert.DirExists(t, oldDir)
	assert.NoDirExists(t, AgentDir("new"))
}

func TestRenameMatchingAgentDirs_PairsEachAddedAgentOnce(t *testing.T) {
	setupAgentTemplateDataDir(t)
	writeTestFile(t, filepath.Join(AgentDir("old-a"), "MEMORY.md"), "a")
	writeTestFile(t, filepath.Join(AgentDir("old-b"), "MEMORY.md"), "b")

	prev := &config.Config{Agents: []config.AgentConfig{{Name: "old-a", Model: "m"}, {Name: "old-b", Model: "m"}}}
	next := &config.Config{Agents: []config.AgentConfig{{Name: "new-a", Model: "m"}}}

	// old-a claims new-a; old-b then finds the only candidate already used.
	assert.NoError(t, RenameMatchingAgentDirs(prev, next))
	content, err := os.ReadFile(filepath.Join(AgentDir("new-a"), "MEMORY.md"))
	require.NoError(t, err)
	assert.Equal(t, "a", string(content))
	assert.NoDirExists(t, AgentDir("old-a"))
	assert.DirExists(t, AgentDir("old-b"))
}

func TestRenameMatchingAgentDirs_EmptySlicesMatchNil(t *testing.T) {
	setupAgentTemplateDataDir(t)
	require.NoError(t, os.MkdirAll(AgentDir("old"), 0o700))

	prev := &config.Config{Agents: []config.AgentConfig{{Name: "old", Model: "m", Fallbacks: []string{}, Channels: []config.ChannelConfig{}, Tasks: []config.TaskConfig{}}}}
	next := &config.Config{Agents: []config.AgentConfig{{Name: "new", Model: "m"}}}

	assert.NoError(t, RenameMatchingAgentDirs(prev, next))
	assert.NoDirExists(t, AgentDir("old"))
	assert.DirExists(t, AgentDir("new"))
}

func TestRenameMatchingAgentDirs_SameSanitizedDirIsNoop(t *testing.T) {
	setupAgentTemplateDataDir(t)
	// "a:b" and "a/b" both sanitize to the "a_b" directory.
	dir := AgentDir("a:b")
	require.Equal(t, dir, AgentDir("a/b"))
	writeTestFile(t, filepath.Join(dir, "MEMORY.md"), "keep")

	prev := &config.Config{Agents: []config.AgentConfig{{Name: "a:b", Model: "m"}}}
	next := &config.Config{Agents: []config.AgentConfig{{Name: "a/b", Model: "m"}}}

	assert.NoError(t, RenameMatchingAgentDirs(prev, next))
	content, err := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	require.NoError(t, err)
	assert.Equal(t, "keep", string(content))
}

func TestRenameMatchingAgentDirs_MissingOldDirIsSkipped(t *testing.T) {
	setupAgentTemplateDataDir(t)
	prev := &config.Config{Agents: []config.AgentConfig{{Name: "old", Model: "m"}}}
	next := &config.Config{Agents: []config.AgentConfig{{Name: "new", Model: "m"}}}

	assert.NoError(t, RenameMatchingAgentDirs(prev, next))
	assert.NoDirExists(t, AgentDir("new"))
}

func TestRenameMatchingAgentDirs_ReplacesTemplateOnlyTarget(t *testing.T) {
	setupAgentTemplateDataDir(t)
	oldDir := AgentDir("old")
	newDir := AgentDir("new")
	writeTestFile(t, filepath.Join(oldDir, "MEMORY.md"), "real memory")
	writeTestFile(t, filepath.Join(newDir, "RULES.md"), "scaffold")
	writeTestFile(t, filepath.Join(newDir, "sessions", ".gitkeep"), "")

	prev := &config.Config{Agents: []config.AgentConfig{{Name: "old", Model: "m"}}}
	next := &config.Config{Agents: []config.AgentConfig{{Name: "new", Model: "m"}}}

	assert.NoError(t, RenameMatchingAgentDirs(prev, next))
	assert.NoDirExists(t, oldDir)
	content, err := os.ReadFile(filepath.Join(newDir, "MEMORY.md"))
	require.NoError(t, err)
	assert.Equal(t, "real memory", string(content))
	assert.NoFileExists(t, filepath.Join(newDir, "RULES.md"))
}

func TestIsTemplateOnlyDir_SyncedScaffold(t *testing.T) {
	setupAgentTemplateDataDir(t)
	require.NoError(t, SyncAgentTemplate("fresh"))

	got, err := isTemplateOnlyDir(AgentDir("fresh"))
	require.NoError(t, err)
	assert.True(t, got)

	writeTestFile(t, filepath.Join(AgentDir("fresh"), "sessions", "s.jsonl"), "{}")
	got, err = isTemplateOnlyDir(AgentDir("fresh"))
	require.NoError(t, err)
	assert.False(t, got)
}

func TestRenameMatchingAgentDirs_ReplacesSyncedScaffoldTarget(t *testing.T) {
	setupAgentTemplateDataDir(t)
	oldDir := AgentDir("old")
	newDir := AgentDir("new")
	require.NoError(t, SyncAgentTemplate("old"))
	writeTestFile(t, filepath.Join(oldDir, "MEMORY.md"), "real memory")
	writeTestFile(t, filepath.Join(oldDir, "sessions", "s.jsonl"), "{}")
	require.NoError(t, SyncAgentTemplate("new"))

	prev := &config.Config{Agents: []config.AgentConfig{{Name: "old", Model: "m"}}}
	next := &config.Config{Agents: []config.AgentConfig{{Name: "new", Model: "m"}}}

	assert.NoError(t, RenameMatchingAgentDirs(prev, next))
	assert.NoDirExists(t, oldDir)
	content, err := os.ReadFile(filepath.Join(newDir, "MEMORY.md"))
	require.NoError(t, err)
	assert.Equal(t, "real memory", string(content))
	assert.FileExists(t, filepath.Join(newDir, "sessions", "s.jsonl"))
}

func TestRenameMatchingAgentDirs_KeepsTargetWithUserData(t *testing.T) {
	setupAgentTemplateDataDir(t)
	oldDir := AgentDir("old")
	newDir := AgentDir("new")
	writeTestFile(t, filepath.Join(oldDir, "MEMORY.md"), "old memory")
	writeTestFile(t, filepath.Join(newDir, "RULES.md"), "scaffold")
	writeTestFile(t, filepath.Join(newDir, "sessions", "s.jsonl"), "{}")

	prev := &config.Config{Agents: []config.AgentConfig{{Name: "old", Model: "m"}}}
	next := &config.Config{Agents: []config.AgentConfig{{Name: "new", Model: "m"}}}

	assert.NoError(t, RenameMatchingAgentDirs(prev, next))
	assert.DirExists(t, oldDir)
	assert.FileExists(t, filepath.Join(newDir, "sessions", "s.jsonl"))
}

func TestRenameMatchingAgentDirs_StatOldDirError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports a file-as-directory path as not existing")
	}
	setupAgentTemplateDataDir(t)
	// A regular file in place of the agents directory makes Stat fail with ENOTDIR.
	writeTestFile(t, filepath.Join(DataDir(), DirAgents), "not a dir")

	prev := &config.Config{Agents: []config.AgentConfig{{Name: "old", Model: "m"}}}
	next := &config.Config{Agents: []config.AgentConfig{{Name: "new", Model: "m"}}}

	err := RenameMatchingAgentDirs(prev, next)
	assert.ErrorContains(t, err, `stat old agent dir for "old"`)
}

func TestRenameMatchingAgentDirs_UnreadableTargetErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions are not enforced on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	setupAgentTemplateDataDir(t)
	require.NoError(t, os.MkdirAll(AgentDir("old"), 0o700))
	newDir := AgentDir("new")
	require.NoError(t, os.MkdirAll(newDir, 0o700))
	require.NoError(t, os.Chmod(newDir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(newDir, 0o700) })

	prev := &config.Config{Agents: []config.AgentConfig{{Name: "old", Model: "m"}}}
	next := &config.Config{Agents: []config.AgentConfig{{Name: "new", Model: "m"}}}

	err := RenameMatchingAgentDirs(prev, next)
	assert.ErrorContains(t, err, `stat new agent dir for "new"`)
	assert.DirExists(t, AgentDir("old"))
}
