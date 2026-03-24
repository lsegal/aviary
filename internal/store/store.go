// Package store manages Aviary's file-based data directory.
package store

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/lsegal/aviary/internal/testenv"
)

// Top-level directory name constants under DataDir().
const (
	DirAgents = "agents" // per-agent subdirectories
	DirCerts  = "certs"
	DirAuth   = "auth"
	DirMedia  = "media"
	DirUsage  = "usage"

	// Deprecated: legacy flat directories kept for backward-compat migration.
	DirJobs     = "jobs"     // deprecated: jobs now live under agents/<name>/jobs/
	DirSessions = "sessions" // deprecated: sessions now live under agents/<name>/sessions/
)

var customDataDir string
var customWorkspaceDir string

// SetDataDir overrides the directory returned by DataDir.
// Pass an empty string to restore automatic resolution via XDG_CONFIG_HOME or ~/.config.
// This is intended for the --data-dir CLI flag and for tests.
func SetDataDir(dir string) { customDataDir = dir }

// SetWorkspaceDir overrides the directory returned by WorkspaceDir.
// Pass an empty string to restore automatic resolution via the process working directory.
// This is intended for tests and tools that need repo-local artifacts.
func SetWorkspaceDir(dir string) { customWorkspaceDir = dir }

// DataDir returns the Aviary data directory.
// Resolution order: SetDataDir value > XDG_CONFIG_HOME > ~/.config/aviary.
func DataDir() string {
	if customDataDir != "" {
		return customDataDir
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "aviary")
	}
	if testHome := testenv.GoTestConfigHome(); testHome != "" {
		return filepath.Join(testHome, "aviary")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "aviary")
}

