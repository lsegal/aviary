package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Walk through full initial setup",
	Long:  `Interactive wizard for full Aviary configuration. Writes results to aviary.yaml.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Configure wizard (not yet implemented)")
		return nil
	},
}

var configureAgentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Add or edit agents interactively",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Agent configuration wizard (not yet implemented)")
		return nil
	},
}

var configureChannelsCmd = &cobra.Command{
	Use:   "channels",
	Short: "Configure channels for an agent",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Channel configuration wizard (not yet implemented)")
		return nil
	},
}

var configureModelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Set up model providers and defaults",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Model configuration wizard (not yet implemented)")
		return nil
	},
}

var configureSchedulerCmd = &cobra.Command{
	Use:   "scheduler",
	Short: "Configure concurrency and task defaults",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Scheduler configuration wizard (not yet implemented)")
		return nil
	},
}

var configureAuthCmd = &cobra.Command{
	Use:   "auth",
	Short: "Add or update credentials (API keys, OAuth)",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Auth configuration wizard (not yet implemented)")
		return nil
	},
}

func init() {
	configureCmd.AddCommand(
		configureAgentsCmd,
		configureChannelsCmd,
		configureModelsCmd,
		configureSchedulerCmd,
		configureAuthCmd,
	)
	rootCmd.AddCommand(configureCmd)
}
