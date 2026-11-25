package browser

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeBrowserEnv makes this test binary act as a browser when LaunchOwned
// runs it, so the launch is exercised end to end without a real browser. Its
// value is the mode the stand-in runs in; see runFakeBrowser.
const fakeBrowserEnv = "AVIARY_TEST_FAKE_BROWSER"

// fakeBrowserArgsEnv names a file the stand-in browser writes the arguments
// it was launched with to, one per line.
const fakeBrowserArgsEnv = "AVIARY_TEST_FAKE_BROWSER_ARGS"

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeBrowserEnv); mode != "" {
		os.Exit(runFakeBrowser(mode, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// runFakeBrowser stands in for a browser. In "serve" mode it answers on the
// DevTools port it was given and exits when it is sent Browser.close;
// "ignore-close" answers the same way but never exits on its own; "hang"
// never answers at all; "exit3" exits at once with status 3.
func runFakeBrowser(mode string, args []string) int {
	if path := os.Getenv(fakeBrowserArgsEnv); path != "" {
		_ = os.WriteFile(path, []byte(strings.Join(args, "\n")), 0o600)
	}
	switch mode {
	case "exit3":
		return 3
	case "hang":
		time.Sleep(time.Hour)
	}
	port := ""
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, "--remote-debugging-port="); ok {
			port = value
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return 2
	}
	closed := make(chan struct{})
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"webSocketDebuggerUrl": "ws://127.0.0.1:" + port + "/devtools/browser/fake",
		})
	})
	mux.HandleFunc("/devtools/browser/fake", func(w http.ResponseWriter, r *http.Request) {
		method, err := readDevToolsMethod(w, r)
		if err == nil && method == "Browser.close" && mode == "serve" {
			once.Do(func() { close(closed) })
		}
	})
	go func() { _ = http.Serve(listener, mux) }()
	<-closed
	return 0
}

// readDevToolsMethod upgrades a DevTools WebSocket request and returns the
// method of the first message sent over it.
func readDevToolsMethod(w http.ResponseWriter, r *http.Request) (string, error) {
	conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	var message struct {
		ID     int64  `json:"id"`
		Method string `json:"method"`
	}
	if err := conn.ReadJSON(&message); err != nil {
		return "", err
	}
	if message.ID != 1 {
		return "", errors.New("unexpected message id")
	}
	return message.Method, nil
}

// fakeSupervisor stands in for a process supervisor: the "process" runs until
// it is killed or the test lets it exit.
type fakeSupervisor struct {
	startErr error
	waitErr  error
	exit     chan struct{}
	once     sync.Once
	mu       sync.Mutex
	kills    int
	started  *exec.Cmd
}

func newFakeSupervisor() *fakeSupervisor {
	return &fakeSupervisor{exit: make(chan struct{})}
}

func (s *fakeSupervisor) Start(cmd *exec.Cmd) error {
	s.started = cmd
	return s.startErr
}

func (s *fakeSupervisor) Wait(*exec.Cmd) error {
	<-s.exit
	return s.waitErr
}

func (s *fakeSupervisor) Kill(*exec.Cmd) error {
	s.mu.Lock()
	s.kills++
	s.mu.Unlock()
	s.exitNow()
	return nil
}

func (s *fakeSupervisor) exitNow() { s.once.Do(func() { close(s.exit) }) }

func (s *fakeSupervisor) killCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kills
}

func waitExited(t *testing.T, child *OwnedChild) {
	t.Helper()
	select {
	case <-child.Exited():
	case <-time.After(10 * time.Second):
		t.Fatal("process did not exit")
	}
}

