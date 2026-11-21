package clidoctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/clidefs"
)

// TestQuotaTextTellsUntrackedFromUnavailable checks the two empty readings
// stay apart on the line: an agent that names no quota source is not tracked by
// design, while one whose reader could not answer is a problem.
func TestQuotaTextTellsUntrackedFromUnavailable(t *testing.T) {
	for _, test := range []struct {
		report Report
		want   string
	}{
		{report: Report{Quota: "week 40% left", TracksQuota: true}, want: "week 40% left"},
		{report: Report{TracksQuota: true}, want: "unavailable"},
		{report: Report{}, want: "not tracked"},
	} {
		if got := test.report.QuotaText(); got != test.want {
			t.Errorf("%+v.QuotaText() = %q, want %q", test.report, got, test.want)
		}
	}
}

// TestStatusOfConvertsReportFields checks the settings dashboard's agents
// tab reads the same probe results the terminal report renders,
// including the quota error text and the machine-readable status key.
func TestStatusOfConvertsReportFields(t *testing.T) {
	report := Report{
		Name: "codex", Binary: "codex", Path: "/opt/codex",
		Version: "1.2.3", Auth: SignedIn,
		Quota: "week 40% left", TracksQuota: true,
		Models: []string{"codex/gpt-5"}, ModelNote: "note",
	}
	status := StatusOf(report)
	if status.Name != "codex" || !status.Installed || status.Auth != SignedIn {
		t.Fatalf("status = %#v", status)
	}
	if status.Quota != "week 40% left" || !status.TracksQuota {
		t.Fatalf("quota fields = %#v", status)
	}
	if len(status.Models) != 1 || status.Models[0] != "codex/gpt-5" || status.ModelNote != "note" {
		t.Fatalf("model fields = %#v", status)
	}
	if status.Status != "ok" {
		t.Fatalf("status.Status = %q, want %q", status.Status, "ok")
	}
	if status.QuotaError != "" {
		t.Fatalf("quotaError = %q, want empty for a successful read", status.QuotaError)
	}
}

// TestStatusOfReportsQuotaError checks a quota reader's failure survives
// the conversion to JSON, so the agents tab can show why "unavailable" wasn't
// just silence.
func TestStatusOfReportsQuotaError(t *testing.T) {
	report := Report{Name: "codex", Path: "/opt/codex", TracksQuota: true, QuotaErr: errors.New("boom")}
	status := StatusOf(report)
	if status.QuotaError != "boom" {
		t.Fatalf("quotaError = %q, want %q", status.QuotaError, "boom")
	}
	if status.Quota != "unavailable: boom" {
		t.Fatalf("quota = %q", status.Quota)
	}
}

