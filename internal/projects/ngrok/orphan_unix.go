//go:build !windows

package ngrok

import (
	"os/exec"
	"syscall"
	"time"

	"github.com/lsegal/aviary/internal/projects/process"
)

// orphanKillGrace is how long a leftover agent gets to shut itself down before
// it is killed outright. It is short because nothing depends on the agent's
// exit: aviary only needs the local ngrok API port and the account session it
// holds.
const orphanKillGrace = 2 * time.Second

// platformRunningProcesses lists every process on the machine with its parent,
// which is how aviary tells an agent it abandoned from one a live aviary still
// owns. BSD-style flags are used because they are what macOS and Linux agree
// on.
func platformRunningProcesses() ([]processEntry, error) {
	output, err := process.Output(exec.Command("ps", "-axo", "pid=,ppid=,command="))
	if err != nil && len(output) == 0 {
		return nil, err
	}
	return parseProcessList(string(output)), nil
}

// platformTerminateProcess asks a leftover agent to exit and kills it if it
// does not. Only the process itself is signalled: an orphan's process group id
// may have been reused in the time it went unowned, and aviary has no claim on
// whatever holds it now.
func platformTerminateProcess(pid int) error {
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(orphanKillGrace)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, syscall.Signal(0)) != nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}
