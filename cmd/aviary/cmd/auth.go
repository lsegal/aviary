package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication credentials",
}

var authLoginCmd = &cobra.Command{
	Use:   "login <provider>",
	Short: "Authorize via OAuth (opens browser)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("OAuth login for %q (not yet implemented)\n", args[0])
		return nil
	},
}

var authSetCmd = &cobra.Command{
	Use:   "set <name> <value>",
	Short: "Store a credential (API key or token) by name",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Stored credential %q (not yet implemented)\n", args[0])
		return nil
	},
}

var authGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Show the credential name (value is masked)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Credential %q: ****** (not yet implemented)\n", args[0])
		return nil
	},
}

var authListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all stored credential names",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Credentials: (not yet implemented)")
		return nil
	},
}

var authDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Remove a stored credential",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Deleted credential %q (not yet implemented)\n", args[0])
		return nil
	},
}

func init() {
	authCmd.AddCommand(authLoginCmd, authSetCmd, authGetCmd, authListCmd, authDeleteCmd)
	rootCmd.AddCommand(authCmd)
}
