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
	doctorDisableVersion bool
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade Aviary to the latest release",
	Long: `Upgrade Aviary to the latest release, or to --version (with or without a
leading v). Does nothing when Aviary is already at that version.

A dev build running from a source checkout instead fast-forwards the checkout
with git pull, and only when it has no local changes. Hot reload picks up the
pulled sources, so nothing restarts.

When the server is running, the upgrade is a safe restart: running jobs are
stopped at a checkpoint and resumed in their own sessions after the restart.
--when idle instead holds new jobs and upgrades once no jobs are running.`,
	RunE: runUpgrade,
}

func init() {
	upgradeCmd.Flags().StringVar(&upgradeVersion, "version", "", "Upgrade to a specific release instead of the latest release")
	upgradeCmd.Flags().StringVar(&upgradeWhen, "when", saferestart.WhenNow, "When the running server restarts: now, or idle (once no jobs are running)")
	rootCmd.AddCommand(upgradeCmd)
}

func runUpgrade(cmd *cobra.Command, _ []string) error {
	if cmd != nil {
		// Flags are already parsed, so failures from here on are runtime
		// errors and the usage block after them is noise.
		cmd.SilenceUsage = true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	check, err := update.Check(ctx, &http.Client{Timeout: 15 * time.Second})
	if err != nil && check.LatestVersion == "" {
		return err
	}
	if check.NoCheckout && upgradeVersion == "" {
		return errors.New(check.Message)
	}
	targetVersion, upToDate := resolveUpgradeTarget(check, upgradeVersion)
	if upToDate {
		fmt.Printf("Aviary is already up to date (%s).\n", check.CurrentVersion)
		return nil
	}
	if targetVersion == "" {
		return fmt.Errorf("unable to determine a target version")
	}

	if running, _, _ := server.IsRunning(); running {
		out, err := dispatcher.CallTool(cmd.Context(), "server_upgrade", map[string]any{
			"version": targetVersion,
			"when":    upgradeWhen,
		})
		if err != nil {
			return err
		}
		var st saferestart.Status
		if json.Unmarshal([]byte(out), &st) == nil && st.Message != "" {
			fmt.Println(st.Message)
			return nil
		}
		if upgradeWhen == saferestart.WhenIdle {
			fmt.Printf("Aviary will upgrade to %s once no jobs are running. New jobs are held until then.\n", targetVersion)
		} else {
			fmt.Printf("Upgrading the running Aviary server to %s. Running jobs resume after the restart.\n", targetVersion)
		}
		return nil
	}

	if dir := update.GitCheckout(); dir != "" {
		pullCtx, pullCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer pullCancel()
		res, err := update.GitPull(pullCtx, dir)
		if err != nil {
			return err
		}
		fmt.Println(update.GitPullMessage(res, update.DevReloadPlan{GoChanged: true, RestartReason: "the server is not running"}))
		return nil
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
	return nil
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
