// Package skills provides discovery and loading for builtin and installed skills.
package skills

import (
	"embed"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lsegal/aviary/internal/config"
)

//go:embed */*
var builtinFS embed.FS

// Definition describes a skill that can be listed or installed.
type Definition struct {
	Name           string                `json:"name"`
	Description    string                `json:"description"`
	Content        string                `json:"content"`
	Path           string                `json:"path"`
	Installed      bool                  `json:"installed"`
	Enabled        bool                  `json:"enabled"`
	Source         string                `json:"source"` // "builtin" or "disk"
	SettingsSchema map[string]any        `json:"settings_schema,omitempty"`
	Runtime        *RuntimeConfiguration `json:"-"`
}

type manifest struct {
	Runtime  *RuntimeConfiguration `json:"runtime,omitempty"`
	Settings map[string]any        `json:"settings,omitempty"`
}

// RuntimeConfiguration describes how a skill directory registers an executable MCP tool.
type RuntimeConfiguration struct {
	Type                    string            `json:"type,omitempty"`
	Binary                  string            `json:"binary,omitempty"`
	BinarySetting           string            `json:"binary_setting,omitempty"`
	Args                    []string          `json:"args,omitempty"`
	StripArgs               []string          `json:"strip_args,omitempty"`
	StripValueFlags         []string          `json:"strip_value_flags,omitempty"`
	StripArgPrefixes        []string          `json:"strip_arg_prefixes,omitempty"`
	TopLevelSkipValueFlags  []string          `json:"top_level_skip_value_flags,omitempty"`
	TopLevelSkipArgPrefixes []string          `json:"top_level_skip_arg_prefixes,omitempty"`
	AllowedCommands         []string          `json:"allowed_commands,omitempty"`
	AllowedCommandsSetting  string            `json:"allowed_commands_setting,omitempty"`
	Env                     map[string]string `json:"env,omitempty"`
	EnvFromTopLevel         string            `json:"env_from_top_level,omitempty"`
}

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// InstalledDir returns the on-disk directory for user-installed skills.
func InstalledDir() string {
	return filepath.Join(config.BaseDir(), "skills")
}

// AgentsInstalledDir returns the shared agent-skills directory.
func AgentsInstalledDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".agents", "skills")
	}
	return filepath.Join(home, ".agents", "skills")
}

// InstalledDirs returns all on-disk directories used for user-installed skills.
func InstalledDirs() []string {
	dirs := []string{InstalledDir(), AgentsInstalledDir()}
	out := make([]string, 0, len(dirs))
	seen := make(map[string]struct{}, len(dirs))
	for _, dir := range dirs {
		dir = filepath.Clean(strings.TrimSpace(dir))
		if dir == "" {
			continue
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		out = append(out, dir)
	}
	return out
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

	for _, dir := range InstalledDirs() {
		disk, err := loadDisk(dir)
		if err != nil {
			return nil, err
		}
		for _, sk := range disk {
			byName[sk.Name] = markEnabled(sk, cfg)
		}
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
		var mf manifest
		manifestPath := path.Join(path.Dir(match), "aviary-skill.json")
		if manifestData, err := builtinFS.ReadFile(manifestPath); err == nil {
			if err := json.Unmarshal(manifestData, &mf); err != nil {
				return nil, err
			}
		}
		sk, err := parseSkill(name, string(data), mf)
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
		var mf manifest
		manifestPath := filepath.Join(filepath.Dir(path), "aviary-skill.json")
		if manifestData, err := os.ReadFile(manifestPath); err == nil {
			if err := json.Unmarshal(manifestData, &mf); err != nil {
				return err
			}
		}
		sk, err := parseSkill(name, string(data), mf)
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

func parseSkill(defaultName, raw string, mf manifest) (Definition, error) {
	sk := Definition{
		Name:           defaultName,
		Content:        strings.TrimSpace(raw),
		SettingsSchema: mf.Settings,
		Runtime:        mf.Runtime,
	}
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
