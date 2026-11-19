package projects

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/agents"
)

// testDoctorRegistry builds a registry of agents with the probe blocks the
// report reads, without touching the built-in definitions: the report has to
// work for an agent declared in a config file too.
func testDoctorRegistry(t *testing.T) *agents.Registry {
	t.Helper()
	base := func(name string) agents.Definition {
		return agents.Definition{
			Name: name, Binary: name,
			Session: agents.Session{Assign: agents.AssignNone},
			Output:  agents.Output{Format: agents.FormatText},
			Args:    agents.Args{Run: []agents.Fragment{{Args: []string{"{prompt}"}}}},
		}
	}
	probed := base("probed")
	probed.Doctor = agents.Doctor{
		Auth:   []string{"{binary}", "login", "status"},
		Models: []string{"{binary}", "models"},
	}
	probed.Quota = agents.Quota{Reader: agents.QuotaClaude}
	probed.Skills = agents.Skills{Target: "probed"}
	declared := base("declared")
	declared.Models = agents.NewAllowList("opus", "sonnet")
	silent := base("silent")
	known := base("known")
	known.Doctor = agents.Doctor{KnownModels: []string{"opus", "sonnet"}}
	noted := base("noted")
	noted.Doctor = agents.Doctor{KnownModels: []string{"vendor/one"}, ModelsNote: "known for the default provider"}
	nomodel := base("nomodel")
	nomodel.Models = agents.NewAllowList()
	old := base("old")
	old.MinVersion = "2.0.0"
	registry, err := agents.NewRegistry(probed, declared, silent, known, noted, nomodel, old)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return registry
}

// stubDoctor wires a doctor whose every probe is answered in-process, so the
// report is testable on a machine with no agent CLI installed.
func stubDoctor(t *testing.T, installed map[string]bool, version, auth, models func(string) (string, bool), quota map[string]string) *agentDoctor {
	t.Helper()
	registry := testDoctorRegistry(t)
	return &agentDoctor{
		registry: registry,
		lookPath: func(binary string) (string, error) {
			if !installed[binary] {
				return "", errors.New("not found")
			}
			return "/opt/" + binary, nil
		},
		version: func(_ context.Context, binary string) ([]byte, error) {
			text, ok := version(binary)
			if !ok {
				return nil, errors.New("no version")
			}
			return []byte(text), nil
		},
		run: func(_ context.Context, argv []string, _ agents.Doctor, _ []string) ([]byte, bool) {
			probe := auth
			if argv[len(argv)-1] == "models" {
				probe = models
			}
			text, ok := probe(argv[0])
			return []byte(text), ok
		},
		quotaFor: func(_ context.Context, name, _ string) (string, error) { return quota[name], nil },
	}
}

// reportFor finds one agent's entry in a report.
func reportFor(t *testing.T, reports []agentReport, name string) agentReport {
	t.Helper()
	for _, report := range reports {
		if report.name == name {
			return report
		}
	}
	t.Fatalf("report has no entry for %q", name)
	return agentReport{}
}

// noProbe answers every probe with a failure, for the agents a test is not
// about.
func noProbe(string) (string, bool) { return "", false }

// TestAgentDoctorReportsEveryRegisteredAgent checks the report covers the whole
// registry in its sorted order, so an agent a config file adds is reported
// beside the built-ins rather than left out of the listing it belongs to.
func TestAgentDoctorReportsEveryRegisteredAgent(t *testing.T) {
	doctor := stubDoctor(t, nil, noProbe, noProbe, noProbe, nil)
	reports := doctor.Report(context.Background())
	if len(reports) != len(doctor.registry.Names()) {
		t.Fatalf("report has %d entries, want %d", len(reports), len(doctor.registry.Names()))
	}
	for i, name := range doctor.registry.Names() {
		if reports[i].name != name {
			t.Fatalf("report[%d] = %q, want %q in registry order", i, reports[i].name, name)
		}
	}
}

// TestAgentDoctorSkipsProbesForAMissingBinary checks nothing is run for an
// agent whose CLI is not installed: every remaining question is about that
// binary, and running them would only produce the same failure five times.
func TestAgentDoctorSkipsProbesForAMissingBinary(t *testing.T) {
	ran := false
	probe := func(string) (string, bool) { ran = true; return "", true }
	doctor := stubDoctor(t, nil, probe, probe, probe, map[string]string{"probed": "week 50% left"})
	doctor.quotaFor = func(context.Context, string, string) (string, error) { ran = true; return "", nil }
	report := reportFor(t, doctor.Report(context.Background()), "probed")
	if ran {
		t.Error("a probe ran for an agent whose binary is not installed")
	}
	if report.installed() || report.status() != statusMissing {
		t.Errorf("status = %q, want %q for a missing binary", report.status(), statusMissing)
	}
	if report.auth != doctorUnknown {
		t.Errorf("auth = %q, want %q for a missing binary", report.auth, doctorUnknown)
	}
}

