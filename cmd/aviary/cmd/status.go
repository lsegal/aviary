package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/server"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether the Aviary server is running",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	running, pid, err := server.IsRunning()
	if err != nil {
		return fmt.Errorf("checking server status: %w", err)
	}
	if !running {
		if pid != 0 {
			// Stale PID file — clean it up.
			_ = server.RemovePID()
		}
		fmt.Println("Aviary is not running.")
		return nil
	}
	fmt.Printf("Aviary is running (PID %d).\n", pid)
	fmt.Printf("PID file: %s\n", server.PIDPath())
	return nil
}
