package channels

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// SignalChannel connects to a signal-cli daemon via its native TCP JSON-RPC interface.
// If url is set, it connects to an existing external daemon. Otherwise it
// launches signal-cli automatically as a managed subprocess.
//
// External daemon (signal-cli must already be running):
//
//	channels:
//	  - type: signal
//	    phone: "+15551234567"
//	    url: "127.0.0.1:7583"
//	    allowFrom:
//	      - "+15559876543"
//
// Managed daemon (signal-cli is launched and managed automatically):
//
//	channels:
//	  - type: signal
//	    phone: "+15551234567"
//	    allowFrom:
//	      - "+15559876543"
type SignalChannel struct {
	phone     string // registered Signal account phone number
	initAddr  string // configured TCP address; empty → managed daemon mode
	allowFrom []string

	addrMu sync.RWMutex
	addr   string // current effective daemon address (set dynamically in managed mode)

	procMu      sync.RWMutex
	procPID     int       // PID of the managed daemon (0 when not running)
	procStarted time.Time // time the managed daemon was started

	handler   func(IncomingMessage)
	handlerMu sync.RWMutex
	stopOnce  sync.Once
	done      chan struct{}
	idSeq     atomic.Int64

	logSinkMu sync.RWMutex
	logSink   *LogSink
}

// reconnectDelay is the wait between reconnect attempts after a connection error.
// Exposed as a variable so tests can set it to zero for fast iteration.
var reconnectDelay = 2 * time.Second

// NewSignalChannel creates a SignalChannel.
// phone is the registered account phone number; addr is the optional TCP
// address of an existing signal-cli JSON-RPC daemon (e.g. "127.0.0.1:7583").
// When addr is empty, signal-cli is launched and managed automatically.
func NewSignalChannel(phone, addr string, allowFrom []string) *SignalChannel {
	return &SignalChannel{
		phone:     phone,
		initAddr:  addr,
		addr:      addr,
		allowFrom: allowFrom,
		done:      make(chan struct{}),
	}
}

// SetLogSink attaches a LogSink that receives stdout/stderr lines from the
// managed signal-cli subprocess. Called by the Manager before Start.
func (c *SignalChannel) SetLogSink(s *LogSink) {
	c.logSinkMu.Lock()
	c.logSink = s
	c.logSinkMu.Unlock()
}

// streamToSink reads lines from r and writes them to the channel's LogSink.
// Runs in its own goroutine; exits when r reaches EOF or an error occurs.
func (c *SignalChannel) streamToSink(r io.ReadCloser) {
	defer r.Close() //nolint:errcheck
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		c.logSinkMu.RLock()
		sink := c.logSink
		c.logSinkMu.RUnlock()
		if sink != nil {
			sink.Write(line)
		}
	}
}

// OnMessage registers a callback for incoming messages.
func (c *SignalChannel) OnMessage(fn func(IncomingMessage)) {
	c.handlerMu.Lock()
	defer c.handlerMu.Unlock()
	c.handler = fn
}

// Send sends a Signal message to a recipient phone number via JSON-RPC over TCP.
// recipient is a phone number in E.164 format (e.g. "+15551234567").
func (c *SignalChannel) Send(recipient, text string) error {
	c.addrMu.RLock()
	addr := c.addr
	c.addrMu.RUnlock()

	if addr == "" {
		return fmt.Errorf("signal: daemon not ready")
	}

	type sendParams struct {
		Recipient []string `json:"recipient"`
		Message   string   `json:"message"`
	}
	req := jsonrpcRequest[sendParams]{
		JSONRPC: "2.0",
		Method:  "send",
		Params:  sendParams{Recipient: []string{recipient}, Message: text},
		ID:      c.idSeq.Add(1),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("signal: marshal request: %w", err)
	}
	body = append(body, '\n')

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("signal: dial: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	if _, err := conn.Write(body); err != nil {
		return fmt.Errorf("signal: write: %w", err)
	}

	// Read response line.
	var resp jsonrpcResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("signal: read response: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("signal: rpc error: %d %s", resp.Error.Code, resp.Error.Message)
	}
	return nil
}

