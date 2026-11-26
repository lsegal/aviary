package clidefs

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// registryTestDefinition is the smallest definition Validate accepts: an
// in-process agent needs no argv templates, output, or session capture.
func registryTestDefinition(name, binary, skillsTarget string) Definition {
	return Definition{
		Name:      name,
		Binary:    binary,
		InProcess: true,
		Session:   Session{Assign: AssignNone},
		Skills:    Skills{Target: skillsTarget},
	}
}

func TestNewRegistryRegistersValidDefinitions(t *testing.T) {
	registry, err := NewRegistry(
		registryTestDefinition("zeta", "zeta-bin", ""),
		registryTestDefinition("alpha", "alpha-bin", ""),
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if got, want := registry.Names(), []string{"alpha", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
	if got, want := registry.order, []string{"zeta", "alpha"}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want insertion order %v", got, want)
	}
	definition, ok := registry.Lookup("alpha")
	if !ok || definition.Binary != "alpha-bin" {
		t.Errorf("Lookup(alpha) = %+v, %v; want binary alpha-bin", definition, ok)
	}
	if _, ok := registry.Lookup("missing"); ok {
		t.Error("Lookup(missing) found a definition")
	}
}

func TestNewRegistryEmpty(t *testing.T) {
	registry, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if names := registry.Names(); len(names) != 0 {
		t.Errorf("Names() = %v, want none", names)
	}
	if targets := registry.SkillsTargets(); len(targets) != 0 {
		t.Errorf("SkillsTargets() = %v, want none", targets)
	}
}

func TestNewRegistryRejectsInvalidDefinitionNamingAgent(t *testing.T) {
	invalid := registryTestDefinition("broken", "", "")
	registry, err := NewRegistry(registryTestDefinition("fine", "fine-bin", ""), invalid)
	if err == nil {
		t.Fatalf("NewRegistry() = %v, want error", registry)
	}
	if registry != nil {
		t.Errorf("NewRegistry() registry = %v, want nil on error", registry)
	}
	message := err.Error()
	if !strings.Contains(message, `agent "broken"`) || !strings.Contains(message, `field "binary" is required`) {
		t.Errorf("NewRegistry() error = %q, want it to name the agent and the field", message)
	}
}

func TestNewRegistryDuplicateKeepsFirstPositionAndLastDefinition(t *testing.T) {
	registry, err := NewRegistry(
		registryTestDefinition("dup", "first-bin", ""),
		registryTestDefinition("other", "other-bin", ""),
		registryTestDefinition("dup", "second-bin", ""),
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if got, want := registry.order, []string{"dup", "other"}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
	if got, want := registry.Names(), []string{"dup", "other"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
	if definition, _ := registry.Lookup("dup"); definition.Binary != "second-bin" {
		t.Errorf("Lookup(dup).Binary = %q, want the later definition's second-bin", definition.Binary)
	}
}

func TestNilRegistryAccessors(t *testing.T) {
	var registry *Registry
	if definition, ok := registry.Lookup("claude"); ok || !reflect.DeepEqual(definition, Definition{}) {
		t.Errorf("nil Lookup() = %+v, %v; want zero, false", definition, ok)
	}
	if names := registry.Names(); names != nil {
		t.Errorf("nil Names() = %v, want nil", names)
	}
	if targets := registry.SkillsTargets(); targets != nil {
		t.Errorf("nil SkillsTargets() = %v, want nil", targets)
	}
}

func TestSkillsTargetsDeduplicatesSortsAndSkipsEmpty(t *testing.T) {
	registry, err := NewRegistry(
		registryTestDefinition("a", "a-bin", "universal"),
		registryTestDefinition("b", "b-bin", "codex"),
		registryTestDefinition("c", "c-bin", ""),
		registryTestDefinition("d", "d-bin", "universal"),
		registryTestDefinition("e", "e-bin", "claude-code"),
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if got, want := registry.SkillsTargets(), []string{"claude-code", "codex", "universal"}; !reflect.DeepEqual(got, want) {
		t.Errorf("SkillsTargets() = %v, want %v", got, want)
	}
}

func TestBuiltinSkillsTargetsCoverEveryBuiltinTarget(t *testing.T) {
	registry := MustBuiltin()
	targets := registry.SkillsTargets()
	seen := map[string]bool{}
	for _, target := range targets {
		if target == "" {
			t.Error("SkillsTargets() contains an empty target")
		}
		if seen[target] {
			t.Errorf("SkillsTargets() repeats %q", target)
		}
		seen[target] = true
	}
	for _, name := range registry.Names() {
		definition, _ := registry.Lookup(name)
		if target := definition.SkillsTarget(); target != "" && !seen[target] {
			t.Errorf("SkillsTargets() = %v, missing %s's target %q", targets, name, target)
		}
	}
}

func TestLoadBuiltinReadsDocumentsInNameOrder(t *testing.T) {
	fsys := fstest.MapFS{
		"builtin/b.json": {Data: []byte(`{"name":"bravo","binary":"bravo-bin","inProcess":true,"session":{"assign":"none"}}`)},
		"builtin/a.json": {Data: []byte(`{"name":"alpha","binary":"alpha-bin","inProcess":true,"session":{"assign":"none"}}`)},
	}
	registry, err := loadBuiltin(fsys)
	if err != nil {
		t.Fatalf("loadBuiltin() error = %v", err)
	}
	if got, want := registry.order, []string{"alpha", "bravo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want file-name order %v", got, want)
	}
}

func TestLoadBuiltinErrors(t *testing.T) {
	tests := []struct {
		name string
		fsys fstest.MapFS
		want []string
	}{
		{
			name: "missing directory",
			fsys: fstest.MapFS{},
			want: []string{"read built-in agent definitions"},
		},
		{
			name: "unreadable entry",
			fsys: fstest.MapFS{"builtin/dir.json/inner": {Data: []byte("{}")}},
			want: []string{"read built-in agent definition dir.json"},
		},
		{
			name: "unknown field",
			fsys: fstest.MapFS{"builtin/typo.json": {Data: []byte(`{"name":"typo","flavor":"x"}`)}},
			want: []string{"built-in agent definition typo.json", `unknown field "flavor"`},
		},
		{
			name: "invalid definition",
			fsys: fstest.MapFS{"builtin/nobinary.json": {Data: []byte(`{"name":"nobinary"}`)}},
			want: []string{"built-in agent definition nobinary.json", `field "binary" is required`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, err := loadBuiltin(tt.fsys)
			if err == nil {
				t.Fatalf("loadBuiltin() = %v, want error", registry)
			}
			if registry != nil {
				t.Errorf("loadBuiltin() registry = %v, want nil on error", registry)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("loadBuiltin() error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

func TestMustBuiltinMatchesBuiltin(t *testing.T) {
	registry, err := Builtin()
	if err != nil {
		t.Fatalf("Builtin() error = %v", err)
	}
	if got, want := MustBuiltin().Names(), registry.Names(); !reflect.DeepEqual(got, want) {
		t.Errorf("MustBuiltin().Names() = %v, want %v", got, want)
	}
}

func TestDescribeJSONError(t *testing.T) {
	t.Run("unknown field is rewritten", func(t *testing.T) {
		_, err := decodeDefinition([]byte(`{"flavor":"x"}`))
		if err == nil || err.Error() != `unknown field "flavor"` {
			t.Errorf("decodeDefinition() error = %v, want unknown field \"flavor\"", err)
		}
	})
	t.Run("type mismatch passes through", func(t *testing.T) {
		_, err := decodeDefinition([]byte(`{"name":42}`))
		var typeErr *json.UnmarshalTypeError
		if !errors.As(err, &typeErr) {
			t.Fatalf("decodeDefinition() error = %v (%T), want *json.UnmarshalTypeError", err, err)
		}
		if typeErr.Field != "name" {
			t.Errorf("UnmarshalTypeError.Field = %q, want name", typeErr.Field)
		}
	})
	t.Run("other errors pass through unchanged", func(t *testing.T) {
		original := errors.New("unexpected EOF")
		if got := describeJSONError(original); got != original {
			t.Errorf("describeJSONError() = %v, want the original error", got)
		}
		_, err := decodeDefinition([]byte(`{`))
		if err == nil || strings.HasPrefix(err.Error(), "unknown field") {
			t.Errorf("decodeDefinition() error = %v, want a syntax error", err)
		}
	})
}
