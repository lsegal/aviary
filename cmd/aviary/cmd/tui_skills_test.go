package cmd

import (
	"path/filepath"
	"testing"

	"github.com/lsegal/aviary/internal/config"

	"github.com/stretchr/testify/assert"
)

func TestConfigureSkillsSummary(t *testing.T) {
	cfg := config.Default()
	got := configureSkillsSummary(&cfg)
	assert.Equal(t, "No skills enabled", got)

	cfg.Skills = map[string]config.SkillConfig{
		"gogcli": {Enabled: true},
	}
	got = configureSkillsSummary(&cfg)
	assert.Equal(t, "1 skill enabled", got)

}

func TestSkillMgrSaveCurrentPersistsConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AVIARY_CONFIG_BASE_DIR", filepath.Join(tmp, "base"))

	cfg := config.Default()
	cfgPath := filepath.Join(tmp, "aviary.yaml")
	m := newSkillMgrModel(&cfg, cfgPath)
	m.refreshInstalled()
	assert.NotEqual(t, 0, len(m.installed))

	gotModel, _ := m.toggleEnabled()
	m = gotModel.(skillMgrModel)

	sk := m.cfg.Skills["gogcli"]
	assert.True(t, sk.Enabled)
	assert.Nil(t, sk.Settings)

	loaded, err := config.Load(cfgPath)
	assert.NoError(t, err)
	assert.True(t, loaded.Skills["gogcli"].Enabled)

}
