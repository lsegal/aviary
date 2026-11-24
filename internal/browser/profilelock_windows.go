//go:build windows

package browser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// lockFile takes an exclusive, non-blocking lock on file.
func lockFile(file *os.File) error {
	err := windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, new(windows.Overlapped),
	)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrProfileLocked
	}
	if err != nil {
		return fmt.Errorf("lock browser profile: %w", err)
	}
	return nil
}

// chromeHoldsProfile reports whether another browser already runs on
// profile. Windows Chromium keeps its profile lock as a file held open
// exclusively, so a lockfile that cannot be removed means a live browser.
func chromeHoldsProfile(profile string) bool {
	path := filepath.Join(profile, "lockfile")
	if _, err := os.Stat(path); err != nil {
		return false
	}
	return os.Remove(path) != nil
}

// processAlive reports whether a process with pid runs on this host.
// OpenProcess can succeed for a process that has exited, so its exit code is
// checked as well.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle) //nolint:errcheck
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}
