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
		{DirSessions},
		{DirMemory},
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
		SubDir(DirJobs),
		SubDir(DirSessions),
		SubDir(DirMemory),
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
	// DataDir() = tmp/aviary, so we create tmp/aviary as a file.
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

// TestPathHelpers verifies the three path helper functions.
func TestPathHelpers(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	base := filepath.Join(tmp, "aviary")

	tests := []struct {
		fn   func(string) string
		dir  string
		ext  string
		id   string
		want string
	}{
		{JobPath, DirJobs, ".json", "job-1", "job-1"},
		{SessionPath, DirSessions, ".jsonl", "session-abc", "session-abc"},
		{MemoryPath, DirMemory, ".jsonl", "mem-xyz", "mem-xyz"},
		{MemoryPath, DirMemory, ".jsonl", "private:default", "private_default"},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			got := tc.fn(tc.id)
			want := filepath.Join(base, tc.dir, tc.want+tc.ext)
			if got != want {
				t.Errorf("path helper(%q) = %q; want %q", tc.id, got, want)
			}
		})
	}
}

// TestIntegration_StoreSetup exercises DataDir + SubDir + EnsureDirs together,
// then verifies that path helpers return paths inside the created directories.
func TestIntegration_StoreSetup(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// EnsureDirs must not fail.
	if err := EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs(): %v", err)
	}

	// All path helpers should produce paths that are children of DataDir.
	dataDir := DataDir()
	for _, p := range []string{
		JobPath("j1"),
		SessionPath("s1"),
		MemoryPath("m1"),
	} {
		if !strings.HasPrefix(p, dataDir) {
			t.Errorf("path %q is not under DataDir %q", p, dataDir)
		}
	}

	// The directories referenced by the path helpers must already exist after
	// EnsureDirs().
	for _, dir := range []string{
		SubDir(DirJobs),
		SubDir(DirSessions),
		SubDir(DirMemory),
	} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("directory %q should exist after EnsureDirs: %v", dir, err)
		}
	}
}
