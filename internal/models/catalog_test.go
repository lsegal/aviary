package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProviders(t *testing.T) {
	assert.Equal(t, []string{"google", "anthropic", "openai", "openai-codex"}, Providers())
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
	entry, ok := Lookup("openai/gpt-4o")
	assert.True(t, ok)
	assert.Equal(t, 128000, entry.InputTokens)
	assert.Equal(t, 16384, entry.OutputTokens)
	assert.True(t, entry.SupportsImageInput)
}

func TestEntriesReturnsCopy(t *testing.T) {
	entries := Entries()
	entries[0].ID = "mutated"
	assert.NotEqual(t, "mutated", Entries()[0].ID)
}
