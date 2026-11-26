package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/channels"
	"github.com/lsegal/aviary/internal/config"
)

// fakeDaemonChannels is a daemonChannelSource with canned statuses and logs.
type fakeDaemonChannels struct {
	statuses []channels.ChannelStatus
	history  map[string][]string
	live     chan string

	mu          sync.Mutex
	unsubCalls  int
	restartErr  error
	restartKeys []string
}

func (f *fakeDaemonChannels) List() []channels.ChannelStatus { return f.statuses }

func (f *fakeDaemonChannels) SubscribeLogs(key string) ([]string, <-chan string, func(), bool) {
	history, ok := f.history[key]
	if !ok {
		return nil, nil, nil, false
	}
	return history, f.live, func() {
		f.mu.Lock()
		f.unsubCalls++
		f.mu.Unlock()
	}, true
}

func (f *fakeDaemonChannels) Restart(_ context.Context, key string, _ func(string, string, string, channels.Channel, channels.IncomingMessage)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restartKeys = append(f.restartKeys, key)
	return f.restartErr
}

func (f *fakeDaemonChannels) unsubscribed() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unsubCalls
}

func newDaemonTestServer(t *testing.T, fake *fakeDaemonChannels) *Server {
	t.Helper()
	setupServerDataDir(t)
	resetSlogForTest()
	srv := New(&config.Config{}, "tok")
	if fake != nil {
		srv.daemonChannels = fake
	}
	return srv
}

func getDaemons(t *testing.T, srv *Server) []DaemonStatus {
	t.Helper()
	rr := httptest.NewRecorder()
	srv.daemonsHandler(rr, httptest.NewRequest(http.MethodGet, "/api/daemons", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	var daemons []DaemonStatus
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&daemons))
	return daemons
}

func TestDaemonsHandler_SelfEntry(t *testing.T) {
	srv := newDaemonTestServer(t, &fakeDaemonChannels{})
	srv.cfg.Server.Port = 18080
	srv.sampler.stats[os.Getpid()] = ProcStats{CPUPercent: 12.5, Status: "sleeping"}

	daemons := getDaemons(t, srv)
	require.Len(t, daemons, 1)
	self := daemons[0]
	assert.Equal(t, "aviary", self.Name)
	assert.Equal(t, os.Getpid(), self.PID)
	assert.Equal(t, ":18080", self.Addr)
	assert.Equal(t, 12.5, self.CPUPercent)
	assert.Equal(t, "sleeping", self.Status)
	assert.NotZero(t, self.RSSBytes)
	assert.False(t, self.Managed)
}

