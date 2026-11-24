package cmd

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// prepareWatchProcess starts a helper process in its own process group, so
// a console control event can reach it without reaching the test.
func prepareWatchProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// interruptWatchProcess sends a helper process Ctrl+Break, which Go
// delivers as os.Interrupt, as it does Ctrl+C: a new process group ignores
// Ctrl+C.
func interruptWatchProcess(cmd *exec.Cmd) error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid))
}
