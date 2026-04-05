package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
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
	if provider != "" && provider != "vllm" && !models.HasProvider(provider) {
		supported := append(models.Providers(), "vllm")
		return fmt.Errorf("unknown provider %q; supported providers: %s", provider, strings.Join(supported, ", "))
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "MODEL\tINPUT\tOUTPUT\tTYPE"); err != nil {
		return err
	}
	if provider != "vllm" {
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
	}
	vllmModels, err := listDynamicVLLMModels(provider)
	if err != nil {
		return err
	}
	for _, modelID := range vllmModels {
		if _, err := fmt.Fprintf(tw, "%s\t-\t-\t\n", modelID); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func listDynamicVLLMModels(provider string) ([]string, error) {
	if provider != "" && provider != "vllm" {
		return nil, nil
	}
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, err
	}
	pc, ok := cfg.Models.Providers["vllm"]
	if !ok || strings.TrimSpace(pc.BaseURI) == "" {
		if provider == "vllm" {
			return nil, fmt.Errorf("vllm provider requires models.providers.vllm.base_uri")
		}
		return nil, nil
	}
	st := authStore()
	resolveAuth := func(ref string) (string, error) {
		return auth.Resolve(st, ref)
	}
	factory := llm.NewFactory(resolveAuth).WithProviderOptionsResolver(func(name string) (llm.ProviderOptions, bool) {
		if strings.TrimSpace(name) != "vllm" {
			return llm.ProviderOptions{}, false
		}
		return llm.ProviderOptions{Auth: pc.Auth, BaseURI: pc.BaseURI}, true
	})
	providerInst, err := factory.ForModel("vllm/_")
	if err != nil {
		return nil, err
	}
	vllmProvider, ok := providerInst.(*llm.VLLMProvider)
	if !ok {
		return nil, fmt.Errorf("unexpected provider type for vllm")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	names, err := vllmProvider.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, "vllm/"+name)
	}
	return out, nil
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
