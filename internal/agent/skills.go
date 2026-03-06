package agent

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Skill represents a discovered SKILL.md file that can be used as a prompt prefix.
type Skill struct {
	Name    string // filename stem, e.g. "summarise"
	Content string // full markdown content
}

// DiscoverSkills walks dir and all subdirectories looking for SKILL.md files.
// Each SKILL.md is returned as a Skill with Name derived from its parent directory.
func DiscoverSkills(dir string) ([]Skill, error) {
	var skills []Skill
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable dirs
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(info.Name(), "SKILL.md") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			slog.Warn("skills: could not read", "path", path, "err", readErr)
			return nil
		}
		name := filepath.Base(filepath.Dir(path))
		if name == "." || name == string(os.PathSeparator) {
			name = "default"
		}
		skills = append(skills, Skill{Name: name, Content: string(data)})
		slog.Info("skill discovered", "name", name, "path", path)
		return nil
	})
	return skills, err
}

// sanitizeDelimitedContent escapes "</" as "&lt;/" so that embedded content
// cannot close its surrounding XML-style delimiter tag and inject prompt text.
func sanitizeDelimitedContent(s string) string {
	return strings.ReplaceAll(s, "</", "&lt;/")
}

// BuildSystemPrompt prepends all skill contents to a base system prompt.
func BuildSystemPrompt(base string, skills []Skill) string {
	if len(skills) == 0 {
		return base
	}
	var sb strings.Builder
	for _, s := range skills {
		sb.WriteString(fmt.Sprintf("<skill name=%q>\n%s\n</skill>\n\n", s.Name, sanitizeDelimitedContent(s.Content)))
	}
	sb.WriteString(base)
	return sb.String()
}
