package projects

import (
	"context"
	"errors"
	"testing"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/clidoctor"
)

// TestDoctorAndDispatchResolveTheSameOverride checks a cli_definitions binary
// override off PATH is reported installed at that path, and that jobs run the
// same executable (#456).
func TestDoctorAndDispatchResolveTheSameOverride(t *testing.T) {
	const override = "/opt/nightly/codex"
	registry, err := clidefs.FromConfig(map[string]any{"codex": map[string]any{"binary": override}})
	if err != nil {
		t.Fatal(err)
	}
	doctor := clidoctor.New(registry)
	doctor.LookPath = func(binary string) (string, error) {
		if binary != override {
			return "", errors.New("not found")
		}
		return binary, nil
	}
	doctor.Version = func(context.Context, string) ([]byte, error) { return nil, errors.New("no version") }
	doctor.Run = func(context.Context, []string, clidefs.Doctor, []string) ([]byte, bool) { return nil, false }
	doctor.Quota = func(context.Context, string, string) (string, error) { return "", nil }
	doctor.SkillsNote = nil
	status := clidoctor.StatusOf(doctor.Probe(context.Background(), "codex"))
	if status.Path != override || status.Binary != override {
		t.Fatalf("doctor binary = %q, path = %q, want the override %q reported installed", status.Binary, status.Path, override)
	}
	runner := CommandRunner{Agent: "codex", Definitions: registry}
	if got := runner.binary("codex"); got != status.Path {
		t.Fatalf("dispatch binary = %q, want the doctor's %q", got, status.Path)
	}
}
