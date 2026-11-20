//go:build !windows

package browser

// binaryPaths lists the well-known Chrome, Chromium and Edge install
// locations on macOS and Linux, searched after PATH so a browser installed
// somewhere the shell cannot see is still found. macOS application bundles are
// included because nothing there is on PATH.
func binaryPaths() []string {
	return []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
		"/usr/local/bin/google-chrome",
		"/usr/local/bin/chromium",
		"/opt/google/chrome/chrome",
		"/usr/bin/microsoft-edge",
		"/usr/bin/microsoft-edge-stable",
		"/opt/microsoft/msedge/msedge",
		"/snap/bin/chromium",
	}
}
