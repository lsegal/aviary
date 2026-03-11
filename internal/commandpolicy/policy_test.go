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
