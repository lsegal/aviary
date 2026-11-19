//go:build !windows

package ngrok

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/process"
)

// The sweep is only as good as the process list it reads, so exercise the real
// platform lister and terminator against a process this test owns.
func TestPlatformProcessSweepFindsAndStopsARealProcess(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := process.Start(cmd); err != nil {
		t.Fatalf("start child process: %v", err)
	}
	reaped := make(chan struct{})
	go func() {
		defer close(reaped)
		_ = process.Wait(cmd)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-reaped
	})
	pid := cmd.Process.Pid

	processes, err := platformRunningProcesses()
	if err != nil {
		t.Fatalf("list running processes: %v", err)
	}
	found := false
	for _, entry := range processes {
		if entry.pid == pid {
			found = true
			if entry.ppid != os.Getpid() {
				t.Fatalf("process %d reported parent %d, want %d", pid, entry.ppid, os.Getpid())
			}
		}
	}
	if !found {
		t.Fatalf("process list did not include child %d", pid)
	}

	if err := platformTerminateProcess(pid); err != nil {
		t.Fatalf("terminate process %d: %v", pid, err)
	}
	if !waitFor(t, 5*time.Second, func() bool {
		select {
		case <-reaped:
			return true
		default:
			return false
		}
	}) {
		t.Fatalf("process %d survived the sweep", pid)
	}
}
