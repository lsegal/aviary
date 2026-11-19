//go:build !windows

package process

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startTreeCommand starts a shell that spawns a grandchild and reports the
// grandchild's pid, so tests can prove the whole tree is terminated.
func startTreeCommand(t *testing.T) (*exec.Cmd, int) {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	cmd := exec.Command("sh", "-c", "sleep 60 & echo $! > "+pidFile+"; sleep 60")
	if err := Start(cmd); err != nil {
		t.Fatalf("start child process: %v", err)
	}
	t.Cleanup(func() { _ = Stop(cmd) })
	var pid int
	waitFor(t, time.Second, func() bool {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
		return err == nil && pid > 0
	})
	return cmd, pid
}

func waitFor(t *testing.T, limit time.Duration, done func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if done() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return done()
}

func isTracked(cmd *exec.Cmd) bool {
	for _, tracked := range childProcesses.commands() {
		if tracked == cmd {
			return true
		}
	}
	return false
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, syscall.Signal(0)) == nil
}

func TestStopChildProcessTerminatesTheWholeTree(t *testing.T) {
	cmd, grandchild := startTreeCommand(t)
	if err := Stop(cmd); err != nil {
		t.Fatalf("stop child process: %v", err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return !processAlive(grandchild) }) {
		t.Fatalf("grandchild %d survived its parent", grandchild)
	}
	if isTracked(cmd) {
		t.Fatalf("stopped process is still tracked")
	}
}

func TestReapChildProcessesTerminatesTrackedProcesses(t *testing.T) {
	cmd, grandchild := startTreeCommand(t)
	ReapAll()
	if !waitFor(t, 2*time.Second, func() bool { return !processAlive(grandchild) }) {
		t.Fatalf("grandchild %d survived reaping", grandchild)
	}
	if err := Wait(cmd); err == nil {
		t.Fatalf("child exited normally, want termination by signal")
	}
	if isTracked(cmd) {
		t.Fatalf("reaped process is still tracked")
	}
}

func TestWaitChildProcessStopsTracking(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 0")
	if err := Start(cmd); err != nil {
		t.Fatalf("start child process: %v", err)
	}
	if err := Wait(cmd); err != nil {
		t.Fatalf("wait child process: %v", err)
	}
	if isTracked(cmd) {
		t.Fatalf("finished process is still tracked")
	}
}

// A child that was never isolated shares glorp's process group, so signalling
// it must never widen to the group and take glorp itself down.
func TestSignalProcessTreeSpareTheParentProcessGroup(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start process: %v", err)
	}
	if err := signalProcessTree(cmd, killSignal); err != nil {
		t.Fatalf("signal process tree: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatalf("child exited normally, want termination by signal")
	}
	if !processAlive(os.Getpid()) {
		t.Fatalf("signalling the child killed the parent's process group")
	}
}

func TestIsolateProcessTreeSkipsTerminalStdin(t *testing.T) {
	terminal, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Skip("no controlling terminal available")
	}
	defer terminal.Close()
	cmd := exec.Command("sleep", "60")
	cmd.Stdin = terminal
	isolateProcessTree(cmd)
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid {
		t.Fatalf("child inheriting the terminal was moved into its own process group")
	}
}

func TestIsolateProcessTreeIsolatesHeadlessChild(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	isolateProcessTree(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatalf("headless child was not moved into its own process group")
	}
}

func TestOutputChildProcessReturnsOutput(t *testing.T) {
	cmd := exec.Command("sh", "-c", "printf hello")
	output, err := Output(cmd)
	if err != nil {
		t.Fatalf("run child process: %v", err)
	}
	if string(output) != "hello" {
		t.Fatalf("output = %q, want %q", output, "hello")
	}
	if isTracked(cmd) {
		t.Fatalf("finished process is still tracked")
	}
}
