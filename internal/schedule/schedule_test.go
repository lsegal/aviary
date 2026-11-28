package schedule

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, f Fields) Spec {
	t.Helper()
	spec, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse(%+v): %v", f, err)
	}
	return spec
}

func fmtTimes(ts []time.Time) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Format(time.RFC3339)
	}
	return out
}

func assertTimes(t *testing.T, got []time.Time, want ...string) {
	t.Helper()
	g := fmtTimes(got)
	if len(g) != len(want) {
		t.Fatalf("got %v, want %v", g, want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Fatalf("got %v, want %v", g, want)
		}
	}
}

func TestInferKind(t *testing.T) {
	cases := []struct {
		f    Fields
		want Kind
	}{
		{Fields{Schedule: "* * * * *"}, KindCron},
		{Fields{Every: "5m"}, KindInterval},
		{Fields{At: "09:00", Days: "mon"}, KindWeekly},
		{Fields{At: "09:00"}, KindDaily},
		{Fields{StartAt: "2026-01-01T00:00:00Z"}, KindOnce},
		{Fields{Trigger: "Daily", Schedule: "* * * * *", At: "09:00"}, KindDaily},
	}
	for _, c := range cases {
		if got := c.f.InferKind(); got != c.want {
			t.Errorf("InferKind(%+v) = %q, want %q", c.f, got, c.want)
		}
	}
	if (Fields{}).HasTrigger() {
		t.Error("empty fields should have no trigger")
	}
}

func TestOnce(t *testing.T) {
	spec := mustParse(t, Fields{Trigger: "once", StartAt: "2026-03-01T09:30", Timezone: "America/Toronto"})
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	assertTimes(t, spec.Preview(from, 3), "2026-03-01T09:30:00-05:00")
	if got := spec.Next(spec.StartAt); !got.IsZero() {
		t.Fatalf("once trigger fired again at %v", got)
	}
}

func TestInterval(t *testing.T) {
	spec := mustParse(t, Fields{Every: "90m", StartAt: "2026-01-01T00:00:00Z"})
	from := time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC)
	assertTimes(t, spec.Preview(from, 3), "2026-01-01T03:00:00Z", "2026-01-01T04:30:00Z", "2026-01-01T06:00:00Z")

	unanchored := mustParse(t, Fields{Every: "1d"})
	if got := unanchored.Next(from); !got.Equal(from.Add(24 * time.Hour)) {
		t.Fatalf("unanchored Next = %v", got)
	}
	if _, err := Parse(Fields{Every: "500ms"}); err == nil {
		t.Fatal("expected sub-second interval to be rejected")
	}
}