// WorkspaceDir returns the current workspace root.
// Resolution order: SetWorkspaceDir value > process working directory.
func WorkspaceDir() string {
	if customWorkspaceDir != "" {
		return customWorkspaceDir
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// SubDir returns the full path to a named subdirectory under DataDir.
func SubDir(name string) string {
	return filepath.Join(DataDir(), name)
}

// agentDirName converts an agent ID into the directory component used under
// agents/.
func agentDirName(agentID string) string {
	return sanitizeFileComponent(agentID)
}

// AgentDir returns the per-agent directory path: <datadir>/agents/<name>/.
func AgentDir(agentID string) string {
	return filepath.Join(DataDir(), DirAgents, agentDirName(agentID))
}

// AgentRulesPath returns the path for an agent's RULES.md file.
func AgentRulesPath(agentID string) string {
	return filepath.Join(AgentDir(agentID), "RULES.md")
}

var protectedAgentRootMarkdownFiles = map[string]struct{}{
	"AGENTS.MD": {},
	"MEMORY.MD": {},
	"RULES.MD":  {},
	"SYSTEM.MD": {},
}

func isProtectedAgentRootMarkdownFile(file string) bool {
	_, ok := protectedAgentRootMarkdownFiles[strings.ToUpper(file)]
	return ok
}

// normalizeAgentMarkdownFile validates and cleans a relative markdown file path,
// allowing subdirectories. Returns the cleaned path.
func normalizeAgentMarkdownFile(file string) (string, error) {
	file = strings.TrimSpace(file)
	if file == "" {
		return "", fmt.Errorf("file is required")
	}
	if filepath.IsAbs(file) {
		return "", fmt.Errorf("file must be relative to the agent directory")
	}
	clean := filepath.Clean(file)
	if clean == "." || clean == "" {
		return "", fmt.Errorf("file is required")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file must stay within the agent directory")
	}
	if !strings.EqualFold(filepath.Ext(clean), ".md") {
		return "", fmt.Errorf("file must be a markdown file")
	}
	return clean, nil
}

// WriteAgentMarkdownFile creates or replaces a markdown file under an agent
// directory. file may be a relative path including subdirectories (e.g. notes/foo.md).
func WriteAgentMarkdownFile(agentID, file, content string) error {
	clean, err := normalizeAgentMarkdownFile(file)
	if err != nil {
		return err
	}
	path := filepath.Join(AgentDir(agentID), clean)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// DeleteAgentMarkdownFile deletes a markdown file from an agent directory.
// Root-level protected files (AGENTS.md, MEMORY.md, RULES.md, SYSTEM.md) cannot be deleted.
func DeleteAgentMarkdownFile(agentID, file string) error {
	clean, err := normalizeAgentMarkdownFile(file)
	if err != nil {
		return err
	}
	// Only protect root-level built-ins.
	if filepath.Base(clean) == clean && isProtectedAgentRootMarkdownFile(clean) {
		return fmt.Errorf("%s is protected and cannot be deleted", clean)
	}
	return os.Remove(filepath.Join(AgentDir(agentID), clean))
}

// ListAgentMarkdownFiles returns all markdown files under an agent directory,
// recursively including subdirectories. Returned paths are slash-delimited
// and relative to the agent dir.
func ListAgentMarkdownFiles(agentID string) ([]string, error) {
	root := AgentDir(agentID)
	entries := []string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.EqualFold(filepath.Ext(name), ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

// ReadAgentMarkdownFile reads a markdown file from an agent directory.
// file must be a relative path beneath the agent directory.
func ReadAgentMarkdownFile(agentID, file string) (string, error) {
	file = strings.TrimSpace(file)
	if file == "" {
		return "", fmt.Errorf("file is required")
	}
	if !strings.EqualFold(filepath.Ext(file), ".md") {
		return "", fmt.Errorf("file must be a markdown file")
	}
	if filepath.IsAbs(file) {
		return "", fmt.Errorf("file must be relative to the agent directory")
	}

	root := AgentDir(agentID)
	clean := filepath.Clean(file)
	if clean == "." || clean == "" {
		return "", fmt.Errorf("file is required")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file must stay within the agent directory")
	}

	path := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file must stay within the agent directory")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return StripMarkdownCommentLines(string(data)), nil
}

// StripMarkdownCommentLines removes HTML comment blocks from markdown when the
// content is being fed back into prompts or prompt-adjacent tools.
func StripMarkdownCommentLines(content string) string {
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	inComment := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if inComment {
			if strings.Contains(trimmed, "-->") {
				inComment = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			if !strings.Contains(trimmed, "-->") {
				inComment = true
			}
			continue
		}
		out = append(out, line)
	}

	return strings.Join(out, "\n")
}

// EnsureDirs creates all required data subdirectories.
func EnsureDirs() error {
	dirs := []string{
		DataDir(),
		SubDir(DirAgents),
		SubDir(DirCerts),
		SubDir(DirAuth),
		SubDir(DirMedia),
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
	return filepath.Join(AgentDir(agentID), "jobs", sanitizeFileComponent(id)+".json")
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

// TaskCompilePath returns the path for a task compile record under the agent's
// task_compiles directory: <datadir>/agents/<agentID>/task_compiles/<id>.json.
func TaskCompilePath(agentID, id string) string {
	return filepath.Join(AgentDir(agentID), "task_compiles", sanitizeFileComponent(id)+".json")
}

// FindTaskCompilePath scans all known agent directories and returns the full
// path for the first task compile record matching id. Returns "" when not found.
func FindTaskCompilePath(id string) string {
	agentsDir := filepath.Join(DataDir(), DirAgents)
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(agentsDir, e.Name(), "task_compiles", id+".json")
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// AllTaskCompileDirs returns all existing agents/<name>/task_compiles/
// directory paths for bulk compile record enumeration.
func AllTaskCompileDirs() []string {
	agentsDir := filepath.Join(DataDir(), DirAgents)
	var dirs []string
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			d := filepath.Join(agentsDir, e.Name(), "task_compiles")
			if _, err := os.Stat(d); err == nil {
				dirs = append(dirs, d)
			}
		}
	}
	return dirs
}

// MediaDir returns the root directory for persisted media artifacts.
func MediaDir() string {
	return SubDir(DirMedia)
}

// BrowserMediaDir returns the directory where browser screenshots are saved.
// Browser artifacts are global resources (browser is not per-agent).
func BrowserMediaDir() string {
	return filepath.Join(MediaDir(), "browser")
}

// IncomingMediaDir returns the directory where inbound channel media is saved.
func IncomingMediaDir(channelType string) string {
	return filepath.Join(MediaDir(), "incoming", sanitizeFileComponent(channelType))
}

// OutgoingMediaDir returns the directory where outbound channel media is saved.
func OutgoingMediaDir(channelType string) string {
	return filepath.Join(MediaDir(), "outgoing", sanitizeFileComponent(channelType))
}

// ScreenshotDir returns the directory where browser screenshots are saved.
// Deprecated: use BrowserMediaDir.
func ScreenshotDir() string {
	return BrowserMediaDir()
}

// SessionChannelsPath returns the path for the session's channel delivery
// config file: <datadir>/agents/<agentID>/sessions/<sessionID>.channels.json.
// It shares the same naming logic as SessionPath.
func SessionChannelsPath(agentID, sessionID string) string {
	p := SessionPath(agentID, sessionID)
	return strings.TrimSuffix(p, ".jsonl") + ".channels.json"
}

// SessionMetaPath returns the path for the sidecar metadata file for a session.
// It mirrors SessionPath but uses a .meta.json extension.
func SessionMetaPath(agentID, sessionID string) string {
	p := SessionPath(agentID, sessionID)
	return strings.TrimSuffix(p, ".jsonl") + ".meta.json"
}

// SessionPath returns the path for the given session file under the agent's
// sessions directory: <datadir>/agents/<agentID>/sessions/<sessionID>.jsonl.
func SessionPath(agentID, sessionID string) string {
	return filepath.Join(AgentDir(agentID), "sessions", sanitizeFileComponent(sessionID)+".jsonl")
}

// FindSessionPath locates the .jsonl file for sessionID under the given agent.
// Returns "" when not found.
func FindSessionPath(agentID, sessionID string) string {
	if strings.TrimSpace(agentID) == "" || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	p := SessionPath(agentID, sessionID)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// NotesPath returns the path for the human-editable markdown notes file in the
// current workspace directory.
func NotesPath(_ string) string {
	return filepath.Join(WorkspaceDir(), "MEMORY.md")
}

// UsagePath returns the path to the global usage log file.
func UsagePath() string {
	return filepath.Join(SubDir(DirUsage), "usage.jsonl")
}

// CheckpointDir returns the directory where pending run checkpoints are stored
// for an agent: <datadir>/agents/<agentID>/checkpoints/.
func CheckpointDir(agentID string) string {
	return filepath.Join(AgentDir(agentID), "checkpoints")
}

// CheckpointPath returns the path for a specific run checkpoint file:
// <datadir>/agents/<agentID>/checkpoints/<checkpointID>.json.
func CheckpointPath(agentID, checkpointID string) string {
	return filepath.Join(CheckpointDir(agentID), sanitizeFileComponent(checkpointID)+".json")
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
