//go:build !windows

package server

import (
	"errors"
	"os"
	"syscall"
)

// pidAlive reports whether the process with the given PID is still running.
// On Unix, FindProcess always succeeds; sending signal 0 checks liveness.
func pidAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
