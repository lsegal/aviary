package agents

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestDoctorArgvSubstitutesBinary checks both probes render against the
// executable the agent was resolved to, so an --agent-binary override reaches
// the report rather than probing whatever is on PATH under the same name.
func TestDoctorArgvSubstitutesBinary(t *testing.T) {
	doctor := Doctor{
		Auth:   []string{"{binary}", "login", "status"},
		Models: []string{"{binary}", "models"},
	}
	if got, want := doctor.AuthArgv("/opt/codex"), []string{"/opt/codex", "login", "status"}; !reflect.DeepEqual(got, want) {
		t.Errorf("AuthArgv = %v, want %v", got, want)
	}
	if got, want := doctor.ModelsArgv("/opt/codex"), []string{"/opt/codex", "models"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ModelsArgv = %v, want %v", got, want)
	}
	if got := (Doctor{}).AuthArgv("/opt/codex"); got != nil {
		t.Errorf("AuthArgv of an undeclared probe = %v, want nil", got)
	}
}

// TestDoctorReportsSignedIn covers the CLIs that report a signed-out account on
// a zero exit status: without the expression the exit status has already
// decided, and with one the output has to match it.
func TestDoctorReportsSignedIn(t *testing.T) {
	for _, test := range []struct {
		name     string
		signedIn string
		output   string
		want     bool
	}{
		{name: "no expression trusts the exit status", output: "anything at all", want: true},
		{name: "matching output", signedIn: `(?im)^\s*Logged in`, output: "Logged in using ChatGPT\n", want: true},
		{name: "signed out is not a match", signedIn: `(?im)^\s*Logged in`, output: "Not logged in\n"},
		{name: "unparseable expression is not signed in", signedIn: "(", output: "Logged in"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := (Doctor{SignedIn: test.signedIn}).ReportsSignedIn(test.output); got != test.want {
				t.Errorf("ReportsSignedIn(%q) = %v, want %v", test.output, got, test.want)
			}
		})
	}
}

// TestDoctorModelsFrom reads a model list the way the report needs it: in the
// CLI's own order, without blanks or duplicates, and filtered by the pattern
// when the output is decorated.
func TestDoctorModelsFrom(t *testing.T) {
	for _, test := range []struct {
		name   string
		doctor Doctor
		output string
		want   []string
	}{
		{
			name:   "one per line in order",
			output: "openai/gpt-5.6\n\n  anthropic/claude-opus-5  \nopenai/gpt-5.6\n",
			want:   []string{"openai/gpt-5.6", "anthropic/claude-opus-5"},
		},
		{
			name:   "pattern drops decoration",
			doctor: Doctor{ModelPattern: `^[A-Za-z0-9._-]+/[A-Za-z0-9._/-]+$`},
			output: "Available models:\nopenai/gpt-5.6\n- not a model\nanthropic/claude-opus-5\n",
			want:   []string{"openai/gpt-5.6", "anthropic/claude-opus-5"},
		},
		{
			name:   "capture group is the model",
			doctor: Doctor{ModelPattern: `^\*?\s*(\S+)\s+\(.*\)$`},
			output: "* gpt-5.6 (default)\nclaude-opus-5 (slow)\nheader line\n",
			want:   []string{"gpt-5.6", "claude-opus-5"},
		},
		{
			name:   "unparseable pattern reads nothing",
			doctor: Doctor{ModelPattern: "("},
			output: "openai/gpt-5.6\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.doctor.ModelsFrom(test.output); !reflect.DeepEqual(got, test.want) && !(len(got) == 0 && len(test.want) == 0) {
				t.Errorf("ModelsFrom() = %v, want %v", got, test.want)
			}
		})
	}
}

// TestDoctorTimeoutDuration checks a probe is bounded by the definition's
// value when it names a usable one and by the default otherwise, so a hanging
// CLI never holds the whole listing up.
func TestDoctorTimeoutDuration(t *testing.T) {
	for _, test := range []struct {
		timeout string
		want    time.Duration
	}{
		{timeout: "", want: DefaultDoctorTimeout},
		{timeout: "5s", want: 5 * time.Second},
		{timeout: "nonsense", want: DefaultDoctorTimeout},
		{timeout: "-1s", want: DefaultDoctorTimeout},
	} {
		if got := (Doctor{Timeout: test.timeout}).TimeoutDuration(); got != test.want {
			t.Errorf("TimeoutDuration(%q) = %v, want %v", test.timeout, got, test.want)
		}
	}
}