func TestDaemonsHandler_ChannelDaemons(t *testing.T) {
	// A listener that accepts gives a reachable external daemon address; a
	// closed listener's address gives an unreachable one.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close() //nolint:errcheck
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close() //nolint:errcheck
		}
	}()
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	deadAddr := dead.Addr().String()
	require.NoError(t, dead.Close())

	chStarted := time.Now().Add(-90 * time.Second)
	daemonStarted := time.Now().Add(-2 * time.Hour)
	fake := &fakeDaemonChannels{statuses: []channels.ChannelStatus{
		{Key: "bot/slack/0", Type: "slack", ID: "T1", Started: chStarted},
		{Key: "bot/discord/0", Type: "discord", Started: chStarted, Error: "auth failed"},
		{Key: "bot/signal/0", Type: "signal", ID: "+1", Started: chStarted,
			Daemon: &channels.DaemonInfo{PID: 4242, Addr: "127.0.0.1:7583", Started: daemonStarted}},
		// Second channel sharing the same subprocess is skipped.
		{Key: "bot/signal/1", Type: "signal", ID: "+1", Started: chStarted,
			Daemon: &channels.DaemonInfo{PID: 4242, Addr: "127.0.0.1:7583", Started: daemonStarted}},
		// Running daemon with no sampled stats and no start time.
		{Key: "other/signal/0", Type: "signal", ID: "+2", Started: chStarted,
			Daemon: &channels.DaemonInfo{PID: 5151}},
		// Managed daemons not yet running dedupe by type+id.
		{Key: "a/signal/0", Type: "signal", ID: "+3", Started: chStarted, Daemon: &channels.DaemonInfo{}},
		{Key: "b/signal/0", Type: "signal", ID: "+3", Started: chStarted, Daemon: &channels.DaemonInfo{}},
		{Key: "ext/signal/0", Type: "signal", ID: "+4", Started: chStarted,
			Daemon: &channels.DaemonInfo{Addr: ln.Addr().String(), External: true}},
		{Key: "ext/signal/1", Type: "signal", ID: "+5", Started: chStarted,
			Daemon: &channels.DaemonInfo{Addr: deadAddr, External: true}},
		{Key: "ext/signal/2", Type: "signal", ID: "+6", Started: chStarted,
			Daemon: &channels.DaemonInfo{External: true}},
	}}
	srv := newDaemonTestServer(t, fake)
	srv.sampler.stats[4242] = ProcStats{CPUPercent: 3.5, RSSBytes: 2048, Status: "sleeping"}

	daemons := getDaemons(t, srv)
	byName := map[string]DaemonStatus{}
	var names []string
	for _, d := range daemons[1:] {
		byName[d.Name] = d
		names = append(names, d.Name)
	}
	assert.Equal(t, []string{
		"bot/slack/0", "bot/discord/0", "bot/signal/0", "other/signal/0",
		"a/signal/0", "ext/signal/0", "ext/signal/1", "ext/signal/2",
	}, names)

	slack := byName["bot/slack/0"]
	assert.Equal(t, "slack", slack.Type)
	assert.Equal(t, "running", slack.Status)
	assert.Equal(t, -1.0, slack.CPUPercent)
	assert.Equal(t, chStarted.UTC().Format(time.RFC3339), slack.Started)
	assert.Equal(t, "1m30s", slack.Uptime)
	assert.False(t, slack.Managed)

	discord := byName["bot/discord/0"]
	assert.Equal(t, "error", discord.Status)
	assert.Equal(t, "auth failed", discord.Error)

	sig := byName["bot/signal/0"]
	assert.Equal(t, 4242, sig.PID)
	assert.Equal(t, "127.0.0.1:7583", sig.Addr)
	assert.True(t, sig.Managed)
	assert.Equal(t, daemonStarted.UTC().Format(time.RFC3339), sig.Started)
	assert.Equal(t, "2h0m", sig.Uptime)
	assert.Equal(t, 3.5, sig.CPUPercent)
	assert.Equal(t, uint64(2048), sig.RSSBytes)
	assert.Equal(t, "sleeping", sig.Status)

	other := byName["other/signal/0"]
	assert.Equal(t, 5151, other.PID)
	assert.Equal(t, chStarted.UTC().Format(time.RFC3339), other.Started)
	assert.Equal(t, -1.0, other.CPUPercent)
	assert.Equal(t, "running", other.Status)

	assert.Equal(t, "starting", byName["a/signal/0"].Status)
	assert.True(t, byName["a/signal/0"].Managed)

	assert.Equal(t, "connected", byName["ext/signal/0"].Status)
	assert.False(t, byName["ext/signal/0"].Managed)
	assert.Equal(t, "unreachable", byName["ext/signal/1"].Status)
	assert.Equal(t, "running", byName["ext/signal/2"].Status)
}

// nonFlushingWriter is an http.ResponseWriter without http.Flusher.
type nonFlushingWriter struct {
	header http.Header
	code   int
	body   strings.Builder
}

func (w *nonFlushingWriter) Header() http.Header         { return w.header }
func (w *nonFlushingWriter) Write(b []byte) (int, error) { return w.body.Write(b) }
func (w *nonFlushingWriter) WriteHeader(code int)        { w.code = code }

func TestDaemonLogsHandler_StreamingNotSupported(t *testing.T) {
	fake := &fakeDaemonChannels{history: map[string][]string{"bot/signal/0": {"old"}}}
	srv := newDaemonTestServer(t, fake)

	w := &nonFlushingWriter{header: http.Header{}}
	srv.daemonLogsHandler(w, httptest.NewRequest(http.MethodGet, "/api/daemons/logs?key=bot/signal/0", nil))
	assert.Equal(t, http.StatusInternalServerError, w.code)
	assert.Contains(t, w.body.String(), "streaming not supported")
	assert.Equal(t, 1, fake.unsubscribed())
}

func TestDaemonLogsHandler_StreamsUntilCancelled(t *testing.T) {
	fake := &fakeDaemonChannels{
		history: map[string][]string{"bot/signal/0": {"first line", `say "hi"`}},
		live:    make(chan string),
	}
	srv := newDaemonTestServer(t, fake)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/daemons/logs?key=bot/signal/0", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		srv.daemonLogsHandler(rr, req)
		close(done)
	}()

	fake.live <- "live line" // blocks until the handler is reading live lines
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not return after context cancel")
	}

	assert.Equal(t, "text/event-stream", rr.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", rr.Header().Get("Cache-Control"))
	assert.Equal(t, "data: \"first line\"\n\ndata: \"say \\\"hi\\\"\"\n\ndata: \"live line\"\n\n", rr.Body.String())
	assert.Equal(t, 1, fake.unsubscribed())
}

