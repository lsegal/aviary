package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/domain"
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Manage scheduled tasks",
}

// taskTriggerFlags lists the trigger flags shared by add, update and preview,
// mapped to their tool argument names.
var taskTriggerFlags = []struct{ flag, arg, usage string }{
	{"schedule", "schedule", "cron expression (5 fields, or 6 with leading seconds)"},
	{"trigger", "trigger", "trigger kind: once, interval, daily, weekly or cron"},
	{"every", "every", "interval period, e.g. 30m, 2h or 1d"},
	{"at", "at", "daily/weekly time of day, HH:MM (comma-separate several)"},
	{"days", "days", "weekly days, e.g. mon,wed,fri"},
	{"timezone", "timezone", "IANA timezone triggers are evaluated in (default: local)"},
	{"start-at", "start_at", "once trigger time, or earliest fire (RFC 3339 or YYYY-MM-DDTHH:MM)"},
	{"overlap", "overlap", "while a run is still active: queue (default) or skip"},
	{"missed", "missed", "fires missed while stopped or asleep: skip (default) or run_once"},
}

func addTaskTriggerFlags(c *cobra.Command) {
	for _, f := range taskTriggerFlags {
		c.Flags().String(f.flag, "", f.usage)
	}
}

// changedTaskFlags copies every flag the user set into params.
func changedTaskFlags(c *cobra.Command, params map[string]any, names map[string]string) {
	for flag, arg := range names {
		if !c.Flags().Changed(flag) {
			continue
		}
		if b, err := c.Flags().GetBool(flag); err == nil {
			params[arg] = b
			continue
		}
		if n, err := c.Flags().GetInt(flag); err == nil {
			params[arg] = n
			continue
		}
		v, _ := c.Flags().GetString(flag)
		params[arg] = v
	}
}

func triggerFlagNames(extra map[string]string) map[string]string {
	names := map[string]string{}
	for _, f := range taskTriggerFlags {
		names[f.flag] = f.arg
	}
	for k, v := range extra {
		names[k] = v
	}
	return names
}

var taskListJSON bool

var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "List scheduled tasks with their trigger, next run and last result",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		out, err := callTool(cmd, dispatcher, "task_list", map[string]any{})
		if err != nil {
			return err
		}
		if taskListJSON {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		}
		var tasks []domain.ScheduledTask
		if err := json.Unmarshal([]byte(out), &tasks); err != nil {
			return fmt.Errorf("parsing task list: %w", err)
		}
		writeTaskTable(cmd.OutOrStdout(), tasks)
		return nil
	},
}

func writeTaskTable(w io.Writer, tasks []domain.ScheduledTask) {
	if len(tasks) == 0 {
		_, _ = fmt.Fprintln(w, "No scheduled tasks.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "TASK\tTRIGGER\tNEXT RUN\tLAST RESULT")
	for _, t := range tasks {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", t.ID, taskTrigger(t), taskNextRun(t), taskLastResult(t))
	}
	_ = tw.Flush()
}

// taskTrigger describes what fires a task, prefixed by its project.
func taskTrigger(t domain.ScheduledTask) string {
	trigger := t.Description
	if trigger == "" && t.Watch != "" {
		trigger = "watch " + t.Watch
	}
	if t.Project != "" {
		trigger = t.Project + ": " + trigger
	}
	return trigger
}

// taskNextRun is a task's next fire in local time, or disabled.
func taskNextRun(t domain.ScheduledTask) string {
	if !t.Enabled {
		return "disabled"
	}
	if t.NextRun == nil {
		return "-"
	}
	return t.NextRun.Local().Format("2006-01-02 15:04")
}

// taskLastResult is a task's latest run status and fire time.
func taskLastResult(t domain.ScheduledTask) string {
	n := len(t.History)
	if n == 0 {
		return "-"
	}
	run := t.History[n-1]
	return fmt.Sprintf("%s %s", run.Status, run.FireTime.Local().Format("2006-01-02 15:04"))
}

var taskAddCmd = &cobra.Command{
	Use:   "add [agent] <name> <prompt>",
	Short: "Add or replace a scheduled task",
	Long: `
Add a scheduled task for an agent, or replace the task with the same name.
The agent may be omitted when only one agent exists.
Choose the trigger with --schedule (cron), or --every, --at, --days and
--start-at (with --trigger to be explicit). For example:

  aviary task add assistant standup "Summarize yesterday" --days mon-fri --at 09:00
  aviary task add assistant digest "Send the digest" --every 2h --overlap skip

--project runs the prompt as a scheduled Code Loop job on that watched
project's board, in an isolated clone, on the project's agent; --model overrides
that agent's model for the task's runs (default: the agent's own model):

  aviary task add assistant triage "Triage new issues" --project lsegal/aviary --at 02:00
`,
	Args: cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		agent, rest, err := splitAgentArg(cmd.Context(), args, 3)
		if err != nil {
			return err
		}
		params := map[string]any{"agent": agent, "name": rest[0], "content": rest[1], "precompile": false}
		changedTaskFlags(cmd, params, triggerFlagNames(map[string]string{"target": "target", "type": "type", "run-once": "run_once", "project": "project", "model": "model"}))
		if _, ok := params["project"]; ok && params["type"] == nil {
			params["type"] = "project"
		}
		if days, ok := params["days"].(string); ok {
			params["days"] = expandDayRange(days)
		}
		for _, f := range []string{"schedule", "trigger", "every", "at", "days", "start_at"} {
			if _, ok := params[f]; ok {
				out, err := callTool(cmd, dispatcher, "task_schedule", params)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), out)
				return nil
			}
		}
		return fmt.Errorf("a trigger is required: pass --schedule, --every, --at, --days or --start-at")
	},
}

var taskUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Change fields of a scheduled task (pass an empty value to clear one)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		params := map[string]any{"name": args[0]}
		changedTaskFlags(cmd, params, triggerFlagNames(map[string]string{
			"prompt": "content", "target": "target", "run-once": "run_once", "enabled": "enabled", "watch": "watch",
			"project": "project", "model": "model",
		}))
		if days, ok := params["days"].(string); ok {
			params["days"] = expandDayRange(days)
		}
		if len(params) == 1 {
			return fmt.Errorf("nothing to update; pass at least one flag")
		}
		out, err := callTool(cmd, dispatcher, "task_update", params)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), out)
		return nil
	},
}

var taskRmCmd = &cobra.Command{
	Use:     "rm <name>",
	Aliases: []string{"remove", "delete"},
	Short:   "Delete a scheduled task; --delete-data also deletes its session and run history",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		deleteData, _ := cmd.Flags().GetBool("delete-data")
		out, err := callTool(cmd, dispatcher, "task_delete", map[string]any{"name": args[0], "delete_data": deleteData})
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), out)
		return nil
	},
}

var taskPreviewCmd = &cobra.Command{
	Use:   "preview [name]",
	Short: "Show the next fires of a saved task, or of the trigger flags given",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		params := map[string]any{}
		if len(args) == 1 {
			params["name"] = args[0]
		}
		changedTaskFlags(cmd, params, triggerFlagNames(map[string]string{"count": "count"}))
		if days, ok := params["days"].(string); ok {
			params["days"] = expandDayRange(days)
		}
		out, err := callTool(cmd, dispatcher, "task_preview", params)
		if err != nil {
			return err
		}
		var res struct {
			Description string   `json:"description"`
			Next        []string `json:"next"`
		}
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		}
		w := cmd.OutOrStdout()
		_, _ = fmt.Fprintln(w, res.Description)
		if len(res.Next) == 0 {
			_, _ = fmt.Fprintln(w, "  no upcoming fires")
		}
		for _, n := range res.Next {
			if ts, err := time.Parse(time.RFC3339, n); err == nil {
				n = ts.Format("Mon 2006-01-02 15:04 MST")
			}
			_, _ = fmt.Fprintln(w, "  "+n)
		}
		return nil
	},
}

var taskRunCmd = &cobra.Command{
	Use:   "run <name>",
	Short: "Run a scheduled task right now",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := callTool(cmd, dispatcher, "task_run", map[string]any{"name": args[0]})
		if err != nil {
			return err
		}
		var job domain.Job
		if json.Unmarshal([]byte(out), &job) == nil && job.ID != "" {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Started task %q (job %s).\n", job.TaskID, job.ID)
			return nil
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), out)
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
		out, err := callTool(cmd, dispatcher, "task_stop", params)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), out)
		return nil
	},
}

// expandDayRange turns "mon-fri" style ranges into comma-separated days.
func expandDayRange(days string) string {
	order := []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}
	index := func(d string) int {
		d = strings.ToLower(strings.TrimSpace(d))
		for i, o := range order {
			if len(d) >= 3 && d[:3] == o {
				return i
			}
		}
		return -1
	}
	var out []string
	for part := range strings.SplitSeq(days, ",") {
		from, to, ok := strings.Cut(part, "-")
		a, b := index(from), index(to)
		if !ok || a < 0 || b < 0 {
			out = append(out, strings.TrimSpace(part))
			continue
		}
		for i := a; ; i = (i + 1) % 7 {
			out = append(out, order[i])
			if i == b {
				break
			}
		}
	}
	return strings.Join(out, ",")
}

func init() {
	taskListCmd.Flags().BoolVar(&taskListJSON, "json", false, "print the raw task list as JSON")

	addTaskTriggerFlags(taskAddCmd)
	taskAddCmd.Flags().String("target", "", "output delivery: silent, session:<name>, or channel:<channel name>")
	taskAddCmd.Flags().String("type", "", "task type: prompt (default), script or project")
	taskAddCmd.Flags().String("project", "", "watched project (owner/repo) to run the prompt on as a board job; implies --type project")
	taskAddCmd.Flags().String("model", "", "model override for a project task: the project's agent runs it with this model, such as claude/opus (default: the agent's model)")
	taskAddCmd.Flags().Bool("run-once", false, "fire only once")

	addTaskTriggerFlags(taskUpdateCmd)
	taskUpdateCmd.Flags().String("prompt", "", "new task prompt")
	taskUpdateCmd.Flags().String("target", "", "output delivery")
	taskUpdateCmd.Flags().String("watch", "", "file glob that triggers the task")
	taskUpdateCmd.Flags().Bool("run-once", false, "fire only once")
	taskUpdateCmd.Flags().Bool("enabled", true, "enable or disable the task")
	taskUpdateCmd.Flags().String("project", "", "watched project of a project task")
	taskUpdateCmd.Flags().String("model", "", "model override for a project task (empty: the project agent's model)")

	taskRmCmd.Flags().Bool("delete-data", false, "also delete the task's session and run history; without it its session becomes a regular chat")

	addTaskTriggerFlags(taskPreviewCmd)
	taskPreviewCmd.Flags().Int("count", 3, "number of fires to show (at most 20)")

	taskCmd.AddCommand(taskListCmd, taskAddCmd, taskUpdateCmd, taskRmCmd, taskPreviewCmd, taskRunCmd, taskStopCmd)
	rootCmd.AddCommand(taskCmd)
}
