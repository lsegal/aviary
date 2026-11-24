package update

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// stubBuildVCS makes the binary report rev as its embedded VCS revision.
func stubBuildVCS(t *testing.T, rev string, modified bool) {
	t.Helper()
	origRev, origMod := buildRevision, buildModified
	buildRevision = func() string { return rev }
	buildModified = func() bool { return modified }
	t.Cleanup(func() { buildRevision, buildModified = origRev, origMod })
}

func TestUpgradeSummary_Release(t *testing.T) {
	withGitMode(t, "v0.7.3", "", false)
	dir := t.TempDir()
	stubExecutable(t, dir)
	if got, want := UpgradeSummary(), "Installed release v0.7.3 at "+filepath.Join(dir, "aviary")+"."; got != want {
		t.Fatalf("UpgradeSummary() = %q, want %q", got, want)
	}
	reasons := InstallReasons()
	if len(reasons) != 1 || !strings.Contains(reasons[0], "v0.7.3 is a release") {
		t.Fatalf("InstallReasons() = %q", reasons)
	}
}

func TestUpgradeSummary_DevBuildFromItsCheckout(t *testing.T) {
	checkout, _ := setupCheckout(t)
	withGitMode(t, "dev", "", false)
	checkoutDir = cachedCheckout
	resetGitCaches(t)
	t.Setenv(CheckoutEnv, "")
	stubExecutable(t, checkout)
	stubBuildVCS(t, "0123456789abcdef0123", true)
	t.Chdir(t.TempDir())

	want := mustEvalSymlinks(t, checkout)
	if got := UpgradeSummary(); got != "Running a dev build of 0123456789ab (with uncommitted changes) from the source checkout at "+want+"." {
		t.Fatalf("UpgradeSummary() = %q", got)
	}
	reasons := strings.Join(InstallReasons(), "\n")
	for _, s := range []string{`"dev" is not a release`, "revision 0123456789ab", "working directory is never used", "is inside the Aviary checkout at " + want} {
		if !strings.Contains(reasons, s) {
			t.Errorf("InstallReasons() = %q, want it to mention %q", reasons, s)
		}
	}
}

func TestUpgradeSummary_DevBuildWithoutACheckout(t *testing.T) {
	checkout, _ := setupCheckout(t)
	withGitMode(t, "dev", "", false)
	stubExecutable(t, t.TempDir())
	stubBuildVCS(t, "", false)
	t.Setenv(CheckoutEnv, "")
	t.Chdir(checkout)

	if got := UpgradeSummary(); got != "Running a dev build with no source checkout to update from." {
		t.Fatalf("UpgradeSummary() = %q", got)
	}
	reasons := strings.Join(InstallReasons(), "\n")
	if !strings.Contains(reasons, "embeds no VCS revision") || !strings.Contains(reasons, "is not inside an Aviary git checkout") {
		t.Fatalf("InstallReasons() = %q", reasons)
	}

	t.Setenv(CheckoutEnv, t.TempDir())
	if reasons := strings.Join(InstallReasons(), "\n"); !strings.Contains(reasons, "is not an Aviary git checkout") {
		t.Fatalf("InstallReasons() with a bad %s = %q", CheckoutEnv, reasons)
	}
}

func TestGitPull_ReportsWhatItCompared(t *testing.T) {
	checkout, other := setupCheckout(t)
	upstream := gitT(t, other, "rev-parse", "--short", "HEAD")
	local := gitT(t, checkout, "rev-parse", "--short", "HEAD")
	res, err := GitPull(context.Background(), checkout)
	if err != nil {
		t.Fatalf("GitPull: %v", err)
	}
	if !res.Fetched || res.Upstream != "origin/main" || res.Head != gitT(t, other, "rev-parse", "HEAD") {
		t.Fatalf("result = %+v", res)
	}
	if got, want := GitCompareMessage(res), "Fetched origin/main: local "+local+" vs origin/main "+upstream+" (0 ahead, 1 behind)."; got != want {
		t.Fatalf("GitCompareMessage() = %q, want %q", got, want)
	}

	res, err = GitPull(context.Background(), checkout)
	if err != nil {
		t.Fatalf("second GitPull: %v", err)
	}
	if got, want := GitPullMessage(res, DevReloadPlan{}), "The source checkout at "+checkout+" is already up to date with origin/main ("+upstream+")."; got != want {
		t.Fatalf("GitPullMessage() = %q, want %q", got, want)
	}
}

func TestBinaryStaleness(t *testing.T) {
	head := "1111111111111111"
	for _, tc := range []struct {
		name, rev string
		modified  bool
		head      string
		want      string
		stale     bool
	}{
		{name: "no head", rev: head},
		{name: "no revision", head: head, want: "embeds no VCS revision"},
		{name: "older", rev: "2222222222222222", head: head, want: "built from 222222222222, but the checkout's HEAD is 111111111111, so the running binary is stale", stale: true},
		{name: "current", rev: head, head: head, want: "built from the checkout's HEAD (111111111111)."},
		{name: "current with changes", rev: head, modified: true, head: head, want: "with uncommitted changes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubBuildVCS(t, tc.rev, tc.modified)
			msg, stale := BinaryStaleness(tc.head)
			if stale != tc.stale || (tc.want == "" && msg != "") || !strings.Contains(msg, tc.want) {
				t.Fatalf("BinaryStaleness(%q) = %q, %v; want %q, %v", tc.head, msg, stale, tc.want, tc.stale)
			}
		})
	}
}

func TestRebuildCommand(t *testing.T) {
	stubExecutable(t, "/usr/local/bin")
	if got, want := RebuildCommand("/src/aviary"), "cd /src/aviary && go build -o /usr/local/bin/aviary ./cmd/aviary"; got != want {
		t.Fatalf("RebuildCommand() = %q, want %q", got, want)
	}
	stubExecutable(t, filepath.Join("/tmp", "go-build123", "b001", "exe"))
	if got, want := RebuildCommand("/src/aviary"), "cd /src/aviary && go build -o aviary ./cmd/aviary"; got != want {
		t.Fatalf("RebuildCommand() for a go run binary = %q, want %q", got, want)
	}
}
