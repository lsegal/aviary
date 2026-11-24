package update

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// withBuild stubs the binary's build info: its main module, embedded
// revision, and path.
func withBuild(t *testing.T, module, revision, exe string) {
	t.Helper()
	origModule, origRev, origModified, origExe := buildMainModule, buildRevision, buildModified, executable
	buildMainModule = func() string { return module }
	buildRevision = func() string { return revision }
	buildModified = func() bool { return false }
	executable = func() (string, error) {
		if exe == "" {
			return "", errors.New("no executable")
		}
		return exe, nil
	}
	t.Cleanup(func() {
		buildMainModule, buildRevision, buildModified, executable = origModule, origRev, origModified, origExe
	})
}

func reasons(det CheckoutDetection) string { return strings.Join(det.Reasons, "; ") }

func TestDetectCheckout_EnvIsAuthoritativeOutsideTheCheckout(t *testing.T) {
	withBuild(t, "", "", "/usr/local/bin/aviary")
	checkout := initCheckout(t, "module github.com/lsegal/aviary\r\n\r\ngo 1.25\r\n")
	t.Chdir(t.TempDir())
	t.Setenv(CheckoutEnv, filepath.Join(checkout, "internal"))
	det := detectCheckout()
	if det.Dir != checkout || !strings.Contains(reasons(det), CheckoutEnv) {
		t.Fatalf("detectCheckout() = %+v, want %q from %s", det, checkout, CheckoutEnv)
	}
	t.Setenv(CheckoutEnv, t.TempDir())
	if det := detectCheckout(); det.Dir != "" || !strings.Contains(reasons(det), "is not an Aviary git checkout") {
		t.Fatalf("detectCheckout() = %+v for a non-checkout %s, want none", det, CheckoutEnv)
	}
}

func TestDetectCheckout_WorkingDirectoryNeedsBuildInfo(t *testing.T) {
	checkout := initCheckout(t, "module github.com/lsegal/aviary\n")
	commitFile(t, checkout, "a.txt", "a")
	head := gitT(t, checkout, "rev-parse", "HEAD")
	t.Setenv(CheckoutEnv, "")
	t.Chdir(filepath.Join(checkout, "internal"))

	withBuild(t, aviaryModule, head, "/usr/local/bin/aviary")
	if det := detectCheckout(); det.Dir != checkout || !strings.Contains(reasons(det), "built from commit "+shortSHA(head)) {
		t.Fatalf("binary built from the checkout's commit: detectCheckout() = %+v", det)
	}

	withBuild(t, aviaryModule, strings.Repeat("ab", 20), "/usr/local/bin/aviary")
	if det := detectCheckout(); det.Dir != "" || !strings.Contains(reasons(det), "does not contain") {
		t.Fatalf("binary built from another commit: detectCheckout() = %+v, want none", det)
	}

	withBuild(t, aviaryModule, "", "/usr/local/bin/aviary")
	if det := detectCheckout(); det.Dir != "" || !strings.Contains(reasons(det), "embeds no VCS revision") {
		t.Fatalf("binary without a revision: detectCheckout() = %+v, want none", det)
	}

	withBuild(t, aviaryModule, "", filepath.Join(t.TempDir(), "go-build123", "b001", "exe", "aviary"))
	if det := detectCheckout(); det.Dir != checkout || !strings.Contains(reasons(det), "go run") {
		t.Fatalf("go run binary: detectCheckout() = %+v", det)
	}

	withBuild(t, "example.com/other", head, "/usr/local/bin/aviary")
	if det := detectCheckout(); det.Dir != "" || !strings.Contains(reasons(det), "main module") {
		t.Fatalf("binary of another module: detectCheckout() = %+v, want none", det)
	}

	other := initCheckout(t, "module example.com/other\n")
	t.Chdir(other)
	withBuild(t, aviaryModule, head, "/usr/local/bin/aviary")
	if det := detectCheckout(); det.Dir != "" || !strings.Contains(reasons(det), "not inside an Aviary git checkout") {
		t.Fatalf("detectCheckout() = %+v in another module, want none", det)
	}
}

func TestDetectMode_SummaryNamesTheMode(t *testing.T) {
	withBuild(t, aviaryModule, "0123456789abcdef0123", "/opt/aviary/bin/aviary")

	withGitMode(t, "1.4.0", "/src/aviary", false)
	m := DetectMode()
	if m.Dev || m.Checkout != "" || m.Summary() != "Installed release 1.4.0 at /opt/aviary/bin/aviary." {
		t.Fatalf("release: %+v, summary %q", m, m.Summary())
	}
	if !strings.Contains(strings.Join(m.Reasons, " "), "is a release") {
		t.Fatalf("release reasons = %q", m.Reasons)
	}

	withGitMode(t, "dev", "/src/aviary", false)
	m = DetectMode()
	if want := "Running a dev build of 0123456789ab from the source checkout at /src/aviary."; m.Summary() != want {
		t.Fatalf("dev checkout summary = %q, want %q", m.Summary(), want)
	}

	withGitMode(t, "dev", "", false)
	if got := DetectMode().Summary(); !strings.Contains(got, "no source checkout") {
		t.Fatalf("dev without checkout summary = %q", got)
	}

	withGitMode(t, "dev", "/src/aviary", true)
	m = DetectMode()
	if m.Checkout != "" || !strings.Contains(m.Summary(), "upgrades from GitHub releases") || !strings.Contains(strings.Join(m.Reasons, " "), "Docker") {
		t.Fatalf("dev in docker: %+v, summary %q", m, m.Summary())
	}
}

func TestBinaryStaleness(t *testing.T) {
	head := strings.Repeat("a", 40)
	withBuild(t, aviaryModule, head, "/usr/local/bin/aviary")
	if msg, stale := BinaryStaleness(head); stale || !strings.Contains(msg, "built from the checkout's HEAD") {
		t.Fatalf("current binary: %q stale=%v", msg, stale)
	}
	withBuild(t, aviaryModule, strings.Repeat("b", 40), "/usr/local/bin/aviary")
	if msg, stale := BinaryStaleness(head); !stale || !strings.Contains(msg, "running binary is stale") {
		t.Fatalf("old binary: %q stale=%v", msg, stale)
	}
	if got := RebuildCommand("/src/aviary"); got != "cd /src/aviary && go build -o /usr/local/bin/aviary ./cmd/aviary" {
		t.Fatalf("RebuildCommand = %q", got)
	}
	withBuild(t, aviaryModule, "", "/usr/local/bin/aviary")
	if msg, stale := BinaryStaleness(head); !stale || !strings.Contains(msg, "no VCS revision") {
		t.Fatalf("binary without a revision: %q stale=%v", msg, stale)
	}
}
