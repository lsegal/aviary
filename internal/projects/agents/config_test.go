package agents

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, document string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), DefaultConfigPath)
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestLoadWithoutAConfigFile checks a run with no config file has exactly the
// built-in agents. A missing file is the normal case, not an error.
func TestLoadWithoutAConfigFile(t *testing.T) {
	registry, err := Load(filepath.Join(t.TempDir(), DefaultConfigPath))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	requireRegistered(t, registry, "claude", "codex")
}

// TestConfigOverridesABuiltinFieldByField checks a definition that names a
// built-in replaces only the fields it mentions, so overriding one flag does
// not mean restating the whole agent.
func TestConfigOverridesABuiltinFieldByField(t *testing.T) {
	// The narrowed models list has to admit the default the built-in
	// carries, so the override names one the list allows (issue #612).
	path := writeConfig(t, `{"agents":[{"name":"claude","binary":"/opt/claude","models":["opus"],"defaultModel":"opus"}]}`)
	registry, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	claude, ok := registry.Lookup("claude")
	if !ok {
		t.Fatal("claude was not registered")
	}
	if claude.Binary != "/opt/claude" {
		t.Fatalf("binary = %q, want the override", claude.Binary)
	}
	if !reflect.DeepEqual(claude.Models.Values(), []string{"opus"}) {
		t.Fatalf("models = %v, want the override", claude.Models)
	}
	// Everything the document did not mention is still the built-in's.
	if claude.Env["CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS"] != "0" {
		t.Fatalf("env = %v, want the built-in's", claude.Env)
	}
	if got := claude.Render(ModeRun, Values{Prompt: "go"}); !reflect.DeepEqual(got, []string{"-p", "--permission-mode", "auto", "--output-format", "stream-json", "--verbose", "go"}) {
		t.Fatalf("argv = %#v, want the built-in template", got)
	}
	requireRegistered(t, registry, "claude", "codex")
}

// TestConfigNullClearsAnInheritedField checks the documented way to remove
// something a built-in declares: give the field as null rather than trying to
// spell an empty value the schema has no syntax for.
func TestConfigNullClearsAnInheritedField(t *testing.T) {
	path := writeConfig(t, `{"agents":[{"name":"claude","env":null,"levels":null}]}`)
	registry, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	claude, _ := registry.Lookup("claude")
	if len(claude.Env) != 0 {
		t.Fatalf("env = %v, want it cleared", claude.Env)
	}
	if claude.Levels.Declared() {
		t.Fatalf("levels = %v, want them cleared", claude.Levels)
	}
	if !claude.AcceptsLevel("blazing") {
		t.Fatal("a cleared allow-list should constrain nothing")
	}
}

// TestConfigRegistersANewAgent checks an unknown name adds an agent rather
// than being rejected for not being one of the built-ins.
func TestConfigRegistersANewAgent(t *testing.T) {
	path := writeConfig(t, `{"agents":[{
		"name":"acme","binary":"acme",
		"session":{"assign":"none"},"output":{"format":"text"},
		"args":{"run":[{"args":["run","{prompt}"]}],"resume":[{"args":["run","{prompt}"]}]}
	}]}`)
	registry, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	requireRegistered(t, registry, "claude", "codex", "acme")
	acme, _ := registry.Lookup("acme")
	if got := acme.Render(ModeRun, Values{Prompt: "go"}); !reflect.DeepEqual(got, []string{"run", "go"}) {
		t.Fatalf("argv = %#v, want the configured template", got)
	}
}

// TestConfigAcceptsAnObjectKeyedByName checks the second shape the agents
// section may take, where the key names the agent instead of a "name" field.
func TestConfigAcceptsAnObjectKeyedByName(t *testing.T) {
	path := writeConfig(t, `{"agents":{"claude":{"binary":"/opt/claude"}}}`)
	registry, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	claude, _ := registry.Lookup("claude")
	if claude.Binary != "/opt/claude" {
		t.Fatalf("binary = %q, want the override", claude.Binary)
	}
}

