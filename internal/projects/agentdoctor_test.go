package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/clidoctor"
)

// TestBuiltinAgentsAskTheirCLIForModels holds every shipped agent that exposes
// a model catalog to reading it from the CLI, which avoids carrying stale
// lists. Claude Code has no listing command, so its maintained aliases are the
// exception and remain suggestions rather than an allow-list.
func TestBuiltinAgentsAskTheirCLIForModels(t *testing.T) {
	registry := clidefs.MustBuiltin()
	for _, name := range registry.Names() {
		definition, ok := registry.Lookup(name)
		if !ok {
			t.Fatalf("registry has no definition for %q", name)
		}
		if len(definition.Doctor.KnownModels) > 0 {
			if name != "claude" || len(definition.Doctor.Models) > 0 {
				t.Errorf("built-in %q hardcodes doctor.knownModels despite having a model probe", name)
			}
			if got := strings.Join(definition.Doctor.KnownModels, ","); got != "opus,sonnet,haiku,claude-opus-5-5" {
				t.Errorf("Claude known models = %q, want opus,sonnet,haiku,claude-opus-5-5", got)
			}
			continue
		}
		if len(definition.Doctor.Models) > 0 || definition.Models.Declared() {
			continue
		}
		if strings.TrimSpace(definition.Doctor.ModelsNote) == "" {
			t.Errorf("built-in %q neither probes its CLI for models nor says why it cannot", name)
		}
	}
}

// TestBuiltinModelProbesReadTheirOwnAnswers checks each shipped probe is
// declared whole: a command that only answers a conversation names the lines
// it is sent, and one that answers in JSON names where the ids are, because a
// probe missing either half runs and extracts nothing.
func TestBuiltinModelProbesReadTheirOwnAnswers(t *testing.T) {
	registry := clidefs.MustBuiltin()
	for _, name := range registry.Names() {
		definition, _ := registry.Lookup(name)
		doctor := definition.Doctor
		if len(doctor.Models) == 0 {
			continue
		}
		if len(doctor.ModelsStdin) > 0 && strings.TrimSpace(doctor.ModelsJSON) == "" {
			t.Errorf("built-in %q talks to its CLI but declares no doctor.modelsJSON to read the reply", name)
		}
		if strings.TrimSpace(doctor.ModelsJSON) == "" && strings.TrimSpace(doctor.ModelPattern) == "" {
			t.Errorf("built-in %q declares a models probe with no way to read its output", name)
		}
	}
}

// TestDescribeQuotaTellsUntrackedFromUnavailable checks the two empty readings
// stay apart on the line: an agent that names no quota source is not tracked by
// design, while one whose reader could not answer is a problem.
func TestDescribeQuotaTellsUntrackedFromUnavailable(t *testing.T) {
	for _, test := range []struct {
		report clidoctor.Report
		want   string
	}{
		{report: clidoctor.Report{Quota: "week 40% left", TracksQuota: true}, want: "week 40% left"},
		{report: clidoctor.Report{TracksQuota: true}, want: "unavailable"},
		{report: clidoctor.Report{}, want: "not tracked"},
	} {
		if got := describeQuota(test.report); got != test.want {
			t.Errorf("describeQuota(%+v) = %q, want %q", test.report, got, test.want)
		}
	}
}

