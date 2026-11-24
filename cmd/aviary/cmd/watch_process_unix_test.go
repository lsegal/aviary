//go:build !windows

package cmd

import (
	"os"
	"os/exec"
)

// prepareWatchProcess sets up a helper process before it starts so the
// test can interrupt it.
func prepareWatchProcess(*exec.Cmd) {}

// interruptWatchProcess sends a helper process the Ctrl+C a user would.
func interruptWatchProcess(cmd *exec.Cmd) error {
	return cmd.Process.Signal(os.Interrupt)
}
