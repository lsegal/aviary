//go:build !windows

package ngrok

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/process"
)

// waitFor polls until done reports true or the limit runs out.
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

func processAlive(pid int) bool {
	return syscall.Kill(pid, syscall.Signal(0)) == nil
}

// startTreeCommand starts a shell that spawns a grandchild and reports the
// grandchild's pid, so tests can prove the whole tunnel tree is terminated.
func startTreeCommand(t *testing.T) (*exec.Cmd, int) {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	cmd := exec.Command("sh", "-c", "sleep 60 & echo $! > "+pidFile+"; sleep 60")
	if err := process.Start(cmd); err != nil {
		t.Fatalf("start child process: %v", err)
	}
	t.Cleanup(func() { _ = process.Stop(cmd) })
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

func TestTunnelCloseTerminatesTheTunnelTree(t *testing.T) {
	cmd, grandchild := startTreeCommand(t)
	tunnel := &Tunnel{cmd: cmd}
	if err := tunnel.Close(); err != nil {
		t.Fatalf("close tunnel: %v", err)
	}
	if !waitFor(t, 2*time.Second, func() bool { return !processAlive(grandchild) }) {
		t.Fatalf("ngrok grandchild %d survived Close", grandchild)
	}
}

func TestTunnelCloseWithoutProcess(t *testing.T) {
	var tunnel *Tunnel
	if err := tunnel.Close(); err != nil {
		t.Fatalf("close nil tunnel: %v", err)
	}
	if err := (&Tunnel{}).Close(); err != nil {
		t.Fatalf("close unstarted tunnel: %v", err)
	}
}
