package projects

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lsegal/aviary/internal/clidefs"
)

func TestIssueFilterReportsTheRealSelection(t *testing.T) {
	if got := issueFilter(engineSettings{Filter: defaultIssueFilter}); got != defaultIssueFilter {
		t.Fatalf("issueFilter() = %q, want %q", got, defaultIssueFilter)
	}
	if got := issueFilter(engineSettings{Filter: defaultIssueFilter, AllIssues: true}); got != "" {
		t.Fatalf("all issues should report no filter, got %q", got)
	}
}

func TestQuotaReadingExhausted(t *testing.T) {
	for reading, want := range map[string]bool{
		"":                                 false,
		"quota 40% left":                   false,
		"weekly 0% left":                   true,
		"session 0% left, week 12% left":   true,
		"session 10% left, week 100% left": false,
		"unavailable: signed out":          false,
	} {
		if got := quotaReadingExhausted(reading); got != want {
			t.Errorf("quotaReadingExhausted(%q) = %v, want %v", reading, got, want)
		}
	}
}

func TestAgentNameForFollowsTheProjectFallbackOrder(t *testing.T) {
	quotas := map[string]string{"claude": "session 0% left", "codex": "quota 50% left"}
	runner := CommandRunner{
		Agents:       []string{"codex"},
		TargetAgents: map[string][]string{"o/r": {"claude/opus", "codex/gpt-5.6-terra:high"}},
		Quotas:       func() map[string]string { return quotas },
	}
	if got := runner.AgentNameFor("o/r"); got != "codex/gpt-5.6-terra:high" {
		t.Fatalf("exhausted first choice: got %q", got)
	}
	quotas["claude"] = "session 3% left"
	if got := runner.AgentNameFor("o/r"); got != "claude/opus" {
		t.Fatalf("available first choice: got %q", got)
	}
	quotas["claude"], quotas["codex"] = "session 0% left", "quota 0% left"
	if got := runner.AgentNameFor("o/r"); got != "claude/opus" {
		t.Fatalf("all exhausted should fall back to the first: got %q", got)
	}
	if got := runner.AgentNameFor("other/repo"); got != "codex" {
		t.Fatalf("a target without its own agents uses Agents: got %q", got)
	}
}

func TestAgentNameForSkipsExhaustedAgentsWhenBalancing(t *testing.T) {
	runner := CommandRunner{
		Agents:      []string{"claude", "codex"},
		agentCursor: &atomic.Uint64{},
		Quotas:      func() map[string]string { return map[string]string{"claude": "week 0% left"} },
	}
	var got []string
	for range 3 {
		got = append(got, runner.AgentNameFor("o/r"))
	}
	if want := []string{"codex", "codex", "codex"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("balanced picks = %v, want %v", got, want)
	}
}

func TestConfiguredAgentsIncludeProjectAgents(t *testing.T) {
	w := &Engine{Runner: CommandRunner{Agents: []string{"codex"}, TargetAgents: map[string][]string{"o/r": {"claude/opus", "codex"}}}}
	if got, want := w.configuredAgents(), []string{"codex", "claude/opus"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("configuredAgents() = %v, want %v", got, want)
	}
	if !w.agentStillConfigured("claude/opus") {
		t.Fatal("a project's own agent was treated as no longer configured")
	}
}

func TestAviaryAgentSpecRunsTheNamedAgent(t *testing.T) {
	registry := clidefs.MustBuiltin()
	spec, err := parseAgentSpecIn(registry, "aviary/reviewer")
	if err != nil {
		t.Fatal(err)
	}
	definition, _ := registry.Lookup(spec.Name)
	if !definition.InProcess || spec.Model != "reviewer" {
		t.Fatalf("spec = %+v, in-process = %v", spec, definition.InProcess)
	}
	if _, err := parseAgentSpecIn(registry, "aviary/reviewer:high"); err == nil {
		t.Fatal("an aviary agent accepted a level")
	}
}

func TestManagerRejectsAnAviarySpecWithoutAnAgent(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.startRun(engineSettings{Targets: []string{"o/r"}, Agents: []string{"aviary"}}); err == nil || !strings.Contains(err.Error(), "aviary/<agent>") {
		t.Fatalf("startRun error = %v", err)
	}
}

// TestGHRunParsesStdoutOnly checks a notice gh prints on stderr never reaches
// the JSON a successful call is parsed from, while a failure still carries it.
func TestGHRunParsesStdoutOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as gh")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "gh")
	body := "#!/bin/sh\necho 'hint: a notice' >&2\nif [ \"$1\" = fail ]; then echo 'HTTP 404' >&2; exit 1; fi\necho '[]'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	gh := GHCLI{Binary: script}
	output, err := gh.run(t.Context(), "api", "x")
	if err != nil || strings.TrimSpace(string(output)) != "[]" {
		t.Fatalf("run() = %q, %v; want only stdout", output, err)
	}
	output, err = gh.run(t.Context(), "fail")
	if err == nil || !strings.Contains(string(output), "HTTP 404") {
		t.Fatalf("failed run() = %q, %v; want stderr included", output, err)
	}
}
