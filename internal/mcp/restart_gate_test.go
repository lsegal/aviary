package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/saferestart"
)

// useTestGate swaps in a fresh restart gate for the test.
func useTestGate(t *testing.T, admitWait time.Duration) *saferestart.Gate {
	t.Helper()
	orig := saferestart.MCPCalls
	g := saferestart.NewGate()
	g.SetTimings(time.Hour, time.Hour, admitWait)
	saferestart.MCPCalls = g
	t.Cleanup(func() { saferestart.MCPCalls = orig })
	return g
}

func TestRestartGated_ReleasesOnErrorAndPanic(t *testing.T) {
	g := useTestGate(t, time.Second)

	failing := restartGated(func(context.Context, *sdkmcp.CallToolRequest, struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		assert.Equal(t, 1, g.Active(), "the call holds the gate while it runs")
		return nil, struct{}{}, errors.New("boom")
	})
	_, _, err := failing(context.Background(), nil, struct{}{})
	require.EqualError(t, err, "boom")
	assert.Equal(t, 0, g.Active())

	panicking := restartGated(func(context.Context, *sdkmcp.CallToolRequest, struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		panic("boom")
	})
	assert.Panics(t, func() { _, _, _ = panicking(context.Background(), nil, struct{}{}) })
	assert.Equal(t, 0, g.Active())
}

func TestToolCallDuringRestartGetsRetryableError(t *testing.T) {
	g := useTestGate(t, 20*time.Millisecond)
	ctx := context.Background()
	client, err := NewInProcessClient(ctx, NewServer())
	require.NoError(t, err)
	defer client.Close() //nolint:errcheck

	unlock, _, err := g.Lock(ctx, saferestart.LockOptions{})
	require.NoError(t, err)
	res, err := client.CallTool(ctx, "ping", map[string]any{})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Equal(t, "server restarting, try again", extractText(res))

	unlock()
	text, err := client.CallToolText(ctx, "ping", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "pong", text)
}

func TestToolCallWaitsOutShortRestartLock(t *testing.T) {
	g := useTestGate(t, 5*time.Second)
	ctx := context.Background()
	client, err := NewInProcessClient(ctx, NewServer())
	require.NoError(t, err)
	defer client.Close() //nolint:errcheck

	unlock, _, err := g.Lock(ctx, saferestart.LockOptions{})
	require.NoError(t, err)
	time.AfterFunc(50*time.Millisecond, unlock)
	text, err := client.CallToolText(ctx, "ping", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "pong", text, "the call gets its result once the lock is released")
}
