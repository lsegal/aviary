package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetFieldPointerLeaf(t *testing.T) {
	cfg := Default()

	require.NoError(t, SetField(&cfg, "browser.reuse_tabs", "false"))
	require.NotNil(t, cfg.Browser.ReuseTabs)
	assert.False(t, *cfg.Browser.ReuseTabs)
	assert.Equal(t, "false", GetField(&cfg, "browser.reuse_tabs"))

	require.NoError(t, SetField(&cfg, "browser.reuse_tabs", "true"))
	require.NotNil(t, cfg.Browser.ReuseTabs)
	assert.True(t, *cfg.Browser.ReuseTabs)

	require.NoError(t, SetField(&cfg, "browser.reuse_tabs", ""))
	assert.Nil(t, cfg.Browser.ReuseTabs)

	prev := true
	cfg.Browser.ReuseTabs = &prev
	assert.Error(t, SetField(&cfg, "browser.reuse_tabs", "maybe"))
	assert.Same(t, &prev, cfg.Browser.ReuseTabs, "a failed set leaves the field unchanged")
}
