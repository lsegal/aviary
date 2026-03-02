package cmd

import (
	"fmt"
	"os"

	"github.com/lsegal/aviary/internal/logging"
	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/mcp"
	"github.com/lsegal/aviary/internal/server"
)

var (
	cfgFile   string
	serverURL string
	token     string
)

var rootCmd = &cobra.Command{
	Use:   "aviary",
	Short: "Aviary — the AI agent orchestrator",
	Long: `Aviary is an autonomous AI agent orchestrator. Connect your AI models
to messaging channels, set up scheduled tasks, and let your agents work for you.`,
}

// dispatcher is the global MCP dispatcher used by all subcommands.
var dispatcher *mcp.Dispatcher

// Execute runs the root command.
func Execute() {
	if err := logging.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to initialize logging: %v\n", err)
	}

	// Wire server package into MCP dispatcher.
	mcp.SetServerChecker(func() bool {
		running, _, _ := server.IsRunning()
		return running
	})
	mcp.SetTokenLoader(server.LoadToken)
	agent.SetToolClientFactory(mcp.NewAgentToolClient)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/aviary/aviary.yaml)")
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", "https://localhost:16677", "Aviary server URL")
	rootCmd.PersistentFlags().StringVar(&token, "token", "", "authentication token (overrides stored token)")

	// Initialize the dispatcher after flags are parsed.
	cobra.OnInitialize(func() {
		dispatcher = mcp.NewDispatcher(serverURL, token)
	})
}
