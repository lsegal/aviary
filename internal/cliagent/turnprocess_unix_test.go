//go:build !windows

package cliagent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/llm"
)

func useLingeringDefinition(t *testing.T, fake string) Options {
	t.Helper()
	previous := turnWaitDelay
	turnWaitDelay = 200 * time.Millisecond
	t.Cleanup(func() { turnWaitDelay = previous })
	opts := useGenericDefinition(t, clidefs.AssignEngine)
	opts.NewSessionID = "sess-1"
	opts.Env = []string{"CLIAGENT_FAKE=" + fake}
	return opts
}

func TestDefinitionSession_FinishedTurnSucceedsWhenOutputLingers(t *testing.T) {
	s, err := NewSession(useLingeringDefinition(t, "lingering"))
	require.NoError(t, err)
	started := time.Now()
	result, err := s.Send(context.Background(), Turn{Text: "hello"}, nil)
	require.NoError(t, err, "a turn that reached its final result succeeds even though its output stayed open")
	assert.Contains(t, result.Text, "prompt=hello")
	assert.Less(t, time.Since(started), 10*time.Second, "the turn stops waiting on the leftover child")
}

func TestDefinitionSession_UnfinishedTurnFailsWhenOutputLingers(t *testing.T) {
	s, err := NewSession(useLingeringDefinition(t, "lingering-unfinished"))
	require.NoError(t, err)
	_, err = s.Send(context.Background(), Turn{Text: "hello"}, nil)
	require.Error(t, err, "a turn without a final result is still a failure")
	assert.Contains(t, err.Error(), "WaitDelay")
}

// useLingeringJSONLDefinition is useLingeringDefinition with the fake CLI
// decoded by the generic JSONL decoder, declaring its final result event.
func useLingeringJSONLDefinition(t *testing.T, fake string) Options {
	t.Helper()
	opts := useLingeringDefinition(t, fake)
	registry, err := llm.CLIProviderRegistry()
	require.NoError(t, err)
	definition, ok := registry.Lookup("fakecli")
	require.True(t, ok)
	definition.Output = clidefs.Output{Format: clidefs.FormatJSONL, JSONL: &clidefs.JSONL{
		Type: "kind", Text: "text",
		Result: &clidefs.JSONLResult{Event: "end", Status: "outcome", Success: []string{"ok"}},
	}}
	registry, err = clidefs.NewRegistry(definition)
	require.NoError(t, err)
	llm.SetCLIProviderRegistry(registry)
	return opts
}

func TestDefinitionSession_JSONLTurnWithDeclaredResultSucceedsWhenOutputLingers(t *testing.T) {
	s, err := NewSession(useLingeringJSONLDefinition(t, "lingering-jsonl"))
	require.NoError(t, err)
	started := time.Now()
	result, err := s.Send(context.Background(), Turn{Text: "hello"}, nil)
	require.NoError(t, err, "a JSONL turn that reached its declared final result succeeds even though its output stayed open")
	assert.Contains(t, result.Text, "all done")
	assert.Less(t, time.Since(started), 10*time.Second, "the turn stops waiting on the leftover child")
}

func TestDefinitionSession_JSONLTurnWithoutDeclaredResultFailsWhenOutputLingers(t *testing.T) {
	s, err := NewSession(useLingeringJSONLDefinition(t, "lingering-jsonl-unfinished"))
	require.NoError(t, err)
	_, err = s.Send(context.Background(), Turn{Text: "hello"}, nil)
	require.Error(t, err, "a JSONL turn without its final result is still a failure")
	assert.Contains(t, err.Error(), "WaitDelay")
}
