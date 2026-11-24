package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/buildinfo"
	"github.com/lsegal/aviary/internal/update"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show the Aviary version",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "aviary version %s\ninstall type: %s\n", buildinfo.Version, update.DescribeInstall())
		return err
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
