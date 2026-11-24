package update

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lsegal/aviary/internal/buildinfo"
)

// buildModified reports whether the binary was built with uncommitted
// changes. It is a variable so tests can stub it.
var buildModified = func() bool { return buildSetting("vcs.modified") == "true" }

// UpgradeSummary says in one line which kind of install aviary upgrade is
// working on: a release at its path, or a dev build and its source checkout.
func UpgradeSummary() string {
	switch DetectInstall() {
	case InstallRelease:
		version := NormalizeVersion(buildinfo.Version)
		if exe, err := executablePath(); err == nil {
			return fmt.Sprintf("Installed release %s at %s.", version, exe)
		}
		return fmt.Sprintf("Installed release %s.", version)
	case InstallSource:
		return fmt.Sprintf("Running %s from the source checkout at %s.", devBuildName(), GitCheckout())
	}
	if EmulationActive() || inDocker() {
		return fmt.Sprintf("Running %s that upgrades from GitHub releases.", devBuildName())
	}
	return fmt.Sprintf("Running %s with no source checkout to update from.", devBuildName())
}

// devBuildName names a dev build by the commit it was built from, when Go
// embedded one.
func devBuildName() string {
	rev := buildRevision()
	if rev == "" {
		return "a dev build"
	}
	name := "a dev build of " + shortSHA(rev)
	if buildModified() {
		name += " (with uncommitted changes)"
	}
	return name
}

// InstallReasons explains how DetectInstall decided the install type, for
// aviary upgrade --verbose. It reruns checkout detection rather than reading
// the cached answer, so each step can say what it found.
func InstallReasons() []string {
	if !IsDevBuild() {
		return []string{fmt.Sprintf("the binary's stamped version %s is a release, so it upgrades from GitHub releases", buildinfo.Version)}
	}
	reasons := []string{fmt.Sprintf("the binary's stamped version %q is not a release, so it is a dev build", buildinfo.Version)}
	if rev := buildRevision(); rev != "" {
		reasons = append(reasons, fmt.Sprintf("Go embedded VCS revision %s in the binary", shortSHA(rev)))
	} else {
		reasons = append(reasons, "the binary embeds no VCS revision")
	}
	switch {
	case EmulationActive():
		return append(reasons, "version emulation is on, so the release updater is in force")
	case inDocker():
		return append(reasons, "it runs in Docker, so it upgrades from GitHub releases")
	}
	if env := os.Getenv(CheckoutEnv); env != "" {
		if top := verifyCheckout(env); top != "" {
			return append(reasons, fmt.Sprintf("%s=%s names the Aviary checkout at %s", CheckoutEnv, env, top))
		}
		return append(reasons, fmt.Sprintf("%s=%s is not an Aviary git checkout", CheckoutEnv, env))
	}
	reasons = append(reasons, CheckoutEnv+" is not set, so the checkout is looked up from the executable's location; the working directory is never used")
	exe, err := executablePath()
	if err != nil {
		return append(reasons, fmt.Sprintf("the executable's path is unknown: %v", err))
	}
	if top := verifyCheckout(filepath.Dir(exe)); top != "" {
		return append(reasons, fmt.Sprintf("the executable %s is inside the Aviary checkout at %s", exe, top))
	}
	return append(reasons, fmt.Sprintf("the executable %s is not inside an Aviary git checkout", exe))
}

// BinaryStaleness compares the commit the running binary was built from with
// head, the checkout's HEAD, and says whether the binary runs older code.
// stale is true when the binary needs rebuilding to run head.
func BinaryStaleness(head string) (msg string, stale bool) {
	rev := buildRevision()
	switch {
	case head == "":
		return "", false
	case rev == "":
		return "This binary embeds no VCS revision, so it cannot be compared with the checkout's HEAD.", false
	case rev != head:
		return fmt.Sprintf("This binary was built from %s, but the checkout's HEAD is %s, so the running binary is stale.", shortSHA(rev), shortSHA(head)), true
	case buildModified():
		return fmt.Sprintf("This binary was built from the checkout's HEAD (%s) with uncommitted changes.", shortSHA(rev)), false
	}
	return fmt.Sprintf("This binary was built from the checkout's HEAD (%s).", shortSHA(rev)), false
}

// RebuildCommand is the command that rebuilds the running binary from the
// checkout at dir.
func RebuildCommand(dir string) string {
	exe, err := executablePath()
	if err != nil || goRunBinary(exe) {
		return fmt.Sprintf("cd %s && go build -o aviary ./cmd/aviary", dir)
	}
	return fmt.Sprintf("cd %s && go build -o %s ./cmd/aviary", dir, exe)
}

// goRunBinary reports whether exe is a temporary binary go run built, which
// lives at <tmp>/go-build<N>/b001/exe/<name> and is not worth rebuilding in
// place.
func goRunBinary(exe string) bool {
	dir := filepath.Dir(exe)
	return filepath.Base(dir) == "exe" && strings.HasPrefix(filepath.Base(filepath.Dir(filepath.Dir(dir))), "go-build")
}
