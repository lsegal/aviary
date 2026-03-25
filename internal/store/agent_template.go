package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/support/templates"
)

const syncedByAviaryHeading = "## Synced by Aviary"
const syncedByAviaryComment = "<!-- This file is synced by Aviary"

// SyncAgentTemplate merges the embedded agent scaffold into an agent
// directory. Files that do not yet exist are created. Existing files are never
// deleted or replaced wholesale; for markdown files, only the "Synced by
// Aviary" section is updated when present in both source and destination.
func SyncAgentTemplate(agentID string) error {
	root := AgentDir(agentID)
	return fs.WalkDir(templates.Agent(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return os.MkdirAll(root, 0o700)
		}

		target := filepath.Join(root, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if filepath.Base(path) == ".gitkeep" {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}

		srcData, err := fs.ReadFile(templates.Agent(), path)
		if err != nil {
			return err
		}
		srcData = bytes.ReplaceAll(srcData, []byte("\r\n"), []byte("\n"))
		destData, err := os.ReadFile(target)
		if err != nil {
			if os.IsNotExist(err) {
				return os.WriteFile(target, srcData, 0o600)
			}
			return err
		}
		if len(destData) == 0 {
			return os.WriteFile(target, srcData, 0o600)
		}

		merged, changed := mergeTemplateFile(path, srcData, destData)
		if !changed {
			return nil
		}
		return os.WriteFile(target, merged, 0o600)
	})
}

// EnsureAgentTemplate is a compatibility wrapper for older callers.
func EnsureAgentTemplate(agentID string) error {
	return SyncAgentTemplate(agentID)
}

func mergeTemplateFile(path string, srcData, destData []byte) ([]byte, bool) {
	if !strings.EqualFold(filepath.Ext(path), ".md") {
		return destData, false
	}
	if strings.EqualFold(filepath.Base(path), "AGENTS.md") {
		merged, changed := replaceAgentsTemplatePrefix(string(destData), string(srcData))
		if !changed {
			return destData, false
		}
		return []byte(merged), true
	}
	merged, changed := replaceSyncedMarkdownSection(string(destData), string(srcData))
	if !changed {
		return destData, false
	}
	return []byte(merged), true
}

func replaceAgentsTemplatePrefix(dest, src string) (string, bool) {
	if !strings.Contains(dest, syncedByAviaryComment) {
		return dest, false
	}
	if dest == src {
		return dest, false
	}
	return src, true
}

func replaceSyncedMarkdownSection(dest, src string) (string, bool) {
	destStart, destEnd, destSection, ok := syncedMarkdownSection(dest)
	if !ok {
		return dest, false
	}
	_, _, srcSection, ok := syncedMarkdownSection(src)
	if !ok || destSection == srcSection {
		return dest, false
	}
	var buf bytes.Buffer
	buf.WriteString(dest[:destStart])
	buf.WriteString(srcSection)
	buf.WriteString(dest[destEnd:])
	return buf.String(), true
}

func syncedMarkdownSection(content string) (start, end int, section string, ok bool) {
	idx := strings.Index(content, syncedByAviaryHeading)
	if idx < 0 {
		return 0, 0, "", false
	}

	sectionEnd := len(content)
	afterHeading := idx + len(syncedByAviaryHeading)
	searchFrom := afterHeading
	if searchFrom < len(content) && content[searchFrom] == '\r' {
		searchFrom++
	}
	if searchFrom < len(content) && content[searchFrom] == '\n' {
		searchFrom++
	}

	for pos := searchFrom; pos < len(content); {
		lineEnd := strings.IndexByte(content[pos:], '\n')
		next := len(content)
		if lineEnd >= 0 {
			next = pos + lineEnd + 1
		}
		line := strings.TrimSpace(content[pos:next])
		if strings.HasPrefix(line, "#") && !strings.HasPrefix(line, syncedByAviaryHeading) {
			sectionEnd = pos
			break
		}
		if next <= pos {
			break
		}
		pos = next
	}

	return idx, sectionEnd, content[idx:sectionEnd], true
}

// EnsureNewAgentTemplates syncs the embedded scaffold for agents that are
// present in next but absent from prev.
func EnsureNewAgentTemplates(prev, next *config.Config) error {
	if next == nil {
		return nil
	}
	prevAgents := map[string]struct{}{}
	if prev != nil {
		for _, agent := range prev.Agents {
			prevAgents[agent.Name] = struct{}{}
		}
	}
	for _, agent := range next.Agents {
		if _, ok := prevAgents[agent.Name]; ok {
			continue
		}
		if err := SyncAgentTemplate(agent.Name); err != nil {
			return fmt.Errorf("syncing template for agent %q: %w", agent.Name, err)
		}
	}
	return nil
}

