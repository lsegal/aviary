package cmd

import (
	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/config"
)

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Interactive configuration wizard",
	Long:  `Full Aviary onboarding wizard: set up providers, create agents, and configure server settings.`,
	RunE:  runConfigure,
}

var configureProvidersCmd = &cobra.Command{
	Use:     "providers",
	Aliases: []string{"auth"},
	Short:   "Authenticate with AI providers (OAuth or API key)",
	RunE:    runConfigureProviders,
}

var configureGeneralCmd = &cobra.Command{
	Use:   "general",
	Short: "Configure shared runtime settings",
	RunE:  runConfigureGeneral,
}

var configureAgentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Add, view, or remove agents",
	RunE:  runConfigureAgents,
}

var configureSkillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Enable and configure installed skills",
	RunE:  runConfigureSkills,
}

var configureServerCmd = &cobra.Command{
	Use:   "server",
	Short: "Configure server port and TLS options",
	RunE:  runConfigureServer,
}

var configureBrowserCmd = &cobra.Command{
	Use:   "browser",
	Short: "Configure browser automation settings",
	RunE:  runConfigureBrowser,
}

var configureSchedulerCmd = &cobra.Command{
	Use:   "scheduler",
	Short: "Configure task concurrency",
	RunE:  runConfigureScheduler,
}

// configureWizard runs the full configure wizard. Tests replace it to avoid
// launching interactive programs.
var configureWizard = runWizard

func init() {
	configureCmd.AddCommand(
		configureGeneralCmd,
		configureProvidersCmd,
		configureAgentsCmd,
		configureSkillsCmd,
		configureServerCmd,
		configureBrowserCmd,
		configureSchedulerCmd,
	)
	rootCmd.AddCommand(configureCmd)
}

func runConfigure(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		d := config.Default()
		cfg = &d
	}
	st := authStore()
	return configureWizard(cfg, cfgFile, st)
}

func runConfigureProviders(_ *cobra.Command, _ []string) error {
	return configureProviderMgr(authStore())
}

func runConfigureGeneral(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}
	return configureGeneralForm(cfg, cfgFile)
}

func runConfigureAgents(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}
	return configureAgentMgr(cfg, cfgFile)
}

func runConfigureSkills(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}
	return configureSkillMgr(cfg, cfgFile)
}

func runConfigureServer(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}
	return configureServerForm(cfg, cfgFile)
}

func runConfigureBrowser(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}
	return configureBrowserForm(cfg, cfgFile)
}

func runConfigureScheduler(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}
	return configureSchedulerForm(cfg, cfgFile)
}
