package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/buildinfo"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/logging"
	"github.com/lsegal/aviary/internal/mcp"
	"github.com/lsegal/aviary/internal/server"
	"github.com/lsegal/aviary/internal/store"
	"github.com/lsegal/aviary/internal/update"
)

var (
	cfgFile   string
	dataDir   string
	serverURL string
	token     string
)

var rootCmd = &cobra.Command{
	Use:   "aviary",
	Short: "Aviary — the AI agent orchestrator",
	Long: `Aviary is an autonomous AI agent orchestrator. Connect your AI models
to messaging channels, set up scheduled tasks, and let your agents work for you.`,
	Version: buildinfo.Version,
}

// dispatcher is the global MCP dispatcher used by all subcommands.
var dispatcher *mcp.Dispatcher

// Execute runs the root command.
func Execute() {
	// A watch session logs into its own temporary directory once it has
	// one, never into the user's.
	if cmd, _, err := rootCmd.Find(os.Args[1:]); err != nil || cmd != watchCmd {
		if err := logging.Init(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to initialize logging: %v\n", err)
		}
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
	rootCmd.SetHelpTemplate(`{{with (or .Long .Short)}}{{.}}{{end}}

Version: {{.Version}}

{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/aviary/aviary.yaml)")
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", "", "data directory (default: ~/.config/aviary)")
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", "", "Aviary server URL (default: the local server's URL from config, https://localhost:16677)")
	rootCmd.PersistentFlags().StringVar(&token, "token", "", "authentication token (overrides stored token)")
	rootCmd.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		return update.ConfigureEmulation(os.Getenv("AVIARY_EMULATE_VERSIONS"))
	}

	// Initialize the dispatcher after flags are parsed.
	cobra.OnInitialize(func() {
		if dataDir != "" {
			store.SetDataDir(dataDir)
		}
		dispatcher = mcp.NewDispatcher(resolvedServerURL(), token)
	})
}

// resolvedServerURL returns the --server URL, or else the local server's URL
// from config, so the CLI uses the scheme and port the server serves.
func resolvedServerURL() string {
	if serverURL != "" {
		return serverURL
	}
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return config.ServerURL(config.ServerConfig{})
	}
	return config.ServerURL(cfg.Server)
}