// TestAgentDoctorReadsTheAuthProbe checks the definition's own probe decides
// the sign-in state, including the CLI that reports a signed-out account on a
// zero exit status.
func TestAgentDoctorReadsTheAuthProbe(t *testing.T) {
	for _, test := range []struct {
		name   string
		output string
		ok     bool
		want   string
	}{
		{name: "signed in", output: "Logged in using ChatGPT", ok: true, want: doctorSignedIn},
		{name: "probe failed", output: "not authenticated", want: doctorSignedOut},
	} {
		t.Run(test.name, func(t *testing.T) {
			auth := func(string) (string, bool) { return test.output, test.ok }
			doctor := stubDoctor(t, map[string]bool{"probed": true}, noProbe, auth, noProbe, nil)
			if got := reportFor(t, doctor.Report(context.Background()), "probed").auth; got != test.want {
				t.Errorf("auth = %q, want %q", got, test.want)
			}
		})
	}
}

// TestAgentDoctorFallsBackToTheQuotaReadingForAuth checks an agent with no
// sign-in probe of its own is reported as signed in when its quota could be
// read, because every quota reader asks the CLI something only a signed-in
// account can answer, and as unknown when there is no evidence either way.
func TestAgentDoctorFallsBackToTheQuotaReadingForAuth(t *testing.T) {
	installed := map[string]bool{"silent": true}
	withQuota := stubDoctor(t, installed, noProbe, noProbe, noProbe, map[string]string{"silent": "week 40% left"})
	if got := reportFor(t, withQuota.Report(context.Background()), "silent").auth; got != doctorSignedIn {
		t.Errorf("auth = %q, want %q when a quota was read", got, doctorSignedIn)
	}
	without := stubDoctor(t, installed, noProbe, noProbe, noProbe, nil)
	if got := reportFor(t, without.Report(context.Background()), "silent").auth; got != doctorUnknown {
		t.Errorf("auth = %q, want %q with no evidence either way", got, doctorUnknown)
	}
}

// TestAgentDoctorQualifiesProbedModels checks the models a CLI lists are
// rendered as the agent/model names --agent takes, which is the whole reason
// the report lists them.
func TestAgentDoctorQualifiesProbedModels(t *testing.T) {
	models := func(string) (string, bool) { return "openai/gpt-5.6\nanthropic/claude-opus-5\n", true }
	doctor := stubDoctor(t, map[string]bool{"probed": true}, noProbe, noProbe, models, nil)
	report := reportFor(t, doctor.Report(context.Background()), "probed")
	want := []string{"probed/openai/gpt-5.6", "probed/anthropic/claude-opus-5"}
	if strings.Join(report.models, ",") != strings.Join(want, ",") {
		t.Errorf("models = %v, want %v", report.models, want)
	}
}

// TestAgentDoctorFallsBackToDeclaredModels checks the three things a definition
// alone can say about models, which stay distinguishable in the report: an
// allow-list, an agent that takes no model, and one that validates any.
func TestAgentDoctorFallsBackToDeclaredModels(t *testing.T) {
	doctor := stubDoctor(t, map[string]bool{"declared": true, "nomodel": true, "silent": true}, noProbe, noProbe, noProbe, nil)
	reports := doctor.Report(context.Background())
	declared := reportFor(t, reports, "declared")
	if strings.Join(declared.models, ",") != "declared/opus,declared/sonnet" {
		t.Errorf("declared models = %v, want the allow-list qualified", declared.models)
	}
	if got := reportFor(t, reports, "nomodel"); len(got.models) != 0 || !strings.Contains(got.modelNote, "not accepted") {
		t.Errorf("nomodel = %v/%q, want no models and a note", got.models, got.modelNote)
	}
	if got := reportFor(t, reports, "silent"); len(got.models) != 0 || !strings.Contains(got.modelNote, "any model") {
		t.Errorf("silent = %v/%q, want no models and a note", got.models, got.modelNote)
	}
}

