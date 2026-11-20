package clidefs

import (
	"strings"
	"testing"
)

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
			supported, canCompare := tc.definition.VersionSupported(tc.reported)
			if supported != tc.wantSupported || canCompare != tc.wantComparable {
				t.Fatalf("VersionSupported(%q) = (%v, %v), want (%v, %v)", tc.reported, supported, canCompare, tc.wantSupported, tc.wantComparable)
			}
		})
	}
}

// TestVersionTooOldError checks the failure names both versions and the agent,
// which is the whole point of declaring a minimum.
func TestVersionTooOldError(t *testing.T) {
	definition := Definition{Name: "gemini", Binary: "gemini", MinVersion: "0.58.0"}
	message := definition.VersionTooOldError("/opt/bin/gemini", "0.2.2").Error()
	for _, want := range []string{"gemini", "0.58.0", "0.2.2", "/opt/bin/gemini", "cli_definitions"} {
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
