package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
)

// lockedBuffer is a bytes.Buffer safe for slog handlers written from
// several goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// captureJobOutput routes the default logger to a buffer for the test and
// returns a func reading the "chunk" of every "job output" entry for jobID.
func captureJobOutput(t *testing.T, jobID string) func() []string {
	t.Helper()
	buf := &lockedBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() []string {
		buf.mu.Lock()
		defer buf.mu.Unlock()
		var chunks []string
		for _, line := range strings.Split(buf.buf.String(), "\n") {
			var entry map[string]any
			if json.Unmarshal([]byte(line), &entry) != nil {
				continue
			}
			if entry["msg"] == "job output" && entry["job_id"] == jobID {
				chunks = append(chunks, entry["chunk"].(string))
			}
		}
		return chunks
	}
}

func TestRunPromptWatched_CompletesOnDone(t *testing.T) {
	var got []agent.StreamEventType
	err := runPromptWatched(context.Background(), time.Second, func(_ context.Context, consume agent.StreamConsumer) {
		go func() {
			consume(agent.StreamEvent{Type: agent.StreamEventText, Text: "hi"})
			consume(agent.StreamEvent{Type: agent.StreamEventDone})
		}()
	}, func(e agent.StreamEvent) { got = append(got, e.Type) })
	require.NoError(t, err)
	assert.Equal(t, []agent.StreamEventType{agent.StreamEventText, agent.StreamEventDone}, got)
}

func TestRunPromptWatched_AgentNeverStarts(t *testing.T) {
	var runCtx context.Context
	err := runPromptWatched(context.Background(), 50*time.Millisecond, func(ctx context.Context, _ agent.StreamConsumer) {
		runCtx = ctx
	}, func(agent.StreamEvent) {})
	var stalled *jobStalledError
	require.ErrorAs(t, err, &stalled)
	assert.False(t, stalled.started)
	assert.Contains(t, err.Error(), "agent did not start")
	assert.False(t, errors.Is(err, context.Canceled), "a stalled run must fail, not count as canceled")
	assert.ErrorIs(t, runCtx.Err(), context.Canceled, "the stalled run is canceled")
}

func TestRunPromptWatched_HangsAfterOutput(t *testing.T) {
	err := runPromptWatched(context.Background(), 50*time.Millisecond, func(_ context.Context, consume agent.StreamConsumer) {
		go consume(agent.StreamEvent{Type: agent.StreamEventText, Text: "working"})
	}, func(agent.StreamEvent) {})
	var stalled *jobStalledError
	require.ErrorAs(t, err, &stalled)
	assert.True(t, stalled.started)
	assert.Contains(t, err.Error(), "no output received for 50ms")
}

// fakeIdleTimer is an idleTimer driven by the test. advance moves its
// virtual clock and fires it once idle has passed since the last reset; every
// Reset is reported on resets so the test knows the watchdog saw an event.
type fakeIdleTimer struct {
	mu      sync.Mutex
	c       chan time.Time
	resets  chan struct{}
	idle    time.Duration
	elapsed time.Duration
}

func newFakeIdleTimer(idle time.Duration) *fakeIdleTimer {
	return &fakeIdleTimer{c: make(chan time.Time, 1), resets: make(chan struct{}, 1), idle: idle}
}

func (f *fakeIdleTimer) new(time.Duration) idleTimer { return f }

func (f *fakeIdleTimer) C() <-chan time.Time { return f.c }

func (f *fakeIdleTimer) Reset(d time.Duration) {
	f.mu.Lock()
	f.idle, f.elapsed = d, 0
	f.mu.Unlock()
	f.resets <- struct{}{}
}

func (f *fakeIdleTimer) Stop() {}

func (f *fakeIdleTimer) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.elapsed += d
	if f.elapsed >= f.idle {
		select {
		case f.c <- time.Time{}:
		default:
		}
	}
}

