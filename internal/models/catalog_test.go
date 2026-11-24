package models

import (
	"testing"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/stretchr/testify/assert"
)

func TestProviders(t *testing.T) {
	assert.Equal(t, []string{"google", "google-gemini", "anthropic", "openai", "openai-codex", "github-copilot", "meta"}, Providers())
}

func TestFilterByProvider(t *testing.T) {
	openai := FilterByProvider("openai")
	assert.NotEmpty(t, openai)
	for _, model := range openai {
		assert.Equal(t, "openai", ProviderOf(model))
	}
	assert.Empty(t, FilterByProvider("does-not-exist"))
}

func TestListReturnsCopy(t *testing.T) {
	list := List()
	list[0] = "mutated"
	assert.NotEqual(t, "mutated", List()[0])
}

func TestEntriesExposeMetadata(t *testing.T) {
	entry, ok := Lookup("openai/gpt-5.5")
	assert.True(t, ok)
	assert.Equal(t, 1050000, entry.InputTokens)
	assert.Equal(t, 128000, entry.OutputTokens)
	assert.True(t, entry.SupportsImageInput)
}

func TestEntriesReturnsCopy(t *testing.T) {
	entries := Entries()
	entries[0].ID = "mutated"
	assert.NotEqual(t, "mutated", Entries()[0].ID)
}

func TestCurrentAnthropicModelsHaveMillionTokenContext(t *testing.T) {
	for _, id := range []string{
		"anthropic/claude-fable-5-2",
		"anthropic/claude-fable-5-1",
		"anthropic/claude-fable-5",
		"anthropic/claude-opus-5-5",
		"anthropic/claude-opus-5",
		"anthropic/claude-opus-4-8",
		"anthropic/claude-opus-4-7",
		"anthropic/claude-opus-4-6",
		"anthropic/claude-sonnet-5-5",
		"anthropic/claude-sonnet-5",
		"anthropic/claude-sonnet-4-6",
		"anthropic/claude-haiku-5-5",
	} {
		entry, ok := Lookup(id)
		if assert.True(t, ok, id) {
			assert.Equal(t, 1000000, entry.InputTokens, id)
			assert.Equal(t, 128000, entry.OutputTokens, id)
			assert.True(t, entry.SupportsImageInput, id)
		}
	}
}

func TestCodexModelsMatchBackendLineup(t *testing.T) {
	assert.Equal(t, []string{
		"openai-codex/gpt-6.2-astra",
		"openai-codex/gpt-6.2-sol",
		"openai-codex/gpt-6.1-terra",
		"openai-codex/gpt-6.1-sol",
		"openai-codex/gpt-6-astra",
		"openai-codex/gpt-6-sol",
		"openai-codex/gpt-6-luna",
		"openai-codex/gpt-5.6-sol",
		"openai-codex/gpt-5.6-terra",
		"openai-codex/gpt-5.6-luna",
	}, FilterByProvider("openai-codex"))
}

// TestCodexDefaultModelIsInTheCodexLineup ties the codex CLI's default model
// to the catalog's codex lineup, so neither can be carried forward without the
// other: issue #1199 was the catalog stuck a generation behind the CLI.
func TestCodexDefaultModelIsInTheCodexLineup(t *testing.T) {
	definition, ok := clidefs.MustBuiltin().Lookup("codex")
	if assert.True(t, ok) {
		assert.Contains(t, FilterByProvider("openai-codex"), "openai-codex/"+definition.ModelOrDefault(""))
	}
}

// TestMetaModelsMatchTheMuseLineup pins the Muse models Meta's Model API
// serves for text generation, each with the 1,048,576-token context window
// its docs list and image input.
func TestMetaModelsMatchTheMuseLineup(t *testing.T) {
	assert.Equal(t, []string{
		"meta/muse-spark-1.3",
		"meta/muse-spark-1.3-contributor",
		"meta/muse-spark-1.2",
		"meta/muse-spark-1.2-contributor",
		"meta/muse-spark-1.1",
		"meta/muse-wonder-1.0",
		"meta/muse-ember-1.0",
	}, FilterByProvider("meta"))
	for _, entry := range EntriesByProvider("meta") {
		assert.Equal(t, 1048576, entry.InputTokens, entry.ID)
		assert.Positive(t, entry.OutputTokens, entry.ID)
		assert.True(t, entry.SupportsImageInput, entry.ID)
	}
	ember, _ := Lookup("meta/muse-ember-1.0")
	assert.Equal(t, 524288, ember.OutputTokens)
}

// TestCatalogPricesAreSetTogether checks that every catalog price is positive
// and that input and output prices are only ever set as a pair, so cost
// estimates never price one side of a run at zero.
func TestCatalogPricesAreSetTogether(t *testing.T) {
	priced := 0
	for _, entry := range Entries() {
		in, out := entry.InputCostPerMTok, entry.OutputCostPerMTok
		if in == 0 && out == 0 {
			continue
		}
		priced++
		assert.Positive(t, in, entry.ID)
		assert.Positive(t, out, entry.ID)
	}
	assert.NotZero(t, priced)

	opus, _ := Lookup("anthropic/claude-opus-5-5")
	assert.Equal(t, 4.0, opus.InputCostPerMTok)
	assert.Equal(t, 20.0, opus.OutputCostPerMTok)

	// Subscription-billed providers have no per-token price to show.
	for _, provider := range []string{"openai-codex", "github-copilot", "google-gemini"} {
		for _, entry := range EntriesByProvider(provider) {
			assert.Zero(t, entry.InputCostPerMTok, entry.ID)
			assert.Zero(t, entry.OutputCostPerMTok, entry.ID)
		}
	}
}
