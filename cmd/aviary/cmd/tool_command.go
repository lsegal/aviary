package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/mcp"
)

// cliToolHints maps the MCP tool names that tool error messages point to onto
// the CLI commands that do the same thing.
var cliToolHints = strings.NewReplacer(
	"use agent_list", "run `aviary agent list`",
	"use task_list", "run `aviary task list`",
)

// callTool calls an MCP tool through d for cmd. A result the tool marked as an
// error comes back as an error, so the command exits non-zero and Execute
// prints the message to stderr. The error is a runtime failure, not misuse,
// so cobra prints neither its own copy of it nor the usage block.
func callTool(cmd *cobra.Command, d *mcp.Dispatcher, tool string, args any) (string, error) {
	out, err := d.CallTool(cmd.Context(), tool, args)
	var toolErr *mcp.ToolError
	if errors.As(err, &toolErr) {
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		return "", &mcp.ToolError{Tool: toolErr.Tool, Text: cliToolHints.Replace(toolErr.Text)}
	}
	return out, err
}

// printToolResult calls an MCP tool through the dispatcher and prints its
// output followed by a newline.
func printToolResult(cmd *cobra.Command, tool string, args any) error {
	out, err := callTool(cmd, dispatcher, tool, args)
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

// agentToolSpec describes a subcommand whose leading agent argument is
// optional and whose remaining positional arguments map onto tool parameters.
type agentToolSpec struct {
	use   string
	short string
	long  string
	args  cobra.PositionalArgs
	tool  string
	// agentKey is the tool parameter that receives the resolved agent name.
	agentKey string
	// restKeys name the tool parameters for the positional arguments that
	// follow the agent, in order.
	restKeys []string
}

// newAgentToolCmd builds a cobra command from an agentToolSpec. The agent
// argument may be omitted when only one agent exists.
func newAgentToolCmd(spec agentToolSpec) *cobra.Command {
	return &cobra.Command{
		Use:   spec.use,
		Short: spec.short,
		Long:  spec.long,
		Args:  spec.args,
		RunE: func(cmd *cobra.Command, args []string) error {
			agent, rest, err := splitAgentArg(cmd.Context(), args, len(spec.restKeys)+1)
			if err != nil {
				return err
			}
			params := map[string]any{spec.agentKey: agent}
			for i, key := range spec.restKeys {
				params[key] = rest[i]
			}
			return printToolResult(cmd, spec.tool, params)
		},
	}
}
