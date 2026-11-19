package browser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeBrowserEnv switches the test binary into a stand-in browser, so launch
// can be exercised end to end without Chrome installed.
const fakeBrowserEnv = "AVIARY_TEST_FAKE_BROWSER"

func TestMain(m *testing.M) {
	if mode := os.Getenv(lifecycleEnv); mode != "" {
		runLifecycleAviary(mode)
		return
	}
	if os.Getenv(fakeBrowserEnv) == "1" {
		runFakeBrowser(os.Args[1:])
		return
	}
	os.Exit(m.Run())
}

// runFakeBrowser behaves like Chrome where it matters here: it allows one
// process per --user-data-dir, and a second one on a directory in use hands
// off and exits without ever serving its DevTools endpoint.
func runFakeBrowser(args []string) {
	var dataDir, port string
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, "--user-data-dir="); ok {
			dataDir = value
		}
		if value, ok := strings.CutPrefix(arg, "--remote-debugging-port="); ok {
			port = value
		}
	}
	singleton, err := os.OpenFile(filepath.Join(dataDir, "FakeSingletonLock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(0)
	}
	_ = singleton.Close()
	serveFakeWindow(dataDir)
	http.HandleFunc("/json/version", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"webSocketDebuggerUrl":"ws://127.0.0.1:%s/devtools/browser/fake"}`, port)
	})
	_ = http.ListenAndServe("127.0.0.1:"+port, nil)
	os.Exit(1)
}

func TestLockProfileRefusesASecondClaim(t *testing.T) {
	profile := t.TempDir()
	first, err := lockProfile(profile)
	if err != nil {
		t.Fatalf("first lockProfile() error = %v", err)
	}
	if _, err := lockProfile(profile); !errors.Is(err, errProfileLocked) {
		t.Fatalf("second lockProfile() error = %v, want errProfileLocked", err)
	}
	first.release()
	again, err := lockProfile(profile)
	if err != nil {
		t.Fatalf("lockProfile() after release error = %v", err)
	}
	again.release()
}

func TestClaimProfileGivesALaterInstanceAPrivateDirectory(t *testing.T) {
	profile := t.TempDir()
	lock, dataDir, err := claimProfile(profile)
	if err != nil {
		t.Fatalf("claimProfile() error = %v", err)
	}
	defer lock.release()
	if lock == nil || dataDir != profile {
		t.Fatalf("first claimProfile() = (%v, %q), want the profile itself", lock, dataDir)
	}
	second, private, err := claimProfile(profile)
	if err != nil {
		t.Fatalf("second claimProfile() error = %v", err)
	}
	if second != nil {
		t.Fatalf("second claimProfile() took the profile lock")
	}
	if private == profile || private == "" {
		t.Fatalf("second claimProfile() dataDir = %q, want a private directory", private)
	}
	if info, err := os.Stat(private); err != nil || !info.IsDir() {
		t.Fatalf("private directory %q not created: %v", private, err)
	}
	(&process{profile: profile, dataDir: private}).release()
	if _, err := os.Stat(private); !os.IsNotExist(err) {
		t.Fatalf("private directory %q survived release: %v", private, err)
	}
	if _, err := os.Stat(profile); err != nil {
		t.Fatalf("shared profile removed by a private release: %v", err)
	}
}

// TestLaunchRunsSeveralInstancesOnOneProfile is issue #663: a second instance
// launched on the profile another is using timed out waiting for a browser
// that had handed itself to the first one and exited.
func TestLaunchRunsSeveralInstancesOnOneProfile(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	t.Setenv(fakeBrowserEnv, "1")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	config := Config{Binary: executable, Profile: t.TempDir()}

	first, err := launch(ctx, config)
	if err != nil {
		t.Fatalf("first launch() error = %v", err)
	}
	defer func() { _ = first.stop() }()
	second, err := launch(ctx, config)
	if err != nil {
		t.Fatalf("second launch() error = %v", err)
	}
	defer func() { _ = second.stop() }()

	if first.dataDir != config.Profile {
		t.Errorf("first instance ran in %q, want the profile %q", first.dataDir, config.Profile)
	}
	if second.dataDir == config.Profile {
		t.Errorf("second instance ran in the profile the first one holds")
	}
	if first.port == second.port {
		t.Errorf("both instances got DevTools port %d", first.port)
	}
	if second.profile != config.Profile {
		t.Errorf("second instance profile = %q, want %q so its sign-in is shared", second.profile, config.Profile)
	}

	private := second.dataDir
	if err := second.stop(); err != nil {
		t.Fatalf("second stop() error = %v", err)
	}
	// The stopped process can hold its directory for a moment on Windows.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(private); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("private directory %q survived stop", private)
		}
		second.dataDir = private
		second.release()
		time.Sleep(100 * time.Millisecond)
	}
}

func TestClaimProfileSidestepsAviarysRunningBrowser(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Chromium's profile lock is a symlink only on Unix")
	}
	profile := t.TempDir()
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(profile, "SingletonLock")
	if err := os.Symlink(fmt.Sprintf("%s-%d", host, os.Getpid()), link); err != nil {
		t.Fatal(err)
	}
	lock, dataDir, err := claimProfile(profile)
	if err != nil {
		t.Fatalf("claimProfile() error = %v", err)
	}
	if lock != nil || dataDir == profile {
		t.Fatalf("claimProfile() = (%v, %q), want a private directory beside the running browser", lock, dataDir)
	}
	(&process{profile: profile, dataDir: dataDir}).release()

	// A lock left by a browser that has exited is Chromium's to take over.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(host+"-999999999", link); err != nil {
		t.Fatal(err)
	}
	lock, dataDir, err = claimProfile(profile)
	if err != nil {
		t.Fatalf("claimProfile() error = %v", err)
	}
	defer lock.release()
	if lock == nil || dataDir != profile {
		t.Fatalf("claimProfile() with a stale lock = (%v, %q), want the profile itself", lock, dataDir)
	}
}
