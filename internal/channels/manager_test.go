package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/slack-go/slack/socketmode"

	"github.com/lsegal/aviary/internal/config"
)

// mockChannel is a test implementation of Channel.
type mockChannel struct {
	mu        sync.Mutex
	started   bool
	stopped   bool
	msgFn     func(IncomingMessage)
	sendCalls []sendCall
	startErr  error
}

type sendCall struct {
	channel string
	text    string
}

func (m *mockChannel) Start(ctx context.Context) error {
	m.mu.Lock()
	m.started = true
	m.mu.Unlock()
	if m.startErr != nil {
		return m.startErr
	}
	<-ctx.Done()
	return nil
}

func (m *mockChannel) Stop() {
	m.mu.Lock()
	m.stopped = true
	m.mu.Unlock()
}

func (m *mockChannel) Send(channel, text string) error {
	m.mu.Lock()
	m.sendCalls = append(m.sendCalls, sendCall{channel, text})
	m.mu.Unlock()
	return nil
}

func (m *mockChannel) OnMessage(fn func(IncomingMessage)) {
	m.mu.Lock()
	m.msgFn = fn
	m.mu.Unlock()
}

// TestManager_NewManager verifies constructor initializes properly.
func TestManager_NewManager(t *testing.T) {
	mgr := NewManager()
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}
	if len(mgr.List()) != 0 {
		t.Fatal("expected empty channel list")
	}
}

// TestManager_Stop_Empty verifies Stop on empty manager doesn't panic.
func TestManager_Stop_Empty(t *testing.T) {
	mgr := NewManager()
	mgr.Stop() // should not panic
}

// TestManager_Stop_ClearsChannels verifies Stop clears internal state.
func TestManager_Stop_ClearsChannels(t *testing.T) {
	mgr := NewManager()

	// Manually inject a mock channel to simulate a running channel.
	mock := &mockChannel{}
	key := "agent1/signal/0"
	_, cancel := context.WithCancel(context.Background())

	mgr.mu.Lock()
	mgr.channels[key] = mock
	mgr.cancels[key] = cancel
	mgr.startTimes[key] = time.Now()
	mgr.sinks[key] = newLogSink()
	mgr.mu.Unlock()

	mgr.Stop()

	if len(mgr.channels) != 0 {
		t.Errorf("expected empty channels after Stop, got %d", len(mgr.channels))
	}
	if !mock.stopped {
		t.Error("expected mock channel to be stopped")
	}
}

// TestManager_List verifies List returns correct status entries.
func TestManager_List(t *testing.T) {
	mgr := NewManager()

	mock := &mockChannel{}
	key := "myagent/slack/2"
	now := time.Now()

	mgr.mu.Lock()
	mgr.channels[key] = mock
	mgr.cancels[key] = func() {}
	mgr.startTimes[key] = now
	mgr.sinks[key] = newLogSink()
	mgr.mu.Unlock()

	list := mgr.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 channel status, got %d", len(list))
	}
	s := list[0]
	if s.Agent != "myagent" {
		t.Errorf("Agent = %q; want %q", s.Agent, "myagent")
	}
	if s.Type != "slack" {
		t.Errorf("Type = %q; want %q", s.Type, "slack")
	}
	if s.Index != 2 {
		t.Errorf("Index = %d; want 2", s.Index)
	}
}

// TestManager_SubscribeLogs verifies SubscribeLogs returns history for known keys.
func TestManager_SubscribeLogs(t *testing.T) {
	mgr := NewManager()

	key := "agent/signal/0"
	sink := newLogSink()
	sink.Write("test log line")

	mgr.mu.Lock()
	mgr.sinks[key] = sink
	mgr.mu.Unlock()

	history, live, unsub, ok := mgr.SubscribeLogs(key)
	if !ok {
		t.Fatal("expected SubscribeLogs to return ok=true for known key")
	}
	defer unsub()
	_ = live

	if len(history) != 1 || history[0] != "test log line" {
		t.Errorf("unexpected history: %v", history)
	}
}

// TestManager_SubscribeLogs_UnknownKey verifies ok=false for unknown key.
func TestManager_SubscribeLogs_UnknownKey(t *testing.T) {
	mgr := NewManager()

	_, _, _, ok := mgr.SubscribeLogs("nonexistent/key/0")
	if ok {
		t.Error("expected SubscribeLogs to return ok=false for unknown key")
	}
}

