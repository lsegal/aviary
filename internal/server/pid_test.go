package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubConfigBaseDir replaces the config base dir resolver for the test.
func stubConfigBaseDir(t *testing.T, base string) {
	t.Helper()
	orig := configBaseDir
	configBaseDir = func() string { return base }
	t.Cleanup(func() { configBaseDir = orig })
}

// exitedPID returns the PID of a child process that has already exited.
func exitedPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$") //nolint:gosec
	require.NoError(t, cmd.Run())
	return cmd.Process.Pid
}

func TestPIDPath_UsesConfigBaseDir(t *testing.T) {
	t.Setenv("AVIARY_PID_FILE", "  ")
	base := t.TempDir()
	t.Setenv("AVIARY_CONFIG_BASE_DIR", base)

	assert.Equal(t, filepath.Join(base, "aviary.pid"), PIDPath())
}

func TestPIDPath_TrimsEnvOverride(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom.pid")
	t.Setenv("AVIARY_PID_FILE", "  "+custom+"\n")

	assert.Equal(t, custom, PIDPath())
}

func TestPIDPath_FallbackWhenBaseDirEmpty(t *testing.T) {
	t.Setenv("AVIARY_PID_FILE", "")
	stubConfigBaseDir(t, "   ")
	programData := t.TempDir()
	t.Setenv("PROGRAMDATA", programData)

	want := filepath.Join(os.TempDir(), "aviary", "aviary.pid")
	if runtime.GOOS == "windows" {
		want = filepath.Join(programData, "aviary", "aviary.pid")
	}
	assert.Equal(t, want, PIDPath())
}

func TestPIDPath_FallbackToTempDirWithoutProgramData(t *testing.T) {
	t.Setenv("AVIARY_PID_FILE", "")
	stubConfigBaseDir(t, "")
	t.Setenv("PROGRAMDATA", " ")

	assert.Equal(t, filepath.Join(os.TempDir(), "aviary", "aviary.pid"), PIDPath())
}

func TestWritePID_CreatesMissingDirectory(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "nested", "dir", "aviary.pid")
	t.Setenv("AVIARY_PID_FILE", pidFile)

	require.NoError(t, WritePID())
	data, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(os.Getpid())+"\n", string(data))
}

func TestWritePID_DirectoryCreationFails(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	t.Setenv("AVIARY_PID_FILE", filepath.Join(blocker, "aviary.pid"))

	err := WritePID()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating PID directory")
}

func TestWritePID_PathIsDirectory(t *testing.T) {
	pidPath := t.TempDir()
	t.Setenv("AVIARY_PID_FILE", pidPath)

	require.Error(t, WritePID())
	info, err := os.Stat(pidPath)
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "existing directory must be left in place")
}

func TestReadPID_ReadFailure(t *testing.T) {
	t.Setenv("AVIARY_PID_FILE", t.TempDir())

	pid, err := ReadPID()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading PID file")
	assert.Equal(t, 0, pid)
}

func TestRemovePID_MissingFileIsNotAnError(t *testing.T) {
	t.Setenv("AVIARY_PID_FILE", filepath.Join(t.TempDir(), "missing.pid"))

	assert.NoError(t, RemovePID())
}

func TestRemovePID_Failure(t *testing.T) {
	// A non-empty directory cannot be removed with os.Remove.
	pidPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(pidPath, "keep"), []byte("x"), 0o600))
	t.Setenv("AVIARY_PID_FILE", pidPath)

	err := RemovePID()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "removing PID file")
	_, statErr := os.Stat(pidPath)
	assert.NoError(t, statErr, "directory must still exist after a failed remove")
}

func TestIsRunning_NoPIDFile(t *testing.T) {
	t.Setenv("AVIARY_PID_FILE", filepath.Join(t.TempDir(), "aviary.pid"))

	running, pid, err := IsRunning()
	require.NoError(t, err)
	assert.False(t, running)
	assert.Equal(t, 0, pid)
}

func TestIsRunning_StalePIDFile(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "aviary.pid")
	t.Setenv("AVIARY_PID_FILE", pidFile)
	stale := exitedPID(t)
	require.NoError(t, os.WriteFile(pidFile, []byte(strconv.Itoa(stale)+"\n"), 0o600))

	running, pid, err := IsRunning()
	require.NoError(t, err)
	assert.False(t, running)
	assert.Equal(t, stale, pid)
}

func TestIsRunning_GarbagePIDFile(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "aviary.pid")
	t.Setenv("AVIARY_PID_FILE", pidFile)
	require.NoError(t, os.WriteFile(pidFile, []byte("garbage\n"), 0o600))

	running, pid, err := IsRunning()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing PID")
	assert.False(t, running)
	assert.Equal(t, 0, pid)
}

func TestProcessAlive(t *testing.T) {
	assert.True(t, ProcessAlive(os.Getpid()))
	assert.False(t, ProcessAlive(exitedPID(t)))
}
