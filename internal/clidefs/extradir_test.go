package clidefs

import (
	"reflect"
	"testing"
)

// TestBuiltinExtraDirArgs pins how claude and codex add the agent's own
// directory to a session's workspace scope in each approval mode (#534).
// Claude's read-only grant only adds the directory; its edit rule is for the
// modes that permit work, so dontAsk still refuses writes there.
func TestBuiltinExtraDirArgs(t *testing.T) {
	registry := MustBuiltin()
	const dir = `C:\Users\me\.config\aviary\agents\Aviary`
	codex := []string{"-c", `sandbox_workspace_write.writable_roots=["C:\\Users\\me\\.config\\aviary\\agents\\Aviary"]`}
	claudeWork := []string{"--add-dir", dir, "--allowedTools", "Edit(//c/Users/me/.config/aviary/agents/Aviary/**)"}
	want := map[string]map[ApprovalMode][]string{
		"claude": {
			ApprovalReadOnly: {"--add-dir", dir},
			ApprovalAuto:     claudeWork,
			ApprovalFull:     claudeWork,
		},
		"codex": {ApprovalReadOnly: codex, ApprovalAuto: codex, ApprovalFull: codex},
	}
	for name, modes := range want {
		definition, ok := registry.Lookup(name)
		if !ok {
			t.Fatalf("Lookup(%q) missing", name)
		}
		for mode, args := range modes {
			if got := definition.ExtraDirArgs(mode, dir); !reflect.DeepEqual(got, args) {
				t.Errorf("%s %s ExtraDirArgs = %q, want %q", name, mode, got, args)
			}
		}
		if got := definition.ExtraDirArgs(ApprovalAuto, ""); got != nil {
			t.Errorf("%s ExtraDirArgs(\"\") = %q, want nothing", name, got)
		}
	}
	claude, _ := registry.Lookup("claude")
	if got := claude.ExtraDirArgs(ApprovalAuto, "/data/agents/a"); !reflect.DeepEqual(got[3], "Edit(//data/agents/a/**)") {
		t.Errorf("POSIX rule = %q", got[3])
	}
	if got := (Definition{}).ExtraDirArgs(ApprovalAuto, "/a"); got != nil {
		t.Errorf("a definition without extraDir granted %q", got)
	}
}

// TestUserDefinitionExtraDir checks a cli_definitions entry's extraDir must
// name a known mode and the directory, and may use no other placeholder.
func TestUserDefinitionExtraDir(t *testing.T) {
	define := func(extraDir map[string]any) error {
		_, err := FromConfig(map[string]any{"mine": map[string]any{
			"binary":   "mine",
			"session":  map[string]any{"assign": "none"},
			"output":   map[string]any{"format": "text"},
			"extraDir": extraDir,
			"args":     map[string]any{"run": []any{map[string]any{"args": []any{"{prompt}"}}}},
		}})
		return err
	}
	if err := define(map[string]any{"read-only": []any{"--include", "{dir}"}}); err != nil {
		t.Fatalf("valid extraDir rejected: %v", err)
	}
	for _, bad := range []map[string]any{
		{"read-only": []any{"--include"}},
		{"read-only": []any{"--include", "{prompt}"}},
		{"yolo": []any{"--include", "{dir}"}},
	} {
		if err := define(bad); err == nil {
			t.Errorf("extraDir %v was accepted", bad)
		}
	}
}
