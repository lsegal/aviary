package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"
)

// chromeVersionInfo is the response from Chrome's /json/version endpoint.
type chromeVersionInfo struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// ChromeTab represents a single open tab from Chrome's /json/list endpoint.
type ChromeTab struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

var launchChromeWithModeFn = launchChromeWithMode
var shouldLaunchHeadlessFn = shouldLaunchHeadless
var waitForChromeFn = waitForChrome

// fetchTabs returns all open tabs from the Chrome CDP /json/list endpoint.
func fetchTabs(cdpBaseURL string) ([]ChromeTab, error) {
	resp, err := http.Get(cdpBaseURL + "/json/list") //nolint:noctx
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	var tabs []ChromeTab
	if err := json.NewDecoder(resp.Body).Decode(&tabs); err != nil {
		return nil, err
	}
	return tabs, nil
}

// createTab creates a new tab at the given URL via Chrome's CDP /json/new endpoint.
func createTab(ctx context.Context, cdpBaseURL, pageURL string) (*ChromeTab, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, cdpBaseURL+"/json/new?"+url.QueryEscape(pageURL), nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("creating tab: unexpected status %s", resp.Status)
	}

	var tab ChromeTab
	if err := json.NewDecoder(resp.Body).Decode(&tab); err != nil {
		return nil, err
	}
	if tab.ID == "" {
		return nil, fmt.Errorf("creating tab: empty tab id in response")
	}

	return &tab, nil
}

// findChrome returns the path to a Chrome/Chromium executable.
// binary is used as-is if non-empty; otherwise well-known paths are searched.
func findChrome(binary string) (string, error) {
	if binary != "" {
		return binary, nil
	}

	// Search PATH for common names.
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}

	// Platform-specific well-known paths.
	for _, path := range platformChromePaths() {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("Chrome/Chromium not found; install Chrome or set browser.binary in aviary.yaml")
}

// fetchWebSocketURL queries Chrome's CDP /json/version endpoint and returns
// the browser-level WebSocket debugger URL.
func fetchWebSocketURL(cdpBaseURL string) (string, error) {
	resp, err := http.Get(cdpBaseURL + "/json/version") //nolint:noctx
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	var info chromeVersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	if info.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("no webSocketDebuggerUrl in CDP response")
	}
	return info.WebSocketDebuggerURL, nil
}

// launchChrome starts Chrome as a detached OS process that will survive after
// the calling process exits. It does not wait for Chrome to become ready.
func launchChrome(m *Manager) error {
	return launchChromeWithMode(m, shouldLaunchHeadless(m))
}

func shouldLaunchHeadless(m *Manager) bool {
	return m.headless || testing.Testing() || (os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "")
}

func launchChromeWithMode(m *Manager, headless bool) error {
	chromePath, err := findChrome(m.binary)
	if err != nil {
		return err
	}

	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", m.cdpPort),
		fmt.Sprintf("--user-data-dir=%s", m.userDataDir()),
		"--no-first-run",
		"--no-default-browser-check",
	}
	if headless {
		args = append(args, "--headless", "--disable-gpu")
	}

	cmd := exec.Command(chromePath, args...)
	detachProcess(cmd) // platform-specific: ensures Chrome outlives this process

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting Chrome (%s): %w", chromePath, err)
	}
	return nil
}

func launchChromeAndWait(ctx context.Context, m *Manager, cdpBaseURL string, headless bool) (string, error) {
	if err := launchChromeWithModeFn(m, headless); err != nil {
		return "", err
	}
	slog.Info("browser: chrome launched", "port", m.cdpPort, "headless", headless)

	waitCtx, cancel := withDefaultTimeout(ctx, 15*time.Second)
	defer cancel()
	return waitForChromeFn(waitCtx, cdpBaseURL)
}

// waitForChrome polls the CDP endpoint until Chrome is ready or the context expires.
func waitForChrome(ctx context.Context, cdpBaseURL string) (string, error) {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("chrome did not become ready at %s: %w", cdpBaseURL, ctx.Err())
		case <-ticker.C:
			if wsURL, err := fetchWebSocketURL(cdpBaseURL); err == nil {
				return wsURL, nil
			}
		}
	}
}
