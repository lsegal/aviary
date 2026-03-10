package update

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lsegal/aviary/internal/buildinfo"
)

func TestConfigureEmulationAndCheck(t *testing.T) {
	orig := buildinfo.Version
	buildinfo.Version = "dev"
	t.Cleanup(func() {
		buildinfo.Version = orig
		_ = ConfigureEmulation("")
	})

	if err := ConfigureEmulation("1.2.3:1.3.0"); err != nil {
		t.Fatalf("ConfigureEmulation: %v", err)
	}
	check, err := Check(context.Background(), nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !check.Emulated {
		t.Fatal("expected emulated check result")
	}
	if !check.UpgradeAvailable {
		t.Fatal("expected upgrade to be available")
	}
	if check.CurrentVersion != "1.2.3" || check.LatestVersion != "1.3.0" {
		t.Fatalf("unexpected versions: %+v", check)
	}
}

func TestConfigureEmulationRejectsReleaseBuild(t *testing.T) {
	orig := buildinfo.Version
	buildinfo.Version = "1.2.3"
	t.Cleanup(func() { buildinfo.Version = orig })
	if err := ConfigureEmulation("1.2.3:1.3.0"); err == nil {
		t.Fatal("expected release build emulation to fail")
	}
}

func TestInstallNoopWhenEmulated(t *testing.T) {
	orig := buildinfo.Version
	buildinfo.Version = "dev"
	t.Cleanup(func() {
		buildinfo.Version = orig
		_ = ConfigureEmulation("")
	})
	if err := ConfigureEmulation("1.2.3:1.3.0"); err != nil {
		t.Fatalf("ConfigureEmulation: %v", err)
	}
	result, err := Install(context.Background(), InstallOptions{
		Version:    "1.3.0",
		TargetPath: filepath.Join(t.TempDir(), "aviary"),
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !result.Noop || !result.Emulated {
		t.Fatalf("expected noop emulated install, got %+v", result)
	}
}
