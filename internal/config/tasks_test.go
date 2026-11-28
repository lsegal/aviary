package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTaskFile writes content to dir/name and returns the full path.
func writeTaskFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// useTaskBaseDir points BaseDir at a fresh temp directory for the test.
func useTaskBaseDir(t *testing.T, base string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("AVIARY_CONFIG_BASE_DIR", base)
}

const badFrontmatter = "---\nname: [unclosed\n---\nbody\n"

func TestFindMarkdownTask(t *testing.T) {
	dir := t.TempDir()
	byStem := writeTaskFile(t, dir, "daily.md", "Run the daily job.")
	byFrontmatter := writeTaskFile(t, dir, "renamed.md", "---\nname: Weekly Report\n---\nSummarize.\n")
	writeTaskFile(t, dir, "_placeholder.md", "---\nname: hidden\n---\nignored\n")
	writeTaskFile(t, dir, "broken.md", badFrontmatter)
	writeTaskFile(t, dir, "notes.txt", "---\nname: text-only\n---\n")

	t.Run("matches filename stem", func(t *testing.T) {
		got, err := FindMarkdownTask(dir, "daily")
		require.NoError(t, err)
		assert.Equal(t, byStem, got)
	})

	t.Run("matches frontmatter name", func(t *testing.T) {
		got, err := FindMarkdownTask(dir, "Weekly Report")
		require.NoError(t, err)
		assert.Equal(t, byFrontmatter, got)
	})

	t.Run("ignores placeholders", func(t *testing.T) {
		_, err := FindMarkdownTask(dir, "hidden")
		assert.ErrorContains(t, err, `no task file defines "hidden"`)
	})

	t.Run("skips unparseable files", func(t *testing.T) {
		// broken.md would default to the name "broken" but fails to parse.
		_, err := FindMarkdownTask(dir, "broken")
		assert.ErrorContains(t, err, `no task file defines "broken"`)
	})

	t.Run("ignores non-markdown files", func(t *testing.T) {
		_, err := FindMarkdownTask(dir, "text-only")
		assert.Error(t, err)
	})

	t.Run("no match", func(t *testing.T) {
		_, err := FindMarkdownTask(dir, "missing")
		assert.ErrorContains(t, err, `no task file defines "missing" in `+dir)
	})

	t.Run("missing directory", func(t *testing.T) {
		_, err := FindMarkdownTask(filepath.Join(dir, "absent"), "daily")
		assert.ErrorContains(t, err, `no task file defines "daily"`)
	})

	t.Run("malformed glob pattern", func(t *testing.T) {
		_, err := FindMarkdownTask(filepath.Join(dir, "bad["), "daily")
		assert.ErrorIs(t, err, filepath.ErrBadPattern)
	})
}

func TestLoadAgentTaskFiles(t *testing.T) {
	t.Run("loads markdown tasks and skips placeholders and broken files", func(t *testing.T) {
		useTaskBaseDir(t, t.TempDir())
		agent := AgentConfig{Name: "bot"}
		dir := AgentTasksDir(agent)
		require.NoError(t, os.MkdirAll(dir, 0o750))
		writeTaskFile(t, dir, "alpha.md", "---\nschedule: 0 9 * * *\n---\nAlpha prompt.\n")
		writeTaskFile(t, dir, "beta.md", "Beta prompt.")
		writeTaskFile(t, dir, "_draft.md", "Draft prompt.")
		writeTaskFile(t, dir, "broken.md", badFrontmatter)

		tasks, err := LoadAgentTaskFiles(agent)
		require.NoError(t, err)
		require.Len(t, tasks, 2)
		assert.Equal(t, "alpha", tasks[0].Name)
		assert.Equal(t, "0 9 * * *", tasks[0].Schedule)
		assert.Equal(t, "Alpha prompt.", tasks[0].Prompt)
		assert.True(t, tasks[0].FromFile)
		assert.Equal(t, "beta", tasks[1].Name)
		assert.Equal(t, "Beta prompt.", tasks[1].Prompt)
	})

	t.Run("missing tasks directory", func(t *testing.T) {
		useTaskBaseDir(t, t.TempDir())
		tasks, err := LoadAgentTaskFiles(AgentConfig{Name: "nobody"})
		require.NoError(t, err)
		assert.Empty(t, tasks)
	})

	t.Run("malformed glob pattern", func(t *testing.T) {
		useTaskBaseDir(t, filepath.Join(t.TempDir(), "bad["))
		tasks, err := LoadAgentTaskFiles(AgentConfig{Name: "bot"})
		assert.ErrorIs(t, err, filepath.ErrBadPattern)
		assert.ErrorContains(t, err, "globbing task files")
		assert.Nil(t, tasks)
	})
}

