// Package notify is Aviary's notification service (#1215): event sources
// emit events to it, it checks the user's notification settings,
// de-duplicates and rate-limits them, keeps a short history for the web UI's
// bell, and pushes each one to connected clients, or to the OS when none is
// connected.
package notify

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/store"
)

// The severities of a notification.
const (
	SeverityInfo    = "info"
	SeveritySuccess = "success"
	SeverityWarning = "warning"
	SeverityError   = "error"
)

// Event is what a source emits.
type Event struct {
	Type     string `json:"type"`
	Severity string `json:"severity,omitempty"`
	Title    string `json:"title"`
	Body     string `json:"body,omitempty"`
	// Link is the web UI path the notification opens, such as a chat, job
	// card, run or task.
	Link string `json:"link,omitempty"`
	// Section is where the event came from; the type's own section when
	// empty.
	Section string `json:"section,omitempty"`
}

// Notification is an event as delivered and kept in the history.
type Notification struct {
	ID       string    `json:"id"`
	Type     string    `json:"type"`
	Severity string    `json:"severity"`
	Title    string    `json:"title"`
	Body     string    `json:"body,omitempty"`
	Link     string    `json:"link,omitempty"`
	Section  string    `json:"section"`
	Time     time.Time `json:"time"`
	Read     bool      `json:"read"`
	// Count is how many events the notification stands for: more than one
	// for repeats of the same event or a grouped burst.
	Count int `json:"count"`
	// Delivery is the event type's style, and Sound the sound to play, ""
	// for none.
	Delivery string `json:"delivery"`
	Sound    string `json:"sound,omitempty"`
	// Quiet marks a notification that arrived during quiet hours: it is in
	// the history, but nothing is shown or played.
	Quiet bool `json:"quiet,omitempty"`
}

// Outcome says what Emit did with an event.
type Outcome string

// The outcomes of Emit.
const (
	// Delivered events were added to the history and pushed.
	Delivered Outcome = "delivered"
	// Merged events repeated a recent one, whose count went up.
	Merged Outcome = "merged"
	// Grouped events were held back by the rate limit and are delivered
	// together when the window ends.
	Grouped Outcome = "grouped"
	// Suppressed events were turned off by the master switch or their
	// type's setting.
	Suppressed Outcome = "suppressed"
)

const (
	// HistoryLimit is how many notifications the history keeps.
	HistoryLimit = 100
	// DedupeWindow is how long a repeat of the same event merges into it.
	DedupeWindow = time.Minute
	// RateWindow is the window the rate limit counts deliveries in.
	RateWindow = 30 * time.Second
	// RateLimit caps one event type's deliveries per RateWindow; the rest
	// are grouped into one.
	RateLimit = 3
)

// Service is the notification service.
type Service struct {
	mu      sync.Mutex
	history []Notification
	// sent holds each type's delivery times within RateWindow.
	sent map[string][]time.Time
	// groups holds each type's events held back by the rate limit.
	groups map[string][]Event

	// Settings reads the user's notification settings.
	Settings func() (store.NotificationPreferences, error)
	// Publish pushes a new notification to connected clients, and Changed
	// tells them the history changed without a new one to show.
	Publish func(Notification)
	Changed func()
	// Connected reports whether any web UI client is connected, and System
	// shows a notification through the OS, for when none is.
	Connected func() bool
	System    func(Notification) error
	// Now and AfterFunc are the clock; tests replace them.
	Now       func() time.Time
	AfterFunc func(time.Duration, func())
}

// New returns a service reading the stored settings.
func New() *Service {
	return &Service{
		sent:      map[string][]time.Time{},
		groups:    map[string][]Event{},
		Settings:  ReadSettings,
		Now:       time.Now,
		AfterFunc: func(d time.Duration, f func()) { time.AfterFunc(d, f) },
	}
}

