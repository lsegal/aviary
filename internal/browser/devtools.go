package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

// Readiness timeouts for the two launch modes. A detached browser is the
// long-lived one Aviary's browser tools share, launched on demand inside a
// tool call; an owned browser is started cold for a run, often on a fresh or
// private profile, which takes several seconds on a slow machine.
const (
	DetachedReadyTimeout  = 15 * time.Second
	DetachedReadyInterval = 300 * time.Millisecond
	OwnedReadyTimeout     = 30 * time.Second
	OwnedReadyInterval    = 100 * time.Millisecond
)

// ChromeTab represents a single open tab from Chrome's /json/list endpoint.
type ChromeTab struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

// versionInfo is the part of the DevTools /json/version response Aviary
// reads: the browser-level WebSocket to drive the browser over.
type versionInfo struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// DebugURL is the base URL of the DevTools endpoint on a given port.
func DebugURL(port int) string {
	return "http://127.0.0.1:" + strconv.Itoa(port)
}

// FreePort reserves a port for a browser's DevTools endpoint by letting the OS
// assign one and closing the listener again. Several Aviary instances can run
// owned browsers at once, so a fixed port would have them collide on the
// endpoint and drive each other's browser.
func FreePort() (int, error) {
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

// LaunchArgs are the flags Aviary launches a browser with. The new headless
// mode is asked for by name because it is the one that behaves like a real
// browser; the first-run and default-browser prompts would otherwise block a
// fresh profile from ever reaching the DevTools endpoint. A headed launch drops
// the headless flags so a window actually appears, and keeps everything else
// so it lands on the same profile and DevTools endpoint.
func LaunchArgs(userDataDir string, port int, headless bool) []string {
	args := make([]string, 0, 6)
	if headless {
		args = append(args, "--headless=new", "--disable-gpu")
	}
	return append(args,
		"--no-first-run",
		"--no-default-browser-check",
		"--user-data-dir="+userDataDir,
		"--remote-debugging-port="+strconv.Itoa(port),
	)
}

// WebSocketURL asks the DevTools endpoint for the browser-level WebSocket URL.
func WebSocketURL(ctx context.Context, baseURL string) (string, error) {
	var info versionInfo
	if err := getJSON(ctx, baseURL+"/json/version", &info); err != nil {
		return "", err
	}
	if info.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("browser DevTools endpoint reported no WebSocket URL")
	}
	return info.WebSocketDebuggerURL, nil
}

// WaitForReady polls the DevTools endpoint every interval until the browser
// answers with the WebSocket to connect on, or the timeout expires. The
// endpoint refuses connections until the browser is genuinely ready, so polling
// it is the readiness check rather than a guess at a startup delay.
func WaitForReady(ctx context.Context, baseURL string, timeout, interval time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if wsURL, err := WebSocketURL(ctx, baseURL); err == nil {
			return wsURL, nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("wait for browser at %s: %w", baseURL, ctx.Err())
		case <-ticker.C:
		}
	}
}

// PageTargets asks the DevTools endpoint for its targets and returns the pages
// among them. Anything that is not a page -- the DevTools front-end itself,
// service workers, extension backgrounds -- is skipped: they are not tabs.
func PageTargets(ctx context.Context, baseURL string) ([]ChromeTab, error) {
	var targets []ChromeTab
	if err := getJSON(ctx, baseURL+"/json/list", &targets); err != nil {
		return nil, err
	}
	var pages []ChromeTab
	for _, target := range targets {
		if target.Type == "page" && target.ID != "" {
			pages = append(pages, target)
		}
	}
	return pages, nil
}

// WaitForPageTarget returns the id of a page the browser already has open,
// polling every interval until one appears or the timeout expires. A headed
// browser creates its first window alongside the DevTools endpoint, not before
// it, so a single read can miss it.
func WaitForPageTarget(ctx context.Context, baseURL string, timeout, interval time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if pages, err := PageTargets(ctx, baseURL); err == nil && len(pages) > 0 {
			return pages[0].ID, nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("no open browser window at %s: %w", baseURL, ctx.Err())
		case <-ticker.C:
		}
	}
}

// getJSON reads a DevTools HTTP endpoint and decodes its JSON response.
func getJSON(ctx context.Context, url string, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("browser DevTools endpoint returned %s", response.Status)
	}
	return json.NewDecoder(response.Body).Decode(out)
}
