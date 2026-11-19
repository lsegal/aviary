package core

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Schedule is an out-of-band prompt glorp runs on a timer rather than for an
// issue, such as a nightly dependency sweep (issue #848). A run takes a
// concurrency slot and shows as a job card like any other job.
type Schedule struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Repository string          `json:"repository"`
	Prompt     string          `json:"prompt"`
	Trigger    ScheduleTrigger `json:"trigger"`
	// Agent is the agent/model:level spec the run uses. Empty rotates through
	// the watch's configured agents like an issue's run.
	Agent   string `json:"agent,omitempty"`
	Enabled bool   `json:"enabled"`
	// Overlap is what a fire does while the schedule's previous run is still
	// active: OverlapSkip records it skipped, OverlapQueue runs it afterwards.
	Overlap string `json:"overlap,omitempty"`
	// Missed is what happens to fires missed while the machine slept or glorp
	// was stopped: MissedSkip drops them, MissedRunOnce runs once for all.
	Missed string `json:"missed,omitempty"`
	// Created anchors an interval trigger, so "every 6h" counts from when the
	// schedule was made rather than from each restart.
	Created time.Time `json:"created,omitempty"`
	// LastFire is the most recent fire time the scheduler accounted for, run
	// or skipped, which is how fires missed while glorp was stopped are found.
	LastFire time.Time `json:"lastFire,omitempty"`
	// RunNow asks the watching instance to start a run at once and clear the
	// flag; `glorp schedule run` sets it in the store file.
	RunNow  bool          `json:"runNow,omitempty"`
	History []ScheduleRun `json:"history,omitempty"`
}

// ScheduleRun is one entry of a schedule's run history.
type ScheduleRun struct {
	Fired    time.Time `json:"fired"`
	Status   string    `json:"status"`
	Summary  string    `json:"summary,omitempty"`
	Duration float64   `json:"duration,omitempty"`
}

// Schedule run statuses.
const (
	ScheduleRunRunning = "running"
	ScheduleRunOK      = "ok"
	ScheduleRunFailed  = "failed"
	ScheduleRunSkipped = "skipped"
)

// Overlap and missed-run policies.
const (
	OverlapSkip   = "skip"
	OverlapQueue  = "queue"
	MissedSkip    = "skip"
	MissedRunOnce = "runOnce"
)

// ScheduleHistoryLimit bounds the run history each schedule keeps.
const ScheduleHistoryLimit = 20

// Trigger kinds.
const (
	TriggerOnce     = "once"
	TriggerInterval = "interval"
	TriggerDaily    = "daily"
	TriggerWeekly   = "weekly"
	TriggerCron     = "cron"
)

// ScheduleTrigger says when a schedule fires.
type ScheduleTrigger struct {
	Kind string `json:"kind"`
	// At is the single fire time of a once trigger.
	At time.Time `json:"at,omitempty"`
	// Every is an interval trigger's period, as a Go duration such as "6h".
	Every string `json:"every,omitempty"`
	// Time is the HH:MM a daily or weekly trigger fires at.
	Time string `json:"time,omitempty"`
	// Weekdays are a weekly trigger's days, 0 for Sunday through 6.
	Weekdays []int `json:"weekdays,omitempty"`
	// Cron is a five-field cron expression: minute hour day month weekday.
	Cron string `json:"cron,omitempty"`
	// Timezone is an IANA zone name; empty means the local zone.
	Timezone string `json:"timezone,omitempty"`
}

// MinScheduleInterval is the shortest period an interval trigger accepts.
const MinScheduleInterval = time.Minute

// Location resolves the trigger's timezone.
func (t ScheduleTrigger) Location() (*time.Location, error) {
	if strings.TrimSpace(t.Timezone) == "" {
		return time.Local, nil
	}
	location, err := time.LoadLocation(strings.TrimSpace(t.Timezone))
	if err != nil {
		return nil, fmt.Errorf("unknown timezone %q", t.Timezone)
	}
	return location, nil
}