func TestShutdownPolicyWithDefaults(t *testing.T) {
	got := ShutdownPolicy{}.withDefaults()
	if got.Timeout != 10*time.Second || got.FailedGrace != 2*time.Second {
		t.Fatalf("withDefaults() = %v / %v, want 10s / 2s", got.Timeout, got.FailedGrace)
	}
	if reflect.ValueOf(got.Close).Pointer() != reflect.ValueOf(CloseOverDevTools).Pointer() {
		t.Fatal("withDefaults() Close is not CloseOverDevTools")
	}

	called := false
	custom := ShutdownPolicy{
		Timeout:     time.Minute,
		FailedGrace: time.Second,
		Close:       func(context.Context, string) error { called = true; return nil },
	}.withDefaults()
	if custom.Timeout != time.Minute || custom.FailedGrace != time.Second {
		t.Fatalf("withDefaults() replaced set values: %v / %v", custom.Timeout, custom.FailedGrace)
	}
	_ = custom.Close(context.Background(), "")
	if !called {
		t.Fatal("withDefaults() replaced a set Close")
	}

	negative := ShutdownPolicy{Timeout: -1, FailedGrace: -1}.withDefaults()
	if negative.Timeout != 10*time.Second || negative.FailedGrace != 2*time.Second {
		t.Fatalf("withDefaults() kept negative values: %v / %v", negative.Timeout, negative.FailedGrace)
	}
}

func TestStartOwnedReportsStartFailure(t *testing.T) {
	supervisor := newFakeSupervisor()
	supervisor.startErr = errors.New("no such browser")
	child, err := StartOwned(supervisor, ShutdownPolicy{}, exec.Command("browser"))
	if err == nil || err.Error() != "no such browser" || child != nil {
		t.Fatalf("StartOwned() = %v, %v, want the start error", child, err)
	}
}

func TestStartOwnedWaitsThroughTheSupervisor(t *testing.T) {
	supervisor := newFakeSupervisor()
	supervisor.waitErr = errors.New("exit status 1")
	cmd := exec.Command("browser")
	child, err := StartOwned(supervisor, ShutdownPolicy{}, cmd)
	if err != nil {
		t.Fatalf("StartOwned() error = %v", err)
	}
	if supervisor.started != cmd {
		t.Fatal("StartOwned() did not start cmd through the supervisor")
	}
	select {
	case <-child.Exited():
		t.Fatal("Exited() closed while the process still runs")
	default:
	}
	supervisor.exitNow()
	waitExited(t, child)
	if err := child.WaitErr(); err == nil || err.Error() != "exit status 1" {
		t.Fatalf("WaitErr() = %v, want the supervisor's wait error", err)
	}
}

func TestStartOwnedRunsAPlainChildWithoutASupervisor(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), fakeBrowserEnv+"=exit3")
	child, err := StartOwned(nil, ShutdownPolicy{}, cmd)
	if err != nil {
		t.Fatalf("StartOwned() error = %v", err)
	}
	waitExited(t, child)
	var exitErr *exec.ExitError
	if !errors.As(child.WaitErr(), &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("WaitErr() = %v, want exit status 3", child.WaitErr())
	}
}

func TestStartOwnedReportsAPlainChildThatCannotStart(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-browser")
	if _, err := StartOwned(nil, ShutdownPolicy{}, exec.Command(missing)); err == nil {
		t.Fatal("StartOwned() error = nil for a missing executable")
	}
}

func TestOwnedChildStopOnNilIsANoOp(_ *testing.T) {
	var child *OwnedChild
	child.Stop("ws://127.0.0.1:1/devtools/browser/x")
}

// startFakeChild starts an OwnedChild on a fake supervisor with close as the
// shutdown request.
func startFakeChild(t *testing.T, policy ShutdownPolicy) (*OwnedChild, *fakeSupervisor) {
	t.Helper()
	supervisor := newFakeSupervisor()
	child, err := StartOwned(supervisor, policy, exec.Command("browser"))
	if err != nil {
		t.Fatalf("StartOwned() error = %v", err)
	}
	t.Cleanup(supervisor.exitNow)
	return child, supervisor
}

