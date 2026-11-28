package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// printToolResult calls an MCP tool through the dispatcher and prints its
// output followed by a newline.
func printToolResult(cmd *cobra.Command, tool string, args any) error {
	out, err := dispatcher.CallTool(cmd.Context(), tool, args)
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
