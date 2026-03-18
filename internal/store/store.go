// Package store manages Aviary's file-based data directory.
package store

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	DirMemory   = "memory"   // deprecated: memory now lives under agents/<name>/memory/
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

var protectedAgentRootMarkdownFiles = map[string]struct{}{
	"MEMORY.MD": {},
	"RULES.MD":  {},
	"SYSTEM.MD": {},
}

func isProtectedAgentRootMarkdownFile(file string) bool {
	_, ok := protectedAgentRootMarkdownFiles[strings.ToUpper(file)]
	return ok
}

func normalizeAgentRootMarkdownFile(file string) (string, error) {
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
	if filepath.Base(clean) != clean {
		return "", fmt.Errorf("file must be in the root of the agent directory")
	}
	if !strings.EqualFold(filepath.Ext(clean), ".md") {
		return "", fmt.Errorf("file must be a markdown file")
	}
	return clean, nil
}

// ListAgentRootMarkdownFiles returns root-level markdown files under an agent
// directory, including built-in files like RULES.md and MEMORY.md.
func ListAgentRootMarkdownFiles(agentID string) ([]string, error) {
	root := AgentDir(agentID)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.EqualFold(filepath.Ext(name), ".md") {
			continue
		}
		files = append(files, name)
	}
	slices.Sort(files)
	return files, nil
}

// ReadAgentRootMarkdownFile reads a root-level markdown file from an agent
// directory without stripping comment lines.
func ReadAgentRootMarkdownFile(agentID, file string) (string, error) {
	clean, err := normalizeAgentRootMarkdownFile(file)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(AgentDir(agentID), clean))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteAgentRootMarkdownFile creates or replaces a root-level markdown file
// under an agent directory.
func WriteAgentRootMarkdownFile(agentID, file, content string) error {
	clean, err := normalizeAgentRootMarkdownFile(file)
	if err != nil {
		return err
	}
	root := AgentDir(agentID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, clean), []byte(content), 0o600)
}

// DeleteAgentRootMarkdownFile deletes a root-level markdown file from an agent
// directory, except for protected built-in files.
func DeleteAgentRootMarkdownFile(agentID, file string) error {
	clean, err := normalizeAgentRootMarkdownFile(file)
	if err != nil {
		return err
	}
	if isProtectedAgentRootMarkdownFile(clean) {
		return fmt.Errorf("%s is protected and cannot be deleted", clean)
	}
	return os.Remove(filepath.Join(AgentDir(agentID), clean))
}

// ListAgentMarkdownFiles returns markdown files under an agent directory,
// excluding RULES.md which is handled as prompt preamble instead of ad hoc
// context. Returned paths are slash-delimited and relative to the agent dir.
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
		if strings.EqualFold(name, "RULES.md") {
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

// ReadAgentMarkdownFile reads a markdown file from an agent directory, excluding
// RULES.md. file must be a relative path beneath the agent directory.
func ReadAgentMarkdownFile(agentID, file string) (string, error) {
	file = strings.TrimSpace(file)
	if file == "" {
		return "", fmt.Errorf("file is required")
	}
	if !strings.EqualFold(filepath.Ext(file), ".md") {
		return "", fmt.Errorf("file must be a markdown file")
	}
	if strings.EqualFold(filepath.Base(file), "RULES.md") {
		return "", fmt.Errorf("RULES.md is loaded automatically and cannot be read via agent_file_read")
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
	name := sessionID
	// Strip agent prefix if it exists in the session ID to avoid redundant filenames.
	// e.g. agentID="agent_assistant", sessionID="agent_assistant-main" -> "main.jsonl"
	agentName := strings.TrimPrefix(agentID, "agent_")
	prefixes := []string{"agent_" + agentName + "-", agentName + "-"}
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			name = strings.TrimPrefix(name, p)
			break
		}
	}
	return filepath.Join(AgentDir(agentID), "sessions", encodeSessionName(name)+".jsonl")
}

// FindSessionPath scans all known agent directories and returns the full path
// for the first session file matching sessionID.  Returns "" when not found.
// FindSessionPath locates the .jsonl file for sessionID by scanning agent
// directories. An optional agentNameHint causes that agent's directory to be
// tried first (case-insensitive), which avoids returning the wrong file when
// multiple agents share the same session name (e.g. "main").
func FindSessionPath(sessionID string, agentNameHint ...string) string {
	agentsDir := filepath.Join(DataDir(), DirAgents)
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return ""
	}

	tryAgent := func(agentName string) string {
		prefixes := []string{"agent_" + agentName + "-", agentName + "-"}
		for _, p := range prefixes {
			if strings.HasPrefix(sessionID, p) {
				stripped := strings.TrimPrefix(sessionID, p)
				p2 := filepath.Join(agentsDir, agentName, "sessions", encodeSessionName(stripped)+".jsonl")
				if _, err := os.Stat(p2); err == nil {
					return p2
				}
			}
		}
		p := filepath.Join(agentsDir, agentName, "sessions", encodeSessionName(sessionID)+".jsonl")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return ""
	}

	// When a hint is provided, try that agent first.
	if len(agentNameHint) > 0 && agentNameHint[0] != "" {
		hint := agentNameHint[0]
		for _, e := range entries {
			if e.IsDir() && strings.EqualFold(e.Name(), hint) {
				if p := tryAgent(e.Name()); p != "" {
					return p
				}
				break
			}
		}
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if len(agentNameHint) > 0 && strings.EqualFold(e.Name(), agentNameHint[0]) {
			continue // already tried above
		}
		if p := tryAgent(e.Name()); p != "" {
			return p
		}
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

// NotesDir returns the workspace-local notes directory: <workspace>/notes.
func NotesDir() string {
	return filepath.Join(WorkspaceDir(), "notes")
}

// WorkspaceNotePath returns the workspace-local path for a markdown note file.
// The provided name may include a ".md" suffix and/or a leading "notes/" segment.
func WorkspaceNotePath(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "notes/")
	name = strings.TrimPrefix(name, "notes\\")
	name = strings.TrimSuffix(name, ".md")
	name = sanitizeFileComponent(name)
	return filepath.Join(NotesDir(), name+".md")
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

// encodeSessionName encodes a session name for use as a filename component.
// Unlike sanitizeFileComponent, this encoding is reversible: use
// decodeSessionName to recover the original name.
func encodeSessionName(name string) string {
	if name == "" {
		return "default"
	}
	var b strings.Builder
	for _, c := range name {
		switch c {
		case '%':
			b.WriteString("%25")
		case '<':
			b.WriteString("%3C")
		case '>':
			b.WriteString("%3E")
		case ':':
			b.WriteString("%3A")
		case '"':
			b.WriteString("%22")
		case '/':
			b.WriteString("%2F")
		case '\\':
			b.WriteString("%5C")
		case '|':
			b.WriteString("%7C")
		case '?':
			b.WriteString("%3F")
		case '*':
			b.WriteString("%2A")
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// DecodeSessionName reverses encodeSessionName to recover the original session name.
func DecodeSessionName(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '%' && i+2 < len(s) {
			hi := unhex(s[i+1])
			lo := unhex(s[i+2])
			if hi >= 0 && lo >= 0 {
				b.WriteByte(byte(hi<<4 | lo))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func unhex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	}
	return -1
}
