//go:build windows

package browser

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// prepareLifecycleAviary gives the stand-in a console process group of its own,
// so a console control event can be aimed at it without reaching the test.
func prepareLifecycleAviary(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// signalLifecycleAviary sends the stand-in Ctrl+Break, the console control
// event Go delivers as os.Interrupt, as Ctrl+C in its console would.
func signalLifecycleAviary(cmd *exec.Cmd) error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid))
}

// processRunning reports whether pid is still a live process.
func processRunning(pid int) bool {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	event, err := windows.WaitForSingleObject(handle, 0)
	return err == nil && event == uint32(windows.WAIT_TIMEOUT)
}
