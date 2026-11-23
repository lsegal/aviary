package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/store"
	"github.com/lsegal/aviary/internal/transcript"
)

// InboxMessage is a scheduled task's or workflow's output sent to a Code Loop
// job (issue #999). It reaches the job's agent the way an allowed commenter's
// comment does: a running or waiting job is interrupted and resumed with it in
// the same session, and a paused or queued job hears it when it next runs.
type InboxMessage struct {
	// Source labels the run that sent the message, such as
	// "workflow bot/triage", and SourceURL links to that run.
	Source    string    `json:"source"`
	SourceURL string    `json:"source_url,omitempty"`
	Text      string    `json:"text"`
	At        time.Time `json:"at"`
}

// The outcomes of sending a message to a Code Loop job.
const (
	// InboxDelivered means the job was running or waiting and was woken with
	// the message.
	InboxDelivered = "delivered"
	// InboxQueued means the job was paused or queued and keeps its state; its
	// agent sees the message when the job next runs.
	InboxQueued = "queued"
	// InboxResumed means the job was paused and, with JobDeliveryOptions.Resume,
	// was resumed so its agent sees the message on its first turn.
	InboxResumed = "resumed"
	// InboxReopened means the job was complete and, with
	// JobDeliveryOptions.Reopen, was retried with the message.
	InboxReopened = "reopened"
)

// JobDeliveryOptions are a Code Loop target's delivery options (issue #1008).
// Without them a paused job queues the output and a completed job rejects it.
type JobDeliveryOptions struct {
	// Resume resumes a paused job when output arrives.
	Resume bool
	// Reopen retries a completed job with the output.
	Reopen bool
}

// jobDeliveryActionTimeout bounds how long a delivery waits for the engine to
// resume or reopen the job it reached.
const jobDeliveryActionTimeout = time.Minute

// ErrJobGone is returned when a message names a Code Loop job that no longer
// exists.
var ErrJobGone = errors.New("the Code Loop job no longer exists")

// jobInbox holds the messages waiting for each job, by issue key. It is saved
// beside the work state, so a message queued for a paused job survives a
// restart.
type jobInbox struct {
	mu       sync.Mutex
	path     string
	loaded   bool
	messages map[string][]InboxMessage
}

func (b *jobInbox) loadLocked() {
	if b.loaded {
		return
	}
	b.loaded = true
	b.messages = map[string][]InboxMessage{}
	if b.path == "" {
		return
	}
	if saved, err := store.ReadJSON[map[string][]InboxMessage](b.path); err == nil && saved != nil {
		b.messages = saved
	}
}

func (b *jobInbox) saveLocked() error {
	if b.path == "" {
		return nil
	}
	if len(b.messages) == 0 {
		if err := os.Remove(b.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return store.WriteJSON(b.path, b.messages)
}

// add queues message for the job keyed key.
func (b *jobInbox) add(key string, message InboxMessage) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.loadLocked()
	b.messages[key] = append(b.messages[key], message)
	return b.saveLocked()
}

// take removes and returns every message waiting for the job keyed key.
func (b *jobInbox) take(key string) []InboxMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.loadLocked()
	messages := b.messages[key]
	if len(messages) == 0 {
		return nil
	}
	delete(b.messages, key)
	_ = b.saveLocked()
	return messages
}

// discard removes the last message waiting for the job keyed key if it is
// message, undoing an add whose delivery failed.
func (b *jobInbox) discard(key string, message InboxMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.loadLocked()
	messages := b.messages[key]
	if n := len(messages); n > 0 && messages[n-1] == message {
		if n == 1 {
			delete(b.messages, key)
		} else {
			b.messages[key] = messages[:n-1]
		}
		_ = b.saveLocked()
	}
}

// pending reports how many messages wait for the job keyed key.
func (b *jobInbox) pending(key string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.loadLocked()
	return len(b.messages[key])
}

// jobInbox returns the engine's inbox, saved beside its work state.
func (w *Engine) jobInbox() *jobInbox {
	w.inboxOnce.Do(func() {
		path := ""
		if w.StatePath != "" {
			path = filepath.Join(filepath.Dir(w.StatePath), "inbox.json")
		}
		w.inbox = &jobInbox{path: path}
	})
	return w.inbox
}

// nudgeJob asks the watcher of the job keyed key, if it is running or
// waiting, to check for news now.
func (w *Engine) nudgeJob(key string) {
	if nudge, ok := w.liveNudges.Load(key); ok {
		select {
		case nudge.(chan struct{}) <- struct{}{}:
		default:
		}
	}
}

