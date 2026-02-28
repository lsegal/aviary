package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Aviary server",
	Long:  `Start the Aviary server over HTTPS on the configured port (default: 16677).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Starting Aviary server... (not yet implemented)")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
