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

// daemonHub is a process-level registry of managed signal-cli subprocesses.
// At most one daemon runs per phone number; multiple SignalChannels sharing
// the same phone number subscribe to the same daemon.
type daemonHub struct {
	mu      sync.Mutex
	daemons map[string]*sharedDaemon
}

var globalDaemonHub = &daemonHub{daemons: map[string]*sharedDaemon{}}

// acquire returns the sharedDaemon for phone, creating it if necessary, and
// increments its reference count. Callers must call release when done.
func (h *daemonHub) acquire(phone string) *sharedDaemon {
	h.mu.Lock()
	defer h.mu.Unlock()
	d, ok := h.daemons[phone]
	if !ok {
		d = &sharedDaemon{phone: phone}
		h.daemons[phone] = d
	}
	d.refcount++
	return d
}

// release decrements the reference count for phone and stops the daemon
// when no subscribers remain.
func (h *daemonHub) release(phone string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	d, ok := h.daemons[phone]
	if !ok {
		return
	}
	d.refcount--
	if d.refcount <= 0 {
		if d.cancel != nil {
			d.cancel()
		}
		delete(h.daemons, phone)
	}
}

// sharedDaemon manages one signal-cli subprocess and fans incoming messages
// out to all registered subscriber SignalChannels for the same phone number.
type sharedDaemon struct {
	phone string

	addrMu sync.RWMutex
	addr   string

	procMu      sync.RWMutex
	procPID     int
	procStarted time.Time

	subsMu sync.RWMutex
	subs   []*SignalChannel

	once     sync.Once // starts the daemon goroutine exactly once
	cancel   context.CancelFunc
	refcount int // guarded by hub.mu

	idSeq atomic.Int64
}

func (d *sharedDaemon) addSub(ch *SignalChannel) {
	d.subsMu.Lock()
	d.subs = append(d.subs, ch)
	d.subsMu.Unlock()
}

func (d *sharedDaemon) removeSub(ch *SignalChannel) {
	d.subsMu.Lock()
	filtered := d.subs[:0]
	for _, s := range d.subs {
		if s != ch {
			filtered = append(filtered, s)
		}
	}
	d.subs = filtered
	d.subsMu.Unlock()
}

func (d *sharedDaemon) getAddr() string {
	d.addrMu.RLock()
	defer d.addrMu.RUnlock()
	return d.addr
}

// writeToSinks broadcasts a log line to all subscribers' log sinks.
func (d *sharedDaemon) writeToSinks(line string) {
	d.subsMu.RLock()
	subs := append([]*SignalChannel(nil), d.subs...)
	d.subsMu.RUnlock()
	for _, sub := range subs {
		sub.logSinkMu.RLock()
		sink := sub.logSink
		sub.logSinkMu.RUnlock()
		if sink != nil {
			sink.Write(line)
		}
	}
}

// streamToSinks reads lines from r and writes them to all subscribers' log sinks.
func (d *sharedDaemon) streamToSinks(r io.ReadCloser) {
	defer r.Close() //nolint:errcheck
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		d.writeToSinks(scanner.Text())
	}
}

// fanOut dispatches a raw JSON-RPC line to all subscriber channels.
func (d *sharedDaemon) fanOut(line []byte) {
	d.subsMu.RLock()
	subs := append([]*SignalChannel(nil), d.subs...)
	d.subsMu.RUnlock()
	for _, sub := range subs {
		sub.dispatch(line)
	}
}

// run is the managed daemon supervisor loop. It launches signal-cli, runs the
// listen loop, and restarts the daemon if it exits unexpectedly.
func (d *sharedDaemon) run(ctx context.Context) {
	for {
		addr, cmd, err := d.launchDaemon(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				slog.Error("signal: failed to launch managed daemon", "phone", d.phone, "err", err)
				return
			}
		}

		d.addrMu.Lock()
		d.addr = addr
		d.addrMu.Unlock()

		d.procMu.Lock()
		d.procPID = cmd.Process.Pid
		d.procStarted = time.Now()
		d.procMu.Unlock()

		slog.Info("signal: managed daemon ready", "addr", addr, "phone", d.phone)
		d.runLoop(ctx, addr)

		cmd.Process.Kill() //nolint:errcheck
		cmd.Wait()         //nolint:errcheck

		d.addrMu.Lock()
		d.addr = ""
		d.addrMu.Unlock()

		d.procMu.Lock()
		d.procPID = 0
		d.procMu.Unlock()

		select {
		case <-ctx.Done():
			return
		default:
			slog.Warn("signal: managed daemon exited, restarting", "phone", d.phone)
		}
	}
}

// launchDaemon starts signal-cli as a subprocess, waits for it to accept TCP
// connections, and returns the address and running Cmd.
func (d *sharedDaemon) launchDaemon(ctx context.Context) (string, *exec.Cmd, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("find free port: %w", err)
	}
	addr := ln.Addr().String()
	ln.Close() //nolint:errcheck

	cmd := exec.CommandContext(ctx, "signal-cli", "--account", d.phone, "daemon", "--tcp", addr)

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
		_ = pw.Close()
		go d.streamToSinks(pr)
	}
	slog.Info("signal: started managed daemon", "addr", addr, "phone", d.phone, "pid", cmd.Process.Pid)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			cmd.Process.Kill() //nolint:errcheck
			cmd.Wait()         //nolint:errcheck
			return "", nil, ctx.Err()
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

// runLoop reconnects to the daemon address until ctx is done.
func (d *sharedDaemon) runLoop(ctx context.Context, addr string) {
	for {
		if err := d.listen(ctx, addr); err != nil {
			slog.Warn("signal: connection lost, retrying", "addr", addr, "err", err, "delay", reconnectDelay)
			select {
			case <-ctx.Done():
				return
			case <-time.After(reconnectDelay):
			}
		} else {
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}
}

// listen opens a persistent TCP connection to signal-cli and fans incoming
// JSON-RPC notifications out to all subscriber channels.
func (d *sharedDaemon) listen(ctx context.Context, addr string) error {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	slog.Info("signal: shared daemon connected", "addr", addr, "phone", d.phone)

	// Enable link previews for the account once connected.
	go func() {
		time.Sleep(500 * time.Millisecond)
		type configParams struct {
			LinkPreviews bool `json:"linkPreviews"`
		}
		req := jsonrpcRequest[configParams]{
			JSONRPC: "2.0",
			Method:  "updateConfiguration",
			Params:  configParams{LinkPreviews: true},
			ID:      d.idSeq.Add(1),
		}
		if body, err := json.Marshal(req); err == nil {
			body = append(body, '\n')
			_, _ = conn.Write(body)
		}
	}()

	go func() {
		<-ctx.Done()
		conn.Close() //nolint:errcheck
	}()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		d.fanOut(scanner.Bytes())
	}

	if err := scanner.Err(); err != nil {
		select {
		case <-ctx.Done():
			return nil
		default:
			return fmt.Errorf("read: %w", err)
		}
	}
	return nil
}