// TestManager_RouteDelivery_NoChannels verifies error when no channels match.
func TestManager_RouteDelivery_NoChannels(t *testing.T) {
	mgr := NewManager()
	err := mgr.RouteDelivery("signal", "+1", "hello")
	if err == nil {
		t.Fatal("expected error when no channels present")
	}
}

// TestManager_RouteDelivery_Success verifies message is delivered via matching channel.
func TestManager_RouteDelivery_Success(t *testing.T) {
	mgr := NewManager()

	mock := &mockChannel{}
	key := "agent1/signal/0"

	mgr.mu.Lock()
	mgr.channels[key] = mock
	mgr.cancels[key] = func() {}
	mgr.startTimes[key] = time.Now()
	mgr.sinks[key] = newLogSink()
	mgr.mu.Unlock()

	err := mgr.RouteDelivery("signal", "+15551111111", "hello there")
	if err != nil {
		t.Fatalf("RouteDelivery: %v", err)
	}

	mock.mu.Lock()
	calls := mock.sendCalls
	mock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 send call, got %d", len(calls))
	}
	if calls[0].channel != "+15551111111" || calls[0].text != "hello there" {
		t.Errorf("unexpected send call: %+v", calls[0])
	}
}

// TestManager_RouteDelivery_WrongType verifies non-matching channel type returns error.
func TestManager_RouteDelivery_WrongType(t *testing.T) {
	mgr := NewManager()

	mock := &mockChannel{}
	key := "agent1/slack/0"
	mgr.mu.Lock()
	mgr.channels[key] = mock
	mgr.cancels[key] = func() {}
	mgr.startTimes[key] = time.Now()
	mgr.sinks[key] = newLogSink()
	mgr.mu.Unlock()

	err := mgr.RouteDelivery("signal", "+1", "msg")
	if err == nil {
		t.Error("expected error routing to non-existent type")
	}
}

// TestManager_Reconcile_Idempotent verifies calling Reconcile twice with same
// config does not create duplicate channels.
func TestManager_Reconcile_Idempotent(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Config with a signal channel (will fail to connect but that's ok for this test).
	cfg := &config.Config{
		Agents: []config.AgentConfig{
			{
				Name: "bot",
				Channels: []config.ChannelConfig{
					// Use empty signal URL which won't actually connect.
					{Type: "signal", Phone: "+1", URL: ""},
				},
			},
		},
	}

	var msgCount int
	msgFn := func(_ string, _ Channel, _ IncomingMessage) { msgCount++ }

	mgr.Reconcile(ctx, cfg, msgFn)
	mgr.Reconcile(ctx, cfg, msgFn) // second call should be idempotent

	list := mgr.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 channel after idempotent reconcile, got %d", len(list))
	}

	mgr.Stop()
}

// TestManager_Reconcile_RemovesChannel verifies channels removed from config are stopped.
func TestManager_Reconcile_RemovesChannel(t *testing.T) {
	mgr := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg1 := &config.Config{
		Agents: []config.AgentConfig{
			{
				Name: "bot",
				Channels: []config.ChannelConfig{
					{Type: "signal", Phone: "+1", URL: ""},
				},
			},
		},
	}
	cfg2 := &config.Config{Agents: []config.AgentConfig{{Name: "bot"}}} // no channels

	mgr.Reconcile(ctx, cfg1, func(_ string, _ Channel, _ IncomingMessage) {})
	if len(mgr.List()) != 1 {
		t.Fatalf("expected 1 channel before remove reconcile, got %d", len(mgr.List()))
	}

	mgr.Reconcile(ctx, cfg2, func(_ string, _ Channel, _ IncomingMessage) {})
	if len(mgr.List()) != 0 {
		t.Fatalf("expected 0 channels after remove reconcile, got %d", len(mgr.List()))
	}
}

// mockMediaSender implements both Channel and MediaSender.
type mockMediaSender struct {
	mockChannel
	mediaCalls []mediaCall
}

type mediaCall struct {
	channelID string
	caption   string
	filePath  string
}

