package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
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

func TestReplaceFileFallsBackToCopyOnCrossDeviceRename(t *testing.T) {
	originalRename := renameFile
	renameCalls := 0
	renameFile = func(oldpath, newpath string) error {
		renameCalls++
		if renameCalls == 1 {
			return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
		}
		return originalRename(oldpath, newpath)
	}
	t.Cleanup(func() { renameFile = originalRename })

	dir := t.TempDir()
	src := filepath.Join(dir, "src-bin")
	target := filepath.Join(dir, "target-bin")
	assert.NoError(t, os.WriteFile(src, []byte("new binary"), 0o755))

	err := replaceFile(src, target)
	assert.NoError(t, err)

	data, readErr := os.ReadFile(target)
	assert.NoError(t, readErr)
	assert.Equal(t, "new binary", string(data))
	_, statErr := os.Stat(src)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestReplaceFileRestoresBackupWhenInstallFails(t *testing.T) {
	originalRename := renameFile
	renameCalls := 0
	renameFile = func(oldpath, newpath string) error {
		renameCalls++
		switch renameCalls {
		case 1:
			return originalRename(oldpath, newpath)
		case 2:
			return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
		case 3:
			return errors.New("rename failed")
		default:
			return originalRename(oldpath, newpath)
		}
	}
	t.Cleanup(func() { renameFile = originalRename })

	dir := t.TempDir()
	src := filepath.Join(dir, "src-bin")
	target := filepath.Join(dir, "target-bin")
	assert.NoError(t, os.WriteFile(src, []byte("new binary"), 0o755))
	assert.NoError(t, os.WriteFile(target, []byte("old binary"), 0o755))

	err := replaceFile(src, target)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "rename failed")

	data, readErr := os.ReadFile(target)
	assert.NoError(t, readErr)
	assert.Equal(t, "old binary", string(data))
}
