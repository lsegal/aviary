package clidefs

import (
	"reflect"
	"testing"
)

// TestBuiltinPermissionArgs pins how claude carries an agent's exec and
// filesystem rules into its own allow and deny rules (#560). Only the modes
// that permit work pass the allows; every mode passes the denies, which
// claude enforces even with its permission checks skipped.
func TestBuiltinPermissionArgs(t *testing.T) {
	claude, ok := MustBuiltin().Lookup("claude")
	if !ok {
		t.Fatal("claude missing")
	}
	commands := []Rule{{Pattern: "*"}, {Pattern: "gh repo *"}, {Pattern: "rm *", Deny: true}}
	paths := []PathRule{
		{Rule: Rule{Pattern: `C:\Users\me\.config\aviary\**`}, Dir: `C:\Users\me\.config\aviary`},
		{Rule: Rule{Pattern: `C:\Users\me\.config\aviary\token`, Deny: true}, Dir: `C:\Users\me\.config\aviary`},
	}
	denies := []string{
		"--disallowedTools", "Bash(rm *)", "--disallowedTools", "PowerShell(rm *)",
		"--disallowedTools", "Read(//c/Users/me/.config/aviary/token)", "--disallowedTools", "Edit(//c/Users/me/.config/aviary/token)",
	}
	auto := []string{
		"--allowedTools", "Bash", "--allowedTools", "PowerShell",
		"--allowedTools", "Bash(gh repo *)", "--allowedTools", "PowerShell(gh repo *)",
		"--disallowedTools", "Bash(rm *)", "--disallowedTools", "PowerShell(rm *)",
		"--add-dir", `C:\Users\me\.config\aviary`, "--allowedTools", "Edit(//c/Users/me/.config/aviary/**)",
		"--disallowedTools", "Read(//c/Users/me/.config/aviary/token)", "--disallowedTools", "Edit(//c/Users/me/.config/aviary/token)",
	}
	for mode, want := range map[ApprovalMode][]string{
		ApprovalReadOnly: denies,
		ApprovalAuto:     auto,
		ApprovalFull:     denies,
		"":               auto,
	} {
		if got := claude.PermissionArgs(mode, commands, paths); !reflect.DeepEqual(got, want) {
			t.Errorf("%q PermissionArgs =\n%q\nwant\n%q", mode, got, want)
		}
	}
	if got := claude.PermissionArgs(ApprovalAuto, nil, nil); got != nil {
		t.Errorf("no rules passed %q", got)
	}
	codex, _ := MustBuiltin().Lookup("codex")
	if got := codex.PermissionArgs(ApprovalAuto, commands, paths); got != nil {
		t.Errorf("codex declares no permissions but passed %q", got)
	}
}

// TestUserDefinitionPermissions checks a cli_definitions entry's permissions
// must name a known mode and use only the placeholders of each rule kind.
func TestUserDefinitionPermissions(t *testing.T) {
	define := func(permissions map[string]any) error {
		_, err := FromConfig(map[string]any{"mine": map[string]any{
			"binary":      "mine",
			"session":     map[string]any{"assign": "none"},
			"output":      map[string]any{"format": "text"},
			"permissions": permissions,
			"args":        map[string]any{"run": []any{map[string]any{"args": []any{"{prompt}"}}}},
		}})
		return err
	}
	if err := define(map[string]any{"auto": map[string]any{
		"allowCommand": []any{"--allow", "shell{commandSpec}"},
		"allowPath":    []any{"--dir", "{dir}", "--write", "{pathPosix}"},
	}}); err != nil {
		t.Fatalf("valid permissions rejected: %v", err)
	}
	for _, bad := range []map[string]any{
		{"auto": map[string]any{"allowCommand": []any{"--allow", "{path}"}}},
		{"auto": map[string]any{"denyPath": []any{"--deny", "{commandSpec}"}}},
		{"yolo": map[string]any{"allowCommand": []any{"--allow"}}},
	} {
		if err := define(bad); err == nil {
			t.Errorf("permissions %v was accepted", bad)
		}
	}
}
