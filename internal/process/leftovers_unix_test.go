//go:build !windows

package process

import (
	"bytes"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A child that exits while a background process it started still holds its
// output has that process killed once WaitDelay expires, and the reaped PID
// logged (#1122).
func TestWaitReapsLeftoversHoldingOutput(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	var output bytes.Buffer
	cmd := exec.Command("sh", "-c", "sleep 30 & echo $!")
	cmd.Stdout = &output
	cmd.WaitDelay = 200 * time.Millisecond
	if err := Start(cmd); err != nil {
		t.Fatal(err)
	}
	if err := Wait(cmd); !CleanupTimedOut(err) {
		t.Fatalf("Wait() = %v, want the WaitDelay expiry", err)
	}
	leftover, err := strconv.Atoi(strings.TrimSpace(output.String()))
	if err != nil {
		t.Fatalf("leftover PID %q: %v", output.String(), err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(leftover, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("leftover process %d is still running", leftover)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(logs.String(), strconv.Itoa(leftover)) {
		t.Fatalf("logs = %q, want the reaped PID %d", logs.String(), leftover)
	}
}
