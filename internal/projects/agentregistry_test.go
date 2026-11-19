package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/projects/agents"
)

// TestConfigPathIsReadBeforeTheFlagsAre checks --config is found in the raw
// argument list whatever order it was written in. Definitions have to be
// loaded before --agent is validated against them, and the flag package hands
// values over in the order they appear, so `--agent mine --config x.json`
// would otherwise fail on an agent the config file defines.
func TestConfigPathIsReadBeforeTheFlagsAre(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"absent", []string{"--agent", "codex", "o/r"}, agents.DefaultConfigPath},
		{"after another flag", []string{"--agent", "mine", "--config", "custom.json"}, "custom.json"},
		{"single dash", []string{"-config", "custom.json"}, "custom.json"},
		{"joined value", []string{"--config=custom.json", "--agent", "mine"}, "custom.json"},
		{"after the terminator", []string{"--", "--config", "custom.json"}, agents.DefaultConfigPath},
		{"trailing with no value", []string{"--config"}, agents.DefaultConfigPath},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := configPathFromArgs(test.args, agents.DefaultConfigPath); got != test.want {
				t.Fatalf("configPathFromArgs(%v) = %q, want %q", test.args, got, test.want)
			}
		})
	}
}

// TestWorkStateFileWithAgentDefinitionsIsRejected checks definitions written
// into the --state file are reported rather than destroyed. glorp rewrites
// that file as issues are handled, so a hand-edited definition living there
// would not survive the next save.
func TestWorkStateFileWithAgentDefinitionsIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".glorp.json")
	if err := os.WriteFile(path, []byte(`{"agents":[{"name":"acme"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := guardWorkStateFile(path)
	if err == nil {
		t.Fatal("a state file holding agent definitions was accepted")
	}
	for _, want := range []string{path, agents.DefaultConfigPath, "--state"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to mention %s", err, want)
		}
	}
	// A real work-state file, and a file that is not there at all, are both
	// none of this check's business.
	if err := os.WriteFile(path, []byte(`{"o/r#12":{"status":"completed"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := guardWorkStateFile(path); err != nil {
		t.Fatalf("a work-state file was rejected: %v", err)
	}
	if err := guardWorkStateFile(filepath.Join(t.TempDir(), "absent.json")); err != nil {
		t.Fatalf("a missing state file was rejected: %v", err)
	}
}

// TestUnknownAgentSpecListsTheKnownAgents checks the error --agent gives is
// built from the registry rather than from a hardcoded pair of names.
func TestUnknownAgentSpecListsTheKnownAgents(t *testing.T) {
	_, err := parseAgentSpec("nosuchagent")
	if err == nil {
		t.Fatal("an unknown agent was accepted")
	}
	want := append([]string{`unknown agent "nosuchagent"`}, agents.MustBuiltin().Names()...)
	for _, want := range want {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to mention %s", err, want)
		}
	}
}

// TestAgentLevelErrorComesFromTheDefinition checks the level message is built
// from the agent's own allow-list, so an agent with different levels does not
// report Codex's.
func TestAgentLevelErrorComesFromTheDefinition(t *testing.T) {
	_, err := parseAgentSpec("codex:turbo")
	if err == nil || !strings.Contains(err.Error(), "agent level must be low, medium, or high") {
		t.Fatalf("error = %v, want the built-in levels", err)
	}
	registry, err := agents.NewRegistry(agents.Definition{
		Name: "acme", Binary: "acme", Levels: agents.NewAllowList("fast", "thorough"),
		Args:    agents.Args{Run: []agents.Fragment{{Args: []string{"{prompt}"}}}, Resume: []agents.Fragment{{Args: []string{"{prompt}"}}}},
		Session: agents.Session{Assign: agents.AssignNone},
		Output:  agents.Output{Format: agents.FormatText},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseAgentSpecIn(registry, "acme:high"); err == nil || !strings.Contains(err.Error(), "fast or thorough") {
		t.Fatalf("error = %v, want the definition's own levels", err)
	}
	if _, err := parseAgentSpecIn(registry, "acme:fast"); err != nil {
		t.Fatalf("a level the definition lists was rejected: %v", err)
	}
}

// TestAgentModelAllowListIsEnforced checks a definition that enumerates its
// models rejects one it does not list, and that the built-ins, which enumerate
// none, keep accepting any model.
func TestAgentModelAllowListIsEnforced(t *testing.T) {
	registry, err := agents.NewRegistry(agents.Definition{
		Name: "acme", Binary: "acme", Models: agents.NewAllowList("acme-1"),
		Args:    agents.Args{Run: []agents.Fragment{{Args: []string{"{prompt}"}}}, Resume: []agents.Fragment{{Args: []string{"{prompt}"}}}},
		Session: agents.Session{Assign: agents.AssignNone},
		Output:  agents.Output{Format: agents.FormatText},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseAgentSpecIn(registry, "acme/acme-2"); err == nil || !strings.Contains(err.Error(), "acme-1") {
		t.Fatalf("error = %v, want the definition's own models", err)
	}
	if _, err := parseAgentSpec("claude/anything-at-all"); err != nil {
		t.Fatalf("an agent that lists no models rejected one: %v", err)
	}
}

// TestAgentAssignsSessionIDFollowsTheDefinition checks which agents glorp
// generates a session ID for is read from the registry, and that an agent it
// has never heard of keeps the historical behaviour of being given one.
func TestAgentAssignsSessionIDFollowsTheDefinition(t *testing.T) {
	if !agentAssignsSessionID("claude/opus:high") {
		t.Fatal("claude should be given a session ID")
	}
	if agentAssignsSessionID("codex") {
		t.Fatal("codex assigns its own session ID")
	}
	if !agentAssignsSessionID("some-old-agent") {
		t.Fatal("an unknown agent should keep the generated ID")
	}
}

// TestDefinitionEnvIsStable checks the child environment a definition asks for
// is rendered in a fixed order, so a run does not vary with map iteration.
func TestDefinitionEnvIsStable(t *testing.T) {
	definition := agents.Definition{Env: map[string]string{"B": "2", "A": "1", "C": "3"}}
	want := []string{"A=1", "B=2", "C=3"}
	for i := 0; i < 5; i++ {
		got := definitionEnv(definition)
		if len(got) != len(want) {
			t.Fatalf("env = %v, want %v", got, want)
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("env = %v, want %v", got, want)
			}
		}
	}
	if got := definitionEnv(agents.Definition{}); got != nil {
		t.Fatalf("env = %v, want none", got)
	}
}

// TestBinaryFallsBackToTheDefinition checks an agent with no flag of its own
// is invoked through the executable its definition names, which is what makes
// a configured agent runnable at all.
func TestBinaryFallsBackToTheDefinition(t *testing.T) {
	registry, err := agents.NewRegistry(agents.Definition{
		Name: "acme", Binary: "/opt/acme",
		Args:    agents.Args{Run: []agents.Fragment{{Args: []string{"{prompt}"}}}, Resume: []agents.Fragment{{Args: []string{"{prompt}"}}}},
		Session: agents.Session{Assign: agents.AssignNone},
		Output:  agents.Output{Format: agents.FormatText},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := CommandRunner{Agent: "acme", Definitions: registry}
	if got := runner.binary("acme"); got != "/opt/acme" {
		t.Fatalf("binary = %q, want the definition's own", got)
	}
	// Binary holds the default agent's executable, so it does not stand in for
	// the configured agent's own.
	runner.Binary = "codex"
	if got := runner.binary("acme"); got != "/opt/acme" {
		t.Fatalf("binary = %q, want the definition's own rather than the default agent's", got)
	}
	// The agents that do have a flag of their own still take it.
	flagged := CommandRunner{Agent: "claude", ClaudeBinary: "claude-bin", CodexBinary: "codex-bin"}
	if got := flagged.binary("claude"); got != "claude-bin" {
		t.Fatalf("binary = %q, want the --claude-binary value", got)
	}
	if got := flagged.binary("codex"); got != "codex-bin" {
		t.Fatalf("binary = %q, want the --codex-binary value", got)
	}
}

// TestAgentBinaryOverridesEveryOtherSource pins the resolution order
// --agent-binary, legacy alias flag, definition binary, run default (issue
// #489). --agent-binary is the only source that can reach a config-defined
// agent, so it has to outrank the two flags that predate it.
func TestAgentBinaryOverridesEveryOtherSource(t *testing.T) {
	registry, err := agents.NewRegistry(
		agents.Definition{
			Name: "muse", Binary: "/opt/muse",
			Args:    agents.Args{Run: []agents.Fragment{{Args: []string{"{prompt}"}}}, Resume: []agents.Fragment{{Args: []string{"{prompt}"}}}},
			Session: agents.Session{Assign: agents.AssignNone},
			Output:  agents.Output{Format: agents.FormatText},
		},
		agents.Definition{
			Name: "codex", Binary: "codex",
			Args:    agents.Args{Run: []agents.Fragment{{Args: []string{"{prompt}"}}}, Resume: []agents.Fragment{{Args: []string{"{prompt}"}}}},
			Session: agents.Session{Assign: agents.AssignNone},
			Output:  agents.Output{Format: agents.FormatText},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	runner := CommandRunner{
		Agent: "codex", Binary: "run-default", CodexBinary: "codex-bin", Definitions: registry,
		AgentBinaries: map[string]string{"codex": "/opt/codex-nightly", "muse": "/opt/muse-nightly"},
	}
	if got := runner.binary("codex"); got != "/opt/codex-nightly" {
		t.Fatalf("binary = %q, want --agent-binary to outrank --codex-binary", got)
	}
	if got := runner.binary("muse"); got != "/opt/muse-nightly" {
		t.Fatalf("binary = %q, want --agent-binary to reach a config-defined agent", got)
	}
	// Without an override the legacy flag, then the definition, then the run
	// default still answer in that order.
	runner.AgentBinaries = nil
	if got := runner.binary("codex"); got != "codex-bin" {
		t.Fatalf("binary = %q, want the legacy alias flag", got)
	}
	if got := runner.binary("muse"); got != "/opt/muse" {
		t.Fatalf("binary = %q, want the definition's own", got)
	}
	if got := runner.binary("stranger"); got != "run-default" {
		t.Fatalf("binary = %q, want the run default for an agent no definition covers", got)
	}
}

// TestAgentBinaryFlagParsesNameEqualsPath covers the flag itself: the value
// has to name a registered agent, so a typo is reported where it is written
// rather than by the wrong executable running later.
func TestAgentBinaryFlagParsesNameEqualsPath(t *testing.T) {
	var flag agentBinaryFlag
	if err := flag.Set("claude=/opt/claude"); err != nil {
		t.Fatal(err)
	}
	if err := flag.Set("codex=/opt/codex"); err != nil {
		t.Fatal(err)
	}
	// A repeat replaces rather than accumulates, matching every other flag.
	if err := flag.Set("claude=/usr/bin/claude"); err != nil {
		t.Fatal(err)
	}
	values := flag.values()
	if values["claude"] != "/usr/bin/claude" || values["codex"] != "/opt/codex" {
		t.Fatalf("values = %v", values)
	}
	if got := flag.String(); got != "claude=/usr/bin/claude,codex=/opt/codex" {
		t.Fatalf("String() = %q", got)
	}
	if (&agentBinaryFlag{}).values() != nil {
		t.Fatal("an unset flag should contribute no overrides")
	}
	for _, bad := range []string{"claude", "=/opt/claude", "claude=", "  =  "} {
		if err := (&agentBinaryFlag{}).Set(bad); err == nil {
			t.Fatalf("Set(%q) accepted a malformed value", bad)
		}
	}
	err := (&agentBinaryFlag{}).Set("bogus=/opt/bogus")
	if err == nil {
		t.Fatal("Set accepted an unregistered agent")
	}
	if !strings.Contains(err.Error(), "known agents are") {
		t.Fatalf("error = %v, want it to list the known agents", err)
	}
}

// TestAgentThatTakesNoLevelRejectsOne checks the declared-empty allow-list
// reaches --agent: gemini has no reasoning-effort flag and no {level} fragment
// to render one into, so a level is refused at startup instead of being parsed
// and then dropped, which from the prompt looked exactly like one that was
// honoured (issue #532).
func TestAgentThatTakesNoLevelRejectsOne(t *testing.T) {
	_, err := parseAgentSpec("gemini:high")
	if err == nil || !strings.Contains(err.Error(), "agent gemini takes no reasoning level") {
		t.Fatalf("error = %v, want gemini to report that it takes no level", err)
	}
	// A bare colon is a level too, and an empty one is no more renderable.
	if _, err := parseAgentSpec("gemini:"); err == nil {
		t.Fatal("an empty level was accepted by an agent that takes none")
	}
	// The agent itself, and a model for it, keep working: the model list is
	// deliberately left open because Gemini's model names move too fast to pin.
	for _, value := range []string{"gemini", "gemini/gemini-2.5-pro", "gemini/anything-at-all"} {
		if _, err := parseAgentSpec(value); err != nil {
			t.Fatalf("parseAgentSpec(%q) error = %v", value, err)
		}
	}
	// Every agent that does declare levels is untouched by the new state.
	for _, agent := range []string{"codex", "claude", "cline", "muse"} {
		definition, ok := agents.MustBuiltin().Lookup(agent)
		if !ok {
			t.Fatalf("%s is not registered", agent)
		}
		if !definition.Levels.Declared() || definition.Levels.AcceptsNothing() {
			t.Fatalf("%s no longer declares the levels it accepts", agent)
		}
		if _, err := parseAgentSpec(agent + ":high"); err != nil {
			t.Fatalf("parseAgentSpec(%q) error = %v", agent+":high", err)
		}
	}
}
