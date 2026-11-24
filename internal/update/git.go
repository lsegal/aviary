package update

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// RestartFileEnv is set by scripts/dev-go.mjs to a file the backend creates
// before exiting to ask for a restart. The script then runs the backend again,
// rebuilding it from the checkout's current sources. A dev update restarts only
// when forgo cannot hot-reload the pulled code (see PlanDevReload).
const RestartFileEnv = "AVIARY_RESTART_FILE"

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
	// Supervised is true when a restart rebuilds the backend from the
	// checkout (see RestartFileEnv), so pulled sources go live by restarting.
	Supervised bool `json:"supervised"`
	// HotReload is true when the backend runs under forgo's hot reload, so
	// pulled Go changes go live without a restart where forgo can apply them.
	HotReload bool `json:"hotReload"`
}

// GitPullResult describes a git-mode update.
type GitPullResult struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Commits    int    `json:"commits"`
	Supervised bool   `json:"supervised"`
	HotReload  bool   `json:"hotReload"`
}

var (
	inDocker = func() bool {
		_, err := os.Stat("/.dockerenv")
		return err == nil
	}
	// checkoutDir returns the checkout git mode works on. It is a variable
	// so tests can stub it.
	checkoutDir = cachedCheckout
)

var (
	checkoutMu    sync.Mutex
	checkoutFound bool
	checkoutTop   string
)

// cachedCheckout returns findCheckout's answer, computed once per process: a
// running backend never moves to another checkout, so request paths never
// run git to find it again.
func cachedCheckout() string {
	checkoutMu.Lock()
	defer checkoutMu.Unlock()
	if !checkoutFound {
		checkoutTop, checkoutFound = findCheckout(), true
	}
	return checkoutTop
}

// findCheckout locates the Aviary checkout from CheckoutEnv, else the working
// directory.
func findCheckout() string {
	dir := os.Getenv(CheckoutEnv)
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return ""
		}
		dir = wd
	}
	return verifyCheckout(dir)
}

