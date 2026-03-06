// Package cmd implements the aviary CLI subcommands.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents",
}

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured agents and their current state",
	RunE: func(cmd *cobra.Command, _ []string) error {
		out, err := dispatcher.CallTool(cmd.Context(), "agent_list", nil)
		if err != nil {
			return err
		}
		fmt.Println(out)
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
		if agentRunFile != "" {
			data, err := os.ReadFile(agentRunFile)
			if err != nil {
				return fmt.Errorf("reading file: %w", err)
			}
			msg = string(data)
		}
		if msg == "" {
			return fmt.Errorf("message required: pass as argument or use --file")
		}
		out, err := dispatcher.CallTool(cmd.Context(), "agent_run", map[string]any{
			"name":    name,
			"message": msg,
		})
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	},
}

var agentStopCmd = &cobra.Command{
	Use:   "stop <name>",
	Short: "Immediately stop all work in progress for an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := dispatcher.CallTool(cmd.Context(), "agent_stop", map[string]any{
			"name": args[0],
		})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

func init() {
	agentRunCmd.Flags().StringVar(&agentRunFile, "file", "", "read prompt from file")
	agentCmd.AddCommand(agentListCmd, agentRunCmd, agentStopCmd)
	rootCmd.AddCommand(agentCmd)
}
