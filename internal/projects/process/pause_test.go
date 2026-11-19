package process

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestPauseHelperProcess is not a real test: TestPauseStopsAndResumesChild
// runs the test binary as a child that appends to a file until it is killed.
func TestPauseHelperProcess(t *testing.T) {
	path := os.Getenv("GLORP_PAUSE_HELPER_FILE")
	if path == "" {
		return
	}
	for {
		file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = file.WriteString(".")
			_ = file.Close()
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// The shutdown checkpoint pauses an agent so its checkout stops changing
// while it is committed (issue #741).
func TestPauseStopsAndResumesChild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ticks")
	cmd := exec.Command(os.Args[0], "-test.run=^TestPauseHelperProcess$")
	cmd.Env = append(os.Environ(), "GLORP_PAUSE_HELPER_FILE="+path)
	if err := Start(cmd); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() { _ = Stop(cmd) })
	deadline := time.Now().Add(10 * time.Second)
	for fileSize(path) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if fileSize(path) == 0 {
		t.Fatal("child never started writing")
	}
	if err := Pause(cmd); err != nil {
		t.Fatalf("pause: %v", err)
	}
	// Let a write already in flight land before measuring.
	time.Sleep(100 * time.Millisecond)
	paused := fileSize(path)
	time.Sleep(300 * time.Millisecond)
	if got := fileSize(path); got != paused {
		t.Fatalf("paused child kept writing: %d -> %d bytes", paused, got)
	}
	if err := Resume(cmd); err != nil {
		t.Fatalf("resume: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for fileSize(path) == paused && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if fileSize(path) == paused {
		t.Fatal("resumed child did not write again")
	}
}

// A paused child can still be stopped, as shutdown does after the
// checkpoint without resuming it.
func TestStopEndsPausedChild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ticks")
	cmd := exec.Command(os.Args[0], "-test.run=^TestPauseHelperProcess$")
	cmd.Env = append(os.Environ(), "GLORP_PAUSE_HELPER_FILE="+path)
	if err := Start(cmd); err != nil {
		t.Fatalf("start child: %v", err)
	}
	if err := Pause(cmd); err != nil {
		t.Fatalf("pause: %v", err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- Stop(cmd) }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("stop paused child: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("stopping a paused child hung")
	}
}
