package projects

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/clidefs"
)

// The delay an agent asks for is clamped to the range Claude Code's own
// runtime allows, so aviary waits exactly as long as the agent would have.
// aviary runs agents in print mode, where the process exits as soon as the
// turn ends, so a wakeup never fires on its own; aviary keeps the run alive
// and fires it instead.
const (
	minWakeupDelay = time.Minute
	maxWakeupDelay = time.Hour
)

// maxConsecutiveWakeups caps how many wakeups in a row one run may schedule
// without doing anything else, so an agent that keeps deferring its work
// cannot hold its slot forever.
const maxConsecutiveWakeups = 48

// maxStalledKeepalives is how many keepalive resumes in a row may end with the
// issue still open and nothing new on GitHub before the job is paused for the
// owner instead of being resumed again.
const maxStalledKeepalives = 4

// maxKeepaliveDelay caps the keepalive backoff, which doubles from the closure
// interval with each turn in a row that made no progress.
const maxKeepaliveDelay = 15 * time.Minute

// runWait is a wait the run loop sits out before resuming its session: the
// agent's own scheduled wakeup, or the keepalive's backoff. For names it in the
// job's output.
type runWait struct {
	Delay  time.Duration
	Reason string
	For    string
}

// keepaliveDelay is how long the keepalive waits before resuming a turn that
// left the issue open: the closure interval, doubled for each turn in a row
// before it that made no progress, up to maxKeepaliveDelay.
func (w *Engine) keepaliveDelay(stalled int) time.Duration {
	delay := w.activeWorkClosureInterval()
	for range stalled {
		if delay >= maxKeepaliveDelay {
			break
		}
		delay *= 2
	}
	return min(delay, maxKeepaliveDelay)
}

// scheduledWakeup is a wakeup call left pending when the agent's
// process exited. It is returned as the run's error so every runner reports it
// through the interface it already has; the run loop takes it back out before
// the error is treated as a failure.
type scheduledWakeup struct {
	Delay  time.Duration
	Prompt string
	Reason string
}

func (w *scheduledWakeup) Error() string {
	return fmt.Sprintf("agent scheduled a wakeup in %s: %s", formatWakeupDelay(w.Delay), w.Reason)
}

// wakeupDetector reads the agent's raw event stream for the deferred-turn
// signal its definition declares. It sits beside the output decoder rather
// than inside it, so any CLI whose definition names a signal can defer a
// turn, whichever format its output is decoded as.
type wakeupDetector struct {
	*lineWriter
	config clidefs.Wakeup
	// wakeup is the last wakeup the agent called, and id the id it was
	// called with, so a call reported as failed is dropped again.
	wakeup *scheduledWakeup
	id     string
}

func newWakeupDetector(config clidefs.Wakeup) *wakeupDetector {
	d := &wakeupDetector{lineWriter: &lineWriter{}, config: config}
	d.line = d.scan
	return d
}

func (d *wakeupDetector) scan(line []byte) error {
	var event any
	if err := json.Unmarshal(bytes.TrimSpace(line), &event); err != nil {
		// A line that is not an event carries no signal.
		return nil
	}
	if d.isEvent(event, d.config.Event) {
		for _, call := range jsonlLookup(event, d.config.Calls) {
			if firstJSONLString(jsonlLookup(call, d.config.Name)) != d.config.Tool {
				continue
			}
			var input any
			if values := jsonlLookup(call, d.config.Input); len(values) > 0 {
				input = values[0]
			}
			d.wakeup = parseScheduledWakeup(d.config, input)
			d.id = ""
			if d.config.ID != "" {
				d.id = firstJSONLString(jsonlLookup(call, d.config.ID))
			}
		}
	}
	if rejected := d.config.Rejected; rejected != nil && d.id != "" && d.isEvent(event, rejected.Event) {
		for _, result := range jsonlLookup(event, rejected.Results) {
			if firstJSONLString(jsonlLookup(result, rejected.ID)) == d.id && jsonlTrue(jsonlLookup(result, rejected.Error)) {
				d.wakeup, d.id = nil, ""
			}
		}
	}
	return nil
}

// isEvent reports whether the event is of the named type; an empty name
// matches every event.
func (d *wakeupDetector) isEvent(event any, kind string) bool {
	if kind == "" {
		return true
	}
	for _, found := range jsonlStrings(event, d.config.Type) {
		if found == kind {
			return true
		}
	}
	return false
}

// PendingWakeup reports the wakeup the agent left pending when its stream
// ended, if any.
func (d *wakeupDetector) PendingWakeup() (scheduledWakeup, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.wakeup == nil {
		return scheduledWakeup{}, false
	}
	return *d.wakeup, true
}

