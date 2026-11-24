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

When the running server is the same kind of install, the upgrade goes
through it as a safe restart: running jobs are stopped at a checkpoint and
resumed in their own sessions after the restart. --when idle instead holds
new jobs and upgrades once no jobs are running. A server of another kind,
such as a dev server from a checkout while this is a release binary, is left
alone.`,
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

// runReleaseUpgrade installs the target release over this binary, or through
// the running server when it is a release install too.
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

	srv, running := runningServerInstall()
	if running && srv.InstallType == update.InstallRelease {
		return upgradeThroughServer(map[string]any{"version": targetVersion, "when": upgradeWhen}, func() {
			if upgradeWhen == saferestart.WhenIdle {
				fmt.Printf("Aviary will upgrade to %s once no jobs are running. New jobs are held until then.\n", targetVersion)
			} else {
				fmt.Printf("Upgrading the running Aviary server to %s. Running jobs resume after the restart.\n", targetVersion)
			}
		})
	}

	if update.EmulationActive() {
		fmt.Printf("Emulated upgrade to %s completed. No files were changed.\n", targetVersion)
		return nil
	}
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating executable: %w", err)
	}
	update.CleanupStaging(exePath)
	installCtx, installCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer installCancel()
	result, err := update.Install(installCtx, update.InstallOptions{
		Version:    targetVersion,
		TargetPath: exePath,
		Client:     &http.Client{Timeout: 2 * time.Minute},
	})
	if err != nil {
		return err
	}
	fmt.Printf("Aviary upgraded to %s.\n", result.Version)
	if running {
		fmt.Println(otherServerNote(srv))
	}
	return nil
}

// runSourceUpgrade pulls the source checkout, or has the running server pull
// it when the server runs from the same checkout.
func runSourceUpgrade(check update.CheckResult) error {
	dir := ""
	if check.Git != nil {
		dir = check.Git.Dir
	}
	if dir == "" {
		dir = update.SourceCheckout()
	}
	srv, running := runningServerInstall()
	if running && srv.InstallType == update.InstallSource && srv.Git != nil && sameDir(srv.Git.Dir, dir) {
		err := upgradeThroughServer(map[string]any{"when": upgradeWhen}, func() {
			fmt.Printf("Updating the running Aviary server from the source checkout at %s.\n", dir)
		})
		if err == nil && update.IsDevBuild() {
			fmt.Println(releaseHint)
		}
		return err
	}
	pullCtx, pullCancel := context.WithTimeout(update.WithCaller(context.Background(), "aviary upgrade"), 2*time.Minute)
	defer pullCancel()
	res, err := update.GitPull(pullCtx, dir)
	if err != nil {
		return fmt.Errorf("updating the source checkout at %s: %w", dir, err)
	}
	fmt.Println(update.GitCompareMessage(res))
	fmt.Println(update.GitPullMessage(res, update.DevReloadPlan{GoChanged: true, RestartReason: "the server is not running"}))
	if update.IsDevBuild() {
		if msg, stale := update.BinaryStaleness(res.Head); msg != "" {
			fmt.Println(msg)
			if stale {
				fmt.Printf("Rebuild it with: %s\n", update.RebuildCommand(dir))
			}
		}
		fmt.Println(releaseHint)
	}
	if running {
		fmt.Println(otherServerNote(srv))
	}
	return nil
}

// releaseHint offers a dev build the switch to a release binary.
const releaseHint = "To install the latest release binary instead, run: aviary upgrade --release"

// runningServerInstall reports the running server's version check, which
// says what kind of install it is. ok is false when no server is running or
// it cannot be asked. It is a variable so tests can stub it.
var runningServerInstall = func() (update.CheckResult, bool) {
	if running, _, _ := server.IsRunning(); !running {
		return update.CheckResult{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := dispatcher.CallTool(ctx, "server_version_check", map[string]any{})
	var check update.CheckResult
	if err != nil || json.Unmarshal([]byte(out), &check) != nil {
		return update.CheckResult{}, false
	}
	return check, true
}

// upgradeThroughServer runs server_upgrade with args and prints its message,
// or fallback's when it has none. It is a variable so tests can stub it.
var upgradeThroughServer = func(args map[string]any, fallback func()) error {
	out, err := dispatcher.CallTool(context.Background(), "server_upgrade", args)
	if err != nil {
		return err
	}
	var st saferestart.Status
	if json.Unmarshal([]byte(out), &st) == nil && st.Message != "" {
		fmt.Println(st.Message)
		return nil
	}
	fallback()
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
