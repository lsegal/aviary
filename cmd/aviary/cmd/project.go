package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	internalmcp "github.com/lsegal/aviary/internal/mcp"
	"github.com/lsegal/aviary/internal/store"
)

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage Code Loop: watched GitHub issues fixed by CLI providers",
	Long: `Manage Code Loop. Every subcommand calls the project_* MCP tool of the same
name with the same arguments, given as --<field> <value> flags. A leading
positional argument fills the subcommand's main field, such as the job number
or the project target.`,
}

// projectToolCommand describes one `aviary project` subcommand.
type projectToolCommand struct {
	path       []string // subcommand words, such as {"job", "get"}
	tool       string   // the MCP tool it calls
	positional string   // the field a leading positional argument fills
	short      string
}

var projectToolCommands = []projectToolCommand{
	{[]string{"status"}, "project_status", "", "Show identity, projects, job counts, delivery, allowed commenters, quotas and recent log lines"},
	{[]string{"add"}, "project_add", "target", "Watch a project, saving it to the --agent's projects in aviary.yaml unless --runtime is set"},
	{[]string{"remove"}, "project_remove", "target", "Stop watching a project"},
	{[]string{"update"}, "project_update", "target", "Change a project: set its own issue --filter, or --reset_filter to use the shared one, or set its own --mode (watch or manual) or --finished_ttl, or inherit to follow the shared value"},
	{[]string{"filter", "check"}, "project_filter_check", "target", "Check an issue --filter for a project and count the open issues it matches"},
	{[]string{"refresh"}, "project_refresh", "target", "Check GitHub for work now"},
	{[]string{"check"}, "project_check", "target", "Check what watching a project needs: GitHub access, CLI providers, skills and fork"},
	{[]string{"job", "list"}, "project_job_list", "", "List jobs, running and errored first"},
	{[]string{"job", "get"}, "project_job_get", "number", "Show one job's state, transcript and scrollback"},
	{[]string{"job", "explain"}, "project_job_explain", "number", "Explain why a job is where it is and what moves it on"},
	{[]string{"job", "retry"}, "project_job_retry", "number", "Requeue a job"},
	{[]string{"job", "pause"}, "project_job_pause", "number", "Pause a job, pushing a running agent's work"},
	{[]string{"job", "resume"}, "project_job_resume", "number", "Resume a paused job"},
	{[]string{"job", "stop"}, "project_job_stop", "number", "Stop a job and wait for its agent to exit"},
	{[]string{"job", "dismiss"}, "project_job_dismiss", "number", "Dismiss a failed scheduled run from the Failed state"},
	{[]string{"job", "clear"}, "project_job_clear", "", "Clear done and failed jobs from the board and job store: one --number, or every --which (done, failed or finished) job of --target or all projects"},
	{[]string{"job", "queue"}, "project_job_queue", "skill", "Queue pull requests for gh-safe-merge or Discussion threads for gh-discuss with --numbers"},
	{[]string{"queue", "candidates"}, "project_queue_candidates", "skill", "List the open pull requests or Discussion threads a project can queue"},
	{[]string{"clone", "list"}, "project_clone_list", "", "List job clones and whether they are in use"},
	{[]string{"clone", "clear"}, "project_clone_clear", "", "Remove unused job clones"},
	{[]string{"unblock"}, "project_unblock_run", "number", "Run a stuck-job watchdog round now"},
	{[]string{"issue", "comment"}, "project_issue_comment", "number", "Post an update comment the job working the issue hears"},
	{[]string{"issue", "create"}, "project_issue_create", "", "File an issue through gh-create"},
	{[]string{"issue", "backfill-media"}, "project_issue_backfill_media", "", "Post the screenshots sent with the requests that filed issues before attachments were supported"},
	{[]string{"task", "list"}, "project_task_list", "project", "List the watched projects' scheduled tasks"},
	{[]string{"task", "get"}, "project_task_get", "name", "Show one project scheduled task's definition and run history"},
	{[]string{"task", "create"}, "project_task_create", "project", "Create a scheduled task on a watched project"},
	{[]string{"task", "update"}, "project_task_update", "name", "Change a project scheduled task"},
	{[]string{"task", "duplicate"}, "project_task_duplicate", "name", "Copy a project scheduled task, to the same or another watched project"},
	{[]string{"task", "pause"}, "project_task_pause", "name", "Pause a project scheduled task"},
	{[]string{"task", "resume"}, "project_task_resume", "name", "Resume a paused project scheduled task"},
	{[]string{"task", "run"}, "project_task_run", "name", "Run a project scheduled task now"},
	{[]string{"task", "delete"}, "project_task_delete", "name", "Delete a project scheduled task and its run history (needs --confirm)"},
	{[]string{"github", "read"}, "project_github_read", "url", "Read a watched repository's commit, file, pull request, comment or Actions run from GitHub"},
	{[]string{"site", "fetch"}, "project_site_fetch", "url", "Fetch a page of a watched project's own site to check a deploy"},
	{[]string{"github", "signin"}, "project_github_signin", "", "Open GitHub's sign-in page in Aviary's browser for browser delivery"},
	{[]string{"github", "use-polling"}, "project_github_use_polling", "", "Read GitHub through gh polling instead of Aviary's browser"},
	{[]string{"github", "import"}, "project_github_import", "profile", "Import the GitHub sign-in from an Aviary browser profile signed in elsewhere"},
}

