package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// HotReloadEnv is set by scripts/dev-go.mjs when the backend runs under
// forgo's hot reload, so a pulled checkout is rebuilt and relaunched on its own.
const HotReloadEnv = "AVIARY_HOT_RELOAD"

// CheckoutEnv is set by scripts/dev-go.mjs to the root of the source checkout
// the backend was built from. It is authoritative for git mode: the process's
// working directory is only a fallback, since a daemonized or restarted
// backend need not run inside the checkout.
const CheckoutEnv = "AVIARY_CHECKOUT"

// aviaryModule is the module path a directory's go.mod must declare to be
// treated as an Aviary checkout.
const aviaryModule = "github.com/lsegal/aviary"

// GitState describes a dev checkout's position relative to its upstream.
type GitState struct {
	Dir      string `json:"dir"`
	Branch   string `json:"branch"`
	Upstream string `json:"upstream,omitempty"`
	Behind   int    `json:"behind"`
	Ahead    int    `json:"ahead"`
	Dirty    bool   `json:"dirty"`
	// HotReload is true when the backend runs under hot reload, so pulled
	// sources go live without a restart.
	HotReload bool `json:"hotReload"`
}

// GitPullResult describes a git-mode update.
type GitPullResult struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Commits   int    `json:"commits"`
	HotReload bool   `json:"hotReload"`
}

var (
	inDocker = func() bool {
		_, err := os.Stat("/.dockerenv")
		return err == nil
	}
	checkoutDir = func() string {
		if dir := os.Getenv(CheckoutEnv); dir != "" {
			return verifyCheckout(dir)
		}
		wd, err := os.Getwd()
		if err != nil {
			return ""
		}
		return verifyCheckout(wd)
	}
)

// verifyCheckout returns the top level of the git checkout containing dir
// when it is an Aviary checkout, or "" otherwise.
func verifyCheckout(dir string) string {
	out, err := runGit(context.Background(), dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	top := filepath.Clean(out)
	mod, err := os.ReadFile(filepath.Join(top, "go.mod"))
	if err != nil || modulePath(mod) != aviaryModule {
		return ""
	}
	return top
}

// modulePath returns the module path declared by a go.mod file, tolerating
// CRLF line endings, surrounding whitespace, comments, and quoting.
func modulePath(mod []byte) string {
	for _, line := range strings.Split(string(mod), "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return strings.Trim(fields[1], "\"`")
		}
	}
	return ""
}

// gitModeEligible reports whether this build would update from a source
// checkout: a dev build outside Docker with version emulation off.
func gitModeEligible() bool {
	return IsDevBuild() && !EmulationActive() && !inDocker()
}

// GitCheckout returns the source checkout the updater works on in git mode: a
// dev build running from an Aviary git checkout outside Docker, with version
// emulation off. It returns "" when the release updater is in force.
func GitCheckout() string {
	if !gitModeEligible() {
		return ""
	}
	return checkoutDir()
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func gitCount(ctx context.Context, dir, rng string) (int, error) {
	out, err := runGit(ctx, dir, "rev-list", "--count", rng)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(out)
}

// ReadGitState fetches the checkout's upstream and reports how far behind it
// is and whether the working tree has local changes.
func ReadGitState(ctx context.Context, dir string) (GitState, error) {
	st := GitState{Dir: dir, HotReload: os.Getenv(HotReloadEnv) != ""}
	var err error
	if st.Branch, err = runGit(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD"); err != nil {
		return st, err
	}
	status, err := runGit(ctx, dir, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return st, err
	}
	st.Dirty = status != ""
	st.Upstream, err = runGit(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		st.Upstream = ""
		return st, nil
	}
	if _, err := runGit(ctx, dir, "fetch", "--quiet"); err != nil {
		return st, err
	}
	if st.Behind, err = gitCount(ctx, dir, "HEAD..@{upstream}"); err != nil {
		return st, err
	}
	if st.Ahead, err = gitCount(ctx, dir, "@{upstream}..HEAD"); err != nil {
		return st, err
	}
	return st, nil
}

// gitBlocker explains why the checkout cannot be updated, or "" when a
// fast-forward pull is safe.
func gitBlocker(st GitState) string {
	switch {
	case st.Upstream == "":
		return fmt.Sprintf("branch %s has no upstream to pull from", st.Branch)
	case st.Dirty:
		return fmt.Sprintf("the checkout at %s has uncommitted changes", st.Dir)
	case st.Ahead > 0 && st.Behind > 0:
		return fmt.Sprintf("branch %s has diverged from %s (%d local, %d upstream commits), so the pull would not fast-forward", st.Branch, st.Upstream, st.Ahead, st.Behind)
	}
	return ""
}

func gitCheckResult(ctx context.Context, dir string) (CheckResult, error) {
	st, err := ReadGitState(ctx, dir)
	out := CheckResult{CurrentVersion: CurrentVersion(), CheckedAt: time.Now().UTC(), Git: &st}
	if err != nil {
		out.Message = err.Error()
		return out, err
	}
	if st.Upstream != "" {
		out.LatestVersion, _ = runGit(ctx, dir, "rev-parse", "--short", "@{upstream}")
	}
	out.UpgradeAvailable = st.Behind > 0
	blocker := gitBlocker(st)
	out.UpgradeSupported = blocker == ""
	switch {
	case blocker != "":
		out.Message = "Updating is blocked: " + blocker + "."
	case st.Behind > 0:
		out.Message = fmt.Sprintf("%s is %d commit(s) ahead of this checkout.", st.Upstream, st.Behind)
	}
	return out, nil
}

// GitPull fast-forwards the checkout to its upstream. It refuses to run when
// the tree has local changes or the pull would not fast-forward.
func GitPull(ctx context.Context, dir string) (GitPullResult, error) {
	st, err := ReadGitState(ctx, dir)
	if err != nil {
		return GitPullResult{}, err
	}
	if blocker := gitBlocker(st); blocker != "" {
		return GitPullResult{}, fmt.Errorf("not updating: %s", blocker)
	}
	res := GitPullResult{Commits: st.Behind, HotReload: st.HotReload}
	res.From, _ = runGit(ctx, dir, "rev-parse", "--short", "HEAD")
	if st.Behind > 0 {
		if _, err := runGit(ctx, dir, "pull", "--ff-only", "--quiet"); err != nil {
			return GitPullResult{}, err
		}
	}
	res.To, _ = runGit(ctx, dir, "rev-parse", "--short", "HEAD")
	ClearCache()
	return res, nil
}

// GitPullMessage summarizes a git-mode update for the user.
func GitPullMessage(res GitPullResult) string {
	if res.Commits == 0 {
		return fmt.Sprintf("The checkout is already up to date (%s).", res.To)
	}
	msg := fmt.Sprintf("Pulled %d commit(s) (%s → %s).", res.Commits, res.From, res.To)
	if res.HotReload {
		return msg + " Hot reload is rebuilding Aviary with the new code."
	}
	return msg + " Restart Aviary to run the new code."
}
