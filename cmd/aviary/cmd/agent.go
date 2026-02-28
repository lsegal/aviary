package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents",
}

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured agents and their current state",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Agents: (not yet implemented)")
		return nil
	},
}

var agentRunFile string

var agentRunCmd = &cobra.Command{
	Use:   "run <name> [message]",
	Short: "Send a message to an agent and stream the response",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		msg := ""
		if len(args) > 1 {
			msg = args[1]
		}
		fmt.Printf("Running agent %q with message %q (not yet implemented)\n", name, msg)
		return nil
	},
}

var agentStopCmd = &cobra.Command{
	Use:   "stop <name>",
	Short: "Immediately stop all work in progress for an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Stopping agent %q (not yet implemented)\n", args[0])
		return nil
	},
}

func init() {
	agentRunCmd.Flags().StringVar(&agentRunFile, "file", "", "read prompt from file")
	agentCmd.AddCommand(agentListCmd, agentRunCmd, agentStopCmd)
	rootCmd.AddCommand(agentCmd)
}
