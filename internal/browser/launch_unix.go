//go:build !windows

package browser

import (
	"os/exec"
	"syscall"
)

// platformChromePaths returns well-known Chrome install locations on Unix/macOS.
func platformChromePaths() []string {
	return []string{
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
		"/usr/local/bin/chromium",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	}
}

// detachProcess configures cmd so Chrome runs in its own session,
// surviving after the parent process exits.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
