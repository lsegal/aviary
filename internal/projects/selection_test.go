package projects

import (
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lsegal/aviary/internal/projects/agents"
)

func TestRepositoryFilterAddsTheReadyLabel(t *testing.T) {
	for _, test := range []struct {
		name       string
		gh         GHCLI
		wantFilter string
		wantAll    bool
	}{
		{"no label", GHCLI{Filter: defaultIssueFilter}, defaultIssueFilter, false},
		{"label with filter", GHCLI{Filter: defaultIssueFilter, ReadyLabel: "ready"}, defaultIssueFilter + " label:ready", false},
		{"label survives all issues", GHCLI{Filter: defaultIssueFilter, AllIssues: true, ReadyLabel: "ready"}, "label:ready", false},
		{"all issues without a label", GHCLI{Filter: defaultIssueFilter, AllIssues: true}, defaultIssueFilter, true},
		{"quoted label", GHCLI{ReadyLabel: "ready to go"}, `label:"ready to go"`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			filter, all := test.gh.repositoryFilter()
			if filter != test.wantFilter || all != test.wantAll {
				t.Fatalf("repositoryFilter() = %q, %v; want %q, %v", filter, all, test.wantFilter, test.wantAll)
			}
		})
	}
	if got := publicIssueSearchQuery("o/r", "label:ready", false, ""); got != "repo:o/r is:issue state:open label:ready" {
		t.Fatalf("query = %q", got)
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
	registry := agents.MustBuiltin()
	spec, err := parseAgentSpecIn(registry, "aviary/reviewer")
	if err != nil {
		t.Fatal(err)
	}
	definition, _ := registry.Lookup(spec.Name)
	got := definition.Render(agents.ModeRun, agents.Values{Model: spec.Model, Prompt: "/gh-fix o/r#1"})
	if want := []string{"agent", "run", "reviewer", "/gh-fix o/r#1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
	if _, err := parseAgentSpecIn(registry, "aviary/reviewer:high"); err == nil {
		t.Fatal("an aviary agent accepted a level")
	}
	binaries := withAviaryBinary(map[string]string{"codex": "/opt/codex"})
	if binaries["aviary"] == "" || binaries["codex"] != "/opt/codex" {
		t.Fatalf("binaries = %v", binaries)
	}
	if got := withAviaryBinary(map[string]string{"aviary": "/x"})["aviary"]; got != "/x" {
		t.Fatalf("a configured aviary binary was replaced: %q", got)
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
