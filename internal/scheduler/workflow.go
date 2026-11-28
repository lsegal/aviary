package scheduler

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/schedule"
)

// WorkflowFire is one workflow a channel message triggered.
type WorkflowFire struct {
	TaskID string `json:"task_id"`
	Name   string `json:"name"`
	// JobID is the run's job, empty when the overlap policy skipped it or
	// enqueueing failed.
	JobID  string               `json:"job_id,omitempty"`
	Status domain.TaskRunStatus `json:"status"`
}

// WildcardMatch reports whether text matches pattern, case-insensitively.
// The pattern covers the whole text: "*" matches any run of characters,
// including none and line breaks, and "?" matches exactly one. An empty
// pattern matches every text, like "*".
func WildcardMatch(pattern, text string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return true
	}
	p := []rune(strings.ToLower(pattern))
	t := []rune(strings.ToLower(strings.TrimSpace(text)))
	// Greedy matching with backtracking to the last star.
	pi, ti := 0, 0
	star, mark := -1, 0
	for ti < len(t) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == t[ti]):
			pi++
			ti++
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, ti
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			ti = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

// WorkflowMatches reports whether the workflow tc fires for a message with
// text on entry of the channel cc. It does not check whether tc is enabled.
func WorkflowMatches(tc config.TaskConfig, cc config.ChannelConfig, entry, text string) bool {
	if !tc.IsWorkflow() {
		return false
	}
	channel := strings.TrimSpace(tc.Channel)
	if !strings.EqualFold(channel, cc.EffectiveName()) && !strings.EqualFold(channel, strings.TrimSpace(cc.ID)) {
		return false
	}
	if want := strings.TrimSpace(tc.Entry); want != "" && want != strings.TrimSpace(entry) {
		return false
	}
	return WildcardMatch(tc.Match, text)
}

// MatchWorkflows returns the keys of agentName's enabled workflows that a
// message with text on entry of the channel cc triggers, sorted by name.
// Paused workflows never match.
func (s *Scheduler) MatchWorkflows(agentName string, cc config.ChannelConfig, entry, text string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.matchWorkflowsLocked(agentName, cc, entry, text)
}

func (s *Scheduler) matchWorkflowsLocked(agentName string, cc config.ChannelConfig, entry, text string) []string {
	var keys []string
	for key, tc := range s.tasks {
		if a, _, _ := strings.Cut(key, "/"); a != agentName {
			continue
		}
		if WorkflowMatches(tc, cc, entry, text) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// FireWorkflows starts a run of every enabled workflow of agentName that the
// message in trigger matches, on the shared job queue with the same overlap
// policy as scheduled tasks. Every matching workflow runs; they are fired in
// name order. Each run records trigger as its trigger context.
func (s *Scheduler) FireWorkflows(agentName string, cc config.ChannelConfig, trigger domain.TriggerContext) []WorkflowFire {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.halted {
		return nil
	}
	keys := s.matchWorkflowsLocked(agentName, cc, trigger.Entry, trigger.Message.Text)
	fires := make([]WorkflowFire, 0, len(keys))
	for _, key := range keys {
		tc := s.tasks[key]
		spec := schedule.Spec{Overlap: strings.ToLower(strings.TrimSpace(tc.Overlap))}
		if spec.Overlap == "" {
			spec.Overlap = schedule.OverlapQueue
		}
		ctx := trigger
		fire := WorkflowFire{TaskID: key, Name: tc.Name}
		job := s.fireLocked(key, spec, "message", time.Time{}, &ctx)
		if job != nil {
			fire.JobID = job.ID
			fire.Status = domain.TaskRunQueued
		} else {
			fire.Status = domain.TaskRunSkipped
		}
		fires = append(fires, fire)
	}
	return fires
}

// WorkflowPrompt is the prompt a workflow run gives its agent: the
// workflow's own content, followed by the message that triggered it.
func WorkflowPrompt(content string, tc *domain.TriggerContext) string {
	if tc == nil {
		return content
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(content))
	b.WriteString("\n\n---\nThis workflow was triggered by a message")
	if tc.Channel != "" {
		fmt.Fprintf(&b, " on the %s channel %q", tc.ChannelType, tc.Channel)
	}
	if tc.Entry != "" {
		fmt.Fprintf(&b, " (conversation %s)", tc.Entry)
	}
	b.WriteString(".\n")
	if len(tc.Before) > 0 {
		b.WriteString("\nEarlier messages:\n")
		for _, m := range tc.Before {
			writeTriggerMessage(&b, m)
		}
	}
	b.WriteString("\nTriggering message:\n")
	writeTriggerMessage(&b, tc.Message)
	return b.String()
}

func writeTriggerMessage(b *strings.Builder, m domain.TriggerMessage) {
	sender := m.Sender
	switch {
	case m.FromAgent:
		sender = "you"
	case sender == "":
		sender = "unknown sender"
	}
	fmt.Fprintf(b, "[%s] %s: %s\n", m.Timestamp.UTC().Format(time.RFC3339), sender, strings.TrimSpace(m.Text))
	for _, a := range m.Attachments {
		fmt.Fprintf(b, "  attachment: %s\n", a)
	}
}

// DisplayName returns name as a sender's display name, or "" when name is
// empty or is only the raw ID, a phone number or a platform user ID, which
// trigger contexts never record.
func DisplayName(id, name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == strings.TrimSpace(id) || looksLikeRawID(name) {
		return ""
	}
	return name
}

// looksLikeRawID reports whether s looks like a phone number, a UUID or a
// platform user ID rather than a person's name.
// rawIDPunctuation lists the non-alphanumeric characters a raw ID such as a
// formatted phone number may contain.
const rawIDPunctuation = "+- ()."

func looksLikeRawID(s string) bool {
	digits, letters := 0, 0
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			digits++
		case unicode.IsLetter(r):
			letters++
		case strings.ContainsRune(rawIDPunctuation, r):
		default:
			return false
		}
	}
	if letters == 0 {
		return digits > 0 // a phone number
	}
	// A UUID, or a Slack or Discord user ID such as U012AB3CD.
	if !strings.Contains(s, " ") && digits > 0 && strings.ToUpper(s) == s {
		return true
	}
	if len(s) == 36 && strings.Count(s, "-") == 4 {
		return true
	}
	return false
}

// workflowDescription summarizes a workflow's trigger, e.g. "messages
// matching *crash* on Team Slack".
func workflowDescription(tc config.TaskConfig) string {
	what := "every message"
	if m := strings.TrimSpace(tc.Match); m != "" && m != "*" {
		what = fmt.Sprintf("messages matching %s", m)
	}
	where := "on " + strings.TrimSpace(tc.Channel)
	if e := strings.TrimSpace(tc.Entry); e != "" {
		where += " (" + e + ")"
	}
	return what + " " + where
}
