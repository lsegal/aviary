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
	Short: "Manage Projects: watched GitHub issues fixed by coding CLIs",
	Long: `Manage Projects. Every subcommand calls the project_* MCP tool of the same
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
	{[]string{"add"}, "project_add", "target", "Watch a project, saving it to aviary.yaml unless --runtime is set"},
	{[]string{"remove"}, "project_remove", "target", "Stop watching a project"},
	{[]string{"refresh"}, "project_refresh", "target", "Check GitHub for work now"},
	{[]string{"job", "list"}, "project_job_list", "", "List jobs, running and errored first"},
	{[]string{"job", "get"}, "project_job_get", "number", "Show one job's state, transcript and scrollback"},
	{[]string{"job", "explain"}, "project_job_explain", "number", "Explain why a job is where it is and what moves it on"},
	{[]string{"job", "retry"}, "project_job_retry", "number", "Requeue a job"},
	{[]string{"job", "pause"}, "project_job_pause", "number", "Pause a job, pushing a running agent's work"},
	{[]string{"job", "resume"}, "project_job_resume", "number", "Resume a paused job"},
	{[]string{"job", "stop"}, "project_job_stop", "number", "Stop a job and wait for its agent to exit"},
	{[]string{"cli", "list"}, "project_cli_list", "", "Check every coding CLI: installed, version, sign-in, quota and models"},
	{[]string{"clone", "list"}, "project_clone_list", "", "List job clones and whether they are in use"},
	{[]string{"clone", "clear"}, "project_clone_clear", "", "Remove unused job clones"},
	{[]string{"unblock"}, "project_unblock_run", "number", "Run a stuck-job watchdog round now"},
	{[]string{"issue", "comment"}, "project_issue_comment", "number", "Post an update comment the job working the issue hears"},
	{[]string{"issue", "create"}, "project_issue_create", "", "File an issue through gh-create"},
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
		dispatcher = internalmcp.NewDispatcher(serverURL, token)
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
				group = &cobra.Command{Use: spec.path[0], Short: "Projects " + spec.path[0] + " commands"}
				groups[spec.path[0]] = group
				projectCmd.AddCommand(group)
			}
			parent = group
		}
		parent.AddCommand(newProjectToolCommand(spec))
	}
	rootCmd.AddCommand(projectCmd)
}
