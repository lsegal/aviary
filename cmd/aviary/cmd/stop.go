package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/server"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Aviary server",
	RunE:  runStop,
}

func init() {
	rootCmd.AddCommand(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	running, pid, err := server.IsRunning()
	if err != nil {
		return fmt.Errorf("checking server status: %w", err)
	}
	if !running {
		if pid != 0 {
			// PID file exists but process is gone — clean up.
			_ = server.RemovePID()
		}
		fmt.Println("Aviary is not running.")
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("finding process %d: %w", pid, err)
	}

	if err := proc.Signal(os.Interrupt); err != nil {
		return fmt.Errorf("sending interrupt to PID %d: %w", pid, err)
	}

	fmt.Printf("Sent stop signal to Aviary (PID %d).\n", pid)
	return nil
}
