package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "View job history and logs",
}

var jobListTask string

var jobListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show job history across all tasks",
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Println("Jobs: (not yet implemented)")
		return nil
	},
}

var jobLogsCmd = &cobra.Command{
	Use:   "logs <job-id>",
	Short: "Stream logs for a specific job run",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		fmt.Printf("Logs for job %q (not yet implemented)\n", args[0])
		return nil
	},
}

func init() {
	jobListCmd.Flags().StringVar(&jobListTask, "task", "", "filter by task name")
	jobCmd.AddCommand(jobListCmd, jobLogsCmd)
	rootCmd.AddCommand(jobCmd)
}
