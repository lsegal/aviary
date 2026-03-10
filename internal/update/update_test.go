package update

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lsegal/aviary/internal/buildinfo"

	"github.com/stretchr/testify/assert"
)

func TestConfigureEmulationAndCheck(t *testing.T) {
	orig := buildinfo.Version
	buildinfo.Version = "dev"
	t.Cleanup(func() {
		buildinfo.Version = orig
		_ = ConfigureEmulation("")
	})
	err := ConfigureEmulation("1.2.3:1.3.0")
	assert.NoError(t, err)

	check, err := Check(context.Background(), nil)
	assert.NoError(t, err)
	assert.True(t, check.Emulated)
	assert.True(t, check.UpgradeAvailable)
	assert.Equal(t, "1.2.3", check.CurrentVersion)
	assert.Equal(t, "1.3.0", check.LatestVersion)

}

func TestConfigureEmulationRejectsReleaseBuild(t *testing.T) {
	orig := buildinfo.Version
	buildinfo.Version = "1.2.3"
	t.Cleanup(func() { buildinfo.Version = orig })
	err := ConfigureEmulation("1.2.3:1.3.0")
	assert.Error(t, err)

}

func TestInstallNoopWhenEmulated(t *testing.T) {
	orig := buildinfo.Version
	buildinfo.Version = "dev"
	t.Cleanup(func() {
		buildinfo.Version = orig
		_ = ConfigureEmulation("")
	})
	err := ConfigureEmulation("1.2.3:1.3.0")
	assert.NoError(t, err)

	result, err := Install(context.Background(), InstallOptions{
		Version:    "1.3.0",
		TargetPath: filepath.Join(t.TempDir(), "aviary"),
	})
	assert.NoError(t, err)
	assert.True(t, result.Noop)
	assert.True(t, result.Emulated)

}
