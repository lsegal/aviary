package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
)

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
	return w.relayInboxTo(issueKey(issue), fmt.Sprintf("issue #%d", issue.Number), issueInboxIntro(issue), cancel)
}

// relayInboxTo is relayInbox for the job keyed key, which logs name it as
// subject and whose resumed session is told about the messages with intro.
func (w *Engine) relayInboxTo(key, subject, intro string, cancel context.CancelCauseFunc) bool {
	messages := w.takeInbox(key)
	if len(messages) == 0 {
		return false
	}
	sources := inboxSources(messages)
	w.logf("%s interrupting agent: output from %s; relaying it into the same session", subject, sources)
	cancel(&workUpdate{summary: fmt.Sprintf("%s received output from %s", subject, sources), instruction: inboxPrompt(intro, messages)})
	return true
}

// takeInbox removes the messages waiting under each of keys, oldest first,
// and records them in the transcript of the job keyed by the first, labelled
// with the run that sent each.
func (w *Engine) takeInbox(keys ...string) []InboxMessage {
	var messages []InboxMessage
	for _, key := range keys {
		messages = append(messages, w.jobInbox().take(key)...)
	}
	if len(messages) == 0 {
		return nil
	}
	slices.SortStableFunc(messages, func(a, b InboxMessage) int { return a.At.Compare(b.At) })
	events := make([]transcript.Event, 0, len(messages))
	for _, m := range messages {
		events = append(events, transcript.Event{Kind: transcript.KindCommentRelay, Time: m.At, Author: m.Source, Source: m.Source, URL: m.SourceURL, Text: m.Text})
	}
	w.jobEvents(keys[0], events...)
	return messages
}

// carryInbox moves the messages left waiting under from to the end of the
// queue under to.
func (w *Engine) carryInbox(from, to string) {
	for _, m := range w.jobInbox().take(from) {
		if err := w.jobInbox().add(to, m); err != nil {
			w.logf("failed to carry output from %s over to %s: %v", m.Source, to, err)
		}
	}
}

func inboxSources(messages []InboxMessage) string {
	sources := make([]string, 0, len(messages))
	for _, m := range messages {
		sources = append(sources, m.Source)
	}
	return strings.Join(sources, ", ")
}

// issueInboxIntro opens the prompt an issue job's session is given with the
// messages sent to it.
func issueInboxIntro(issue Issue) string {
	return fmt.Sprintf("New output was sent to your work on issue %s#%d by Aviary scheduled tasks or workflows. Treat it as you would a new comment from an allowed commenter on the issue: read it and determine next steps.", issueRepository(issue.Target, issue), issue.Number)
}

// discussionInboxIntro is issueInboxIntro for a Discussion job.
func discussionInboxIntro(issue Issue) string {
	return fmt.Sprintf("New output was sent to your work on discussion %s#%d by Aviary scheduled tasks or workflows. Treat it as you would a new reply from an allowed commenter on the discussion: read it and determine next steps.", issueRepository(issue.Target, issue), issue.Number)
}

// scheduleInboxIntro is issueInboxIntro for a scheduled project run.
func scheduleInboxIntro(issue Issue) string {
	return fmt.Sprintf("New output was sent to this scheduled run (schedule %q, %q) by Aviary scheduled tasks or workflows. Read it and take it into account in this run.", issue.ScheduleID, issue.Title)
}

// inboxPrompt is what a session is told about the messages sent to its job,
// after intro.
func inboxPrompt(intro string, messages []InboxMessage) string {
	var b strings.Builder
	b.WriteString(intro)
	b.WriteString("\n")
	for _, m := range messages {
		fmt.Fprintf(&b, "\n--- From %s at %s ---\n%s\n", m.Source, m.At.UTC().Format(time.RFC3339), strings.TrimSpace(m.Text))
	}
	return b.String()
}

// scheduleInboxKey keys the messages waiting for target's schedule
// scheduleID between runs; its next run starts with them.
func scheduleInboxKey(target, scheduleID string) string {
	return target + "#schedule:" + scheduleID
}