// relayInbox hands the messages waiting for issue's job to its running agent:
// it records each in the job's transcript, labelled with the run that sent
// it, and interrupts the run with a workUpdate that resumes the same session
// with them. It reports whether it interrupted the run.
func (w *Engine) relayInbox(issue Issue, cancel context.CancelCauseFunc) bool {
	key := issueKey(issue)
	messages := w.jobInbox().take(key)
	if len(messages) == 0 {
		return false
	}
	events := make([]transcript.Event, 0, len(messages))
	sources := make([]string, 0, len(messages))
	for _, m := range messages {
		events = append(events, transcript.Event{Kind: transcript.KindCommentRelay, Time: m.At, Author: m.Source, Source: m.Source, URL: m.SourceURL, Text: m.Text})
		sources = append(sources, m.Source)
	}
	w.jobEvents(key, events...)
	summary := fmt.Sprintf("issue #%d received output from %s", issue.Number, strings.Join(sources, ", "))
	w.logf("issue #%d interrupting agent: output from %s; relaying it into the same session", issue.Number, strings.Join(sources, ", "))
	cancel(&workUpdate{summary: summary, instruction: inboxPrompt(issue, messages)})
	return true
}

// inboxPrompt is what a resumed session is told about the messages sent to
// its job.
func inboxPrompt(issue Issue, messages []InboxMessage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "New output was sent to your work on issue %s#%d by Aviary scheduled tasks or workflows. Treat it as you would a new comment from an allowed commenter on the issue: read it and determine next steps.\n", issueRepository(issue.Target, issue), issue.Number)
	for _, m := range messages {
		fmt.Fprintf(&b, "\n--- From %s at %s ---\n%s\n", m.Source, m.At.UTC().Format(time.RFC3339), strings.TrimSpace(m.Text))
	}
	return b.String()
}

// SendJobMessage delivers a task's or workflow's output to the Code Loop job
// number of target, by the job's state: a running or waiting job is woken
// with it, a paused, queued or failed job keeps its state and sees it when it
// next runs, and a completed job, or one that no longer exists, rejects it.
// With options.Resume a paused job is resumed with the output instead, and
// with options.Reopen a completed job is retried with it, both through the
// same resume and retry actions as the dashboard's buttons. It returns the
// InboxDelivered, InboxQueued, InboxResumed or InboxReopened outcome and the
// job it reached.
func (m *Manager) SendJobMessage(ctx context.Context, target string, number int, message InboxMessage, options JobDeliveryOptions) (string, core.JobSnapshot, error) {
	engine := m.engine()
	if engine == nil {
		return "", core.JobSnapshot{}, ErrNotWatching
	}
	target = strings.TrimSpace(target)
	var job core.JobSnapshot
	found := false
	for _, j := range m.currentSnapshot().Jobs {
		if j.Number == number && strings.EqualFold(j.Target, target) {
			job, found = j, true
			break
		}
	}
	if !found {
		return "", core.JobSnapshot{}, fmt.Errorf("%w: %s#%d", ErrJobGone, target, number)
	}
	key := job.Target + "#" + fmt.Sprint(job.Number)
	if job.Kind != "" && job.Kind != core.JobKindIssue {
		return "", job, fmt.Errorf("the Code Loop job %s is a %s job; only issue jobs accept output", key, job.Kind)
	}
	var outcome, action string
	switch job.Status {
	case "active", "waiting":
		outcome = InboxDelivered
	case "paused":
		outcome = InboxQueued
		if options.Resume {
			outcome, action = InboxResumed, "resume"
		}
	case "queued", "failed":
		outcome = InboxQueued
	case "complete":
		if !options.Reopen {
			return "", job, fmt.Errorf("the Code Loop job %s is complete; it no longer accepts output", key)
		}
		outcome, action = InboxReopened, "retry"
	default:
		return "", job, fmt.Errorf("the Code Loop job %s is %s and cannot accept output", key, job.Status)
	}
	if message.At.IsZero() {
		message.At = time.Now()
	}
	if err := engine.jobInbox().add(key, message); err != nil {
		return "", job, fmt.Errorf("queuing output for Code Loop job %s: %w", key, err)
	}
	switch outcome {
	case InboxDelivered:
		engine.nudgeJob(key)
		m.logf("Code Loop job %s received output from %s; waking it", key, message.Source)
		return outcome, job, nil
	case InboxQueued:
		m.logf("Code Loop job %s received output from %s; queued until it runs again", key, message.Source)
		return outcome, job, nil
	}
	// The queued message is relayed into the session when the resumed or
	// retried run starts.
	actionCtx, cancel := context.WithTimeout(ctx, jobDeliveryActionTimeout)
	defer cancel()
	if _, err := m.JobAction(actionCtx, action, job.Target, job.Number); err != nil {
		if outcome == InboxReopened {
			// A completed job never runs again on its own, so the output
			// is not left waiting for it.
			engine.jobInbox().discard(key, message)
			return "", job, fmt.Errorf("reopening the completed Code Loop job %s: %w", key, err)
		}
		m.logf("Code Loop job %s received output from %s; resuming it failed (%v), so it stays queued until the job runs again", key, message.Source, err)
		return InboxQueued, job, nil
	}
	if outcome == InboxResumed {
		m.logf("Code Loop job %s received output from %s; resumed it", key, message.Source)
	} else {
		m.logf("Code Loop job %s received output from %s; reopened it", key, message.Source)
	}
	return outcome, job, nil
}
