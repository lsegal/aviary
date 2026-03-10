//go:build windows

package update

import (
	"fmt"
	"syscall"
	"time"
)

func waitForPIDExit(pid int, timeout time.Duration) error {
	const processQueryLimitedInformation = 0x1000
	const stillActive = 259
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
		if err != nil {
			return nil
		}
		var code uint32
		err = syscall.GetExitCodeProcess(handle, &code)
		_ = syscall.CloseHandle(handle)
		if err != nil || code != stillActive {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for process %d to exit", pid)
}
