package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lsegal/aviary/internal/config"
)

// TestDataDir_XDG verifies that when XDG_CONFIG_HOME is set, DataDir returns
// $XDG_CONFIG_HOME/aviary.
func TestDataDir_XDG(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	got := DataDir()
	want := filepath.Join(tmp, "aviary")
	assert.Equal(t, want, got)

}

// TestDataDir_Fallback verifies that when XDG_CONFIG_HOME is empty, DataDir
// falls back to an isolated test directory instead of ~/.config/aviary.
func TestDataDir_Fallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")

	got := DataDir()
	assert.True(t, strings.Contains(got, "aviary"))
	home, err := os.UserHomeDir()
	if err == nil {
		assert.False(t, strings.HasPrefix(got, filepath.Join(home, ".config", "aviary")))
	}

}

// TestSubDir verifies that SubDir returns the correct path under DataDir.
func TestSubDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	tests := []struct {
		name string
	}{
		{DirJobs},
		{DirAgents},
		{DirCerts},
		{DirAuth},
		{DirMedia},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SubDir(tc.name)
			want := filepath.Join(tmp, "aviary", tc.name)
			assert.Equal(t, want, got)

		})
	}
}

// TestEnsureDirs_Success verifies that EnsureDirs creates all subdirectories.
func TestEnsureDirs_Success(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	err := EnsureDirs()
	assert.NoError(t, err)

	expected := []string{
		DataDir(),
		SubDir(DirAgents),
		SubDir(DirCerts),
		SubDir(DirAuth),
		SubDir(DirMedia),
	}
	for _, d := range expected {
		info, err := os.Stat(d)
		assert.NoError(t, err)
		if err != nil {
			continue
		}
		assert.True(t, info.IsDir())

	}
}

// TestEnsureDirs_Error verifies that EnsureDirs returns an error when a
// directory cannot be created (e.g. because a file is in the way).
func TestEnsureDirs_Error(t *testing.T) {
	tmp := t.TempDir()
	// Place a regular file where DataDir() expects a directory to be created.
	blocker := filepath.Join(tmp, "aviary")
	err := os.WriteFile(blocker, []byte("not a dir"), 0o600)
	assert.NoError(t, err)

	t.Setenv("XDG_CONFIG_HOME", tmp)

	err = EnsureDirs()
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "creating directory"))

}

// TestPathHelpers verifies path helper functions.
func TestPathHelpers(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	SetWorkspaceDir(tmp)
	t.Cleanup(func() { SetWorkspaceDir("") })

	base := filepath.Join(tmp, "aviary")

	t.Run("JobPath", func(t *testing.T) {
		got := JobPath("bot", "job-1")
		want := filepath.Join(base, DirAgents, "bot", "jobs", "job-1.json")
		assert.Equal(t, want, got)

	})

	t.Run("SessionPath", func(t *testing.T) {
		got := SessionPath("assistant", "main")
		want := filepath.Join(base, DirAgents, "assistant", "sessions", "main.jsonl")
		assert.Equal(t, want, got)

	})

	t.Run("SessionPath_plain_name", func(t *testing.T) {
		// agentID without "" prefix should also work.
		got := SessionPath("assistant", "main")
		want := filepath.Join(base, DirAgents, "assistant", "sessions", "main.jsonl")
		assert.Equal(t, want, got)

	})

	t.Run("FindSessionPath", func(t *testing.T) {
		// Setup dummy session file.
		agentName := "searcher"
		sessID := "main"
		path := SessionPath(""+agentName, sessID)
		err := os.MkdirAll(filepath.Dir(path), 0o700)
		assert.NoError(t, err)

		err = os.WriteFile(path, []byte("{}"), 0o600)
		assert.NoError(t, err)

		got := FindSessionPath(""+agentName, sessID)
		assert.Equal(t, path, got)

		// Non-prefixed sessID should also work.
		sess2 := "sess_123"
		path2 := SessionPath(""+agentName, sess2)
		err = os.WriteFile(path2, []byte("{}"), 0o600)
		assert.NoError(t, err)

		got2 := FindSessionPath(""+agentName, sess2)
		assert.Equal(t, path2, got2)

	})

	t.Run("SessionPath_channel_name_uses_underscores_on_disk", func(t *testing.T) {
		got := SessionPath("assistant", "signal:+12066439160")
		want := filepath.Join(base, DirAgents, "assistant", "sessions", "signal_+12066439160.jsonl")
		assert.Equal(t, want, got)
	})

	t.Run("AgentDir", func(t *testing.T) {
		got := AgentDir("researcher")
		want := filepath.Join(base, DirAgents, "researcher")
		assert.Equal(t, want, got)

	})

	t.Run("AgentRulesPath", func(t *testing.T) {
		got := AgentRulesPath("researcher")
		want := filepath.Join(base, DirAgents, "researcher", "RULES.md")
		assert.Equal(t, want, got)

	})

}

