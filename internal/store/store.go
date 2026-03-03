// Package store manages Aviary's file-based data directory.
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Top-level directory name constants under DataDir().
const (
	DirAgents = "agents" // per-agent subdirectories
	DirCerts  = "certs"
	DirAuth   = "auth"
	DirUsage  = "usage"

	// Deprecated: legacy flat directories kept for backward-compat migration.
	DirJobs     = "jobs"     // deprecated: jobs now live under agents/<name>/jobs/
	DirSessions = "sessions" // deprecated: sessions now live under agents/<name>/sessions/
	DirMemory   = "memory"   // deprecated: memory now lives under agents/<name>/memory/
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

// agentDirName converts an agent ID ("agent_foo" or plain "foo") into the
// directory component used under agents/.
func agentDirName(agentID string) string {
	name := strings.TrimPrefix(agentID, "agent_")
	return sanitizeFileComponent(name)
}

// AgentDir returns the per-agent directory path: <datadir>/agents/<name>/.
// agentID may be a full ID like "agent_assistant" or just the name "assistant".
func AgentDir(agentID string) string {
	return filepath.Join(DataDir(), DirAgents, agentDirName(agentID))
}

// AgentRulesPath returns the path for an agent's RULES.md file.
func AgentRulesPath(agentID string) string {
	return filepath.Join(AgentDir(agentID), "RULES.md")
}

// EnsureDirs creates all required data subdirectories.
func EnsureDirs() error {
	dirs := []string{
		DataDir(),
		SubDir(DirAgents),
		SubDir(DirCerts),
		SubDir(DirAuth),
		SubDir(DirUsage),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
	}
	return nil
}

// JobPath returns the path for a job file under the agent's jobs directory:
// <datadir>/agents/<agentID>/jobs/<id>.json.
func JobPath(agentID, id string) string {
	return filepath.Join(AgentDir(agentID), "jobs", id+".json")
}

// FindJobPath scans all known agent directories and returns the full path
// for the first job file matching id. Returns "" when not found.
func FindJobPath(id string) string {
	agentsDir := filepath.Join(DataDir(), DirAgents)
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(agentsDir, e.Name(), "jobs", id+".json")
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	// Fallback: legacy flat jobs directory.
	p := filepath.Join(SubDir(DirJobs), id+".json")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// AllJobDirs returns all existing agents/<name>/jobs/ directory paths,
// plus the legacy jobs/ directory if it exists, for bulk job enumeration.
func AllJobDirs() []string {
	agentsDir := filepath.Join(DataDir(), DirAgents)
	var dirs []string
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			d := filepath.Join(agentsDir, e.Name(), "jobs")
			if _, err := os.Stat(d); err == nil {
				dirs = append(dirs, d)
			}
		}
	}
	// Legacy fallback.
	if legacy := SubDir(DirJobs); func() bool {
		_, err := os.Stat(legacy)
		return err == nil
	}() {
		dirs = append(dirs, legacy)
	}
	return dirs
}

// ScreenshotDir returns the directory where browser screenshots are saved.
// Screenshots are a global resource (browser is not per-agent).
func ScreenshotDir() string {
	return filepath.Join(DataDir(), "screenshots")
}

// SessionPath returns the path for the given session file under the agent's
// sessions directory: <datadir>/agents/<agentID>/sessions/<sessionID>.jsonl.
func SessionPath(agentID, sessionID string) string {
	return filepath.Join(AgentDir(agentID), "sessions", sessionID+".jsonl")
}

// FindSessionPath scans all known agent directories and returns the full path
// for the first session file matching sessionID.  Returns "" when not found.
func FindSessionPath(sessionID string) string {
	agentsDir := filepath.Join(DataDir(), DirAgents)
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(agentsDir, e.Name(), "sessions", sessionID+".jsonl")
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	// Fallback: legacy flat sessions directory.
	p := filepath.Join(SubDir(DirSessions), sessionID+".jsonl")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// MemoryPath returns the path for a memory pool file.
// Pool IDs follow the format "type:agentname" (e.g. "private:assistant"),
// which maps to <datadir>/agents/<agentname>/memory/<type>.jsonl.
func MemoryPath(poolID string) string {
	if i := strings.Index(poolID, ":"); i >= 0 {
		poolType := sanitizeFileComponent(poolID[:i])
		agentName := sanitizeFileComponent(poolID[i+1:])
		return filepath.Join(DataDir(), DirAgents, agentName, "memory", poolType+".jsonl")
	}
	// Fallback for pool IDs without a colon.
	return filepath.Join(DataDir(), DirAgents, "default", "memory", sanitizeFileComponent(poolID)+".jsonl")
}

// NotesPath returns the path for the human-editable markdown notes file for a
// memory pool. Pool IDs follow the same format as MemoryPath.
// e.g. "private:assistant" → <datadir>/agents/assistant/MEMORY.md
func NotesPath(poolID string) string {
	if i := strings.Index(poolID, ":"); i >= 0 {
		agentName := sanitizeFileComponent(poolID[i+1:])
		return filepath.Join(DataDir(), DirAgents, agentName, "MEMORY.md")
	}
	return filepath.Join(DataDir(), DirAgents, "default", "MEMORY.md")
}

// UsagePath returns the path to the global usage log file.
func UsagePath() string {
	return filepath.Join(SubDir(DirUsage), "usage.jsonl")
}

func sanitizeFileComponent(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "default"
	}

	r := strings.NewReplacer(
		"<", "_",
		">", "_",
		":", "_",
		"\"", "_",
		"/", "_",
		"\\", "_",
		"|", "_",
		"?", "_",
		"*", "_",
	)
	out := r.Replace(s)
	out = strings.TrimSpace(out)
	if out == "" {
		return "default"
	}
	return out
}
