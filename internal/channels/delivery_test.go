package channels

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/store"
)

func TestStageOutgoingMedia_RequiresSourcePath(t *testing.T) {
	useTempDataDir(t)

	for _, path := range []string{"", "   \t"} {
		target, err := StageOutgoingMedia("slack", path)
		require.EqualError(t, err, "source path is required")
		assert.Empty(t, target)
	}
	assert.NoDirExists(t, store.OutgoingMediaDir("slack"))
}

func TestStageOutgoingMedia_MissingSource(t *testing.T) {
	useTempDataDir(t)

	target, err := StageOutgoingMedia("slack", filepath.Join(t.TempDir(), "missing.png"))
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.Empty(t, target)
	assert.NoDirExists(t, store.OutgoingMediaDir("slack"))
}

func TestStageOutgoingMedia_DirectorySource(t *testing.T) {
	useTempDataDir(t)

	target, err := StageOutgoingMedia("slack", t.TempDir())
	require.EqualError(t, err, "source path is a directory")
	assert.Empty(t, target)
	assert.NoDirExists(t, store.OutgoingMediaDir("slack"))
}

func TestStageOutgoingMedia_CopiesIntoChannelDir(t *testing.T) {
	useTempDataDir(t)
	src := writeSource(t, "chart.png", pngBytes)

	target, err := StageOutgoingMedia("signal", src)
	require.NoError(t, err)

	dir := store.OutgoingMediaDir("signal")
	assert.Equal(t, dir, filepath.Dir(target))
	assert.Regexp(t, regexp.MustCompile(`^\d+_chart\.png$`), filepath.Base(target))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, pngBytes, got)

	// The source is copied, not moved.
	orig, err := os.ReadFile(src)
	require.NoError(t, err)
	assert.Equal(t, pngBytes, orig)

	if runtime.GOOS != "windows" {
		info, err := os.Stat(target)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestStageOutgoingMedia_StagingDirIsFile(t *testing.T) {
	useTempDataDir(t)
	src := writeSource(t, "chart.png", pngBytes)

	dir := store.OutgoingMediaDir("slack")
	require.NoError(t, os.MkdirAll(filepath.Dir(dir), 0o700))
	require.NoError(t, os.WriteFile(dir, []byte("not a dir"), 0o600))

	target, err := StageOutgoingMedia("slack", src)
	require.Error(t, err)
	assert.Empty(t, target)
}

func TestStageOutgoingMedia_TargetNotWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not block file creation on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	useTempDataDir(t)
	src := writeSource(t, "chart.png", pngBytes)

	dir := store.OutgoingMediaDir("slack")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	target, err := StageOutgoingMedia("slack", src)
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrPermission)
	assert.Empty(t, target)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
