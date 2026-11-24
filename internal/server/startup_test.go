package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/startup"
	"github.com/lsegal/aviary/internal/store"
)

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())
	return port
}

// stubBootSideEffects keeps a test boot from installing skills into the
// CLIs' directories or checking for updates.
func stubBootSideEffects(t *testing.T, install func(context.Context)) {
	t.Helper()
	resetSlogForTest()
	store.SetDataDir(t.TempDir())
	origInstall, origSync, origCheckout := installShippedSkills, syncShippedSkills, gitCheckout
	installShippedSkills = install
	syncShippedSkills = func(context.Context) {}
	gitCheckout = func() string { return "" }
	t.Cleanup(func() {
		installShippedSkills, syncShippedSkills, gitCheckout = origInstall, origSync, origCheckout
		store.SetDataDir("")
	})
}

// TestTimeToMCPReady guards the time-to-MCP-ready target: /mcp answers
// within startup.MCPReadyTarget of boot beginning, even while a background
// subsystem is still initializing, and /readyz reports that subsystem as
// pending until it finishes.
func TestTimeToMCPReady(t *testing.T) {
	release := make(chan struct{})
	stubBootSideEffects(t, func(ctx context.Context) {
		// A skill sync that takes far longer than the target.
		select {
		case <-release:
		case <-ctx.Done():
		}
	})

	port := freeTCPPort(t)
	cfg := &config.Config{Server: config.ServerConfig{Port: port, NoTLS: true}}
	trace := startup.Begin()
	srv := New(cfg, "tok")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(ctx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("server did not stop")
		}
	}()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := client.Get(base + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		require.True(t, time.Now().Before(deadline), "server never answered /healthz")
		time.Sleep(10 * time.Millisecond)
	}

	report := trace.Snapshot()
	require.Positive(t, report.MCPReadyMS, "/mcp ready was never recorded")
	assert.LessOrEqual(t, report.MCPReadyMS, startup.MCPReadyTarget.Milliseconds(),
		"time to MCP ready regressed past the %s target: %s", startup.MCPReadyTarget, report.Detail())

	req, err := http.NewRequest(http.MethodPost, base+"/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := client.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "/mcp should serve while skills sync")

	readyz := func() (int, startup.Report) {
		resp, err := client.Get(base + "/readyz")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		var r startup.Report
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&r))
		return resp.StatusCode, r
	}
	code, r := readyz()
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Contains(t, subsystemStates(r), "skills=pending")
	assert.Contains(t, subsystemStates(r), "mcp=ready")

	close(release)
	require.Eventually(t, func() bool {
		code, _ := readyz()
		return code == http.StatusOK
	}, 5*time.Second, 10*time.Millisecond)
	_, r = readyz()
	assert.Contains(t, subsystemStates(r), "skills=ready")
	assert.Contains(t, subsystemStates(r), "channels=ready")
	assert.Contains(t, subsystemStates(r), "projects=ready")
}

func subsystemStates(r startup.Report) []string {
	var out []string
	for _, s := range r.Subsystems {
		out = append(out, s.Name+"="+s.State)
	}
	return out
}

func TestHealthzAnswersOK(t *testing.T) {
	rr := httptest.NewRecorder()
	healthzHandler(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.JSONEq(t, `{"ok":true}`, rr.Body.String())
}

func TestReadyzLeavesOutErrorText(t *testing.T) {
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
	trace := startup.Begin()
	failed := make(chan struct{})
	trace.Go("channels", "channels connect", func() error {
		return fmt.Errorf("token xoxb-secret rejected")
	}, func() { close(failed) })
	<-failed
	trace.MarkMCPReady()

	rr := httptest.NewRecorder()
	readyzHandler(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	assert.Equal(t, http.StatusOK, rr.Code, "a failed subsystem is no longer initializing")
	assert.Contains(t, rr.Body.String(), `"state":"failed"`)
	assert.NotContains(t, rr.Body.String(), "xoxb-secret")
}

// TestShutdownHTTPEndsEventStreams covers a restart with an MCP client's
// standing event stream open: stopping the server ends the stream instead of
// waiting for it forever.
func TestShutdownHTTPEndsEventStreams(t *testing.T) {
	streaming := make(chan struct{})
	ended := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(streaming)
		<-r.Context().Done()
		close(ended)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	streams, endStreams := context.WithCancel(context.Background())
	srv := &http.Server{Handler: endableStreams(streams, h)}
	go func() { _ = srv.Serve(ln) }()

	req, err := http.NewRequest(http.MethodGet, "http://"+ln.Addr().String()+"/mcp", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	<-streaming

	start := time.Now()
	shutdownHTTP(srv, endStreams, 10*time.Second)
	assert.Less(t, time.Since(start), 2*time.Second, "shutdown waited on the event stream")
	select {
	case <-ended:
	default:
		t.Fatal("event stream handler was not ended")
	}
}

func TestEndableStreamsLeavesOtherRequestsAlone(t *testing.T) {
	streams, endStreams := context.WithCancel(context.Background())
	endStreams()
	var canceled bool
	h := endableStreams(streams, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		canceled = r.Context().Err() != nil
	}))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Accept", "application/json, text/event-stream")
	h.ServeHTTP(httptest.NewRecorder(), req)
	assert.False(t, canceled, "a tool call must not be ended with the streams")
}
