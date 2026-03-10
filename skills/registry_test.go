package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/config"
)

func TestInstalledDirUsesConfigBaseDir(t *testing.T) {
	t.Setenv("AVIARY_CONFIG_BASE_DIR", filepath.Join(t.TempDir(), "aviary-config"))
	got := InstalledDir()
	if !strings.Contains(got, "aviary-config") {
		t.Fatalf("expected InstalledDir to use AVIARY_CONFIG_BASE_DIR, got %q", got)
	}
}

func TestListInstalledIncludesBuiltinSkill(t *testing.T) {
	cfg := &config.Config{}
	skills, err := ListInstalled(cfg)
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	found := false
	for _, skill := range skills {
		if skill.Name == "gogcli" && skill.Source == "builtin" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected builtin gogcli skill in installed list, got %+v", skills)
	}
}

func TestListInstalledDiskOverridesBuiltin(t *testing.T) {
	base := t.TempDir()
	t.Setenv("AVIARY_CONFIG_BASE_DIR", base)

	overrideDir := filepath.Join(base, "skills", "gogcli")
	if err := os.MkdirAll(overrideDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data := `---
name: gogcli
description: Disk override.
---
Disk version
`
	if err := os.WriteFile(filepath.Join(overrideDir, "SKILL.md"), []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	skills, err := ListInstalled(&config.Config{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	for _, skill := range skills {
		if skill.Name == "gogcli" {
			if skill.Source != "disk" {
				t.Fatalf("expected disk override source, got %+v", skill)
			}
			if skill.Description != "Disk override." {
				t.Fatalf("expected disk override description, got %+v", skill)
			}
			return
		}
	}
	t.Fatal("expected gogcli skill in installed list")
}

func TestListInstalledMarksEnabledFromConfig(t *testing.T) {
	cfg := &config.Config{
		Skills: map[string]config.SkillConfig{
			"gogcli": {Enabled: true},
		},
	}
	skills, err := ListInstalled(cfg)
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	for _, skill := range skills {
		if skill.Name == "gogcli" {
			if !skill.Enabled {
				t.Fatalf("expected gogcli to be enabled, got %+v", skill)
			}
			return
		}
	}
	t.Fatal("expected gogcli skill in installed list")
}
