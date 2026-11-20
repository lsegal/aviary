package clidefs

import (
	"reflect"
	"strings"
	"testing"
)

// TestBuiltinApprovalTranslations pins each built-in CLI's translation of
// aviary's approval modes into its native permission flags (issue #513).
func TestBuiltinApprovalTranslations(t *testing.T) {
	registry := MustBuiltin()
	want := map[string]map[ApprovalMode][]string{
		"claude": {
			ApprovalReadOnly: {"--permission-mode", "dontAsk"},
			ApprovalAuto:     {"--permission-mode", "auto"},
			ApprovalFull:     {"--dangerously-skip-permissions"},
		},
		"codex": {
			ApprovalReadOnly: {"-c", "sandbox_mode=read-only"},
			ApprovalAuto:     {"-c", "sandbox_mode=workspace-write", "-c", "sandbox_workspace_write.network_access=true"},
			ApprovalFull:     {"--dangerously-bypass-approvals-and-sandbox"},
		},
		"gemini": {
			ApprovalReadOnly: {"--approval-mode", "plan"},
			ApprovalAuto:     {"--approval-mode", "auto_edit"},
			ApprovalFull:     {"--yolo"},
		},
		"muse": {
			ApprovalReadOnly: {"--approval-mode", "untrusted", "--disable-write"},
			ApprovalAuto:     {"--approval-mode", "never"},
			ApprovalFull:     {"--yolo"},
		},
		"agy": {
			ApprovalReadOnly: {"--mode", "plan"},
			ApprovalAuto:     {"--mode", "auto"},
			ApprovalFull:     {"--dangerously-skip-permissions"},
		},
		// Cline and opencode have no sandbox, so full adds nothing to auto.
		"cline": {
			ApprovalReadOnly: {"--plan", "--auto-approve", "true"},
			ApprovalAuto:     {"--auto-approve", "true"},
			ApprovalFull:     {"--auto-approve", "true"},
		},
		"opencode": {
			ApprovalReadOnly: {"--agent", "plan"},
			ApprovalAuto:     {"--auto"},
			ApprovalFull:     {"--auto"},
		},
	}
	for name, modes := range want {
		definition, ok := registry.Lookup(name)
		if !ok {
			t.Fatalf("Lookup(%q) missing", name)
		}
		for mode, args := range modes {
			if got := definition.ApprovalArgs(mode); !reflect.DeepEqual(got, args) {
				t.Errorf("%s %s = %q, want %q", name, mode, got, args)
			}
		}
		// Every template that runs the agent renders the translation.
		for _, mode := range []Mode{ModeRun, ModeResume, ModeVision} {
			if !definition.Supports(mode) {
				continue
			}
			argv := strings.Join(definition.Render(mode, Values{Prompt: "p", Session: "s", Image: "i", Approval: ApprovalReadOnly}), " ")
			if !strings.Contains(argv, strings.Join(modes[ApprovalReadOnly], " ")) {
				t.Errorf("%s %s argv %q does not render its read-only flags", name, mode, argv)
			}
		}
	}
}

// TestApprovalArgsFallBackToLessPermissive checks a definition missing a mode
// takes the closest less permissive one it declares, never a more permissive
// one.
func TestApprovalArgsFallBackToLessPermissive(t *testing.T) {
	d := Definition{Approval: map[ApprovalMode][]string{ApprovalReadOnly: {"--ro"}, ApprovalFull: {"--all"}}}
	for mode, want := range map[ApprovalMode][]string{
		ApprovalReadOnly: {"--ro"},
		ApprovalAuto:     {"--ro"},
		ApprovalFull:     {"--all"},
		"":               {"--ro"},
	} {
		if got := d.ApprovalArgs(mode); !reflect.DeepEqual(got, want) {
			t.Errorf("ApprovalArgs(%q) = %q, want %q", mode, got, want)
		}
	}
	if got := (Definition{Approval: map[ApprovalMode][]string{ApprovalFull: {"--all"}}}).ApprovalArgs(ApprovalAuto); got != nil {
		t.Errorf("auto took the full mode's flags: %q", got)
	}
}

// TestUserDefinitionApproval checks a cli_definitions entry declares its own
// translation, that a definition still written against the yolo condition
// renders it for the full mode, and that bad translations are rejected.
func TestUserDefinitionApproval(t *testing.T) {
	registry, err := FromConfig(map[string]any{"mine": map[string]any{
		"binary":   "mine",
		"session":  map[string]any{"assign": "none"},
		"output":   map[string]any{"format": "text"},
		"approval": map[string]any{"read-only": []any{"--safe"}, "auto": []any{"--auto"}},
		"args": map[string]any{"run": []any{
			map[string]any{"args": []any{"{approval}"}},
			map[string]any{"when": "yolo", "args": []any{"--yolo"}},
			map[string]any{"args": []any{"{prompt}"}},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	mine, _ := registry.Lookup("mine")
	for mode, want := range map[ApprovalMode][]string{
		ApprovalReadOnly: {"--safe", "p"},
		ApprovalAuto:     {"--auto", "p"},
		ApprovalFull:     {"--auto", "--yolo", "p"},
	} {
		if got := mine.Render(ModeRun, Values{Prompt: "p", Approval: mode}); !reflect.DeepEqual(got, want) {
			t.Errorf("%s argv = %q, want %q", mode, got, want)
		}
	}

	base := Definition{Name: "x", Binary: "x", Session: Session{Assign: AssignNone}, Output: Output{Format: "text"}, Args: Args{Run: []Fragment{{Args: []string{"{prompt}"}}}}}
	for name, tc := range map[string]struct {
		approval map[ApprovalMode][]string
		run      []Fragment
		want     string
	}{
		"unknown mode":       {approval: map[ApprovalMode][]string{"yolo": {"--y"}}, want: `unknown mode "yolo"`},
		"placeholder":        {approval: map[ApprovalMode][]string{ApprovalAuto: {"{model}"}}, want: "cannot contain a placeholder"},
		"partial {approval}": {run: []Fragment{{Args: []string{"--mode={approval}", "{prompt}"}}}, want: "must be a whole argument"},
	} {
		d := base
		d.Approval = tc.approval
		if tc.run != nil {
			d.Args.Run = tc.run
		}
		if err := d.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Validate() = %v, want %q", name, err, tc.want)
		}
	}
}
