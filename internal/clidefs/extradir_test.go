package clidefs

import (
	"reflect"
	"testing"
)

// TestBuiltinExtraDirArgs pins how claude and codex add the agent's own
// directory to a session's workspace scope (#534).
func TestBuiltinExtraDirArgs(t *testing.T) {
	registry := MustBuiltin()
	for name, want := range map[string][]string{
		"claude": {"--add-dir", `C:\Users\me\.config\aviary\agents\Aviary`},
		"codex":  {"-c", `sandbox_workspace_write.writable_roots=["C:\\Users\\me\\.config\\aviary\\agents\\Aviary"]`},
	} {
		definition, ok := registry.Lookup(name)
		if !ok {
			t.Fatalf("Lookup(%q) missing", name)
		}
		if got := definition.ExtraDirArgs(`C:\Users\me\.config\aviary\agents\Aviary`); !reflect.DeepEqual(got, want) {
			t.Errorf("%s ExtraDirArgs = %q, want %q", name, got, want)
		}
		if got := definition.ExtraDirArgs(""); got != nil {
			t.Errorf("%s ExtraDirArgs(\"\") = %q, want nothing", name, got)
		}
	}
	if got := (Definition{}).ExtraDirArgs("/a"); got != nil {
		t.Errorf("a definition without extraDir granted %q", got)
	}
}

// TestUserDefinitionExtraDir checks a cli_definitions entry's extraDir must
// name the directory and may use no other placeholder.
func TestUserDefinitionExtraDir(t *testing.T) {
	define := func(extraDir ...any) error {
		_, err := FromConfig(map[string]any{"mine": map[string]any{
			"binary":   "mine",
			"session":  map[string]any{"assign": "none"},
			"output":   map[string]any{"format": "text"},
			"extraDir": extraDir,
			"args":     map[string]any{"run": []any{map[string]any{"args": []any{"{prompt}"}}}},
		}})
		return err
	}
	if err := define("--include", "{dir}"); err != nil {
		t.Fatalf("valid extraDir rejected: %v", err)
	}
	if err := define("--include"); err == nil {
		t.Error("extraDir without {dir} was accepted")
	}
	if err := define("--include", "{prompt}"); err == nil {
		t.Error("extraDir with {prompt} was accepted")
	}
}
