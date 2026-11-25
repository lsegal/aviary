package store

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// TestSetDataDir_ConcurrentReads verifies that resetting the data and
// workspace dirs while other goroutines read them is race-free, as when a
// test's cleanup runs while agent checkpoint recovery is still reading paths.
func TestSetDataDir_ConcurrentReads(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() {
		SetDataDir("")
		SetWorkspaceDir("")
	})

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 100 {
				_ = DataDir()
				_ = WorkspaceDir()
			}
		})
	}
	for range 100 {
		SetDataDir(dir)
		SetWorkspaceDir(dir)
		SetDataDir("")
		SetWorkspaceDir("")
	}
	wg.Wait()

	SetDataDir(dir)
	SetWorkspaceDir(dir)
	assert.Equal(t, dir, DataDir())
	assert.Equal(t, dir, WorkspaceDir())
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

func TestAgentFiles(t *testing.T) {
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

	files, err := ListAgentFiles("assistant")
	assert.NoError(t, err)
	assert.Equal(t, []string{"IDENTITY.md", "MEMORY.md", "RULES.md", "notes/USER.md", "plain.txt"}, files)

	content, err := ReadAgentFile("assistant", "notes/USER.md")
	assert.NoError(t, err)
	assert.Equal(t, "user", content)

	content, err = ReadAgentFile("assistant", "RULES.md")
	assert.NoError(t, err)
	assert.Equal(t, "rules", content)

	content, err = ReadAgentFile("assistant", "plain.txt")
	assert.NoError(t, err)
	assert.Equal(t, "txt", content)

	_, err = ReadAgentFile("assistant", "../outside.md")
	assert.ErrorContains(t, err, "stay within")
}

func TestAgentFilesWriteDelete(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	agentDir := AgentDir("assistant")
	err := os.MkdirAll(filepath.Join(agentDir, "notes"), 0o700)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "RULES.md"), []byte("rules"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "AGENTS.md"), []byte("agents"), 0o600))

	// Write a root-level file.
	assert.NoError(t, WriteAgentFile("assistant", "PROFILE.txt", "profile"))
	content, err := ReadAgentFile("assistant", "PROFILE.txt")
	assert.NoError(t, err)
	assert.Equal(t, "profile", content)

	// Write a subdir file.
	assert.NoError(t, WriteAgentFile("assistant", "notes/summary.json", `{"summary":true}`))
	content, err = ReadAgentFile("assistant", "notes/summary.json")
	assert.NoError(t, err)
	assert.Equal(t, `{"summary":true}`, content)

	// Delete a regular file.
	assert.NoError(t, DeleteAgentFile("assistant", "PROFILE.txt"))
	_, err = ReadAgentFile("assistant", "PROFILE.txt")
	assert.Error(t, err)

	// Protected files cannot be deleted.
	err = DeleteAgentFile("assistant", "RULES.md")
	assert.ErrorContains(t, err, "protected")
	err = DeleteAgentFile("assistant", "AGENTS.md")
	assert.ErrorContains(t, err, "protected")

	// Traversal is rejected.
	err = WriteAgentFile("assistant", "../outside.txt", "x")
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

func TestReadAgentFile_StripsCommentLinesForMarkdown(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	agentDir := AgentDir("assistant")
	err := os.MkdirAll(agentDir, 0o700)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "IDENTITY.md"), []byte("visible\n<!-- hidden -->\nstill here\n"), 0o600))

	content, err := ReadAgentFile("assistant", "IDENTITY.md")
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

// TestSetAuthDir verifies a shared auth directory replaces SubDir(DirAuth)
// and is never created by EnsureDirs, which belongs to another data dir.
func TestSetAuthDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	shared := filepath.Join(tmp, "elsewhere", "auth")
	SetAuthDir(shared)
	t.Cleanup(func() { SetAuthDir("") })

	assert.Equal(t, shared, SubDir(DirAuth))
	assert.Equal(t, filepath.Join(tmp, "aviary", DirCerts), SubDir(DirCerts))

	assert.NoError(t, EnsureDirs())
	_, err := os.Stat(shared)
	assert.True(t, os.IsNotExist(err), "shared auth dir must not be created")
	_, err = os.Stat(filepath.Join(tmp, "aviary", DirAuth))
	assert.True(t, os.IsNotExist(err), "local auth dir is replaced by the shared one")

	SetAuthDir("")
	assert.Equal(t, filepath.Join(tmp, "aviary", DirAuth), SubDir(DirAuth))
}

func TestCLIHomesDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	assert.Equal(t, filepath.Join(tmp, "aviary", "cli-homes"), CLIHomesDir())
}

func TestWorkspaceDirDefaultsToWorkingDir(t *testing.T) {
	SetWorkspaceDir("")
	wd, err := os.Getwd()
	assert.NoError(t, err)
	assert.Equal(t, wd, WorkspaceDir())
}

func TestNormalizeAgentFileRejectsInvalidPaths(t *testing.T) {
	abs, err := filepath.Abs("x.md")
	assert.NoError(t, err)
	for _, tc := range []struct {
		file string
		want string
	}{
		{"   ", "file is required"},
		{".", "file is required"},
		{"notes/..", "file is required"},
		{abs, "relative to the agent directory"},
		{"..", "stay within"},
		{"../x/y.md", "stay within"},
	} {
		_, err := normalizeAgentFile(tc.file)
		assert.ErrorContains(t, err, tc.want, "file %q", tc.file)
	}
	got, err := normalizeAgentFile(" notes/./a/../b.md ")
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join("notes", "b.md"), got)
}

func TestRenameAgentFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	assert.NoError(t, WriteAgentFile("bot", "draft.txt", "hello"))
	assert.NoError(t, WriteAgentFile("bot", "taken.txt", "other"))

	// Moving into a new subdirectory creates it.
	assert.NoError(t, RenameAgentFile("bot", "draft.txt", "notes/final.txt"))
	content, err := ReadAgentFile("bot", "notes/final.txt")
	assert.NoError(t, err)
	assert.Equal(t, "hello", content)
	_, err = ReadAgentFile("bot", "draft.txt")
	assert.True(t, os.IsNotExist(err))

	// Renaming onto an existing file is refused and leaves both intact.
	err = RenameAgentFile("bot", "notes/final.txt", "taken.txt")
	assert.ErrorContains(t, err, "already exists")
	content, err = ReadAgentFile("bot", "taken.txt")
	assert.NoError(t, err)
	assert.Equal(t, "other", content)

	// Renaming to itself is a no-op.
	assert.NoError(t, RenameAgentFile("bot", "taken.txt", "./taken.txt"))

	// Protected root files cannot be the source or the destination.
	assert.ErrorContains(t, RenameAgentFile("bot", "RULES.md", "x.md"), "protected")
	assert.ErrorContains(t, RenameAgentFile("bot", "taken.txt", "memory.md"), "protected")

	// A missing source and invalid paths are errors.
	err = RenameAgentFile("bot", "missing.txt", "new.txt")
	assert.True(t, os.IsNotExist(err))
	assert.ErrorContains(t, RenameAgentFile("bot", "", "new.txt"), "required")
	assert.ErrorContains(t, RenameAgentFile("bot", "taken.txt", "../out.txt"), "stay within")
}

func TestListAgentFilesMissingAgent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	files, err := ListAgentFiles("nobody")
	assert.NoError(t, err)
	assert.Nil(t, files)
}

func TestReadAgentFileStripsMarkdownComments(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	assert.NoError(t, WriteAgentFile("bot", "NOTES.MD", "keep\n<!-- one-line -->\n<!--\nmulti\n-->\nend"))
	assert.NoError(t, WriteAgentFile("bot", "raw.txt", "<!-- kept -->"))

	content, err := ReadAgentFile("bot", "NOTES.MD")
	assert.NoError(t, err)
	assert.Equal(t, "keep\nend", content)

	content, err = ReadAgentFile("bot", "raw.txt")
	assert.NoError(t, err)
	assert.Equal(t, "<!-- kept -->", content)

	_, err = ReadAgentFile("bot", "absent.md")
	assert.True(t, os.IsNotExist(err))
	_, err = ReadAgentFile("bot", "")
	assert.ErrorContains(t, err, "required")
}

func TestTaskCompilePaths(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	p := TaskCompilePath("bot", "compile:1")
	assert.Equal(t, filepath.Join(tmp, "aviary", DirAgents, "bot", "task_compiles", "compile_1.json"), p)

	assert.Equal(t, "", FindTaskCompilePath("c1"))
	assert.Empty(t, AllTaskCompileDirs())

	found := TaskCompilePath("bot", "c1")
	assert.NoError(t, os.MkdirAll(filepath.Dir(found), 0o700))
	assert.NoError(t, os.WriteFile(found, []byte("{}"), 0o600))
	// An agent without task_compiles and a stray file are skipped.
	assert.NoError(t, os.MkdirAll(AgentDir("idle"), 0o700))
	assert.NoError(t, os.WriteFile(filepath.Join(tmp, "aviary", DirAgents, "stray"), nil, 0o600))

	assert.Equal(t, found, FindTaskCompilePath("c1"))
	assert.Equal(t, "", FindTaskCompilePath("c2"))
	assert.Equal(t, []string{filepath.Dir(found)}, AllTaskCompileDirs())
}

func TestSessionSidecarAndCheckpointPaths(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	agent := filepath.Join(tmp, "aviary", DirAgents, "bot")

	assert.Equal(t, filepath.Join(agent, "sessions", "signal_+1.meta.json"), SessionMetaPath("bot", "signal:+1"))
	assert.Equal(t, filepath.Join(agent, "checkpoints"), CheckpointDir("bot"))
	assert.Equal(t, filepath.Join(agent, "checkpoints", "run_1.json"), CheckpointPath("bot", "run/1"))
	assert.Equal(t, filepath.Join(agent, "checkpoints", "default.json"), CheckpointPath("bot", " "))
}

func TestFindSessionPathMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	assert.Equal(t, "", FindSessionPath("", "main"))
	assert.Equal(t, "", FindSessionPath("bot", "  "))
	assert.Equal(t, "", FindSessionPath("bot", "main"))
}

func TestFindJobPathLegacy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	legacy := filepath.Join(SubDir(DirJobs), "old.json")
	assert.NoError(t, os.MkdirAll(filepath.Dir(legacy), 0o700))
	assert.NoError(t, os.WriteFile(legacy, []byte("{}"), 0o600))
	assert.Equal(t, legacy, FindJobPath("old"))
}