// TestSanitizeFileComponent verifies the sanitizer replaces forbidden chars and
// handles edge cases.
func TestSanitizeFileComponent(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"normal", "normal"},
		{"", "default"},
		{"   ", "default"},
		{"file<name>", "file_name_"},
		{"path/to/file", "path_to_file"},
		{"back\\slash", "back_slash"},
		{"pipe|char", "pipe_char"},
		{"question?mark", "question_mark"},
		{"star*glob", "star_glob"},
		{"colon:sep", "colon_sep"},
		{"quote\"char", "quote_char"},
		{"  spaces  ", "spaces"},
	}
	for _, tc := range tests {
		got := sanitizeFileComponent(tc.in)
		assert.Equal(t, tc.want, got)

	}
}

// TestAgentDirName verifies the agent_prefix stripping.
func TestAgentDirName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"foo", "foo"},
		{"assistant", "assistant"},
		{"foo", "foo"},
		{"", "default"},
	}
	for _, tc := range tests {
		got := agentDirName(tc.in)
		assert.Equal(t, tc.want, got)

	}
}

// TestFindJobPath verifies FindJobPath returns the correct path.
func TestFindJobPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Create a dummy job file.
	agentID := "finder"
	jobID := "job-abc"
	p := JobPath(agentID, jobID)
	err := os.MkdirAll(filepath.Dir(p), 0o700)
	assert.NoError(t, err)

	err = os.WriteFile(p, []byte("{}"), 0o600)
	assert.NoError(t, err)

	got := FindJobPath(jobID)
	assert.Equal(t, p, got)

	// Non-existent job should return "".
	got2 := FindJobPath("nonexistent-job")
	assert.Equal(t, "", got2)

}

// TestAllJobDirs verifies AllJobDirs includes agent job directories.
func TestAllJobDirs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Create jobs dirs for two agents.
	for _, agent := range []string{"a1", "a2"} {
		jobDir := filepath.Join(AgentDir(agent), "jobs")
		err := os.MkdirAll(jobDir, 0o700)
		assert.NoError(t, err)

	}

	dirs := AllJobDirs()
	assert.GreaterOrEqual(t, len(dirs), 2)

}

// TestAllJobDirs_Legacy verifies AllJobDirs includes the legacy jobs dir.
func TestAllJobDirs_Legacy(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Create only the legacy flat jobs directory.
	legacyDir := SubDir(DirJobs)
	err := os.MkdirAll(legacyDir, 0o700)
	assert.NoError(t, err)

	dirs := AllJobDirs()
	found := false
	for _, d := range dirs {
		if d == legacyDir {
			found = true
		}
	}
	assert.True(t, found)

}

// TestScreenshotDir verifies path format.
func TestMediaDirs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	assert.Equal(t, filepath.Join(DataDir(), "media", "browser"), ScreenshotDir())
	assert.Equal(t, filepath.Join(DataDir(), "media", "browser"), BrowserMediaDir())
	assert.Equal(t, filepath.Join(DataDir(), "media", "incoming", "slack"), IncomingMediaDir("slack"))
	assert.Equal(t, filepath.Join(DataDir(), "media", "outgoing", "signal"), OutgoingMediaDir("signal"))
}

// TestNotesPath verifies notes path is workspace-local.
func TestNotesPath(t *testing.T) {
	tmp := t.TempDir()
	SetWorkspaceDir(tmp)
	t.Cleanup(func() { SetWorkspaceDir("") })

	got := NotesPath("private:assistant")
	assert.Equal(t, filepath.Join(tmp, "MEMORY.md"), got)

	got2 := NotesPath("")
	assert.Equal(t, filepath.Join(tmp, "MEMORY.md"), got2)
}

func TestAgentMarkdownFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	agentDir := AgentDir("assistant")
	err := os.MkdirAll(filepath.Join(agentDir, "notes"), 0o700)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "IDENTITY.md"), []byte("id"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "MEMORY.md"), []byte("mem"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "notes", "USER.md"), []byte("user"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "RULES.md"), []byte("rules"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "plain.txt"), []byte("txt"), 0o600))

	files, err := ListAgentMarkdownFiles("assistant")
	assert.NoError(t, err)
	assert.Equal(t, []string{"IDENTITY.md", "MEMORY.md", "RULES.md", "notes/USER.md"}, files)

	content, err := ReadAgentMarkdownFile("assistant", "notes/USER.md")
	assert.NoError(t, err)
	assert.Equal(t, "user", content)

	content, err = ReadAgentMarkdownFile("assistant", "RULES.md")
	assert.NoError(t, err)
	assert.Equal(t, "rules", content)

	_, err = ReadAgentMarkdownFile("assistant", "../outside.md")
	assert.ErrorContains(t, err, "stay within")
	_, err = ReadAgentMarkdownFile("assistant", "plain.txt")
	assert.ErrorContains(t, err, "markdown")
}

func TestAgentMarkdownFilesWriteDelete(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	agentDir := AgentDir("assistant")
	err := os.MkdirAll(filepath.Join(agentDir, "notes"), 0o700)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "RULES.md"), []byte("rules"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "AGENTS.md"), []byte("agents"), 0o600))

	// Write a root-level file.
	assert.NoError(t, WriteAgentMarkdownFile("assistant", "PROFILE.md", "profile"))
	content, err := ReadAgentMarkdownFile("assistant", "PROFILE.md")
	assert.NoError(t, err)
	assert.Equal(t, "profile", content)

	// Write a subdir file.
	assert.NoError(t, WriteAgentMarkdownFile("assistant", "notes/summary.md", "summary"))
	content, err = ReadAgentMarkdownFile("assistant", "notes/summary.md")
	assert.NoError(t, err)
	assert.Equal(t, "summary", content)

	// Delete a regular file.
	assert.NoError(t, DeleteAgentMarkdownFile("assistant", "PROFILE.md"))
	_, err = ReadAgentMarkdownFile("assistant", "PROFILE.md")
	assert.Error(t, err)

	// Protected files cannot be deleted.
	err = DeleteAgentMarkdownFile("assistant", "RULES.md")
	assert.ErrorContains(t, err, "protected")
	err = DeleteAgentMarkdownFile("assistant", "AGENTS.md")
	assert.ErrorContains(t, err, "protected")

	// Traversal is rejected.
	err = WriteAgentMarkdownFile("assistant", "../outside.md", "x")
	assert.ErrorContains(t, err, "stay within")
}

func TestSyncAgentTemplate(t *testing.T) {
	tmp := t.TempDir()
	SetDataDir(filepath.Join(tmp, "aviary"))
	t.Cleanup(func() { SetDataDir("") })

	err := SyncAgentTemplate("assistant")
	assert.NoError(t, err)

	agentDir := AgentDir("assistant")
	assert.DirExists(t, filepath.Join(agentDir, "jobs"))
	assert.DirExists(t, filepath.Join(agentDir, "memory"))
	assert.DirExists(t, filepath.Join(agentDir, "sessions"))
	assert.FileExists(t, filepath.Join(agentDir, "MEMORY.md"))
	assert.FileExists(t, filepath.Join(agentDir, "AGENTS.md"))
	assert.FileExists(t, filepath.Join(agentDir, "RULES.md"))
	assert.NoFileExists(t, filepath.Join(agentDir, "jobs", ".gitkeep"))

	memoryContent, err := os.ReadFile(filepath.Join(agentDir, "MEMORY.md"))
	assert.NoError(t, err)
	assert.Equal(t, "# Persisent memory for this agent\n", string(memoryContent))

	rulesContent, err := os.ReadFile(filepath.Join(agentDir, "RULES.md"))
	assert.NoError(t, err)
	assert.Contains(t, string(rulesContent), "## Synced by Aviary")

	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "MEMORY.md"), []byte("custom"), 0o600))
	assert.NoError(t, SyncAgentTemplate("assistant"))

	memoryContent, err = os.ReadFile(filepath.Join(agentDir, "MEMORY.md"))
	assert.NoError(t, err)
	assert.Equal(t, "custom", string(memoryContent))
}

func TestSyncAgentTemplate_DoesNotDeleteExtraFiles(t *testing.T) {
	tmp := t.TempDir()
	SetDataDir(filepath.Join(tmp, "aviary"))
	t.Cleanup(func() { SetDataDir("") })

	agentDir := AgentDir("assistant")
	assert.NoError(t, os.MkdirAll(agentDir, 0o700))
	customPath := filepath.Join(agentDir, "CUSTOM.md")
	assert.NoError(t, os.WriteFile(customPath, []byte("keep me"), 0o600))

	assert.NoError(t, SyncAgentTemplate("assistant"))
	assert.FileExists(t, customPath)
}

