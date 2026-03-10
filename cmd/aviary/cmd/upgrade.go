package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/update"
)

var (
	upgradeVersion       string
	doctorDisableVersion bool
	helperTargetPath     string
	helperWaitPID        int
	helperRestartArgs    string
	helperRepo           string
	helperAPIBase        string
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade Aviary to the latest release",
	RunE:  runUpgrade,
}

var upgradeHelperCmd = &cobra.Command{
	Use:    "__upgrade-helper",
	Hidden: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		restartArgs, err := update.DecodeRestartArgs(helperRestartArgs)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		return update.RunHelper(ctx, update.HelperRequest{
			TargetPath:  helperTargetPath,
			WaitPID:     helperWaitPID,
			Version:     upgradeVersion,
			RestartArgs: restartArgs,
			Repo:        helperRepo,
			APIBase:     helperAPIBase,
		}, os.Stdout, os.Stderr)
	},
}

func init() {
	upgradeCmd.Flags().StringVar(&upgradeVersion, "version", "", "Upgrade to a specific release tag instead of the latest release")
	rootCmd.AddCommand(upgradeCmd)

	upgradeHelperCmd.Flags().StringVar(&helperTargetPath, "target-path", "", "target binary path")
	upgradeHelperCmd.Flags().IntVar(&helperWaitPID, "wait-pid", 0, "wait for this PID to exit before upgrading")
	upgradeHelperCmd.Flags().StringVar(&upgradeVersion, "version", "", "target release version")
	upgradeHelperCmd.Flags().StringVar(&helperRestartArgs, "restart-args", "", "base64-encoded restart arguments")
	upgradeHelperCmd.Flags().StringVar(&helperRepo, "repo", update.DefaultRepo, "release repository")
	upgradeHelperCmd.Flags().StringVar(&helperAPIBase, "api-base", update.DefaultAPIBase, "release API base")
	rootCmd.AddCommand(upgradeHelperCmd)
}

func runUpgrade(_ *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	check, err := update.Check(ctx, &http.Client{Timeout: 15 * time.Second})
	if err != nil && check.LatestVersion == "" {
		return err
	}
	targetVersion := strings.TrimSpace(upgradeVersion)
	if targetVersion == "" {
		targetVersion = check.LatestVersion
	}
	if targetVersion == "" {
		return fmt.Errorf("unable to determine a target version")
	}
	if strings.TrimSpace(upgradeVersion) == "" && !check.UpgradeAvailable {
		fmt.Printf("Aviary is already up to date (%s).\n", check.CurrentVersion)
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
	if err := update.StartHelper(update.HelperRequest{
		TargetPath: exePath,
		WaitPID:    os.Getpid(),
		Version:    targetVersion,
		Repo:       update.DefaultRepo,
		APIBase:    update.DefaultAPIBase,
	}); err != nil {
		return err
	}
	fmt.Printf("Starting Aviary upgrade to %s.\n", targetVersion)
	fmt.Println("This process will exit; the helper will replace the binary in place.")
	return nil
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