func TestDailyAcrossDST(t *testing.T) {
	spec := mustParse(t, Fields{At: "09:00, 02:30", Timezone: "America/Toronto"})
	// 2026-03-08 is the spring-forward day in Toronto: 02:30 does not exist.
	from := time.Date(2026, 3, 7, 15, 0, 0, 0, time.UTC)
	got := fmtTimes(spec.Preview(from, 3))
	want := []string{"2026-03-08T03:30:00-04:00", "2026-03-08T09:00:00-04:00", "2026-03-09T02:30:00-04:00"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestWeekly(t *testing.T) {
	spec := mustParse(t, Fields{Trigger: "weekly", Days: "Friday,mon", At: "08:15", Timezone: "UTC"})
	from := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC) // Wednesday
	assertTimes(t, spec.Preview(from, 3), "2026-11-20T08:15:00Z", "2026-11-23T08:15:00Z", "2026-11-27T08:15:00Z")
	if _, err := Parse(Fields{Trigger: "weekly", At: "08:15"}); err == nil {
		t.Fatal("expected weekly without days to be rejected")
	}
	if _, err := Parse(Fields{Trigger: "weekly", At: "08:15", Days: "funday"}); err == nil {
		t.Fatal("expected unknown day to be rejected")
	}
}

func TestCronInTimezone(t *testing.T) {
	spec := mustParse(t, Fields{Schedule: "0 9 * * *", Timezone: "Asia/Tokyo"})
	from := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	assertTimes(t, spec.Preview(from, 2), "2026-11-19T09:00:00+09:00", "2026-11-20T09:00:00+09:00")
}

func TestCronDescribeIncludesExpression(t *testing.T) {
	spec := mustParse(t, Fields{Schedule: "0 * * * *", Timezone: "America/Los_Angeles"})
	if got, want := spec.Describe(), "cron 0 * * * * (America/Los_Angeles)"; got != want {
		t.Fatalf("Describe() = %q, want %q", got, want)
	}
}

func TestStartAtBoundsRecurring(t *testing.T) {
	spec := mustParse(t, Fields{At: "09:00", StartAt: "2026-12-01T09:00:00Z", Timezone: "UTC"})
	from := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	assertTimes(t, spec.Preview(from, 2), "2026-12-01T09:00:00Z", "2026-12-02T09:00:00Z")
}

func TestRunOncePreviewsOneFire(t *testing.T) {
	spec := mustParse(t, Fields{Schedule: "*/5 * * * *", RunOnce: true})
	if got := spec.Preview(time.Now(), 3); len(got) != 1 {
		t.Fatalf("run_once preview = %v", got)
	}
}

func TestPolicies(t *testing.T) {
	spec := mustParse(t, Fields{Every: "1h"})
	if spec.Overlap != OverlapQueue || spec.Missed != MissedSkip {
		t.Fatalf("defaults = %q/%q", spec.Overlap, spec.Missed)
	}
	spec = mustParse(t, Fields{Every: "1h", Overlap: "SKIP", Missed: "run_once"})
	if spec.Overlap != OverlapSkip || spec.Missed != MissedRunOnce {
		t.Fatalf("parsed = %q/%q", spec.Overlap, spec.Missed)
	}
	for _, f := range []Fields{
		{Every: "1h", Overlap: "parallel"},
		{Every: "1h", Missed: "all"},
		{Every: "1h", Timezone: "Mars/Olympus"},
		{Trigger: "hourly"},
		{Trigger: "once"},
		{Trigger: "cron"},
		{Schedule: "not cron"},
		{At: "25:00"},
	} {
		if _, err := Parse(f); err == nil {
			t.Errorf("Parse(%+v) succeeded, want error", f)
		}
	}
}

func TestRunsIn(t *testing.T) {
	from := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	month := 30 * 24 * time.Hour
	cases := []struct {
		name string
		f    Fields
		want int
	}{
		{"interval", Fields{Every: "30m", Timezone: "UTC"}, 1440},
		{"daily twice", Fields{At: "09:00,17:00", Timezone: "UTC"}, 60},
		{"weekly", Fields{Days: "mon,wed", At: "09:00", Timezone: "UTC"}, 9},
		{"cron hourly", Fields{Schedule: "0 * * * *", Timezone: "UTC"}, 720},
		{"cron every second", Fields{Schedule: "* * * * * *", Timezone: "UTC"}, 2592000},
		{"future start", Fields{At: "09:00", StartAt: "2027-01-01T00:00:00Z", Timezone: "UTC"}, 30},
		{"once", Fields{Trigger: "once", StartAt: "2027-01-01T00:00:00Z", Timezone: "UTC"}, 0},
		{"run once", Fields{Every: "1h", RunOnce: true, Timezone: "UTC"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustParse(t, tc.f).RunsIn(from, month); got != tc.want {
				t.Errorf("RunsIn = %d, want %d", got, tc.want)
			}
		})
	}
}

func wantErr(t *testing.T, err error, want string) {
	t.Helper()
	got := "<nil>"
	if err != nil {
		got = err.Error()
	}
	if got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

func TestCheckApplicable(t *testing.T) {
	wantErr(t, Fields{Every: "1h", StartAt: "2026-01-01T00:00"}.CheckApplicable(), "<nil>")
	wantErr(t, Fields{Days: "mon", At: "09:00"}.CheckApplicable(), "<nil>")
	wantErr(t, Fields{Schedule: "0 9 * * *", Every: "1h", At: "09:00"}.CheckApplicable(),
		"every and at do not apply to the cron trigger that schedule selects; remove them or choose a different trigger")
	wantErr(t, Fields{Trigger: "once", StartAt: "2099-01-01T00:00", Days: "mon"}.CheckApplicable(),
		"days does not apply to trigger=once; remove it or choose a different trigger")
}

func TestCheckFuture(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	once := Spec{Kind: KindOnce, StartAt: now.Add(-time.Minute), Location: time.UTC}
	wantErr(t, once.CheckFuture(now), "start_at 2026-06-01 11:59 is in the past, so this once trigger would never fire")
	once.StartAt = now.Add(time.Minute)
	wantErr(t, once.CheckFuture(now), "<nil>")
	// Recurring triggers may start in the past.
	wantErr(t, Spec{Kind: KindInterval, StartAt: now.Add(-time.Hour)}.CheckFuture(now), "<nil>")
}