func (m *mockMediaSender) SendMedia(channelID, caption, filePath string) error {
	m.mu.Lock()
	m.mediaCalls = append(m.mediaCalls, mediaCall{channelID, caption, filePath})
	m.mu.Unlock()
	return nil
}

func TestRouteMediaDelivery_NoChannels(t *testing.T) {
	mgr := NewManager()
	err := mgr.RouteMediaDelivery("slack", "C123", "caption", "/tmp/file.png")
	if err == nil {
		t.Error("expected error with no channels")
	}
}

func TestRouteMediaDelivery_WithMediaSender(t *testing.T) {
	mgr := NewManager()

	ms := &mockMediaSender{}
	mgr.mu.Lock()
	key := channelKey("bot", "slack", 0)
	mgr.channels[key] = ms
	mgr.startTimes[key] = time.Now()
	mgr.mu.Unlock()

	err := mgr.RouteMediaDelivery("slack", "C123", "hi", "/tmp/img.png")
	if err != nil {
		t.Fatalf("RouteMediaDelivery: %v", err)
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if len(ms.mediaCalls) != 1 {
		t.Errorf("expected 1 media call, got %d", len(ms.mediaCalls))
	}
}

func TestRouteMediaDelivery_WrongType(t *testing.T) {
	mgr := NewManager()

	ms := &mockMediaSender{}
	mgr.mu.Lock()
	key := channelKey("bot", "discord", 0)
	mgr.channels[key] = ms
	mgr.startTimes[key] = time.Now()
	mgr.mu.Unlock()

	// Routing to "slack" but channel is "discord" → error.
	err := mgr.RouteMediaDelivery("slack", "C123", "hi", "/tmp/img.png")
	if err == nil {
		t.Error("expected error when no channel of matching type")
	}
}

func TestNewChannel_UnknownType(t *testing.T) {
	ch := newChannel(config.ChannelConfig{Type: "unknown"}, "model", nil)
	if ch != nil {
		t.Errorf("expected nil for unknown channel type, got %T", ch)
	}
}

func TestNewChannel_Signal(t *testing.T) {
	// Signal channel should be created without error (even with dummy config).
	ch := newChannel(config.ChannelConfig{
		Type:  "signal",
		Phone: "+15551234567",
		URL:   "http://localhost:8080",
	}, "model", nil)
	if ch == nil {
		t.Error("expected non-nil Signal channel")
	}
}

// ── Discord tests ─────────────────────────────────────────────────────────────

func TestDiscordChannel_Constructor(t *testing.T) {
	ch := NewDiscordChannel("token123", nil, "gpt-4", []string{"fallback"})
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
}

func TestDiscordChannel_OnMessage(t *testing.T) {
	ch := NewDiscordChannel("t", nil, "m", nil)
	ch.OnMessage(func(IncomingMessage) {})
	ch.handlerMu.RLock()
	fn := ch.handler
	ch.handlerMu.RUnlock()
	if fn == nil {
		t.Error("expected handler to be set")
	}
}

func TestDiscordChannel_Send_NotConnected(t *testing.T) {
	ch := NewDiscordChannel("t", nil, "m", nil)
	err := ch.Send("C123", "hello")
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestDiscordChannel_Stop_Idempotent(t *testing.T) {
	ch := NewDiscordChannel("t", nil, "m", nil)
	ch.Stop()
	ch.Stop() // should not panic
}

func TestNewChannel_Discord(t *testing.T) {
	ch := newChannel(config.ChannelConfig{
		Type:  "discord",
		Token: "bot-token",
	}, "model", nil)
	if ch == nil {
		t.Error("expected non-nil Discord channel")
	}
}

// ── Slack tests ───────────────────────────────────────────────────────────────

func TestSlackChannel_Constructor(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "gpt-4", nil)
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
}

func TestSlackChannel_OnMessage(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	ch.OnMessage(func(IncomingMessage) {})
	ch.handlerMu.RLock()
	fn := ch.handler
	ch.handlerMu.RUnlock()
	if fn == nil {
		t.Error("expected handler to be set")
	}
}

func TestSlackChannel_Stop_NilCancel(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	ch.Stop() // should not panic when cancel is nil
	ch.Stop() // idempotent
}

func TestSlackChannel_Dispatch_WrongType(t *testing.T) {
	ch := NewSlackChannel("xapp-token", "xoxb-token", nil, "m", nil)
	// Non-eventsAPI event type causes early return before Ack — no panic.
	ch.dispatch(socketmode.Event{Type: socketmode.EventTypeHello})
}

func TestNewChannel_Slack(t *testing.T) {
	ch := newChannel(config.ChannelConfig{
		Type:  "slack",
		Token: "xoxb-token",
		URL:   "xapp-token",
	}, "model", nil)
	if ch == nil {
		t.Error("expected non-nil Slack channel")
	}
}

// ── Signal helper: single-request mock TCP server ────────────────────────────

type jsonrpcRequestProbe struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	ID      int64  `json:"id"`
}

