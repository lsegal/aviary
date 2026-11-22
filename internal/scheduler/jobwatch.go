package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
)

// jobStalledError ends a run whose agent went quiet for longer than the
// task's idle timeout.
type jobStalledError struct {
	idle    time.Duration
	started bool
}

func (e *jobStalledError) Error() string {
	if !e.started {
		return fmt.Sprintf("agent did not start: no output received within %s", e.idle)
	}
	return fmt.Sprintf("run timed out: no output received for %s", e.idle)
}

// taskIdleTimeout returns the idle timeout of the task that job runs. Zero
// disables the watchdog.
func taskIdleTimeout(job *domain.Job, cfg *config.AgentConfig) time.Duration {
	if cfg != nil {
		name := jobSessionName(job)
		for _, task := range cfg.Tasks {
			if task.Name == name {
				return task.EffectiveIdleTimeout()
			}
		}
	}
	return config.DefaultTaskIdleTimeout
}

// runPromptWatched starts a run with prompt and waits for it to finish,
// passing each of its stream events to handle. A run that emits no event for
// idle is canceled and ends with a *jobStalledError. handle is never called
// after runPromptWatched returns, so the caller may read what it collected.
func runPromptWatched(ctx context.Context, idle time.Duration, prompt func(context.Context, agent.StreamConsumer), handle agent.StreamConsumer) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
	closed := false
	defer func() {
		mu.Lock()
		closed = true
		mu.Unlock()
	}()

	activity := make(chan struct{}, 1)
	done := make(chan struct{}, 1)
	prompt(runCtx, func(e agent.StreamEvent) {
		mu.Lock()
		defer mu.Unlock()
		if closed {
			return
		}
		handle(e)
		signal := activity
		switch e.Type {
		case agent.StreamEventDone, agent.StreamEventStop, agent.StreamEventError:
			signal = done
		}
		select {
		case signal <- struct{}{}:
		default:
		}
	})

	var timeout <-chan time.Time
	var timer *time.Timer
	if idle > 0 {
		timer = time.NewTimer(idle)
		defer timer.Stop()
		timeout = timer.C
	}
	started := false
	for {
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-activity:
			started = true
			if timer != nil {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(idle)
			}
		case <-timeout:
			cancel()
			return &jobStalledError{idle: idle, started: started}
		}
	}
}

// maxLiveChunk is the longest partial line held back from the live output
// feed while waiting for its newline.
const maxLiveChunk = 400

// liveJobOutput streams a running job's output to the server log, where the
// run details panel's LIVE OUTPUT pane reads it by job_id. Text arrives in
// small fragments, so it is published a line at a time.
type liveJobOutput struct {
	jobID   string
	agentID string
	pending strings.Builder
}

func (l *liveJobOutput) emit(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	slog.Info("job output", "job_id", l.jobID, "agent", l.agentID, "chunk", line)
}

// Text adds a fragment of the agent's reply.
func (l *liveJobOutput) Text(text string) {
	l.pending.WriteString(text)
	buf := l.pending.String()
	for {
		i := strings.IndexByte(buf, '\n')
		if i < 0 {
			break
		}
		l.emit(strings.TrimRight(buf[:i], "\r"))
		buf = buf[i+1:]
	}
	if len(buf) > maxLiveChunk {
		l.emit(buf)
		buf = ""
	}
	l.pending.Reset()
	l.pending.WriteString(buf)
}

// Line publishes a complete line, after any reply text held back.
func (l *liveJobOutput) Line(format string, args ...any) {
	l.Flush()
	l.emit(fmt.Sprintf(format, args...))
}

// Flush publishes reply text held back waiting for its newline.
func (l *liveJobOutput) Flush() {
	l.emit(l.pending.String())
	l.pending.Reset()
}
