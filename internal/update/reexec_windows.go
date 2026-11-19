//go:build windows

package update

import (
	"os"
	"os/exec"
)

// Reexec starts exe as a new process with the same console. Windows cannot
// replace a running process image, so the caller exits after this returns.
func Reexec(exe string, args []string, env []string) error {
	cmd := exec.Command(exe, args...) //nolint:gosec
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}
