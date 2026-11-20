//go:build !windows

package browser

import (
	"os/exec"
	"syscall"
)

// detachProcess configures cmd so Chrome runs in its own session,
// surviving after the parent process exits.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