// TestAgentStatusConvertsReportFields checks the settings dashboard's agents
// tab reads the same probe results the terminal report renders,
// including the quota error text and the machine-readable status key.
func TestAgentStatusConvertsReportFields(t *testing.T) {
	report := clidoctor.Report{
		Name: "codex", Binary: "codex", Path: "/opt/codex",
		Version: "1.2.3", Auth: clidoctor.SignedIn,
		Quota: "week 40% left", TracksQuota: true,
		Models: []string{"codex/gpt-5"}, ModelNote: "note",
	}
	status := agentStatus(report)
	if status.Name != "codex" || !status.Installed || status.Auth != clidoctor.SignedIn {
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

// TestAgentStatusReportsQuotaError checks a quota reader's failure survives
// the conversion to JSON, so the agents tab can show why "unavailable" wasn't
// just silence.
func TestAgentStatusReportsQuotaError(t *testing.T) {
	report := clidoctor.Report{Name: "codex", Path: "/opt/codex", TracksQuota: true, QuotaErr: errors.New("boom")}
	status := agentStatus(report)
	if status.QuotaError != "boom" {
		t.Fatalf("quotaError = %q, want %q", status.QuotaError, "boom")
	}
	if status.Quota != "unavailable: boom" {
		t.Fatalf("quota = %q", status.Quota)
	}
}

// TestAgentStatusesReportsMissingAgentAsSuch checks the status key an
// uninstalled agent's CLI gets, which the dashboard uses to grey it out
// rather than parsing the ASCII marker the terminal report uses.
func TestAgentStatusesReportsMissingAgentAsSuch(t *testing.T) {
	registry, err := clidefs.NewRegistry(clidefs.Definition{
		Name: "missing", Binary: "aviary-test-missing-cli",
		Session: clidefs.Session{Assign: clidefs.AssignNone},
		Output:  clidefs.Output{Format: clidefs.FormatText},
		Args:    clidefs.Args{Run: []clidefs.Fragment{{Args: []string{"{prompt}"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := agentStatuses(context.Background(), registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != len(registry.Names()) {
		t.Fatalf("statuses has %d entries, want %d", len(statuses), len(registry.Names()))
	}
	for _, status := range statuses {
		if status.Installed || status.Status != "missing" {
			t.Fatalf("status for %q = %#v, want missing on a machine with no CLIs installed", status.Name, status)
		}
	}
}

// TestWriteAgentReportsRendersEveryField checks the rendered report carries
// what the command exists to show, with each model on a line of its own so it
// can be copied straight into --agent.
func TestWriteAgentReportsRendersEveryField(t *testing.T) {
	var out strings.Builder
	writeAgentReports(&out, []clidoctor.Report{
		{
			Name: "codex", Binary: "codex", Path: "/opt/codex", Version: "1.2.3",
			Auth: clidoctor.SignedIn, Quota: "week 40% left", TracksQuota: true,
			Models: []string{"codex/gpt-5.6", "codex/gpt-5.6-codex"},
		},
		{
			Name: "cline", Binary: "cline", Path: "/opt/cline", Auth: clidoctor.Unknown,
			Models: []string{"cline/anthropic/claude-opus-5"}, ModelNote: clidoctor.KnownModelsNote,
		},
		{Name: "muse", Binary: "muse", ModelNote: "any model the CLI accepts"},
		{Name: "agy", Binary: "agy", ProjectsNote: "no machine-readable output mode"},
	})
	text := out.String()
	for _, want := range []string{
		"projects  unavailable: no machine-readable output mode",
		statusReady + " codex", "/opt/codex, version 1.2.3", "signed in", "week 40% left",
		"2 available", "1 " + clidoctor.KnownModelsNote, "cline/anthropic/claude-opus-5", "\n" + strings.Repeat(" ", 12) + "codex/gpt-5.6\n",
		statusMissing + "  muse", "muse (not installed)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report does not contain %q:\n%s", want, text)
		}
	}
	// A missing binary reports nothing beyond that: the remaining fields were
	// never probed, so printing them would be inventing answers.
	muse := text[strings.Index(text, statusMissing):strings.Index(text, "agy")]
	if strings.Contains(muse, "auth") || strings.Contains(muse, "quota") {
		t.Errorf("report prints unprobed fields for a missing binary:\n%s", muse)
	}
}

// TestAgentReportShowsWhyAQuotaIsUnavailable checks the report says why a
// declared quota reader could not answer. A bare "unavailable" hid a Codex
// reader that had been passing a flag the CLI rejects for as long as the flag
// had been wrong; the reason is what makes that visible.
func TestAgentReportShowsWhyAQuotaIsUnavailable(t *testing.T) {
	failed := describeQuota(clidoctor.Report{TracksQuota: true, QuotaErr: errors.New("unexpected argument '--stdio' found")})
	if !strings.Contains(failed, "unavailable") || !strings.Contains(failed, "--stdio") {
		t.Fatalf("quota line = %q, want it to report unavailable and why", failed)
	}
	if got := describeQuota(clidoctor.Report{TracksQuota: true}); got != "unavailable" {
		t.Fatalf("quota line without a reason = %q, want %q", got, "unavailable")
	}
	if got := describeQuota(clidoctor.Report{}); got != "not tracked" {
		t.Fatalf("quota line for an agent with no source = %q, want %q", got, "not tracked")
	}
}

// TestClineAndGeminiAskOverTheAgentProtocol checks the two CLIs with no
// listing subcommand are asked the way they can be asked -- an ACP handshake
// whose session answers with its available models -- rather than carrying a
// provider-scoped list that goes stale, which is what issue #566 was.
func TestClineAndGeminiAskOverTheAgentProtocol(t *testing.T) {
	registry := clidefs.MustBuiltin()
	for _, name := range []string{"cline", "gemini"} {
		definition, ok := registry.Lookup(name)
		if !ok {
			t.Fatalf("registry has no definition for %q", name)
		}
		argv := definition.Doctor.ModelsArgv(name)
		if len(argv) == 0 || argv[len(argv)-1] != "--acp" {
			t.Errorf("%s models probe = %q, want the ACP mode its CLI answers in", name, argv)
		}
		stdin := strings.Join(definition.Doctor.ModelsStdinLines(name), " ")
		if !strings.Contains(stdin, "initialize") || !strings.Contains(stdin, "session/new") {
			t.Errorf("%s probe writes %q, want the ACP handshake that produces a model list", name, stdin)
		}
	}
}

// TestAgentReportShowsTheManagedDefaultModel checks `aviary agents` names the
// model an agent runs with when --agent gives it none, qualified
// so it can be copied straight back into --agent, and says nothing extra for an
// agent whose definition leaves the choice to its own CLI.
func TestAgentReportShowsTheManagedDefaultModel(t *testing.T) {
	var out strings.Builder
	writeAgentReports(&out, []clidoctor.Report{
		{
			Name: "codex", Binary: "codex", Path: "/opt/codex", Auth: clidoctor.SignedIn,
			DefaultModel: "gpt-5.6-terra", Models: []string{"codex/gpt-5.6-terra"},
		},
		{Name: "gemini", Binary: "gemini", Path: "/opt/gemini", Auth: clidoctor.SignedIn},
	})
	text := out.String()
	if !strings.Contains(text, "default   codex/gpt-5.6-terra") {
		t.Errorf("report does not name the managed default:\n%s", text)
	}
	gemini := text[strings.Index(text, "gemini"):]
	if strings.Contains(gemini, "default") {
		t.Errorf("report invents a default for an agent declaring none:\n%s", gemini)
	}
}

// TestAgentStatusCarriesTheDefaultModel checks the settings dashboard's agents
// tab is told the same default the terminal report prints.
func TestAgentStatusCarriesTheDefaultModel(t *testing.T) {
	status := agentStatus(clidoctor.Report{Name: "codex", Path: "/opt/codex", DefaultModel: "gpt-5.6-terra"})
	if status.DefaultModel != "gpt-5.6-terra" {
		t.Fatalf("DefaultModel = %q, want the report's", status.DefaultModel)
	}
}

// TestAgentReportShowsStaleSkills checks a stale-skills note core's doctor
// found is printed on the report and carried to the dashboard as a warning.
func TestAgentReportShowsStaleSkills(t *testing.T) {
	probed := clidoctor.Report{
		Name: "probed", Binary: "probed", Path: "/opt/probed", Auth: clidoctor.SignedIn,
		SkillsNote: "gh-fix stale for aviary v1.5.25; restart aviary to reinstall",
	}
	if status(probed) != statusWarn {
		t.Fatalf("status = %q, want a warning", status(probed))
	}
	var out strings.Builder
	writeAgentReports(&out, []clidoctor.Report{probed})
	if !strings.Contains(out.String(), "gh-fix stale for aviary v1.5.25") {
		t.Fatalf("report = %q, want the skills line", out.String())
	}
	if status := agentStatus(probed); status.SkillsNote != probed.SkillsNote || status.Status != "warn" {
		t.Fatalf("status = %+v, want the skills note and a warning", status)
	}
}

// TestNewAgentDoctorReportsStaleSkills checks the production doctor reads the
// skills installed in the user's home, so a stale copy warns in the real app
// and not only when a test sets the note.
func TestNewAgentDoctorReportsStaleSkills(t *testing.T) {
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
	doctor := newAgentDoctor(clidefs.MustBuiltin())
	if note := doctor.SkillsNote("claude"); !strings.HasPrefix(note, "gh-fix stale for ") {
		t.Fatalf("claude skillsNote = %q, want gh-fix reported stale", note)
	}
	if note := doctor.SkillsNote("gemini"); note != "" {
		t.Fatalf("gemini skillsNote = %q, want none with no skills installed", note)
	}
}

// TestAgentReportCarriesProjectsGate checks the doctor reports why agy is
// gated out of Projects dispatch, whether or not its CLI is installed.
func TestAgentReportCarriesProjectsGate(t *testing.T) {
	doctor := newAgentDoctor(clidefs.MustBuiltin())
	doctor.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	status := agentStatus(doctor.Probe(context.Background(), "agy"))
	if !strings.Contains(status.ProjectsNote, "machine-readable output mode") {
		t.Fatalf("agy projects note = %q, want the gate's reason", status.ProjectsNote)
	}
	if note := agentStatus(doctor.Probe(context.Background(), "claude")).ProjectsNote; note != "" {
		t.Fatalf("claude projects note = %q, want none", note)
	}
}

// TestDoctorAndDispatchResolveTheSameOverride checks a cli_definitions binary
// override off PATH is reported installed at that path, and that jobs run the
// same executable (#456).
func TestDoctorAndDispatchResolveTheSameOverride(t *testing.T) {
	const override = "/opt/nightly/codex"
	registry, err := LoadCLIDefinitions(map[string]any{"codex": map[string]any{"binary": override}})
	if err != nil {
		t.Fatal(err)
	}
	doctor := newAgentDoctor(registry)
	doctor.LookPath = func(binary string) (string, error) {
		if binary != override {
			return "", errors.New("not found")
		}
		return binary, nil
	}
	doctor.Version = func(context.Context, string) ([]byte, error) { return nil, errors.New("no version") }
	doctor.Run = func(context.Context, []string, clidefs.Doctor, []string) ([]byte, bool) { return nil, false }
	doctor.Quota = func(context.Context, string, string) (string, error) { return "", nil }
	doctor.SkillsNote = nil
	status := agentStatus(doctor.Probe(context.Background(), "codex"))
	if status.Path != override || status.Binary != override {
		t.Fatalf("doctor binary = %q, path = %q, want the override %q reported installed", status.Binary, status.Path, override)
	}
	runner := CommandRunner{Agent: "codex", Definitions: registry}
	if got := runner.binary("codex"); got != status.Path {
		t.Fatalf("dispatch binary = %q, want the doctor's %q", got, status.Path)
	}
}
