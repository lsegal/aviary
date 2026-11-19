package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// Config collects the overrides that decide which browser Aviary drives
// and where it keeps that browser's state. Both are empty by default, which
// means "discover a Chromium-based browser" and "use Aviary's own profile
// directory"; the -browser flags fill them in.
type Config struct {
	// Binary is an explicit browser executable (--browser-binary).
	Binary string
	// Profile is an explicit profile directory (--browser-profile).
	Profile string
	// Headed launches a visible browser window instead of a headless one. It is
	// how `Aviary auth` puts a login form in front of the user, on the same
	// profile the headless watch loop reads GitHub with.
	Headed bool
	// Supervisor starts, waits on, and kills the browser process. A nil
	// supervisor hands it to Aviary's own child-process tracking, so it cannot
	// outlive the run that started it.
	Supervisor Supervisor
}

const (
	// profileName is the directory, under Aviary's own configuration
	// directory, that holds the browser profile Aviary owns. It is deliberately
	// separate from the user's everyday browser profile: Aviary logs a browser
	// into GitHub and drives it unattended, which has no business touching the
	// profile the user browses with.
	ProfileName = "browser-data"
	// readyTimeout bounds the wait for a freshly launched browser to
	// start serving the DevTools endpoint. A cold profile on a slow machine
	// takes several seconds; anything past this is a failed launch.
	readyTimeout = 30 * time.Second
	// readyInterval is how often the DevTools endpoint is polled while
	// waiting for the browser to come up.
	readyInterval = 100 * time.Millisecond
	// targetTimeout bounds the wait for the window a headed browser
	// opens by itself to show up in the target list. The browser is already
	// answering the DevTools endpoint by the time it is asked, so the window is
	// normally there on the first read; past this it is not coming.
	targetTimeout = 2 * time.Second
	// commandTimeout bounds a single command sent to a tab: a
	// navigation, a reload, a script evaluation, a screenshot. GitHub pages
	// are large and a run paces its loads, so this is deliberately generous;
	// what it exists to catch is a page or a renderer that has stopped
	// answering altogether, which would otherwise park the poll loop for the
	// rest of the run (issue #472).
	commandTimeout = 2 * time.Minute
)

// tabOpenTimeout bounds opening a tab and enabling the events it is read
// through. It is a variable only so tests need not spend it.
var tabOpenTimeout = 30 * time.Second

// binaryNames are the Chromium-based executables looked for on PATH, in
// preference order. Edge is included because it is Chromium underneath and so
// speaks the same DevTools Protocol; Safari has no CDP and cannot be used.
var binaryNames = []string{
	"google-chrome",
	"google-chrome-stable",
	"chromium",
	"chromium-browser",
	"msedge",
}

// These indirections let the tests exercise discovery, profile paths, and the
// readiness wait without a browser installed and without launching one.
var (
	lookPath   = exec.LookPath
	statBinary = func(path string) error { _, err := os.Stat(path); return err }
	configDir  = os.UserConfigDir
)

// ProfileDir reports the profile directory the browser is launched
// against: the override when one was given, otherwise Aviary's own directory
// under the platform configuration directory (`~/Library/Application
// Support/Aviary/browser-data` on macOS, `%AppData%\Aviary\browser-data` on
// Windows, `~/.config/Aviary/browser-data` on Linux).
func ProfileDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	dir, err := configDir()
	if err != nil {
		return "", fmt.Errorf("locate browser profile directory: %w", err)
	}
	return filepath.Join(dir, "aviary", ProfileName), nil
}