// TestDoctorValidate rejects the blocks that would silently do nothing, on the
// same grounds the quota block does: a probe that is configured and never runs
// looks exactly like a working one.
func TestDoctorValidate(t *testing.T) {
	for _, test := range []struct {
		name   string
		doctor Doctor
		want   string
	}{
		{name: "empty block is valid"},
		{name: "empty auth argument", doctor: Doctor{Auth: []string{"codex", " "}}, want: "doctor.auth"},
		{name: "empty models argument", doctor: Doctor{Models: []string{"opencode", ""}}, want: "doctor.models"},
		{name: "signedIn without auth", doctor: Doctor{SignedIn: "ok"}, want: "doctor.signedIn"},
		{name: "modelPattern without models", doctor: Doctor{ModelPattern: "."}, want: "doctor.modelPattern"},
		{name: "empty known model", doctor: Doctor{KnownModels: []string{"opus", " "}}, want: "doctor.knownModels"},
		{name: "unparseable signedIn", doctor: Doctor{Auth: []string{"codex"}, SignedIn: "("}, want: "doctor.signedIn"},
		{name: "unparseable modelPattern", doctor: Doctor{Models: []string{"opencode"}, ModelPattern: "("}, want: "doctor.modelPattern"},
		{name: "unparseable timeout", doctor: Doctor{Timeout: "soon"}, want: "doctor.timeout"},
		{name: "non-positive timeout", doctor: Doctor{Timeout: "0s"}, want: "doctor.timeout"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.doctor.validate()
			if test.want == "" {
				if err != nil {
					t.Fatalf("validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validate() error = %v, want one naming %q", err, test.want)
			}
		})
	}
}

// TestDoctorSurvivesTheConfigRoundTrip checks a doctor block reaches a
// definition through the config file's marshal-and-merge, which is what lets an
// agent glorp has never heard of report its models without a code change.
func TestDoctorSurvivesTheConfigRoundTrip(t *testing.T) {
	registry, err := Load(writeConfig(t, `{"agents": [{
		"name": "mine", "binary": "mine",
		"session": {"assign": "none"},
		"output": {"format": "text"},
		"doctor": {"auth": ["{binary}", "whoami"], "models": ["{binary}", "models"], "timeout": "3s"},
		"args": {"run": [{"args": ["{prompt}"]}]}
	}]}`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	definition, ok := registry.Lookup("mine")
	if !ok {
		t.Fatal("config-defined agent is not registered")
	}
	if got, want := definition.Doctor.AuthArgv("mine"), []string{"mine", "whoami"}; !reflect.DeepEqual(got, want) {
		t.Errorf("AuthArgv = %v, want %v", got, want)
	}
	if got := definition.Doctor.TimeoutDuration(); got != 3*time.Second {
		t.Errorf("TimeoutDuration = %v, want 3s", got)
	}
}

// TestBuiltinDoctorProbesAreDeclaredOnce holds the shipped probes to what the
// report needs of them: every argv names the agent through {binary}, so a
// --agent-binary override is honoured rather than silently probing PATH.
func TestBuiltinDoctorProbesAreDeclaredOnce(t *testing.T) {
	for _, name := range MustBuiltin().Names() {
		definition, _ := MustBuiltin().Lookup(name)
		for field, argv := range map[string][]string{"auth": definition.Doctor.Auth, "models": definition.Doctor.Models} {
			if len(argv) == 0 {
				continue
			}
			if argv[0] != "{binary}" {
				t.Errorf("%s doctor.%s runs %q, want it to run {binary}", name, field, argv[0])
			}
		}
	}
}

func TestClaudeKnownModelsIncludeOpus55(t *testing.T) {
	claude, ok := MustBuiltin().Lookup("claude")
	if !ok {
		t.Fatal("builtin registry has no claude definition")
	}
	if got, want := strings.Join(claude.Doctor.KnownModels, ","), "opus,sonnet,haiku,claude-opus-5-5"; got != want {
		t.Errorf("Claude known models = %q, want %q", got, want)
	}
}

// TestAgyModelsFromRealProbeOutput pins the `agy` definition's filtering to
// what `agy models` really prints, captured from agy 1.1.26: a
// `Fetching available models...` progress line, then one model per line as a
// tab-separated id and display name. The pattern the definition shipped with
// allowed spaces but not tabs, so it rejected every model line and kept only
// the progress line, and the report offered `agy/Fetching available models...`
// as something to paste into --agent.
func TestAgyModelsFromRealProbeOutput(t *testing.T) {
	agy, ok := MustBuiltin().Lookup("agy")
	if !ok {
		t.Fatal("builtin registry has no agy definition")
	}
	output := strings.Join([]string{
		"Fetching available models...",
		"gemini-3.8-flash-high\tGemini 3.8 Flash (High)",
		"gemini-3.8-flash-medium\tGemini 3.8 Flash (Medium)",
		"gemini-3.1-pro-high\tGemini 3.1 Pro (High)",
		"claude-opus-4-6-thinking\tClaude Opus 4.6 (Thinking)",
		"gpt-oss-120b-medium\tGPT-OSS 120B (Medium)",
		"",
	}, "\n")
	want := []string{
		"gemini-3.8-flash-high",
		"gemini-3.8-flash-medium",
		"gemini-3.1-pro-high",
		"claude-opus-4-6-thinking",
		"gpt-oss-120b-medium",
	}
	got := agy.Doctor.ModelsFrom(output)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ModelsFrom() = %v, want %v", got, want)
	}
	for _, model := range got {
		if strings.Contains(model, " ") {
			t.Errorf("ModelsFrom() reported %q, want no status or progress line as a model", model)
		}
	}
}
