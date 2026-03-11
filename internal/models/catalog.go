package models

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

//go:embed catalog.json
var catalogJSON []byte

var supportedModels []string

func init() {
	if err := json.Unmarshal(catalogJSON, &supportedModels); err != nil {
		panic(fmt.Errorf("loading model catalog: %w", err))
	}
}

// List returns the supported provider/model pairs in catalog order.
func List() []string {
	out := make([]string, len(supportedModels))
	copy(out, supportedModels)
	return out
}

// Providers returns the unique provider names in first-seen order.
func Providers() []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(supportedModels))
	for _, model := range supportedModels {
		provider := ProviderOf(model)
		if provider == "" {
			continue
		}
		if _, ok := seen[provider]; ok {
			continue
		}
		seen[provider] = struct{}{}
		out = append(out, provider)
	}
	return out
}

// FilterByProvider returns provider/model pairs for the requested provider.
func FilterByProvider(provider string) []string {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return List()
	}
	out := make([]string, 0, len(supportedModels))
	for _, model := range supportedModels {
		if ProviderOf(model) == provider {
			out = append(out, model)
		}
	}
	return out
}

// ProviderOf extracts the provider prefix from a provider/model identifier.
func ProviderOf(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	if idx := strings.Index(model, "/"); idx > 0 {
		return model[:idx]
	}
	return ""
}

// HasProvider reports whether the provider exists in the catalog.
func HasProvider(provider string) bool {
	return slices.Contains(Providers(), strings.TrimSpace(provider))
}
