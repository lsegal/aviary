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
// the built-in CLI providers beside the API providers.
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

// saveCLIProviders restores the CLI provider state in force when the test
// started, including a nil registry, recorded definitions and error.
func saveCLIProviders(t *testing.T) {
	t.Helper()
	cliProviders.mu.RLock()
	registry, definitions, err := cliProviders.registry, cliProviders.definitions, cliProviders.err
	cliProviders.mu.RUnlock()
	t.Cleanup(func() {
		cliProviders.mu.Lock()
		defer cliProviders.mu.Unlock()
		cliProviders.registry, cliProviders.definitions, cliProviders.err = registry, definitions, err
	})
}

// TestSetCLIProviderRegistryReplacesConfiguredProviders checks an installed
// registry is the one served, and that it clears the definitions and error a
// previous ConfigureCLIProviders recorded.
func TestSetCLIProviderRegistryReplacesConfiguredProviders(t *testing.T) {
	saveCLIProviders(t)
	if err := ConfigureCLIProviders(map[string]any{"codex": map[string]any{"nosuchfield": true}}); err == nil {
		t.Fatal("ConfigureCLIProviders accepted invalid definitions")
	}
	if _, err := CLIProviderRegistry(); err == nil {
		t.Fatal("CLIProviderRegistry did not report the invalid definitions")
	}
	claude, ok := clidefs.MustBuiltin().Lookup("claude")
	if !ok {
		t.Fatal("built-in claude definition missing")
	}
	claude.Binary = "/opt/claude-test"
	custom, err := clidefs.NewRegistry(claude)
	if err != nil {
		t.Fatal(err)
	}

	SetCLIProviderRegistry(custom)

	if got := CLIProviders(); got != custom {
		t.Fatalf("CLIProviders() = %p, want the installed registry %p", got, custom)
	}
	registry, err := CLIProviderRegistry()
	if err != nil {
		t.Fatalf("CLIProviderRegistry error = %v, want it cleared", err)
	}
	if registry != custom {
		t.Fatalf("CLIProviderRegistry() = %p, want the installed registry %p", registry, custom)
	}
	if got := CLIProviderDefinitions(); got != nil {
		t.Fatalf("definitions = %v, want them cleared", got)
	}
	if definition, ok := CLIProvider("claude"); !ok || definition.Binary != "/opt/claude-test" {
		t.Fatalf("claude = %+v, %v; want the installed definition", definition, ok)
	}
	if _, ok := CLIProvider("codex"); ok {
		t.Fatal("codex resolved, want only the installed registry's providers")
	}
}

// TestCLIProvidersBuildsBuiltinsWhenUnset checks CLIProviders lazily builds
// the built-in set when no registry is installed, and keeps serving it.
func TestCLIProvidersBuildsBuiltinsWhenUnset(t *testing.T) {
	saveCLIProviders(t)
	SetCLIProviderRegistry(nil)

	first := CLIProviders()
	if first == nil {
		t.Fatal("CLIProviders() = nil, want the built-in set")
	}
	if got, want := strings.Join(first.Names(), ","), strings.Join(clidefs.MustBuiltin().Names(), ","); got != want {
		t.Fatalf("names = %s, want the built-in names %s", got, want)
	}
	if second := CLIProviders(); second != first {
		t.Fatalf("second CLIProviders() = %p, want the lazily built %p", second, first)
	}
	if registry, err := CLIProviderRegistry(); err != nil || registry != first {
		t.Fatalf("CLIProviderRegistry() = %p, %v; want %p, nil", registry, err, first)
	}
}

// TestIsAPIProvider checks every API provider is recognized and CLI or
// unknown names are not.
func TestIsAPIProvider(t *testing.T) {
	providers := APIProviders()
	if len(providers) == 0 {
		t.Fatal("APIProviders() is empty")
	}
	for _, name := range providers {
		if !IsAPIProvider(name) {
			t.Errorf("IsAPIProvider(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"claude", clidefs.Aviary, "cli", "nosuch", "", "Anthropic"} {
		if IsAPIProvider(name) {
			t.Errorf("IsAPIProvider(%q) = true, want false", name)
		}
	}
	providers[0] = "mutated"
	if IsAPIProvider("mutated") || APIProviders()[0] == "mutated" {
		t.Fatal("APIProviders returned the package slice, want a copy")
	}
}

// TestUnknownProviderErrorListsProviders checks the error names the bad
// provider and model and lists both API and CLI providers.
func TestUnknownProviderErrorListsProviders(t *testing.T) {
	registry := clidefs.MustBuiltin()
	err := UnknownProviderError(registry, "nosuch", "nosuch/model-1")
	wants := append([]string{`unknown provider "nosuch"`, `"nosuch/model-1"`}, APIProviders()...)
	wants = append(wants, registry.Providers()...)
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to mention %s", err, want)
		}
	}
}
