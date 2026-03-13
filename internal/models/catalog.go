// Package models exposes the embedded catalog of supported model identifiers.
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

// Entry describes a supported API model and its key token/modal capability limits.
type Entry struct {
	ID                 string `json:"id"`
	InputTokens        int    `json:"input_tokens"`
	OutputTokens       int    `json:"output_tokens"`
	SupportsImageInput bool   `json:"supports_image_input"`
}

var supportedModels []Entry

func init() {
	if err := json.Unmarshal(catalogJSON, &supportedModels); err != nil {
		panic(fmt.Errorf("loading model catalog: %w", err))
	}
}

// List returns the supported provider/model pairs in catalog order.
func List() []string {
	out := make([]string, len(supportedModels))
	for i, model := range supportedModels {
		out[i] = model.ID
	}
	return out
}

// Entries returns the structured model catalog in catalog order.
func Entries() []Entry {
	out := make([]Entry, len(supportedModels))
	copy(out, supportedModels)
	return out
}

// Providers returns the unique provider names in first-seen order.
func Providers() []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(supportedModels))
	for _, model := range supportedModels {
		provider := ProviderOf(model.ID)
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
		if ProviderOf(model.ID) == provider {
			out = append(out, model.ID)
		}
	}
	return out
}

// EntriesByProvider returns structured catalog entries for the requested provider.
func EntriesByProvider(provider string) []Entry {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return Entries()
	}
	out := make([]Entry, 0, len(supportedModels))
	for _, model := range supportedModels {
		if ProviderOf(model.ID) == provider {
			out = append(out, model)
		}
	}
	return out
}

// Lookup returns the model entry for the provided model identifier.
func Lookup(id string) (Entry, bool) {
	id = strings.TrimSpace(id)
	for _, model := range supportedModels {
		if model.ID == id {
			return model, true
		}
	}
	return Entry{}, false
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