func TestSyncAgentTemplate_AddsMissingFiles(t *testing.T) {
	tmp := t.TempDir()
	SetDataDir(filepath.Join(tmp, "aviary"))
	t.Cleanup(func() { SetDataDir("") })

	agentDir := AgentDir("assistant")
	assert.NoError(t, os.MkdirAll(agentDir, 0o700))

	assert.NoError(t, SyncAgentTemplate("assistant"))
	assert.FileExists(t, filepath.Join(agentDir, "MEMORY.md"))
	assert.FileExists(t, filepath.Join(agentDir, "AGENTS.md"))
	assert.FileExists(t, filepath.Join(agentDir, "RULES.md"))
}

func TestRenameMatchingAgentDirs(t *testing.T) {
	tmp := t.TempDir()
	SetDataDir(filepath.Join(tmp, "aviary"))
	t.Cleanup(func() { SetDataDir("") })

	oldDir := AgentDir("old-name")
	assert.NoError(t, os.MkdirAll(filepath.Join(oldDir, "sessions"), 0o700))
	assert.NoError(t, os.WriteFile(filepath.Join(oldDir, "MEMORY.md"), []byte("custom memory"), 0o600))

	prev := &config.Config{
		Agents: []config.AgentConfig{{
			Name:   "old-name",
			Model:  "anthropic/claude-sonnet-4-5",
			Memory: "private",
		}},
	}
	next := &config.Config{
		Agents: []config.AgentConfig{{
			Name:   "new-name",
			Model:  "anthropic/claude-sonnet-4-5",
			Memory: "private",
		}},
	}

	assert.NoError(t, RenameMatchingAgentDirs(prev, next))

	newDir := AgentDir("new-name")
	assert.NoDirExists(t, oldDir)
	assert.FileExists(t, filepath.Join(newDir, "MEMORY.md"))
	content, err := os.ReadFile(filepath.Join(newDir, "MEMORY.md"))
	assert.NoError(t, err)
	assert.Equal(t, "custom memory", string(content))
}

func TestRenameMatchingAgentDirs_SkipsWhenTargetExists(t *testing.T) {
	tmp := t.TempDir()
	SetDataDir(filepath.Join(tmp, "aviary"))
	t.Cleanup(func() { SetDataDir("") })

	oldDir := AgentDir("old-name")
	newDir := AgentDir("new-name")
	assert.NoError(t, os.MkdirAll(oldDir, 0o700))
	assert.NoError(t, os.MkdirAll(newDir, 0o700))

	prev := &config.Config{Agents: []config.AgentConfig{{Name: "old-name", Model: "m"}}}
	next := &config.Config{Agents: []config.AgentConfig{{Name: "new-name", Model: "m"}}}

	assert.NoError(t, RenameMatchingAgentDirs(prev, next))
	assert.DirExists(t, oldDir)
	assert.DirExists(t, newDir)
}

func TestSyncAgentTemplate_ReplacesEmptyExistingFile(t *testing.T) {
	tmp := t.TempDir()
	SetDataDir(filepath.Join(tmp, "aviary"))
	t.Cleanup(func() { SetDataDir("") })

	agentDir := AgentDir("assistant")
	assert.NoError(t, os.MkdirAll(agentDir, 0o700))
	rulesPath := filepath.Join(agentDir, "RULES.md")
	assert.NoError(t, os.WriteFile(rulesPath, nil, 0o600))

	assert.NoError(t, SyncAgentTemplate("assistant"))

	rulesContent, err := os.ReadFile(rulesPath)
	assert.NoError(t, err)
	assert.Contains(t, string(rulesContent), "## Synced by Aviary")
}

func TestSyncAgentTemplate_ReplacesOnlySyncedMarkdownSection(t *testing.T) {
	tmp := t.TempDir()
	SetDataDir(filepath.Join(tmp, "aviary"))
	t.Cleanup(func() { SetDataDir("") })

	agentDir := AgentDir("assistant")
	assert.NoError(t, os.MkdirAll(agentDir, 0o700))
	dest := strings.Join([]string{
		"# Rules the agent must follow",
		"",
		"Custom intro",
		"",
		"## Synced by Aviary",
		"",
		"- old synced line",
		"",
		"## Local Notes",
		"",
		"- keep this local change",
		"",
	}, "\n")
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "RULES.md"), []byte(dest), 0o600))

	assert.NoError(t, SyncAgentTemplate("assistant"))

	rulesContent, err := os.ReadFile(filepath.Join(agentDir, "RULES.md"))
	assert.NoError(t, err)
	content := string(rulesContent)
	assert.Contains(t, content, "Custom intro")
	assert.Contains(t, content, "## Local Notes")
	assert.Contains(t, content, "- keep this local change")
	assert.Contains(t, content, "Never share sensitive information")
	assert.NotContains(t, content, "- old synced line")
}

