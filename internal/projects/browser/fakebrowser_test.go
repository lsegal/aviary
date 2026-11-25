package browser

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
	lock := filepath.Join(dataDir, "FakeSingletonLock")
	singleton, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		// Like Chrome, a lock left by a process that has since died is stale
		// and taken over, so a profile can be relaunched after a stop.
		owner, readErr := os.ReadFile(lock)
		pid, atoiErr := strconv.Atoi(string(owner))
		if readErr != nil || atoiErr != nil || processRunning(pid) {
			os.Exit(0)
		}
		if singleton, err = os.OpenFile(lock, os.O_TRUNC|os.O_WRONLY, 0o600); err != nil {
			os.Exit(0)
		}
	}
	_, _ = fmt.Fprint(singleton, os.Getpid())
	_ = singleton.Close()
	serveFakeWindow(dataDir)
	http.HandleFunc("/json/version", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"webSocketDebuggerUrl":"ws://127.0.0.1:%s/devtools/browser/fake"}`, port)
	})
	_ = http.ListenAndServe("127.0.0.1:"+port, nil)
	os.Exit(1)
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
	defer func() { first.Stop() }()
	second, err := launch(ctx, config)
	if err != nil {
		t.Fatalf("second launch() error = %v", err)
	}
	defer func() { second.Stop() }()

	if first.DataDir != config.Profile {
		t.Errorf("first instance ran in %q, want the profile %q", first.DataDir, config.Profile)
	}
	if second.DataDir == config.Profile {
		t.Errorf("second instance ran in the profile the first one holds")
	}
	if first.Port == second.Port {
		t.Errorf("both instances got DevTools port %d", first.Port)
	}
	if second.Profile != config.Profile {
		t.Errorf("second instance profile = %q, want %q so its sign-in is shared", second.Profile, config.Profile)
	}

	private := second.DataDir
	second.Stop()
	// The stopped process can hold its directory for a moment on Windows.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(private); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("private directory %q survived stop", private)
		}
		second.DataDir = private
		second.Release()
		time.Sleep(100 * time.Millisecond)
	}
}