// Validate reports the first problem with the trigger.
func (t ScheduleTrigger) Validate() error {
	if _, err := t.Location(); err != nil {
		return err
	}
	switch t.Kind {
	case TriggerOnce:
		if t.At.IsZero() {
			return fmt.Errorf("a once trigger needs a time")
		}
	case TriggerInterval:
		every, err := time.ParseDuration(strings.TrimSpace(t.Every))
		if err != nil {
			return fmt.Errorf("invalid interval %q: use a duration such as 30m or 6h", t.Every)
		}
		if every < MinScheduleInterval {
			return fmt.Errorf("interval %s is shorter than %s", every, MinScheduleInterval)
		}
	case TriggerDaily, TriggerWeekly:
		if _, _, err := parseClock(t.Time); err != nil {
			return err
		}
		if t.Kind == TriggerWeekly {
			if len(t.Weekdays) == 0 {
				return fmt.Errorf("a weekly trigger needs at least one weekday")
			}
			for _, day := range t.Weekdays {
				if day < 0 || day > 6 {
					return fmt.Errorf("weekday %d is out of range 0-6", day)
				}
			}
		}
	case TriggerCron:
		if _, err := ParseCron(t.Cron); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown trigger kind %q: use once, interval, daily, weekly, or cron", t.Kind)
	}
	return nil
}

// Next is the first fire time strictly after after, or false when the trigger
// never fires again. anchor is where an interval trigger counts from.
func (t ScheduleTrigger) Next(after, anchor time.Time) (time.Time, bool) {
	location, err := t.Location()
	if err != nil {
		return time.Time{}, false
	}
	switch t.Kind {
	case TriggerOnce:
		if t.At.After(after) {
			return t.At, true
		}
		return time.Time{}, false
	case TriggerInterval:
		every, err := time.ParseDuration(strings.TrimSpace(t.Every))
		if err != nil || every < MinScheduleInterval {
			return time.Time{}, false
		}
		if anchor.IsZero() {
			anchor = after
		}
		if after.Before(anchor) {
			return anchor.Add(every), true
		}
		steps := after.Sub(anchor)/every + 1
		return anchor.Add(steps * every), true
	case TriggerDaily, TriggerWeekly:
		hour, minute, err := parseClock(t.Time)
		if err != nil {
			return time.Time{}, false
		}
		local := after.In(location)
		for day := 0; day <= 8; day++ {
			date := local.AddDate(0, 0, day)
			candidate := time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, location)
			if !candidate.After(after) {
				continue
			}
			if t.Kind == TriggerWeekly && !slices.Contains(t.Weekdays, int(candidate.Weekday())) {
				continue
			}
			return candidate, true
		}
		return time.Time{}, false
	case TriggerCron:
		spec, err := ParseCron(t.Cron)
		if err != nil {
			return time.Time{}, false
		}
		return spec.Next(after, location)
	}
	return time.Time{}, false
}

// NextFires lists up to n fire times after after.
func (t ScheduleTrigger) NextFires(after, anchor time.Time, n int) []time.Time {
	var fires []time.Time
	for len(fires) < n {
		next, ok := t.Next(after, anchor)
		if !ok {
			break
		}
		fires = append(fires, next)
		after = next
	}
	return fires
}

// parseClock reads an HH:MM time of day.
func parseClock(value string) (int, int, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid time %q: use HH:MM", value)
	}
	return parsed.Hour(), parsed.Minute(), nil
}

// CronSpec is a parsed five-field cron expression.
type CronSpec struct {
	minutes, hours, days, months, weekdays []bool
	// anyDay and anyWeekday record a "*" day or weekday field, which decides
	// how the two combine: cron fires on either one when both are restricted.
	anyDay, anyWeekday bool
}

var cronMonths = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}
var cronWeekdays = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// cronShorthands are the @-names cron accepts for common expressions.
var cronShorthands = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

// ParseCron parses a standard five-field cron expression: minute, hour, day of
// month, month, and day of week, each a "*", a number or name, a range, a
// list, or a step such as "*/15".
func ParseCron(expression string) (CronSpec, error) {
	expression = strings.TrimSpace(expression)
	if shorthand, ok := cronShorthands[strings.ToLower(expression)]; ok {
		expression = shorthand
	}
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return CronSpec{}, fmt.Errorf("invalid cron expression %q: want 5 fields (minute hour day month weekday)", expression)
	}
	var spec CronSpec
	var err error
	if spec.minutes, err = parseCronField(fields[0], 0, 59, nil); err != nil {
		return CronSpec{}, fmt.Errorf("cron minute: %w", err)
	}
	if spec.hours, err = parseCronField(fields[1], 0, 23, nil); err != nil {
		return CronSpec{}, fmt.Errorf("cron hour: %w", err)
	}
	if spec.days, err = parseCronField(fields[2], 1, 31, nil); err != nil {
		return CronSpec{}, fmt.Errorf("cron day: %w", err)
	}
	if spec.months, err = parseCronField(fields[3], 1, 12, cronMonths); err != nil {
		return CronSpec{}, fmt.Errorf("cron month: %w", err)
	}
	// 7 is also Sunday.
	weekdays, err := parseCronField(fields[4], 0, 7, cronWeekdays)
	if err != nil {
		return CronSpec{}, fmt.Errorf("cron weekday: %w", err)
	}
	if weekdays[7] {
		weekdays[0] = true
	}
	spec.weekdays = weekdays[:7]
	spec.anyDay = strings.HasPrefix(fields[2], "*")
	spec.anyWeekday = strings.HasPrefix(fields[4], "*")
	return spec, nil
}

