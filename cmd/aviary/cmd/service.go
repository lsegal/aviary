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
	exe, err := os.Executable()
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
	if err := server.InstallService(opts); err != nil {
		return err
	}
	fmt.Printf("Service installed and started: %s\n", opts.Name)
	return nil
}

func runUninstallService(_ *cobra.Command, _ []string) error {
	name := "aviary"
	fmt.Printf("Uninstalling service: %s\n", name)
	if err := server.UninstallService(name); err != nil {
		return err
	}
	fmt.Printf("Service uninstalled: %s\n", name)
	return nil
}

func runInstallDevService(_ *cobra.Command, _ []string) error {
	wd, _ := os.Getwd()
	// Determine user's home and shell. Prefer the user inferred from the
	// project working directory (e.g. /home/USER/...) so running under sudo
	// doesn't force the service to run as root.
	usr, err := user.Current()
	home := os.Getenv("HOME")
	username := ""
	if err == nil {
		if usr.HomeDir != "" {
			home = usr.HomeDir
		}
		username = usr.Username
	}
	// Try to infer user from the project path (wd) when possible.
	if wd != "" {
		// Linux: /home/<user>/..., macOS: /Users/<user>/...
		if strings.HasPrefix(wd, "/home/") {
			parts := strings.SplitN(strings.TrimPrefix(wd, "/home/"), "/", 2)
			if len(parts) > 0 && parts[0] != "" {
				username = parts[0]
				home = filepath.Join("/home", username)
			}
		} else if strings.HasPrefix(wd, "/Users/") {
			parts := strings.SplitN(strings.TrimPrefix(wd, "/Users/"), "/", 2)
			if len(parts) > 0 && parts[0] != "" {
				username = parts[0]
				home = filepath.Join("/Users", username)
			}
		}
	}

	var execPath string
	var args []string
	if wd == "" {
		wd = home
	}
	if runtime.GOOS == "windows" {
		execPath = "cmd.exe"
		args = []string{"/C", "pnpm dev"}
	} else {
		// Use the user's shell to run the pnpm command in the project directory.
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/zsh"
		}
		execPath = shell
		// Inline-source the user's ~/.zshrc and run pnpm dev (use tilde form).
		cmd := "source ~/.zshrc; pnpm dev"
		args = []string{"-lc", cmd}
	}

	opts := server.ServiceOptions{
		Name:        "aviary-dev",
		DisplayName: "Aviary (dev pnpm dev)",
		Description: "Runs `pnpm dev` for aviary (development)",
		Exec:        execPath,
		Args:        args,
		WorkingDir:  wd,
		User:        username,
	}
	fmt.Printf("Installing service: %s\n", opts.Name)
	if err := server.InstallService(opts); err != nil {
		return err
	}
	fmt.Printf("Service installed and started: %s\n", opts.Name)
	return nil
}

func runUninstallDevService(_ *cobra.Command, _ []string) error {
	name := "aviary-dev"
	fmt.Printf("Uninstalling service: %s\n", name)
	if err := server.UninstallService(name); err != nil {
		return err
	}
	fmt.Printf("Service uninstalled: %s\n", name)
	return nil
}

func runInstallDevDocsService(_ *cobra.Command, _ []string) error {
	wd, _ := os.Getwd()
	usr, err := user.Current()
	home := os.Getenv("HOME")
	username := ""
	if err == nil {
		if usr.HomeDir != "" {
			home = usr.HomeDir
		}
		username = usr.Username
	}

	var execPath string
	var args []string
	if wd == "" {
		wd = home
	}
	if runtime.GOOS == "windows" {
		execPath = "cmd.exe"
		args = []string{"/C", "pnpm docs:dev"}
	} else {
		// Use the user's shell to run the pnpm docs dev command in the project directory.
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/zsh"
		}
		execPath = shell
		// Inline-source the user's ~/.zshrc and run pnpm docs:dev (use tilde form).
		cmd := "source ~/.zshrc; pnpm docs:dev"
		args = []string{"-lc", cmd}
	}

	opts := server.ServiceOptions{
		Name:        "aviary-dev-docs",
		DisplayName: "Aviary (docs:dev)",
		Description: "Runs `pnpm docs:dev` for aviary (development)",
		Exec:        execPath,
		Args:        args,
		WorkingDir:  wd,
		User:        username,
	}
	fmt.Printf("Installing service: %s\n", opts.Name)
	if err := server.InstallService(opts); err != nil {
		return err
	}
	fmt.Printf("Service installed and started: %s\n", opts.Name)
	return nil
}

func runUninstallDevDocsService(_ *cobra.Command, _ []string) error {
	name := "aviary-dev-docs"
	fmt.Printf("Uninstalling service: %s\n", name)
	if err := server.UninstallService(name); err != nil {
		return err
	}
	fmt.Printf("Service uninstalled: %s\n", name)
	return nil
}

func runStartService(_ *cobra.Command, _ []string) error {
	name := "aviary"
	fmt.Printf("Starting service: %s\n", name)
	if err := server.StartService(name); err != nil {
		return err
	}
	fmt.Printf("Service started: %s\n", name)
	return nil
}
func runStopService(_ *cobra.Command, _ []string) error {
	name := "aviary"
	fmt.Printf("Stopping service: %s\n", name)
	if err := server.StopService(name); err != nil {
		return err
	}
	fmt.Printf("Service stopped: %s\n", name)
	return nil
}

func runStartDevService(_ *cobra.Command, _ []string) error {
	name := "aviary-dev"
	fmt.Printf("Starting service: %s\n", name)
	if err := server.StartService(name); err != nil {
		return err
	}
	fmt.Printf("Service started: %s\n", name)
	return nil
}

func runStopDevService(_ *cobra.Command, _ []string) error {
	name := "aviary-dev"
	fmt.Printf("Stopping service: %s\n", name)
	if err := server.StopService(name); err != nil {
		return err
	}
	fmt.Printf("Service stopped: %s\n", name)
	return nil
}

func runStartDevDocsService(_ *cobra.Command, _ []string) error {
	name := "aviary-dev-docs"
	fmt.Printf("Starting service: %s\n", name)
	if err := server.StartService(name); err != nil {
		return err
	}
	fmt.Printf("Service started: %s\n", name)
	return nil
}

func runStopDevDocsService(_ *cobra.Command, _ []string) error {
	name := "aviary-dev-docs"
	fmt.Printf("Stopping service: %s\n", name)
	if err := server.StopService(name); err != nil {
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
	running, pid, err := server.IsRunning()
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
