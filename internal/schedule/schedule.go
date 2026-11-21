// Package schedule computes when a scheduled task fires. It understands the
// once, interval, daily, weekly and cron triggers, each evaluated in an IANA
// timezone, and has no dependency on how tasks are stored or run.
package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // IANA zones on hosts without a zoneinfo database

	"github.com/robfig/cron/v3"

	"github.com/lsegal/aviary/internal/cronutil"
)

// Kind identifies a trigger.
type Kind string

// Kind values.
const (
	KindOnce     Kind = "once"
	KindInterval Kind = "interval"
	KindDaily    Kind = "daily"
	KindWeekly   Kind = "weekly"
	KindCron     Kind = "cron"
)

// Overlap policies decide what a fire does while an earlier run of the same
// task is still pending or running.
const (
	OverlapQueue = "queue"
	OverlapSkip  = "skip"
)

// Missed-run policies decide what happens to fires that were due while
// Aviary was stopped or the machine slept.
const (
	MissedSkip    = "skip"
	MissedRunOnce = "run_once"
)

// Fields are the raw trigger settings of a task, as written in its config.
type Fields struct {
	Trigger  string
	Schedule string
	StartAt  string
	RunOnce  bool
	Every    string
	At       string
	Days     string
	Timezone string
	Overlap  string
	Missed   string
}

// Spec is a parsed trigger.
type Spec struct {
	Kind     Kind
	Location *time.Location
	// StartAt is the once trigger's fire time, and the earliest fire of any
	// recurring trigger. It is zero when unset.
	StartAt time.Time
	RunOnce bool
	Every   time.Duration
	Times   []ClockTime
	Days    []time.Weekday
	Cron    cron.Schedule
	// Expr is the cron trigger's expression, as configured.
	Expr    string
	Overlap string
	Missed  string
}

// ClockTime is a wall-clock time of day.
type ClockTime struct{ Hour, Minute int }

func (c ClockTime) String() string { return fmt.Sprintf("%02d:%02d", c.Hour, c.Minute) }

// HasTrigger reports whether f configures any time-based trigger.
func (f Fields) HasTrigger() bool {
	return strings.TrimSpace(f.Trigger) != "" || strings.TrimSpace(f.Schedule) != "" ||
		strings.TrimSpace(f.Every) != "" || strings.TrimSpace(f.At) != "" ||
		strings.TrimSpace(f.StartAt) != ""
}

// InferKind returns the trigger kind f selects, inferring it from the
// populated fields when Trigger is empty.
func (f Fields) InferKind() Kind {
	if k := Kind(strings.ToLower(strings.TrimSpace(f.Trigger))); k != "" {
		return k
	}
	switch {
	case strings.TrimSpace(f.Schedule) != "":
		return KindCron
	case strings.TrimSpace(f.Every) != "":
		return KindInterval
	case strings.TrimSpace(f.Days) != "":
		return KindWeekly
	case strings.TrimSpace(f.At) != "":
		return KindDaily
	case strings.TrimSpace(f.StartAt) != "":
		return KindOnce
	}
	return ""
}

