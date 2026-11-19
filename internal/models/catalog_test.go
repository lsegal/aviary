package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProviders(t *testing.T) {
	assert.Equal(t, []string{"google", "google-gemini", "anthropic", "openai", "openai-codex", "github-copilot"}, Providers())
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
		"openai-codex/gpt-6-astra",
		"openai-codex/gpt-6-sol",
		"openai-codex/gpt-6-luna",
		"openai-codex/gpt-5.6-sol",
		"openai-codex/gpt-5.6-terra",
		"openai-codex/gpt-5.6-luna",
	}, FilterByProvider("openai-codex"))
}