// Start connects to signal-cli and listens for incoming messages.
// If no daemon address was configured, signal-cli is launched automatically.
// Reconnects on connection loss until ctx is done or Stop is called.
func (c *SignalChannel) Start(ctx context.Context) error {
	if c.initAddr != "" {
		c.runLoop(ctx, c.initAddr)
		return nil
	}

	if c.phone == "" {
		slog.Warn("signal: no phone or daemon address configured; channel inactive")
		<-ctx.Done()
		return nil
	}

	return c.managedLoop(ctx)
}

// Stop disconnects and prevents reconnection.
func (c *SignalChannel) Stop() {
	c.stopOnce.Do(func() { close(c.done) })
}

// DaemonInfo returns info about the signal-cli daemon.
// For managed mode it includes the subprocess PID and start time.
// For external mode (url configured) it returns the configured address with PID=0.
// Returns nil only when in managed mode and the daemon is not currently running.
func (c *SignalChannel) DaemonInfo() *DaemonInfo {
	if c.initAddr != "" {
		// External daemon: show the configured address even though we don't own it.
		return &DaemonInfo{Addr: c.initAddr, External: true}
	}
	c.procMu.RLock()
	pid := c.procPID
	started := c.procStarted
	c.procMu.RUnlock()
	if pid == 0 {
		return nil
	}
	c.addrMu.RLock()
	addr := c.addr
	c.addrMu.RUnlock()
	return &DaemonInfo{PID: pid, Addr: addr, Started: started}
}

// managedLoop launches signal-cli as a subprocess, runs the connect loop,
// and restarts the daemon if it exits unexpectedly.
func (c *SignalChannel) managedLoop(ctx context.Context) error {
	for {
		addr, cmd, err := c.launchDaemon(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-c.done:
				return nil
			default:
			}
			return fmt.Errorf("signal: launch daemon: %w", err)
		}

		c.addrMu.Lock()
		c.addr = addr
		c.addrMu.Unlock()

		c.procMu.Lock()
		c.procPID = cmd.Process.Pid
		c.procStarted = time.Now()
		c.procMu.Unlock()

		slog.Info("signal: managed daemon ready", "addr", addr, "phone", c.phone)
		c.runLoop(ctx, addr)

		// Ensure the process is stopped before waiting.
		cmd.Process.Kill() //nolint:errcheck
		cmd.Wait()         //nolint:errcheck

		c.addrMu.Lock()
		c.addr = ""
		c.addrMu.Unlock()

		c.procMu.Lock()
		c.procPID = 0
		c.procMu.Unlock()

		select {
		case <-ctx.Done():
			return nil
		case <-c.done:
			return nil
		default:
			slog.Warn("signal: managed daemon exited, restarting", "phone", c.phone)
		}
	}
}

// launchDaemon starts signal-cli daemon --tcp on a free local port and polls
// until it accepts TCP connections, then returns the address and the running Cmd.
func (c *SignalChannel) launchDaemon(ctx context.Context) (string, *exec.Cmd, error) {
	// Ask the OS for a free port, then release it for signal-cli to bind.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("find free port: %w", err)
	}
	addr := ln.Addr().String()
	ln.Close() //nolint:errcheck

	// exec.CommandContext kills the process when ctx is cancelled.
	cmd := exec.CommandContext(ctx, "signal-cli", "--account", c.phone, "daemon", "--tcp", addr)

	// Capture stdout+stderr so the output is visible in the daemon log view.
	pr, pw, pipeErr := os.Pipe()
	if pipeErr == nil {
		cmd.Stdout = pw
		cmd.Stderr = pw
	}

	if err := cmd.Start(); err != nil {
		if pipeErr == nil {
			pr.Close() //nolint:errcheck
			pw.Close() //nolint:errcheck
		}
		return "", nil, fmt.Errorf("start signal-cli: %w", err)
	}
	if pipeErr == nil {
		_ = pw.Close()        // parent only needs the read end
		go c.streamToSink(pr) // goroutine exits when process closes its write end
	}
	slog.Info("signal: started managed daemon", "addr", addr, "phone", c.phone, "pid", cmd.Process.Pid)

	// Poll until the TCP server accepts connections (up to 30 s).
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			cmd.Process.Kill() //nolint:errcheck
			cmd.Wait()         //nolint:errcheck
			return "", nil, ctx.Err()
		case <-c.done:
			cmd.Process.Kill() //nolint:errcheck
			cmd.Wait()         //nolint:errcheck
			return "", nil, fmt.Errorf("stopped")
		default:
		}
		if conn, dialErr := net.DialTimeout("tcp", addr, 200*time.Millisecond); dialErr == nil {
			conn.Close() //nolint:errcheck
			return addr, cmd, nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	cmd.Process.Kill() //nolint:errcheck
	cmd.Wait()         //nolint:errcheck
	return "", nil, fmt.Errorf("daemon did not become ready within 30s")
}

