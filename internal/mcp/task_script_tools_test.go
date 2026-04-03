package mcp

import (
	"context"
	"testing"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/llm"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingProvider struct {
	requests []llm.Request
	events   []llm.Event
}

func (p *recordingProvider) Stream(_ context.Context, req llm.Request) (<-chan llm.Event, error) {
	p.requests = append(p.requests, req)
	ch := make(chan llm.Event, len(p.events)+1)
	for _, ev := range p.events {
		ch <- ev
	}
	if len(p.events) == 0 || p.events[len(p.events)-1].Type != llm.EventTypeDone {
		ch <- llm.Event{Type: llm.EventTypeDone}
	}
	close(ch)
	return ch, nil
}

func TestBuildCompileStageUserPromptIncludesInlineToolCatalog(t *testing.T) {
	prompt := buildCompileStageUserPrompt(
		"check status",
		"slack://alerts",
		[]agent.ToolInfo{{
			Name:        "web_get",
			Description: "Fetch a URL.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{"type": "string"},
				},
				"required": []string{"url"},
			},
		}},
		"Compiler analysis JSON:\n{}",
	)

	assert.Contains(t, prompt, "Available tools:\n- web_get")
	assert.Contains(t, prompt, "description: Fetch a URL.")
	assert.Contains(t, prompt, "\"url\": \"https://example.com\"")
	assert.Contains(t, prompt, "Compiler analysis JSON:\n{}")
	assert.NotContains(t, prompt, "registered via the provider API")
}

func TestCompleteLLMTextDoesNotRegisterNativeToolsWhenNoneRequested(t *testing.T) {
	provider := &recordingProvider{
		events: []llm.Event{
			{Type: llm.EventTypeText, Text: "ok"},
			{Type: llm.EventTypeDone},
		},
	}

	out, err := completeLLMText(context.Background(), provider, "test/model", "generation", "system", "user", nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", out)
	require.Len(t, provider.requests, 1)
	assert.Nil(t, provider.requests[0].Tools)
}
