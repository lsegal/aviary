package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/server"
)

var tokenNewFlag bool

var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Show or regenerate the Aviary server token",
	Long: `Show the current Aviary authentication token, or generate a new one.

The token is used to authenticate with the Aviary web UI and API.
Run with --new to replace the existing token with a freshly generated one.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if tokenNewFlag {
			tok, err := server.GenerateToken()
			if err != nil {
				return fmt.Errorf("generating token: %w", err)
			}
			fmt.Println("New token generated:")
			fmt.Println(tok)
			return nil
		}

		tok, err := server.LoadToken()
		if err != nil {
			return err
		}
		fmt.Println(tok)
		return nil
	},
}

func init() {
	tokenCmd.Flags().BoolVar(&tokenNewFlag, "new", false, "Generate and store a new token")
	rootCmd.AddCommand(tokenCmd)
}
