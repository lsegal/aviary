//go:build windows

package browser

import (
	"os"
	"path/filepath"
)

// binaryPaths lists the well-known Chrome and Edge install locations on
// Windows, searched after PATH because neither installer puts its executable
// there. Entries whose base directory is unset are returned empty and skipped
// by the caller.
func binaryPaths() []string {
	programFiles := os.Getenv("PROGRAMFILES")
	programFilesX86 := os.Getenv("PROGRAMFILES(X86)")
	localAppData := os.Getenv("LOCALAPPDATA")
	return []string{
		installPath(programFiles, "Google", "Chrome", "Application", "chrome.exe"),
		installPath(programFilesX86, "Google", "Chrome", "Application", "chrome.exe"),
		installPath(localAppData, "Google", "Chrome", "Application", "chrome.exe"),
		installPath(programFiles, "Chromium", "Application", "chrome.exe"),
		installPath(localAppData, "Chromium", "Application", "chrome.exe"),
		installPath(programFiles, "Microsoft", "Edge", "Application", "msedge.exe"),
		installPath(programFilesX86, "Microsoft", "Edge", "Application", "msedge.exe"),
	}
}

// installPath joins an install location onto its base directory,
// reporting an empty path when the environment does not define that base.
func installPath(base string, elements ...string) string {
	if base == "" {
		return ""
	}
	return filepath.Join(append([]string{base}, elements...)...)
}