func TestOwnedChildStopSkipsAnExitedProcess(t *testing.T) {
	closes := 0
	child, supervisor := startFakeChild(t, ShutdownPolicy{
		Close: func(context.Context, string) error { closes++; return nil },
	})
	supervisor.exitNow()
	waitExited(t, child)
	child.Stop("ws://browser")
	if closes != 0 || supervisor.killCount() != 0 {
		t.Fatalf("Stop() on an exited process: %d closes, %d kills, want none", closes, supervisor.killCount())
	}
}

func TestOwnedChildStopKillsAtOnceWithoutAnEndpoint(t *testing.T) {
	closes := 0
	child, supervisor := startFakeChild(t, ShutdownPolicy{
		Timeout: time.Hour,
		Close:   func(context.Context, string) error { closes++; return nil },
	})
	done := make(chan struct{})
	go func() { child.Stop(""); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop(\"\") waited instead of killing at once")
	}
	if closes != 0 || supervisor.killCount() != 1 {
		t.Fatalf("Stop(\"\"): %d closes, %d kills, want 0 and 1", closes, supervisor.killCount())
	}
}

func TestOwnedChildStopLetsTheBrowserCloseCleanly(t *testing.T) {
	var gotURL string
	var supervisor *fakeSupervisor
	child, supervisor := startFakeChild(t, ShutdownPolicy{
		Timeout: time.Hour,
		Close: func(ctx context.Context, wsURL string) error {
			if _, ok := ctx.Deadline(); !ok {
				t.Error("Close() got a context without the shutdown deadline")
			}
			gotURL = wsURL
			go supervisor.exitNow()
			return nil
		},
	})
	child.Stop("ws://browser")
	if gotURL != "ws://browser" {
		t.Fatalf("Close() got %q, want ws://browser", gotURL)
	}
	if supervisor.killCount() != 0 {
		t.Fatalf("Stop() killed a browser that closed cleanly (%d kills)", supervisor.killCount())
	}
}

func TestOwnedChildStopKillsAfterTheTimeout(t *testing.T) {
	child, supervisor := startFakeChild(t, ShutdownPolicy{
		Timeout:     50 * time.Millisecond,
		FailedGrace: time.Hour,
		Close:       func(context.Context, string) error { return nil },
	})
	start := time.Now()
	child.Stop("ws://browser")
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("Stop() killed after %v, before the 50ms timeout", elapsed)
	}
	if supervisor.killCount() != 1 {
		t.Fatalf("Stop() made %d kills, want 1", supervisor.killCount())
	}
}

func TestOwnedChildStopKillsSoonerWhenTheRequestFails(t *testing.T) {
	child, supervisor := startFakeChild(t, ShutdownPolicy{
		Timeout:     time.Hour,
		FailedGrace: 50 * time.Millisecond,
		Close:       func(context.Context, string) error { return errors.New("connection refused") },
	})
	done := make(chan struct{})
	go func() { child.Stop("ws://browser"); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop() waited out the full timeout after a failed request")
	}
	if supervisor.killCount() != 1 {
		t.Fatalf("Stop() made %d kills, want 1", supervisor.killCount())
	}
}

func TestCloseOverDevToolsSendsBrowserClose(t *testing.T) {
	methods := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, err := readDevToolsMethod(w, r)
		if err != nil {
			method = "error: " + err.Error()
		}
		methods <- method
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := CloseOverDevTools(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/devtools/browser/x"); err != nil {
		t.Fatalf("CloseOverDevTools() error = %v", err)
	}
	select {
	case method := <-methods:
		if method != "Browser.close" {
			t.Fatalf("browser received %q, want Browser.close", method)
		}
	case <-ctx.Done():
		t.Fatal("browser received nothing")
	}
}

func TestCloseOverDevToolsFailsWhenNothingListens(t *testing.T) {
	port, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := CloseOverDevTools(ctx, strings.Replace(DebugURL(port), "http", "ws", 1)+"/devtools/browser/x"); err == nil {
		t.Fatal("CloseOverDevTools() error = nil with nothing listening")
	}
}

