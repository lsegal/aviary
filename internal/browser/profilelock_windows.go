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

// BrowserHoldsProfile reports whether another browser already runs on
// profile. Windows Chromium keeps its profile lock as a file held open
// exclusively, so a lockfile that cannot be removed means a live browser.
func BrowserHoldsProfile(profile string) bool {
	path := filepath.Join(profile, "lockfile")
	if _, err := os.Stat(path); err != nil {
		return false
	}
	return os.Remove(path) != nil
}
