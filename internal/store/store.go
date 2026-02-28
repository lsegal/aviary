// Package store manages Aviary's file-based data directory.
package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// Directory name constants under DataDir().
const (
	DirJobs     = "jobs"
	DirSessions = "sessions"
	DirMemory   = "memory"
	DirCerts    = "certs"
	DirAuth     = "auth"
)

// DataDir returns the Aviary data directory.
// Respects XDG_CONFIG_HOME; falls back to ~/.config/aviary.
func DataDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "aviary")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "aviary")
}

// SubDir returns the full path to a named subdirectory under DataDir.
func SubDir(name string) string {
	return filepath.Join(DataDir(), name)
}

// EnsureDirs creates all required data subdirectories.
func EnsureDirs() error {
	dirs := []string{
		DataDir(),
		SubDir(DirJobs),
		SubDir(DirSessions),
		SubDir(DirMemory),
		SubDir(DirCerts),
		SubDir(DirAuth),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
	}
	return nil
}

// JobPath returns the file path for a job by ID.
func JobPath(id string) string {
	return filepath.Join(SubDir(DirJobs), id+".json")
}

// SessionPath returns the file path for a session log by ID.
func SessionPath(id string) string {
	return filepath.Join(SubDir(DirSessions), id+".jsonl")
}

// MemoryPath returns the file path for a memory pool by ID.
func MemoryPath(id string) string {
	return filepath.Join(SubDir(DirMemory), id+".jsonl")
}
