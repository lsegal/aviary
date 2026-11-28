// Package cmd implements the aviary CLI subcommands.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

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
		out, err := callTool(cmd, dispatcher, "agent_list", nil)
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var agentRunFile string
var agentRunBare bool
var agentRunHistory bool
var agentRunWorkdir string
var agentRunOutputFormat string

var agentRunCmd = &cobra.Command{
	Use:   "run [name] [message]",
	Short: "Send a message to an agent and stream the response",
	Long: `Send a message to an agent and stream the response. The name may be
omitted when only one agent exists: "aviary agent run hello" sends "hello" to
the sole agent, and with --file every argument is optional.`,
	Args: cobra.RangeArgs(0, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		want := 2
		if agentRunFile != "" {
			want = 1
		}
		name, rest, err := splitAgentArg(cmd.Context(), args, want)
		if err != nil {
			return err
		}
		msg := ""
		if len(rest) > 0 {
			msg = rest[0]
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
		toolArgs := map[string]any{
			"name":    name,
			"message": msg,
			"bare":    agentRunBare,
		}
		if cmd.Flags().Changed("history") {
			toolArgs["history"] = agentRunHistory
		}
		if agentRunWorkdir != "" {
			workdir, err := filepath.Abs(agentRunWorkdir)
			if err != nil {
				return fmt.Errorf("resolving workdir: %w", err)
			}
			toolArgs["workdir"] = workdir
		}
		switch agentRunOutputFormat {
		case "text":
			out, err := callTool(cmd, dispatcher, "agent_run", toolArgs)
			if err != nil {
				return err
			}
			fmt.Print(out)
			return nil
		case "json":
			toolArgs["stream_events"] = true
			w := &agentEventWriter{out: cmd.OutOrStdout()}
			out, isError, err := dispatcher.CallToolWithProgress(cmd.Context(), "agent_run", toolArgs, w.progress)
			if err != nil {
				return err
			}
			w.finish(out, isError)
			return nil
		}
		return fmt.Errorf("--output-format must be text or json, not %q", agentRunOutputFormat)
	},
}

var agentStopCmd = newAgentToolCmd(agentToolSpec{
	use:      "stop [name]",
	short:    "Immediately stop all work in progress for an agent",
	args:     cobra.MaximumNArgs(1),
	tool:     "agent_stop",
	agentKey: "name",
})

var agentTemplateSyncCmd = newAgentToolCmd(agentToolSpec{
	use:      "template-sync [name]",
	short:    "Sync embedded template files into an agent directory",
	args:     cobra.MaximumNArgs(1),
	tool:     "agent_template_sync",
	agentKey: "agent",
})

func init() {
	agentRunCmd.Flags().StringVar(&agentRunFile, "file", "", "read prompt from file")
	agentRunCmd.Flags().BoolVar(&agentRunBare, "bare", false, "run without any system prompt, rules, memory, or tool preamble")
	agentRunCmd.Flags().BoolVar(&agentRunHistory, "history", true, "include prior session history in the prompt")
	agentRunCmd.Flags().StringVar(&agentRunWorkdir, "workdir", "", "grant the run's file tools and exec this directory for its duration, e.g. a job's clone")
	agentRunCmd.Flags().StringVar(&agentRunOutputFormat, "output-format", "text", "text, or json for one JSON event per line: messages, tool calls and results, file changes")
	agentCmd.AddCommand(agentListCmd, agentRunCmd, agentStopCmd, agentTemplateSyncCmd)
	rootCmd.AddCommand(agentCmd)
}
