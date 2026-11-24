package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/codingskills"
	"github.com/lsegal/aviary/internal/saferestart"
	"github.com/lsegal/aviary/internal/server"
	"github.com/lsegal/aviary/internal/update"
)

var (
	upgradeVersion       string
	upgradeWhen          string
	upgradeSource        bool
	upgradeRelease       bool
	upgradeVerbose       bool
	doctorDisableVersion bool
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade Aviary to the latest release",
	Long: `Upgrade Aviary to the latest release, or to --version (with or without a
leading v). Does nothing when Aviary is already at that version. The first
line of output says what kind of install is being upgraded; --verbose also
says how that was decided.

The upgrade mode follows how this binary was built: a release build
downloads the latest GitHub release, and a dev build running from a source
checkout (the checkout holding the binary, or AVIARY_CHECKOUT) instead
fast-forwards the checkout with git pull, and only when it has no local
changes. It reports the local and upstream commits it compared after
fetching, says whether the running binary is older than the checkout's HEAD,
and prints the command that rebuilds it. The working directory never decides
the mode. --release and
--source force a mode; --source looks in the working directory when no other
checkout is known.

The upgrade always runs in this process, with this binary's upgrade code;
the running server is never asked to upgrade itself. When the running server
is the same kind of install, it is only restarted around the swap as a safe
restart: running jobs are stopped at a checkpoint and resumed in their own
sessions after the server comes back on the new binary, which aviary upgrade
confirms. --when idle instead holds new jobs and restarts once no jobs are
running. A dev server from the pulled checkout hot-reloads the pulled code
where it can and restarts only when it cannot. A server of another kind,
such as a dev server from a checkout while this is a release binary, is left
alone. The binary is upgraded even when the server does not respond, and
aviary upgrade then fails with a note that the server still runs the old
version.`,
	RunE: runUpgrade,
}

func init() {
	upgradeCmd.Flags().StringVar(&upgradeVersion, "version", "", "Upgrade to a specific release instead of the latest release")
	upgradeCmd.Flags().StringVar(&upgradeWhen, "when", saferestart.WhenNow, "When the running server restarts: now, or idle (once no jobs are running)")
	upgradeCmd.Flags().BoolVar(&upgradeRelease, "release", false, "Upgrade from GitHub releases, even on a dev build")
	upgradeCmd.Flags().BoolVar(&upgradeSource, "source", false, "Upgrade by pulling the source checkout, even on a release build")
	upgradeCmd.Flags().BoolVar(&upgradeVerbose, "verbose", false, "Explain how the install type was detected")
	upgradeCmd.MarkFlagsMutuallyExclusive("release", "source")
	rootCmd.AddCommand(upgradeCmd)
}

// upgradeMode returns the mode --release or --source forces, or "" to follow
// the detected install type.
func upgradeMode() update.InstallType {
	switch {
	case upgradeRelease:
		return update.InstallRelease
	case upgradeSource:
		return update.InstallSource
	}
	return ""
}

func runUpgrade(cmd *cobra.Command, _ []string) error {
	if cmd != nil {
		// Flags are already parsed, so failures from here on are runtime
		// errors and the usage block after them is noise.
		cmd.SilenceUsage = true
	}
	mode := upgradeMode()
	if mode == update.InstallSource && upgradeVersion != "" {
		return errors.New("--version picks a release, so it cannot be combined with --source")
	}
	fmt.Println(update.UpgradeSummary())
	if upgradeVerbose {
		for _, reason := range update.InstallReasons() {
			fmt.Printf("  - %s\n", reason)
		}
	}
	ctx, cancel := context.WithTimeout(update.WithCaller(context.Background(), "aviary upgrade"), 15*time.Second)
	defer cancel()

	check, err := update.CheckMode(ctx, &http.Client{Timeout: 15 * time.Second}, mode)
	if err != nil && check.LatestVersion == "" {
		return err
	}
	if check.InstallType == update.InstallSource {
		return runSourceUpgrade(check)
	}
	if check.NoCheckout && upgradeVersion == "" {
		return errors.New(check.Message)
	}
	return runReleaseUpgrade(check, mode == update.InstallRelease)
}