// projectToolArgs turns a leading positional argument into the --<field>
// flag the tool takes.
func projectToolArgs(positional string, args []string) []string {
	if positional == "" || len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return args
	}
	return append([]string{"--" + positional, args[0]}, args[1:]...)
}

// globalFlagTargets are the root flags a command that disables flag parsing
// still has to honour.
var globalFlagTargets = map[string]*string{
	"config":   &cfgFile,
	"data-dir": &dataDir,
	"server":   &serverURL,
	"token":    &token,
}

// applyGlobalFlags takes the root flags out of args, as a command with
// DisableFlagParsing receives them, applies them, and returns the rest.
func applyGlobalFlags(args []string) []string {
	var rest []string
	changed := false
	for i := 0; i < len(args); i++ {
		name, inline := splitFlagToken(args[i])
		target, ok := globalFlagTargets[name]
		if !strings.HasPrefix(args[i], "--") || !ok {
			rest = append(rest, args[i])
			continue
		}
		switch {
		case inline != nil:
			*target = *inline
		case i+1 < len(args):
			i++
			*target = args[i]
		default:
			rest = append(rest, args[i])
			continue
		}
		changed = true
	}
	if changed {
		if dataDir != "" {
			store.SetDataDir(dataDir)
		}
		dispatcher = internalmcp.NewDispatcher(resolvedServerURL(), token)
	}
	return rest
}

func newProjectToolCommand(spec projectToolCommand) *cobra.Command {
	use := spec.path[len(spec.path)-1]
	if spec.positional != "" {
		use += " [" + spec.positional + "]"
	}
	use += " [--field value ...]"
	return &cobra.Command{
		Use:                use,
		Short:              spec.short,
		Long:               fmt.Sprintf("%s.\n\nCalls the %s tool; run 'aviary tool %s --help' for its fields.", spec.short, spec.tool, spec.tool),
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			args = applyGlobalFlags(args)
			tool, err := lookupTool(cmd.Context(), spec.tool)
			if err != nil {
				return err
			}
			if containsHelpArg(args) {
				return writeSingleToolHelp(cmd.OutOrStdout(), tool)
			}
			payload, err := parseToolInvocationArgs(tool, projectToolArgs(spec.positional, args))
			if err != nil {
				return err
			}
			out, err := activeDispatcher().CallTool(cmd.Context(), spec.tool, payload)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), out)
			return err
		},
	}
}

func init() {
	groups := map[string]*cobra.Command{}
	for _, spec := range projectToolCommands {
		parent := projectCmd
		if len(spec.path) > 1 {
			group, ok := groups[spec.path[0]]
			if !ok {
				group = &cobra.Command{Use: spec.path[0], Short: "Code Loop " + spec.path[0] + " commands"}
				groups[spec.path[0]] = group
				projectCmd.AddCommand(group)
			}
			parent = group
		}
		parent.AddCommand(newProjectToolCommand(spec))
	}
	rootCmd.AddCommand(projectCmd)
}
