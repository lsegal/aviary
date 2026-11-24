package update

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/lsegal/aviary/internal/buildinfo"
)

// CheckoutDetection is where git mode found the source checkout a dev build
// updates from, and why. Dir is "" when no checkout was accepted.
type CheckoutDetection struct {
	Dir     string
	Reasons []string
}

// Mode is how this binary upgrades: from GitHub releases, or by pulling the
// source checkout it was built from.
type Mode struct {
	// Dev is true for a binary whose version is not a clean semver release.
	Dev bool
	// Version is the release version, or "dev" for a dev build.
	Version string
	// Revision is the VCS commit Go embedded in the binary, if any, and
	// Modified whether it was built with uncommitted changes.
	Revision string
	Modified bool
	// Checkout is the source checkout a dev build updates from, or "" when
	// upgrades come from releases or no checkout was found.
	Checkout string
	// Reasons explains how the mode was chosen.
	Reasons []string
}

var (
	// buildMainModule and buildModified read the binary's build info. They
	// are variables so tests can stub them.
	buildMainModule = func() string {
		if info, ok := debug.ReadBuildInfo(); ok {
			return info.Main.Path
		}
		return ""
	}
	buildModified = func() bool { return buildSetting("vcs.modified") == "true" }
	executable    = os.Executable
)

func buildSetting(key string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}

// detectCheckout locates the Aviary checkout from CheckoutEnv, else the
// working directory. The working directory alone does not prove the binary
// was built there, so its checkout is accepted only when the binary's build
// info ties the two together: the binary is Aviary's main module and either
// embeds a commit the checkout contains or was built by go run, which builds
// the working directory's module.
func detectCheckout() CheckoutDetection {
	if env := os.Getenv(CheckoutEnv); env != "" {
		if top := verifyCheckout(env); top != "" {
			return CheckoutDetection{Dir: top, Reasons: []string{fmt.Sprintf("%s=%s names the Aviary checkout at %s", CheckoutEnv, env, top)}}
		}
		return CheckoutDetection{Reasons: []string{fmt.Sprintf("%s=%s is not an Aviary git checkout", CheckoutEnv, env)}}
	}
	wd, err := os.Getwd()
	if err != nil {
		return CheckoutDetection{Reasons: []string{fmt.Sprintf("%s is not set and the working directory is unknown: %v", CheckoutEnv, err)}}
	}
	top := verifyCheckout(wd)
	if top == "" {
		return CheckoutDetection{Reasons: []string{fmt.Sprintf("%s is not set and the working directory %s is not inside an Aviary git checkout", CheckoutEnv, wd)}}
	}
	reject := func(why string) CheckoutDetection {
		return CheckoutDetection{Reasons: []string{fmt.Sprintf("the working directory is inside the Aviary checkout at %s, but %s; set %s to update from it", top, why, CheckoutEnv)}}
	}
	if mod := buildMainModule(); mod != aviaryModule {
		return reject(fmt.Sprintf("the binary's main module is %q, not %s", mod, aviaryModule))
	}
	if rev := buildRevision(); rev != "" {
		if _, err := runGit(WithCaller(context.Background(), "checkout detection"), top, "cat-file", "-e", rev+"^{commit}"); err != nil {
			return reject(fmt.Sprintf("the binary was built from commit %s, which that checkout does not contain", shortSHA(rev)))
		}
		return CheckoutDetection{Dir: top, Reasons: []string{fmt.Sprintf("the binary was built from commit %s, which the Aviary checkout at %s in the working directory contains", shortSHA(rev), top)}}
	}
	if goRunBinary() {
		return CheckoutDetection{Dir: top, Reasons: []string{fmt.Sprintf("the binary was built by go run from the Aviary checkout at %s in the working directory", top)}}
	}
	return reject("the binary embeds no VCS revision to tie it to that checkout")
}

// goRunBinary reports whether the binary is one go run built: go run leaves
// it at <tmp>/go-build<N>/b001/exe/<name>.
func goRunBinary() bool {
	exe, err := executable()
	if err != nil {
		return false
	}
	dir := filepath.Dir(exe)
	return filepath.Base(dir) == "exe" && strings.HasPrefix(filepath.Base(filepath.Dir(filepath.Dir(dir))), "go-build")
}

// DetectMode reports how this binary upgrades and why.
func DetectMode() Mode {
	m := Mode{Dev: IsDevBuild(), Version: buildinfo.Version, Revision: buildRevision(), Modified: buildModified()}
	switch {
	case !m.Dev:
		m.Reasons = []string{fmt.Sprintf("the binary's version %s is a release, so it upgrades from GitHub releases", m.Version)}
	case EmulationActive():
		m.Reasons = []string{"version emulation (AVIARY_EMULATE_VERSIONS) is on, so the release updater is in force"}
	case inDocker():
		m.Reasons = []string{"a dev build in Docker upgrades from GitHub releases"}
	default:
		det := locateCheckout()
		m.Checkout, m.Reasons = det.Dir, det.Reasons
	}
	return m
}

// Summary says in one line which mode upgrades use.
func (m Mode) Summary() string {
	if !m.Dev {
		if exe, err := executable(); err == nil {
			return fmt.Sprintf("Installed release %s at %s.", m.Version, exe)
		}
		return fmt.Sprintf("Installed release %s.", m.Version)
	}
	build := "a dev build"
	if m.Revision != "" {
		build += " of " + shortSHA(m.Revision)
		if m.Modified {
			build += " (with uncommitted changes)"
		}
	}
	switch {
	case m.Checkout != "":
		return fmt.Sprintf("Running %s from the source checkout at %s.", build, m.Checkout)
	case EmulationActive() || inDocker():
		return fmt.Sprintf("Running %s that upgrades from GitHub releases.", build)
	}
	return fmt.Sprintf("Running %s with no source checkout to update from.", build)
}

// BinaryStaleness compares the running binary's embedded revision with head,
// the checkout's HEAD commit, and says whether the binary runs older code.
// stale is true when the binary needs rebuilding to run head.
func BinaryStaleness(head string) (msg string, stale bool) {
	rev := buildRevision()
	switch {
	case head == "":
		return "", false
	case rev == "" && goRunBinary():
		return "", false
	case rev == "":
		return "This binary embeds no VCS revision, so it cannot be compared with the checkout's HEAD.", true
	case rev != head:
		return fmt.Sprintf("This binary was built from %s, but the checkout's HEAD is %s, so the running binary is stale.", shortSHA(rev), shortSHA(head)), true
	case buildModified():
		return fmt.Sprintf("This binary was built from the checkout's HEAD (%s) with uncommitted changes.", shortSHA(rev)), false
	}
	return fmt.Sprintf("This binary was built from the checkout's HEAD (%s).", shortSHA(rev)), false
}

// RebuildCommand is the command that rebuilds the running binary from the
// checkout at dir, so it runs the code the checkout now has.
func RebuildCommand(dir string) string {
	if goRunBinary() {
		return fmt.Sprintf("cd %s && go run ./cmd/aviary", dir)
	}
	exe, err := executable()
	if err != nil {
		exe = "aviary"
	}
	return fmt.Sprintf("cd %s && go build -o %s ./cmd/aviary", dir, exe)
}
