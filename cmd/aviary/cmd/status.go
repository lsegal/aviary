package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show server status, uptime, and connected agents",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Server status: (not yet implemented)")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
