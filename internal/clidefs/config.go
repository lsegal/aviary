package clidefs

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Load builds the registry a run dispatches with: the built-in definitions,
// overridden and extended by definitions, keyed by agent name, as the
// models.cli_definitions section of aviary.yaml holds them. Anything wrong
// with a definition is fatal and named: a definition silently dropped for
// being malformed is indistinguishable, at dispatch, from a typo.
func Load(definitions map[string]any) (*Registry, error) {
	registry, err := Builtin()
	if err != nil {
		return nil, err
	}
	if len(definitions) == 0 {
		return registry, nil
	}
	raw, err := json.Marshal(definitions)
	if err != nil {
		return nil, fmt.Errorf("cli_definitions: %w", err)
	}
	overrides, err := parseAgents(raw)
	if err != nil {
		return nil, err
	}
	for _, override := range overrides {
		merged, err := merge(registry, override)
		if err != nil {
			return nil, fmt.Errorf("cli_definitions: agent %q: %w", override.name, err)
		}
		if err := merged.Validate(); err != nil {
			return nil, fmt.Errorf("cli_definitions: agent %q: %w", override.name, err)
		}
		registry.set(merged)
	}
	return registry, nil
}

// override is one definition document read from the config file, kept as its
// raw fields so merging can tell "absent" from "set to the zero value".
type override struct {
	name   string
	fields rawDefinition
}

// parseAgents reads definitions keyed by agent name.
func parseAgents(raw json.RawMessage) ([]override, error) {
	var keyed map[string]rawDefinition
	if err := json.Unmarshal(raw, &keyed); err != nil {
		return nil, fmt.Errorf("cli_definitions must map agent names to definitions: %w", err)
	}
	names := make([]string, 0, len(keyed))
	for name := range keyed {
		names = append(names, name)
	}
	sort.Strings(names)
	overrides := make([]override, 0, len(names))
	for _, name := range names {
		fields := keyed[name]
		if fields == nil {
			fields = rawDefinition{}
		}
		if declared, err := documentName(fields); err == nil && declared != name {
			return nil, fmt.Errorf("cli_definitions: agent %q declares the name %q; the key and the name have to agree", name, declared)
		}
		encoded, err := json.Marshal(name)
		if err != nil {
			return nil, err
		}
		fields["name"] = encoded
		overrides = append(overrides, override{name: name, fields: fields})
	}
	return overrides, nil
}

func documentName(fields rawDefinition) (string, error) {
	raw, ok := fields["name"]
	if !ok {
		return "", fmt.Errorf(`field "name" is required`)
	}
	var name string
	if err := json.Unmarshal(raw, &name); err != nil {
		return "", fmt.Errorf(`field "name": %w`, err)
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf(`field "name" is required`)
	}
	return name, nil
}

// merge applies one override to the registry. A definition whose name matches
// a built-in overrides it field by field: fields the document does not mention
// keep the built-in's value, and a field given as null is reset to the schema's
// own default rather than inheriting. A name the registry does not know
// registers a new agent.
func merge(registry *Registry, o override) (Definition, error) {
	base := rawDefinition{}
	if existing, ok := registry.Lookup(o.name); ok {
		encoded, err := json.Marshal(existing)
		if err != nil {
			return Definition{}, err
		}
		if err := json.Unmarshal(encoded, &base); err != nil {
			return Definition{}, err
		}
	}
	for field, value := range o.fields {
		if isJSONNull(value) {
			delete(base, field)
			continue
		}
		base[field] = value
	}
	encoded, err := json.Marshal(base)
	if err != nil {
		return Definition{}, err
	}
	return decodeDefinition(encoded)
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}
