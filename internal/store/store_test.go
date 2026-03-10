package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
func TestScreenshotDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	got := ScreenshotDir()
	want := filepath.Join(DataDir(), "screenshots")
	assert.Equal(t, want, got)

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

// TestUsagePath verifies usage path format.
func TestUsagePath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	got := UsagePath()
	want := filepath.Join(SubDir(DirUsage), "usage.jsonl")
	assert.Equal(t, want, got)

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