// Parse validates f and returns its Spec.
func Parse(f Fields) (Spec, error) {
	spec := Spec{Kind: f.InferKind(), RunOnce: f.RunOnce, Location: time.Local}
	if spec.Kind == "" {
		return Spec{}, fmt.Errorf("no trigger configured")
	}
	if tz := strings.TrimSpace(f.Timezone); tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return Spec{}, fmt.Errorf("invalid timezone %q: %w", tz, err)
		}
		spec.Location = loc
	}
	if v := strings.TrimSpace(f.StartAt); v != "" {
		ts, err := ParseTime(v, spec.Location)
		if err != nil {
			return Spec{}, fmt.Errorf("invalid start_at %q: %w", v, err)
		}
		spec.StartAt = ts
	}
	switch o := strings.ToLower(strings.TrimSpace(f.Overlap)); o {
	case "":
		spec.Overlap = OverlapQueue
	case OverlapQueue, OverlapSkip:
		spec.Overlap = o
	default:
		return Spec{}, fmt.Errorf("invalid overlap %q; must be %q or %q", f.Overlap, OverlapQueue, OverlapSkip)
	}
	switch m := strings.ToLower(strings.TrimSpace(f.Missed)); m {
	case "":
		spec.Missed = MissedSkip
	case MissedSkip, MissedRunOnce:
		spec.Missed = m
	default:
		return Spec{}, fmt.Errorf("invalid missed %q; must be %q or %q", f.Missed, MissedSkip, MissedRunOnce)
	}

	var err error
	switch spec.Kind {
	case KindOnce:
		if spec.StartAt.IsZero() {
			return Spec{}, fmt.Errorf("once trigger requires start_at")
		}
	case KindInterval:
		spec.Every, err = ParseEvery(f.Every)
		if err != nil {
			return Spec{}, err
		}
	case KindDaily, KindWeekly:
		spec.Times, err = parseClockTimes(f.At)
		if err != nil {
			return Spec{}, err
		}
		if spec.Kind == KindWeekly {
			spec.Days, err = parseDays(f.Days)
			if err != nil {
				return Spec{}, err
			}
		}
	case KindCron:
		expr := strings.TrimSpace(f.Schedule)
		if expr == "" {
			return Spec{}, fmt.Errorf("cron trigger requires schedule")
		}
		spec.Expr = expr
		spec.Cron, err = cronutil.Parser().Parse(expr)
		if err != nil {
			return Spec{}, fmt.Errorf("invalid cron expression %q: %w", expr, err)
		}
	default:
		return Spec{}, fmt.Errorf("invalid trigger %q; must be once, interval, daily, weekly or cron", f.Trigger)
	}
	return spec, nil
}

// Next returns the first fire strictly after after, or the zero time when the
// trigger never fires again. Interval triggers are anchored at StartAt when
// it is set, and otherwise at after itself.
func (s Spec) Next(after time.Time) time.Time {
	if !s.StartAt.IsZero() && after.Before(s.StartAt) {
		if s.Kind == KindOnce || s.Kind == KindInterval {
			return s.StartAt
		}
		// Recurring triggers may fire exactly at StartAt.
		after = s.StartAt.Add(-time.Nanosecond)
	}
	local := after.In(s.Location)
	switch s.Kind {
	case KindOnce:
		return time.Time{}
	case KindInterval:
		if s.StartAt.IsZero() {
			return after.Add(s.Every)
		}
		n := after.Sub(s.StartAt)/s.Every + 1
		return s.StartAt.Add(n * s.Every)
	case KindDaily, KindWeekly:
		// Look a little over a week ahead: weekly triggers always fire in it.
		for d := 0; d <= 8; d++ {
			day := time.Date(local.Year(), local.Month(), local.Day()+d, 0, 0, 0, 0, s.Location)
			if s.Kind == KindWeekly && !containsDay(s.Days, day.Weekday()) {
				continue
			}
			for _, c := range s.Times {
				t := wallClock(day, c, s.Location)
				if t.After(after) {
					return t
				}
			}
		}
		return time.Time{}
	case KindCron:
		return s.Cron.Next(local)
	}
	return time.Time{}
}

// Preview returns up to n upcoming fires after after.
func (s Spec) Preview(after time.Time, n int) []time.Time {
	out := make([]time.Time, 0, n)
	for len(out) < n {
		next := s.Next(after)
		if next.IsZero() {
			break
		}
		out = append(out, next)
		if s.RunOnce {
			break
		}
		after = next
	}
	return out
}

// maxCountedFires bounds the fires RunsIn walks one by one; denser triggers
// are extrapolated from the time those fires span.
const maxCountedFires = 10000

// RunsIn returns how many times a recurring trigger fires in the window
// [from, from+d). The window opens at from, or at StartAt when that is later, so a
// trigger that starts in the future is counted at its steady rate. It returns
// 0 for once and run-once triggers, which have no rate.
func (s Spec) RunsIn(from time.Time, d time.Duration) int {
	if s.Kind == KindOnce || s.RunOnce || d <= 0 {
		return 0
	}
	if s.StartAt.After(from) {
		from = s.StartAt
	}
	if s.Kind == KindInterval {
		return int(d / s.Every)
	}
	until := from.Add(d)
	after := from.Add(-time.Nanosecond)
	var first time.Time
	n := 0
	for {
		next := s.Next(after)
		if next.IsZero() || !next.Before(until) {
			return n
		}
		if n == 0 {
			first = next
		}
		n++
		after = next
		if n == maxCountedFires {
			// n fires span n-1 gaps from first to next.
			return int(float64(n-1) * float64(d) / float64(next.Sub(first)))
		}
	}
}

