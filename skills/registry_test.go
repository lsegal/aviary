package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/config"

	"github.com/stretchr/testify/assert"
)

func TestInstalledDirUsesConfigBaseDir(t *testing.T) {
	t.Setenv("AVIARY_CONFIG_BASE_DIR", filepath.Join(t.TempDir(), "aviary-config"))
	got := InstalledDir()
	assert.True(t, strings.Contains(got, "aviary-config"))
}

func TestAgentsInstalledDirUsesHomeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	got := AgentsInstalledDir()
	assert.Equal(t, filepath.Join(home, ".agents", "skills"), got)
}

func TestInstalledDirsIncludesAviaryAndAgentsDir(t *testing.T) {
	base := filepath.Join(t.TempDir(), "aviary-config")
	home := t.TempDir()
	t.Setenv("AVIARY_CONFIG_BASE_DIR", base)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	got := InstalledDirs()
	assert.Contains(t, got, filepath.Join(base, "skills"))
	assert.Contains(t, got, filepath.Join(home, ".agents", "skills"))
}

func TestListInstalledIncludesBuiltinSkill(t *testing.T) {
	cfg := &config.Config{}
	skills, err := ListInstalled(cfg)
	assert.NoError(t, err)

	found := false
	for _, skill := range skills {
		if skill.Name == "gogcli" && skill.Source == "builtin" {
			found = true
			break
		}
	}
	assert.True(t, found)

}

func TestListInstalledDiskOverridesBuiltin(t *testing.T) {
	base := t.TempDir()
	t.Setenv("AVIARY_CONFIG_BASE_DIR", base)

	overrideDir := filepath.Join(base, "skills", "gogcli")
	err := os.MkdirAll(overrideDir, 0o700)
	assert.NoError(t, err)

	data := `---
name: gogcli
description: Disk override.
---
Disk version
`
	err = os.WriteFile(filepath.Join(overrideDir, "SKILL.md"), []byte(data), 0o600)
	assert.NoError(t, err)

	skills, err := ListInstalled(&config.Config{})
	assert.NoError(t, err)

	found := false
	for _, skill := range skills {
		if skill.Name == "gogcli" {
			found = true
			assert.Equal(t, "disk", skill.Source)
			assert.Equal(t, "Disk override.", skill.Description)
		}
	}
	assert.True(t, found)
}

func TestListInstalledLoadsSkillsFromAgentsDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	overrideDir := filepath.Join(home, ".agents", "skills", "deploy")
	err := os.MkdirAll(overrideDir, 0o700)
	assert.NoError(t, err)

	data := `---
name: deploy
description: Deploy from shared agents dir.
---
Deploy version
`
	err = os.WriteFile(filepath.Join(overrideDir, "SKILL.md"), []byte(data), 0o600)
	assert.NoError(t, err)

	list, err := ListInstalled(&config.Config{})
	assert.NoError(t, err)

	found := false
	for _, skill := range list {
		if skill.Name == "deploy" {
			found = true
			assert.Equal(t, "disk", skill.Source)
			assert.Equal(t, "Deploy from shared agents dir.", skill.Description)
			assert.Equal(t, filepath.Join(overrideDir, "SKILL.md"), skill.Path)
		}
	}
	assert.True(t, found)
}

func TestListInstalledMarksEnabledFromConfig(t *testing.T) {
	cfg := &config.Config{
		Skills: map[string]config.SkillConfig{
			"gogcli": {Enabled: true},
		},
	}
	skills, err := ListInstalled(cfg)
	assert.NoError(t, err)

	found := false
	for _, skill := range skills {
		if skill.Name == "gogcli" {
			found = true
			assert.True(t, skill.Enabled)
		}
	}
	assert.True(t, found)
}