func TestMergeTasksByName(t *testing.T) {
	base := []TaskConfig{
		{Name: "keep", Prompt: "config keep"},
		{Name: "override", Prompt: "config override"},
	}
	fileTasks := []TaskConfig{
		{Name: "override", Prompt: "file override", FromFile: true},
		{Name: "extra", Prompt: "file extra", FromFile: true},
	}

	merged := mergeTasksByName(base, fileTasks)
	assert.Equal(t, []TaskConfig{
		{Name: "keep", Prompt: "config keep"},
		{Name: "override", Prompt: "file override", FromFile: true},
		{Name: "extra", Prompt: "file extra", FromFile: true},
	}, merged)
	// The inputs are left untouched.
	assert.Equal(t, "config override", base[1].Prompt)

	assert.Empty(t, mergeTasksByName(nil, nil))
	assert.Equal(t, fileTasks, mergeTasksByName(nil, fileTasks))
	assert.Equal(t, base, mergeTasksByName(base, nil))
}

func TestTaskNameToFilename(t *testing.T) {
	cases := map[string]string{
		"daily":                 "daily",
		"  Daily Report  ":      "daily-report",
		"a/b\\c:d*e":            "a-b-c-d-e",
		"keep_under.dots-ok":    "keep_under.dots-ok",
		"many   spaces!!here":   "many-spaces-here",
		"--edge--":              "edge",
		"!!!":                   "task",
		"":                      "task",
		"Émoji 🚀 name":          "moji-name",
		"Release v1.2 (beta)?":  "release-v1.2-beta",
		"../../escape attempt":  "..-..-escape-attempt",
		"tab\tand\nnewline end": "tab-and-newline-end",
	}
	for in, want := range cases {
		assert.Equal(t, want, taskNameToFilename(in), "input %q", in)
	}
}

func TestLoadMarkdownTaskMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.md")
	_, err := LoadMarkdownTask(path)
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.ErrorContains(t, err, "reading task file")
}

func TestLoadMarkdownTaskInvalidFrontmatter(t *testing.T) {
	path := writeTaskFile(t, t.TempDir(), "broken.md", badFrontmatter)
	_, err := LoadMarkdownTask(path)
	assert.ErrorContains(t, err, "parsing task frontmatter")
}

func TestSaveMarkdownTaskSanitizesNameAndKeepsOriginal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "tasks")
	path, err := SaveMarkdownTask(dir, TaskConfig{Name: "Weekly Report: Q1/Q2", Prompt: "  Summarize.  "})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "weekly-report-q1-q2.md"), path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "name: 'Weekly Report: Q1/Q2'")
	assert.Contains(t, string(data), "\n---\n\nSummarize.\n")

	loaded, err := LoadMarkdownTask(path)
	require.NoError(t, err)
	assert.Equal(t, "Weekly Report: Q1/Q2", loaded.Name)
	assert.Equal(t, "Summarize.", loaded.Prompt)

	found, err := FindMarkdownTask(dir, "Weekly Report: Q1/Q2")
	require.NoError(t, err)
	assert.Equal(t, path, found)
}

func TestSaveMarkdownTaskEmptyPromptOmitsBody(t *testing.T) {
	dir := t.TempDir()
	path, err := SaveMarkdownTask(dir, TaskConfig{Name: "empty", Schedule: "@hourly"})
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "---\nschedule: '@hourly'\n---\n", string(data))
}

func TestSaveMarkdownTaskErrors(t *testing.T) {
	t.Run("tasks dir cannot be created", func(t *testing.T) {
		// A regular file where a parent directory should be makes MkdirAll
		// fail on every platform, unlike permission bits.
		blocker := writeTaskFile(t, t.TempDir(), "blocker", "")
		_, err := SaveMarkdownTask(filepath.Join(blocker, "tasks"), TaskConfig{Name: "x"})
		assert.ErrorContains(t, err, "creating tasks dir")
	})

	t.Run("task file cannot be written", func(t *testing.T) {
		// A directory occupying the target filename makes WriteFile fail.
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "x.md"), 0o750))
		_, err := SaveMarkdownTask(dir, TaskConfig{Name: "x", Prompt: "p"})
		assert.ErrorContains(t, err, "writing task file")
	})
}

func TestParseMarkdownTaskUnclosedFrontmatterIsPrompt(t *testing.T) {
	content := "---\nname: never-closed\nschedule: daily\n"
	task, err := ParseMarkdownTask("fallback", []byte(content))
	require.NoError(t, err)
	assert.Equal(t, "fallback", task.Name)
	assert.Equal(t, "", task.Schedule)
	assert.Equal(t, "---\nname: never-closed\nschedule: daily", task.Prompt)
	assert.True(t, task.FromFile)
}
