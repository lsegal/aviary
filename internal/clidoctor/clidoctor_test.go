package clidoctor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/clidefs"
)

// testDoctorRegistry builds a registry of agents with the probe blocks the
// report reads, without touching the built-in definitions: the report has to
// work for an agent declared in a config file too.
func testDoctorRegistry(t *testing.T) *clidefs.Registry {
	t.Helper()
	base := func(name string) clidefs.Definition {
		return clidefs.Definition{
			Name: name, Binary: name,
			Session: clidefs.Session{Assign: clidefs.AssignNone},
			Output:  clidefs.Output{Format: clidefs.FormatText},
			Args:    clidefs.Args{Run: []clidefs.Fragment{{Args: []string{"{prompt}"}}}},
		}
	}
	probed := base("probed")
	probed.Doctor = clidefs.Doctor{
		Auth:   []string{"{binary}", "login", "status"},
		Models: []string{"{binary}", "models"},
	}
	probed.Quota = clidefs.Quota{Reader: clidefs.QuotaClaude}
	probed.Skills = clidefs.Skills{Target: "probed"}
	declared := base("declared")
	declared.Models = clidefs.NewAllowList("opus", "sonnet")
	silent := base("silent")
	known := base("known")
	known.Doctor = clidefs.Doctor{KnownModels: []string{"opus", "sonnet"}}
	noted := base("noted")
	noted.Doctor = clidefs.Doctor{KnownModels: []string{"vendor/one"}, ModelsNote: "known for the default provider"}
	nomodel := base("nomodel")
	nomodel.Models = clidefs.NewAllowList()
	old := base("old")
	old.MinVersion = "2.0.0"
	registry, err := clidefs.NewRegistry(probed, declared, silent, known, noted, nomodel, old)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return registry
}

// stubDoctor wires a doctor whose every probe is answered in-process, so the
// report is testable on a machine with no agent CLI installed.
func stubDoctor(t *testing.T, installed map[string]bool, version, auth, models func(string) (string, bool), quota map[string]string) *Doctor {
	t.Helper()
	registry := testDoctorRegistry(t)
	return &Doctor{
		Registry: registry,
		LookPath: func(binary string) (string, error) {
			if !installed[binary] {
				return "", errors.New("not found")
			}
			return "/opt/" + binary, nil
		},
		Version: func(_ context.Context, binary string) ([]byte, error) {
			text, ok := version(binary)
			if !ok {
				return nil, errors.New("no version")
			}
			return []byte(text), nil
		},
		Run: func(_ context.Context, argv []string, _ clidefs.Doctor, _ []string) ([]byte, bool) {
			probe := auth
			if argv[len(argv)-1] == "models" {
				probe = models
			}
			text, ok := probe(argv[0])
			return []byte(text), ok
		},
		Quota: func(_ context.Context, name, _ string) (string, error) { return quota[name], nil },
	}
}

// reportFor finds one agent's entry in a report.
func reportFor(t *testing.T, reports []Report, name string) Report {
	t.Helper()
	for _, report := range reports {
		if report.Name == name {
			return report
		}
	}
	t.Fatalf("report has no entry for %q", name)
	return Report{}
}

// noProbe answers every probe with a failure, for the agents a test is not
// about.
func noProbe(string) (string, bool) { return "", false }

// TestDoctorReportsEveryRegisteredAgent checks the report covers the whole
// registry in its sorted order, so an agent a config file adds is reported
// beside the built-ins rather than left out of the listing it belongs to.
func TestDoctorReportsEveryRegisteredAgent(t *testing.T) {
	doctor := stubDoctor(t, nil, noProbe, noProbe, noProbe, nil)
	reports := doctor.Report(context.Background())
	if len(reports) != len(doctor.Registry.Names()) {
		t.Fatalf("report has %d entries, want %d", len(reports), len(doctor.Registry.Names()))
	}
	for i, name := range doctor.Registry.Names() {
		if reports[i].Name != name {
			t.Fatalf("report[%d] = %q, want %q in registry order", i, reports[i].Name, name)
		}
	}
}