// runReleaseUpgrade downloads, verifies and installs the target release over
// this binary itself, then restarts the running server on it when the server
// is a release install too. The server never runs the upgrade.
func runReleaseUpgrade(check update.CheckResult, forced bool) error {
	current := update.DisplayVersion(check.CurrentVersion)
	targetVersion, upToDate := resolveUpgradeTarget(check, upgradeVersion, forced)
	if upToDate {
		fmt.Printf("Aviary %s is up to date.\n", current)
		return nil
	}
	if targetVersion == "" {
		return fmt.Errorf("unable to determine a target version")
	}
	fmt.Printf("Upgrading %s → %s\n", current, targetVersion)

	if update.EmulationActive() {
		fmt.Printf("Emulated upgrade to %s completed. No files were changed.\n", targetVersion)
		return nil
	}
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating executable: %w", err)
	}
	srv, running := findRunningServer()
	targets := []string{exePath}
	restart := running && srv.Problem == "" && srv.InstallType == update.InstallRelease
	if restart && srv.Executable != "" && !sameFile(srv.Executable, exePath) {
		// The server re-executes its own binary, so that is the one to swap.
		targets = append(targets, srv.Executable)
	}
	var installed string
	for _, target := range targets {
		if installed, err = installRelease(targetVersion, target); err != nil {
			return err
		}
	}
	fmt.Printf("Aviary upgraded to %s.\n", installed)
	skillsCtx, skillsCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer skillsCancel()
	if err := updateSkills(skillsCtx, installed); err != nil {
		fmt.Printf("Updating skills for %s: %v\n", installed, err)
	}
	switch {
	case !running:
		return nil
	case srv.Problem != "":
		return unmanagedServerError(installed, srv.Problem)
	case !restart:
		fmt.Println(otherServerNote(srv.CheckResult))
		return nil
	}
	return restartOnNewBinary(installed, func(st runningServer) bool {
		return update.SameVersion(st.CurrentVersion, installed)
	})
}

// installRelease downloads and verifies version and installs it at target,
// returning the installed version. It is a variable so tests can stub it.
var installRelease = func(version, target string) (string, error) {
	update.CleanupStaging(target)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := update.Install(ctx, update.InstallOptions{
		Version:    version,
		TargetPath: target,
		Client:     &http.Client{Timeout: 2 * time.Minute},
	})
	if err != nil {
		return "", err
	}
	return result.Version, nil
}

// updateSkills updates the skills Aviary ships to every signed-in CLI
// provider for version, as an upgrade always has. It is a variable so tests
// can stub it.
var updateSkills = func(ctx context.Context, version string) error {
	_, err := codingskills.Default().UpdateTo(ctx, version)
	return err
}

// runSourceUpgrade pulls the source checkout in this process. When the
// running server is a dev build from the same checkout, the pulled code goes
// live by forgo's hot reload where it can, and otherwise by restarting the
// server, which the dev supervisor rebuilds from the checkout.
func runSourceUpgrade(check update.CheckResult) error {
	dir := ""
	if check.Git != nil {
		dir = check.Git.Dir
	}
	if dir == "" {
		dir = update.SourceCheckout()
	}
	srv, running := findRunningServer()
	sameCheckout := running && srv.Problem == "" && srv.InstallType == update.InstallSource && srv.Git != nil && sameDir(srv.Git.Dir, dir)
	hotDir, since := "", 0
	if sameCheckout && srv.Git.HotReload {
		hotDir = srv.Git.HotReloadDir
		since = update.HotReloadGeneration(hotDir)
	}
	pullCtx, pullCancel := context.WithTimeout(update.WithCaller(context.Background(), "aviary upgrade"), 2*time.Minute)
	defer pullCancel()
	res, err := update.GitPull(pullCtx, dir)
	if err != nil {
		return fmt.Errorf("updating the source checkout at %s: %w", dir, err)
	}
	fmt.Println(update.GitCompareMessage(res))
	if sameCheckout {
		err = applyPulledCode(pullCtx, srv, res, hotDir, since)
	} else {
		fmt.Println(update.GitPullMessage(res, update.DevReloadPlan{GoChanged: true, RestartReason: "the server is not running"}))
	}
	if update.IsDevBuild() {
		if msg, stale := update.BinaryStaleness(res.Head); msg != "" {
			fmt.Println(msg)
			if stale {
				fmt.Printf("Rebuild it with: %s\n", update.RebuildCommand(dir))
			}
		}
		fmt.Println(releaseHint)
	}
	if err != nil {
		return err
	}
	switch {
	case !running || sameCheckout:
		return nil
	case srv.Problem != "":
		return fmt.Errorf("the running Aviary server %s, so it could not be restarted on the pulled code. %s", srv.Problem, restartHint)
	}
	fmt.Println(otherServerNote(srv.CheckResult))
	return nil
}