func parseCronField(field string, low, high int, names []string) ([]bool, error) {
	set := make([]bool, high+1)
	for _, part := range strings.Split(field, ",") {
		step := 1
		if base, stepText, ok := strings.Cut(part, "/"); ok {
			value, err := strconv.Atoi(stepText)
			if err != nil || value <= 0 {
				return nil, fmt.Errorf("invalid step %q", part)
			}
			part, step = base, value
		}
		start, end := low, high
		if part != "*" {
			first, last, isRange := strings.Cut(part, "-")
			var err error
			if start, err = cronValue(first, low, high, names); err != nil {
				return nil, err
			}
			end = start
			if isRange {
				if end, err = cronValue(last, low, high, names); err != nil {
					return nil, err
				}
				if end < start {
					return nil, fmt.Errorf("invalid range %q", part)
				}
			} else if step > 1 {
				end = high
			}
		}
		for value := start; value <= end; value += step {
			set[value] = true
		}
	}
	return set, nil
}

func cronValue(text string, low, high int, names []string) (int, error) {
	if index := slices.Index(names, strings.ToLower(text)); index >= 0 {
		return index + low, nil
	}
	value, err := strconv.Atoi(text)
	if err != nil || value < low || value > high {
		return 0, fmt.Errorf("value %q is out of range %d-%d", text, low, high)
	}
	return value, nil
}

// Next is the first minute strictly after after that the expression matches
// in location, searching up to five years ahead.
func (c CronSpec) Next(after time.Time, location *time.Location) (time.Time, bool) {
	t := after.In(location).Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		if !c.months[int(t.Month())] {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, location)
			continue
		}
		if !c.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, location)
			continue
		}
		if !c.hours[t.Hour()] {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, location)
			continue
		}
		if !c.minutes[t.Minute()] {
			t = t.Add(time.Minute)
			continue
		}
		return t, true
	}
	return time.Time{}, false
}

func (c CronSpec) dayMatches(t time.Time) bool {
	day, weekday := c.days[t.Day()], c.weekdays[int(t.Weekday())]
	switch {
	case c.anyDay && c.anyWeekday:
		return true
	case c.anyDay:
		return weekday
	case c.anyWeekday:
		return day
	}
	return day || weekday
}

// Normalize fills in the defaults of the policies and trims the text fields.
func (s *Schedule) Normalize() {
	s.ID = strings.TrimSpace(s.ID)
	s.Name = strings.TrimSpace(s.Name)
	s.Repository = strings.TrimSpace(s.Repository)
	s.Agent = strings.TrimSpace(s.Agent)
	s.Trigger.Timezone = strings.TrimSpace(s.Trigger.Timezone)
	if s.Overlap == "" {
		s.Overlap = OverlapSkip
	}
	if s.Missed == "" {
		s.Missed = MissedSkip
	}
	if s.Name == "" {
		s.Name = s.ID
	}
}

// Validate reports the first problem with the schedule.
func (s Schedule) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("a schedule needs an id")
	}
	if strings.ContainsAny(s.ID, " \t\n/#:") {
		return fmt.Errorf("schedule id %q may not contain spaces, '/', '#', or ':'", s.ID)
	}
	if !ValidRepo(s.Repository) {
		return fmt.Errorf("repository %q is not OWNER/REPO", s.Repository)
	}
	if strings.TrimSpace(s.Prompt) == "" {
		return fmt.Errorf("a schedule needs a prompt")
	}
	if s.Overlap != OverlapSkip && s.Overlap != OverlapQueue {
		return fmt.Errorf("unknown overlap policy %q: use skip or queue", s.Overlap)
	}
	if s.Missed != MissedSkip && s.Missed != MissedRunOnce {
		return fmt.Errorf("unknown missed-run policy %q: use skip or runOnce", s.Missed)
	}
	return s.Trigger.Validate()
}

// Next is the schedule's next fire strictly after after.
func (s Schedule) Next(after time.Time) (time.Time, bool) {
	return s.Trigger.Next(after, s.Created)
}

// AddRun appends run to the history, keeping the most recent
// ScheduleHistoryLimit entries.
func (s *Schedule) AddRun(run ScheduleRun) {
	s.History = append(s.History, run)
	if len(s.History) > ScheduleHistoryLimit {
		s.History = append([]ScheduleRun(nil), s.History[len(s.History)-ScheduleHistoryLimit:]...)
	}
}

