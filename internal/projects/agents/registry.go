package agents

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

//go:embed builtin/*.json
var builtinFS embed.FS

// Registry is the set of agents a run knows how to dispatch to. It is built
// from the embedded built-in definitions and then, optionally, from the user's
// own config file.
type Registry struct {
	definitions map[string]Definition
	order       []string
}

// Builtin returns a registry holding only the definitions shipped with glorp.
// The embedded documents are validated at load, so a built-in that no longer
// parses is a build-time mistake surfaced on the first call rather than a
// silently missing agent.
func Builtin() (*Registry, error) {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		return nil, fmt.Errorf("read built-in agent definitions: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	registry := &Registry{definitions: make(map[string]Definition, len(names))}
	for _, name := range names {
		raw, err := builtinFS.ReadFile(path.Join("builtin", name))
		if err != nil {
			return nil, fmt.Errorf("read built-in agent definition %s: %w", name, err)
		}
		definition, err := decodeDefinition(raw)
		if err != nil {
			return nil, fmt.Errorf("built-in agent definition %s: %w", name, err)
		}
		if err := definition.Validate(); err != nil {
			return nil, fmt.Errorf("built-in agent definition %s: %w", name, err)
		}
		registry.set(definition)
	}
	return registry, nil
}

// MustBuiltin is Builtin for the callers that have nowhere to report a broken
// embedded document, which can only be a bug in glorp itself.
func MustBuiltin() *Registry {
	registry, err := Builtin()
	if err != nil {
		panic(err)
	}
	return registry
}

// NewRegistry builds a registry from explicit definitions, for tests and for
// callers assembling their own set.
func NewRegistry(definitions ...Definition) (*Registry, error) {
	registry := &Registry{definitions: make(map[string]Definition, len(definitions))}
	for _, definition := range definitions {
		if err := definition.Validate(); err != nil {
			return nil, fmt.Errorf("agent %q: %w", definition.Name, err)
		}
		registry.set(definition)
	}
	return registry, nil
}

func (r *Registry) set(definition Definition) {
	if _, ok := r.definitions[definition.Name]; !ok {
		r.order = append(r.order, definition.Name)
	}
	r.definitions[definition.Name] = definition
}

// Lookup returns the definition for an agent name.
func (r *Registry) Lookup(name string) (Definition, bool) {
	if r == nil {
		return Definition{}, false
	}
	definition, ok := r.definitions[name]
	return definition, ok
}

// Names lists the registered agents in sorted order, for validation messages
// and for the UI surfaces that show what a run can dispatch to.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.definitions))
	for name := range r.definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// SkillsTargets lists the skills.sh target ids of the registered agents,
// deduplicated and sorted. It is what the installers install glorp's skills
// for, so an agent added to the registry is covered without either installer
// script being edited. Agents whose definition names no target are skipped.
func (r *Registry) SkillsTargets() []string {
	if r == nil {
		return nil
	}
	seen := make(map[string]bool, len(r.definitions))
	targets := make([]string, 0, len(r.definitions))
	for _, definition := range r.definitions {
		target := definition.SkillsTarget()
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		targets = append(targets, target)
	}
	sort.Strings(targets)
	return targets
}

// Raw returns the definition documents as they were loaded, keyed by name, so
// a merge can be applied field by field rather than wholesale.
type rawDefinition map[string]json.RawMessage

// decodeDefinition unmarshals one definition document, rejecting fields the
// schema does not define so a typo is reported rather than dropped.
func decodeDefinition(raw []byte) (Definition, error) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var definition Definition
	if err := decoder.Decode(&definition); err != nil {
		return Definition{}, describeJSONError(err)
	}
	return definition, nil
}

// describeJSONError rewrites encoding/json's messages into the form the rest
// of the loader reports, which always names the offending field.
func describeJSONError(err error) error {
	message := err.Error()
	if field, ok := strings.CutPrefix(message, "json: unknown field "); ok {
		return fmt.Errorf("unknown field %s", field)
	}
	return err
}
