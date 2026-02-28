package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Aviary server",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Stopping Aviary server... (not yet implemented)")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