// TestAgentDoctorListsKnownModels checks a definition that names no allow-list
// but knows what its CLI takes is enumerated rather than dismissed with the
// note that any model is accepted, which is the whole point of the field
//. The list is labelled as aviary's rather than the CLI's, because
// it is not an allow-list: a model released after the definition still runs.
func TestAgentDoctorListsKnownModels(t *testing.T) {
	doctor := stubDoctor(t, map[string]bool{"known": true}, noProbe, noProbe, noProbe, nil)
	report := reportFor(t, doctor.Report(context.Background()), "known")
	if strings.Join(report.models, ",") != "known/opus,known/sonnet" {
		t.Errorf("known models = %v, want the known list qualified", report.models)
	}
	if !strings.Contains(report.modelNote, "known to aviary") {
		t.Errorf("known note = %q, want it to say the list is aviary's", report.modelNote)
	}
	registry := testDoctorRegistry(t)
	definition, _ := registry.Lookup("known")
	if !definition.AcceptsModel("a-model-released-this-morning") {
		t.Error("knownModels rejects an unlisted model, want it to stay a hint rather than an allow-list")
	}
}

// TestBuiltinAgentsAskTheirCLIForModels holds every shipped agent that exposes
// a model catalog to reading it from the CLI, which avoids carrying stale
// lists. Claude Code has no listing command, so its maintained aliases are the
// exception and remain suggestions rather than an allow-list.
func TestBuiltinAgentsAskTheirCLIForModels(t *testing.T) {
	registry := agents.MustBuiltin()
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
	registry := agents.MustBuiltin()
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

// TestAgentDoctorPrefersProbedModelsOverKnownOnes checks a live listing wins:
// the known list exists for the CLIs that have no listing command, not to
// override one that does.
func TestAgentDoctorPrefersProbedModelsOverKnownOnes(t *testing.T) {
	models := func(string) (string, bool) { return "gpt-5.6\n", true }
	doctor := stubDoctor(t, map[string]bool{"probed": true}, noProbe, noProbe, models, nil)
	report := reportFor(t, doctor.Report(context.Background()), "probed")
	if strings.Join(report.models, ",") != "probed/gpt-5.6" || report.modelNote != "" {
		t.Errorf("probed = %v/%q, want the probed list with no note", report.models, report.modelNote)
	}
}

// TestAgentDoctorFlagsAnOldBinary checks a CLI below its definition's declared
// minimum is called out rather than reported as ready, which is the whole point
// of declaring the minimum.
func TestAgentDoctorFlagsAnOldBinary(t *testing.T) {
	version := func(string) (string, bool) { return "1.4.0", true }
	doctor := stubDoctor(t, map[string]bool{"old": true}, version, noProbe, noProbe, nil)
	report := reportFor(t, doctor.Report(context.Background()), "old")
	if report.version != "1.4.0" {
		t.Errorf("version = %q, want the reported one", report.version)
	}
	if !strings.Contains(report.versionNote, "2.0.0") || report.status() != statusWarn {
		t.Errorf("versionNote = %q, status = %q, want a warning naming the minimum", report.versionNote, report.status())
	}
}

// TestDescribeQuotaTellsUntrackedFromUnavailable checks the two empty readings
// stay apart on the line: an agent that names no quota source is not tracked by
// design, while one whose reader could not answer is a problem.
func TestDescribeQuotaTellsUntrackedFromUnavailable(t *testing.T) {
	for _, test := range []struct {
		report agentReport
		want   string
	}{
		{report: agentReport{quota: "week 40% left", tracksQuota: true}, want: "week 40% left"},
		{report: agentReport{tracksQuota: true}, want: "unavailable"},
		{report: agentReport{}, want: "not tracked"},
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
	report := agentReport{
		name: "codex", binary: "codex", path: "/opt/codex",
		version: "1.2.3", auth: doctorSignedIn,
		quota: "week 40% left", tracksQuota: true,
		models: []string{"codex/gpt-5"}, modelNote: "note",
	}
	status := agentStatus(report)
	if status.Name != "codex" || !status.Installed || status.Auth != doctorSignedIn {
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
	report := agentReport{name: "codex", path: "/opt/codex", tracksQuota: true, quotaErr: errors.New("boom")}
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
	doctor := stubDoctor(t, nil, noProbe, noProbe, noProbe, nil)
	statuses, err := agentStatuses(context.Background(), doctor.registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != len(doctor.registry.Names()) {
		t.Fatalf("statuses has %d entries, want %d", len(statuses), len(doctor.registry.Names()))
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
	writeAgentReports(&out, []agentReport{
		{
			name: "codex", binary: "codex", path: "/opt/codex", version: "1.2.3",
			auth: doctorSignedIn, quota: "week 40% left", tracksQuota: true,
			models: []string{"codex/gpt-5.6", "codex/gpt-5.6-codex"},
		},
		{
			name: "cline", binary: "cline", path: "/opt/cline", auth: doctorUnknown,
			models: []string{"cline/anthropic/claude-opus-5"}, modelNote: knownModelsNote,
		},
		{name: "muse", binary: "muse", modelNote: "any model the CLI accepts"},
	})
	text := out.String()
	for _, want := range []string{
		statusReady + " codex", "/opt/codex, version 1.2.3", "signed in", "week 40% left",
		"2 available", "1 " + knownModelsNote, "cline/anthropic/claude-opus-5", "\n" + strings.Repeat(" ", 12) + "codex/gpt-5.6\n",
		statusMissing + "  muse", "muse (not installed)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report does not contain %q:\n%s", want, text)
		}
	}
	// A missing binary reports nothing beyond that: the remaining fields were
	// never probed, so printing them would be inventing answers.
	muse := text[strings.Index(text, statusMissing):]
	if strings.Contains(muse, "auth") || strings.Contains(muse, "quota") {
		t.Errorf("report prints unprobed fields for a missing binary:\n%s", muse)
	}
}

// TestAgentReportShowsWhyAQuotaIsUnavailable checks the report says why a
// declared quota reader could not answer. A bare "unavailable" hid a Codex
// reader that had been passing a flag the CLI rejects for as long as the flag
// had been wrong; the reason is what makes that visible.
func TestAgentReportShowsWhyAQuotaIsUnavailable(t *testing.T) {
	failed := describeQuota(agentReport{tracksQuota: true, quotaErr: errors.New("unexpected argument '--stdio' found")})
	if !strings.Contains(failed, "unavailable") || !strings.Contains(failed, "--stdio") {
		t.Fatalf("quota line = %q, want it to report unavailable and why", failed)
	}
	if got := describeQuota(agentReport{tracksQuota: true}); got != "unavailable" {
		t.Fatalf("quota line without a reason = %q, want %q", got, "unavailable")
	}
	if got := describeQuota(agentReport{}); got != "not tracked" {
		t.Fatalf("quota line for an agent with no source = %q, want %q", got, "not tracked")
	}
}

// TestAgentDoctorPrefersTheDefinitionsOwnModelsNote checks a definition that
// writes its own caveat is reported with it rather than with the generic one:
// a CLI that routes to a provider has no single catalog, and "the CLI may
// accept others" does not say which provider the list belongs to.
func TestAgentDoctorPrefersTheDefinitionsOwnModelsNote(t *testing.T) {
	doctor := stubDoctor(t, map[string]bool{"noted": true}, noProbe, noProbe, noProbe, nil)
	report := reportFor(t, doctor.Report(context.Background()), "noted")
	if report.modelNote != "known for the default provider" {
		t.Errorf("modelNote = %q, want the definition's own note", report.modelNote)
	}
	if strings.Join(report.models, ",") != "noted/vendor/one" {
		t.Errorf("models = %v, want the known list qualified", report.models)
	}
}

// TestClineAndGeminiAskOverTheAgentProtocol checks the two CLIs with no
// listing subcommand are asked the way they can be asked -- an ACP handshake
// whose session answers with its available models -- rather than carrying a
// provider-scoped list that goes stale, which is what issue #566 was.
func TestClineAndGeminiAskOverTheAgentProtocol(t *testing.T) {
	registry := agents.MustBuiltin()
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

// TestRunProbeConversationAsksAndStopsAtTheAnswer checks the conversational
// probe against a real process: it writes the definition's lines, reads the
// reply, and returns as soon as the output carries a model list rather than
// waiting for a server that never exits, which is what every CLI answering
// over a stdio protocol is.
func TestRunProbeConversationAsksAndStopsAtTheAnswer(t *testing.T) {
	spec := agents.Doctor{
		Models:      []string{os.Args[0]},
		ModelsStdin: []string{`{"id":0,"method":"initialize"}`, `{"id":1,"method":"model/list"}`},
		ModelsJSON:  "result.models[].modelId",
		Timeout:     "30s",
	}
	argv := []string{os.Args[0], "-test.run=TestDoctorProbeHelperProcess", "-test.timeout=60s"}
	started := time.Now()
	out, ok := runDoctorProbeAsHelper(t, argv, spec)
	if !ok {
		t.Fatalf("probe reported failure, output = %q", out)
	}
	models := spec.ModelsFrom(string(out))
	if strings.Join(models, ",") != "muse-spark-1.3,muse-spark-1.2" {
		t.Errorf("models = %v, want the ones the helper answered with", models)
	}
	if elapsed := time.Since(started); elapsed > 20*time.Second {
		t.Errorf("probe took %s, want it to stop at the answer rather than wait out the helper", elapsed)
	}
}

// runProbeConversation is driven through the same entry point the report uses,
// with the helper's environment carried over so it runs as the child half of
// the test rather than as the whole suite.
func runDoctorProbeAsHelper(t *testing.T, argv []string, spec agents.Doctor) ([]byte, bool) {
	t.Helper()
	t.Setenv("AVIARY_DOCTOR_PROBE_HELPER", "1")
	return runDoctorProbe(context.Background(), argv, spec, spec.ModelsStdinLines(argv[0]))
}

// TestDoctorProbeHelperProcess is not a test: it is the CLI half of
// TestRunProbeConversationAsksAndStopsAtTheAnswer, a stand-in for an agent
// that answers a handshake and then goes on waiting for its client.
func TestDoctorProbeHelperProcess(t *testing.T) {
	if os.Getenv("AVIARY_DOCTOR_PROBE_HELPER") != "1" {
		t.Skip("helper process for TestRunProbeConversationAsksAndStopsAtTheAnswer")
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if !strings.Contains(scanner.Text(), "model/list") {
			fmt.Println(`{"id":0,"result":{"serverInfo":{"name":"helper"}}}`)
			continue
		}
		fmt.Println(`{"id":1,"result":{"models":[{"modelId":"muse-spark-1.3"},{"modelId":"muse-spark-1.2"}]}}`)
		break
	}
	// The report is what ends this process: an agent serving a protocol waits
	// for the next request, and a probe that waited with it would never return.
	time.Sleep(30 * time.Second)
}

// TestAgentReportShowsTheManagedDefaultModel checks `aviary agents` names the
// model an agent runs with when --agent gives it none, qualified
// so it can be copied straight back into --agent, and says nothing extra for an
// agent whose definition leaves the choice to its own CLI.
func TestAgentReportShowsTheManagedDefaultModel(t *testing.T) {
	var out strings.Builder
	writeAgentReports(&out, []agentReport{
		{
			name: "codex", binary: "codex", path: "/opt/codex", auth: doctorSignedIn,
			defaultModel: "gpt-5.6-terra", models: []string{"codex/gpt-5.6-terra"},
		},
		{name: "gemini", binary: "gemini", path: "/opt/gemini", auth: doctorSignedIn},
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
	status := agentStatus(agentReport{name: "codex", path: "/opt/codex", defaultModel: "gpt-5.6-terra"})
	if status.DefaultModel != "gpt-5.6-terra" {
		t.Fatalf("DefaultModel = %q, want the report's", status.DefaultModel)
	}
}

// TestAgentDoctorFlagsStaleSkills checks stale installed skills
// warn on every installed agent that installs them, and are printed on the
// report and carried to the dashboard, while an agent with no skills target is
// left alone.
func TestAgentDoctorFlagsStaleSkills(t *testing.T) {
	installed := map[string]bool{"probed": true, "silent": true}
	signedIn := func(string) (string, bool) { return "logged in", true }
	doctor := stubDoctor(t, installed, noProbe, signedIn, noProbe, nil)
	doctor.skillsNote = func() string { return "gh-fix stale for aviary v1.5.25; restart aviary to reinstall" }
	reports := doctor.Report(context.Background())
	probed := reportFor(t, reports, "probed")
	if probed.skillsNote == "" || probed.status() != statusWarn {
		t.Fatalf("probed skillsNote = %q, status = %q, want a warning", probed.skillsNote, probed.status())
	}
	if silent := reportFor(t, reports, "silent"); silent.skillsNote != "" {
		t.Fatalf("silent skillsNote = %q, want none for an agent with no skills target", silent.skillsNote)
	}
	var out strings.Builder
	writeAgentReports(&out, []agentReport{probed})
	if !strings.Contains(out.String(), "gh-fix stale for aviary v1.5.25") {
		t.Fatalf("report = %q, want the skills line", out.String())
	}
	if status := agentStatus(probed); status.SkillsNote != probed.skillsNote || status.Status != "warn" {
		t.Fatalf("status = %+v, want the skills note and a warning", status)
	}
}
