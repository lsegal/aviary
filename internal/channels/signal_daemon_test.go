package channels

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/lsegal/aviary/internal/config"
)

// signalDaemonHelperEnv selects the helper-process mode that stands in for
// signal-cli. The test binary re-executes itself with this variable set.
const signalDaemonHelperEnv = "AVIARY_SIGNAL_DAEMON_HELPER"

// TestSignalDaemonHelperProcess is not a real test: it runs only as a child
// process started by helperDaemonCommand. Mode "listen" accepts TCP
// connections on the address after "--" and writes a log line to stdout and
// stderr; mode "idle" does nothing. Both exit on their own after a while so a
// leaked child never outlives the test run.
func TestSignalDaemonHelperProcess(t *testing.T) {
	mode := os.Getenv(signalDaemonHelperEnv)
	if mode == "" {
		return
	}
	if mode == "listen" {
		args := os.Args
		for i, a := range args {
			if a == "--" {
				args = args[i+1:]
				break
			}
		}
		ln, err := net.Listen("tcp", args[len(args)-1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "helper listen:", err)
			os.Exit(2)
		}
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				conn.Close() //nolint:errcheck
			}
		}()
		fmt.Fprintln(os.Stdout, "helper stdout ready")
		fmt.Fprintln(os.Stderr, "helper stderr ready")
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

// helperDaemonCommand returns a signalCLICommand replacement that re-executes
// the test binary in the given helper mode instead of running signal-cli.
func helperDaemonCommand(mode string) func(ctx context.Context, phone, addr string) *exec.Cmd {
	return func(ctx context.Context, phone, addr string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSignalDaemonHelperProcess$", "--", phone, addr)
		cmd.Env = append(os.Environ(), signalDaemonHelperEnv+"="+mode)
		return cmd
	}
}

// stubSignalCLI swaps signalCLICommand and daemonReadyTimeout for the test.
func stubSignalCLI(t *testing.T, cmd func(ctx context.Context, phone, addr string) *exec.Cmd, timeout time.Duration) {
	t.Helper()
	prevCmd, prevTimeout := signalCLICommand, daemonReadyTimeout
	signalCLICommand, daemonReadyTimeout = cmd, timeout
	t.Cleanup(func() { signalCLICommand, daemonReadyTimeout = prevCmd, prevTimeout })
}

