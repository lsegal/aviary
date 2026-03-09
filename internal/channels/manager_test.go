package channels

import (
	"context"
	"sync"
	"testing"
	"time"

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
