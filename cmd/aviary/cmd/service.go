package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/buildinfo"
	"github.com/lsegal/aviary/internal/server"
)

// Service-manager calls and environment lookups the service handlers use,
// replaced in tests so they never touch the real launchd/systemd/Windows
// service manager.
var (
	installService     = server.InstallService
	uninstallService   = server.UninstallService
	startService       = server.StartService
	stopService        = server.StopService
	serviceExecutable  = os.Executable
	serviceGetwd       = os.Getwd
	serviceCurrentUser = user.Current
	serviceGOOS        = runtime.GOOS
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage OS service installation and status",
	Long:  `Install/uninstall the Aviary service and show its status.`,
}

func init() {
	rootCmd.AddCommand(serviceCmd)

	serviceCmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show whether the Aviary service is running",
		RunE:  runStatus,
	})

	serviceCmd.AddCommand(&cobra.Command{
		Use:   "start",
		Short: "Start the Aviary service (if installed)",
		RunE:  runStartService,
	})

	serviceCmd.AddCommand(&cobra.Command{
		Use:   "stop",
		Short: "Stop the Aviary service (if installed)",
		RunE:  runStopService,
	})

	serviceCmd.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Install the Aviary OS service",
		RunE:  runInstallService,
	})

	serviceCmd.AddCommand(&cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall the Aviary OS service",
		RunE:  runUninstallService,
	})

	// Dev-only service helpers (visible only in non-release builds).
	if buildinfo.Version == "dev" {
		serviceCmd.AddCommand(&cobra.Command{
			Use:   "install-dev",
			Short: "Install a service that runs `pnpm dev` (dev builds only)",
			RunE:  runInstallDevService,
		})
		serviceCmd.AddCommand(&cobra.Command{
			Use:   "uninstall-dev",
			Short: "Uninstall the `pnpm dev` service (dev builds only)",
			RunE:  runUninstallDevService,
		})
		serviceCmd.AddCommand(&cobra.Command{
			Use:   "install-dev-docs",
			Short: "Install a service that runs `pnpm docs:dev` (dev builds only)",
			RunE:  runInstallDevDocsService,
		})
		serviceCmd.AddCommand(&cobra.Command{
			Use:   "uninstall-dev-docs",
			Short: "Uninstall the `pnpm docs:dev` service (dev builds only)",
			RunE:  runUninstallDevDocsService,
		})

		serviceCmd.AddCommand(&cobra.Command{
			Use:   "start-dev",
			Short: "Start the Aviary dev service (dev builds only)",
			RunE:  runStartDevService,
		})

		serviceCmd.AddCommand(&cobra.Command{
			Use:   "stop-dev",
			Short: "Stop the Aviary dev service (dev builds only)",
			RunE:  runStopDevService,
		})

		serviceCmd.AddCommand(&cobra.Command{
			Use:   "start-dev-docs",
			Short: "Start the Aviary docs:dev service (dev builds only)",
			RunE:  runStartDevDocsService,
		})

		serviceCmd.AddCommand(&cobra.Command{
			Use:   "stop-dev-docs",
			Short: "Stop the Aviary docs:dev service (dev builds only)",
			RunE:  runStopDevDocsService,
		})
	}
}

func runInstallService(_ *cobra.Command, _ []string) error {
	exe, err := serviceExecutable()
	if err != nil {
		return fmt.Errorf("locating executable: %w", err)
	}
	opts := server.ServiceOptions{
		Name:        "aviary",
		DisplayName: "Aviary",
		Description: "Aviary agent server",
		Exec:        exe,
		Args:        []string{"serve", "start", "-d"},
		WorkingDir:  filepath.Dir(exe),
	}
	fmt.Printf("Installing service: %s\n", opts.Name)
	if err := installService(opts); err != nil {
		return err
	}
	fmt.Printf("Service installed and started: %s\n", opts.Name)
	return nil
}

func runUninstallService(_ *cobra.Command, _ []string) error {
	name := "aviary"
	fmt.Printf("Uninstalling service: %s\n", name)
	if err := uninstallService(name); err != nil {
		return err
	}
	fmt.Printf("Service uninstalled: %s\n", name)
	return nil
}

// devServiceUser returns the user and home directory a development service
// should run as. It prefers the user inferred from the project working
// directory (e.g. /home/USER/...) so running under sudo doesn't force the
// service to run as root.
func devServiceUser(wd string) (username, home string) {
	home = os.Getenv("HOME")
	if usr, err := serviceCurrentUser(); err == nil {
		if usr.HomeDir != "" {
			home = usr.HomeDir
		}
		username = usr.Username
	}
	// Linux: /home/<user>/..., macOS: /Users/<user>/...
	for _, root := range []string{"/home", "/Users"} {
		rest, ok := strings.CutPrefix(wd, root+"/")
		if !ok {
			continue
		}
		if name, _, _ := strings.Cut(rest, "/"); name != "" {
			return name, filepath.Join(root, name)
		}
	}
	return username, home
}

