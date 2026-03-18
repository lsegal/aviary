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
// falls back to ~/.config/aviary.
func TestDataDir_Fallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir:", err)
	}

	got := DataDir()
	want := filepath.Join(home, ".config", "aviary")
	assert.Equal(t, want, got)

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
		got := JobPath("agent_bot", "job-1")
		want := filepath.Join(base, DirAgents, "bot", "jobs", "job-1.json")
		assert.Equal(t, want, got)

	})

	t.Run("SessionPath", func(t *testing.T) {
		got := SessionPath("agent_assistant", "agent_assistant-main")
		want := filepath.Join(base, DirAgents, "assistant", "sessions", "main.jsonl")
		assert.Equal(t, want, got)

	})

	t.Run("SessionPath_plain_name", func(t *testing.T) {
		// agentID without "agent_" prefix should also work.
		got := SessionPath("assistant", "agent_assistant-main")
		want := filepath.Join(base, DirAgents, "assistant", "sessions", "main.jsonl")
		assert.Equal(t, want, got)

	})

	t.Run("FindSessionPath", func(t *testing.T) {
		// Setup dummy session file.
		agentName := "searcher"
		sessID := "agent_searcher-main"
		path := SessionPath("agent_"+agentName, sessID)
		err := os.MkdirAll(filepath.Dir(path), 0o700)
		assert.NoError(t, err)

		err = os.WriteFile(path, []byte("{}"), 0o600)
		assert.NoError(t, err)

		got := FindSessionPath(sessID)
		assert.Equal(t, path, got)

		// Non-prefixed sessID should also work.
		sess2 := "sess_123"
		path2 := SessionPath("agent_"+agentName, sess2)
		err = os.WriteFile(path2, []byte("{}"), 0o600)
		assert.NoError(t, err)

		got2 := FindSessionPath(sess2)
		assert.Equal(t, path2, got2)

	})

	t.Run("MemoryPath_typed", func(t *testing.T) {
		got := MemoryPath("private:assistant")
		want := filepath.Join(base, DirAgents, "assistant", "memory", "private.jsonl")
		assert.Equal(t, want, got)

	})

	t.Run("AgentDir", func(t *testing.T) {
		got := AgentDir("agent_researcher")
		want := filepath.Join(base, DirAgents, "researcher")
		assert.Equal(t, want, got)

	})

	t.Run("AgentRulesPath", func(t *testing.T) {
		got := AgentRulesPath("agent_researcher")
		want := filepath.Join(base, DirAgents, "researcher", "RULES.md")
		assert.Equal(t, want, got)

	})

	t.Run("WorkspaceNotePath", func(t *testing.T) {
		got := WorkspaceNotePath("notes/project plan.md")
		want := filepath.Join(tmp, "notes", "project plan.md")
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
		{"agent_foo", "foo"},
		{"agent_assistant", "assistant"},
		{"foo", "foo"},
		{"agent_", "default"},
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
	agentID := "agent_finder"
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
	for _, agent := range []string{"agent_a1", "agent_a2"} {
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

// TestNotesPath verifies notes path format.
func TestNotesPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	got := NotesPath("private:assistant")
	want := filepath.Join(DataDir(), DirAgents, "assistant", "MEMORY.md")
	assert.Equal(t, want, got)

	// Without colon: fallback to default.
	got2 := NotesPath("standalone")
	want2 := filepath.Join(DataDir(), DirAgents, "default", "MEMORY.md")
	assert.Equal(t, want2, got2)

}

func TestAgentMarkdownFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	agentDir := AgentDir("agent_assistant")
	err := os.MkdirAll(filepath.Join(agentDir, "notes"), 0o700)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "IDENTITY.md"), []byte("id"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "MEMORY.md"), []byte("mem"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "notes", "USER.md"), []byte("user"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "RULES.md"), []byte("rules"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "plain.txt"), []byte("txt"), 0o600))

	files, err := ListAgentMarkdownFiles("agent_assistant")
	assert.NoError(t, err)
	assert.Equal(t, []string{"IDENTITY.md", "MEMORY.md", "notes/USER.md"}, files)

	content, err := ReadAgentMarkdownFile("agent_assistant", "notes/USER.md")
	assert.NoError(t, err)
	assert.Equal(t, "user", content)

	_, err = ReadAgentMarkdownFile("agent_assistant", "RULES.md")
	assert.ErrorContains(t, err, "loaded automatically")
	_, err = ReadAgentMarkdownFile("agent_assistant", "../outside.md")
	assert.ErrorContains(t, err, "stay within")
	_, err = ReadAgentMarkdownFile("agent_assistant", "plain.txt")
	assert.ErrorContains(t, err, "markdown")
}

func TestAgentRootMarkdownFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	agentDir := AgentDir("agent_assistant")
	err := os.MkdirAll(filepath.Join(agentDir, "notes"), 0o700)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "MEMORY.md"), []byte("memory"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "AGENTS.md"), []byte("agents"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "RULES.md"), []byte("rules"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "SYSTEM.md"), []byte("system"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "IDENTITY.md"), []byte("identity"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "notes", "USER.md"), []byte("user"), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "plain.txt"), []byte("txt"), 0o600))

	files, err := ListAgentRootMarkdownFiles("agent_assistant")
	assert.NoError(t, err)
	assert.Equal(t, []string{"AGENTS.md", "IDENTITY.md", "MEMORY.md", "RULES.md", "SYSTEM.md"}, files)

	content, err := ReadAgentRootMarkdownFile("agent_assistant", "SYSTEM.md")
	assert.NoError(t, err)
	assert.Equal(t, "system", content)

	assert.NoError(t, WriteAgentRootMarkdownFile("agent_assistant", "PROFILE.md", "profile"))
	content, err = ReadAgentRootMarkdownFile("agent_assistant", "PROFILE.md")
	assert.NoError(t, err)
	assert.Equal(t, "profile", content)

	assert.NoError(t, DeleteAgentRootMarkdownFile("agent_assistant", "PROFILE.md"))
	_, err = ReadAgentRootMarkdownFile("agent_assistant", "PROFILE.md")
	assert.Error(t, err)

	err = DeleteAgentRootMarkdownFile("agent_assistant", "RULES.md")
	assert.ErrorContains(t, err, "protected")
	err = DeleteAgentRootMarkdownFile("agent_assistant", "AGENTS.md")
	assert.ErrorContains(t, err, "protected")
	_, err = ReadAgentRootMarkdownFile("agent_assistant", "notes/USER.md")
	assert.ErrorContains(t, err, "root")
	err = WriteAgentRootMarkdownFile("agent_assistant", "../outside.md", "x")
	assert.ErrorContains(t, err, "stay within")
}

func TestSyncAgentTemplate(t *testing.T) {
	tmp := t.TempDir()
	SetDataDir(filepath.Join(tmp, "aviary"))
	t.Cleanup(func() { SetDataDir("") })

	err := SyncAgentTemplate("agent_assistant")
	assert.NoError(t, err)

	agentDir := AgentDir("agent_assistant")
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
	assert.NoError(t, SyncAgentTemplate("agent_assistant"))

	memoryContent, err = os.ReadFile(filepath.Join(agentDir, "MEMORY.md"))
	assert.NoError(t, err)
	assert.Equal(t, "custom", string(memoryContent))
}

func TestSyncAgentTemplate_DoesNotDeleteExtraFiles(t *testing.T) {
	tmp := t.TempDir()
	SetDataDir(filepath.Join(tmp, "aviary"))
	t.Cleanup(func() { SetDataDir("") })

	agentDir := AgentDir("agent_assistant")
	assert.NoError(t, os.MkdirAll(agentDir, 0o700))
	customPath := filepath.Join(agentDir, "CUSTOM.md")
	assert.NoError(t, os.WriteFile(customPath, []byte("keep me"), 0o600))

	assert.NoError(t, SyncAgentTemplate("agent_assistant"))
	assert.FileExists(t, customPath)
}

