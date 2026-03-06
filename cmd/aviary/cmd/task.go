package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Manage scheduled tasks",
}

var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tasks, their trigger type, and last run status",
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Println("Tasks: (not yet implemented)")
		return nil
	},
}

var taskRunCmd = &cobra.Command{
	Use:   "run <name>",
	Short: "Manually trigger a task right now",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		fmt.Printf("Running task %q (not yet implemented)\n", args[0])
		return nil
	},
}

var taskStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop all currently running scheduled task jobs",
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Println("Stopping all tasks... (not yet implemented)")
		return nil
	},
}

func init() {
	taskCmd.AddCommand(taskListCmd, taskRunCmd, taskStopCmd)
	rootCmd.AddCommand(taskCmd)
}