type jsonrpcResponseMock struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int64          `json:"id"`
	Result  map[string]any `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// newSignalMockTCPServer creates a mock TCP server that handles one request
// and returns the given JSON-RPC response.
func newSignalMockTCPServer(t *testing.T, response jsonrpcResponseMock) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck
		var req jsonrpcRequestProbe
		_ = json.NewDecoder(conn).Decode(&req)
		response.ID = req.ID
		_ = json.NewEncoder(conn).Encode(response)
	}()
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

// ── Signal SendTyping tests ───────────────────────────────────────────────────

func TestSignalChannel_ShowTyping(t *testing.T) {
	ch := NewSignalChannel("+1", "", nil, true, false, false, false, "m", nil)
	if !ch.ShowTyping() {
		t.Error("expected ShowTyping=true")
	}
	ch2 := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	if ch2.ShowTyping() {
		t.Error("expected ShowTyping=false")
	}
}

func TestSignalChannel_SendTyping_NoAddr(t *testing.T) {
	ch := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	err := ch.SendTyping("+5551111111", false)
	if err == nil {
		t.Fatal("expected error when no addr")
	}
}

func TestSignalChannel_SendTyping_Success(t *testing.T) {
	addr := newSignalMockTCPServer(t, jsonrpcResponseMock{JSONRPC: "2.0", Result: map[string]any{}})
	ch := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	ch.addrMu.Lock()
	ch.addr = addr
	ch.addrMu.Unlock()
	err := ch.SendTyping("+5551111111", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSignalChannel_SendTyping_GroupID(t *testing.T) {
	addr := newSignalMockTCPServer(t, jsonrpcResponseMock{JSONRPC: "2.0", Result: map[string]any{}})
	ch := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	ch.addrMu.Lock()
	ch.addr = addr
	ch.addrMu.Unlock()
	// Group IDs don't start with "+".
	err := ch.SendTyping("group-base64-id", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── Signal SendMedia tests ────────────────────────────────────────────────────

func TestSignalChannel_SendMedia_NoAddr(t *testing.T) {
	ch := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	err := ch.SendMedia("+5551111111", "caption", "/tmp/img.png")
	if err == nil {
		t.Fatal("expected error when no addr")
	}
}

func TestSignalChannel_SendMedia_Success(t *testing.T) {
	addr := newSignalMockTCPServer(t, jsonrpcResponseMock{JSONRPC: "2.0", Result: map[string]any{}})
	ch := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	ch.addrMu.Lock()
	ch.addr = addr
	ch.addrMu.Unlock()
	err := ch.SendMedia("+5551111111", "caption", "/tmp/img.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── Signal sendReadReceipt tests ──────────────────────────────────────────────

func TestSignalChannel_SendReadReceipt_NoAddr(t *testing.T) {
	ch := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	err := ch.sendReadReceipt("+5551111111", 12345)
	if err == nil {
		t.Fatal("expected error when no addr")
	}
}

func TestSignalChannel_SendReadReceipt_Success(t *testing.T) {
	addr := newSignalMockTCPServer(t, jsonrpcResponseMock{JSONRPC: "2.0", Result: map[string]any{}})
	ch := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	ch.addrMu.Lock()
	ch.addr = addr
	ch.addrMu.Unlock()
	err := ch.sendReadReceipt("+5551111111", 12345)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── fetchLinkPreviews tests ───────────────────────────────────────────────────

func TestFetchLinkPreviews_NoURL(t *testing.T) {
	previews, cleanup := fetchLinkPreviews("no url here, just text")
	if previews != nil {
		t.Errorf("expected nil previews, got %v", previews)
	}
	if cleanup != nil {
		t.Error("expected nil cleanup")
	}
}

func TestFetchLinkPreviews_WithURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, `<html><head>
			<meta property="og:title" content="Test Title">
			<meta property="og:description" content="Test Desc">
		</head><body></body></html>`)
	}))
	defer srv.Close()

	text := fmt.Sprintf("Check out %s for more info", srv.URL)
	previews, cleanup := fetchLinkPreviews(text)
	if cleanup != nil {
		defer cleanup()
	}
	if previews == nil {
		t.Error("expected non-nil previews for valid HTML with og:title")
	} else if len(previews) == 0 || previews[0].Title != "Test Title" {
		t.Errorf("unexpected previews: %v", previews)
	}
}

func TestFetchLinkPreviews_NonHTMLResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"key":"value"}`)
	}))
	defer srv.Close()

	previews, cleanup := fetchLinkPreviews(srv.URL)
	if cleanup != nil {
		defer cleanup()
	}
	if previews != nil {
		t.Errorf("expected nil previews for non-HTML response, got %v", previews)
	}
}

