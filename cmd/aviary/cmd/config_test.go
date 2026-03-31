package cmd

import (
	"testing"

	"github.com/lsegal/aviary/internal/config"

	"github.com/stretchr/testify/assert"
)

func TestConfigGetKey_Simple(t *testing.T) {
	cfg := config.Default()
	cfg.Browser.ProfileDir = "/my/profile"
	cfg.Browser.Binary = "/usr/bin/chromium"
	cfg.Browser.CDPPort = 9222
	reuseTabs := false
	cfg.Browser.ReuseTabs = &reuseTabs
	cfg.Scheduler.Concurrency = "auto"

	tests := []struct {
		key  string
		want string
	}{
		{"browser.profile_directory", "/my/profile"},
		{"browser.binary", "/usr/bin/chromium"},
		{"browser.cdp_port", "9222"},
		{"browser.reuse_tabs", "false"},
		{"server.port", "16677"},
		{"scheduler.concurrency", "auto"},
	}

	for _, tt := range tests {
		got, err := configGetKey(&cfg, tt.key)
		assert.NoError(t, err)
		if err != nil {
			continue
		}
		assert.Equal(t, tt.want, got)

	}
}

func TestConfigGetKey_NotFound(t *testing.T) {
	cfg := config.Default()
	_, err := configGetKey(&cfg, "browser.nonexistent")
	assert.Error(t, err)

}

func TestConfigGetKey_TraverseError(t *testing.T) {
	cfg := config.Default()
	// browser.cdp_port is a scalar (int), not a map — can't traverse into it.
	_, err := configGetKey(&cfg, "browser.cdp_port.foo")
	assert.Error(t, err)

}

func TestConfigSetKey_String(t *testing.T) {
	cfg := config.Default()
	err := configSetKey(&cfg, "browser.profile_directory", "/my/profile")
	assert.NoError(t, err)

	assert.Equal(t, "/my/profile", cfg.Browser.ProfileDir)

}

func TestConfigSetKey_Int(t *testing.T) {
	cfg := config.Default()
	err := configSetKey(&cfg, "server.port", "9999")
	assert.NoError(t, err)

	assert.Equal(t, 9999, cfg.Server.Port)

}

func TestConfigSetKey_BrowserBinary(t *testing.T) {
	cfg := config.Default()
	err := configSetKey(&cfg, "browser.binary", "/usr/bin/chromium")
	assert.NoError(t, err)

	assert.Equal(t, "/usr/bin/chromium", cfg.Browser.Binary)

}

func TestConfigSetKey_CDPPort(t *testing.T) {
	cfg := config.Default()
	err := configSetKey(&cfg, "browser.cdp_port", "9333")
	assert.NoError(t, err)

	assert.Equal(t, 9333, cfg.Browser.CDPPort)

}

func TestConfigSetKey_BrowserReuseTabs(t *testing.T) {
	cfg := config.Default()
	err := configSetKey(&cfg, "browser.reuse_tabs", "false")
	assert.NoError(t, err)

	if assert.NotNil(t, cfg.Browser.ReuseTabs) {
		assert.False(t, *cfg.Browser.ReuseTabs)
	}
}

func TestConfigSetKey_NestedModel(t *testing.T) {
	cfg := config.Default()
	err := configSetKey(&cfg, "models.defaults.model", "anthropic/claude-sonnet-4-5")
	assert.NoError(t, err)

	assert.Equal(t, "anthropic/claude-sonnet-4-5", cfg.Models.Defaults.Model)

}

func TestConfigRoundtrip(t *testing.T) {
	cfg := config.Default()

	// Set several values, then get them back.
	sets := []struct{ key, val string }{
		{"browser.profile_directory", "/aviary/profile"},
		{"browser.cdp_port", "9444"},
		{"browser.reuse_tabs", "false"},
		{"server.port", "8080"},
	}
	for _, s := range sets {
		err := configSetKey(&cfg, s.key, s.val)
		assert.NoError(t, err)

	}
	for _, s := range sets {
		got, err := configGetKey(&cfg, s.key)
		assert.NoError(t, err)
		assert.Equal(t, s.val, got)

	}
}
