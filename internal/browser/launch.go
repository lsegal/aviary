package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"testing"
)

var launchChromeWithModeFn = launchChromeWithMode
var shouldLaunchHeadlessFn = shouldLaunchHeadless
var waitForChromeFn = waitForChrome

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

// launchChrome starts Chrome as a detached OS process that will survive after
// the calling process exits. It does not wait for Chrome to become ready.
func launchChrome(m *Manager) error {
	return launchChromeWithMode(m, shouldLaunchHeadless(m))
}

func shouldLaunchHeadless(m *Manager) bool {
	return m.headless || testing.Testing() || shouldAutoLaunchHeadless(runtime.GOOS)
}

func shouldAutoLaunchHeadless(goos string) bool {
	if goos == "windows" {
		return false
	}
	return os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == ""
}

func launchChromeWithMode(m *Manager, headless bool) error {
	chromePath, err := FindBinary(m.binary)
	if err != nil {
		return err
	}
	if err := checkProfileFree(m.userDataDir()); err != nil {
		return err
	}

	cmd := exec.Command(chromePath, LaunchArgs(m.userDataDir(), m.cdpPort, headless)...)
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

	waitCtx, cancel := withDefaultTimeout(ctx, DetachedReadyTimeout)
	defer cancel()
	return waitForChromeFn(waitCtx, cdpBaseURL)
}

// waitForChrome polls the CDP endpoint until Chrome is ready or the context expires.
func waitForChrome(ctx context.Context, cdpBaseURL string) (string, error) {
	return WaitForReady(ctx, cdpBaseURL, DetachedReadyTimeout, DetachedReadyInterval)
}

// checkProfileFree refuses to launch the detached browser on a profile an
// owned browser of another Aviary run already holds. Chrome would hand the
// launch to that browser and exit without ever serving this DevTools port, so
// the launch would only time out. The lock is released again at once: the
// detached browser outlives this process, so it cannot hold the claim, and an
// owned browser started later sees it through chromeHoldsProfile instead.
func checkProfileFree(profile string) error {
	lock, err := LockProfile(profile)
	if errors.Is(err, ErrProfileLocked) {
		return fmt.Errorf("%w (%s): set the browser profile_directory setting to use a different directory", err, profile)
	}
	lock.Release()
	return nil
}
