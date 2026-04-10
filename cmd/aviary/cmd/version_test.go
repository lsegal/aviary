package cmd

import (
	"bytes"
	"testing"

	"github.com/lsegal/aviary/internal/buildinfo"
)

func TestVersionCommand(t *testing.T) {
	var out bytes.Buffer
	versionCmd.SetOut(&out)

	t.Cleanup(func() {
		versionCmd.SetOut(nil)
	})

	if err := versionCmd.RunE(versionCmd, nil); err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	want := "aviary version " + buildinfo.Version + "\n"
	if got := out.String(); got != want {
		t.Fatalf("unexpected output: got %q want %q", got, want)
	}
}
