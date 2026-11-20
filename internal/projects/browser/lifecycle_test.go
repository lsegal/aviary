package browser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	aviarybrowser "github.com/lsegal/aviary/internal/browser"

	owned "github.com/lsegal/aviary/internal/projects/process"
)

// The tests here are issue #773: every browser Aviary starts must be gone once
// Aviary is, however Aviary went. Each one runs the test binary as a stand-in
// Aviary that opens a browser the way Aviary does, ends that Aviary one of the
// ways Aviary ends, and then checks that the browser died with it and left the
// profile free for the next launch.
const (
	// lifecycleEnv switches the test binary into the stand-in Aviary. Its value
	// names the launch path: lifecycleWindow or lifecycleDriver.
	lifecycleEnv = "AVIARY_TEST_LIFECYCLE"
	// lifecycleDirEnv is the directory the stand-in and the test talk through.
	lifecycleDirEnv = "AVIARY_TEST_LIFECYCLE_DIR"
	// lifecycleBinaryEnv and lifecycleProfileEnv are the browser to launch and
	// the profile to launch it on.
	lifecycleBinaryEnv  = "AVIARY_TEST_LIFECYCLE_BINARY"
	lifecycleProfileEnv = "AVIARY_TEST_LIFECYCLE_PROFILE"

	// lifecycleWindow opens the interactive window `Aviary browser` and the
	// sign-in recovery open; lifecycleDriver starts the browser a watch reads
	// GitHub through.
	lifecycleWindow = "window"
	lifecycleDriver = "driver"

	// Files in lifecycleDirEnv: the stand-in writes the browser's process id to
	// lifecycleReady once the browser answers its DevTools endpoint, or the
	// reason it could not start to lifecycleFailed; the test writes
	// lifecycleQuit to ask the stand-in to exit on its own.
	lifecycleReady  = "ready"
	lifecycleFailed = "failed"
	lifecycleQuit   = "quit"
)

// lifecycleSupervisor is Aviary's own supervisor, recording the browser it
// starts so the stand-in can report it.
type lifecycleSupervisor struct {
	ownedSupervisor
	started chan *exec.Cmd
}

func (s lifecycleSupervisor) Start(cmd *exec.Cmd) error {
	if err := s.ownedSupervisor.Start(cmd); err != nil {
		return err
	}
	s.started <- cmd
	return nil
}

