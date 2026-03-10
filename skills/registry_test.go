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
