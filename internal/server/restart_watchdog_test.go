package server

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/update"
)

func stubRestartWatchdog(t *testing.T, timeout time.Duration) <-chan struct{} {
	t.Helper()
	fired := make(chan struct{}, 1)
	prevTimeout, prevTimedOut := restartWatchdog, restartTimedOut
	restartWatchdog = timeout
	restartTimedOut = func() { fired <- struct{}{} }
	t.Cleanup(func() { restartWatchdog, restartTimedOut = prevTimeout, prevTimedOut })
	return fired
}

func TestRestartWatchdogFiresWhenShutdownHangs(t *testing.T) {
	fired := stubRestartWatchdog(t, 10*time.Millisecond)

	stop := startRestartWatchdog()
	defer stop()
	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("a restart that never finished shutting down did not trip the watchdog")
	}
}

func TestRestartWatchdogStoppedByFinishedShutdown(t *testing.T) {
	fired := stubRestartWatchdog(t, 50*time.Millisecond)

	startRestartWatchdog()()
	select {
	case <-fired:
		t.Fatal("the watchdog fired after the shutdown finished")
	case <-time.After(200 * time.Millisecond):
	}
}

// TestExitStalledRestart runs exitStalledRestart in a child process: it must
// exit non-zero, release the PID file, and ask the dev supervisor to rerun.
func TestExitStalledRestart(t *testing.T) {
	if os.Getenv("AVIARY_TEST_EXIT_STALLED_RESTART") == "1" {
		exitStalledRestart()
		return
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "aviary.pid")
	restartFile := filepath.Join(dir, "restart")
	require.NoError(t, os.WriteFile(pidFile, []byte("1"), 0o600))

	cmd := exec.Command(os.Args[0], "-test.run=^TestExitStalledRestart$") //nolint:gosec
	cmd.Env = append(os.Environ(),
		"AVIARY_TEST_EXIT_STALLED_RESTART=1",
		"AVIARY_PID_FILE="+pidFile,
		update.RestartFileEnv+"="+restartFile,
	)
	err := cmd.Run()
	var exitErr *exec.ExitError
	require.True(t, errors.As(err, &exitErr), "expected a non-zero exit, got %v", err)
	assert.Equal(t, 1, exitErr.ExitCode())
	assert.NoFileExists(t, pidFile)
	assert.FileExists(t, restartFile, "the dev supervisor must be asked to run the backend again")
}