// Emit delivers e as the user's settings say.
func (s *Service) Emit(e Event) (Notification, Outcome) {
	settings, err := s.Settings()
	if err != nil {
		slog.Warn("notify: reading settings", "err", err)
	}
	e.Title = strings.TrimSpace(e.Title)
	if e.Type == "" {
		e.Type = TypeCustom
	}
	if !settings.Enabled {
		return Notification{}, Suppressed
	}
	test := e.Type == TypeTest
	ev, known := settings.Events[e.Type]
	if !test && (!known || !ev.Enabled) {
		return Notification{}, Suppressed
	}
	if test {
		ev = store.NotificationEvent{Enabled: true, Delivery: DeliverySmall}
	}
	now := s.Now()
	n := Notification{
		Type:     e.Type,
		Severity: e.Severity,
		Title:    e.Title,
		Body:     strings.TrimSpace(e.Body),
		Link:     strings.TrimSpace(e.Link),
		Section:  e.Section,
		Time:     now.UTC(),
		Count:    1,
		Delivery: ev.Delivery,
		Sound:    sound(settings, ev),
		Quiet:    !test && inQuietHours(settings.QuietHours, now),
	}
	if n.Severity == "" {
		n.Severity = SeverityInfo
	}
	if n.Section == "" {
		n.Section = SectionSystem
		if t, ok := eventType(e.Type); ok {
			n.Section = t.Section
		}
	}

	s.mu.Lock()
	if !test {
		if merged, ok := s.mergeLocked(n); ok {
			s.mu.Unlock()
			s.changed()
			return merged, Merged
		}
		if !s.allowLocked(e.Type, now) {
			first := len(s.groups[e.Type]) == 0
			s.groups[e.Type] = append(s.groups[e.Type], e)
			s.mu.Unlock()
			if first {
				s.AfterFunc(RateWindow, func() { s.flush(e.Type) })
			}
			return Notification{}, Grouped
		}
	}
	n.ID = newID()
	s.addLocked(n)
	s.mu.Unlock()
	s.deliver(n, settings)
	return n, Delivered
}

// sound is what ev plays, or "".
func sound(settings store.NotificationPreferences, ev store.NotificationEvent) string {
	if !settings.Sound.Enabled || ev.Sound == SoundNone {
		return ""
	}
	if ev.Sound != "" {
		return ev.Sound
	}
	return settings.Sound.Type
}

// mergeLocked folds n into a recent notification of the same event.
func (s *Service) mergeLocked(n Notification) (Notification, bool) {
	for i := len(s.history) - 1; i >= 0; i-- {
		prev := &s.history[i]
		if n.Time.Sub(prev.Time) > DedupeWindow {
			break
		}
		if prev.Type == n.Type && prev.Title == n.Title && prev.Link == n.Link {
			prev.Count++
			prev.Time, prev.Body, prev.Read = n.Time, n.Body, false
			return *prev, true
		}
	}
	return Notification{}, false
}

// allowLocked records a delivery of typ unless the rate limit is reached.
func (s *Service) allowLocked(typ string, now time.Time) bool {
	if len(s.groups[typ]) > 0 {
		// A burst is being grouped: the rest of it joins the group.
		return false
	}
	var recent []time.Time
	for _, t := range s.sent[typ] {
		if now.Sub(t) < RateWindow {
			recent = append(recent, t)
		}
	}
	if len(recent) >= RateLimit {
		s.sent[typ] = recent
		return false
	}
	s.sent[typ] = append(recent, now)
	return true
}

