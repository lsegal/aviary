//go:build !windows

package process

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// A detached child is reaped once it exits, so no <defunct> zombie is left
// under aviary (issue #1426).
func TestStartDetachedReapsTheChild(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	reaped, err := StartDetached(cmd)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-reaped:
	case <-time.After(5 * time.Second):
		t.Fatal("detached child was never reaped")
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
		t.Fatalf("process state = %v", cmd.ProcessState)
	}
	// A reaped child is gone from the process table: waiting on it again
	// finds no such child rather than a zombie.
	var status syscall.WaitStatus
	if _, err := syscall.Wait4(cmd.Process.Pid, &status, syscall.WNOHANG, nil); !errors.Is(err, syscall.ECHILD) {
		t.Fatalf("Wait4 on the reaped child = %v; want ECHILD", err)
	}
}
