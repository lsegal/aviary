package commandpolicy

import "testing"

func TestPolicy_AllowsOrderedRules(t *testing.T) {
	p, err := New([]string{"*", "!rm *", "rm safe"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if !p.Allows("echo hi") {
		t.Fatalf("expected echo hi to be allowed")
	}
	if p.Allows("rm nope") {
		t.Fatalf("expected rm nope to be denied")
	}
	if !p.Allows("rm safe") {
		t.Fatalf("expected rm safe to be re-allowed by later rule")
	}
}

func TestPolicy_DecideNamesTheRule(t *testing.T) {
	policy, err := New([]string{"*", "!rm *", "rm -i *"})
	if err != nil {
		t.Fatal(err)
	}
	for command, want := range map[string]struct {
		allowed bool
		rule    string
	}{
		"ls":       {true, "*"},
		"rm -rf /": {false, "!rm *"},
		"rm -i x":  {true, "rm -i *"},
	} {
		allowed, rule := policy.Decide(command)
		if allowed != want.allowed || rule != want.rule {
			t.Errorf("Decide(%q) = %v, %q; want %v, %q", command, allowed, rule, want.allowed, want.rule)
		}
	}
	empty, _ := New([]string{"git *"})
	if allowed, rule := empty.Decide("ls"); allowed || rule != "" {
		t.Errorf("unmatched Decide = %v, %q", allowed, rule)
	}
}
