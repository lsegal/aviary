package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// scheduleWakeupTool is the Claude Code tool an agent calls to be re-invoked
// later. glorp runs Claude in print mode, where the process exits as soon as
// the turn ends, so the wakeup it asked for never fires on its own; glorp keeps
// the run alive and fires it instead (issue #671).
const scheduleWakeupTool = "ScheduleWakeup"

// The delay an agent asks for is clamped to the range Claude Code's own
// runtime allows, so glorp waits exactly as long as the agent would have.
const (
	minWakeupDelay = time.Minute
	maxWakeupDelay = time.Hour
)

// maxConsecutiveWakeups caps how many wakeups in a row one run may schedule
// without doing anything else, so an agent that keeps deferring its work
// cannot hold its slot forever.
const maxConsecutiveWakeups = 48

// scheduledWakeup is a ScheduleWakeup call left pending when the agent's
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

// wakeupReporter is an output decoder that can tell a scheduled wakeup from
// the agent's stream.
type wakeupReporter interface {
	PendingWakeup() (scheduledWakeup, bool)
}

// parseScheduledWakeup reads a ScheduleWakeup call's input. A call that stops
// the loop returns nil, which clears any wakeup scheduled before it.
func parseScheduledWakeup(input json.RawMessage) *scheduledWakeup {
	var fields struct {
		DelaySeconds float64 `json:"delaySeconds"`
		Prompt       string  `json:"prompt"`
		Reason       string  `json:"reason"`
		Stop         bool    `json:"stop"`
	}
	if err := json.Unmarshal(input, &fields); err != nil || fields.Stop {
		return nil
	}
	delay := time.Duration(fields.DelaySeconds * float64(time.Second))
	if delay < minWakeupDelay {
		delay = minWakeupDelay
	} else if delay > maxWakeupDelay {
		delay = maxWakeupDelay
	}
	return &scheduledWakeup{Delay: delay, Prompt: strings.TrimSpace(fields.Prompt), Reason: strings.TrimSpace(fields.Reason)}
}

// wakeupPrompt is what the session is resumed with when its wakeup is due:
// the agent's own prompt, as the wakeup would have delivered it. The loop
// sentinels are resolved by Claude Code's runtime rather than by the model, so
// they, like an empty prompt, are replaced with a plain instruction to go on.
func wakeupPrompt(issue Issue, wakeup scheduledWakeup) string {
	if wakeup.Prompt != "" && !(strings.HasPrefix(wakeup.Prompt, "<<") && strings.HasSuffix(wakeup.Prompt, ">>")) {
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
func (w *Glorp) awaitWakeup(ctx context.Context, checker WorkClosureChecker, issue Issue, delay time.Duration) (due, closed bool) {
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
func (w *Glorp) wakeupDelay(wakeup scheduledWakeup) time.Duration {
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