// RenameMatchingAgentDirs renames on-disk agent directories for agents whose
// config changed only by name between prev and next. Ambiguous matches are
// ignored, and genuinely new agents are left for template sync.
func RenameMatchingAgentDirs(prev, next *config.Config) error {
	if prev == nil || next == nil {
		return nil
	}

	prevByName := make(map[string]config.AgentConfig, len(prev.Agents))
	nextByName := make(map[string]config.AgentConfig, len(next.Agents))
	for _, agent := range prev.Agents {
		prevByName[agent.Name] = agent
	}
	for _, agent := range next.Agents {
		nextByName[agent.Name] = agent
	}

	removed := make([]config.AgentConfig, 0)
	added := make([]config.AgentConfig, 0)
	for _, agent := range prev.Agents {
		if _, ok := nextByName[agent.Name]; !ok {
			removed = append(removed, agent)
		}
	}
	for _, agent := range next.Agents {
		if _, ok := prevByName[agent.Name]; !ok {
			added = append(added, agent)
		}
	}

	type pair struct {
		from string
		to   string
	}
	pairs := make([]pair, 0)
	usedAdded := make(map[int]struct{})
	for _, oldAgent := range removed {
		matchIdx := -1
		oldKey, err := agentConfigRenameKey(oldAgent)
		if err != nil {
			return err
		}
		for idx, newAgent := range added {
			if _, used := usedAdded[idx]; used {
				continue
			}
			newKey, err := agentConfigRenameKey(newAgent)
			if err != nil {
				return err
			}
			if oldKey != newKey {
				continue
			}
			if matchIdx >= 0 {
				matchIdx = -1
				break
			}
			matchIdx = idx
		}
		if matchIdx < 0 {
			continue
		}
		usedAdded[matchIdx] = struct{}{}
		pairs = append(pairs, pair{from: oldAgent.Name, to: added[matchIdx].Name})
	}

	for _, rename := range pairs {
		oldDir := AgentDir(rename.from)
		newDir := AgentDir(rename.to)
		if oldDir == newDir {
			continue
		}

		if _, err := os.Stat(oldDir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("stat old agent dir for %q: %w", rename.from, err)
		}
		if _, err := os.Stat(newDir); err == nil {
			// If the target exists but appears to contain only scaffold/template
			// files (i.e. it was created by SyncAgentTemplate rather than
			// containing real user data), remove it so we can perform the
			// rename. If it contains any non-template files, skip the rename.
			isTemplateOnly, tErr := isTemplateOnlyDir(newDir)
			if tErr != nil {
				return fmt.Errorf("stat new agent dir for %q: %w", rename.to, tErr)
			}
			if !isTemplateOnly {
				continue
			}
			if err := os.RemoveAll(newDir); err != nil {
				return fmt.Errorf("remove template dir %q: %w", newDir, err)
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat new agent dir for %q: %w", rename.to, err)
		}
		if err := os.MkdirAll(filepath.Dir(newDir), 0o700); err != nil {
			return fmt.Errorf("create agents dir for %q: %w", rename.to, err)
		}
		if err := os.Rename(oldDir, newDir); err != nil {
			return fmt.Errorf("rename agent dir %q -> %q: %w", rename.from, rename.to, err)
		}
	}

	return nil
}

func agentConfigRenameKey(agent config.AgentConfig) (string, error) {
	// Build a canonical representation that ignores the agent name and
	// normalizes empty slices/maps so logically-identical configs compare equal
	// even when zero-valued fields differ (nil vs empty slice).
	type canonAgent struct {
		Model        string                    `json:"model,omitempty"`
		Fallbacks    []string                  `json:"fallbacks,omitempty"`
		Memory       string                    `json:"memory,omitempty"`
		MemoryTokens int                       `json:"memory_tokens,omitempty"`
		CompactKeep  int                       `json:"compact_keep,omitempty"`
		WorkingDir   string                    `json:"working_dir,omitempty"`
		Rules        string                    `json:"rules,omitempty"`
		Permissions  *config.PermissionsConfig `json:"permissions,omitempty"`
		Channels     []config.ChannelConfig    `json:"channels,omitempty"`
		Tasks        []config.TaskConfig       `json:"tasks,omitempty"`
	}

	c := canonAgent{
		Model:        agent.Model,
		Fallbacks:    agent.Fallbacks,
		Memory:       agent.Memory,
		MemoryTokens: agent.MemoryTokens,
		CompactKeep:  agent.CompactKeep,
		WorkingDir:   agent.WorkingDir,
		Rules:        agent.Rules,
		Permissions:  agent.Permissions,
		Channels:     agent.Channels,
		Tasks:        agent.Tasks,
	}

	// Normalize empty slices to nil so that [] vs nil don't affect equality.
	if len(c.Fallbacks) == 0 {
		c.Fallbacks = nil
	}
	if len(c.Channels) == 0 {
		c.Channels = nil
	}
	if len(c.Tasks) == 0 {
		c.Tasks = nil
	}

	data, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("marshal agent config: %w", err)
	}
	return string(data), nil
}

// isTemplateOnlyDir returns true when dir contains only the standard
// scaffold files and (at most) empty scaffold directories created by the
// embedded agent template. Any extra files cause it to return false.
func isTemplateOnlyDir(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}

	allowedFiles := map[string]struct{}{
		"AGENTS.md": {},
		"MEMORY.md": {},
		"RULES.md":  {},
	}
	allowedDirs := map[string]struct{}{
		"jobs":          {},
		"sessions":      {},
		"memory":        {},
		"task_compiles": {},
	}

	markerFound := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if _, ok := allowedDirs[name]; !ok {
				return false, nil
			}
			// Ensure the scaffold dir contains only .gitkeep or is empty.
			subs, err := os.ReadDir(filepath.Join(dir, name))
			if err != nil {
				return false, err
			}
			for _, se := range subs {
				if se.IsDir() {
					return false, nil
				}
				if se.Name() == ".gitkeep" {
					markerFound = true
					continue
				}
				// Any other file in the subdir disqualifies it as template-only.
				return false, nil
			}
		} else {
			if _, ok := allowedFiles[name]; !ok {
				return false, nil
			}
			markerFound = true
		}
	}
	return markerFound, nil
}