// TestConfigErrorsNameTheFileTheAgentAndTheField checks every rejection is
// loud and specific. A definition dropped quietly looks exactly like a typo in
// --agent, which is the failure this whole file exists to avoid.
func TestConfigErrorsNameTheFileTheAgentAndTheField(t *testing.T) {
	for _, test := range []struct {
		name     string
		document string
		wantSubs []string
	}{
		{
			name:     "malformed json",
			document: `{"agents":[`,
			wantSubs: []string{DefaultConfigPath},
		},
		{
			name:     "unknown field",
			document: `{"agents":[{"name":"claude","binaries":"claude"}]}`,
			wantSubs: []string{DefaultConfigPath, `agent "claude"`, `unknown field "binaries"`},
		},
		{
			name:     "invalid value",
			document: `{"agents":[{"name":"claude","output":{"format":"yaml"}}]}`,
			wantSubs: []string{`agent "claude"`, `"output.format"`},
		},
		{
			name:     "unknown condition",
			document: `{"agents":[{"name":"claude","args":{"run":[{"when":"sandbox","args":["-x"]}],"resume":[{"args":["-x"]}]}}]}`,
			wantSubs: []string{`agent "claude"`, `unknown condition "sandbox"`},
		},
		{
			name:     "nameless definition",
			document: `{"agents":[{"binary":"acme"}]}`,
			wantSubs: []string{"agents[0]", `"name"`},
		},
		{
			name:     "key and name disagree",
			document: `{"agents":{"acme":{"name":"cline"}}}`,
			wantSubs: []string{`"acme"`, `"cline"`},
		},
		{
			name:     "unknown section",
			document: `{"targets":["o/r"]}`,
			wantSubs: []string{`unknown section "targets"`, `"agents"`},
		},
		{
			name:     "a work state file",
			document: `{"12":{"status":"completed"}}`,
			wantSubs: []string{"work-state record", "--state"},
		},
		{
			name:     "a scoped work state file",
			document: `{"o/r#12":{"status":"completed"}}`,
			wantSubs: []string{"work-state record", "--state"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, test.document))
			if err == nil {
				t.Fatal("the config was accepted")
			}
			for _, want := range test.wantSubs {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error = %v, want it to mention %s", err, want)
				}
			}
		})
	}
}

// TestConfigIsNeverWritten checks loading leaves the file exactly as the user
// wrote it. The whole reason definitions do not live in the state file is that
// glorp rewrites that one.
func TestConfigIsNeverWritten(t *testing.T) {
	document := `{"agents":[{"name":"claude","binary":"/opt/claude"}]}`
	path := writeConfig(t, document)
	if _, err := Load(path); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != document {
		t.Fatalf("config file = %q, want it untouched", raw)
	}
}

// TestConfigEmptyAllowListDeclaresNoValue checks the config file can say what
// a built-in says: an empty list declares that the agent takes no value for
// that dimension, which is distinct from null, the documented way to clear an
// inherited list back to accepting anything (issue #532).
func TestConfigEmptyAllowListDeclaresNoValue(t *testing.T) {
	path := writeConfig(t, `{"agents":[{"name":"codex","levels":[]}]}`)
	registry, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	codex, _ := registry.Lookup("codex")
	if !codex.Levels.AcceptsNothing() {
		t.Fatalf("levels = %v, want an agent that takes none", codex.Levels.Values())
	}
	if codex.AcceptsLevel("high") {
		t.Fatal("a level was admitted by an agent that declares none")
	}
	// The rest of the built-in is still inherited through the merge.
	if codex.Binary != "codex" || !codex.Supports(ModeResume) {
		t.Fatalf("codex = %#v, want the built-in's other fields", codex)
	}
}

// TestConfigSetsAndDropsTheDefaultModel checks a config file owns the managed
// default (issue #612): it can point a built-in at another model, and can drop
// the inherited one with null so the agent's own CLI chooses again.
func TestConfigSetsAndDropsTheDefaultModel(t *testing.T) {
	replaced, err := Load(writeConfig(t, `{"agents":[{"name":"codex","defaultModel":"gpt-5.4"}]}`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	codex, _ := replaced.Lookup("codex")
	if got := codex.ModelOrDefault(""); got != "gpt-5.4" {
		t.Fatalf("default model = %q, want the config's", got)
	}
	dropped, err := Load(writeConfig(t, `{"agents":[{"name":"codex","defaultModel":null}]}`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	codex, _ = dropped.Lookup("codex")
	if got := codex.ModelOrDefault(""); got != "" {
		t.Fatalf("default model = %q, want none once the config dropped it", got)
	}
}

// TestConfigRejectsADefaultModelItsOwnAllowListDenies checks the file is told
// which field is wrong rather than glorp running a model the same document says
// it does not accept.
func TestConfigRejectsADefaultModelItsOwnAllowListDenies(t *testing.T) {
	_, err := Load(writeConfig(t, `{"agents":[{"name":"codex","models":["gpt-5.4"],"defaultModel":"gpt-5.6-sol"}]}`))
	if err == nil {
		t.Fatal("Load() error = nil, want the conflicting default rejected")
	}
	if !strings.Contains(err.Error(), "defaultModel") {
		t.Fatalf("Load() error = %v, want it to name the field", err)
	}
}