func TestRunPromptWatched_OutputKeepsRunAlive(t *testing.T) {
	const idle = 80 * time.Millisecond
	timer := newFakeIdleTimer(idle)
	err := runPromptWatchedTimer(context.Background(), idle, timer.new, func(_ context.Context, consume agent.StreamConsumer) {
		go func() {
			// 8 events 25ms apart span 200ms, well past the 80ms idle timeout.
			for range 8 {
				timer.advance(25 * time.Millisecond)
				consume(agent.StreamEvent{Type: agent.StreamEventStatus, Text: "tick"})
				<-timer.resets
			}
			consume(agent.StreamEvent{Type: agent.StreamEventDone})
		}()
	}, func(agent.StreamEvent) {})
	assert.NoError(t, err, "a run that keeps producing output outlives the idle timeout")
}

func TestRunPromptWatched_FakeTimerFiresWhenQuiet(t *testing.T) {
	timer := newFakeIdleTimer(80 * time.Millisecond)
	err := runPromptWatchedTimer(context.Background(), timer.idle, timer.new, func(_ context.Context, consume agent.StreamConsumer) {
		go func() {
			consume(agent.StreamEvent{Type: agent.StreamEventStatus, Text: "tick"})
			<-timer.resets
			timer.advance(80 * time.Millisecond)
		}()
	}, func(agent.StreamEvent) {})
	var stalled *jobStalledError
	require.ErrorAs(t, err, &stalled, "the same fake timer ends a run that goes quiet, so the test above is not vacuous")
	assert.True(t, stalled.started)
}

func TestRunPromptWatched_DisabledTimeoutWaitsForCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := runPromptWatched(ctx, 0, func(context.Context, agent.StreamConsumer) {}, func(agent.StreamEvent) {})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRunPromptWatched_NoEventsAfterReturn(t *testing.T) {
	var consume agent.StreamConsumer
	calls := 0
	err := runPromptWatched(context.Background(), 20*time.Millisecond, func(_ context.Context, c agent.StreamConsumer) {
		consume = c
	}, func(agent.StreamEvent) { calls++ })
	require.Error(t, err)
	consume(agent.StreamEvent{Type: agent.StreamEventText, Text: "late"})
	assert.Zero(t, calls)
}

func TestLiveJobOutput_StreamsLines(t *testing.T) {
	chunks := captureJobOutput(t, "job-1")
	live := &liveJobOutput{jobID: "job-1", agentID: "a"}
	live.Text("Hello, ")
	live.Text("world\nsecond ")
	assert.Equal(t, []string{"Hello, world"}, chunks())
	live.Line("status: %s", "reading files")
	live.Text(strings.Repeat("x", maxLiveChunk+1))
	live.Text("tail")
	live.Flush()
	assert.Equal(t, []string{
		"Hello, world",
		"second ",
		"status: reading files",
		strings.Repeat("x", maxLiveChunk+1),
		"tail",
	}, chunks())
}

func TestWorkerPool_ExecuteJob_StreamsLiveOutput(t *testing.T) {
	setupSchedulerDataDir(t)
	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "alpha", Model: "m"}}})
	p := NewWorkerPool(NewJobQueue(), mgr, 1)
	chunks := captureJobOutput(t, "job-live")

	err := p.executeJob(context.Background(), &domain.Job{ID: "job-live", TaskID: "alpha/t", AgentID: "alpha", Prompt: "hello"})
	require.NoError(t, err)
	got := chunks()
	require.NotEmpty(t, got, "the run's events reach the LIVE OUTPUT feed by job_id")
	assert.Contains(t, strings.Join(got, "\n"), "no LLM provider configured")
}

func TestTaskIdleTimeout(t *testing.T) {
	cfg := &config.AgentConfig{Tasks: []config.TaskConfig{
		{Name: "slow", IdleTimeout: "1h"},
		{Name: "off", IdleTimeout: "0"},
		{Name: "plain"},
	}}
	assert.Equal(t, time.Hour, taskIdleTimeout(&domain.Job{TaskID: "a/slow"}, cfg))
	assert.Zero(t, taskIdleTimeout(&domain.Job{TaskID: "a/off"}, cfg))
	assert.Equal(t, config.DefaultTaskIdleTimeout, taskIdleTimeout(&domain.Job{TaskID: "a/plain"}, cfg))
	assert.Equal(t, config.DefaultTaskIdleTimeout, taskIdleTimeout(&domain.Job{TaskID: "a/missing"}, nil))
}
