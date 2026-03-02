//go:build windows

package browser

import (
	"os"
	"os/exec"
	"syscall"
)

// platformChromePaths returns well-known Chrome install locations on Windows.
func platformChromePaths() []string {
	localAppData := os.Getenv("LOCALAPPDATA")
	programFiles := os.Getenv("PROGRAMFILES")
	programFilesX86 := os.Getenv("PROGRAMFILES(X86)")
	return []string{
		programFiles + `\Google\Chrome\Application\chrome.exe`,
		programFilesX86 + `\Google\Chrome\Application\chrome.exe`,
		localAppData + `\Google\Chrome\Application\chrome.exe`,
		programFiles + `\Microsoft\Edge\Application\msedge.exe`,
		programFilesX86 + `\Microsoft\Edge\Application\msedge.exe`,
	}
}

// detachProcess configures cmd so Chrome runs in its own process group,
// surviving after the parent process exits.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