// applyPulledCode makes the running dev server, from the checkout just
// pulled, run the pulled code: hot reload applies Go changes in place, and a
// change it cannot apply restarts the server when the dev supervisor rebuilds
// it on restart.
func applyPulledCode(ctx context.Context, srv runningServer, res update.GitPullResult, hotDir string, since int) error {
	res.Supervised, res.HotReload = srv.Git.Supervised, srv.Git.HotReload
	var plan update.DevReloadPlan
	if res.Commits > 0 {
		var err error
		if plan, err = update.PlanDevReload(ctx, res.Dir, res.From, res.To, res.HotReload); err != nil {
			plan = update.DevReloadPlan{GoChanged: true, RestartReason: "listing the pulled changes failed: " + err.Error()}
		}
	}
	fmt.Println(update.GitPullMessage(res, plan))
	if res.Commits == 0 || !plan.GoChanged && plan.RestartReason == "" {
		return nil
	}
	if plan.RestartReason == "" {
		if hotDir == "" {
			return nil
		}
		result := waitHotReload(context.Background(), hotDir, since, update.DefaultHotReloadWait)
		if result.Applied {
			fmt.Println("The running Aviary server hot-reloaded the pulled code.")
			return nil
		}
		fmt.Printf("The running Aviary server could not hot-reload the pulled code: %s.\n", result.Reason)
		if !res.Supervised {
			return fmt.Errorf("the running Aviary server still runs the old code. %s", restartHint)
		}
	} else if !res.Supervised {
		return nil
	}
	return restartOnNewBinary(res.To, func(runningServer) bool { return true })
}

// waitHotReload waits for forgo's verdict on the reload a pull started. It is
// a variable so tests can stub it.
var waitHotReload = update.WaitHotReload

// restartHint tells the user how to restart a server aviary upgrade could not.
const restartHint = "Restart it to run the new version: aviary stop, then aviary serve"

// unmanagedServerError reports a binary upgraded under a server that could
// not be restarted for problem, which still runs the old version.
func unmanagedServerError(version, problem string) error {
	return fmt.Errorf("upgraded to %s, but the running Aviary server %s, so it could not be restarted and still runs the old version. %s", version, problem, restartHint)
}

// restartOnNewBinary has the running server restart safely, checkpointing
// running jobs, so it comes back up on the new binary or code, which target
// names. With --when idle the server restarts later, once no jobs are
// running. Otherwise it waits for the server to come back from the restart
// and confirms with current that it runs the new version.
func restartOnNewBinary(target string, current func(runningServer) bool) error {
	if err := restartServer(upgradeWhen); err != nil {
		return fmt.Errorf("restarting the running Aviary server on %s: %w. %s", target, err, restartHint)
	}
	if upgradeWhen == saferestart.WhenIdle {
		fmt.Printf("The running Aviary server restarts on %s once no jobs are running. New jobs are held until then.\n", target)
		return nil
	}
	fmt.Printf("Restarting the running Aviary server on %s. Running jobs resume after the restart.\n", target)
	if err := awaitServer(current); err != nil {
		return fmt.Errorf("upgraded to %s, but %w. %s", target, err, restartHint)
	}
	fmt.Printf("The Aviary server is running %s.\n", target)
	return nil
}

// serverRestartWait bounds how long aviary upgrade waits for the server to
// come back. A restart first waits for in-flight MCP calls (up to
// server.restart_call_max_wait) and checkpoints running jobs.
var serverRestartWait = 10 * time.Minute

// serverRestartPoll is how often aviary upgrade asks whether the server is
// back.
var serverRestartPoll = time.Second