// findBinary returns the browser executable to launch. An override is
// resolved through PATH so a bare command name works as well as a full path,
// and a bad one is reported instead of being silently replaced by a discovered
// browser: a user who named a browser meant that browser.
func findBinary(override string) (string, error) {
	if override != "" {
		path, err := lookPath(override)
		if err != nil {
			return "", fmt.Errorf("browser binary %q not found or not executable: %w", override, err)
		}
		return path, nil
	}
	for _, name := range binaryNames {
		if path, err := lookPath(name); err == nil {
			return path, nil
		}
	}
	for _, path := range binaryPaths() {
		if path == "" {
			continue
		}
		if err := statBinary(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no Chrome, Chromium or Edge installation found: install Google Chrome, or pass --browser-binary with the path to a Chromium-based browser (Safari cannot be used, it has no DevTools Protocol)")
}

// freePort reserves a port for the browser's DevTools endpoint by
// letting the OS assign one and closing the listener again. Several Aviary
// instances can watch at once, so a fixed port would have them collide on the
// endpoint and drive each other's browser.
func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve browser debugging port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, fmt.Errorf("reserve browser debugging port: %w", err)
	}
	return port, nil
}

// launchArgs are the flags Aviary launches the browser with. The new
// headless mode is asked for by name because it is the one that behaves like a
// real browser; the first-run and default-browser prompts would otherwise block
// a fresh profile from ever reaching the DevTools endpoint. A headed launch
// drops the headless flags so a window actually appears, and keeps everything
// else so it lands on the same profile and DevTools endpoint.
func launchArgs(profile string, port int, headed bool) []string {
	args := make([]string, 0, 6)
	if !headed {
		args = append(args, "--headless=new", "--disable-gpu")
	}
	return append(args,
		"--no-first-run",
		"--no-default-browser-check",
		"--user-data-dir="+profile,
		"--remote-debugging-port="+strconv.Itoa(port),
	)
}

// debugURL is the base URL of the DevTools endpoint on a given port.
func debugURL(port int) string {
	return "http://127.0.0.1:" + strconv.Itoa(port)
}

// process is a running browser and the DevTools endpoint it answers on.
type process struct {
	cmd   *child
	port  int
	wsURL string
	// profile is the profile the browser was launched for, so a page that
	// turns out to have been read by a signed-out session can name the
	// directory to sign in, and so the saved sign-in is kept in one place.
	profile string
	// dataDir is the --user-data-dir the browser actually runs in: profile
	// itself when this instance holds it, or a private directory of its own
	// when another Aviary instance already does (issue #663).
	dataDir string
	// lock is this instance's claim on profile, nil when it runs in a private
	// directory instead.
	lock *profileLock
}

// launch starts a browser against Aviary's own profile and waits
// for its DevTools endpoint to answer. The browser is started as a tracked
// child process, so Aviary's orphan guards take it down on every exit path,
// including one that runs no cleanup of its own.
//
// ctx bounds the launch only. Canceling it later does not kill the browser:
// the run that owns it closes it, cleanly, on its way out, and a kill would
// skip the shutdown that writes the profile to disk (issue #773).
func launch(ctx context.Context, config Config) (*process, error) {
	// A run that is already shutting down must not start a browser, such as
	// the one a sign-in window hands the profile back to.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	binary, err := findBinary(config.Binary)
	if err != nil {
		return nil, err
	}
	profile, err := ProfileDir(config.Profile)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return nil, fmt.Errorf("create browser profile directory %s: %w", profile, err)
	}
	lock, dataDir, err := claimProfile(profile)
	if err != nil {
		return nil, err
	}
	proc := &process{profile: profile, dataDir: dataDir, lock: lock}
	port, err := freePort()
	if err != nil {
		proc.release()
		return nil, err
	}
	cmd := exec.Command(binary, launchArgs(dataDir, port, config.Headed)...)
	// The browser writes a steady stream of diagnostics to standard error that
	// would otherwise be painted over Aviary's terminal UI.
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	started, err := startChild(config.supervisor(), cmd)
	if err != nil {
		proc.release()
		return nil, fmt.Errorf("start browser (%s): %w", binary, err)
	}
	proc.cmd, proc.port = started, port
	wsURL, err := waitForReady(ctx, debugURL(port), readyTimeout)
	if err != nil {
		_ = proc.stop()
		return nil, err
	}
	proc.wsURL = wsURL
	return proc, nil
}