func TestCloseOverDevToolsGivesUpOnABrowserThatNeverAnswers(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	var conns []net.Conn
	var mu sync.Mutex
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range conns {
			_ = conn.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := CloseOverDevTools(ctx, "ws://"+listener.Addr().String()+"/devtools/browser/x"); err == nil {
		t.Fatal("CloseOverDevTools() error = nil from a browser that never answered")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("CloseOverDevTools() took %v, ignoring its context deadline", elapsed)
	}
}

// useFakeBrowser makes LaunchOwned discover this test binary as the browser,
// running in mode, and returns the file the launch arguments are written to.
func useFakeBrowser(t *testing.T, mode string) string {
	t.Helper()
	fakeLookup(t, map[string]string{"google-chrome": os.Args[0]}, nil)
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv(fakeBrowserEnv, mode)
	t.Setenv(fakeBrowserArgsEnv, argsFile)
	return argsFile
}

func launchArgs(t *testing.T, argsFile string) []string {
	t.Helper()
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read launch arguments: %v", err)
	}
	return strings.Split(string(data), "\n")
}

func assertProfileFree(t *testing.T, profile string) {
	t.Helper()
	lock, err := LockProfile(profile)
	if err != nil {
		t.Fatalf("profile still held after Stop(): %v", err)
	}
	lock.Release()
}

func TestLaunchOwnedStartsAndStopsABrowserOnTheProfile(t *testing.T) {
	argsFile := useFakeBrowser(t, "serve")
	profile := filepath.Join(t.TempDir(), "profile")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	owned, err := LaunchOwned(ctx, OwnedOptions{Profile: profile})
	if err != nil {
		t.Fatalf("LaunchOwned() error = %v", err)
	}
	if owned.Profile != profile || owned.DataDir != profile {
		t.Fatalf("LaunchOwned() profile %q in %q, want both %q", owned.Profile, owned.DataDir, profile)
	}
	if want := DebugURL(owned.Port); owned.BaseURL() != want {
		t.Fatalf("BaseURL() = %q, want %q", owned.BaseURL(), want)
	}
	if want := "ws://127.0.0.1:" + strings.TrimPrefix(owned.BaseURL(), "http://127.0.0.1:") + "/devtools/browser/fake"; owned.WSURL != want {
		t.Fatalf("WSURL = %q, want %q", owned.WSURL, want)
	}
	if owner := readProfileOwner(profile); owner == nil || owner.DevTools != owned.BaseURL() {
		t.Fatalf("profile owner = %+v, want DevTools %s", owner, owned.BaseURL())
	}
	if got, want := launchArgs(t, argsFile), LaunchArgs(profile, owned.Port, true); !reflect.DeepEqual(got, want) {
		t.Fatalf("browser launched with %q, want %q", got, want)
	}

	owned.Stop()
	select {
	case <-owned.Child.Exited():
	default:
		t.Fatal("Stop() returned before the browser exited")
	}
	if err := owned.Child.WaitErr(); err != nil {
		t.Fatalf("browser exited with %v, want a clean shutdown", err)
	}
	if owned.DataDir != "" {
		t.Fatalf("DataDir = %q after Stop(), want empty", owned.DataDir)
	}
	if _, err := os.Stat(profile); err != nil {
		t.Fatalf("Stop() removed the shared profile: %v", err)
	}
	assertProfileFree(t, profile)
}

