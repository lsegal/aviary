package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Manage agent memory",
}

var memorySearchCmd = &cobra.Command{
	Use:   "search <agent> <query>",
	Short: "Search an agent's memory",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := dispatcher.CallTool(cmd.Context(), "memory_search", map[string]any{
			"agent": args[0],
			"query": args[1],
		})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var memoryShowCmd = &cobra.Command{
	Use:   "show <agent>",
	Short: "Display the full memory for an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := dispatcher.CallTool(cmd.Context(), "memory_show", map[string]any{"agent": args[0]})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var memoryClearCmd = &cobra.Command{
	Use:   "clear <agent>",
	Short: "Wipe all memory for an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := dispatcher.CallTool(cmd.Context(), "memory_clear", map[string]any{"agent": args[0]})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

func init() {
	memoryCmd.AddCommand(memorySearchCmd, memoryShowCmd, memoryClearCmd)
	rootCmd.AddCommand(memoryCmd)
}
