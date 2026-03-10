package skills

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lsegal/aviary/internal/config"
	"gopkg.in/yaml.v3"
)

//go:embed */SKILL.md
var builtinFS embed.FS

type Definition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Path        string `json:"path"`
	Installed   bool   `json:"installed"`
	Enabled     bool   `json:"enabled"`
	Source      string `json:"source"` // "builtin" or "disk"
}

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// InstalledDir returns the on-disk directory for user-installed skills.
func InstalledDir() string {
	return filepath.Join(config.BaseDir(), "skills")
}

// ListInstalled returns all bundled and on-disk skills.
// Bundled skills are loaded first, then overridden by on-disk skills of the same name.
func ListInstalled(cfg *config.Config) ([]Definition, error) {
	byName := map[string]Definition{}

	builtin, err := loadEmbedded()
	if err != nil {
		return nil, err
	}
	for _, sk := range builtin {
		byName[sk.Name] = markEnabled(sk, cfg)
	}

	disk, err := loadDisk(InstalledDir())
	if err != nil {
		return nil, err
	}
	for _, sk := range disk {
		byName[sk.Name] = markEnabled(sk, cfg)
	}

	out := make([]Definition, 0, len(byName))
	for _, sk := range byName {
		out = append(out, sk)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func markEnabled(sk Definition, cfg *config.Config) Definition {
	if cfg != nil && cfg.Skills != nil {
		if skillCfg, ok := cfg.Skills[sk.Name]; ok {
			sk.Enabled = skillCfg.Enabled
		}
	}
	return sk
}

func loadEmbedded() ([]Definition, error) {
	matches, err := fs.Glob(builtinFS, "*/SKILL.md")
	if err != nil {
		return nil, err
	}
	out := make([]Definition, 0, len(matches))
	for _, match := range matches {
		data, err := builtinFS.ReadFile(match)
		if err != nil {
			return nil, err
		}
		name := filepath.Base(filepath.Dir(match))
		sk, err := parseSkill(name, string(data))
		if err != nil {
			return nil, err
		}
		sk.Path = match
		sk.Installed = true
		sk.Source = "builtin"
		out = append(out, sk)
	}
	return out, nil
}

func loadDisk(root string) ([]Definition, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}

	var out []Definition
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !strings.EqualFold(info.Name(), "SKILL.md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		name := filepath.Base(filepath.Dir(path))
		sk, err := parseSkill(name, string(data))
		if err != nil {
			return nil
		}
		sk.Path = path
		sk.Installed = true
		sk.Source = "disk"
		out = append(out, sk)
		return nil
	})
	return out, err
}

func parseSkill(defaultName, raw string) (Definition, error) {
	sk := Definition{Name: defaultName, Content: strings.TrimSpace(raw)}
	if !strings.HasPrefix(raw, "---\n") {
		return sk, nil
	}
	rest := strings.TrimPrefix(raw, "---\n")
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return sk, nil
	}
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(rest[:idx]), &fm); err != nil {
		return Definition{}, err
	}
	if strings.TrimSpace(fm.Name) != "" {
		sk.Name = strings.TrimSpace(fm.Name)
	}
	sk.Description = strings.TrimSpace(fm.Description)
	sk.Content = strings.TrimSpace(rest[idx+5:])
	return sk, nil
}
