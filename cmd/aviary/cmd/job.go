package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/domain"
)

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "View job history and logs",
}

var (
	jobListTask string
	jobListJSON bool
)

var jobListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show job history across all tasks, or one task's runs",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		params := map[string]any{}
		if jobListTask != "" {
			params["task"] = jobListTask
		}
		out, err := dispatcher.CallToolChecked(cmd.Context(), "job_list", params)
		if err != nil {
			return err
		}
		if jobListJSON {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		}
		var jobs []domain.Job
		if err := json.Unmarshal([]byte(out), &jobs); err != nil {
			return fmt.Errorf("parsing job list: %w", err)
		}
		writeJobTable(cmd.OutOrStdout(), jobs)
		return nil
	},
}

var jobLogsCmd = &cobra.Command{
	Use:   "logs <job-id>",
	Short: "Show the captured output of a job run",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := dispatcher.CallToolChecked(cmd.Context(), "job_logs", map[string]any{"id": args[0]})
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), out)
		return nil
	},
}

// writeJobTable prints jobs newest first.
func writeJobTable(w io.Writer, jobs []domain.Job) {
	if len(jobs) == 0 {
		_, _ = fmt.Fprintln(w, "No jobs.")
		return
	}
	sort.SliceStable(jobs, func(i, j int) bool { return jobs[i].CreatedAt.After(jobs[j].CreatedAt) })
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "JOB\tTASK\tSTATUS\tTRIGGER\tCREATED")
	for _, j := range jobs {
		task, trigger := j.TaskID, j.Trigger
		if task == "" {
			task = "-"
		}
		if trigger == "" {
			trigger = "-"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", j.ID, task, j.Status, trigger, j.CreatedAt.Local().Format("2006-01-02 15:04"))
	}
	_ = tw.Flush()
}

func init() {
	jobListCmd.Flags().StringVar(&jobListTask, "task", "", "only show runs of this task (<agent>/<task> or task name)")
	jobListCmd.Flags().BoolVar(&jobListJSON, "json", false, "print the raw job list as JSON")
	jobCmd.AddCommand(jobListCmd, jobLogsCmd)
	rootCmd.AddCommand(jobCmd)
}
