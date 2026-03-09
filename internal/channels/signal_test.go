package channels

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

func init() {
	// Speed up reconnect delay for tests.
	reconnectDelay = 0
}

// ── helpers ──────────────────────────────────────────────────────────────────

// fakeDaemon is a minimal in-process signal-cli TCP JSON-RPC server.
// It accepts TCP connections and handles newline-delimited JSON-RPC messages.
type fakeDaemon struct {
	listener net.Listener
	mu       sync.Mutex
	conns    []net.Conn
	sent     []map[string]interface{} // captured send requests
	stopCh   chan struct{}
}

func newFakeDaemon(t *testing.T) *fakeDaemon {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fd := &fakeDaemon{
		listener: ln,
		stopCh:   make(chan struct{}),
	}
	go fd.acceptLoop()
	return fd
}

// Addr returns host:port, matching what SignalChannel expects.
func (fd *fakeDaemon) Addr() string {
	return fd.listener.Addr().String()
}

// acceptLoop accepts incoming connections and handles them.
func (fd *fakeDaemon) acceptLoop() {
	for {
		select {
		case <-fd.stopCh:
			return
		default:
		}

		conn, err := fd.listener.Accept()
		if err != nil {
			return
		}

		fd.mu.Lock()
		fd.conns = append(fd.conns, conn)
		fd.mu.Unlock()

		go fd.handleConn(conn)
	}
}

// handleConn reads JSON-RPC requests from a connection and responds.
func (fd *fakeDaemon) handleConn(conn net.Conn) {
	defer conn.Close() //nolint:errcheck
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var req map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}

		method, _ := req["method"].(string)
		reqID := req["id"]

		// Capture send requests.
		if method == "send" {
			fd.mu.Lock()
			fd.sent = append(fd.sent, req)
			fd.mu.Unlock()
		}

		// Send response.
		resp := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqID,
			"result":  map[string]interface{}{},
		}
		b, _ := json.Marshal(resp)
		b = append(b, '\n')
		_, _ = conn.Write(b)
	}
}

// Push sends a raw JSON-RPC notification to all connected clients.
func (fd *fakeDaemon) Push(msg []byte) {
	if msg[len(msg)-1] != '\n' {
		msg = append(msg, '\n')
	}
	fd.mu.Lock()
	conns := append([]net.Conn(nil), fd.conns...)
	fd.mu.Unlock()
	for _, c := range conns {
		_, _ = c.Write(msg)
	}
}

// PushNotification sends a well-formed "receive" notification.
func (fd *fakeDaemon) PushNotification(from, message string) {
	type dataMessage struct {
		Message string `json:"message"`
	}
	type envelope struct {
		Source      string      `json:"source"`
		DataMessage dataMessage `json:"dataMessage"`
	}
	type params struct {
		Envelope envelope `json:"envelope"`
	}
	notif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "receive",
		"params":  params{Envelope: envelope{Source: from, DataMessage: dataMessage{Message: message}}},
	}
	b, _ := json.Marshal(notif)
	fd.Push(b)
}

// SentRequests returns a copy of all captured RPC send requests.
func (fd *fakeDaemon) SentRequests() []map[string]interface{} {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	return append([]map[string]interface{}(nil), fd.sent...)
}

// Close closes all connections and the listener.
func (fd *fakeDaemon) Close() {
	close(fd.stopCh)
	fd.mu.Lock()
	for _, c := range fd.conns {
		c.Close() //nolint:errcheck
	}
	fd.mu.Unlock()
	fd.listener.Close() //nolint:errcheck
}

// startChannel starts a SignalChannel and returns a cancel func + error channel.
func startChannel(ch *SignalChannel) (context.CancelFunc, <-chan error) {
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- ch.Start(ctx) }()
	return cancel, errCh
}

// waitMsg waits up to timeout for a message, or fails the test.
func waitMsg(t *testing.T, msgs <-chan IncomingMessage, timeout time.Duration) IncomingMessage {
	t.Helper()
	select {
	case m := <-msgs:
		return m
	case <-time.After(timeout):
		t.Fatal("timed out waiting for message")
		return IncomingMessage{}
	}
}

// waitConnected polls until the fake daemon has at least one connection.
func waitConnected(t *testing.T, fd *fakeDaemon, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		fd.mu.Lock()
		n := len(fd.conns)
		fd.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for TCP connection")
}

// ── dispatch tests ────────────────────────────────────────────────────────────

func TestDispatch_ValidReceive(t *testing.T) {
	ch := NewSignalChannel("", "", []string{"*"})
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	line := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","dataMessage":{"message":"hello"}}}}`
	ch.dispatch([]byte(line))

	msg := waitMsg(t, msgs, time.Second)
	if msg.From != "+15550001111" {
		t.Errorf("From = %q, want +15550001111", msg.From)
	}
	if msg.Text != "hello" {
		t.Errorf("Text = %q, want hello", msg.Text)
	}
	if msg.Channel != "+15550001111" {
		t.Errorf("Channel = %q, want +15550001111", msg.Channel)
	}
}