// awaitServer waits up to serverRestartWait for the server to come back
// from the restart it was asked for, in a state current accepts. A server
// that answers with no restart pending is the restarted one, since the
// request marks the old one as restarting before it returns.
func awaitServer(current func(runningServer) bool) error {
	deadline := time.Now().Add(serverRestartWait)
	for {
		if srv, ok := findRunningServer(); ok && srv.Problem == "" {
			switch {
			case srv.Restart.Phase != "" && srv.Restart.Phase != saferestart.PhaseIdle:
			case srv.Restart.Error != "":
				return fmt.Errorf("the running Aviary server failed to restart: %s", srv.Restart.Error)
			case current(srv):
				return nil
			default:
				return errors.New("the running Aviary server came back on another version")
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the running Aviary server did not come back within %s", serverRestartWait)
		}
		time.Sleep(serverRestartPoll)
	}
}

// releaseHint offers a dev build the switch to a release binary.
const releaseHint = "To install the latest release binary instead, run: aviary upgrade --release"

// runningServer is what the running server reports about itself.
type runningServer struct {
	update.CheckResult
	// Executable is the binary the server runs and re-executes on restart.
	Executable string `json:"executable,omitempty"`
	// Restart is the server's restart state.
	Restart saferestart.Status `json:"restart"`
	// Problem, when set, says why the running server cannot be restarted:
	// it did not respond, or could not report its install.
	Problem string `json:"-"`
}

// findRunningServer reports the running server's install. ok is false when
// no server is running. It is a variable so tests can stub it.
var findRunningServer = func() (srv runningServer, ok bool) {
	if running, _, _ := server.IsRunning(); !running {
		return runningServer{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := dispatcher.CallTool(ctx, "server_version_check", map[string]any{})
	if err != nil {
		return runningServer{Problem: "did not respond"}, true
	}
	if json.Unmarshal([]byte(out), &srv) != nil {
		// A tool error comes back as its message.
		return runningServer{Problem: "could not report its install (" + strings.TrimSpace(out) + ")"}, true
	}
	return srv, true
}

// restartServer asks the running server for a safe restart with when. It is a
// variable so tests can stub it.
var restartServer = func(when string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := dispatcher.CallTool(ctx, "server_restart", map[string]any{"when": when})
	if err != nil {
		return err
	}
	// The restart status comes back as JSON, and a tool error as its message.
	var st saferestart.Status
	if json.Unmarshal([]byte(out), &st) != nil {
		return errors.New(strings.TrimSpace(out))
	}
	return nil
}

// otherServerNote explains that the running server, a different kind of
// install than the one just upgraded, was left alone.
func otherServerNote(srv update.CheckResult) string {
	switch srv.InstallType {
	case update.InstallRelease:
		return fmt.Sprintf("The running Aviary server is a release install (%s), so it was not changed. Run aviary upgrade --release to upgrade it.", update.DisplayVersion(srv.CurrentVersion))
	case update.InstallSource:
		dir := "its checkout"
		if srv.Git != nil {
			dir = "the source checkout at " + srv.Git.Dir
		}
		return fmt.Sprintf("The running Aviary server is a dev build from %s, so it was not changed. Run aviary upgrade --source from that checkout to update it.", dir)
	}
	return "The running Aviary server is a different kind of install, so it was not changed."
}

func sameDir(a, b string) bool {
	return a != "" && b != "" && filepath.Clean(a) == filepath.Clean(b)
}

// sameFile reports whether paths a and b name the same file, following
// symlinks.
func sameFile(a, b string) bool {
	if sameDir(a, b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && filepath.Clean(ra) == filepath.Clean(rb)
}

// resolveUpgradeTarget picks the release to install. upToDate is true when
// there is nothing to do: no newer release, or the requested version is the
// one already running. forced (--release) installs the latest release over a
// build that is not a release itself.
func resolveUpgradeTarget(check update.CheckResult, requested string, forced bool) (target string, upToDate bool) {
	target = update.NormalizeVersion(requested)
	if target == "" {
		if forced && check.LatestVersion != "" && !update.IsReleaseVersion(check.CurrentVersion) {
			return update.NormalizeVersion(check.LatestVersion), false
		}
		if !check.UpgradeAvailable {
			return "", check.LatestVersion != ""
		}
		target = update.NormalizeVersion(check.LatestVersion)
	}
	if update.SameVersion(check.CurrentVersion, target) {
		return target, true
	}
	return target, false
}

func maybeRunDoctorVersionCheck() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	check, err := update.Check(ctx, &http.Client{Timeout: 10 * time.Second})
	if err != nil && check.LatestVersion == "" {
		fmt.Printf("\nVersion check: [WARN] %v\n", err)
		return nil
	}
	if check.LatestVersion == "" {
		return nil
	}
	if !check.UpgradeSupported {
		fmt.Printf("\nVersion check: [INFO] %s\n", update.CheckMessage(check))
		return nil
	}
	if !check.UpgradeAvailable {
		fmt.Printf("\nVersion check: [OK] %s\n", check.CurrentVersion)
		return nil
	}

	fmt.Printf("\nVersion check: [WARN] upgrade available %s -> %s\n", check.CurrentVersion, check.LatestVersion)
	answer, err := promptYesNo(fmt.Sprintf("Upgrade to %s now? [y/N]: ", check.LatestVersion))
	if err != nil {
		return err
	}
	if !answer {
		return nil
	}
	return runUpgrade(nil, nil)
}

func promptYesNo(prompt string) (bool, error) {
	fmt.Print(prompt)
	line, err := readConsoleLine()
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