// startIdleHelper starts a helper process that does nothing until killed.
func startIdleHelper(t *testing.T, ctx context.Context) *exec.Cmd {
	t.Helper()
	cmd := helperDaemonCommand("idle")(ctx, "", "")
	assert.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

// sinkSubscriber returns a subscriber channel with its own log sink.
func sinkSubscriber() (*SignalChannel, *LogSink) {
	sink := newLogSink()
	ch := &SignalChannel{done: make(chan struct{})}
	ch.SetLogSink(sink)
	return ch, sink
}

// waitForLine waits until sink has recorded line.
func waitForLine(t *testing.T, sink *LogSink, line string) {
	t.Helper()
	assert.Eventually(t, func() bool {
		history, _, unsub := sink.Subscribe()
		unsub()
		for _, h := range history {
			if h == line {
				return true
			}
		}
		return false
	}, 10*time.Second, 10*time.Millisecond, "sink never received %q", line)
}

// countRequests reports how many requests fd has received for method.
func countRequests(fd *fakeDaemon, method string) int {
	n := 0
	for _, r := range fd.Requests() {
		if r["method"] == method {
			n++
		}
	}
	return n
}

// dropConns closes every connection fd has accepted, simulating signal-cli
// dropping its clients.
func (fd *fakeDaemon) dropConns() {
	fd.mu.Lock()
	conns := fd.conns
	fd.conns = nil
	fd.mu.Unlock()
	for _, c := range conns {
		c.Close() //nolint:errcheck
	}
}

// messageSubscriber returns a subscriber that accepts messages from anyone and
// reports their text on the returned channel.
func messageSubscriber(phone, addr string) (*SignalChannel, chan string) {
	ch := NewSignalChannel(phone, addr, []config.AllowFromEntry{{From: "*"}}, false, false, false, false, "m", nil)
	got := make(chan string, 8)
	ch.OnMessage(func(im IncomingMessage) { got <- im.Text })
	return ch, got
}

func receiveText(t *testing.T, got <-chan string) string {
	t.Helper()
	select {
	case text := <-got:
		return text
	case <-time.After(10 * time.Second):
		t.Fatal("subscriber received no message")
		return ""
	}
}

// ── writeToSinks / streamToSinks ─────────────────────────────────────────────

func TestSharedDaemon_WriteToSinks(t *testing.T) {
	d := &sharedDaemon{phone: "+1"}
	sub1, sink1 := sinkSubscriber()
	sub2, sink2 := sinkSubscriber()
	noSink := &SignalChannel{done: make(chan struct{})}
	d.addSub(sub1)
	d.addSub(noSink)
	d.addSub(sub2)

	d.writeToSinks("first")
	d.removeSub(sub2)
	d.writeToSinks("second")

	h1, _, unsub1 := sink1.Subscribe()
	unsub1()
	h2, _, unsub2 := sink2.Subscribe()
	unsub2()
	assert.Equal(t, []string{"first", "second"}, h1)
	assert.Equal(t, []string{"first"}, h2)
}

type trackingReadCloser struct {
	io.Reader
	closed atomic.Bool
}

func (r *trackingReadCloser) Close() error {
	r.closed.Store(true)
	return nil
}

func TestSharedDaemon_StreamToSinks(t *testing.T) {
	d := &sharedDaemon{phone: "+1"}
	sub, sink := sinkSubscriber()
	d.addSub(sub)
	d.addSub(&SignalChannel{done: make(chan struct{})})

	r := &trackingReadCloser{Reader: strings.NewReader("line one\nline two\n")}
	d.streamToSinks(r)

	history, _, unsub := sink.Subscribe()
	unsub()
	assert.Equal(t, []string{"line one", "line two"}, history)
	assert.True(t, r.closed.Load(), "streamToSinks must close its reader")
}

// ── fanOut ───────────────────────────────────────────────────────────────────

func TestSharedDaemon_FanOut(t *testing.T) {
	d := &sharedDaemon{phone: "+1"}
	sub1, got1 := messageSubscriber("+1", "")
	sub2, got2 := messageSubscriber("+1", "")
	d.addSub(sub1)
	d.addSub(sub2)

	d.fanOut([]byte(`{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+2","dataMessage":{"message":"hello all"}}}}`))

	assert.Contains(t, receiveText(t, got1), "hello all")
	assert.Contains(t, receiveText(t, got2), "hello all")
}

// ── listen ───────────────────────────────────────────────────────────────────

func TestSharedDaemon_ListenConfiguresAndFansOut(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()
	fd.SetResult("listAccounts", []map[string]string{{"number": "+1", "uuid": "uuid-1"}})

	d := &sharedDaemon{phone: "+1"}
	sub1, got1 := messageSubscriber("+1", fd.Addr())
	sub2, got2 := messageSubscriber("+1", fd.Addr())
	d.addSub(sub1)
	d.addSub(sub2)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.listen(ctx, fd.Addr()) }()

	// updateConfiguration arrives on the listen connection once it is open,
	// followed by one listAccounts lookup per subscriber.
	assert.Eventually(t, func() bool {
		return countRequests(fd, "updateConfiguration") == 1 && countRequests(fd, "listAccounts") == 2
	}, 10*time.Second, 10*time.Millisecond)
	for _, r := range fd.Requests() {
		if r["method"] == "updateConfiguration" {
			assert.Equal(t, map[string]any{"linkPreviews": true}, r["params"])
		}
	}

	fd.PushNotification("+2", "fan me out")
	assert.Contains(t, receiveText(t, got1), "fan me out")
	assert.Contains(t, receiveText(t, got2), "fan me out")

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err, "listen returns nil once its context is cancelled")
	case <-time.After(10 * time.Second):
		t.Fatal("listen did not return after context cancellation")
	}
}

func TestSharedDaemon_ListenDialError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	addr := ln.Addr().String()
	ln.Close() //nolint:errcheck

	d := &sharedDaemon{phone: "+1"}
	err = d.listen(context.Background(), addr)
	assert.ErrorContains(t, err, "dial:")
}

func TestSharedDaemon_ListenReturnsNilOnEOF(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	d := &sharedDaemon{phone: "+1"}
	done := make(chan error, 1)
	go func() { done <- d.listen(context.Background(), fd.Addr()) }()

	assert.Eventually(t, func() bool {
		fd.mu.Lock()
		defer fd.mu.Unlock()
		return len(fd.conns) == 1
	}, 10*time.Second, 10*time.Millisecond)
	fd.dropConns()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("listen did not return after the server closed the connection")
	}
}

func TestSharedDaemon_ListenReadError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	defer ln.Close() //nolint:errcheck
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck
		// A line longer than the scanner's 2 MiB limit is a read error.
		_, _ = conn.Write([]byte(strings.Repeat("x", 3*1024*1024)))
	}()

	d := &sharedDaemon{phone: "+1"}
	err = d.listen(context.Background(), ln.Addr().String())
	assert.ErrorContains(t, err, "read:")
}

// ── runLoop ──────────────────────────────────────────────────────────────────

