package commandpolicy

import (
	"errors"
	"regexp/syntax"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

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

func TestPolicy_DecideGlobSyntax(t *testing.T) {
	tests := []struct {
		name    string
		rule    string
		command string
		allowed bool
	}{
		{"question mark matches one char", "ls -?", "ls -a", true},
		{"question mark needs a char", "ls -?", "ls -", false},
		{"question mark matches only one", "ls -?", "ls -al", false},
		{"class matches member", "git [abc]dd", "git add", true},
		{"class rejects non-member", "git [abc]dd", "git odd", false},
		{"class range", "make [a-c]", "make b", true},
		{"unterminated bracket is literal", "echo [abc", "echo [abc", true},
		{"unterminated bracket is not a class", "echo [abc", "echo a", false},
		{"dot is literal", "cat a.txt", "cat a.txt", true},
		{"dot does not match any char", "cat a.txt", "cat abtxt", false},
		{"plus is literal", "g++ main.cc", "g++ main.cc", true},
		{"plus does not repeat", "g++ main.cc", "g main.cc", false},
		{"parens and pipe are literal", "echo (a|b)", "echo (a|b)", true},
		{"pipe does not alternate", "echo (a|b)", "echo a", false},
		{"braces are literal", "echo {1}", "echo {1}", true},
		{"braces do not repeat", "x{2}", "xx", false},
		{"caret and dollar are literal", "echo ^$HOME", "echo ^$HOME", true},
		{"backslash is literal", `dir C:\tmp`, `dir C:\tmp`, true},
		{"backslash does not escape", `echo \d`, "echo 1", false},
		{"star spans spaces", "go test *", "go test ./... -race", true},
		{"match is anchored at start", "test", "go test", false},
		{"match is anchored at end", "go", "go test", false},
		{"command whitespace is trimmed", "ls", "  ls  ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New([]string{tt.rule})
			if err != nil {
				t.Fatalf("New(%q) error = %v", tt.rule, err)
			}
			allowed, rule := p.Decide(tt.command)
			if allowed != tt.allowed {
				t.Fatalf("Decide(%q) with rule %q allowed = %v, want %v", tt.command, tt.rule, allowed, tt.allowed)
			}
			wantRule := ""
			if tt.allowed {
				wantRule = tt.rule
			}
			if rule != wantRule {
				t.Fatalf("Decide(%q) rule = %q, want %q", tt.command, rule, wantRule)
			}
		})
	}
}

func TestPolicy_LaterDenyOverridesAllow(t *testing.T) {
	p, err := New([]string{"git *", "  !  git push *  "})
	if err != nil {
		t.Fatal(err)
	}
	if allowed, rule := p.Decide("git push origin main"); allowed || rule != "!  git push *" {
		t.Fatalf("Decide(git push) = %v, %q; want false, %q", allowed, rule, "!  git push *")
	}
	if allowed, rule := p.Decide("git status"); !allowed || rule != "git *" {
		t.Fatalf("Decide(git status) = %v, %q; want true, %q", allowed, rule, "git *")
	}
}

func TestNew_SkipsBlankRules(t *testing.T) {
	p, err := New([]string{"", "   ", "\t\n", "ls"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if len(p.rules) != 1 || p.rules[0].raw != "ls" {
		t.Fatalf("rules = %+v, want only ls", p.rules)
	}
	if !p.Allows("ls") {
		t.Fatal("expected ls to be allowed")
	}
	if p.Allows("") {
		t.Fatal("expected empty command to be denied")
	}
}

func TestNew_EmptyPolicyDeniesEverything(t *testing.T) {
	for _, patterns := range [][]string{nil, {}, {" "}} {
		p, err := New(patterns)
		if err != nil {
			t.Fatalf("New(%q) error = %v", patterns, err)
		}
		if allowed, rule := p.Decide("ls"); allowed || rule != "" {
			t.Fatalf("New(%q).Decide(ls) = %v, %q; want false, empty", patterns, allowed, rule)
		}
	}
}

func TestNew_BareNegationIsAnError(t *testing.T) {
	for _, raw := range []string{"!", " ! ", "!\t"} {
		p, err := New([]string{"ls", raw})
		if err == nil {
			t.Fatalf("New(%q) error = nil, want error", raw)
		}
		if p != nil {
			t.Fatalf("New(%q) policy = %v, want nil", raw, p)
		}
		if !strings.Contains(err.Error(), "invalid command allowlist rule") || !strings.Contains(err.Error(), strconv.Quote(raw)) {
			t.Fatalf("New(%q) error = %q, want invalid rule naming %q", raw, err, raw)
		}
	}
}

func TestNew_CompileErrorIsWrapped(t *testing.T) {
	p, err := New([]string{"*", "make [z-a]"})
	if err == nil {
		t.Fatal("New() error = nil, want compile error")
	}
	if p != nil {
		t.Fatalf("New() policy = %v, want nil", p)
	}
	if !strings.Contains(err.Error(), `compiling command allowlist rule "make [z-a]"`) {
		t.Fatalf("error = %q, want rule context", err)
	}
	var syntaxErr *syntax.Error
	if !errors.As(err, &syntaxErr) {
		t.Fatalf("error = %v, want wrapped *syntax.Error", err)
	}
}

func TestPolicy_NilDeniesEverything(t *testing.T) {
	var p *Policy
	if allowed, rule := p.Decide("ls"); allowed || rule != "" {
		t.Fatalf("nil Decide = %v, %q; want false, empty", allowed, rule)
	}
	if p.Allows("ls") {
		t.Fatal("nil Allows = true, want false")
	}
}

func TestFoldCase_DefaultsToWindows(t *testing.T) {
	if want := runtime.GOOS == "windows"; foldCase != want {
		t.Fatalf("foldCase = %v on %s, want %v", foldCase, runtime.GOOS, want)
	}
}

func TestPolicy_CaseFolding(t *testing.T) {
	tests := []struct {
		fold    bool
		command string
		allowed bool
	}{
		{true, "git status", true},
		{true, "GIT STATUS", true},
		{true, "git push", false},
		{true, "Git Push --Force", false},
		{false, "Git Status", true},
		{false, "git status", false},
		{false, "GIT PUSH", false},
	}
	for _, tt := range tests {
		setFoldCase(t, tt.fold)
		p, err := New([]string{"Git *", "!GIT PUSH*"})
		if err != nil {
			t.Fatal(err)
		}
		if got := p.Allows(tt.command); got != tt.allowed {
			t.Errorf("foldCase=%v Allows(%q) = %v, want %v", tt.fold, tt.command, got, tt.allowed)
		}
	}
}

func setFoldCase(t *testing.T, fold bool) {
	t.Helper()
	prev := foldCase
	foldCase = fold
	t.Cleanup(func() { foldCase = prev })
}
