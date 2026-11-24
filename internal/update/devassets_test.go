package update

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestDevAssets_ReadsTheCheckoutInGitMode(t *testing.T) {
	checkout := t.TempDir()
	if err := os.MkdirAll(filepath.Join(checkout, "skills", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "skills", "a", "SKILL.md"), []byte("checkout"), 0o644); err != nil {
		t.Fatal(err)
	}
	embedded := fstest.MapFS{"a/SKILL.md": {Data: []byte("embedded")}}

	read := func() string {
		t.Helper()
		data, err := fs.ReadFile(DevAssets(embedded, "skills"), "a/SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	withGitMode(t, "dev", checkout, false)
	if got := read(); got != "checkout" {
		t.Fatalf("dev checkout: read %q, want the checkout's copy", got)
	}
	if err := os.WriteFile(filepath.Join(checkout, "skills", "a", "SKILL.md"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "edited" {
		t.Fatalf("after an edit: read %q, want the edited copy", got)
	}

	inDocker = func() bool { return true }
	if got := read(); got != "embedded" {
		t.Fatalf("docker: read %q, want the embedded copy", got)
	}
	inDocker = func() bool { return false }
	checkoutDir = func() string { return "" }
	if got := read(); got != "embedded" {
		t.Fatalf("no checkout: read %q, want the embedded copy", got)
	}
}

func TestDevAssets_ReleaseBuildUsesEmbedded(t *testing.T) {
	embedded := fstest.MapFS{"a/SKILL.md": {Data: []byte("embedded")}}
	withGitMode(t, "1.2.3", t.TempDir(), false)
	if _, ok := DevAssets(embedded, "skills").(fstest.MapFS); !ok {
		t.Fatal("release build: DevAssets did not return the embedded FS")
	}
}

func TestLiveAsset(t *testing.T) {
	for f, want := range map[string]bool{
		".agents/skills/gh-fix/SKILL.md": true,
		"skills/gogcli/SKILL.md":         true,
		"skills/registry.go":             false,
		"support/templates/agent/x.md":   false,
		"internal/models/catalog.json":   false,
		"skillsx/a.md":                   false,
	} {
		if got := liveAsset(f); got != want {
			t.Errorf("liveAsset(%q) = %v, want %v", f, got, want)
		}
	}
}