func TestLaunchOwnedRunsHeadedInAPrivateDirectoryAndKillsAStuckBrowser(t *testing.T) {
	profile := t.TempDir()
	// The holder is started before the fake browser is set up, which it would
	// otherwise run as instead.
	holdProfileInChild(t, profile)
	argsFile := useFakeBrowser(t, "ignore-close")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	supervisor := &countingSupervisor{}
	owned, err := LaunchOwned(ctx, OwnedOptions{
		Profile:    profile,
		Headed:     true,
		Supervisor: supervisor,
		Shutdown:   ShutdownPolicy{Timeout: 200 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("LaunchOwned() error = %v", err)
	}
	dataDir := owned.DataDir
	if dataDir == profile || dataDir == "" {
		t.Fatalf("DataDir = %q, want a private directory beside the held profile", dataDir)
	}
	if got, want := launchArgs(t, argsFile), LaunchArgs(dataDir, owned.Port, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("browser launched with %q, want %q", got, want)
	}

	owned.Stop()
	if supervisor.killCount() != 1 {
		t.Fatalf("Stop() made %d kills of a browser that ignored Browser.close, want 1", supervisor.killCount())
	}
	if owned.Child.WaitErr() == nil {
		t.Fatal("WaitErr() = nil for a killed browser")
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("private directory %s still there after Stop(): %v", dataDir, err)
	}
}

// countingSupervisor runs a plain child process and counts the kills.
type countingSupervisor struct {
	execSupervisor
	mu    sync.Mutex
	kills int
}

func (s *countingSupervisor) Kill(cmd *exec.Cmd) error {
	s.mu.Lock()
	s.kills++
	s.mu.Unlock()
	return s.execSupervisor.Kill(cmd)
}

func (s *countingSupervisor) killCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kills
}

func TestLaunchOwnedStopsABrowserThatNeverAnswers(t *testing.T) {
	useFakeBrowser(t, "hang")
	profile := t.TempDir()
	supervisor := &countingSupervisor{}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	owned, err := LaunchOwned(ctx, OwnedOptions{Profile: profile, Supervisor: supervisor})
	if !errors.Is(err, context.DeadlineExceeded) || owned != nil {
		t.Fatalf("LaunchOwned() = %v, %v, want a deadline error", owned, err)
	}
	if supervisor.killCount() != 1 {
		t.Fatalf("browser killed %d times, want once", supervisor.killCount())
	}
	assertProfileFree(t, profile)
}

func TestLaunchOwnedRefusesACanceledContext(t *testing.T) {
	fakeLookup(t, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LaunchOwned(ctx, OwnedOptions{Profile: t.TempDir()}); !errors.Is(err, context.Canceled) {
		t.Fatalf("LaunchOwned() error = %v, want context.Canceled", err)
	}
}

func TestLaunchOwnedReportsAMissingBrowser(t *testing.T) {
	fakeLookup(t, nil, nil)
	_, err := LaunchOwned(context.Background(), OwnedOptions{Binary: "no-such-browser", Profile: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "no-such-browser") {
		t.Fatalf("LaunchOwned() error = %v, want the missing binary named", err)
	}
}

func TestLaunchOwnedReportsAnUncreatableProfile(t *testing.T) {
	fakeLookup(t, map[string]string{"google-chrome": os.Args[0]}, nil)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LaunchOwned(context.Background(), OwnedOptions{Profile: filepath.Join(file, "profile")})
	if err == nil || !strings.Contains(err.Error(), "create browser profile directory") {
		t.Fatalf("LaunchOwned() error = %v, want a profile directory error", err)
	}
}

func TestLaunchOwnedReportsAnUnclaimableProfile(t *testing.T) {
	fakeLookup(t, map[string]string{"google-chrome": os.Args[0]}, nil)
	profile := t.TempDir()
	if err := os.Mkdir(filepath.Join(profile, profileLockName), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := LaunchOwned(context.Background(), OwnedOptions{Profile: profile})
	if err == nil || !strings.Contains(err.Error(), "open browser profile lock") {
		t.Fatalf("LaunchOwned() error = %v, want a profile lock error", err)
	}
}

func TestLaunchOwnedReleasesTheProfileWhenTheBrowserCannotStart(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-browser")
	fakeLookup(t, map[string]string{"google-chrome": missing}, nil)
	profile := t.TempDir()
	_, err := LaunchOwned(context.Background(), OwnedOptions{Profile: profile})
	if err == nil || !strings.Contains(err.Error(), "start browser ("+missing+")") {
		t.Fatalf("LaunchOwned() error = %v, want a start error naming %s", err, missing)
	}
	assertProfileFree(t, profile)
}