func TestDispatch_NonReceiveMethodIgnored(t *testing.T) {
	ch := NewSignalChannel("", "", []string{"*"})
	called := false
	ch.OnMessage(func(_ IncomingMessage) { called = true })

	ch.dispatch([]byte(`{"jsonrpc":"2.0","method":"syncMessage","params":{}}`))
	if called {
		t.Error("handler called for non-receive method")
	}
}

func TestDispatch_EmptyMessageIgnored(t *testing.T) {
	ch := NewSignalChannel("", "", []string{"*"})
	called := false
	ch.OnMessage(func(_ IncomingMessage) { called = true })

	line := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+1","dataMessage":{"message":""}}}}`
	ch.dispatch([]byte(line))
	if called {
		t.Error("handler called for empty message")
	}
}

func TestDispatch_NilDataMessageIgnored(t *testing.T) {
	ch := NewSignalChannel("", "", []string{"*"})
	called := false
	ch.OnMessage(func(_ IncomingMessage) { called = true })

	line := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+1"}}}`
	ch.dispatch([]byte(line))
	if called {
		t.Error("handler called when dataMessage is absent")
	}
}

func TestDispatch_MalformedJSON(t *testing.T) {
	ch := NewSignalChannel("", "", []string{"*"})
	called := false
	ch.OnMessage(func(_ IncomingMessage) { called = true })

	ch.dispatch([]byte(`{not valid json`))
	if called {
		t.Error("handler called for malformed JSON")
	}
}

func TestDispatch_NoHandlerRegistered(_ *testing.T) {
	ch := NewSignalChannel("", "", []string{"*"})
	// No handler — should not panic.
	line := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+1","dataMessage":{"message":"hi"}}}}`
	ch.dispatch([]byte(line))
}

// ── allowed tests ─────────────────────────────────────────────────────────────

func TestAllowed(t *testing.T) {
	tests := []struct {
		name      string
		allowFrom []string
		phone     string
		want      bool
	}{
		{"empty allowFrom blocks all", nil, "+1", false},
		{"wildcard allows all", []string{"*"}, "+1", true},
		{"exact match", []string{"+15551111111"}, "+15551111111", true},
		{"no match", []string{"+15551111111"}, "+15559999999", false},
		{"multiple entries, match second", []string{"+15551111111", "+15552222222"}, "+15552222222", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := NewSignalChannel("", "", tc.allowFrom)
			if got := ch.allowed(tc.phone); got != tc.want {
				t.Errorf("allowed(%q) = %v, want %v", tc.phone, got, tc.want)
			}
		})
	}
}

// ── DaemonInfo tests ──────────────────────────────────────────────────────────

func TestDaemonInfo_ExternalMode(t *testing.T) {
	ch := NewSignalChannel("+1", "127.0.0.1:7583", nil)
	info := ch.DaemonInfo()
	if info == nil {
		t.Fatal("DaemonInfo returned nil for external mode")
	}
	if info.Addr != "127.0.0.1:7583" {
		t.Errorf("Addr = %q, want 127.0.0.1:7583", info.Addr)
	}
	if !info.External {
		t.Error("External should be true for external mode")
	}
	if info.PID != 0 {
		t.Errorf("PID = %d, want 0 for external mode", info.PID)
	}
}

func TestDaemonInfo_ManagedMode_NotRunning(t *testing.T) {
	ch := NewSignalChannel("+1", "", nil)
	if info := ch.DaemonInfo(); info != nil {
		t.Errorf("DaemonInfo = %+v, want nil when daemon not running", info)
	}
}

func TestDaemonInfo_ManagedMode_Running(t *testing.T) {
	ch := NewSignalChannel("+1", "", nil)
	ch.procMu.Lock()
	ch.procPID = 1234
	ch.procStarted = time.Now()
	ch.procMu.Unlock()
	ch.addrMu.Lock()
	ch.addr = "127.0.0.1:9999"
	ch.addrMu.Unlock()

	info := ch.DaemonInfo()
	if info == nil {
		t.Fatal("DaemonInfo returned nil for running managed daemon")
	}
	if info.PID != 1234 {
		t.Errorf("PID = %d, want 1234", info.PID)
	}
	if info.Addr != "127.0.0.1:9999" {
		t.Errorf("Addr = %q, want 127.0.0.1:9999", info.Addr)
	}
	if info.External {
		t.Error("External should be false for managed mode")
	}
}

// ── Send tests (HTTP POST) ────────────────────────────────────────────────────

