//go:build !windows

package browser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// lockFile takes an exclusive, non-blocking lock on file.
func lockFile(file *os.File) error {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return errProfileLocked
	}
	if err != nil {
		return fmt.Errorf("lock browser profile: %w", err)
	}
	return nil
}

// chromeSingletonLock is the symlink Chromium keeps in a profile it is
// running on, pointing at "<hostname>-<pid>".
const chromeSingletonLock = "SingletonLock"

// browserHoldsProfile reports whether a live Chromium process, such as
// Aviary's own browser, already runs on profile. A lock left by a browser
// that has exited, or one written on another host, does not count: Chromium
// takes over a stale lock itself.
func browserHoldsProfile(profile string) bool {
	target, err := os.Readlink(filepath.Join(profile, chromeSingletonLock))
	if err != nil {
		return false
	}
	at := strings.LastIndex(target, "-")
	if at < 0 {
		return false
	}
	host, err := os.Hostname()
	if err != nil || host != target[:at] {
		return false
	}
	pid, err := strconv.Atoi(target[at+1:])
	if err != nil || pid <= 0 {
		return false
	}
	err = syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
