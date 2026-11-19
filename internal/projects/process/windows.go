//go:build windows

package process

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// isolateProcessTree gives the child its own console process group so console
// signals aimed at glorp are not forwarded to it before glorp can shut it down
// in order.
func isolateProcessTree(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
}

// signalProcessTree ends the child and everything it spawned. Windows has no
// process-group signalling, so taskkill's /T flag walks the tree instead. It
// reports os.ErrProcessDone once the tree is gone.
func signalProcessTree(cmd *exec.Cmd, signal processSignal) error {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return os.ErrProcessDone
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	if signal == checkSignal {
		if cmd.ProcessState != nil {
			return os.ErrProcessDone
		}
		return nil
	}
	if signal == pauseSignal || signal == resumeSignal {
		return suspendProcessTree(uint32(cmd.Process.Pid), signal == pauseSignal)
	}
	args := []string{"/T", "/PID", pid}
	if signal == killSignal {
		args = append([]string{"/F"}, args...)
	}
	if err := exec.Command("taskkill", args...).Run(); err != nil {
		// taskkill without /F cannot end a process that has no window, so fall
		// back to ending the child directly rather than leaving it running.
		if killErr := cmd.Process.Kill(); killErr != nil {
			return os.ErrProcessDone
		}
	}
	return nil
}

var (
	ntdll                = windows.NewLazySystemDLL("ntdll.dll")
	procNtSuspendProcess = ntdll.NewProc("NtSuspendProcess")
	procNtResumeProcess  = ntdll.NewProc("NtResumeProcess")
)

// suspendProcessTree suspends or resumes the process and every descendant.
// Windows has no SIGSTOP, so each process in the tree is suspended through
// ntdll, the call debuggers use. It reports os.ErrProcessDone when nothing in
// the tree could be signalled.
func suspendProcessTree(root uint32, suspend bool) error {
	proc := procNtResumeProcess
	if suspend {
		proc = procNtSuspendProcess
	}
	if err := proc.Find(); err != nil {
		return err
	}
	signalled := false
	for _, id := range processTreeIDs(root) {
		handle, err := windows.OpenProcess(windows.PROCESS_SUSPEND_RESUME, false, id)
		if err != nil {
			continue
		}
		status, _, _ := proc.Call(uintptr(handle))
		_ = windows.CloseHandle(handle)
		if status == 0 {
			signalled = true
		}
	}
	if !signalled {
		return os.ErrProcessDone
	}
	return nil
}

// processTreeIDs lists root and every process descended from it, read from
// one snapshot of the system process table.
func processTreeIDs(root uint32) []uint32 {
	ids := []uint32{root}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return ids
	}
	defer windows.CloseHandle(snapshot)
	children := map[uint32][]uint32{}
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if entry.ProcessID != entry.ParentProcessID {
			children[entry.ParentProcessID] = append(children[entry.ParentProcessID], entry.ProcessID)
		}
	}
	seen := map[uint32]bool{root: true}
	for i := 0; i < len(ids); i++ {
		for _, child := range children[ids[i]] {
			if !seen[child] {
				seen[child] = true
				ids = append(ids, child)
			}
		}
	}
	return ids
}
