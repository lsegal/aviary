package clidefs

import (
	"strings"
	"testing"
)

// TestBuiltinAgentsAskTheirCLIForModels holds every shipped agent that exposes
// a model catalog to reading it from the CLI, which avoids carrying stale
// lists. Claude Code has no listing command, so its maintained aliases are the
// exception and remain suggestions rather than an allow-list.
func TestBuiltinAgentsAskTheirCLIForModels(t *testing.T) {
	registry := MustBuiltin()
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
	registry := MustBuiltin()
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

// TestClineAndGeminiAskOverTheAgentProtocol checks the two CLIs with no
// listing subcommand are asked the way they can be asked -- an ACP handshake
// whose session answers with its available models -- rather than carrying a
// provider-scoped list that goes stale, which is what issue #566 was.
func TestClineAndGeminiAskOverTheAgentProtocol(t *testing.T) {
	registry := MustBuiltin()
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