// Describe summarizes the trigger in a short human-readable phrase.
func (s Spec) Describe() string {
	var desc string
	switch s.Kind {
	case KindOnce:
		desc = "once at " + s.StartAt.In(s.Location).Format("2006-01-02 15:04")
	case KindInterval:
		desc = "every " + s.Every.String()
	case KindDaily:
		desc = "daily at " + joinTimes(s.Times)
	case KindWeekly:
		names := make([]string, len(s.Days))
		for i, d := range s.Days {
			names[i] = d.String()[:3]
		}
		desc = "weekly on " + strings.Join(names, ", ") + " at " + joinTimes(s.Times)
	case KindCron:
		desc = "cron " + s.Expr
	}
	return desc + " (" + s.Location.String() + ")"
}

// ParseTime parses an RFC 3339 timestamp, or a "YYYY-MM-DDTHH:MM[:SS]" or
// "YYYY-MM-DD HH:MM[:SS]" wall-clock time in loc.
func ParseTime(v string, loc *time.Location) (time.Time, error) {
	if ts, err := time.Parse(time.RFC3339, v); err == nil {
		return ts, nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if ts, err := time.ParseInLocation(layout, v, loc); err == nil {
			return ts, nil
		}
	}
	return time.Time{}, fmt.Errorf("expected RFC 3339 or YYYY-MM-DDTHH:MM")
}

// ParseEvery parses an interval such as "90s", "15m", "2h" or "1d".
func ParseEvery(v string) (time.Duration, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return 0, fmt.Errorf("interval trigger requires every")
	}
	var d time.Duration
	var err error
	if days, ok := strings.CutSuffix(v, "d"); ok {
		var n int
		n, err = strconv.Atoi(days)
		d = time.Duration(n) * 24 * time.Hour
	} else {
		d, err = time.ParseDuration(v)
	}
	if err != nil {
		return 0, fmt.Errorf("invalid every %q: %w", v, err)
	}
	if d < time.Second {
		return 0, fmt.Errorf("invalid every %q: must be at least 1s", v)
	}
	return d, nil
}

func parseClockTimes(v string) ([]ClockTime, error) {
	if strings.TrimSpace(v) == "" {
		return nil, fmt.Errorf("daily and weekly triggers require at (HH:MM)")
	}
	var out []ClockTime
	for part := range strings.SplitSeq(v, ",") {
		t, err := time.Parse("15:04", strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("invalid at %q: expected HH:MM", strings.TrimSpace(part))
		}
		out = append(out, ClockTime{t.Hour(), t.Minute()})
	}
	sortClockTimes(out)
	return out, nil
}

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

func parseDays(v string) ([]time.Weekday, error) {
	if strings.TrimSpace(v) == "" {
		return nil, fmt.Errorf("weekly trigger requires days")
	}
	var out []time.Weekday
	for part := range strings.SplitSeq(v, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if len(name) >= 3 {
			name = name[:3]
		}
		d, ok := weekdays[name]
		if !ok {
			return nil, fmt.Errorf("invalid day %q", strings.TrimSpace(part))
		}
		if !containsDay(out, d) {
			out = append(out, d)
		}
	}
	return out, nil
}

// wallClock returns c on day in loc. A time skipped by a daylight-saving
// transition moves forward by the transition's length, so 02:30 on a
// spring-forward night fires at 03:30 rather than before the jump.
func wallClock(day time.Time, c ClockTime, loc *time.Location) time.Time {
	t := time.Date(day.Year(), day.Month(), day.Day(), c.Hour, c.Minute, 0, 0, loc)
	if t.Hour() != c.Hour || t.Minute() != c.Minute {
		_, before := t.Zone()
		_, after := t.Add(3 * time.Hour).Zone()
		t = t.Add(time.Duration(after-before) * time.Second)
	}
	return t
}

func containsDay(days []time.Weekday, d time.Weekday) bool {
	for _, x := range days {
		if x == d {
			return true
		}
	}
	return false
}

func sortClockTimes(ts []ClockTime) {
	for i := 1; i < len(ts); i++ {
		for j := i; j > 0 && ts[j].Hour*60+ts[j].Minute < ts[j-1].Hour*60+ts[j-1].Minute; j-- {
			ts[j], ts[j-1] = ts[j-1], ts[j]
		}
	}
}

func joinTimes(ts []ClockTime) string {
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = t.String()
	}
	return strings.Join(parts, ", ")
}