// flush delivers typ's grouped events as one notification.
func (s *Service) flush(typ string) {
	s.mu.Lock()
	events := s.groups[typ]
	delete(s.groups, typ)
	s.sent[typ] = nil
	s.mu.Unlock()
	if len(events) == 0 {
		return
	}
	settings, _ := s.Settings()
	if !settings.Enabled {
		return
	}
	ev := settings.Events[typ]
	first := events[0]
	n := Notification{
		ID:       newID(),
		Type:     typ,
		Severity: first.Severity,
		Section:  first.Section,
		Time:     s.Now().UTC(),
		Count:    len(events),
		Delivery: ev.Delivery,
		Sound:    sound(settings, ev),
		Quiet:    inQuietHours(settings.QuietHours, s.Now()),
		Link:     first.Link,
	}
	if n.Severity == "" {
		n.Severity = SeverityInfo
	}
	if t, ok := eventType(typ); ok {
		if n.Section == "" {
			n.Section = t.Section
		}
		n.Title = fmt.Sprintf("%d more: %s", len(events), t.Label)
	} else {
		n.Title = fmt.Sprintf("%d more notifications", len(events))
	}
	if n.Section == "" {
		n.Section = SectionSystem
	}
	var titles []string
	for i, e := range events {
		if e.Link != first.Link {
			n.Link = ""
		}
		if i < 5 {
			titles = append(titles, e.Title)
		}
	}
	if len(events) > len(titles) {
		titles = append(titles, fmt.Sprintf("and %d more", len(events)-len(titles)))
	}
	n.Body = strings.Join(titles, "\n")
	s.mu.Lock()
	s.addLocked(n)
	s.mu.Unlock()
	s.deliver(n, settings)
}

func (s *Service) addLocked(n Notification) {
	s.history = append(s.history, n)
	if over := len(s.history) - HistoryLimit; over > 0 {
		s.history = append([]Notification(nil), s.history[over:]...)
	}
}

// deliver pushes n to the web UI, or shows it through the OS when no client
// is connected and the server method is on.
func (s *Service) deliver(n Notification, settings store.NotificationPreferences) {
	if s.Publish != nil {
		s.Publish(n)
	}
	if n.Quiet || !settings.Server || s.System == nil {
		return
	}
	if s.Connected != nil && s.Connected() {
		return
	}
	if err := s.System(n); err != nil {
		slog.Warn("notify: system notification", "err", err)
	}
}

func (s *Service) changed() {
	if s.Changed != nil {
		s.Changed()
	}
}

// History returns the notifications, newest first, and how many are unread.
func (s *Service) History() ([]Notification, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Notification, 0, len(s.history))
	unread := 0
	for i := len(s.history) - 1; i >= 0; i-- {
		out = append(out, s.history[i])
		if !s.history[i].Read {
			unread++
		}
	}
	return out, unread
}

// MarkRead marks the notifications ids names read, or every one when ids is
// empty.
func (s *Service) MarkRead(ids []string) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	s.mu.Lock()
	for i := range s.history {
		if len(want) == 0 || want[s.history[i].ID] {
			s.history[i].Read = true
		}
	}
	s.mu.Unlock()
	s.changed()
}

// Clear empties the history.
func (s *Service) Clear() {
	s.mu.Lock()
	s.history = nil
	s.mu.Unlock()
	s.changed()
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// MaxBodyBytes caps the body of a notification an event source builds from
// run output.
const MaxBodyBytes = 2000

// Clip shortens text to limit bytes on a rune boundary.
func Clip(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && text[cut]&0xC0 == 0x80 {
		cut--
	}
	return strings.TrimSpace(text[:cut]) + "…"
}

// Sanitize limits an event sent through the notify endpoint or tool to the
// custom type, or the test one, with a known severity, clipped text, and a
// link inside the web UI.
func Sanitize(e Event) Event {
	if e.Type != TypeTest {
		e.Type = TypeCustom
	}
	switch e.Severity {
	case SeverityInfo, SeveritySuccess, SeverityWarning, SeverityError:
	default:
		e.Severity = SeverityInfo
	}
	e.Title = Clip(e.Title, 200)
	e.Body = Clip(e.Body, MaxBodyBytes)
	e.Link = strings.TrimSpace(e.Link)
	if !strings.HasPrefix(e.Link, "/") || strings.HasPrefix(e.Link, "//") {
		e.Link = ""
	}
	e.Section = ""
	return e
}
