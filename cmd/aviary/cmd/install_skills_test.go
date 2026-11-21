package cmd

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/lsegal/aviary/internal/codingskills"
)

func TestInstallSkillsCommandInstallsGlobally(t *testing.T) {
	var calls [][]string
	manager := &codingskills.Manager{
		Env: codingskills.Env{
			Home:     t.TempDir(),
			Getenv:   func(string) string { return "" },
			LookPath: func(string) (string, error) { return "", errors.New("not found") },
		},
		Bundle:  fstest.MapFS{},
		Version: func() string { return "2.0.0" },
		CLIs:    func() []codingskills.CLI { return nil },
		Run: func(_ context.Context, args ...string) error {
			calls = append(calls, args)
			return nil
		},
	}
	installSkillsManager = func() *codingskills.Manager { return manager }
	var out bytes.Buffer
	installSkillsCmd.SetOut(&out)
	installSkillsCmd.SetContext(context.Background())
	t.Cleanup(func() {
		installSkillsManager = codingskills.Default
		installSkillsCmd.SetOut(nil)
	})

	if err := installSkillsCmd.RunE(installSkillsCmd, nil); err != nil {
		t.Fatalf("install-skills failed: %v", err)
	}
	want := "installed gh-fix\ninstalled gh-discuss\ninstalled gh-create\ninstalled gh-safe-merge\n"
	if got := out.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if len(calls) != 4 || calls[0][4] != "--global" || calls[0][6] != "universal" {
		t.Fatalf("npx calls = %v, want four global installs for universal", calls)
	}
}
