//go:build !windows

package server

import "os"

// pidAlive reports whether the process with the given PID is still running.
// On Unix, FindProcess always succeeds; sending signal 0 checks liveness.
func pidAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(os.Signal(nil)) == nil
}
