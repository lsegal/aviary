//go:build windows

package server

import "syscall"

// pidAlive reports whether the process with the given PID is still running.
// On Windows, OpenProcess can succeed for a dead process whose PID was reused,
// so we call GetExitCodeProcess and check for STILL_ACTIVE (259).
func pidAlive(pid int) bool {
	const processQueryLimitedInformation = 0x1000
	const stillActive = 259
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(handle) //nolint:errcheck
	var code uint32
	if err = syscall.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}
