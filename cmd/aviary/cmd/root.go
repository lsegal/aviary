package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgFile string
	server  string
	token   string
)

var rootCmd = &cobra.Command{
	Use:   "aviary",
	Short: "Aviary — the AI agent orchestrator",
	Long: `Aviary is an autonomous AI agent orchestrator. Connect your AI models
to messaging channels, set up scheduled tasks, and let your agents work for you.`,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/aviary/aviary.yaml)")
	rootCmd.PersistentFlags().StringVar(&server, "server", "https://localhost:16677", "Aviary server URL")
	rootCmd.PersistentFlags().StringVar(&token, "token", "", "authentication token (overrides stored token)")
}
