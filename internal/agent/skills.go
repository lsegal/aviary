package agent

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill represents a discovered SKILL.md file that can be used as a prompt prefix.
type Skill struct {
	Name        string // filename stem, e.g. "summarise"
	Description string // optional frontmatter description
	Content     string // markdown body without frontmatter
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
		skill, parseErr := parseSkillFile(name, data)
		if parseErr != nil {
			slog.Warn("skills: could not parse frontmatter", "path", path, "err", parseErr)
			skill = Skill{Name: name, Content: string(data)}
		}
		skills = append(skills, skill)
		slog.Info("skill discovered", "name", name, "path", path)
		return nil
	})
	return skills, err
}

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func parseSkillFile(defaultName string, data []byte) (Skill, error) {
	content := string(data)
	skill := Skill{Name: defaultName, Content: content}
	if !strings.HasPrefix(content, "---\n") {
		return skill, nil
	}

	rest := strings.TrimPrefix(content, "---\n")
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return skill, nil
	}

	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(rest[:idx]), &fm); err != nil {
		return Skill{}, err
	}

	if strings.TrimSpace(fm.Name) != "" {
		skill.Name = strings.TrimSpace(fm.Name)
	}
	skill.Description = strings.TrimSpace(fm.Description)
	skill.Content = strings.TrimSpace(rest[idx+5:])
	return skill, nil
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
		fmt.Fprintf(&sb, "<skill name=%q", s.Name)
		if s.Description != "" {
			fmt.Fprintf(&sb, " description=%q", sanitizeDelimitedContent(s.Description))
		}
		sb.WriteString(">\n")
		sb.WriteString(sanitizeDelimitedContent(s.Content))
		sb.WriteString("\n</skill>\n\n")
	}
	sb.WriteString(base)
	return sb.String()
}
