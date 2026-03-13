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
	assert.Contains(t, text, "MODEL")
	assert.Contains(t, text, "INPUT")
	assert.Contains(t, text, "OUTPUT")
	assert.Contains(t, text, "TYPE")
	assert.Contains(t, text, "anthropic/claude-sonnet-4-5")
	assert.Contains(t, text, "openai-codex/gpt-5.2")
	assert.Contains(t, text, "text+image")
}

func TestRunModelsList_FilterProvider(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, runModelsList(&out, "openai"))
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Greater(t, len(lines), 1)
	for _, line := range lines[1:] {
		assert.True(t, strings.HasPrefix(strings.TrimSpace(line), "openai/"))
	}
}

func TestRunModelsList_UnknownProvider(t *testing.T) {
	var out bytes.Buffer
	err := runModelsList(&out, "bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown provider "bogus"`)
}
