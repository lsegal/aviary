//go:build !windows

package browser

import (
	"errors"
	"fmt"
	"os"
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
