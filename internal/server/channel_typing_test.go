package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/channels"
	"github.com/lsegal/aviary/internal/config"
)

// typingRecorder records the typing indicators a channel sends, in order.
type typingRecorder struct {
	stubChannel
	mu     sync.Mutex
	events []string
	// block, when set, holds every typing start until it is closed.
	block chan struct{}
	// failStops makes this many stops fail before one succeeds.
	failStops int
}

func (c *typingRecorder) ShowTyping() bool { return true }

func (c *typingRecorder) SendTyping(_ string, stop bool) error {
	c.mu.Lock()
	block := c.block
	c.mu.Unlock()
	if !stop && block != nil {
		<-block
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if stop && c.failStops > 0 {
		c.failStops--
		c.events = append(c.events, "stop-failed")
		return errors.New("daemon unavailable")
	}
	if stop {
		c.events = append(c.events, "stop")
	} else {
		c.events = append(c.events, "start")
	}
	return nil
}

func (c *typingRecorder) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.events...)
}

// stoppedOnce reports whether the indicator ended with exactly one stop and
// nothing after it.
func (c *typingRecorder) stoppedOnce() bool {
	events := c.snapshot()
	stops := 0
	for _, e := range events {
		if e == "stop" {
			stops++
		}
	}
	return stops == 1 && events[len(events)-1] == "stop"
}

func setTypingInterval(t *testing.T, d time.Duration) {
	t.Helper()
	prev := channelTypingInterval
	channelTypingInterval = d
	t.Cleanup(func() { channelTypingInterval = prev })
}

func TestChannelTyping_StopEndsKeepalive(t *testing.T) {
	setTypingInterval(t, 5*time.Millisecond)
	ch := &typingRecorder{}

	typing := startChannelTyping(context.Background(), ch, "+15550001")
	assert.Eventually(t, func() bool { return len(ch.snapshot()) >= 3 }, time.Second, time.Millisecond, "keepalive refreshes the indicator")
	typing.Stop()
	typing.Stop()

	assert.Eventually(t, ch.stoppedOnce, time.Second, time.Millisecond)
	time.Sleep(10 * channelTypingInterval)
	assert.True(t, ch.stoppedOnce(), "no typing start after the stop")
}

func TestChannelTyping_StopFollowsInFlightRefresh(t *testing.T) {
	setTypingInterval(t, 5*time.Millisecond)
	ch := &typingRecorder{}
	typing := startChannelTyping(context.Background(), ch, "+15550001")

	// Hold the next keepalive refresh in flight, then end the turn.
	ch.mu.Lock()
	ch.block = make(chan struct{})
	ch.mu.Unlock()
	time.Sleep(4 * channelTypingInterval)
	typing.Stop()
	time.Sleep(4 * channelTypingInterval)
	assert.NotContains(t, ch.snapshot(), "stop", "the stop waits for the refresh in flight")

	close(ch.block)
	assert.Eventually(t, ch.stoppedOnce, time.Second, time.Millisecond)
	events := ch.snapshot()
	require.GreaterOrEqual(t, len(events), 2)
	assert.Equal(t, "start", events[len(events)-2], "the in-flight refresh lands before the stop")
}

func TestChannelTyping_ContextCancelStops(t *testing.T) {
	setTypingInterval(t, time.Hour)
	ch := &typingRecorder{}
	ctx, cancel := context.WithCancel(context.Background())

	startChannelTyping(ctx, ch, "group-id")
	cancel()

	assert.Eventually(t, ch.stoppedOnce, time.Second, time.Millisecond)
	assert.Equal(t, []string{"start", "stop"}, ch.snapshot())
}

func TestChannelTyping_RetriesFailedStop(t *testing.T) {
	setTypingInterval(t, time.Hour)
	ch := &typingRecorder{failStops: 1}

	startChannelTyping(context.Background(), ch, "+15550001").Stop()

	assert.Eventually(t, ch.stoppedOnce, time.Second, time.Millisecond)
	assert.Equal(t, []string{"start", "stop-failed", "stop"}, ch.snapshot())
}

func TestHandleIncomingChannelMessage_StopsTypingWhenTurnEnds(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()

	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "stub"}}}
	srv := New(cfg, "tok")
	srv.agents.Reconcile(cfg)
	runner, ok := srv.agents.Get("bot")
	require.True(t, ok)
	t.Cleanup(runner.Wait)

	ch := &typingRecorder{}
	srv.handleIncomingChannelMessage(context.Background(), "bot", "signal", "dm", ch, channels.IncomingMessage{
		Type:    "signal",
		Channel: "+15550001",
		From:    "+15550001",
		Text:    "hello",
	})

	assert.Eventually(t, ch.stoppedOnce, time.Second, 10*time.Millisecond)
	assert.Equal(t, "start", ch.snapshot()[0])
}

type stoppableTypingChannel struct {
	stubChannel
	*statusStubChannel
	*typingRecorder
}

func TestHandleIncomingChannelMessage_StopsTypingWhenRunIsStopped(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()

	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "stub"}}}
	srv := New(cfg, "tok")
	srv.agents.Reconcile(cfg)
	runner, ok := srv.agents.Get("bot")
	require.True(t, ok)
	t.Cleanup(runner.Wait)

	typing := &typingRecorder{}
	ch := stoppableTypingChannel{statusStubChannel: &statusStubChannel{stopOnRegister: true}, typingRecorder: typing}
	srv.handleIncomingChannelMessage(context.Background(), "bot", "slack", "alerts", ch, channels.IncomingMessage{
		Type:     "slack",
		Channel:  "D123",
		ThreadTS: "1710000000.123456",
		From:     "U123",
		Text:     "hello",
	})

	assert.Eventually(t, typing.stoppedOnce, time.Second, 10*time.Millisecond)
}
