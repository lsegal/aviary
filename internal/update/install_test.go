package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	// Drops an inherited AVIARY_CHECKOUT so no test pulls a real checkout.
	_ "github.com/lsegal/aviary/internal/testenv"
)

// serveLatestRelease answers the releases/latest redirect with tag.
func serveLatestRelease(t *testing.T, tag string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/lsegal/aviary/releases/latest" {
			http.Redirect(w, r, "/lsegal/aviary/releases/tag/"+tag, http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(DownloadBaseEnv, srv.URL)
}

func TestDetectInstall_ReleaseBinaryInsideACheckoutIsARelease(t *testing.T) {
	checkout, _ := setupCheckout(t)
	withGitMode(t, "v0.7.3", "", false)
	checkoutDir = cachedCheckout
	resetGitCaches(t)
	t.Setenv(CheckoutEnv, "")
	stubExecutable(t, t.TempDir())
	t.Chdir(checkout)

	if got := DetectInstall(); got != InstallRelease {
		t.Fatalf("DetectInstall() = %q, want release", got)
	}
	if got := DescribeInstall(); got != "release build v0.7.3" {
		t.Fatalf("DescribeInstall() = %q", got)
	}
	serveLatestRelease(t, "v0.7.5")
	check, err := Check(context.Background(), nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if check.InstallType != InstallRelease || check.Git != nil || check.LatestVersion != "v0.7.5" || !check.UpgradeAvailable {
		t.Fatalf("check = %+v, want a release check against v0.7.5", check)
	}
}

func TestDetectInstall_DevBuildFromItsCheckoutIsASource(t *testing.T) {
	checkout, _ := setupCheckout(t)
	withGitMode(t, "dev", "", false)
	checkoutDir = cachedCheckout
	resetGitCaches(t)
	t.Setenv(CheckoutEnv, "")
	stubExecutable(t, checkout)
	t.Chdir(t.TempDir())

	if got := DetectInstall(); got != InstallSource {
		t.Fatalf("DetectInstall() = %q, want source", got)
	}
	want := mustEvalSymlinks(t, checkout)
	if got := DescribeInstall(); !strings.HasSuffix(got, want) {
		t.Fatalf("DescribeInstall() = %q, want it to name %s", got, want)
	}
	check, err := Check(context.Background(), nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if check.InstallType != InstallSource || check.Git == nil || check.Git.Dir != want || !check.UpgradeAvailable {
		t.Fatalf("check = %+v, want a source check of %s", check, want)
	}
}

func TestDetectInstall_DevBuildWithoutACheckout(t *testing.T) {
	withGitMode(t, "dev", "", false)
	if got := DetectInstall(); got != InstallDev {
		t.Fatalf("DetectInstall() = %q, want dev", got)
	}
	if got := DescribeInstall(); got != "dev build without a source checkout" {
		t.Fatalf("DescribeInstall() = %q", got)
	}
}

func TestCheckMode_ForcesTheMode(t *testing.T) {
	checkout, _ := setupCheckout(t)
	withGitMode(t, "v0.7.3", "", false)
	checkoutDir = cachedCheckout
	resetGitCaches(t)
	t.Setenv(CheckoutEnv, "")
	stubExecutable(t, t.TempDir())
	t.Chdir(checkout)

	check, err := CheckMode(context.Background(), nil, InstallSource)
	if err != nil {
		t.Fatalf("CheckMode(source): %v", err)
	}
	if check.InstallType != InstallSource || check.Git == nil || check.Git.Dir != mustEvalSymlinks(t, checkout) {
		t.Fatalf("check = %+v, want --source to check the working directory's checkout", check)
	}

	t.Chdir(t.TempDir())
	if _, err := CheckMode(context.Background(), nil, InstallSource); err == nil || !strings.Contains(err.Error(), "no Aviary source checkout") {
		t.Fatalf("CheckMode(source) outside a checkout: err = %v", err)
	}

	withGitMode(t, "dev", checkout, false)
	serveLatestRelease(t, "v0.7.5")
	check, err = CheckMode(context.Background(), nil, InstallRelease)
	if err != nil {
		t.Fatalf("CheckMode(release): %v", err)
	}
	if check.InstallType != InstallRelease || check.Git != nil || check.LatestVersion != "v0.7.5" {
		t.Fatalf("check = %+v, want --release to check GitHub releases on a dev build", check)
	}
}

func TestGitPullMessage_NamesTheCheckout(t *testing.T) {
	if got := GitPullMessage(GitPullResult{Dir: "/src/aviary", From: "abc", To: "abc"}, DevReloadPlan{}); got != "The source checkout at /src/aviary is already up to date (abc)." {
		t.Fatalf("up to date: %q", got)
	}
	if got := GitPullMessage(GitPullResult{Dir: "/src/aviary", From: "abc", To: "def", Commits: 2}, DevReloadPlan{}); !strings.HasPrefix(got, "Pulled 2 commit(s) into /src/aviary (abc → def).") {
		t.Fatalf("pulled: %q", got)
	}
}

func TestDisplayVersion(t *testing.T) {
	for in, want := range map[string]string{"0.7.4": "v0.7.4", "v0.7.4": "v0.7.4", "dev": "dev", "588e26e2": "588e26e2"} {
		if got := DisplayVersion(in); got != want {
			t.Errorf("DisplayVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
