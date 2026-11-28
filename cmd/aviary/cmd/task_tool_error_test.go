package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalmcp "github.com/lsegal/aviary/internal/mcp"
)

// A tool that reports an error fails the command instead of printing the
// message as a normal result (#1663).
func TestTaskCLI_ToolErrorsFailTheCommand(t *testing.T) {
	setupTaskCLI(t)

	cases := []struct {
		name string
		run  func() (string, error)
		want string
	}{
		{"rm unknown task", func() (string, error) { return runTaskCmd(t, taskRmCmd, "nosuch") },
			"task \"nosuch\" not found; run `aviary task list` to see tasks"},
		{"add invalid every", func() (string, error) { return runTaskCmd(t, taskAddCmd, "bot", "weird", "x", "--every", "0m") },
			`invalid every "0m"`},
		{"add unknown agent", func() (string, error) { return runTaskCmd(t, taskAddCmd, "nobody", "t", "x", "--every", "1h") },
			"agent \"nobody\" not found; run `aviary agent list` to see available agents"},
		{"update unknown task", func() (string, error) { return runTaskCmd(t, taskUpdateCmd, "nosuch", "--every", "1h") },
			`task "nosuch" not found`},
		{"run unknown task", func() (string, error) { return runTaskCmd(t, taskRunCmd, "nosuch") },
			`nosuch`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := c.run()
			var toolErr *internalmcp.ToolError
			require.ErrorAs(t, err, &toolErr)
			assert.Contains(t, err.Error(), c.want)
			assert.Empty(t, out, "the error must not be printed as a result")
		})
	}
}

// Through the root command, a tool error prints neither cobra's own copy of
// the message nor the usage block; Execute prints the message to stderr.
func TestRootCommand_ToolErrorSkipsUsage(t *testing.T) {
	setupTaskCLI(t)
	t.Cleanup(func() {
		taskRmCmd.SilenceErrors, taskRmCmd.SilenceUsage = false, false
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})

	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"task", "rm", "nosuch"})
	// cobra keeps a subcommand's context from an earlier run in this binary.
	taskRmCmd.SetContext(t.Context())
	err := rootCmd.ExecuteContext(t.Context())

	require.ErrorContains(t, err, `task "nosuch" not found`)
	assert.Empty(t, stdout.String())
	assert.Empty(t, stderr.String())
}
