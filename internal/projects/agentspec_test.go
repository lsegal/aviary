package projects

import (
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/llm"
)

// TestUnknownAgentSpecListsTheKnownAgents checks the error --agent gives is
// built from the registry rather than from a hardcoded pair of names.
func TestUnknownAgentSpecListsTheKnownAgents(t *testing.T) {
	_, err := parseAgentSpec("nosuchagent")
	if err == nil {
		t.Fatal("an unknown agent was accepted")
	}
	want := append([]string{`unknown agent "nosuchagent"`}, clidefs.MustBuiltin().Names()...)
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
	registry, err := clidefs.NewRegistry(clidefs.Definition{
		Name: "acme", Binary: "acme", Levels: clidefs.NewAllowList("fast", "thorough"),
		Args:    clidefs.Args{Run: []clidefs.Fragment{{Args: []string{"{prompt}"}}}, Resume: []clidefs.Fragment{{Args: []string{"{prompt}"}}}},
		Session: clidefs.Session{Assign: clidefs.AssignNone},
		Output:  clidefs.Output{Format: clidefs.FormatText},
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
	registry, err := clidefs.NewRegistry(clidefs.Definition{
		Name: "acme", Binary: "acme", Models: clidefs.NewAllowList("acme-1"),
		Args:    clidefs.Args{Run: []clidefs.Fragment{{Args: []string{"{prompt}"}}}, Resume: []clidefs.Fragment{{Args: []string{"{prompt}"}}}},
		Session: clidefs.Session{Assign: clidefs.AssignNone},
		Output:  clidefs.Output{Format: clidefs.FormatText},
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

// TestAgentAssignsSessionIDFollowsTheDefinition checks which agents aviary
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

// TestBinaryFallsBackToTheDefinition checks an agent with no flag of its own
// is invoked through the executable its definition names, which is what makes
// a configured agent runnable at all.
func TestBinaryFallsBackToTheDefinition(t *testing.T) {
	registry, err := clidefs.NewRegistry(clidefs.Definition{
		Name: "acme", Binary: "/opt/acme",
		Args:    clidefs.Args{Run: []clidefs.Fragment{{Args: []string{"{prompt}"}}}, Resume: []clidefs.Fragment{{Args: []string{"{prompt}"}}}},
		Session: clidefs.Session{Assign: clidefs.AssignNone},
		Output:  clidefs.Output{Format: clidefs.FormatText},
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

// TestDefinitionBinaryOverridesBuiltin pins the resolution order legacy
// alias flag, definition binary, run default. A binary override in
// models.cli_definitions is the only config source of an executable (#456),
// so it reaches built-in and config-defined agents alike.
func TestDefinitionBinaryOverridesBuiltin(t *testing.T) {
	registry, err := clidefs.FromConfig(map[string]any{
		"codex": map[string]any{"binary": "/opt/codex-nightly"},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := CommandRunner{Agent: "codex", Binary: "run-default", Definitions: registry}
	if got := runner.binary("codex"); got != "/opt/codex-nightly" {
		t.Fatalf("binary = %q, want the cli_definitions override", got)
	}
	if got := runner.binary("claude"); got != "claude" {
		t.Fatalf("binary = %q, want the built-in definition's own", got)
	}
	if got := runner.binary("stranger"); got != "run-default" {
		t.Fatalf("binary = %q, want the run default for an agent no definition covers", got)
	}
	runner.CodexBinary = "codex-bin"
	if got := runner.binary("codex"); got != "codex-bin" {
		t.Fatalf("binary = %q, want the legacy alias flag", got)
	}
}

// TestAgentThatTakesNoLevelRejectsOne checks the declared-empty allow-list
// reaches --agent: gemini has no reasoning-effort flag and no {level} fragment
// to render one into, so a level is refused at startup instead of being parsed
// and then dropped, which from the prompt looked exactly like one that was
// honoured.
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
		definition, ok := clidefs.MustBuiltin().Lookup(agent)
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

// TestDispatchResolvesThroughCoreCLIProviders checks Projects has no CLI
// registry of its own: the cli_definitions configured in core's provider layer
// are the ones a job spec parses against and the executable it runs, and
// invalid definitions keep a run from starting.
func TestDispatchResolvesThroughCoreCLIProviders(t *testing.T) {
	t.Cleanup(func() { _ = llm.ConfigureCLIProviders(nil) })
	if err := llm.ConfigureCLIProviders(map[string]any{
		"claude": map[string]any{"binary": "/opt/claude-nightly"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := parseAgentSpec("claude/opus:high"); err != nil {
		t.Fatalf("parseAgentSpec(claude/opus:high) error = %v", err)
	}
	runner := CommandRunner{Agent: "claude/opus", Binary: "run-default"}
	if got := runner.binary("claude"); got != "/opt/claude-nightly" {
		t.Fatalf("binary = %q, want core's cli_definitions override", got)
	}
	if err := llm.ConfigureCLIProviders(map[string]any{"claude": map[string]any{"nosuchfield": true}}); err == nil {
		t.Fatal("invalid cli_definitions were accepted")
	}
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.startRun(engineSettings{Targets: []string{"o/r"}, Agents: []string{"claude/opus"}}); err == nil || !strings.Contains(err.Error(), "cli_definitions") {
		t.Fatalf("startRun error = %v, want the invalid cli_definitions reported", err)
	}
}
