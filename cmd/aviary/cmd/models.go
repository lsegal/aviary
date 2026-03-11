package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/models"
)

var modelsProvider string

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Inspect supported provider/model pairs",
}

var modelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all supported provider/model pairs",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runModelsList(cmd.OutOrStdout(), modelsProvider)
	},
}

func init() {
	modelsListCmd.Flags().StringVar(&modelsProvider, "provider", "", "filter to a provider name")
	modelsCmd.AddCommand(modelsListCmd)
	rootCmd.AddCommand(modelsCmd)
}

func runModelsList(w io.Writer, provider string) error {
	provider = strings.TrimSpace(provider)
	if provider != "" && !models.HasProvider(provider) {
		return fmt.Errorf("unknown provider %q; supported providers: %s", provider, strings.Join(models.Providers(), ", "))
	}
	for _, model := range models.FilterByProvider(provider) {
		if _, err := fmt.Fprintln(w, model); err != nil {
			return err
		}
	}
	return nil
}