// TestDoctorLeavesOutInProcessAgents checks the in-process aviary backend,
// whose model names one of aviary's own agents, is not reported as a CLI
// provider (issue #494).
func TestDoctorLeavesOutInProcessAgents(t *testing.T) {
	registry, err := clidefs.NewRegistry(
		clidefs.Definition{
			Name: "probed", Binary: "probed",
			Session: clidefs.Session{Assign: clidefs.AssignNone},
			Output:  clidefs.Output{Format: clidefs.FormatText},
			Args:    clidefs.Args{Run: []clidefs.Fragment{{Args: []string{"{prompt}"}}}},
		},
		clidefs.Definition{
			Name: clidefs.Aviary, Binary: clidefs.Aviary, InProcess: true,
			Session: clidefs.Session{Assign: clidefs.AssignNone},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	doctor := &Doctor{Registry: registry, LookPath: func(string) (string, error) { return "", errors.New("not found") }}
	reports := doctor.Report(context.Background())
	if len(reports) != 1 || reports[0].Name != "probed" {
		t.Fatalf("reports = %+v, want only probed", reports)
	}
}

// TestDoctorSkipsProbesForAMissingBinary checks nothing is run for an
// agent whose CLI is not installed: every remaining question is about that
// binary, and running them would only produce the same failure five times.
func TestDoctorSkipsProbesForAMissingBinary(t *testing.T) {
	ran := false
	probe := func(string) (string, bool) { ran = true; return "", true }
	doctor := stubDoctor(t, nil, probe, probe, probe, map[string]string{"probed": "week 50% left"})
	doctor.Quota = func(context.Context, string, string) (string, error) { ran = true; return "", nil }
	report := reportFor(t, doctor.Report(context.Background()), "probed")
	if ran {
		t.Error("a probe ran for an agent whose binary is not installed")
	}
	if report.Installed() || report.StatusKey() != StatusMissing {
		t.Errorf("status = %q, want %q for a missing binary", report.StatusKey(), StatusMissing)
	}
	if report.Auth != Unknown {
		t.Errorf("auth = %q, want %q for a missing binary", report.Auth, Unknown)
	}
}

// TestDoctorReadsTheAuthProbe checks the definition's own probe decides
// the sign-in state, including the CLI that reports a signed-out account on a
// zero exit status.
func TestDoctorReadsTheAuthProbe(t *testing.T) {
	for _, test := range []struct {
		name   string
		output string
		ok     bool
		want   string
	}{
		{name: "signed in", output: "Logged in using ChatGPT", ok: true, want: SignedIn},
		{name: "probe failed", output: "not authenticated", want: SignedOut},
	} {
		t.Run(test.name, func(t *testing.T) {
			auth := func(string) (string, bool) { return test.output, test.ok }
			doctor := stubDoctor(t, map[string]bool{"probed": true}, noProbe, auth, noProbe, nil)
			if got := reportFor(t, doctor.Report(context.Background()), "probed").Auth; got != test.want {
				t.Errorf("auth = %q, want %q", got, test.want)
			}
		})
	}
}

// TestDoctorFallsBackToTheQuotaReadingForAuth checks an agent with no
// sign-in probe of its own is reported as signed in when its quota could be
// read, because every quota reader asks the CLI something only a signed-in
// account can answer, and as unknown when there is no evidence either way.
func TestDoctorFallsBackToTheQuotaReadingForAuth(t *testing.T) {
	installed := map[string]bool{"silent": true}
	withQuota := stubDoctor(t, installed, noProbe, noProbe, noProbe, map[string]string{"silent": "week 40% left"})
	if got := reportFor(t, withQuota.Report(context.Background()), "silent").Auth; got != SignedIn {
		t.Errorf("auth = %q, want %q when a quota was read", got, SignedIn)
	}
	without := stubDoctor(t, installed, noProbe, noProbe, noProbe, nil)
	if got := reportFor(t, without.Report(context.Background()), "silent").Auth; got != Unknown {
		t.Errorf("auth = %q, want %q with no evidence either way", got, Unknown)
	}
}

// TestDoctorQualifiesProbedModels checks the models a CLI lists are
// rendered as the agent/model names --agent takes, which is the whole reason
// the report lists them.
func TestDoctorQualifiesProbedModels(t *testing.T) {
	models := func(string) (string, bool) { return "openai/gpt-5.6\nanthropic/claude-opus-5\n", true }
	doctor := stubDoctor(t, map[string]bool{"probed": true}, noProbe, noProbe, models, nil)
	report := reportFor(t, doctor.Report(context.Background()), "probed")
	want := []string{"probed/openai/gpt-5.6", "probed/anthropic/claude-opus-5"}
	if strings.Join(report.Models, ",") != strings.Join(want, ",") {
		t.Errorf("models = %v, want %v", report.Models, want)
	}
}

// TestDoctorFallsBackToDeclaredModels checks the three things a definition
// alone can say about models, which stay distinguishable in the report: an
// allow-list, an agent that takes no model, and one that validates any.
func TestDoctorFallsBackToDeclaredModels(t *testing.T) {
	doctor := stubDoctor(t, map[string]bool{"declared": true, "nomodel": true, "silent": true}, noProbe, noProbe, noProbe, nil)
	reports := doctor.Report(context.Background())
	declared := reportFor(t, reports, "declared")
	if strings.Join(declared.Models, ",") != "declared/opus,declared/sonnet" {
		t.Errorf("declared models = %v, want the allow-list qualified", declared.Models)
	}
	if got := reportFor(t, reports, "nomodel"); len(got.Models) != 0 || !strings.Contains(got.ModelNote, "not accepted") {
		t.Errorf("nomodel = %v/%q, want no models and a note", got.Models, got.ModelNote)
	}
	if got := reportFor(t, reports, "silent"); len(got.Models) != 0 || !strings.Contains(got.ModelNote, "any model") {
		t.Errorf("silent = %v/%q, want no models and a note", got.Models, got.ModelNote)
	}
}

// TestDoctorListsKnownModels checks a definition that names no allow-list
// but knows what its CLI takes is enumerated rather than dismissed with the
// note that any model is accepted, which is the whole point of the field
// . The list is labelled as aviary's rather than the CLI's, because
// it is not an allow-list: a model released after the definition still runs.
func TestDoctorListsKnownModels(t *testing.T) {
	doctor := stubDoctor(t, map[string]bool{"known": true}, noProbe, noProbe, noProbe, nil)
	report := reportFor(t, doctor.Report(context.Background()), "known")
	if strings.Join(report.Models, ",") != "known/opus,known/sonnet" {
		t.Errorf("known models = %v, want the known list qualified", report.Models)
	}
	if !strings.Contains(report.ModelNote, "known to aviary") {
		t.Errorf("known note = %q, want it to say the list is aviary's", report.ModelNote)
	}
	registry := testDoctorRegistry(t)
	definition, _ := registry.Lookup("known")
	if !definition.AcceptsModel("a-model-released-this-morning") {
		t.Error("knownModels rejects an unlisted model, want it to stay a hint rather than an allow-list")
	}
}

// TestDoctorPrefersProbedModelsOverKnownOnes checks a live listing wins:
// the known list exists for the CLIs that have no listing command, not to
// override one that does.
func TestDoctorPrefersProbedModelsOverKnownOnes(t *testing.T) {
	models := func(string) (string, bool) { return "gpt-5.6\n", true }
	doctor := stubDoctor(t, map[string]bool{"probed": true}, noProbe, noProbe, models, nil)
	report := reportFor(t, doctor.Report(context.Background()), "probed")
	if strings.Join(report.Models, ",") != "probed/gpt-5.6" || report.ModelNote != "" {
		t.Errorf("probed = %v/%q, want the probed list with no note", report.Models, report.ModelNote)
	}
}

// TestDoctorFlagsAnOldBinary checks a CLI below its definition's declared
// minimum is called out rather than reported as ready, which is the whole point
// of declaring the minimum.
func TestDoctorFlagsAnOldBinary(t *testing.T) {
	version := func(string) (string, bool) { return "1.4.0", true }
	doctor := stubDoctor(t, map[string]bool{"old": true}, version, noProbe, noProbe, nil)
	report := reportFor(t, doctor.Report(context.Background()), "old")
	if report.Version != "1.4.0" {
		t.Errorf("version = %q, want the reported one", report.Version)
	}
	if !strings.Contains(report.VersionNote, "2.0.0") || report.StatusKey() != StatusWarn {
		t.Errorf("versionNote = %q, status = %q, want a warning naming the minimum", report.VersionNote, report.StatusKey())
	}
}

// TestDoctorPrefersTheDefinitionsOwnModelsNote checks a definition that
// writes its own caveat is reported with it rather than with the generic one:
// a CLI that routes to a provider has no single catalog, and "the CLI may
// accept others" does not say which provider the list belongs to.
func TestDoctorPrefersTheDefinitionsOwnModelsNote(t *testing.T) {
	doctor := stubDoctor(t, map[string]bool{"noted": true}, noProbe, noProbe, noProbe, nil)
	report := reportFor(t, doctor.Report(context.Background()), "noted")
	if report.ModelNote != "known for the default provider" {
		t.Errorf("modelNote = %q, want the definition's own note", report.ModelNote)
	}
	if strings.Join(report.Models, ",") != "noted/vendor/one" {
		t.Errorf("models = %v, want the known list qualified", report.Models)
	}
}

// TestRunProbeConversationAsksAndStopsAtTheAnswer checks the conversational
// probe against a real process: it writes the definition's lines, reads the
// reply, and returns as soon as the output carries a model list rather than
// waiting for a server that never exits, which is what every CLI answering
// over a stdio protocol is.
func TestRunProbeConversationAsksAndStopsAtTheAnswer(t *testing.T) {
	spec := clidefs.Doctor{
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
func runDoctorProbeAsHelper(t *testing.T, argv []string, spec clidefs.Doctor) ([]byte, bool) {
	t.Helper()
	t.Setenv("AVIARY_DOCTOR_PROBE_HELPER", "1")
	return RunProbe(context.Background(), argv, spec, spec.ModelsStdinLines(argv[0]))
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

// TestDoctorFlagsStaleSkills checks stale installed skills warn on every
// installed agent that installs them, while an agent with no skills target is
// left alone.
func TestDoctorFlagsStaleSkills(t *testing.T) {
	installed := map[string]bool{"probed": true, "silent": true}
	signedIn := func(string) (string, bool) { return "logged in", true }
	doctor := stubDoctor(t, installed, noProbe, signedIn, noProbe, nil)
	doctor.SkillsNote = func(string) string { return "gh-fix stale for aviary v1.5.25; restart aviary to reinstall" }
	reports := doctor.Report(context.Background())
	probed := reportFor(t, reports, "probed")
	if probed.SkillsNote == "" || probed.StatusKey() != StatusWarn {
		t.Fatalf("probed SkillsNote = %q, status = %q, want a warning", probed.SkillsNote, probed.StatusKey())
	}
	if silent := reportFor(t, reports, "silent"); silent.SkillsNote != "" {
		t.Fatalf("silent SkillsNote = %q, want none for an agent with no skills target", silent.SkillsNote)
	}
}

// TestDoctorReadsJSONAuthStatus checks the claude definition's auth probe:
// `claude auth status` answers in JSON, and the report reads both whether it
// is signed in and how, which is what `aviary doctor` prints.
func TestDoctorReadsJSONAuthStatus(t *testing.T) {
	for _, test := range []struct {
		name       string
		output     string
		ok         bool
		wantAuth   string
		wantDetail string
	}{
		{name: "signed in", output: `{"loggedIn":true,"authMethod":"claude.ai"}`, ok: true, wantAuth: SignedIn, wantDetail: "claude.ai"},
		{name: "signed out on a zero exit", output: `{"loggedIn":false}`, ok: true, wantAuth: SignedOut},
		{name: "probe failed", output: `{"loggedIn":false}`, wantAuth: SignedOut},
		{name: "unreadable", output: "not json", ok: true, wantAuth: SignedOut},
	} {
		t.Run(test.name, func(t *testing.T) {
			doctor := builtinDoctor(t, "claude", func(argv []string) ([]byte, bool) {
				if strings.Join(argv[1:], " ") != "auth status" {
					return nil, false
				}
				return []byte(test.output), test.ok
			})
			report := doctor.Probe(context.Background(), "claude")
			if report.Auth != test.wantAuth || report.AuthDetail != test.wantDetail {
				t.Errorf("auth = %q (%q), want %q (%q)", report.Auth, report.AuthDetail, test.wantAuth, test.wantDetail)
			}
		})
	}
}

// TestDoctorReadsCodexLoginStatus checks codex's text auth probe reports the
// sign-in method the way core's doctor always has.
func TestDoctorReadsCodexLoginStatus(t *testing.T) {
	doctor := builtinDoctor(t, "codex", func(argv []string) ([]byte, bool) {
		if strings.Join(argv[1:], " ") != "login status" {
			return nil, false
		}
		return []byte("Logged in using ChatGPT\n"), true
	})
	report := doctor.Probe(context.Background(), "codex")
	if report.Auth != SignedIn || report.AuthDetail != "ChatGPT" {
		t.Errorf("auth = %q (%q), want signed in (ChatGPT)", report.Auth, report.AuthDetail)
	}
}

// builtinDoctor probes a built-in definition with every process answered
// in-process.
func builtinDoctor(t *testing.T, name string, run func(argv []string) ([]byte, bool)) *Doctor {
	t.Helper()
	return &Doctor{
		Registry: clidefs.MustBuiltin(),
		LookPath: func(binary string) (string, error) { return "/opt/" + binary, nil },
		Version: func(context.Context, string) ([]byte, error) {
			return []byte("2.1.312 (" + name + ")\n"), nil
		},
		Run: func(_ context.Context, argv []string, _ clidefs.Doctor, _ []string) ([]byte, bool) { return run(argv) },
	}
}

// TestMuseFallsBackToItsKnownLineup checks the built-in muse definition reports
// the live model/list answer when its CLI gives one, and the published lineup
// it carries when the CLI is signed out and answers with no catalog.
func TestMuseFallsBackToItsKnownLineup(t *testing.T) {
	live := builtinDoctor(t, "muse", func(argv []string) ([]byte, bool) {
		if len(argv) > 1 && argv[1] == "serve" {
			return []byte(`{"id":1,"result":{"models":[{"modelId":"muse-spark-1.4"}]}}`), true
		}
		return nil, false
	}).Probe(context.Background(), "muse")
	if got := strings.Join(live.Models, ","); got != "muse/muse-spark-1.4" {
		t.Errorf("live models = %q, want the CLI's own answer", got)
	}

	signedOut := builtinDoctor(t, "muse", func(argv []string) ([]byte, bool) {
		if len(argv) > 1 && argv[1] == "serve" {
			return []byte(`{"id":0,"result":{"serverInfo":{"name":"muse"}}}`), true
		}
		return nil, false
	}).Probe(context.Background(), "muse")
	if !slices.Contains(signedOut.Models, "muse/muse-spark-1.3") || !slices.Contains(signedOut.Models, "muse/muse-ember-1.0") {
		t.Errorf("signed-out models = %v, want the known Muse lineup", signedOut.Models)
	}
	if signedOut.ModelNote != KnownModelsNote {
		t.Errorf("signed-out models note = %q, want %q", signedOut.ModelNote, KnownModelsNote)
	}
	if signedOut.DefaultModel != "muse-spark-1.3" {
		t.Errorf("default model = %q, want muse-spark-1.3", signedOut.DefaultModel)
	}
}
