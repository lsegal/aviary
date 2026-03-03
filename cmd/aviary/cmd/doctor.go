package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Validate configuration and credentials",
	Long: `Check aviary.yaml for configuration errors and verify that required
credentials exist in the auth store. Lists all issues with full context.

Exits with status 1 if any errors are found.`,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(_ *cobra.Command, _ []string) error {
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}
	fmt.Fprintf(os.Stdout, "Config file: %s\n\n", cfgPath)

	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		fmt.Fprintln(os.Stdout, "[WARN] config: file not found; using built-in defaults")
		fmt.Fprintln(os.Stdout, "       Run 'aviary configure' to create one.")
		fmt.Fprintln(os.Stdout)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	st := authStore()
	issues := config.Validate(cfg, func(key string) (string, error) {
		return st.Get(key)
	})

	nerrs, nwarns := 0, 0
	for _, issue := range issues {
		fmt.Fprintf(os.Stdout, "  [%s] %s: %s\n", issue.Level, issue.Field, issue.Message)
		if issue.Level == config.LevelError {
			nerrs++
		} else {
			nwarns++
		}
	}

	// Ping each unique provider using the first model we find for it.
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Checking model credentials...")
	factory := llm.NewFactory(func(ref string) (string, error) {
		key := strings.TrimPrefix(ref, "auth:")
		return st.Get(key)
	})
	providerModels := config.UniqueProviderModels(cfg)
	if len(providerModels) == 0 {
		fmt.Fprintln(os.Stdout, "  (no models configured)")
	}
	for provider, model := range providerModels {
		fmt.Fprintf(os.Stdout, "  %-12s ", provider)
		if err := factory.PingModel(model); err != nil {
			fmt.Fprintf(os.Stdout, "[ERROR] %v\n", err)
			nerrs++
		} else {
			fmt.Fprintln(os.Stdout, "[OK]")
		}
	}

	fmt.Fprintf(os.Stdout, "\n%d error(s), %d warning(s)\n", nerrs, nwarns)
	if nerrs > 0 {
		os.Exit(1)
	}
	return nil
}
