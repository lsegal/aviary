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
