package cmd

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/lsegal/aviary/internal/config"
)

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Walk through full initial setup",
	Long:  `Interactive wizard for full Aviary configuration. Writes results to aviary.yaml.`,
	RunE:  runConfigure,
}

var configureAgentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Add or edit agents interactively",
	RunE:  runConfigureAgents,
}

var configureChannelsCmd = &cobra.Command{
	Use:   "channels",
	Short: "Configure channels for an agent",
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Println("Channel configuration: edit aviary.yaml directly (wizard coming in a later release).")
		return nil
	},
}

var configureModelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Set up model providers and defaults",
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Println("Model configuration: edit aviary.yaml directly (wizard coming in a later release).")
		return nil
	},
}

var configureSchedulerCmd = &cobra.Command{
	Use:   "scheduler",
	Short: "Configure concurrency and task defaults",
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Println("Scheduler configuration: edit aviary.yaml directly (wizard coming in a later release).")
		return nil
	},
}

var configureAuthCmd = &cobra.Command{
	Use:   "auth",
	Short: "Add or update credentials (API keys, OAuth)",
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Println("Use 'aviary auth set <name> <value>' to store credentials.")
		return nil
	},
}

var configureBrowserCmd = &cobra.Command{
	Use:   "browser",
	Short: "Configure browser automation settings",
	RunE:  runConfigureBrowser,
}

func init() {
	configureCmd.AddCommand(
		configureAgentsCmd,
		configureChannelsCmd,
		configureModelsCmd,
		configureSchedulerCmd,
		configureAuthCmd,
		configureBrowserCmd,
	)
	rootCmd.AddCommand(configureCmd)
}

// runConfigure runs the full initial setup wizard.
func runConfigure(_ *cobra.Command, _ []string) error {
	cfg := config.Default()

	var agentName, model, port string
	port = "16677"

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("Aviary Setup Wizard").Description("Let's configure your Aviary instance."),
			huh.NewInput().Title("Server port").Value(&port).Placeholder("16677"),
		),
		huh.NewGroup(
			huh.NewNote().Title("First Agent").Description("Configure your first AI agent (you can add more later)."),
			huh.NewInput().Title("Agent name").Value(&agentName).Placeholder("assistant"),
			huh.NewSelect[string]().
				Title("Model").
				Value(&model).
				Options(
					huh.NewOption("Anthropic Claude Sonnet 4.5", "anthropic/claude-sonnet-4-5"),
					huh.NewOption("Anthropic Claude Opus 4.5", "anthropic/claude-opus-4-5"),
					huh.NewOption("OpenAI GPT-4o", "openai/gpt-4o"),
					huh.NewOption("Google Gemini Pro", "gemini/gemini-pro"),
				),
		),
	)

	if err := form.Run(); err != nil {
		return fmt.Errorf("wizard cancelled: %w", err)
	}

	// Apply wizard values.
	if port != "" && port != "16677" {
		var p int
		fmt.Sscanf(port, "%d", &p)
		if p > 0 {
			cfg.Server.Port = p
		}
	}
	if agentName != "" {
		cfg.Agents = []config.AgentConfig{{Name: agentName, Model: model}}
	}

	return writeConfig(&cfg)
}

// runConfigureAgents adds a new agent via wizard.
func runConfigureAgents(_ *cobra.Command, _ []string) error {
	path := config.DefaultPath()
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	var name, model string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Agent name").Value(&name).Placeholder("assistant"),
			huh.NewSelect[string]().
				Title("Model").
				Value(&model).
				Options(
					huh.NewOption("Anthropic Claude Sonnet 4.5", "anthropic/claude-sonnet-4-5"),
					huh.NewOption("Anthropic Claude Opus 4.5", "anthropic/claude-opus-4-5"),
					huh.NewOption("OpenAI GPT-4o", "openai/gpt-4o"),
					huh.NewOption("Google Gemini Pro", "gemini/gemini-pro"),
				),
		),
	)
	if err := form.Run(); err != nil {
		return fmt.Errorf("wizard cancelled: %w", err)
	}
	if name == "" {
		return fmt.Errorf("agent name is required")
	}

	cfg.Agents = append(cfg.Agents, config.AgentConfig{Name: name, Model: model})
	return writeConfig(cfg)
}

// runConfigureBrowser walks through browser-specific settings interactively.
func runConfigureBrowser(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	profileDir := cfg.Browser.ProfileDir
	binary := cfg.Browser.Binary
	port := cfg.Browser.CDPPort
	if port == 0 {
		port = 9222
	}
	portStr := fmt.Sprintf("%d", port)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Browser Configuration").
				Description("Configure the Chromium browser used for automation.\nAviary uses your browser's default user data directory and selects a profile by name."),
			huh.NewInput().
				Title("Profile directory name").
				Value(&profileDir).
				Placeholder("Aviary").
				Description("Chrome --profile-directory value (e.g. Default, Profile 1, Work)"),
			huh.NewInput().
				Title("Browser binary").
				Value(&binary).
				Placeholder("auto-detected").
				Description("Path to Chrome or Chromium executable (leave empty to auto-detect)"),
			huh.NewInput().
				Title("CDP port").
				Value(&portStr).
				Placeholder("9222").
				Description("Chrome DevTools Protocol debugging port"),
		),
	)

	if err := form.Run(); err != nil {
		return fmt.Errorf("wizard cancelled: %w", err)
	}

	cfg.Browser.ProfileDir = profileDir
	cfg.Browser.Binary = binary
	if portStr != "" {
		var p int
		fmt.Sscanf(portStr, "%d", &p)
		if p > 0 {
			cfg.Browser.CDPPort = p
		}
	}

	return writeConfig(cfg)
}

func writeConfig(cfg *config.Config) error {
	path := config.DefaultPath()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := os.MkdirAll(fmt.Sprintf("%s/..", path), 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	fmt.Printf("Configuration written to %s\n", path)
	return nil
}