func TestSharedDaemon_RunLoopReconnectsAfterDrop(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	d := &sharedDaemon{phone: "+1"}
	sub, got := messageSubscriber("+1", "")
	d.addSub(sub)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.runLoop(ctx, fd.Addr())
		close(done)
	}()

	assert.Eventually(t, func() bool { return countRequests(fd, "updateConfiguration") == 1 }, 10*time.Second, 10*time.Millisecond)
	fd.dropConns()
	assert.Eventually(t, func() bool { return countRequests(fd, "updateConfiguration") == 2 }, 10*time.Second, 10*time.Millisecond,
		"runLoop should reconnect after the daemon drops the connection")

	fd.PushNotification("+2", "after reconnect")
	assert.Contains(t, receiveText(t, got), "after reconnect")

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("runLoop did not return after context cancellation")
	}
}

func TestSharedDaemon_RunLoopRetriesDialErrorsUntilCancelled(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	addr := ln.Addr().String()
	ln.Close() //nolint:errcheck

	prev := reconnectDelay
	reconnectDelay = time.Hour
	t.Cleanup(func() { reconnectDelay = prev })

	d := &sharedDaemon{phone: "+1"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.runLoop(ctx, addr)
		close(done)
	}()

	// With an hour-long delay, only cancellation can end the retry wait.
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("runLoop did not stop waiting to reconnect after context cancellation")
	}
}

// ── run ──────────────────────────────────────────────────────────────────────

func TestSharedDaemon_RunRecordsAndClearsProcess(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var launches atomic.Int32
	cmds := make(chan *exec.Cmd, 1)
	d := &sharedDaemon{phone: "+1", launch: func(ctx context.Context, d *sharedDaemon) (string, *exec.Cmd, error) {
		launches.Add(1)
		cmd := startIdleHelper(t, ctx)
		cmds <- cmd
		return fd.Addr(), cmd, nil
	}}

	done := make(chan struct{})
	go func() {
		d.run(ctx)
		close(done)
	}()

	cmd := <-cmds
	assert.Eventually(t, func() bool { return countRequests(fd, "updateConfiguration") == 1 }, 10*time.Second, 10*time.Millisecond)
	assert.Equal(t, fd.Addr(), d.getAddr())
	d.procMu.RLock()
	pid, started := d.procPID, d.procStarted
	d.procMu.RUnlock()
	assert.Equal(t, cmd.Process.Pid, pid)
	assert.False(t, started.IsZero())

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after context cancellation")
	}

	assert.Equal(t, int32(1), launches.Load())
	assert.Equal(t, "", d.getAddr())
	d.procMu.RLock()
	assert.Equal(t, 0, d.procPID)
	d.procMu.RUnlock()
	assert.NotNil(t, cmd.ProcessState, "run must wait for the killed daemon process")
}

// ── defaultLaunchDaemon ──────────────────────────────────────────────────────

func TestDefaultLaunchDaemon_ReadyStreamsOutput(t *testing.T) {
	stubSignalCLI(t, helperDaemonCommand("listen"), 30*time.Second)

	d := &sharedDaemon{phone: "+15550001111"}
	sub, sink := sinkSubscriber()
	d.addSub(sub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr, cmd, err := defaultLaunchDaemon(ctx, d)
	assert.NoError(t, err)
	if cmd == nil {
		t.FailNow()
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	assert.True(t, strings.HasPrefix(addr, "127.0.0.1:"), "addr %q", addr)
	assert.Equal(t, []string{"--", "+15550001111", addr}, cmd.Args[len(cmd.Args)-3:])
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	assert.NoError(t, err, "daemon should be accepting connections once launch returns")
	if conn != nil {
		conn.Close() //nolint:errcheck
	}
	waitForLine(t, sink, "helper stdout ready")
	waitForLine(t, sink, "helper stderr ready")
}

func TestDefaultLaunchDaemon_StartError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-signal-cli")
	stubSignalCLI(t, func(ctx context.Context, _, _ string) *exec.Cmd {
		return exec.CommandContext(ctx, missing)
	}, 30*time.Second)

	addr, cmd, err := defaultLaunchDaemon(context.Background(), &sharedDaemon{phone: "+1"})
	assert.ErrorContains(t, err, "start signal-cli:")
	assert.Empty(t, addr)
	assert.Nil(t, cmd)
}

func TestDefaultLaunchDaemon_NotReadyTimeout(t *testing.T) {
	stubSignalCLI(t, helperDaemonCommand("idle"), 300*time.Millisecond)

	start := time.Now()
	addr, cmd, err := defaultLaunchDaemon(context.Background(), &sharedDaemon{phone: "+1"})
	assert.ErrorContains(t, err, "daemon did not become ready within 300ms")
	assert.Empty(t, addr)
	assert.Nil(t, cmd)
	assert.Less(t, time.Since(start), 20*time.Second)
}

func TestDefaultLaunchDaemon_ContextCancelled(t *testing.T) {
	stubSignalCLI(t, helperDaemonCommand("idle"), time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	addr, cmd, err := defaultLaunchDaemon(ctx, &sharedDaemon{phone: "+1"})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, addr)
	assert.Nil(t, cmd)
}
