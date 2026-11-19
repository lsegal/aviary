package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
)

// DefaultConfigPath is the agent-definition config file glorp reads when
// --config is not given. It is deliberately not .glorp.json: that file is the
// work state the daemon rewrites as issues are handled, and a hand-edited
// agent definition living there would be clobbered by the next state save.
// glorp only ever reads this file.
const DefaultConfigPath = ".glorp.config.json"

// SettingsSection is the other section that file takes: default values for
// glorp's own switches (issue #614). The agent loader does not read it, but
// it has to know the name so a file carrying it is not rejected as unknown.
const SettingsSection = "settings"

// config is the shape of that file. The top level is an object so later
// configuration sections can be added beside the agents rather than the file
// being a bare array of definitions.
type config struct {
	Agents json.RawMessage `json:"agents"`
}

// stateKeyPattern matches the keys a work-state file uses -- a bare issue
// number, or an owner/repo#number -- so a state file handed to --config is
// reported as the mix-up it is instead of as a pile of unknown fields.
var stateKeyPattern = regexp.MustCompile(`^(\d+|[^#]+#\d+)$`)

// Load builds the registry a run dispatches with: the built-in definitions,
// overridden and extended by path when it exists. A missing file is not an
// error; glorp runs on the built-ins alone. Anything else wrong with the file
// is fatal and named: a definition silently dropped for being malformed is
// indistinguishable, at the --agent prompt, from a typo.
func Load(path string) (*Registry, error) {
	registry, err := Builtin()
	if err != nil {
		return nil, err
	}
	if path == "" {
		return registry, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return registry, nil
		}
		return nil, fmt.Errorf("read agent config %s: %w", path, err)
	}
	overrides, err := parseConfig(path, raw)
	if err != nil {
		return nil, err
	}
	for _, override := range overrides {
		merged, err := merge(registry, override)
		if err != nil {
			return nil, fmt.Errorf("agent config %s: agent %q: %w", path, override.name, err)
		}
		if err := merged.Validate(); err != nil {
			return nil, fmt.Errorf("agent config %s: agent %q: %w", path, override.name, err)
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

func parseConfig(path string, raw []byte) ([]override, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("agent config %s: %w", path, err)
	}
	for key := range top {
		if stateKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("agent config %s: %q looks like a work-state record; work state belongs in the --state file (%s) and agent definitions in this file", path, key, "default .glorp.json")
		}
		if key != "agents" && key != SettingsSection {
			return nil, fmt.Errorf("agent config %s: unknown section %q; the sections defined so far are \"agents\" and %q", path, key, SettingsSection)
		}
	}
	var parsed config
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("agent config %s: %w", path, err)
	}
	if len(parsed.Agents) == 0 {
		return nil, nil
	}
	return parseAgents(path, parsed.Agents)
}

// parseAgents accepts either shape the "agents" section may take: an array of
// definitions, each naming itself, or an object keyed by agent name.
func parseAgents(path string, raw json.RawMessage) ([]override, error) {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		var documents []rawDefinition
		if err := json.Unmarshal(raw, &documents); err != nil {
			return nil, fmt.Errorf("agent config %s: section \"agents\": %w", path, err)
		}
		overrides := make([]override, 0, len(documents))
		for i, fields := range documents {
			name, err := documentName(fields)
			if err != nil {
				return nil, fmt.Errorf("agent config %s: agents[%d]: %w", path, i, err)
			}
			overrides = append(overrides, override{name: name, fields: fields})
		}
		return overrides, nil
	}
	var keyed map[string]rawDefinition
	if err := json.Unmarshal(raw, &keyed); err != nil {
		return nil, fmt.Errorf("agent config %s: section \"agents\" must be an array of definitions or an object keyed by agent name: %w", path, err)
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
			return nil, fmt.Errorf("agent config %s: agent %q declares the name %q; the key and the name have to agree", path, name, declared)
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
