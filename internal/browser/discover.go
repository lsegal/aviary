package browser

import (
	"fmt"
	"os"
	"os/exec"
)

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

// These indirections let the tests exercise discovery without a browser
// installed.
var (
	lookPath   = exec.LookPath
	statBinary = func(path string) error { _, err := os.Stat(path); return err }
)

// FindBinary returns the browser executable to launch. An override is
// resolved through PATH so a bare command name works as well as a full path,
// and a bad one is reported instead of being silently replaced by a discovered
// browser: a user who named a browser meant that browser.
func FindBinary(override string) (string, error) {
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
	return "", fmt.Errorf("no Chrome, Chromium or Edge installation found: install Google Chrome, or set the browser binary setting to the path to a Chromium-based browser (Safari cannot be used, it has no DevTools Protocol)")
}
