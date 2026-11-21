package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/cliagent"
)

// cliHookCmd answers the PreToolUse hook Aviary installs in a Claude Code
// session, refusing shell commands the agent's Allowed Exec Commands refuse.
var cliHookCmd = &cobra.Command{
	Use:    "cli-hook",
	Short:  "Check a CLI agent's shell call against its Allowed Exec Commands",
	Args:   cobra.NoArgs,
	Hidden: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cliagent.RunCommandHook(cmd.InOrStdin(), cmd.OutOrStdout(), os.Getenv)
	},
}

func init() {
	rootCmd.AddCommand(cliHookCmd)
}
