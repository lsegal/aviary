package core

import (
	"strings"
	"testing"
	"time"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("timezone %s unavailable: %v", name, err)
	}
	return location
}

func TestScheduleTriggerNextDailyUsesItsTimezone(t *testing.T) {
	toronto := mustZone(t, "America/Toronto")
	trigger := ScheduleTrigger{Kind: TriggerDaily, Time: "09:30", Timezone: "America/Toronto"}
	after := time.Date(2026, 11, 18, 10, 0, 0, 0, toronto)
	next, ok := trigger.Next(after, time.Time{})
	want := time.Date(2026, 11, 19, 9, 30, 0, 0, toronto)
	if !ok || !next.Equal(want) {
		t.Fatalf("Next = %v, %v; want %v", next, ok, want)
	}
	next, _ = trigger.Next(time.Date(2026, 11, 18, 9, 0, 0, 0, toronto), time.Time{})
	if want := time.Date(2026, 11, 18, 9, 30, 0, 0, toronto); !next.Equal(want) {
		t.Fatalf("same-day Next = %v, want %v", next, want)
	}
}

func TestScheduleTriggerNextWeeklyPicksTheNextListedDay(t *testing.T) {
	// 2026-11-18 is a Wednesday.
	trigger := ScheduleTrigger{Kind: TriggerWeekly, Time: "08:00", Weekdays: []int{1, 5}, Timezone: "UTC"}
	after := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	fires := trigger.NextFires(after, time.Time{}, 3)
	want := []time.Time{
		time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 23, 8, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC),
	}
	if len(fires) != len(want) {
		t.Fatalf("fires = %v, want %v", fires, want)
	}
	for i := range want {
		if !fires[i].Equal(want[i]) {
			t.Fatalf("fires = %v, want %v", fires, want)
		}
	}
}

func TestScheduleTriggerNextIntervalCountsFromItsAnchor(t *testing.T) {
	anchor := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	trigger := ScheduleTrigger{Kind: TriggerInterval, Every: "6h"}
	next, ok := trigger.Next(anchor.Add(13*time.Hour), anchor)
	if want := anchor.Add(18 * time.Hour); !ok || !next.Equal(want) {
		t.Fatalf("Next = %v, want %v", next, want)
	}
	next, _ = trigger.Next(anchor.Add(12*time.Hour), anchor)
	if want := anchor.Add(18 * time.Hour); !next.Equal(want) {
		t.Fatalf("Next on a fire = %v, want the one after, %v", next, want)
	}
}

func TestScheduleTriggerOnceFiresOnlyBeforeItsTime(t *testing.T) {
	at := time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)
	trigger := ScheduleTrigger{Kind: TriggerOnce, At: at}
	if next, ok := trigger.Next(at.Add(-time.Hour), time.Time{}); !ok || !next.Equal(at) {
		t.Fatalf("Next before = %v, %v", next, ok)
	}
	if _, ok := trigger.Next(at, time.Time{}); ok {
		t.Fatal("a once trigger fired again after its time")
	}
}

func TestCronNext(t *testing.T) {
	for _, test := range []struct {
		expression string
		after      time.Time
		want       time.Time
	}{
		{"*/15 * * * *", time.Date(2026, 11, 18, 10, 7, 30, 0, time.UTC), time.Date(2026, 11, 18, 10, 15, 0, 0, time.UTC)},
		{"0 9 * * mon-fri", time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC), time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)},
		{"30 2 1 * *", time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 2, 30, 0, 0, time.UTC)},
		{"0 0 29 2 *", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2028, 2, 29, 0, 0, 0, 0, time.UTC)},
		{"@daily", time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)},
		{"0 12 * * 7", time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)},
		// Both day fields restricted: either matches, as in cron.
		{"0 0 1 * fri", time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)},
	} {
		trigger := ScheduleTrigger{Kind: TriggerCron, Cron: test.expression, Timezone: "UTC"}
		next, ok := trigger.Next(test.after, time.Time{})
		if !ok || !next.Equal(test.want) {
			t.Errorf("%q after %v = %v, %v; want %v", test.expression, test.after, next, ok, test.want)
		}
	}
}

func TestScheduleValidateRejectsBadInput(t *testing.T) {
	valid := Schedule{ID: "nightly", Repository: "o/r", Prompt: "sweep", Overlap: OverlapSkip, Missed: MissedSkip, Trigger: ScheduleTrigger{Kind: TriggerDaily, Time: "03:00"}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid schedule rejected: %v", err)
	}
	for name, change := range map[string]func(*Schedule){
		"repository": func(s *Schedule) { s.Repository = "nope" },
		"prompt":     func(s *Schedule) { s.Prompt = " " },
		"id":         func(s *Schedule) { s.ID = "a:b" },
		"overlap":    func(s *Schedule) { s.Overlap = "maybe" },
		"missed":     func(s *Schedule) { s.Missed = "all" },
		"kind":       func(s *Schedule) { s.Trigger.Kind = "hourly" },
		"time":       func(s *Schedule) { s.Trigger.Time = "25:00" },
		"timezone":   func(s *Schedule) { s.Trigger.Timezone = "Mars/Olympus" },
		"interval":   func(s *Schedule) { s.Trigger = ScheduleTrigger{Kind: TriggerInterval, Every: "5s"} },
		"cron":       func(s *Schedule) { s.Trigger = ScheduleTrigger{Kind: TriggerCron, Cron: "61 * * * *"} },
		"weekdays":   func(s *Schedule) { s.Trigger = ScheduleTrigger{Kind: TriggerWeekly, Time: "09:00"} },
	} {
		schedule := valid
		change(&schedule)
		if err := schedule.Validate(); err == nil {
			t.Errorf("%s: invalid schedule accepted", name)
		}
	}
}

func TestScheduleAddRunKeepsABoundedHistory(t *testing.T) {
	var schedule Schedule
	start := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	for i := 0; i < ScheduleHistoryLimit+5; i++ {
		schedule.AddRun(ScheduleRun{Fired: start.Add(time.Duration(i) * time.Hour), Status: ScheduleRunOK})
	}
	if len(schedule.History) != ScheduleHistoryLimit {
		t.Fatalf("history has %d entries, want %d", len(schedule.History), ScheduleHistoryLimit)
	}
	last, _ := schedule.LastRun()
	if want := start.Add(time.Duration(ScheduleHistoryLimit+4) * time.Hour); !last.Fired.Equal(want) {
		t.Fatalf("last run fired %v, want %v", last.Fired, want)
	}
}

func TestScheduleTriggerDescribe(t *testing.T) {
	got := ScheduleTrigger{Kind: TriggerWeekly, Time: "09:00", Weekdays: []int{1, 3}, Timezone: "UTC"}.Describe()
	if !strings.Contains(got, "Mon,Wed") || !strings.Contains(got, "09:00") {
		t.Fatalf("Describe = %q", got)
	}
}
