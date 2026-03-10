package cmd

import (
	"path/filepath"
	"testing"

	"github.com/lsegal/aviary/internal/config"
)

func TestConfigureSkillsSummary(t *testing.T) {
	cfg := config.Default()
	if got := configureSkillsSummary(&cfg); got != "No skills enabled" {
		t.Fatalf("expected no skills summary, got %q", got)
	}

	cfg.Skills = map[string]config.SkillConfig{
		"gogcli": {Enabled: true},
	}
	if got := configureSkillsSummary(&cfg); got != "1 skill enabled" {
		t.Fatalf("expected single skill summary, got %q", got)
	}
}

func TestSkillMgrSaveCurrentPersistsConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AVIARY_CONFIG_BASE_DIR", filepath.Join(tmp, "base"))

	cfg := config.Default()
	cfgPath := filepath.Join(tmp, "aviary.yaml")
	m := newSkillMgrModel(&cfg, cfgPath)
	m.refreshInstalled()
	if len(m.installed) == 0 {
		t.Fatal("expected installed skills")
	}

	m.binary.SetValue("gog")
	m.allowed.SetValue("gmail, calendar")
	gotModel, _ := m.toggleEnabled()
	m = gotModel.(skillMgrModel)

	sk := m.cfg.Skills["gogcli"]
	if !sk.Enabled {
		t.Fatal("expected gogcli enabled")
	}
	if sk.Binary != "gog" {
		t.Fatalf("expected binary gog, got %q", sk.Binary)
	}
	if len(sk.AllowedCommands) != 2 || sk.AllowedCommands[0] != "gmail" || sk.AllowedCommands[1] != "calendar" {
		t.Fatalf("unexpected allowed commands: %#v", sk.AllowedCommands)
	}

	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load saved config: %v", err)
	}
	if !loaded.Skills["gogcli"].Enabled {
		t.Fatal("expected saved gogcli enabled")
	}
}
