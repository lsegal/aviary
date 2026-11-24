package update

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/buildinfo"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "add", name)
	gitT(t, dir, "commit", "-q", "-m", "change "+name)
}

// setupCheckout returns a clone one commit behind its upstream, plus a second
// clone used to push further upstream commits.
func setupCheckout(t *testing.T) (checkout, other string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	gitT(t, root, "init", "-q", "--bare", "-b", "main", remote)
	other = filepath.Join(root, "other")
	gitT(t, root, "clone", "-q", remote, other)
	gitT(t, other, "checkout", "-q", "-b", "main")
	commitFile(t, other, "go.mod", "module github.com/lsegal/aviary\n")
	gitT(t, other, "push", "-q", "-u", "origin", "main")
	checkout = filepath.Join(root, "checkout")
	gitT(t, root, "clone", "-q", remote, checkout)
	commitFile(t, other, "a.txt", "a")
	gitT(t, other, "push", "-q")
	return checkout, other
}

func TestReadGitState_BehindUpstream(t *testing.T) {
	checkout, _ := setupCheckout(t)
	t.Setenv(RestartFileEnv, filepath.Join(t.TempDir(), "restart"))
	st, err := ReadGitState(context.Background(), checkout)
	if err != nil {
		t.Fatalf("ReadGitState: %v", err)
	}
	if st.Branch != "main" || st.Upstream != "origin/main" || st.Behind != 1 || st.Ahead != 0 || st.Dirty || !st.Supervised {
		t.Fatalf("state = %+v", st)
	}
}

func TestGitPull_FastForwardsCleanCheckout(t *testing.T) {
	checkout, other := setupCheckout(t)
	t.Setenv(RestartFileEnv, "")
	res, err := GitPull(context.Background(), checkout)
	if err != nil {
		t.Fatalf("GitPull: %v", err)
	}
	if res.Commits != 1 || res.From == res.To || res.Supervised {
		t.Fatalf("result = %+v", res)
	}
	if got, want := gitT(t, checkout, "rev-parse", "HEAD"), gitT(t, other, "rev-parse", "HEAD"); got != want {
		t.Fatalf("HEAD = %s, want upstream %s", got, want)
	}
	if msg := GitPullMessage(res, DevReloadPlan{GoChanged: true, RestartReason: "forgo hot reload is not active"}); !strings.Contains(msg, "Restart Aviary") {
		t.Fatalf("message without hot reload = %q, want a restart hint", msg)
	}
}

func TestGitPull_RefusesDirtyTree(t *testing.T) {
	checkout, _ := setupCheckout(t)
	before := gitT(t, checkout, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module github.com/lsegal/aviary\n// edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := GitPull(context.Background(), checkout)
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("err = %v, want a dirty-tree refusal", err)
	}
	if after := gitT(t, checkout, "rev-parse", "HEAD"); after != before {
		t.Fatalf("HEAD moved from %s to %s on a dirty tree", before, after)
	}
}

func TestGitPull_RefusesDivergedBranch(t *testing.T) {
	checkout, _ := setupCheckout(t)
	commitFile(t, checkout, "local.txt", "local")
	_, err := GitPull(context.Background(), checkout)
	if err == nil || !strings.Contains(err.Error(), "would not fast-forward") {
		t.Fatalf("err = %v, want a non-fast-forward refusal", err)
	}
}

func withGitMode(t *testing.T, version, dir string, docker bool) {
	t.Helper()
	origVersion, origDocker, origDir := buildinfo.Version, inDocker, checkoutDir
	buildinfo.Version = version
	inDocker = func() bool { return docker }
	checkoutDir = func() string { return dir }
	ClearCache()
	t.Cleanup(func() {
		buildinfo.Version, inDocker, checkoutDir = origVersion, origDocker, origDir
		_ = ConfigureEmulation("")
		ClearCache()
	})
}

func TestGitCheckout_OnlyForDevCheckoutsOutsideDocker(t *testing.T) {
	withGitMode(t, "dev", "/src/aviary", false)
	if got := GitCheckout(); got != "/src/aviary" {
		t.Fatalf("dev checkout: GitCheckout() = %q", got)
	}
	inDocker = func() bool { return true }
	if got := GitCheckout(); got != "" {
		t.Fatalf("docker: GitCheckout() = %q, want release mode", got)
	}
	inDocker = func() bool { return false }
	if err := ConfigureEmulation("1.0.0:1.1.0"); err != nil {
		t.Fatal(err)
	}
	if got := GitCheckout(); got != "" {
		t.Fatalf("emulation: GitCheckout() = %q, want release mode", got)
	}
	_ = ConfigureEmulation("")
	buildinfo.Version = "1.2.3"
	if got := GitCheckout(); got != "" {
		t.Fatalf("release build: GitCheckout() = %q, want release mode", got)
	}
}