func TestSend_PostsJSONRPCOverTCP(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	ch := NewSignalChannel("", fd.Addr(), nil)
	if err := ch.Send("+15550001111", "test message"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	reqs := fd.SentRequests()
	if len(reqs) != 1 {
		t.Fatalf("got %d RPC requests, want 1", len(reqs))
	}
	req := reqs[0]
	if req["method"] != "send" {
		t.Errorf("method = %q, want send", req["method"])
	}
	if req["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", req["jsonrpc"])
	}
	params, _ := req["params"].(map[string]interface{})
	if params == nil {
		t.Fatal("params missing")
	}
	recipients, _ := params["recipient"].([]interface{})
	if len(recipients) != 1 || recipients[0] != "+15550001111" {
		t.Errorf("recipient = %v, want [\"+15550001111\"]", recipients)
	}
	if params["message"] != "test message" {
		t.Errorf("message = %q, want 'test message'", params["message"])
	}
}

func TestSend_DaemonNotReady(t *testing.T) {
	ch := NewSignalChannel("", "", nil)
	if err := ch.Send("+1", "hi"); err == nil {
		t.Error("expected error when daemon not ready, got nil")
	}
}

// ── Start / Stop (external mode, TCP JSON-RPC) ───────────────────────────────

func TestStart_ExternalMode_ReceivesMessages(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	ch := NewSignalChannel("", fd.Addr(), []string{"*"})
	msgs := make(chan IncomingMessage, 4)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	cancel, errCh := startChannel(ch)
	defer cancel()

	waitConnected(t, fd, 2*time.Second)
	fd.PushNotification("+15550001111", "hello from signal")

	msg := waitMsg(t, msgs, 2*time.Second)
	if msg.From != "+15550001111" {
		t.Errorf("From = %q, want +15550001111", msg.From)
	}
	if msg.Text != "hello from signal" {
		t.Errorf("Text = %q, want 'hello from signal'", msg.Text)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Start returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Start to return")
	}
}

func TestStart_ExternalMode_FiltersByAllowFrom(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	ch := NewSignalChannel("", fd.Addr(), []string{"+15550001111"})
	msgs := make(chan IncomingMessage, 4)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	cancel, _ := startChannel(ch)
	defer cancel()

	waitConnected(t, fd, 2*time.Second)

	// Blocked sender first, then allowed.
	fd.PushNotification("+19999999999", "should be blocked")
	fd.PushNotification("+15550001111", "should pass")

	msg := waitMsg(t, msgs, 2*time.Second)
	if msg.From != "+15550001111" {
		t.Errorf("received message from wrong sender: %q", msg.From)
	}

	select {
	case extra := <-msgs:
		t.Errorf("unexpected extra message: %+v", extra)
	default:
	}
}

func TestStop_StopsChannel(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	ch := NewSignalChannel("", fd.Addr(), []string{"*"})
	cancel, errCh := startChannel(ch)
	defer cancel()

	waitConnected(t, fd, 2*time.Second)
	ch.Stop()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Start returned error after Stop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out: Stop did not cause Start to return")
	}
}

func TestStart_ExternalMode_ReconnectsAfterDisconnect(t *testing.T) {
	fd := newFakeDaemon(t)

	ch := NewSignalChannel("", fd.Addr(), []string{"*"})
	msgs := make(chan IncomingMessage, 4)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	cancel, _ := startChannel(ch)
	defer cancel()

	waitConnected(t, fd, 2*time.Second)
	fd.PushNotification("+1", "first")
	waitMsg(t, msgs, 2*time.Second)

	// Close the server and bring up a new one at the same address.
	addr := fd.Addr()
	fd.Close()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Skipf("could not reuse address %s: %v", addr, err)
	}
	fd2 := &fakeDaemon{
		listener: ln,
		stopCh:   make(chan struct{}),
	}
	go fd2.acceptLoop()
	defer fd2.Close()

	// reconnectDelay is 0 — should reconnect quickly.
	waitConnected(t, fd2, 2*time.Second)
	fd2.PushNotification("+1", "after reconnect")

	msg := waitMsg(t, msgs, 2*time.Second)
	if msg.Text != "after reconnect" {
		t.Errorf("Text = %q, want 'after reconnect'", msg.Text)
	}
}

func TestStart_ExternalMode_MultipleMessages(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	ch := NewSignalChannel("", fd.Addr(), []string{"*"})
	msgs := make(chan IncomingMessage, 10)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	cancel, _ := startChannel(ch)
	defer cancel()

	waitConnected(t, fd, 2*time.Second)

	for i := range 5 {
		fd.PushNotification("+1", fmt.Sprintf("msg%d", i))
	}

	for i := range 5 {
		msg := waitMsg(t, msgs, 2*time.Second)
		want := fmt.Sprintf("msg%d", i)
		if msg.Text != want {
			t.Errorf("msg[%d].Text = %q, want %q", i, msg.Text, want)
		}
	}
}
