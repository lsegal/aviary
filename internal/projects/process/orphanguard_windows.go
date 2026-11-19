package process

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ownedJob holds every subprocess glorp starts. The job is configured to kill
// its members when its last handle closes, which happens when glorp's process
// object is destroyed — including when glorp is terminated outright and runs no
// cleanup of its own (issue #264). Unlike Linux's parent-death signal, job
// membership is inherited, so grandchildren are covered too.
var ownedJob struct {
	once   sync.Once
	handle windows.Handle
	err    error
}

func ownedJobObject() (windows.Handle, error) {
	ownedJob.once.Do(func() {
		handle, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			ownedJob.err = err
			return
		}
		limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
				LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
			},
		}
		if _, err := windows.SetInformationJobObject(
			handle,
			windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&limits)),
			uint32(unsafe.Sizeof(limits)),
		); err != nil {
			_ = windows.CloseHandle(handle)
			ownedJob.err = err
			return
		}
		// The handle is deliberately never closed: it is what keeps the job
		// alive, and its destruction with the process is the guarantee.
		ownedJob.handle = handle
	})
	return ownedJob.handle, ownedJob.err
}

// guardOrphanedProcess creates the child suspended so it can be made a job
// member before it runs a single instruction. Windows can only put a process
// that already exists into a job, and a child assigned after it starts running
// has a window in which it can spawn grandchildren the job never covers, or in
// which glorp can be killed with the child not yet a member (issue #267).
//
// The child stays suspended until adoptOrphanedProcess resumes it, so nothing
// may return a started child to a caller without calling that.
func guardOrphanedProcess(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
}

// adoptOrphanedProcess makes a freshly created child a member of glorp's job
// object and then lets it run. It reports an error only when the child cannot
// be resumed, which is the one failure that would leave it hung forever;
// failing to join the job costs the kill-on-close guarantee for that child but
// leaves it working, and the userspace cleanup paths in this package still cover
// every exit glorp can observe.
func adoptOrphanedProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return nil
	}
	pid := uint32(cmd.Process.Pid)
	assignToOwnedJob(pid)
	if err := resumePrimaryThread(pid); err != nil {
		return fmt.Errorf("resume suspended child %d: %w", pid, err)
	}
	return nil
}

// assignToOwnedJob puts a process into glorp's job object, ignoring the
// failures described on adoptOrphanedProcess. glorp already running inside
// another job is not one of them: Windows 8 and later nest jobs, so the child
// simply belongs to both.
func assignToOwnedJob(pid uint32) {
	job, err := ownedJobObject()
	if err != nil {
		return
	}
	const access = windows.PROCESS_SET_QUOTA | windows.PROCESS_TERMINATE
	handle, err := windows.OpenProcess(access, false, pid)
	if err != nil {
		return
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	_ = windows.AssignProcessToJobObject(job, handle)
}

// resumePrimaryThread starts a process created with CREATE_SUSPENDED. Go does
// not expose the primary thread handle CreateProcess returns, so the thread is
// found by enumerating the system's threads; a process that has not executed
// anything yet has exactly one, which is that thread.
func resumePrimaryThread(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		defer func() { _ = windows.CloseHandle(thread) }()
		if _, err := windows.ResumeThread(thread); err != nil {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("no thread found for process %d", pid)
}

// releaseOrphanedProcess has nothing to do on Windows: a child that exits leaves
// the job object on its own, and the job only kills processes still in it.
func releaseOrphanedProcess(*exec.Cmd) {}