func TestCheck_GitModeReportsUpstreamState(t *testing.T) {
	checkout, other := setupCheckout(t)
	withGitMode(t, "dev", checkout, false)
	check, err := Check(context.Background(), nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if check.Git == nil || !check.UpgradeAvailable || !check.UpgradeSupported {
		t.Fatalf("check = %+v, want an available git update", check)
	}
	if want := gitT(t, checkout, "rev-parse", "HEAD"); check.CurrentVersion != want {
		t.Fatalf("CurrentVersion = %q, want checkout HEAD %q", check.CurrentVersion, want)
	}
	if want := gitT(t, other, "rev-parse", "HEAD"); check.LatestVersion != want {
		t.Fatalf("LatestVersion = %q, want upstream %q", check.LatestVersion, want)
	}

	if err := os.WriteFile(filepath.Join(checkout, "go.mod"), []byte("module github.com/lsegal/aviary\n// edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ClearCache()
	check, err = Check(context.Background(), nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !check.UpgradeAvailable || check.UpgradeSupported || !strings.Contains(check.Message, "uncommitted changes") {
		t.Fatalf("dirty check = %+v, want the update blocked with the git state as the reason", check)
	}
}

func TestCurrentVersion_DevCheckoutReportsHEAD(t *testing.T) {
	checkout, other := setupCheckout(t)
	withGitMode(t, "dev", checkout, false)
	if got, want := CurrentVersion(context.Background()), gitT(t, checkout, "rev-parse", "HEAD"); got != want {
		t.Fatalf("CurrentVersion() = %q, want HEAD %q", got, want)
	}
	if _, err := GitPull(context.Background(), checkout); err != nil {
		t.Fatalf("GitPull: %v", err)
	}
	if got, want := CurrentVersion(context.Background()), gitT(t, other, "rev-parse", "HEAD"); got != want {
		t.Fatalf("after pull: CurrentVersion() = %q, want new HEAD %q", got, want)
	}
}

func TestCurrentVersion_DevFallbacks(t *testing.T) {
	withGitMode(t, "dev", "", false)
	origRevision := buildRevision
	t.Cleanup(func() { buildRevision = origRevision })

	buildRevision = func() string { return "0123456789abcdef0123456789abcdef01234567" }
	if got := CurrentVersion(context.Background()); got != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("outside a checkout: CurrentVersion() = %q, want the embedded revision", got)
	}
	buildRevision = func() string { return "" }
	if got := CurrentVersion(context.Background()); got != "dev" {
		t.Fatalf("no revision: CurrentVersion() = %q, want dev", got)
	}
}

func TestCurrentVersion_ReleaseBuildUnchanged(t *testing.T) {
	checkout, _ := setupCheckout(t)
	withGitMode(t, "1.2.3", checkout, false)
	if got := CurrentVersion(context.Background()); got != "1.2.3" {
		t.Fatalf("release: CurrentVersion() = %q, want 1.2.3", got)
	}
}

func TestModulePath_ToleratesLineEndingsAndWhitespace(t *testing.T) {
	for _, mod := range []string{
		"module github.com/lsegal/aviary\n",
		"module github.com/lsegal/aviary\r\n\r\ngo 1.25\r\n",
		"// Aviary\n\n  module   github.com/lsegal/aviary  \t\n",
		"module \"github.com/lsegal/aviary\" // quoted\n",
		"module github.com/lsegal/aviary",
	} {
		if got := modulePath([]byte(mod)); got != aviaryModule {
			t.Errorf("modulePath(%q) = %q, want %q", mod, got, aviaryModule)
		}
	}
	if got := modulePath([]byte("module github.com/lsegal/aviary-fork\r\n")); got == aviaryModule {
		t.Errorf("another module matched: %q", got)
	}
}

// initCheckout creates a git checkout whose go.mod is written verbatim.
func initCheckout(t *testing.T, gomod string) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	gitT(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestCheckoutDir_EnvIsAuthoritativeOutsideTheCheckout(t *testing.T) {
	checkout := initCheckout(t, "module github.com/lsegal/aviary\r\n\r\ngo 1.25\r\n")
	t.Chdir(t.TempDir())
	t.Setenv(CheckoutEnv, filepath.Join(checkout, "internal"))
	if got := findCheckout(); got != checkout {
		t.Fatalf("findCheckout() = %q, want %q from %s", got, checkout, CheckoutEnv)
	}
	t.Setenv(CheckoutEnv, t.TempDir())
	if got := findCheckout(); got != "" {
		t.Fatalf("findCheckout() = %q for a non-checkout %s, want none", got, CheckoutEnv)
	}
}

func TestCheckoutDir_FallsBackToWorkingDirectory(t *testing.T) {
	checkout := initCheckout(t, "module github.com/lsegal/aviary\r\n")
	t.Setenv(CheckoutEnv, "")
	t.Chdir(filepath.Join(checkout, "internal"))
	if got := findCheckout(); got != checkout {
		t.Fatalf("findCheckout() = %q, want %q", got, checkout)
	}
	other := initCheckout(t, "module example.com/other\n")
	t.Chdir(other)
	if got := findCheckout(); got != "" {
		t.Fatalf("findCheckout() = %q in another module, want none", got)
	}
}

func TestCheck_DevBuildWithoutCheckoutReportsNoCheckout(t *testing.T) {
	withGitMode(t, "dev", "", false)
	check, err := Check(context.Background(), nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !check.NoCheckout || check.Git != nil || check.UpgradeAvailable || check.LatestVersion != "" {
		t.Fatalf("check = %+v, want the no-checkout state", check)
	}
	if !strings.Contains(check.Message, "no source checkout") {
		t.Fatalf("message = %q", check.Message)
	}
}

func TestRequestRebuild_WritesSupervisorRestartFile(t *testing.T) {
	t.Setenv(RestartFileEnv, "")
	if Supervised() || RequestRebuild() {
		t.Fatal("an unsupervised backend must re-exec instead of asking for a rebuild")
	}
	path := filepath.Join(t.TempDir(), "restart")
	t.Setenv(RestartFileEnv, path)
	if !Supervised() || !RequestRebuild() {
		t.Fatal("a supervised backend asks the supervisor to rebuild it")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("restart file not written: %v", err)
	}
}
