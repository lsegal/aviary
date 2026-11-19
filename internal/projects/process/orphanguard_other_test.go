//go:build !linux && !windows

package process

import (
	"fmt"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// startReapTarget starts a process in its own process group, standing in for a
// subprocess glorp owns. It is waited on for the whole test, because a child
// nobody has waited on lingers as a zombie that still answers a liveness check.
func startReapTarget(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if err := Start(cmd); err != nil {
		t.Fatalf("start child process: %v", err)
	}
	reaped := make(chan struct{})
	go func() {
		defer close(reaped)
		_ = Wait(cmd)
	}()
	t.Cleanup(func() {
		_ = signalProcessTree(cmd, killSignal)
		<-reaped
	})
	if pgid, err := syscall.Getpgid(cmd.Process.Pid); err != nil || pgid != cmd.Process.Pid {
		t.Fatalf("child %d did not get its own process group: pgid %d, err %v", cmd.Process.Pid, pgid, err)
	}
	return cmd.Process.Pid
}

// Closing the pipe is what the kernel does for glorp when it is killed outright,
// so a recorded group must not survive it.
func TestOrphanReaperKillsRecordedGroupWhenPipeCloses(t *testing.T) {
	pipe, reaper, err := startOrphanReaper()
	if err != nil {
		t.Fatalf("start orphan reaper: %v", err)
	}
	t.Cleanup(func() { _ = signalProcessTree(reaper, killSignal) })
	pid := startReapTarget(t)

	if _, err := fmt.Fprintf(pipe, "+ %d\n", pid); err != nil {
		t.Fatalf("record owned group: %v", err)
	}
	if !processAlive(pid) {
		t.Fatalf("child %d never started", pid)
	}
	if err := pipe.Close(); err != nil {
		t.Fatalf("close reaper pipe: %v", err)
	}
	if !waitFor(t, 5*time.Second, func() bool { return !processAlive(pid) }) {
		t.Fatalf("child %d outlived the reaper's pipe", pid)
	}
}

// A released group belongs to nobody as far as glorp is concerned, and its id
// may have been handed to an unrelated process by now.
func TestOrphanReaperLeavesReleasedGroupAlone(t *testing.T) {
	pipe, reaper, err := startOrphanReaper()
	if err != nil {
		t.Fatalf("start orphan reaper: %v", err)
	}
	t.Cleanup(func() { _ = signalProcessTree(reaper, killSignal) })
	pid := startReapTarget(t)

	if _, err := fmt.Fprintf(pipe, "+ %d\n- %d\n", pid, pid); err != nil {
		t.Fatalf("record owned group: %v", err)
	}
	if err := pipe.Close(); err != nil {
		t.Fatalf("close reaper pipe: %v", err)
	}
	if !waitFor(t, 5*time.Second, func() bool { return !processAlive(reaper.Process.Pid) }) {
		t.Fatalf("reaper %d never finished", reaper.Process.Pid)
	}
	if !processAlive(pid) {
		t.Fatalf("released child %d was killed", pid)
	}
}

// A second glorp instance records its own children with its own reaper, so one
// instance shutting down must not disturb the other's tree.
func TestOrphanReaperIgnoresAnotherInstancesTree(t *testing.T) {
	pipe, reaper, err := startOrphanReaper()
	if err != nil {
		t.Fatalf("start orphan reaper: %v", err)
	}
	t.Cleanup(func() { _ = signalProcessTree(reaper, killSignal) })
	other := startReapTarget(t)

	if err := pipe.Close(); err != nil {
		t.Fatalf("close reaper pipe: %v", err)
	}
	if !waitFor(t, 5*time.Second, func() bool { return !processAlive(reaper.Process.Pid) }) {
		t.Fatalf("reaper %d never finished", reaper.Process.Pid)
	}
	if !processAlive(other) {
		t.Fatalf("unrecorded child %d was killed", other)
	}
}

// Ownership bookkeeping is what keeps the reaper from signalling a process group
// glorp has already cleaned up.
func TestOwnedProcessGroupIsReleasedWhenChildIsForgotten(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := Start(cmd); err != nil {
		t.Fatalf("start child process: %v", err)
	}
	if !ownedProcessGroupRecorded(cmd) {
		t.Fatalf("running child was not recorded with the reaper")
	}
	if err := Stop(cmd); err != nil {
		t.Fatalf("stop child process: %v", err)
	}
	if ownedProcessGroupRecorded(cmd) {
		t.Fatalf("stopped child is still recorded with the reaper")
	}
}

func ownedProcessGroupRecorded(cmd *exec.Cmd) bool {
	orphanReaper.mu.Lock()
	defer orphanReaper.mu.Unlock()
	_, ok := orphanReaper.owned[cmd]
	return ok
}