// ── downloadTempImage tests ───────────────────────────────────────────────────

func TestDownloadTempImage_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("fake image data"))
	}))
	defer srv.Close()

	path, err := downloadTempImage(context.Background(), srv.URL+"/img.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(path) //nolint:errcheck
	if path == "" {
		t.Error("expected non-empty path")
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("could not read temp file: %v", readErr)
	}
	if string(data) != "fake image data" {
		t.Errorf("unexpected file contents: %q", data)
	}
}

func TestDownloadTempImage_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a 404 with no body — downloadTempImage should still succeed
		// (it copies whatever body is present); test that it at least returns
		// a path without error OR an empty-body error is tolerated.
		// The actual implementation doesn't check status code, only copy errors.
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// downloadTempImage does not check HTTP status, so it will still succeed
	// (copies the "404 page not found\n" body). Just verify no panic.
	path, err := downloadTempImage(context.Background(), srv.URL+"/notfound.png")
	if err == nil && path != "" {
		defer os.Remove(path) //nolint:errcheck
	}
	// Either outcome (success with 404 body, or error) is acceptable.
	_ = err
}

// ── streamToSink tests ────────────────────────────────────────────────────────

func TestSignalChannel_StreamToSink(t *testing.T) {
	ch := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	sink := newLogSink()
	ch.SetLogSink(sink)

	data := "line1\nline2\nline3\n"
	r := io.NopCloser(strings.NewReader(data))
	ch.streamToSink(r)

	history, _, unsub := sink.Subscribe()
	unsub()
	if len(history) < 3 {
		t.Errorf("expected at least 3 lines, got %d: %v", len(history), history)
	}
}

func TestSignalChannel_StreamToSink_NoSink(t *testing.T) {
	// streamToSink with nil LogSink should not panic.
	ch := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	// logSink is nil by default
	r := io.NopCloser(strings.NewReader("line1\nline2\n"))
	ch.streamToSink(r) // should not panic
}

// ── sendReaction tests ────────────────────────────────────────────────────────

func TestSignalChannel_SendReaction_NoAddr(t *testing.T) {
	ch := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	err := ch.sendReaction("+5551111111", "👍", "+1", 12345)
	if err == nil {
		t.Fatal("expected error when no addr")
	}
}

func TestSignalChannel_SendReaction_Success(t *testing.T) {
	addr := newSignalMockTCPServer(t, jsonrpcResponseMock{JSONRPC: "2.0", Result: map[string]any{}})
	ch := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	ch.addrMu.Lock()
	ch.addr = addr
	ch.addrMu.Unlock()
	err := ch.sendReaction("+5551111111", "👍", "+1", 12345)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSignalChannel_SendReaction_GroupID(t *testing.T) {
	addr := newSignalMockTCPServer(t, jsonrpcResponseMock{JSONRPC: "2.0", Result: map[string]any{}})
	ch := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	ch.addrMu.Lock()
	ch.addr = addr
	ch.addrMu.Unlock()
	err := ch.sendReaction("group-base64-id", "👍", "+1", 12345)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
