package server

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/mcp"
	"github.com/lsegal/aviary/internal/saferestart"
	"github.com/lsegal/aviary/internal/update"
)

// syncBuffer is a log destination safe for concurrent writers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// slowToolClient connects to an MCP server whose "slow" tool runs until
// release is closed, appending "slow call done" to log as it returns.
func slowToolClient(t *testing.T, log *[]string, logMu *sync.Mutex, release <-chan struct{}) *mcp.InProcessClient {
	t.Helper()
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "aviary-test"}, nil)
	mcp.AddTool(srv, &sdkmcp.Tool{Name: "slow"}, func(context.Context, *sdkmcp.CallToolRequest, struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		<-release
		logMu.Lock()
		*log = append(*log, "slow call done")
		logMu.Unlock()
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "slow result"}}}, struct{}{}, nil
	})
	client, err := mcp.NewInProcessClient(context.Background(), srv)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestSelfUpdater_AutoUpdateWaitsForSlowMCPCall(t *testing.T) {
	check := update.CheckResult{CurrentVersion: "1.2.3", LatestVersion: "v1.3.0", UpgradeAvailable: true, UpgradeSupported: true}
	srv, _, log := setupSafeRestart(t, "1.2.3", check)
	logs := captureLogs(t)
	var logMu sync.Mutex
	installRelease = func(_ context.Context, opts update.InstallOptions) (update.InstallResult, error) {
		logMu.Lock()
		*log = append(*log, "install "+opts.Version)
		logMu.Unlock()
		return update.InstallResult{Version: opts.Version}, nil
	}

	release := make(chan struct{})
	client := slowToolClient(t, log, &logMu, release)
	result := make(chan string, 1)
	go func() {
		text, err := client.CallToolText(context.Background(), "slow", map[string]any{})
		assert.NoError(t, err)
		result <- text
	}()
	require.Eventually(t, func() bool { return saferestart.MCPCalls.Active() == 1 }, 2*time.Second, time.Millisecond)

	runUpdater(t, srv, true)
	require.Eventually(t, func() bool {
		st := srv.RestartStatus()
		return st.Phase == saferestart.PhaseDraining && st.MCPCalls == 1
	}, 2*time.Second, time.Millisecond, "the restart status shows it is waiting on 1 MCP call")
	require.Eventually(t, func() bool {
		out := logs.String()
		return strings.Contains(out, "restart deferred") &&
			strings.Contains(out, "active_mcp_calls=1") &&
			strings.Contains(out, `reason="waiting for in-flight MCP calls"`)
	}, 2*time.Second, time.Millisecond, "the deferral is logged with its reason and the call count")
	select {
	case <-srv.reexecCh:
		t.Fatal("restarted while an MCP call was in flight")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	assert.Equal(t, "slow result", <-result, "the in-flight call completes with its result")
	waitReexec(t, srv)
	logMu.Lock()
	defer logMu.Unlock()
	assert.Equal(t, []string{"slow call done", "install v1.3.0", "checkpoint"}, *log,
		"the call finishes before the update installs and restarts")
}

func TestSafeRestart_ForceSkipsMCPCallWait(t *testing.T) {
	srv, _, _ := setupSafeRestart(t, "1.2.3", update.CheckResult{})
	hold, err := saferestart.MCPCalls.Acquire(context.Background())
	require.NoError(t, err)
	defer hold()

	_, err = srv.requestRestart(context.Background(), saferestart.Request{Force: true})
	require.NoError(t, err)
	waitReexec(t, srv)
}

func TestSafeRestart_CancelWhileWaitingOnMCPCalls(t *testing.T) {
	srv, p, _ := setupSafeRestart(t, "1.2.3", update.CheckResult{})
	hold, err := saferestart.MCPCalls.Acquire(context.Background())
	require.NoError(t, err)
	defer hold()

	_, err = srv.requestRestart(context.Background(), saferestart.Request{})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return srv.RestartStatus().Phase == saferestart.PhaseDraining }, 2*time.Second, time.Millisecond)

	st, err := srv.requestRestart(context.Background(), saferestart.Request{Cancel: true})
	require.NoError(t, err)
	assert.Equal(t, saferestart.PhaseIdle, st.Phase)
	held, _ := p.snapshot()
	assert.Equal(t, []bool{true, false}, held, "canceling releases held jobs")

	require.Eventually(t, func() bool {
		next, err := saferestart.MCPCalls.Acquire(context.Background())
		if err != nil {
			return false
		}
		next()
		return true
	}, 2*time.Second, time.Millisecond, "a canceled restart admits new MCP calls")
	select {
	case <-srv.reexecCh:
		t.Fatal("a canceled restart re-executed")
	case <-time.After(50 * time.Millisecond):
	}
}