func TestSyncAgentTemplate_AddsMissingFiles(t *testing.T) {
	tmp := t.TempDir()
	SetDataDir(filepath.Join(tmp, "aviary"))
	t.Cleanup(func() { SetDataDir("") })

	agentDir := AgentDir("agent_assistant")
	assert.NoError(t, os.MkdirAll(agentDir, 0o700))

	assert.NoError(t, SyncAgentTemplate("agent_assistant"))
	assert.FileExists(t, filepath.Join(agentDir, "MEMORY.md"))
	assert.FileExists(t, filepath.Join(agentDir, "AGENTS.md"))
	assert.FileExists(t, filepath.Join(agentDir, "RULES.md"))
}

func TestRenameMatchingAgentDirs(t *testing.T) {
	tmp := t.TempDir()
	SetDataDir(filepath.Join(tmp, "aviary"))
	t.Cleanup(func() { SetDataDir("") })

	oldDir := AgentDir("agent_old-name")
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

	newDir := AgentDir("agent_new-name")
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

	oldDir := AgentDir("agent_old-name")
	newDir := AgentDir("agent_new-name")
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

	agentDir := AgentDir("agent_assistant")
	assert.NoError(t, os.MkdirAll(agentDir, 0o700))
	rulesPath := filepath.Join(agentDir, "RULES.md")
	assert.NoError(t, os.WriteFile(rulesPath, nil, 0o600))

	assert.NoError(t, SyncAgentTemplate("agent_assistant"))

	rulesContent, err := os.ReadFile(rulesPath)
	assert.NoError(t, err)
	assert.Contains(t, string(rulesContent), "## Synced by Aviary")
}

func TestSyncAgentTemplate_ReplacesOnlySyncedMarkdownSection(t *testing.T) {
	tmp := t.TempDir()
	SetDataDir(filepath.Join(tmp, "aviary"))
	t.Cleanup(func() { SetDataDir("") })

	agentDir := AgentDir("agent_assistant")
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

	assert.NoError(t, SyncAgentTemplate("agent_assistant"))

	rulesContent, err := os.ReadFile(filepath.Join(agentDir, "RULES.md"))
	assert.NoError(t, err)
	content := string(rulesContent)
	assert.Contains(t, content, "Custom intro")
	assert.Contains(t, content, "## Local Notes")
	assert.Contains(t, content, "- keep this local change")
	assert.Contains(t, content, "Never share sensitive information")
	assert.NotContains(t, content, "- old synced line")
}

func TestSyncAgentTemplate_ReplacesAGENTSContentBeforeMakeItYours(t *testing.T) {
	tmp := t.TempDir()
	SetDataDir(filepath.Join(tmp, "aviary"))
	t.Cleanup(func() { SetDataDir("") })

	agentDir := AgentDir("agent_assistant")
	assert.NoError(t, os.MkdirAll(agentDir, 0o700))
	dest := strings.Join([]string{
		"# Old AGENTS",
		"",
		"Custom stale preface",
		"",
		"## Make It Yours",
		"",
		"My local convention",
		"- keep this",
		"",
	}, "\n")
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "AGENTS.md"), []byte(dest), 0o600))

	assert.NoError(t, SyncAgentTemplate("agent_assistant"))

	agentsContent, err := os.ReadFile(filepath.Join(agentDir, "AGENTS.md"))
	assert.NoError(t, err)
	content := string(agentsContent)
	assert.Contains(t, content, "# AGENTS.md - Your Workspace")
	assert.Contains(t, content, "## Make It Yours")
	assert.Contains(t, content, "My local convention")
	assert.Contains(t, content, "- keep this")
	assert.NotContains(t, content, "Custom stale preface")
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

	agentDir := AgentDir("agent_assistant")
	err := os.MkdirAll(agentDir, 0o700)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(agentDir, "IDENTITY.md"), []byte("visible\n<!-- hidden -->\nstill here\n"), 0o600))

	content, err := ReadAgentMarkdownFile("agent_assistant", "IDENTITY.md")
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

func TestWorkspaceDirAndNotesDir(t *testing.T) {
	tmp := t.TempDir()
	SetWorkspaceDir(tmp)
	t.Cleanup(func() { SetWorkspaceDir("") })

	assert.Equal(t, tmp, WorkspaceDir())
	assert.Equal(t, filepath.Join(tmp, "notes"), NotesDir())
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
		JobPath("agent_bot", "j1"),
		SessionPath("agent_bot", "agent_bot-main"),
		MemoryPath("private:bot"),
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