// parseScheduledWakeup reads a wakeup call's input through the paths its
// definition names. A call that stops the loop, or whose input is not an
// object, returns nil, which clears any wakeup scheduled before it.
func parseScheduledWakeup(config clidefs.Wakeup, input any) *scheduledWakeup {
	if _, ok := input.(map[string]any); !ok {
		return nil
	}
	if config.Stop != "" && jsonlTrue(jsonlLookup(input, config.Stop)) {
		return nil
	}
	var seconds float64
	if values := jsonlLookup(input, config.Delay); len(values) > 0 {
		switch value := values[0].(type) {
		case float64:
			seconds = value
		case string:
			seconds, _ = strconv.ParseFloat(strings.TrimSpace(value), 64)
		}
	}
	delay := time.Duration(seconds * float64(time.Second))
	if delay < minWakeupDelay {
		delay = minWakeupDelay
	} else if delay > maxWakeupDelay {
		delay = maxWakeupDelay
	}
	wakeup := &scheduledWakeup{Delay: delay}
	if config.Prompt != "" {
		wakeup.Prompt = firstJSONLString(jsonlLookup(input, config.Prompt))
	}
	if config.Reason != "" {
		wakeup.Reason = firstJSONLString(jsonlLookup(input, config.Reason))
	}
	return wakeup
}

// jsonlTrue reports whether the first value a path names is boolean true.
func jsonlTrue(values []any) bool {
	if len(values) == 0 {
		return false
	}
	value, ok := values[0].(bool)
	return ok && value
}

// wakeupPrompt is what the session is resumed with when its wakeup is due:
// the agent's own prompt, as the wakeup would have delivered it. The loop
// sentinels are resolved by Claude Code's runtime rather than by the model, so
// they, like an empty prompt, are replaced with a plain instruction to go on.
func wakeupPrompt(issue Issue, wakeup scheduledWakeup) string {
	if wakeup.Prompt != "" && (!strings.HasPrefix(wakeup.Prompt, "<<") || !strings.HasSuffix(wakeup.Prompt, ">>")) {
		return wakeup.Prompt
	}
	reason := wakeup.Reason
	if reason == "" {
		reason = "no reason given"
	}
	return fmt.Sprintf("The wakeup you scheduled while working on issue #%d is due (%s). Continue the work from where you left it.", issue.Number, reason)
}

// awaitWakeup waits out a scheduled wakeup. It reports due when the delay
// elapsed and the session should be resumed, and closed when the issue closed
// first, which cancels the wakeup. Both are false when ctx ended, whether from
// a stop or an update to the issue, which the caller reads from its cause.
func (w *Engine) awaitWakeup(ctx context.Context, checker WorkClosureChecker, issue Issue, delay time.Duration) (due, closed bool) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	var poll <-chan time.Time
	if checker != nil {
		ticker := time.NewTicker(w.activeWorkClosureInterval())
		defer ticker.Stop()
		poll = ticker.C
	}
	repo := issueRepository(issue.Target, issue)
	for {
		select {
		case <-ctx.Done():
			return false, false
		case <-timer.C:
			return true, false
		case <-poll:
			// A closure without a merge is the issue watcher's to relay; a
			// merged one leaves nothing for the wakeup to do.
			state, err := checker.OriginatingWorkState(ctx, repo, issue.Number)
			if err == nil && strings.EqualFold(state.IssueState, "closed") && closedWorkReason(OriginatingWorkState{}, state, issue.Number) == "" {
				return false, true
			}
		}
	}
}

// wakeupDelay is how long the run loop waits for a wakeup; tests shorten it.
func (w *Engine) wakeupDelay(wakeup scheduledWakeup) time.Duration {
	if w.wakeupDelayOverride > 0 {
		return w.wakeupDelayOverride
	}
	return wakeup.Delay
}

// jobWaitLabel describes a waiting job the way both dashboards show it, such
// as "waiting: CI is running, resumes in 4m".
func jobWaitLabel(job JobSnapshot, now time.Time) string {
	label := "waiting"
	if job.Status == "paused" {
		label = "paused"
	}
	if job.WaitReason != "" {
		label += ": " + job.WaitReason
	}
	if job.WakeAt.IsZero() {
		return label
	}
	remaining := job.WakeAt.Sub(now)
	if remaining <= 0 {
		return label + ", resuming"
	}
	return label + ", resumes in " + formatWakeupDelay(remaining)
}

// formatWakeupDelay renders a wait rounded up to the unit a person reads it
// in: seconds under a minute, whole minutes otherwise.
func formatWakeupDelay(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int((d+time.Second-1)/time.Second))
	}
	return fmt.Sprintf("%dm", int((d+time.Minute-1)/time.Minute))
}
