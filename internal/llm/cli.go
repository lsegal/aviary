package llm

import (
	"fmt"
	"maps"
	"strings"
	"sync"

	"github.com/lsegal/aviary/internal/clidefs"
)

// Providers reached through a coding CLI (claude, codex, cline, muse, gemini,
// aviary and any in models.cli_definitions) sit in the provider layer beside
// the API and OAuth providers above. Their definitions say how to invoke the
// CLI; their installed, version, sign-in, quota and model state is read by
// clidoctor. Everything that runs a CLI, Projects included, resolves it here.

// cliProviders is the CLI provider set in force.
var cliProviders struct {
	mu          sync.RWMutex
	registry    *clidefs.Registry
	definitions map[string]any
	err         error
}

// ConfigureCLIProviders installs the CLI providers the models: section's
// cli_definitions describe, merged over the built-in definitions. When the
// definitions are invalid the error is returned and kept for CLIProviderRegistry
// to report, and the previous providers stay in force.
func ConfigureCLIProviders(definitions map[string]any) error {
	registry, err := clidefs.FromConfig(definitions)
	cliProviders.mu.Lock()
	defer cliProviders.mu.Unlock()
	cliProviders.definitions = maps.Clone(definitions)
	cliProviders.err = err
	if err != nil {
		return fmt.Errorf("models.cli_definitions: %w", err)
	}
	cliProviders.registry = registry
	return nil
}

// SetCLIProviderRegistry installs an already-built CLI provider set, for
// callers and tests that assemble their own definitions.
func SetCLIProviderRegistry(registry *clidefs.Registry) {
	cliProviders.mu.Lock()
	defer cliProviders.mu.Unlock()
	cliProviders.registry = registry
	cliProviders.definitions = nil
	cliProviders.err = nil
}

// CLIProviders returns the CLI providers in force. Until a config has been
// applied that is the built-in set, with the aviary provider pointing at the
// running executable.
func CLIProviders() *clidefs.Registry {
	cliProviders.mu.RLock()
	registry := cliProviders.registry
	cliProviders.mu.RUnlock()
	if registry != nil {
		return registry
	}
	cliProviders.mu.Lock()
	defer cliProviders.mu.Unlock()
	if cliProviders.registry == nil {
		registry, err := clidefs.FromConfig(nil)
		if err != nil {
			// The built-in documents are embedded and validated by clidefs'
			// own tests, so this is a build that shipped a broken one.
			panic(err)
		}
		cliProviders.registry = registry
	}
	return cliProviders.registry
}

// CLIProviderRegistry returns the CLI providers in force, or the error the
// last ConfigureCLIProviders reported, so a caller that must not run on stale
// definitions can refuse to start.
func CLIProviderRegistry() (*clidefs.Registry, error) {
	cliProviders.mu.RLock()
	err := cliProviders.err
	cliProviders.mu.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("models.cli_definitions: %w", err)
	}
	return CLIProviders(), nil
}

// CLIProviderDefinitions returns the cli_definitions last configured, so a
// caller can tell when they change.
func CLIProviderDefinitions() map[string]any {
	cliProviders.mu.RLock()
	defer cliProviders.mu.RUnlock()
	return maps.Clone(cliProviders.definitions)
}

// CLIProvider resolves one CLI provider by name.
func CLIProvider(name string) (clidefs.Definition, bool) {
	return CLIProviders().Lookup(name)
}

// UnknownCLIProviderError reports a CLI provider name registry does not
// define, listing what it does define so a typo is obvious.
func UnknownCLIProviderError(registry *clidefs.Registry, name string) error {
	return fmt.Errorf("unknown agent %q; known agents are %s", name, strings.Join(registry.Names(), ", "))
}