// runLoop runs the reconnect loop against a known daemon address.
func (c *SignalChannel) runLoop(ctx context.Context, addr string) {
	for {
		if err := c.listen(ctx, addr); err != nil {
			slog.Warn("signal: connection lost, retrying", "addr", addr, "err", err, "delay", reconnectDelay)
			select {
			case <-ctx.Done():
				return
			case <-c.done:
				return
			case <-time.After(reconnectDelay):
				// reconnect after delay
			}
		} else {
			select {
			case <-ctx.Done():
				return
			case <-c.done:
				return
			default:
				// reconnect immediately after clean close (daemon restarted, etc.)
			}
		}
	}
}

// listen opens a TCP connection to signal-cli and reads incoming
// JSON-RPC notifications until the connection closes, ctx is done, or Stop is called.
func (c *SignalChannel) listen(ctx context.Context, addr string) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	slog.Info("signal: connected", "addr", addr, "phone", c.phone)

	// Close the connection when context is cancelled or Stop is called.
	go func() {
		select {
		case <-ctx.Done():
		case <-c.done:
		}
		conn.Close() //nolint:errcheck
	}()

	// Read newline-delimited JSON-RPC messages.
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		case <-c.done:
			return nil
		default:
		}
		c.dispatch(scanner.Bytes())
	}

	if err := scanner.Err(); err != nil {
		select {
		case <-ctx.Done():
			return nil
		case <-c.done:
			return nil
		default:
			return fmt.Errorf("read: %w", err)
		}
	}
	return nil
}

type jsonrpcRequest[T any] struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  T      `json:"params,omitempty"`
	ID      int64  `json:"id"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *jsonrpcError   `json:"error"`
}

// jsonrpcNotification is the top-level shape of a signal-cli JSON-RPC push notification.
type jsonrpcNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// receiveParams is the params block of a "receive" notification.
type receiveParams struct {
	Envelope struct {
		Source      string `json:"source"`
		DataMessage *struct {
			Message string `json:"message"`
		} `json:"dataMessage"`
	} `json:"envelope"`
}

func (c *SignalChannel) dispatch(line []byte) {
	var notif jsonrpcNotification
	if err := json.Unmarshal(line, &notif); err != nil {
		slog.Debug("signal: parse error", "err", err)
		return
	}
	if notif.Method != "receive" {
		return
	}

	var p receiveParams
	if err := json.Unmarshal(notif.Params, &p); err != nil {
		slog.Debug("signal: parse receive params", "err", err)
		return
	}

	c.dispatchEnvelope(p.Envelope.Source, p.Envelope.DataMessage)
}

func (c *SignalChannel) dispatchEnvelope(source string, dataMessage *struct{ Message string "json:\"message\"" }) {
	if dataMessage == nil || dataMessage.Message == "" {
		return
	}
	if !c.allowed(source) {
		return
	}

	c.handlerMu.RLock()
	fn := c.handler
	c.handlerMu.RUnlock()

	if fn != nil {
		// Signal uses phone numbers as the channel/conversation identifier.
		fn(IncomingMessage{
			From:    source,
			Channel: source,
			Text:    dataMessage.Message,
		})
	} else {
		slog.Debug("signal: no handler registered", "from", source)
	}
}

func (c *SignalChannel) allowed(phone string) bool {
	if len(c.allowFrom) == 0 {
		return false
	}
	for _, a := range c.allowFrom {
		if a == "*" || a == phone {
			return true
		}
	}
	return false
}
