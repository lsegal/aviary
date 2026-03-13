package store

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/support/templates"
)

const syncedByAviaryHeading = "## Synced by Aviary"

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
	merged, changed := replaceSyncedMarkdownSection(string(destData), string(srcData))
	if !changed {
		return destData, false
	}
	return []byte(merged), true
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
