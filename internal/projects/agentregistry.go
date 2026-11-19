package projects

import (
	"encoding/json"
	"fmt"
	"os"
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

// configPathFromArgs finds the --config value in a raw argument list, before
// the flag set is parsed. Agent definitions have to be loaded before --agent is
// validated against them, and the flag package hands values over in the order
// they were written, which would otherwise make `--agent mine --config x.json`
// fail on an agent the config file defines.
func configPathFromArgs(args []string, fallback string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		name, value, split := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !strings.HasPrefix(arg, "-") || name != "config" {
			continue
		}
		if split {
			return value
		}
		if i+1 < len(args) {
			return args[i+1]
		}
	}
	return fallback
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

// guardWorkStateFile rejects a --state file that holds agent definitions. The
// two files are deliberately separate -- aviary rewrites the state file and only
// ever reads the config file -- so definitions written into the state file
// would be silently destroyed by the next save. Say which file they belong in
// rather than losing them.
func guardWorkStateFile(path string) error {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		// A missing or unreadable state file is not this check's business;
		// the run reports it where the state is actually loaded.
		return nil
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil
	}
	if _, ok := top["agents"]; ok {
		return fmt.Errorf("state file %s contains an %q section: agent definitions belong in %s (or the file named by --config), which aviary only ever reads, while --state is rewritten as issues are handled", path, "agents", agents.DefaultConfigPath)
	}
	return nil
}
