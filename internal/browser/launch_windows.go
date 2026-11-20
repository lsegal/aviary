//go:build windows

package browser

import (
	"os/exec"
	"syscall"
)

// detachProcess configures cmd so Chrome runs in its own process group,
// surviving after the parent process exits.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
