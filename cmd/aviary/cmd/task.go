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
	Use:   "stop [name]",
	Short: "Stop running scheduled task jobs (all or a single task)",
	Long: `
Stop running scheduled task jobs. When called without arguments this stops
all pending and running jobs. When given a single argument it stops the
specified task (format: <agent>/<task> or <task-name>).
`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		params := map[string]any{}
		if len(args) == 1 {
			params["name"] = args[0]
		}
		out, err := dispatcher.CallTool(cmd.Context(), "task_stop", params)
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var taskMoveToFileCmd = &cobra.Command{
	Use:   "move-to-file <agent> <task>",
	Short: "Move a task from aviary.yaml to a markdown file in the agent's tasks/ directory",
	Long: `Move a scheduled task that is currently defined inline in aviary.yaml to a
markdown file inside the agent's tasks/ directory. The task is removed from
aviary.yaml and written as <task-name>.md.

Example:
  aviary task move-to-file myagent daily-report`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := dispatcher.CallTool(cmd.Context(), "config_task_move_to_file", map[string]any{
			"agent": args[0],
			"task":  args[1],
		})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

func init() {
	taskCmd.AddCommand(taskListCmd, taskRunCmd, taskStopCmd, taskMoveToFileCmd)
	rootCmd.AddCommand(taskCmd)
}
