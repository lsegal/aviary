//go:build !windows

package process

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Linux delivers the parent-death signal when the *thread* that forked the
// child exits, and Go retires idle threads freely, so a child forked from an
// ordinary goroutine would die at an unpredictable moment while glorp is still
// running. Churn threads hard and prove the child is untouched.
func TestOwnedChildSurvivesThreadChurn(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := Start(cmd); err != nil {
		t.Fatalf("start child process: %v", err)
	}
	t.Cleanup(func() { _ = Stop(cmd) })
	pid := cmd.Process.Pid

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Returning while still locked destroys the OS thread, which is
			// exactly what must not take a child down with it.
			runtime.LockOSThread()
			time.Sleep(5 * time.Millisecond)
		}()
	}
	wg.Wait()
	runtime.GC()
	runtime.GC()
	time.Sleep(500 * time.Millisecond)

	if !processAlive(pid) {
		t.Fatalf("child %d was killed while glorp was still running", pid)
	}
}

// Every owned child must be forked from the one pinned spawn thread, including
// children started concurrently from many goroutines.
func TestSpawnerStartsChildrenFromEveryGoroutine(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command("sh", "-c", "exit 0")
			if err := Start(cmd); err != nil {
				t.Errorf("start child process: %v", err)
				return
			}
			if err := Wait(cmd); err != nil {
				t.Errorf("wait child process: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestSpawnerReportsStartFailures(t *testing.T) {
	cmd := exec.Command("glorp-no-such-binary-exists")
	if err := Start(cmd); err == nil {
		t.Fatalf("starting a missing binary succeeded")
	}
	if isTracked(cmd) {
		t.Fatalf("failed start is tracked")
	}
}

// The acceptance case from issues #264, #266 and #364: however glorp itself
// dies, its subprocesses must go with it on every Unix, even though no
// userspace cleanup can run. A hangup stands in for the terminal window being
// closed out from under a `glorp watch`, and a panic for a crash.
func TestKilledParentTakesItsChildrenDown(t *testing.T) {
	for _, exit := range []struct {
		name string
		mode string
	}{
		{name: "sigkill", mode: "kill"},
		{name: "closed terminal", mode: "hangup"},
		{name: "panic", mode: "panic"},
	} {
		t.Run(exit.name, func(t *testing.T) { assertParentTakesItsChildDown(t, exit.mode) })
	}
}

// assertParentTakesItsChildDown re-executes the test binary as a stand-in
// glorp, ends it the way mode describes, and requires the child it owned to be
// gone.
func assertParentTakesItsChildDown(t *testing.T, mode string) {
	parent := exec.Command(os.Args[0], "-test.run=TestOrphanGuardHelperProcess", "-test.timeout=60s")
	parent.Env = append(os.Environ(), "GLORP_ORPHAN_HELPER=1", "GLORP_ORPHAN_HELPER_EXIT="+mode)
	stdout, err := parent.StdoutPipe()
	if err != nil {
		t.Fatalf("pipe helper stdout: %v", err)
	}
	if err := parent.Start(); err != nil {
		t.Fatalf("start helper parent: %v", err)
	}
	defer func() {
		_ = parent.Process.Kill()
		_ = parent.Wait()
	}()

	var pid int
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if child, err := strconv.Atoi(strings.TrimPrefix(line, "child-pid ")); err == nil && strings.HasPrefix(line, "child-pid ") {
			pid = child
			break
		}
	}
	if pid <= 0 {
		t.Fatalf("helper parent never reported a child pid")
	}
	if !processAlive(pid) {
		t.Fatalf("helper child %d never started", pid)
	}

	// A hangup and a panic end the parent through its own code path; only a
	// SIGKILL has to be delivered here, and it is the case the guards exist for.
	if mode == "kill" {
		if err := parent.Process.Signal(syscall.SIGKILL); err != nil {
			t.Fatalf("kill helper parent: %v", err)
		}
	}
	if !waitFor(t, 10*time.Second, func() bool { return !processAlive(pid) }) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("child %d outlived a parent ended by %s", pid, mode)
	}
}

// TestOrphanGuardHelperProcess is not a test: it is the child half of
// TestKilledParentTakesItsChildrenDown, re-executed as its own process so it
// can be killed outright.
func TestOrphanGuardHelperProcess(t *testing.T) {
	if os.Getenv("GLORP_ORPHAN_HELPER") != "1" {
		t.Skip("helper process for TestKilledParentTakesItsChildrenDown")
	}
	cmd := exec.Command("sleep", "60")
	if err := Start(cmd); err != nil {
		t.Fatalf("start child process: %v", err)
	}
	fmt.Printf("child-pid %d\n", cmd.Process.Pid)
	os.Stdout.Sync()
	switch os.Getenv("GLORP_ORPHAN_HELPER_EXIT") {
	case "hangup":
		// What a closed terminal sends, with no handler installed, so the
		// process dies exactly as an unattended `glorp watch` would.
		_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
	case "panic":
		panic("helper crash")
	}
	time.Sleep(50 * time.Second)
}
