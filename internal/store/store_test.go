package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDataDir_XDG verifies that when XDG_CONFIG_HOME is set, DataDir returns
// $XDG_CONFIG_HOME/aviary.
func TestDataDir_XDG(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	got := DataDir()
	want := filepath.Join(tmp, "aviary")
	if got != want {
		t.Errorf("DataDir() = %q; want %q", got, want)
	}
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
	if got != want {
		t.Errorf("DataDir() = %q; want %q", got, want)
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SubDir(tc.name)
			want := filepath.Join(tmp, "aviary", tc.name)
			if got != want {
				t.Errorf("SubDir(%q) = %q; want %q", tc.name, got, want)
			}
		})
	}
}

// TestEnsureDirs_Success verifies that EnsureDirs creates all subdirectories.
func TestEnsureDirs_Success(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	if err := EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() returned unexpected error: %v", err)
	}

	expected := []string{
		DataDir(),
		SubDir(DirAgents),
		SubDir(DirCerts),
		SubDir(DirAuth),
	}
	for _, d := range expected {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("expected directory %q to exist, got error: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %q to be a directory", d)
		}
	}
}

// TestEnsureDirs_Error verifies that EnsureDirs returns an error when a
// directory cannot be created (e.g. because a file is in the way).
func TestEnsureDirs_Error(t *testing.T) {
	tmp := t.TempDir()
	// Place a regular file where DataDir() expects a directory to be created.
	blocker := filepath.Join(tmp, "aviary")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", tmp)

	err := EnsureDirs()
	if err == nil {
		t.Fatal("EnsureDirs() expected error but got nil")
	}
	if !strings.Contains(err.Error(), "creating directory") {
		t.Errorf("error message should contain 'creating directory', got: %v", err)
	}
}

// TestPathHelpers verifies path helper functions.
func TestPathHelpers(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	base := filepath.Join(tmp, "aviary")

	t.Run("JobPath", func(t *testing.T) {
		got := JobPath("agent_bot", "job-1")
		want := filepath.Join(base, DirAgents, "bot", "jobs", "job-1.json")
		if got != want {
			t.Errorf("JobPath(%q) = %q; want %q", "job-1", got, want)
		}
	})

	t.Run("SessionPath", func(t *testing.T) {
		got := SessionPath("agent_assistant", "agent_assistant-main")
		want := filepath.Join(base, DirAgents, "assistant", "sessions", "agent_assistant-main.jsonl")
		if got != want {
			t.Errorf("SessionPath = %q; want %q", got, want)
		}
	})

	t.Run("SessionPath_plain_name", func(t *testing.T) {
		// agentID without "agent_" prefix should also work.
		got := SessionPath("assistant", "agent_assistant-main")
		want := filepath.Join(base, DirAgents, "assistant", "sessions", "agent_assistant-main.jsonl")
		if got != want {
			t.Errorf("SessionPath (plain) = %q; want %q", got, want)
		}
	})

	t.Run("MemoryPath_typed", func(t *testing.T) {
		got := MemoryPath("private:assistant")
		want := filepath.Join(base, DirAgents, "assistant", "memory", "private.jsonl")
		if got != want {
			t.Errorf("MemoryPath(%q) = %q; want %q", "private:assistant", got, want)
		}
	})

	t.Run("AgentDir", func(t *testing.T) {
		got := AgentDir("agent_researcher")
		want := filepath.Join(base, DirAgents, "researcher")
		if got != want {
			t.Errorf("AgentDir = %q; want %q", got, want)
		}
	})

	t.Run("AgentRulesPath", func(t *testing.T) {
		got := AgentRulesPath("agent_researcher")
		want := filepath.Join(base, DirAgents, "researcher", "rules.md")
		if got != want {
			t.Errorf("AgentRulesPath = %q; want %q", got, want)
		}
	})
}

// TestIntegration_StoreSetup exercises DataDir + SubDir + EnsureDirs together,
// then verifies that path helpers return paths inside the created data directory.
func TestIntegration_StoreSetup(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	if err := EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs(): %v", err)
	}

	dataDir := DataDir()
	for _, p := range []string{
		JobPath("agent_bot", "j1"),
		SessionPath("agent_bot", "agent_bot-main"),
		MemoryPath("private:bot"),
	} {
		if !strings.HasPrefix(p, dataDir) {
			t.Errorf("path %q is not under DataDir %q", p, dataDir)
		}
	}

	// Directories created by EnsureDirs must exist.
	for _, dir := range []string{
		SubDir(DirAgents),
	} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("directory %q should exist after EnsureDirs: %v", dir, err)
		}
	}
}

