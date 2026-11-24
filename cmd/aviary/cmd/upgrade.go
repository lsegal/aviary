package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
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
	upgradeRelease       bool
	upgradeVerbose       bool
	doctorDisableVersion bool
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade Aviary to the latest release",
	Long: `Upgrade Aviary to the latest release, or to --version (with or without a
leading v). Does nothing when Aviary is already at that version. The first line
of output says which kind of upgrade runs; --verbose also says why.

A dev build built from an Aviary source checkout instead fetches the checkout's
upstream branch, reports the local and upstream commits, and fast-forwards the
checkout with git pull, only when it has no local changes. Hot reload picks up
the pulled sources, so nothing restarts; otherwise the output says whether the
running binary is stale and gives the command that rebuilds it. A binary counts
as built from a checkout when AVIARY_CHECKOUT names it, or when the working
directory's checkout contains the commit the binary was built from (or the
binary was built there by go run). --release, or --version, installs a release
binary instead.

When the server is running, the upgrade is a safe restart: running jobs are
stopped at a checkpoint and resumed in their own sessions after the restart.
--when idle instead holds new jobs and upgrades once no jobs are running.`,
	RunE: runUpgrade,
}

func init() {
	upgradeCmd.Flags().StringVar(&upgradeVersion, "version", "", "Upgrade to a specific release instead of the latest release")
	upgradeCmd.Flags().StringVar(&upgradeWhen, "when", saferestart.WhenNow, "When the running server restarts: now, or idle (once no jobs are running)")
	upgradeCmd.Flags().BoolVar(&upgradeRelease, "release", false, "Install the latest release binary even on a dev build from a source checkout")
	upgradeCmd.Flags().BoolVar(&upgradeVerbose, "verbose", false, "Explain how the upgrade mode was chosen")
	rootCmd.AddCommand(upgradeCmd)
}

func runUpgrade(cmd *cobra.Command, _ []string) error {
	if cmd != nil {
		// Flags are already parsed, so failures from here on are runtime
		// errors and the usage block after them is noise.
		cmd.SilenceUsage = true
	}
	mode := update.DetectMode()
	fmt.Println(mode.Summary())
	if upgradeVerbose {
		for _, reason := range mode.Reasons {
			fmt.Printf("  - %s\n", reason)
		}
	}
	if mode.Dev && (upgradeRelease || upgradeVersion != "") {
		return runReleaseUpgrade(cmd)
	}

	ctx, cancel := context.WithTimeout(update.WithCaller(context.Background(), "aviary upgrade"), 15*time.Second)
	defer cancel()

	check, err := update.Check(ctx, &http.Client{Timeout: 15 * time.Second})
	if err != nil && check.LatestVersion == "" {
		return err
	}
	if check.NoCheckout {
		return errors.New(check.Message)
	}
	if mode.Checkout != "" && check.Git != nil {
		return runGitUpgrade(cmd, mode.Checkout)
	}
	targetVersion, upToDate := resolveUpgradeTarget(check, upgradeVersion)
	if upToDate {
		fmt.Printf("Aviary is already up to date (%s).\n", check.CurrentVersion)
		return nil
	}
	if targetVersion == "" {
		return fmt.Errorf("unable to determine a target version")
	}
	return installUpgrade(cmd, targetVersion, false)
}

// runReleaseUpgrade installs a release binary over a dev build: --version,
// else the latest release, looked up through the releases/latest redirect.
func runReleaseUpgrade(cmd *cobra.Command) error {
	target := update.NormalizeVersion(upgradeVersion)
	if target == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		latest, err := update.LatestRelease(ctx, &http.Client{Timeout: 15 * time.Second})
		if err != nil {
			return err
		}
		target = update.NormalizeVersion(latest)
		fmt.Printf("The latest release is %s.\n", target)
	}
	return installUpgrade(cmd, target, true)
}

// runGitUpgrade pulls the dev checkout at dir, through the running server
// when there is one, and reports what it compared and pulled and whether the
// running binary needs rebuilding.
func runGitUpgrade(cmd *cobra.Command, dir string) error {
	if running, _, _ := server.IsRunning(); running {
		out, err := dispatcher.CallTool(commandContext(cmd), "server_upgrade", map[string]any{"when": upgradeWhen})
		if err != nil {
			return err
		}
		var st saferestart.Status
		if json.Unmarshal([]byte(out), &st) == nil && st.Message != "" {
			fmt.Println(st.Message)
		}
		fmt.Println(releaseHint)
		return nil
	}
	pullCtx, pullCancel := context.WithTimeout(update.WithCaller(context.Background(), "aviary upgrade"), 2*time.Minute)
	defer pullCancel()
	res, err := update.GitPull(pullCtx, dir)
	if err != nil {
		return err
	}
	fmt.Println(update.GitCompareMessage(res))
	fmt.Println(update.GitPullMessage(res, update.DevReloadPlan{GoChanged: true, RestartReason: "the server is not running"}))
	if msg, stale := update.BinaryStaleness(res.Head); msg != "" {
		fmt.Println(msg)
		if stale {
			fmt.Printf("Rebuild it with: %s\n", update.RebuildCommand(dir))
		}
	}
	fmt.Println(releaseHint)
	return nil
}

const releaseHint = "To switch to the latest release binary instead, run: aviary upgrade --release"

// installUpgrade installs release target, through the running server's safe
// restart when there is one. release asks the server to install it even when
// it is a dev build that would otherwise pull its checkout.
func installUpgrade(cmd *cobra.Command, target string, release bool) error {
	if running, _, _ := server.IsRunning(); running {
		args := map[string]any{"version": target, "when": upgradeWhen}
		if release {
			args["release"] = true
		}
		out, err := dispatcher.CallTool(commandContext(cmd), "server_upgrade", args)
		if err != nil {
			return err
		}
		var st saferestart.Status
		if json.Unmarshal([]byte(out), &st) == nil && st.Message != "" {
			fmt.Println(st.Message)
			return nil
		}
		if upgradeWhen == saferestart.WhenIdle {
			fmt.Printf("Aviary will upgrade to %s once no jobs are running. New jobs are held until then.\n", target)
		} else {
			fmt.Printf("Upgrading the running Aviary server to %s. Running jobs resume after the restart.\n", target)
		}
		return nil
	}

	if update.EmulationActive() {
		fmt.Printf("Emulated upgrade to %s completed. No files were changed.\n", target)
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
		Version:    target,
		TargetPath: exePath,
		Client:     &http.Client{Timeout: 2 * time.Minute},
	})
	if err != nil {
		return err
	}
	fmt.Printf("Aviary upgraded to %s at %s.\n", result.Version, exePath)
	return nil
}

func commandContext(cmd *cobra.Command) context.Context {
	if cmd != nil && cmd.Context() != nil {
		return cmd.Context()
	}
	return context.Background()
}

// resolveUpgradeTarget picks the release to install. upToDate is true when
// there is nothing to do: no newer release, or the requested version is the
// one already running.
func resolveUpgradeTarget(check update.CheckResult, requested string) (target string, upToDate bool) {
	target = update.NormalizeVersion(requested)
	if target == "" {
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
