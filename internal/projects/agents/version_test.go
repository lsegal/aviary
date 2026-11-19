package agents

import (
	"strings"
	"testing"
)

// TestParseVersion covers the shapes a CLI prints its version in, since the
// number has to be read out of whatever banner surrounds it.
func TestParseVersion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		text   string
		want   []int
		wantOK bool
	}{
		{name: "bare", text: "0.58.0", want: []int{0, 58, 0}, wantOK: true},
		{name: "named", text: "gemini 0.58.0", want: []int{0, 58, 0}, wantOK: true},
		{name: "banner", text: "codex-cli 0.5.0 (rust)\n", want: []int{0, 5, 0}, wantOK: true},
		{name: "two components", text: "1.2", want: []int{1, 2}, wantOK: true},
		{name: "prerelease", text: "2.0.0-beta.3", want: []int{2, 0, 0}, wantOK: true},
		{name: "no version", text: "unknown", wantOK: false},
		{name: "empty", text: "", wantOK: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseVersion(tc.text)
			if ok != tc.wantOK {
				t.Fatalf("ParseVersion(%q) ok = %v, want %v", tc.text, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("ParseVersion(%q) = %v, want %v", tc.text, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("ParseVersion(%q) = %v, want %v", tc.text, got, tc.want)
				}
			}
		})
	}
}

// TestCompareVersions pins the ordering the minimum-version check rests on,
// including the component-wise comparison that keeps 0.9.0 below 0.58.0 from
// being read the way a string comparison would.
func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b   string
		want   int
		wantOK bool
	}{
		{a: "0.58.0", b: "0.58.0", want: 0, wantOK: true},
		{a: "0.58", b: "0.58.0", want: 0, wantOK: true},
		{a: "0.2.2", b: "0.58.0", want: -1, wantOK: true},
		{a: "0.9.0", b: "0.58.0", want: -1, wantOK: true},
		{a: "1.0.0", b: "0.58.0", want: 1, wantOK: true},
		{a: "0.58.1", b: "0.58.0", want: 1, wantOK: true},
		{a: "unknown", b: "0.58.0", wantOK: false},
		{a: "0.58.0", b: "", wantOK: false},
	} {
		got, ok := CompareVersions(tc.a, tc.b)
		if ok != tc.wantOK {
			t.Fatalf("CompareVersions(%q, %q) ok = %v, want %v", tc.a, tc.b, ok, tc.wantOK)
		}
		if ok && got != tc.want {
			t.Fatalf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestVersionSupported covers the three paths a declared minimum has: below
// it, at or above it, and a reported version nothing can be read from.
func TestVersionSupported(t *testing.T) {
	declared := Definition{Name: "gemini", Binary: "gemini", MinVersion: "0.58.0"}
	for _, tc := range []struct {
		name           string
		definition     Definition
		reported       string
		wantSupported  bool
		wantComparable bool
	}{
		{name: "below", definition: declared, reported: "0.2.2", wantSupported: false, wantComparable: true},
		{name: "equal", definition: declared, reported: "0.58.0", wantSupported: true, wantComparable: true},
		{name: "above", definition: declared, reported: "1.4.0", wantSupported: true, wantComparable: true},
		{name: "unreadable", definition: declared, reported: "version unavailable", wantSupported: true, wantComparable: false},
		{name: "no minimum declared", definition: Definition{Name: "codex", Binary: "codex"}, reported: "0.0.1", wantSupported: true, wantComparable: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			supported, comparable := tc.definition.VersionSupported(tc.reported)
			if supported != tc.wantSupported || comparable != tc.wantComparable {
				t.Fatalf("VersionSupported(%q) = (%v, %v), want (%v, %v)", tc.reported, supported, comparable, tc.wantSupported, tc.wantComparable)
			}
		})
	}
}

// TestVersionTooOldError checks the failure names both versions and the agent,
// which is the whole point of declaring a minimum (issue #535).
func TestVersionTooOldError(t *testing.T) {
	definition := Definition{Name: "gemini", Binary: "gemini", MinVersion: "0.58.0"}
	message := definition.VersionTooOldError("/opt/bin/gemini", "0.2.2").Error()
	for _, want := range []string{"gemini", "0.58.0", "0.2.2", "/opt/bin/gemini", "--agent-binary"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error %q does not mention %q", message, want)
		}
	}
}

// TestMinVersionValidation rejects a minimum nothing can be compared against,
// so a typo in a config file is reported rather than silently checking nothing.
func TestMinVersionValidation(t *testing.T) {
	base := Definition{
		Name: "example", Binary: "example",
		Args:    Args{Run: []Fragment{{Args: []string{"{prompt}"}}}, Resume: []Fragment{{Args: []string{"{prompt}"}}}},
		Session: Session{Assign: AssignNone}, Output: Output{Format: FormatText},
	}
	for _, tc := range []struct {
		name       string
		minVersion string
		wantErr    bool
	}{
		{name: "none", minVersion: ""},
		{name: "dotted", minVersion: "0.58.0"},
		{name: "major only", minVersion: "2"},
		{name: "words", minVersion: "latest", wantErr: true},
		{name: "spaces only", minVersion: "   ", wantErr: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			definition := base
			definition.MinVersion = tc.minVersion
			err := definition.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("minVersion %q validated, want an error", tc.minVersion)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("minVersion %q: %v", tc.minVersion, err)
			}
		})
	}
}

// TestGeminiDeclaresMinVersion pins the version the built-in gemini definition
// was measured against, since its argv uses flags older CLIs do not have.
func TestGeminiDeclaresMinVersion(t *testing.T) {
	registry := MustBuiltin()
	definition, ok := registry.Lookup("gemini")
	if !ok {
		t.Fatal("gemini is not registered")
	}
	if definition.MinVersion != "0.58.0" {
		t.Fatalf("gemini minVersion = %q, want %q", definition.MinVersion, "0.58.0")
	}
}
