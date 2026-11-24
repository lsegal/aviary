//go:build !windows

package cliagent

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/clidefs"
)

// fakeLinger leaves a background child holding the fake CLI's stdout, as an
// agent that started a `&` shell does (#1122).
func fakeLinger() {
	child := exec.Command("sleep", "30")
	child.Stdout = os.Stdout
	_ = child.Start()
}

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
