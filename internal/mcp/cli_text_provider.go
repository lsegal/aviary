package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/llm"
)

// completeCLIText runs one prompt through a CLI-backed model; tests replace it.
var completeCLIText = agent.CompleteCLIText

// cliTextProvider answers text-only requests to a CLI-backed model, such as
// "claude/opus", by running its CLI once, so the task compiler, task_analyze,
// and session titles work for agents whose model has no model API.
type cliTextProvider struct{ model string }

func (p cliTextProvider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	if len(req.Tools) > 0 {
		return nil, fmt.Errorf("model %q runs through its CLI, which does not take native tool definitions", p.model)
	}
	var prompt []string
	for _, msg := range req.Messages {
		if text := strings.TrimSpace(msg.Content); text != "" {
			prompt = append(prompt, text)
		}
	}
	ch := make(chan llm.Event, 2)
	go func() {
		defer close(ch)
		text, err := completeCLIText(ctx, p.model, req.System, strings.Join(prompt, "\n\n"))
		if err != nil {
			ch <- llm.Event{Type: llm.EventTypeError, Error: err}
			return
		}
		ch <- llm.Event{Type: llm.EventTypeText, Text: text}
		ch <- llm.Event{Type: llm.EventTypeDone}
	}()
	return ch, nil
}