// LastRun is the most recent history entry, if any.
func (s Schedule) LastRun() (ScheduleRun, bool) {
	if len(s.History) == 0 {
		return ScheduleRun{}, false
	}
	return s.History[len(s.History)-1], true
}

// Describe summarizes the trigger in a few words, such as "daily at 09:00".
func (t ScheduleTrigger) Describe() string {
	zone := ""
	if t.Timezone != "" {
		zone = " " + t.Timezone
	}
	switch t.Kind {
	case TriggerOnce:
		return "once at " + t.At.Format("2006-01-02 15:04") + zone
	case TriggerInterval:
		return "every " + t.Every
	case TriggerDaily:
		return "daily at " + t.Time + zone
	case TriggerWeekly:
		days := make([]string, 0, len(t.Weekdays))
		for _, day := range t.Weekdays {
			if day >= 0 && day < 7 {
				days = append(days, strings.ToUpper(cronWeekdays[day][:1])+cronWeekdays[day][1:])
			}
		}
		return "weekly " + strings.Join(days, ",") + " at " + t.Time + zone
	case TriggerCron:
		return "cron " + t.Cron + zone
	}
	return t.Kind
}

// ScheduleView is a schedule as the API and dashboards report it, with its
// next fire time and whether a run is active.
type ScheduleView struct {
	Schedule
	Next    time.Time `json:"next,omitempty"`
	Running bool      `json:"running,omitempty"`
}

// ScheduleUpdate is a partial change to a schedule. Nil fields are kept.
type ScheduleUpdate struct {
	Name       *string          `json:"name,omitempty"`
	Repository *string          `json:"repository,omitempty"`
	Prompt     *string          `json:"prompt,omitempty"`
	Trigger    *ScheduleTrigger `json:"trigger,omitempty"`
	Agent      *string          `json:"agent,omitempty"`
	Enabled    *bool            `json:"enabled,omitempty"`
	Overlap    *string          `json:"overlap,omitempty"`
	Missed     *string          `json:"missed,omitempty"`
}

// Apply returns s with the update's fields set.
func (u ScheduleUpdate) Apply(s Schedule) Schedule {
	if u.Name != nil {
		s.Name = *u.Name
	}
	if u.Repository != nil {
		s.Repository = *u.Repository
	}
	if u.Prompt != nil {
		s.Prompt = *u.Prompt
	}
	if u.Trigger != nil {
		s.Trigger = *u.Trigger
	}
	if u.Agent != nil {
		s.Agent = *u.Agent
	}
	if u.Enabled != nil {
		s.Enabled = *u.Enabled
	}
	if u.Overlap != nil {
		s.Overlap = *u.Overlap
	}
	if u.Missed != nil {
		s.Missed = *u.Missed
	}
	return s
}

// UpcomingRun is one of the next scheduled runs the dashboards list.
type UpcomingRun struct {
	ScheduleID string    `json:"scheduleId"`
	Name       string    `json:"name"`
	Repository string    `json:"repository"`
	At         time.Time `json:"at"`
}

// MaxUpcomingRuns is how many upcoming runs a snapshot carries.
const MaxUpcomingRuns = 5

// ScheduleRequest is one schedule operation a dashboard or the CLI asks the
// run to perform: list, create, update, delete, run, or preview.
type ScheduleRequest struct {
	Op       string
	ID       string
	Schedule Schedule
	Update   ScheduleUpdate
	Trigger  ScheduleTrigger
}

// Schedule request operations.
const (
	ScheduleOpList    = "list"
	ScheduleOpCreate  = "create"
	ScheduleOpUpdate  = "update"
	ScheduleOpDelete  = "delete"
	ScheduleOpRun     = "run"
	ScheduleOpPreview = "preview"
)

// ScheduleResponse answers a ScheduleRequest.
type ScheduleResponse struct {
	Schedules []ScheduleView `json:"schedules,omitempty"`
	Schedule  *ScheduleView  `json:"schedule,omitempty"`
	Fires     []time.Time    `json:"fires,omitempty"`
}

// ScheduleJobTarget is the job Target of a scheduled run. Its job Number is
// the run's fire time in Unix seconds, so a run's job key is
// "schedule:<id>#<unix>".
func ScheduleJobTarget(id string) string {
	return "schedule:" + id
}

// ScheduleIDFromTarget reads the schedule ID back out of a job Target.
func ScheduleIDFromTarget(target string) (string, bool) {
	id, ok := strings.CutPrefix(target, "schedule:")
	return id, ok && id != ""
}
