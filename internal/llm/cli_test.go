package llm

import (
	"os"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/clidefs"
)

// resetCLIProviders restores the built-in CLI providers after a test.
func resetCLIProviders(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if err := ConfigureCLIProviders(nil); err != nil {
			t.Fatal(err)
		}
	})
}

// TestCLIProvidersPointAviaryAtRunningExecutable checks the aviary CLI
// provider runs this executable unless cli_definitions names another.
func TestCLIProvidersPointAviaryAtRunningExecutable(t *testing.T) {
	resetCLIProviders(t)
	executable, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	if err := ConfigureCLIProviders(nil); err != nil {
		t.Fatal(err)
	}
	if definition, _ := CLIProvider(clidefs.Aviary); definition.Binary != executable {
		t.Fatalf("aviary binary = %q, want the running executable %q", definition.Binary, executable)
	}
	if err := ConfigureCLIProviders(map[string]any{clidefs.Aviary: map[string]any{"binary": "/opt/aviary"}}); err != nil {
		t.Fatal(err)
	}
	if definition, _ := CLIProvider(clidefs.Aviary); definition.Binary != "/opt/aviary" {
		t.Fatalf("aviary binary = %q, want the cli_definitions override", definition.Binary)
	}
}

// TestCLIProvidersIncludeEveryBuiltinCLI checks the provider layer serves
// the built-in coding CLIs beside the API providers.
func TestCLIProvidersIncludeEveryBuiltinCLI(t *testing.T) {
	resetCLIProviders(t)
	for _, name := range []string{"claude", "codex", "cline", "muse", "gemini", clidefs.Aviary} {
		if _, ok := CLIProvider(name); !ok {
			t.Fatalf("CLI provider %q is not registered", name)
		}
	}
}

// TestConfigureCLIProvidersKeepsPreviousOnError checks invalid definitions
// are reported, leave the last good providers in force for lookups, and make
// CLIProviderRegistry refuse until fixed.
func TestConfigureCLIProvidersKeepsPreviousOnError(t *testing.T) {
	resetCLIProviders(t)
	good := map[string]any{"codex": map[string]any{"binary": "/opt/codex-nightly"}}
	if err := ConfigureCLIProviders(good); err != nil {
		t.Fatal(err)
	}
	if got := CLIProviderDefinitions(); got["codex"] == nil {
		t.Fatalf("definitions = %v, want the configured ones", got)
	}
	err := ConfigureCLIProviders(map[string]any{"codex": map[string]any{"nosuchfield": true}})
	if err == nil || !strings.Contains(err.Error(), "cli_definitions") {
		t.Fatalf("error = %v, want the invalid definitions reported", err)
	}
	if definition, _ := CLIProvider("codex"); definition.Binary != "/opt/codex-nightly" {
		t.Fatalf("codex binary = %q, want the last good definitions", definition.Binary)
	}
	if _, err := CLIProviderRegistry(); err == nil {
		t.Fatal("CLIProviderRegistry accepted invalid definitions")
	}
	if err := ConfigureCLIProviders(good); err != nil {
		t.Fatal(err)
	}
	if _, err := CLIProviderRegistry(); err != nil {
		t.Fatalf("CLIProviderRegistry error = %v after fixing the definitions", err)
	}
}

// TestUnknownCLIProviderErrorListsKnown checks a typo names what exists.
func TestUnknownCLIProviderErrorListsKnown(t *testing.T) {
	resetCLIProviders(t)
	err := UnknownCLIProviderError(CLIProviders(), "nosuch")
	for _, want := range append([]string{`unknown agent "nosuch"`}, CLIProviders().Names()...) {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to mention %s", err, want)
		}
	}
}