// runLifecycleAviary is the stand-in Aviary. It shuts down the way Aviary's own
// commands do: the shared shutdown signals cancel its context, and owned
// subprocesses are reaped before it returns.
func runLifecycleAviary(mode string) {
	dir := os.Getenv(lifecycleDirEnv)
	// The browser inherits this environment, and must not be a stand-in Aviary
	// itself when it is the test binary.
	_ = os.Unsetenv(lifecycleEnv)
	fail := func(err error) {
		_ = os.WriteFile(filepath.Join(dir, lifecycleFailed), []byte(err.Error()), 0o600)
		os.Exit(1)
	}
	ctx, stop := owned.ShutdownContext(nil)
	defer stop()
	defer owned.ReapAll()
	ctx, quit := context.WithCancel(ctx)
	defer quit()
	go func() {
		for ctx.Err() == nil {
			if _, err := os.Stat(filepath.Join(dir, lifecycleQuit)); err == nil {
				quit()
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	started := make(chan *exec.Cmd, 1)
	config := Config{
		Binary:     os.Getenv(lifecycleBinaryEnv),
		Profile:    os.Getenv(lifecycleProfileEnv),
		Supervisor: lifecycleSupervisor{started: started},
	}
	done := make(chan error, 1)
	switch mode {
	case lifecycleWindow:
		go func() { done <- OpenWindow(ctx, config) }()
	case lifecycleDriver:
		go func() {
			driver, err := Start(ctx, config)
			if err != nil {
				done <- err
				return
			}
			<-ctx.Done()
			done <- driver.Close()
		}()
	default:
		fail(fmt.Errorf("unknown launch path %q", mode))
	}
	var cmd *exec.Cmd
	select {
	case cmd = <-started:
	case err := <-done:
		fail(fmt.Errorf("browser never started: %v", err))
	}
	if _, err := aviarybrowser.WaitForReady(ctx, aviarybrowser.DebugURL(debugPort(cmd.Args)), aviarybrowser.OwnedReadyTimeout, aviarybrowser.OwnedReadyInterval); err != nil {
		fail(err)
	}
	if err := os.WriteFile(filepath.Join(dir, lifecycleReady), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		fail(err)
	}
	if err := <-done; err != nil {
		fail(err)
	}
	// The orphan guard would take the browser down anyway once this process
	// exits, so a browser still running here means the shutdown path skipped
	// the clean close that writes the profile out.
	if processRunning(cmd.Process.Pid) {
		fail(fmt.Errorf("browser %d still running after shutdown", cmd.Process.Pid))
	}
}

// debugPort reads the DevTools port back out of a browser's arguments.
func debugPort(args []string) int {
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, "--remote-debugging-port="); ok {
			port, _ := strconv.Atoi(value)
			return port
		}
	}
	return 0
}

// lifecycleBrowser is a browser the lifecycle tests launch.
type lifecycleBrowser struct {
	name   string
	binary string
	// fake is the test binary standing in for a browser. It answers the
	// DevTools endpoint but not the shutdown request, so it is always killed,
	// and it never releases its own singleton, so the next launch is checked
	// against Aviary's profile lock alone.
	fake bool
}

// lifecycleBrowsers are the stand-in browser, which runs everywhere, and a
// real one where one is installed.
func lifecycleBrowsers(t *testing.T) []lifecycleBrowser {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	browsers := []lifecycleBrowser{{name: "fake", binary: executable, fake: true}}
	if binary, err := aviarybrowser.FindBinary(""); err == nil {
		browsers = append(browsers, lifecycleBrowser{name: "real", binary: binary})
	}
	return browsers
}

// lifecycleExits are the ways Aviary ends: returning on its own, being sent a
// shutdown signal, and being killed outright, which runs no cleanup and
// leaves only the orphan guard.
var lifecycleExits = []string{"clean", "signal", "kill"}

func TestBrowserDiesWithAviary(t *testing.T) {
	if testing.Short() {
		t.Skip("launches browsers")
	}
	for _, browser := range lifecycleBrowsers(t) {
		for _, mode := range []string{lifecycleWindow, lifecycleDriver} {
			for _, exit := range lifecycleExits {
				t.Run(browser.name+"/"+mode+"/"+exit, func(t *testing.T) {
					testBrowserDiesWithAviary(t, browser, mode, exit)
				})
			}
		}
	}
}

func testBrowserDiesWithAviary(t *testing.T, browser lifecycleBrowser, mode, exit string) {
	if mode == lifecycleWindow && !browser.fake && !displayAvailable() {
		t.Skip("no display server to open a window on")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	dir, profile := t.TempDir(), t.TempDir()
	aviary := exec.Command(executable, "-test.run=^$")
	aviary.Env = append(os.Environ(),
		lifecycleEnv+"="+mode,
		lifecycleDirEnv+"="+dir,
		lifecycleBinaryEnv+"="+browser.binary,
		lifecycleProfileEnv+"="+profile,
	)
	// The stand-in passes this on to the browser it launches, which is the
	// test binary again when the browser is the fake one.
	fake := ""
	if browser.fake {
		fake = "1"
	}
	aviary.Env = append(aviary.Env, fakeBrowserEnv+"="+fake)
	prepareLifecycleAviary(aviary)
	var output bytes.Buffer
	aviary.Stdout, aviary.Stderr = &output, &output
	if err := aviary.Start(); err != nil {
		t.Fatalf("start stand-in aviary: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- aviary.Wait() }()
	t.Cleanup(func() {
		_ = aviary.Process.Kill()
		<-exited
	})

	pid := awaitLifecycleBrowser(t, dir, browser, exited)
	switch exit {
	case "clean":
		if err := os.WriteFile(filepath.Join(dir, lifecycleQuit), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	case "signal":
		if err := signalLifecycleAviary(aviary); err != nil {
			t.Fatalf("signal stand-in aviary: %v", err)
		}
	case "kill":
		if err := aviary.Process.Kill(); err != nil {
			t.Fatalf("kill stand-in aviary: %v", err)
		}
	}
	select {
	case err := <-exited:
		exited <- err
		if exit != "kill" && err != nil {
			failed, _ := os.ReadFile(filepath.Join(dir, lifecycleFailed))
			t.Fatalf("stand-in aviary exited with %v: %s\n%s", err, failed, output.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("stand-in aviary did not exit")
	}

	deadline := time.Now().Add(15 * time.Second)
	for processRunning(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("browser %d outlived aviary (%s exit)", pid, exit)
		}
		time.Sleep(50 * time.Millisecond)
	}
	assertProfileFree(t, browser, profile)
}

// awaitLifecycleBrowser waits for the stand-in to report its browser running,
// skipping where a real browser is installed but cannot start here.
func awaitLifecycleBrowser(t *testing.T, dir string, browser lifecycleBrowser, exited chan error) int {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for {
		if data, err := os.ReadFile(filepath.Join(dir, lifecycleReady)); err == nil && len(data) > 0 {
			pid, err := strconv.Atoi(string(data))
			if err != nil {
				t.Fatalf("stand-in reported browser %q", data)
			}
			return pid
		}
		select {
		case err := <-exited:
			exited <- err
			failed, _ := os.ReadFile(filepath.Join(dir, lifecycleFailed))
			if !browser.fake {
				t.Skipf("browser would not start: %s", failed)
			}
			t.Fatalf("stand-in aviary exited before its browser started (%v): %s", err, failed)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the stand-in never reported its browser")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// assertProfileFree checks that the next launch gets the profile: Aviary's own
// lock is free, and a real browser comes up on it instead of handing off to a
// survivor of the last one.
func assertProfileFree(t *testing.T, browser lifecycleBrowser, profile string) {
	t.Helper()
	lock, err := aviarybrowser.LockProfile(profile)
	if err != nil {
		t.Fatalf("profile still locked after aviary exited: %v", err)
	}
	lock.Release()
	if browser.fake {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	next, err := launch(ctx, Config{Binary: browser.binary, Profile: profile})
	if err != nil {
		t.Fatalf("relaunch on the profile: %v", err)
	}
	defer next.Stop()
	if next.DataDir != profile {
		t.Fatalf("relaunch ran in %q, not the profile", next.DataDir)
	}
}

// displayAvailable reports whether a headed browser could open a window here.
func displayAvailable() bool {
	return runtime.GOOS != "linux" || os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// A Config that names no supervisor once started the browser as a bare child,
// outside every guard, and no caller ever named one (issue #773).
func TestBrowserDefaultsToAviarysOwnSupervisor(t *testing.T) {
	if _, ok := (Config{}).supervisor().(ownedSupervisor); !ok {
		t.Fatalf("default supervisor = %T, want aviary's own process tracking", Config{}.supervisor())
	}
}

// A sign-in window that closes because the run is shutting down hands the
// profile back to a browser that must not start.
func TestLaunchStartsNothingOnceTheRunIsShuttingDown(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	t.Setenv(fakeBrowserEnv, "1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	config := Config{Binary: executable, Profile: t.TempDir()}
	if proc, err := launch(ctx, config); !errors.Is(err, context.Canceled) {
		if proc != nil {
			proc.Stop()
		}
		t.Fatalf("launch() error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(filepath.Join(config.Profile, "FakeSingletonLock")); err == nil {
		t.Fatal("a browser was started for a run that is shutting down")
	}
}
