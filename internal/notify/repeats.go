package notify

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/store"
)

// streak is one source's run of failures (#1419): how many in a row, the
// last one's normalized error, the history entry its repeats merge into,
// and whether the in-a-row notification was sent.
type streak struct {
	failures int
	err      string
	entry    string
	last     time.Time
	alerted  bool
}

func streakKey(e Event) string { return e.Type + "\x00" + e.Source }

// repeatLocked handles a failure with a Source, with s.mu held. It folds a
// repeat of the source's last failure into that failure's entry without
// delivering it again, and sends one notification when the source has
// failed StreakCount times in a row. When it handled the event it unlocks
// s.mu and reports ok; otherwise s.mu stays held and the event is delivered
// as a new notification.
func (s *Service) repeatLocked(e Event, n Notification, settings store.NotificationPreferences) (Notification, Outcome, bool) {
	r := DefaultRepeats()
	if settings.Repeats != nil {
		r = *settings.Repeats
	}
	key := streakKey(e)
	st := s.streaks[key]
	if st == nil {
		st = &streak{}
		s.streaks[key] = st
	}
	st.failures++
	norm := normalizeError(e.Body)
	same := st.entry != "" && st.err == norm && n.Time.Sub(st.last) <= time.Duration(r.WindowMinutes)*time.Minute
	st.err, st.last = norm, n.Time

	var alert *Notification
	if r.Streak && !st.alerted && st.failures >= r.StreakCount {
		st.alerted = true
		a := n
		a.ID = newID()
		a.Title = fmt.Sprintf("%s has failed %d times in a row", e.Source, st.failures)
		a.PauseTask = e.PauseTask
		alert = &a
	}

	if r.Suppress && same {
		if merged, ok := s.bumpLocked(st.entry, n); ok {
			if alert != nil {
				s.addLocked(*alert)
			}
			s.mu.Unlock()
			if alert != nil {
				s.deliver(*alert, settings)
			} else {
				s.changed()
			}
			return merged, Merged, true
		}
	}
	if alert != nil {
		// The in-a-row notification stands for this failure too, and later
		// repeats merge into it.
		s.addLocked(*alert)
		st.entry = alert.ID
		s.mu.Unlock()
		s.deliver(*alert, settings)
		return *alert, Delivered, true
	}
	// Emit records the new entry once it is added.
	st.entry = ""
	return Notification{}, "", false
}

// bumpLocked raises the count of the history entry id for the repeat n and
// moves it to the newest place, keeping whether it was read.
func (s *Service) bumpLocked(id string, n Notification) (Notification, bool) {
	for i := range s.history {
		if s.history[i].ID != id {
			continue
		}
		prev := s.history[i]
		prev.Count++
		prev.Time, prev.Body = n.Time, n.Body
		s.history = append(append(s.history[:i:i], s.history[i+1:]...), prev)
		return prev, true
	}
	return Notification{}, false
}

// Recover ends source's run of failures, of every type, after a success, so
// its next failure notifies again.
func (s *Service) Recover(source string) {
	if source == "" {
		return
	}
	s.mu.Lock()
	for key := range s.streaks {
		if _, src, _ := strings.Cut(key, "\x00"); src == source {
			delete(s.streaks, key)
		}
	}
	s.mu.Unlock()
}

var (
	hexRun    = regexp.MustCompile(`\b[0-9a-f]{6,}\b`)
	digitRun  = regexp.MustCompile(`\d+`)
	hasDigits = regexp.MustCompile(`\d`)
)

// normalizeError reduces an error message to what stays the same across
// runs of the same failure: lowercased, with IDs, hashes, numbers, times and
// durations replaced and spacing collapsed.
func normalizeError(text string) string {
	text = strings.ToLower(text)
	text = hexRun.ReplaceAllStringFunc(text, func(m string) string {
		if hasDigits.MatchString(m) {
			return "#"
		}
		return m
	})
	text = digitRun.ReplaceAllString(text, "#")
	return strings.Join(strings.Fields(text), " ")
}
