package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lsegal/aviary/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveConfigPath_Default(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)

	got, err := resolveConfigPath("")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(base, "aviary", "aviary.yaml"), got)
}

func TestResolveConfigPath_Relative(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)

	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))
	defer func() { _ = os.Chdir(wd) }()

	got, err := resolveConfigPath(filepath.Join("nested", "aviary.yaml"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tmp, "nested", "aviary.yaml"), got)
}

func TestChdirToConfigDir_Default(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(wd) }()

	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("AVIARY_CONFIG_BASE_DIR", "")

	got, err := chdirToConfigDir("")
	require.NoError(t, err)

	cwd, err := os.Getwd()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(base, "aviary"), cwd)
	assert.Equal(t, filepath.Join(base, "aviary", "aviary.yaml"), got)
	assert.Equal(t, filepath.Join(base, "aviary"), os.Getenv("AVIARY_CONFIG_BASE_DIR"))
}

func TestChdirToConfigDir_ExplicitConfigPath(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(wd) }()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "custom", "aviary.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o750))
	require.NoError(t, config.Save(cfgPath, &config.Config{}))

	got, err := chdirToConfigDir(cfgPath)
	require.NoError(t, err)

	cwd, err := os.Getwd()
	require.NoError(t, err)
	assert.Equal(t, filepath.Dir(cfgPath), cwd)
	assert.Equal(t, cfgPath, got)
	assert.Equal(t, filepath.Dir(cfgPath), os.Getenv("AVIARY_CONFIG_BASE_DIR"))
}
