package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/saferestart"
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

// TestShutdownWaitsForSkillSync guards #1143: ListenAndServe returns only
// after the skill sync goroutine has finished, so it cannot write into the
// data dir after the server has stopped.
func TestShutdownWaitsForSkillSync(t *testing.T) {
	stubBootSideEffects(t, func(context.Context) {})
	var synced atomic.Bool
	started := make(chan struct{})
	syncShippedSkills = func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		time.Sleep(50 * time.Millisecond)
		synced.Store(true)
	}

	cfg := &config.Config{Server: config.ServerConfig{Port: freeTCPPort(t), NoTLS: true}}
	startup.Begin()
	srv := New(cfg, "tok")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(ctx) }()

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("skill sync never started")
	}
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("server did not stop")
	}
	assert.True(t, synced.Load(), "ListenAndServe returned before skill sync finished")
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

func TestServerListen(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })

	t.Run("plain", func(t *testing.T) {
		port := freeTCPPort(t)
		srv := &Server{cfg: &config.Config{Server: config.ServerConfig{Port: port, NoTLS: true}}}
		ln, err := srv.listen()
		require.NoError(t, err)
		defer func() { _ = ln.Close() }()
		assert.Equal(t, fmt.Sprintf("127.0.0.1:%d", port), ln.Addr().String())
	})

	t.Run("tls", func(t *testing.T) {
		srv := &Server{cfg: &config.Config{Server: config.ServerConfig{Port: freeTCPPort(t)}}}
		ln, err := srv.listen()
		require.NoError(t, err)
		defer func() { _ = ln.Close() }()
		go func() {
			conn, err := ln.Accept()
			if err == nil {
				_ = conn.(*tls.Conn).Handshake()
				_ = conn.Close()
			}
		}()
		conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // self-signed test certificate
		require.NoError(t, err, "the listener serves TLS")
		assert.GreaterOrEqual(t, conn.ConnectionState().Version, uint16(tls.VersionTLS12))
		_ = conn.Close()
	})

	t.Run("bad certificate", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing.pem")
		srv := &Server{cfg: &config.Config{Server: config.ServerConfig{Port: freeTCPPort(t), TLS: &config.TLSConfig{Cert: missing, Key: missing}}}}
		_, err := srv.listen()
		assert.ErrorContains(t, err, "loading TLS")
	})

	for _, noTLS := range []bool{true, false} {
		t.Run(fmt.Sprintf("port in use notls=%v", noTLS), func(t *testing.T) {
			busy, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer func() { _ = busy.Close() }()
			port := busy.Addr().(*net.TCPAddr).Port
			srv := &Server{cfg: &config.Config{Server: config.ServerConfig{Port: port, NoTLS: noTLS}}}
			_, err = srv.listen()
			assert.ErrorContains(t, err, fmt.Sprintf("listening on 127.0.0.1:%d", port))
		})
	}
}

// startTestServer boots srv on a free port and waits until it answers.
func startTestServer(t *testing.T, srv *Server, port int) (<-chan error, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe(ctx) }()
	t.Cleanup(cancel)
	waitHealthy(t, port)
	return done, cancel
}

func waitHealthy(t *testing.T, port int) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	require.Eventually(t, func() bool {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 10*time.Second, 10*time.Millisecond, "server never answered /healthz")
}

func TestListenAndServe_RestartRequests(t *testing.T) {
	tests := []struct {
		name    string
		trigger func(*Server)
		want    error
	}{
		{name: "re-exec", trigger: func(s *Server) { s.reexecCh <- struct{}{} }, want: ErrReexecRequired},
		{name: "hard restart", trigger: func(s *Server) { s.hardRestartCh <- struct{}{} }, want: ErrRestartRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubBootSideEffects(t, func(context.Context) {})
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			fired := stubRestartWatchdog(t, time.Minute)
			require.NoError(t, saferestart.WriteMarker(saferestart.Marker{Reason: "restart"}))

			port := freeTCPPort(t)
			startup.Begin()
			srv := New(&config.Config{Server: config.ServerConfig{Port: port, NoTLS: true}}, "tok")
			done, _ := startTestServer(t, srv, port)
			_, pending := saferestart.TakeMarker()
			assert.False(t, pending, "boot consumes the previous restart's marker")

			srv.listenerRestartCh <- struct{}{}
			waitHealthy(t, port)

			tt.trigger(srv)
			select {
			case err := <-done:
				assert.ErrorIs(t, err, tt.want)
			case <-time.After(15 * time.Second):
				t.Fatal("server did not stop")
			}
			select {
			case <-fired:
				t.Fatal("restart watchdog fired on a clean shutdown")
			default:
			}
		})
	}
}

func TestListenAndServe_PortInUse(t *testing.T) {
	stubBootSideEffects(t, func(context.Context) {})
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = busy.Close() }()
	port := busy.Addr().(*net.TCPAddr).Port

	srv := New(&config.Config{Server: config.ServerConfig{Port: port, NoTLS: true}}, "tok")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err = srv.ListenAndServe(ctx)
	assert.ErrorContains(t, err, fmt.Sprintf("listening on 127.0.0.1:%d", port))
}