// TestStatusesReportMissingAgentAsSuch checks the status key an
// uninstalled agent's CLI gets, which the dashboard uses to grey it out
// rather than parsing the ASCII marker the terminal report uses.
func TestStatusesReportMissingAgentAsSuch(t *testing.T) {
	registry, err := clidefs.NewRegistry(clidefs.Definition{
		Name: "missing", Binary: "aviary-test-missing-cli",
		Session: clidefs.Session{Assign: clidefs.AssignNone},
		Output:  clidefs.Output{Format: clidefs.FormatText},
		Args:    clidefs.Args{Run: []clidefs.Fragment{{Args: []string{"{prompt}"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	statuses := New(registry).Statuses(context.Background())
	if len(statuses) != len(registry.Names()) {
		t.Fatalf("statuses has %d entries, want %d", len(statuses), len(registry.Names()))
	}
	for _, status := range statuses {
		if status.Installed || status.Status != "missing" {
			t.Fatalf("status for %q = %#v, want missing on a machine with no CLIs installed", status.Name, status)
		}
	}
}

// TestQuotaTextShowsWhyAQuotaIsUnavailable checks the report says why a
// declared quota reader could not answer. A bare "unavailable" hid a Codex
// reader that had been passing a flag the CLI rejects for as long as the flag
// had been wrong; the reason is what makes that visible.
func TestQuotaTextShowsWhyAQuotaIsUnavailable(t *testing.T) {
	failed := (Report{TracksQuota: true, QuotaErr: errors.New("unexpected argument '--stdio' found")}).QuotaText()
	if !strings.Contains(failed, "unavailable") || !strings.Contains(failed, "--stdio") {
		t.Fatalf("quota line = %q, want it to report unavailable and why", failed)
	}
	if got := (Report{TracksQuota: true}).QuotaText(); got != "unavailable" {
		t.Fatalf("quota line without a reason = %q, want %q", got, "unavailable")
	}
	if got := (Report{}).QuotaText(); got != "not tracked" {
		t.Fatalf("quota line for an agent with no source = %q, want %q", got, "not tracked")
	}
}

// TestStatusOfCarriesTheDefaultModel checks the settings dashboard's agents
// tab is told the same default the terminal report prints.
func TestStatusOfCarriesTheDefaultModel(t *testing.T) {
	status := StatusOf(Report{Name: "codex", Path: "/opt/codex", DefaultModel: "gpt-5.6-terra"})
	if status.DefaultModel != "gpt-5.6-terra" {
		t.Fatalf("DefaultModel = %q, want the report's", status.DefaultModel)
	}
}

// TestStaleSkillsNoted checks a stale-skills note core's doctor found is
// carried to the dashboard without turning a healthy CLI's status into a
// warning (#495).
func TestStaleSkillsNoted(t *testing.T) {
	probed := Report{
		Name: "probed", Binary: "probed", Path: "/opt/probed", Auth: SignedIn,
		SkillsNote: "gh-fix stale for aviary v1.5.25; restart aviary to reinstall",
	}
	if status := StatusOf(probed); status.SkillsNote != probed.SkillsNote || status.Status != StatusOK {
		t.Fatalf("status = %+v, want the skills note on an ok status", status)
	}
}

// TestNewReportsStaleSkills checks the production doctor reads the
// skills installed in the user's home, so a stale copy warns in the real app
// and not only when a test sets the note.
func TestNewReportsStaleSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	skill := filepath.Join(home, ".claude", "skills", "gh-fix")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("an old gh-fix\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doctor := New(clidefs.MustBuiltin())
	if note := doctor.SkillsNote("claude"); !strings.HasPrefix(note, "gh-fix stale for ") {
		t.Fatalf("claude skillsNote = %q, want gh-fix reported stale", note)
	}
	if note := doctor.SkillsNote("gemini"); note != "" {
		t.Fatalf("gemini skillsNote = %q, want none with no skills installed", note)
	}
}

// TestReportCarriesProjectsGate checks the doctor reports why agy is
// gated out of Code Loop dispatch, whether or not its CLI is installed.
func TestReportCarriesProjectsGate(t *testing.T) {
	doctor := New(clidefs.MustBuiltin())
	doctor.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	status := StatusOf(doctor.Probe(context.Background(), "agy"))
	if !strings.Contains(status.ProjectsNote, "machine-readable output mode") {
		t.Fatalf("agy projects note = %q, want the gate's reason", status.ProjectsNote)
	}
	if note := StatusOf(doctor.Probe(context.Background(), "claude")).ProjectsNote; note != "" {
		t.Fatalf("claude projects note = %q, want none", note)
	}
}

// TestStatusKeyFollowsTheStateTheRowShows checks the status reads as healthy
// only for a signed-in CLI, warns only for a problem StatusNote names, and
// never reads as healthy when the sign-in state is unknown (#495).
func TestStatusKeyFollowsTheStateTheRowShows(t *testing.T) {
	installed := Report{Name: "claude", Path: "/bin/claude"}
	cases := []struct {
		name      string
		report    Report
		key, note string
	}{
		{"missing", Report{Name: "claude", Auth: SignedIn}, StatusMissing, ""},
		{"signed in", with(installed, func(r *Report) { r.Auth = SignedIn }), StatusOK, ""},
		{"signed in with stale skills", with(installed, func(r *Report) {
			r.Auth = SignedIn
			r.SkillsNote = "gh-fix stale for this aviary dev build"
		}), StatusOK, ""},
		{"unknown", with(installed, func(r *Report) { r.Auth = Unknown }), StatusUnknown, "sign-in state unknown"},
		{"no auth probe", installed, StatusUnknown, "sign-in state unknown"},
		{"signed out", with(installed, func(r *Report) { r.Auth = SignedOut }), StatusWarn, "signed out"},
		{"old version", with(installed, func(r *Report) {
			r.Auth = Unknown
			r.VersionNote = "below the 2.0.0 minimum"
		}), StatusWarn, "version below the 2.0.0 minimum"},
		{"old version signed out", with(installed, func(r *Report) {
			r.Auth = SignedOut
			r.VersionNote = "below the 2.0.0 minimum"
		}), StatusWarn, "version below the 2.0.0 minimum; signed out"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status := StatusOf(c.report)
			if status.Status != c.key || status.StatusNote != c.note {
				t.Fatalf("status = %q, note = %q, want %q, %q", status.Status, status.StatusNote, c.key, c.note)
			}
		})
	}
}

func with(r Report, edit func(*Report)) Report {
	edit(&r)
	return r
}