// SendJobMessage delivers a task's or workflow's output to the Code Loop job
// number of target, by the job's state: a running or waiting job is woken
// with it, a paused, queued or failed job keeps its state and sees it when it
// next runs, and a completed job, or one that no longer exists, rejects it.
// Any run of a schedule names the schedule: its run in flight is woken with
// the output, and between runs the schedule's next run starts with it.
// It returns InboxDelivered or InboxQueued and the job it reached.
func (m *Manager) SendJobMessage(target string, number int, message InboxMessage) (string, core.JobSnapshot, error) {
	engine := m.engine()
	if engine == nil {
		return "", core.JobSnapshot{}, ErrNotWatching
	}
	target = strings.TrimSpace(target)
	jobs := m.currentSnapshot().Jobs
	var job core.JobSnapshot
	found := false
	for _, j := range jobs {
		if j.Number == number && strings.EqualFold(j.Target, target) {
			job, found = j, true
			break
		}
	}
	if !found {
		job, found = m.scheduleRunFromHistory(target, number)
	}
	if !found {
		return "", core.JobSnapshot{}, fmt.Errorf("%w: %s#%d", ErrJobGone, target, number)
	}
	key := job.Target + "#" + fmt.Sprint(job.Number)
	var outcome string
	switch {
	case job.Kind == core.JobKindSchedule:
		job, key, outcome = scheduleInboxTarget(jobs, job)
	case job.Status == "active" || job.Status == "waiting":
		outcome = InboxDelivered
	case job.Status == "paused" || job.Status == "queued" || job.Status == "failed":
		outcome = InboxQueued
	case job.Status == "complete":
		return "", job, fmt.Errorf("the Code Loop job %s is complete; it no longer accepts output", key)
	default:
		return "", job, fmt.Errorf("the Code Loop job %s is %s and cannot accept output", key, job.Status)
	}
	if message.At.IsZero() {
		message.At = time.Now()
	}
	if err := engine.jobInbox().add(key, message); err != nil {
		return "", job, fmt.Errorf("queuing output for Code Loop job %s: %w", key, err)
	}
	if outcome == InboxDelivered {
		engine.nudgeJob(key)
		m.logf("Code Loop job %s received output from %s; waking it", key, message.Source)
	} else {
		m.logf("Code Loop job %s received output from %s; queued until it runs again", key, message.Source)
	}
	return outcome, job, nil
}

// scheduleInboxTarget resolves output sent to run, a run of a schedule, to the
// schedule's run in flight, which is woken with it while running and starts
// with it while queued, or, between runs, to the schedule's own queue, which
// its next run starts with. It returns the job the output reaches, the inbox
// key it waits under, and the outcome.
func scheduleInboxTarget(jobs []core.JobSnapshot, run core.JobSnapshot) (core.JobSnapshot, string, string) {
	for _, j := range jobs {
		if j.Kind != core.JobKindSchedule || j.ScheduleID != run.ScheduleID || !strings.EqualFold(j.Target, run.Target) {
			continue
		}
		key := j.Target + "#" + fmt.Sprint(j.Number)
		switch j.Status {
		case "active", "waiting":
			return j, key, InboxDelivered
		case "queued":
			return j, key, InboxQueued
		}
	}
	return run, scheduleInboxKey(run.Target, run.ScheduleID), InboxQueued
}

// scheduleRunFromHistory finds the run of target's schedule numbered number
// in core's run history, which outlives the board's runs across a restart: a
// scheduled run's number is its fire time.
func (m *Manager) scheduleRunFromHistory(target string, number int) (core.JobSnapshot, bool) {
	for _, task := range m.scheduledTasks() {
		if !strings.EqualFold(task.Project, target) {
			continue
		}
		for _, run := range task.History {
			if int(run.FireTime.Unix()) == number {
				return core.JobSnapshot{Kind: core.JobKindSchedule, ScheduleID: task.ID, Number: number, Target: task.Project, Title: task.Name, Status: "complete"}, true
			}
		}
	}
	return core.JobSnapshot{}, false
}