// devPnpmService describes a development service that runs a pnpm script
// from the project working directory.
type devPnpmService struct {
	name        string
	displayName string
	script      string
}

func runInstallDevService(_ *cobra.Command, _ []string) error {
	return installDevPnpmService(devPnpmService{
		name:        "aviary-dev",
		displayName: "Aviary (dev pnpm dev)",
		script:      "dev",
	})
}

func runUninstallDevService(_ *cobra.Command, _ []string) error {
	name := "aviary-dev"
	fmt.Printf("Uninstalling service: %s\n", name)
	if err := uninstallService(name); err != nil {
		return err
	}
	fmt.Printf("Service uninstalled: %s\n", name)
	return nil
}

func runInstallDevDocsService(_ *cobra.Command, _ []string) error {
	return installDevPnpmService(devPnpmService{
		name:        "aviary-dev-docs",
		displayName: "Aviary (docs:dev)",
		script:      "docs:dev",
	})
}

// installDevPnpmService installs and starts a service that runs `pnpm
// <script>` in the project directory as the inferred development user.
func installDevPnpmService(svc devPnpmService) error {
	wd, _ := serviceGetwd()
	username, home := devServiceUser(wd)

	var execPath string
	var args []string
	if wd == "" {
		wd = home
	}
	pnpmCmd := "pnpm " + svc.script
	if serviceGOOS == "windows" {
		execPath = "cmd.exe"
		args = []string{"/C", pnpmCmd}
	} else {
		// Use the user's shell to run the pnpm command in the project directory.
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/zsh"
		}
		execPath = shell
		// Inline-source the user's ~/.zshrc and run the pnpm script (use tilde form).
		args = []string{"-lc", "source ~/.zshrc; " + pnpmCmd}
	}

	opts := server.ServiceOptions{
		Name:        svc.name,
		DisplayName: svc.displayName,
		Description: "Runs `" + pnpmCmd + "` for aviary (development)",
		Exec:        execPath,
		Args:        args,
		WorkingDir:  wd,
		User:        username,
	}
	fmt.Printf("Installing service: %s\n", opts.Name)
	if err := installService(opts); err != nil {
		return err
	}
	fmt.Printf("Service installed and started: %s\n", opts.Name)
	return nil
}

func runUninstallDevDocsService(_ *cobra.Command, _ []string) error {
	name := "aviary-dev-docs"
	fmt.Printf("Uninstalling service: %s\n", name)
	if err := uninstallService(name); err != nil {
		return err
	}
	fmt.Printf("Service uninstalled: %s\n", name)
	return nil
}

func runStartService(_ *cobra.Command, _ []string) error {
	name := "aviary"
	fmt.Printf("Starting service: %s\n", name)
	if err := startService(name); err != nil {
		return err
	}
	fmt.Printf("Service started: %s\n", name)
	return nil
}
func runStopService(_ *cobra.Command, _ []string) error {
	name := "aviary"
	fmt.Printf("Stopping service: %s\n", name)
	if err := stopService(name); err != nil {
		return err
	}
	fmt.Printf("Service stopped: %s\n", name)
	return nil
}

func runStartDevService(_ *cobra.Command, _ []string) error {
	name := "aviary-dev"
	fmt.Printf("Starting service: %s\n", name)
	if err := startService(name); err != nil {
		return err
	}
	fmt.Printf("Service started: %s\n", name)
	return nil
}

func runStopDevService(_ *cobra.Command, _ []string) error {
	name := "aviary-dev"
	fmt.Printf("Stopping service: %s\n", name)
	if err := stopService(name); err != nil {
		return err
	}
	fmt.Printf("Service stopped: %s\n", name)
	return nil
}

func runStartDevDocsService(_ *cobra.Command, _ []string) error {
	name := "aviary-dev-docs"
	fmt.Printf("Starting service: %s\n", name)
	if err := startService(name); err != nil {
		return err
	}
	fmt.Printf("Service started: %s\n", name)
	return nil
}

func runStopDevDocsService(_ *cobra.Command, _ []string) error {
	name := "aviary-dev-docs"
	fmt.Printf("Stopping service: %s\n", name)
	if err := stopService(name); err != nil {
		return err
	}
	fmt.Printf("Service stopped: %s\n", name)
	return nil
}

func runStatus(_ *cobra.Command, _ []string) error {
	// The status command reports on the running server process rather than
	// the platform service name, but include the canonical service name
	// being queried for clarity.
	serviceName := "aviary"
	running, pid, err := configServerRunning(cfgFile)
	if err != nil {
		return fmt.Errorf("checking server status: %w", err)
	}
	if !running {
		if pid != 0 {
			// Stale PID file — clean it up.
			_ = server.RemovePID()
		}
		fmt.Printf("Service '%s' is not running.\n", serviceName)
		return nil
	}
	fmt.Printf("Service '%s' is running (PID %d).\n", serviceName, pid)
	fmt.Printf("PID file: %s\n", server.PIDPath())
	return nil
}