// stop shuts the browser down, cleanly when it will go, and then gives up
// whatever it was run in.
func (p *process) stop() error {
	p.cmd.stop(p.wsURL)
	p.release()
	return nil
}

// release gives up the profile claim, or removes the private directory the
// browser ran in instead. It runs once the browser is stopped, because a
// running browser still holds the directory.
func (p *process) release() {
	p.lock.release()
	p.lock = nil
	if p.dataDir != "" && p.dataDir != p.profile {
		_ = os.RemoveAll(p.dataDir)
	}
	p.dataDir = ""
}

// versionInfo is the part of the DevTools /json/version response Aviary
// reads: the browser-level WebSocket to drive the browser over.
type versionInfo struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// waitForReady polls the DevTools endpoint until the browser answers with the
// WebSocket to connect on, or the timeout expires. The endpoint refuses
// connections until the browser is genuinely ready, so polling it is the
// readiness check rather than a guess at a startup delay.
func waitForReady(ctx context.Context, baseURL string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(readyInterval)
	defer ticker.Stop()
	for {
		if wsURL, err := webSocketURL(ctx, baseURL); err == nil {
			return wsURL, nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("wait for browser at %s: %w", baseURL, ctx.Err())
		case <-ticker.C:
		}
	}
}

// webSocketURL asks the DevTools endpoint for the browser-level
// WebSocket URL.
func webSocketURL(ctx context.Context, baseURL string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/json/version", nil)
	if err != nil {
		return "", err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("browser DevTools endpoint returned %s", response.Status)
	}
	var info versionInfo
	if err := json.NewDecoder(response.Body).Decode(&info); err != nil {
		return "", err
	}
	if info.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("browser DevTools endpoint reported no WebSocket URL")
	}
	return info.WebSocketDebuggerURL, nil
}

// targetInfo is the part of a DevTools /json/list entry Aviary reads: a
// target's id and what kind of target it is.
type targetInfo struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// openPageTarget returns the id of a page the browser already has open.
// A headed browser puts a window on screen before Aviary ever connects -- Chrome
// always opens one -- so the auth flow drives that window rather than asking
// for a new target, which would leave an empty "New Tab" window sitting beside
// the login window (issue #412).
//
// The target list is polled rather than read once because the initial window is
// created alongside the DevTools endpoint, not before it, and an empty list is
// reported as an error so the caller can fall back to opening its own tab.
func openPageTarget(ctx context.Context, baseURL string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(readyInterval)
	defer ticker.Stop()
	for {
		if id, err := pageTargetID(ctx, baseURL); err == nil && id != "" {
			return id, nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("no open browser window at %s: %w", baseURL, ctx.Err())
		case <-ticker.C:
		}
	}
}

// pageTargetID asks the DevTools endpoint for its targets and returns
// the first page among them. Anything that is not a page -- the DevTools
// front-end itself, service workers, extension backgrounds -- is skipped: they
// are not windows and cannot be navigated to GitHub.
func pageTargetID(ctx context.Context, baseURL string) (string, error) {
	pages, err := openPages(ctx, baseURL)
	if err != nil {
		return "", err
	}
	if len(pages) == 0 {
		return "", fmt.Errorf("browser DevTools endpoint reported no open page")
	}
	return pages[0], nil
}

// openPages asks the DevTools endpoint for its targets and returns the ids of
// the pages among them.
func openPages(ctx context.Context, baseURL string) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/json/list", nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("browser DevTools endpoint returned %s", response.Status)
	}
	var targets []targetInfo
	if err := json.NewDecoder(response.Body).Decode(&targets); err != nil {
		return nil, err
	}
	var pages []string
	for _, info := range targets {
		if info.Type == "page" && info.ID != "" {
			pages = append(pages, info.ID)
		}
	}
	return pages, nil
}
