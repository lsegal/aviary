package projects

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/lsegal/aviary/internal/projects/agents"
)

// registeredAgents is the agent registry the run dispatches with: the built-in
// definitions until a config file has been read, and the merged set afterwards.
// It is read from every goroutine that renders an agent invocation and written
// once, before any of them start, so it is held atomically rather than behind a
// mutex nothing else would use.
var registeredAgents atomic.Pointer[agents.Registry]

// agentRegistry returns the registry in force. Nothing has to have loaded a
// config file first: the built-in definitions are the registry until one is.
func agentRegistry() *agents.Registry {
	if registry := registeredAgents.Load(); registry != nil {
		return registry
	}
	// The built-in documents are embedded and validated by this package's own
	// tests, so a failure here is a aviary build that shipped a broken one.
	registry := agents.MustBuiltin()
	registeredAgents.CompareAndSwap(nil, registry)
	return registeredAgents.Load()
}

// setAgentRegistry installs the registry a run was configured with.
func setAgentRegistry(registry *agents.Registry) { registeredAgents.Store(registry) }

// agentDefinition resolves one agent name against the registry in force.
func agentDefinition(name string) (agents.Definition, bool) { return agentRegistry().Lookup(name) }

// agentAssignsSessionID reports whether aviary generates the session ID for an
// agent spec and hands it over, rather than reading one back from the agent's
// own output. An agent the registry does not know keeps the historical
// behaviour of being given a generated ID.
func agentAssignsSessionID(value string) bool {
	definition, ok := agentDefinition(agentProvider(value))
	if !ok {
		return true
	}
	return definition.AssignsSessionID()
}

// unknownAgentError reports an agent name the registry does not define,
// listing what it does define so a typo is obvious.
func unknownAgentError(registry *agents.Registry, name string) error {
	return fmt.Errorf("unknown agent %q; known agents are %s", name, strings.Join(registry.Names(), ", "))
}

// definitionEnv renders a definition's environment as KEY=VALUE pairs, in a
// stable order so a run's child environment does not depend on map iteration.
func definitionEnv(definition agents.Definition) []string {
	if len(definition.Env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(definition.Env))
	for key := range definition.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+definition.Env[key])
	}
	return env
}