// verifyCheckout returns the top level of the git checkout containing dir
// when it is an Aviary checkout, or "" otherwise.
func verifyCheckout(dir string) string {
	out, err := runGit(WithCaller(context.Background(), "checkout detection"), dir, "rev-parse", "--show-toplevel")
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

// headTTL is how long checkoutHead reuses a HEAD it read. A pull or an update
// check refreshes it right away (see ClearCache and readGitCheckResult); the
// TTL only bounds how long a commit made outside Aviary goes unreported.
var headTTL = time.Minute

var (
	headMu     sync.Mutex
	headDir    string
	headSHA    string
	headReadAt time.Time
)

// checkoutHead returns the commit checked out in dir, reading it with git at
// most once per headTTL. Concurrent callers wait for one read instead of
// each running git.
func checkoutHead(ctx context.Context, dir string) string {
	headMu.Lock()
	defer headMu.Unlock()
	if dir == headDir && time.Since(headReadAt) < headTTL {
		return headSHA
	}
	return readHeadLocked(ctx, dir)
}

// refreshHead reads the commit checked out in dir now, updating the cache.
func refreshHead(ctx context.Context, dir string) string {
	headMu.Lock()
	defer headMu.Unlock()
	return readHeadLocked(ctx, dir)
}

// readHeadLocked reads dir's HEAD into the cache, holding headMu. A failed
// read keeps the last known commit and is not retried until headTTL passes,
// and a changed HEAD is logged.
func readHeadLocked(ctx context.Context, dir string) string {
	sha, err := runGit(ctx, dir, "rev-parse", "HEAD")
	headReadAt = time.Now()
	if dir != headDir {
		headDir, headSHA = dir, ""
	}
	if err != nil || sha == "" {
		return headSHA
	}
	if headSHA != "" && sha != headSHA {
		slog.Info("update: checkout HEAD changed", "component", "update", "caller", callerOf(ctx),
			"dir", dir, "from", shortSHA(headSHA), "to", shortSHA(sha))
	}
	headSHA = sha
	return sha
}

// expireHead makes the next checkoutHead read HEAD again.
func expireHead() {
	headMu.Lock()
	headReadAt = time.Time{}
	headMu.Unlock()
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
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

// Supervised reports whether a restart rebuilds the backend from its checkout.
func Supervised() bool {
	return os.Getenv(RestartFileEnv) != ""
}

// RequestRebuild asks scripts/dev-go.mjs to run the backend again from the
// checkout's sources once this process exits. It reports false when the
// backend is not supervised or the request could not be written, in which
// case the caller re-executes its own binary instead.
func RequestRebuild() bool {
	path := os.Getenv(RestartFileEnv)
	if path == "" {
		return false
	}
	return os.WriteFile(path, nil, 0o600) == nil
}

// gitRuns counts the git commands the updater has started, so tests can
// check that request paths do not run git.
var gitRuns atomic.Int64

// gitWaitDelay is how long a cancelled git command has to exit after its
// interrupt before it is killed.
const gitWaitDelay = time.Minute

// gitWriteTimeout bounds a fetch or pull, which runs to completion even when
// its caller gives up so it never leaves a lock behind.
const gitWriteTimeout = 10 * time.Minute

// runGit runs a git command for the updater. GIT_OPTIONAL_LOCKS=0 keeps
// read-only commands such as git status from refreshing the index, so
// polling never takes .git/index.lock. A cancelled context interrupts git,
// which removes its lock files as it exits, instead of killing it.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	gitRuns.Add(1)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = gitWaitDelay
	start := time.Now()
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Successful commands are not logged: version lookups would flood
		// the log. Some failures are expected, such as a branch without an
		// upstream, so they stay at debug level.
		slog.Debug("update: git failed", "component", "update", "caller", callerOf(ctx), "dir", dir,
			"args", strings.Join(args, " "), "duration", time.Since(start), "error", err)
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

// runGitWrite runs a fetch or pull: it holds repository locks, so it runs to
// completion, within gitWriteTimeout, even when ctx is cancelled, and it
// retries while another git command holds a lock.
func runGitWrite(ctx context.Context, dir string, args ...string) (string, error) {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gitWriteTimeout)
	defer cancel()
	slog.Info("update: running git", "component", "update", "caller", callerOf(ctx), "dir", dir, "args", strings.Join(args, " "))
	return runGitRetrying(wctx, dir, args...)
}

// ReadGitState fetches the checkout's upstream and reports how far behind it
// is and whether the working tree has local changes. It waits for any other
// updater's git work on the checkout, in this or another process, to finish.
func ReadGitState(ctx context.Context, dir string) (GitState, error) {
	var st GitState
	err := withUpdaterLock(ctx, dir, func() (err error) {
		st, err = readGitState(ctx, dir)
		return err
	})
	return st, err
}

func readGitState(ctx context.Context, dir string) (GitState, error) {
	st := GitState{Dir: dir, Supervised: Supervised(), HotReload: HotReloadDir() != ""}
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
	if _, err := runGitWrite(ctx, dir, "fetch", "--quiet"); err != nil {
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

func gitCheckResult(ctx context.Context, dir string) (out CheckResult, err error) {
	lockErr := withUpdaterLock(ctx, dir, func() error {
		out, err = readGitCheckResult(ctx, dir)
		return nil
	})
	if lockErr != nil {
		out = CheckResult{CurrentVersion: CurrentVersion(ctx), CheckedAt: time.Now().UTC(), Message: lockErr.Error()}
		return out, lockErr
	}
	return out, err
}

func readGitCheckResult(ctx context.Context, dir string) (CheckResult, error) {
	st, err := readGitState(ctx, dir)
	refreshHead(ctx, dir)
	out := CheckResult{CurrentVersion: CurrentVersion(ctx), CheckedAt: time.Now().UTC(), Git: &st}
	if err != nil {
		out.Message = err.Error()
		return out, err
	}
	if st.Upstream != "" {
		out.LatestVersion, _ = runGit(ctx, dir, "rev-parse", "@{upstream}")
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
// the tree has local changes or the pull would not fast-forward. Concurrent
// pulls on one checkout, from this or other processes, run one at a time, so
// a pull that waited finds the checkout already current.
func GitPull(ctx context.Context, dir string) (res GitPullResult, err error) {
	err = withUpdaterLock(ctx, dir, func() error {
		res, err = gitPull(ctx, dir)
		return err
	})
	return res, err
}

func gitPull(ctx context.Context, dir string) (GitPullResult, error) {
	st, err := readGitState(ctx, dir)
	if err != nil {
		return GitPullResult{}, err
	}
	if blocker := gitBlocker(st); blocker != "" {
		return GitPullResult{}, fmt.Errorf("not updating: %s", blocker)
	}
	res := GitPullResult{Commits: st.Behind, Supervised: st.Supervised, HotReload: st.HotReload}
	res.From, _ = runGit(ctx, dir, "rev-parse", "--short", "HEAD")
	if st.Behind > 0 {
		if _, err := runGitWrite(ctx, dir, "pull", "--ff-only", "--quiet"); err != nil {
			return GitPullResult{}, err
		}
	}
	res.To, _ = runGit(ctx, dir, "rev-parse", "--short", "HEAD")
	ClearCache()
	return res, nil
}

// GitPullMessage summarizes a git-mode update for the user, given how the
// pulled code goes live.
func GitPullMessage(res GitPullResult, plan DevReloadPlan) string {
	if res.Commits == 0 {
		return fmt.Sprintf("The checkout is already up to date (%s).", res.To)
	}
	msg := fmt.Sprintf("Pulled %d commit(s) (%s → %s).", res.Commits, res.From, res.To)
	switch {
	case plan.RestartReason == "" && plan.GoChanged:
		return msg + " The backend hot-reloads the new code, and restarts only if forgo cannot apply it."
	case plan.RestartReason == "":
		return msg + " No backend code changed."
	case res.Supervised:
		return msg + " Aviary restarts to build and run the new code (" + plan.RestartReason + ")."
	}
	return msg + " Restart Aviary to run the new code (" + plan.RestartReason + ")."
}
