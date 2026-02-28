package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lsegal/aviary/internal/store"
)

// PIDPath returns the path to the PID file.
func PIDPath() string {
	return filepath.Join(store.DataDir(), "aviary.pid")
}

// WritePID writes the current process PID to the PID file.
func WritePID() error {
	pid := strconv.Itoa(os.Getpid())
	return os.WriteFile(PIDPath(), []byte(pid+"\n"), 0o600)
}

// ReadPID reads the PID from the PID file.
// Returns 0 and no error if the file does not exist.
func ReadPID() (int, error) {
	data, err := os.ReadFile(PIDPath())
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading PID file: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parsing PID: %w", err)
	}
	return pid, nil
}

// RemovePID deletes the PID file.
func RemovePID() error {
	if err := os.Remove(PIDPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing PID file: %w", err)
	}
	return nil
}

// IsRunning returns true if a process with the stored PID is alive.
func IsRunning() (bool, int, error) {
	pid, err := ReadPID()
	if err != nil {
		return false, 0, err
	}
	if pid == 0 {
		return false, 0, nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, pid, nil
	}
	// On Unix, FindProcess always succeeds; signal 0 checks existence.
	// On Windows, FindProcess returns an error if process doesn't exist.
	if err := proc.Signal(os.Signal(nil)); err != nil {
		return false, pid, nil
	}
	return true, pid, nil
}
