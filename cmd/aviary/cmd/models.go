package cmd

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/models"
)

var modelsProvider string

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Inspect supported provider/model pairs",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runModelsList(cmd.OutOrStdout(), modelsProvider)
	},
}

var modelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all supported provider/model pairs",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runModelsList(cmd.OutOrStdout(), modelsProvider)
	},
}

func init() {
	modelsCmd.Flags().StringVar(&modelsProvider, "provider", "", "filter to a provider name")
	modelsListCmd.Flags().StringVar(&modelsProvider, "provider", "", "filter to a provider name")
	modelsCmd.AddCommand(modelsListCmd)
	rootCmd.AddCommand(modelsCmd)
}

func runModelsList(w io.Writer, provider string) error {
	provider = strings.TrimSpace(provider)
	if provider != "" && !models.HasProvider(provider) {
		return fmt.Errorf("unknown provider %q; supported providers: %s", provider, strings.Join(models.Providers(), ", "))
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "MODEL\tINPUT\tOUTPUT\tTYPE"); err != nil {
		return err
	}
	for _, model := range models.EntriesByProvider(provider) {
		modelType := "text only"
		if model.SupportsImageInput {
			modelType = ""
		}
		if _, err := fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\n",
			model.ID,
			formatTokenCount(model.InputTokens),
			formatTokenCount(model.OutputTokens),
			modelType,
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func formatTokenCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
	case n >= 1_000:
		if n%1_000 == 0 {
			return fmt.Sprintf("%dk", n/1_000)
		}
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