func TestSyncAgentTemplate_PreservesAGENTSWithoutSyncComment(t *testing.T) {
	tmp := t.TempDir()
	SetDataDir(filepath.Join(tmp, "aviary"))
	t.Cleanup(func() { SetDataDir("") })

	agentDir := AgentDir("assistant")
	assert.NoError(t, os.MkdirAll(agentDir, 0o700))
	dest := strings.Join([]string{
		"# Old AGENTS",
		"",
		"Custom stale preface",
		"",
	}, "\n")
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "AGENTS.md"), []byte(dest), 0o600))

	assert.NoError(t, SyncAgentTemplate("assistant"))

	agentsContent, err := os.ReadFile(filepath.Join(agentDir, "AGENTS.md"))
	assert.NoError(t, err)
	content := string(agentsContent)
	assert.Contains(t, content, "Custom stale preface")
	assert.NotContains(t, content, "# AGENTS.md - Your Workspace")
}

func TestSyncAgentTemplate_OverwritesAGENTSWhenSyncCommentPresent(t *testing.T) {
	tmp := t.TempDir()
	SetDataDir(filepath.Join(tmp, "aviary"))
	t.Cleanup(func() { SetDataDir("") })

	agentDir := AgentDir("assistant")
	assert.NoError(t, os.MkdirAll(agentDir, 0o700))
	dest := strings.Join([]string{
		"# Old AGENTS",
		"",
		"<!-- This file is synced by Aviary, remove this line to disable syncing -->",
		"",
		"Custom stale content",
		"",
	}, "\n")
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "AGENTS.md"), []byte(dest), 0o600))

	assert.NoError(t, SyncAgentTemplate("assistant"))

	agentsContent, err := os.ReadFile(filepath.Join(agentDir, "AGENTS.md"))
	assert.NoError(t, err)
	content := string(agentsContent)
	assert.Contains(t, content, "# AGENTS.md - Your Workspace")
	assert.NotContains(t, content, "Custom stale content")
}

func TestStripMarkdownCommentLines(t *testing.T) {
	got := StripMarkdownCommentLines(strings.Join([]string{
		"# Title",
		"<!-- hidden -->",
		"Visible",
		"  <!--",
		"still hidden",
		"-->",
		"Done",
	}, "\n"))

	assert.Equal(t, strings.Join([]string{
		"# Title",
		"Visible",
		"Done",
	}, "\n"), got)
}

func TestReadAgentMarkdownFile_StripsCommentLines(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	agentDir := AgentDir("assistant")
	err := os.MkdirAll(agentDir, 0o700)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "IDENTITY.md"), []byte("visible\n<!-- hidden -->\nstill here\n"), 0o600))

	content, err := ReadAgentMarkdownFile("assistant", "IDENTITY.md")
	assert.NoError(t, err)
	assert.Equal(t, "visible\nstill here\n", content)
}

// TestUsagePath verifies usage path format.
func TestUsagePath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	got := UsagePath()
	want := filepath.Join(SubDir(DirUsage), "usage.jsonl")
	assert.Equal(t, want, got)

}

func TestWorkspaceDir(t *testing.T) {
	tmp := t.TempDir()
	SetWorkspaceDir(tmp)
	t.Cleanup(func() { SetWorkspaceDir("") })

	assert.Equal(t, tmp, WorkspaceDir())
}

// TestIntegration_StoreSetup exercises DataDir + SubDir + EnsureDirs together,
// then verifies that path helpers return paths inside the created data directory.
func TestIntegration_StoreSetup(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	err := EnsureDirs()
	assert.NoError(t, err)

	dataDir := DataDir()
	for _, p := range []string{
		JobPath("bot", "j1"),
		SessionPath("bot", "bot-main"),
	} {
		assert.True(t, strings.HasPrefix(p, dataDir))

	}

	// Directories created by EnsureDirs must exist.
	for _, dir := range []string{
		SubDir(DirAgents),
	} {
		_, err := os.Stat(dir)
		assert.NoError(t, err)

	}
}