func TestDaemonLogsHandler_LiveChannelClosed(t *testing.T) {
	live := make(chan string, 1)
	live <- "last"
	close(live)
	fake := &fakeDaemonChannels{history: map[string][]string{"k": nil}, live: live}
	srv := newDaemonTestServer(t, fake)

	rr := httptest.NewRecorder()
	srv.daemonLogsHandler(rr, httptest.NewRequest(http.MethodGet, "/api/daemons/logs?key=k", nil))
	assert.Equal(t, "data: \"last\"\n\n", rr.Body.String())
	assert.Equal(t, 1, fake.unsubscribed())
}

func postRestart(srv *Server, body string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	srv.daemonRestartHandler(rr, httptest.NewRequest(http.MethodPost, "/api/daemons/restart", strings.NewReader(body)))
	return rr
}

func withRuntime(srv *Server) {
	srv.runCtx = context.Background()
	srv.msgFn = func(string, string, string, channels.Channel, channels.IncomingMessage) {}
}

func TestDaemonRestartHandler_InvalidJSON(t *testing.T) {
	srv := newDaemonTestServer(t, nil)
	rr := postRestart(srv, "{not json")
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid JSON body")
}

func TestDaemonRestartHandler_SelfRestartPending(t *testing.T) {
	// A second request while a restart is already queued still answers 202
	// without blocking on the full channel.
	srv := newDaemonTestServer(t, nil)
	for range 2 {
		rr := postRestart(srv, `{"key":"aviary"}`)
		assert.Equal(t, http.StatusAccepted, rr.Code)
		assert.JSONEq(t, `{"status":"restarting"}`, rr.Body.String())
	}
	assert.Len(t, srv.hardRestartCh, 1)
}

func TestDaemonRestartHandler_RuntimeNotInitialized(t *testing.T) {
	fake := &fakeDaemonChannels{statuses: []channels.ChannelStatus{
		{Key: "bot/signal/0", Daemon: &channels.DaemonInfo{PID: 1}},
	}}
	srv := newDaemonTestServer(t, fake)
	rr := postRestart(srv, `{"key":"bot/signal/0"}`)
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
	assert.Empty(t, fake.restartKeys)
}

func TestDaemonRestartHandler_NotRestartable(t *testing.T) {
	fake := &fakeDaemonChannels{statuses: []channels.ChannelStatus{
		{Key: "bot/slack/0"},
		{Key: "bot/signal/ext", Daemon: &channels.DaemonInfo{External: true}},
		{Key: "bot/signal/0", Daemon: &channels.DaemonInfo{PID: 1}},
	}}
	srv := newDaemonTestServer(t, fake)
	withRuntime(srv)
	for _, key := range []string{"bot/slack/0", "bot/signal/ext", "missing"} {
		rr := postRestart(srv, `{"key":"`+key+`"}`)
		assert.Equal(t, http.StatusBadRequest, rr.Code, key)
		assert.Contains(t, rr.Body.String(), "daemon not restartable", key)
	}
	assert.Empty(t, fake.restartKeys)
}

func TestDaemonRestartHandler_RestartError(t *testing.T) {
	fake := &fakeDaemonChannels{
		statuses:   []channels.ChannelStatus{{Key: "bot/signal/0", Daemon: &channels.DaemonInfo{}}},
		restartErr: errors.New("channel bot/signal/0 not found"),
	}
	srv := newDaemonTestServer(t, fake)
	withRuntime(srv)
	rr := postRestart(srv, `{"key":"bot/signal/0"}`)
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "channel bot/signal/0 not found")
	assert.Equal(t, []string{"bot/signal/0"}, fake.restartKeys)
}

func TestDaemonRestartHandler_Success(t *testing.T) {
	fake := &fakeDaemonChannels{statuses: []channels.ChannelStatus{
		{Key: "bot/slack/0"},
		{Key: "bot/signal/0", Daemon: &channels.DaemonInfo{PID: 99}},
	}}
	srv := newDaemonTestServer(t, fake)
	withRuntime(srv)
	rr := postRestart(srv, `{"key":"bot/signal/0"}`)
	assert.Equal(t, http.StatusAccepted, rr.Code)
	assert.JSONEq(t, `{"status":"restarting"}`, rr.Body.String())
	assert.Equal(t, []string{"bot/signal/0"}, fake.restartKeys)
	assert.Empty(t, srv.hardRestartCh)
}
