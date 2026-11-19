package process

import (
	"errors"
	"os/exec"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A child assigned to the job only after it starts running can spawn
// grandchildren, or outlive a killed glorp, before it is a member (issue #267).
// Creating it suspended is what closes that window.
func TestOwnedChildIsCreatedSuspended(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit 0")
	isolateProcessTree(cmd)
	guardOrphanedProcess(cmd)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&windows.CREATE_SUSPENDED == 0 {
		t.Fatalf("owned child is not created suspended: %+v", cmd.SysProcAttr)
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatalf("suspending the child dropped its process group isolation")
	}
}

// The suspended child must be a job member before it runs, and must actually
// run afterwards: a child left suspended would hang glorp forever.
func TestOwnedChildJoinsJobAndRuns(t *testing.T) {
	job, err := ownedJobObject()
	if err != nil {
		t.Fatalf("owned job object: %v", err)
	}
	cmd := exec.Command("cmd", "/c", "exit 7")
	if err := Start(cmd); err != nil {
		t.Fatalf("start child process: %v", err)
	}
	t.Cleanup(func() { _ = Stop(cmd) })
	if !processInJob(t, uint32(cmd.Process.Pid), job) {
		t.Fatalf("child %d is not a member of glorp's job object", cmd.Process.Pid)
	}
	var exit *exec.ExitError
	if err := Wait(cmd); !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("resumed child did not run to completion: %v", err)
	}
}

// A resume failure must be reported rather than silently handing the caller a
// child that will never run, so the lookup has to fail when there is no thread.
func TestResumeReportsAProcessWithNoThread(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit 0")
	if err := Run(cmd); err != nil {
		t.Fatalf("run child process: %v", err)
	}
	if err := resumePrimaryThread(uint32(cmd.Process.Pid)); err == nil {
		t.Fatalf("resuming an exited process succeeded")
	}
}

var procIsProcessInJob = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")

func processInJob(t *testing.T, pid uint32, job windows.Handle) bool {
	t.Helper()
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		t.Fatalf("open process %d: %v", pid, err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	var member int32
	ret, _, err := procIsProcessInJob.Call(uintptr(handle), uintptr(job), uintptr(unsafe.Pointer(&member)))
	if ret == 0 {
		t.Fatalf("IsProcessInJob: %v", err)
	}
	return member != 0
}
