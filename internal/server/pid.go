package server

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/lsegal/aviary/internal/config"
)

// PIDPath returns the path to the PID file.
func PIDPath() string {
	if p := strings.TrimSpace(os.Getenv("AVIARY_PID_FILE")); p != "" {
		return p
	}

	// Use the configured Aviary config base dir when available. `config.BaseDir`
	// returns the explicit `AVIARY_CONFIG_BASE_DIR` when set, otherwise the
	// parent directory of `DefaultPath()`.
	base := strings.TrimSpace(config.BaseDir())
	if base != "" {
		return filepath.Join(base, "aviary.pid")
	}

	if runtime.GOOS == "windows" {
		if programData := strings.TrimSpace(os.Getenv("PROGRAMDATA")); programData != "" {
			return filepath.Join(programData, "aviary", "aviary.pid")
		}
	}

	return filepath.Join(os.TempDir(), "aviary", "aviary.pid")
}

// WritePID writes the current process PID to the PID file.
func WritePID() error {
	if err := os.MkdirAll(filepath.Dir(PIDPath()), 0o755); err != nil {
		return fmt.Errorf("creating PID directory: %w", err)
	}

	pid := strconv.Itoa(os.Getpid())
	return os.WriteFile(PIDPath(), []byte(pid+"\n"), 0o644)
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
	return pidAlive(pid), pid, nil
}
