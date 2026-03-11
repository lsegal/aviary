package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunModelsList_All(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, runModelsList(&out, ""))
	text := out.String()
	assert.Contains(t, text, "anthropic/claude-sonnet-4-5")
	assert.Contains(t, text, "openai-codex/gpt-5.2")
}

func TestRunModelsList_FilterProvider(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, runModelsList(&out, "openai"))
	lines := strings.Fields(strings.TrimSpace(out.String()))
	require.NotEmpty(t, lines)
	for _, line := range lines {
		assert.True(t, strings.HasPrefix(line, "openai/"))
	}
}

func TestRunModelsList_UnknownProvider(t *testing.T) {
	var out bytes.Buffer
	err := runModelsList(&out, "bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown provider "bogus"`)
}
