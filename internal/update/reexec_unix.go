//go:build !windows

package update

import "syscall"

// Reexec replaces the current process with exe, keeping its PID so service
// managers see the same process.
func Reexec(exe string, args []string, env []string) error {
	return syscall.Exec(exe, append([]string{exe}, args...), env) //nolint:gosec
}
