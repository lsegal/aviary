package server

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ServiceOptions describes a service to install.
type ServiceOptions struct {
	Name        string
	DisplayName string
	Description string
	Exec        string   // path to executable
	Args        []string // arguments
	WorkingDir  string
	User        string
}

// InstallService installs and starts a simple service for common platforms.
func InstallService(opts ServiceOptions) error {
	// If User not provided (or explicitly set to "root"), try to infer a sensible non-root user to run the
	// service as. This avoids writing units that run as root when the CLI was
	// invoked via sudo by a normal user.
	if opts.User == "root" {
		// treat explicit root as unspecified so we don't force User=root in units
		opts.User = ""
	}
	if opts.User == "" {
		// Prefer SUDO_USER if present (user who invoked sudo).
		if su := os.Getenv("SUDO_USER"); su != "" && su != "root" {
			opts.User = su
		}
		// If still empty, try to infer from WorkingDir (e.g. /home/<user>/... or /Users/<user>/...)
		if opts.User == "" && opts.WorkingDir != "" {
			wd := opts.WorkingDir
			if strings.HasPrefix(wd, "/home/") {
				parts := strings.SplitN(strings.TrimPrefix(wd, "/home/"), "/", 2)
				if len(parts) > 0 && parts[0] != "" {
					opts.User = parts[0]
				}
			} else if strings.HasPrefix(wd, "/Users/") {
				parts := strings.SplitN(strings.TrimPrefix(wd, "/Users/"), "/", 2)
				if len(parts) > 0 && parts[0] != "" {
					opts.User = parts[0]
				}
			}
		}
		// Finally fall back to the current user if one is available and not root.
		if opts.User == "" {
			if u, err := os.UserHomeDir(); err == nil {
				// try to parse home dir for username (best-effort)
				if strings.HasPrefix(u, "/home/") {
					parts := strings.SplitN(strings.TrimPrefix(u, "/home/"), "/", 2)
					if len(parts) > 0 && parts[0] != "" && parts[0] != "root" {
						opts.User = parts[0]
					}
				} else if strings.HasPrefix(u, "/Users/") {
					parts := strings.SplitN(strings.TrimPrefix(u, "/Users/"), "/", 2)
					if len(parts) > 0 && parts[0] != "" && parts[0] != "root" {
						opts.User = parts[0]
					}
				}
			}
		}
	}
	switch runtime.GOOS {
	case "linux":
		// Guard: if dev helper detected but name left as default, prefer a dev service name
		joined := strings.Join(opts.Args, " ")
		if strings.Contains(joined, "pnpm docs:dev") && opts.Name == "aviary" {
			opts.Name = "aviary-dev-docs"
		} else if strings.Contains(joined, "pnpm dev") && opts.Name == "aviary" {
			opts.Name = "aviary-dev"
		}

		if err := installSystemd(opts); err != nil {
			return trySudoRetry("install", err)
		}
		return nil
	case "darwin":
		// Guard for dev helpers on mac as well
		joined := strings.Join(opts.Args, " ")
		if strings.Contains(joined, "pnpm docs:dev") && opts.Name == "aviary" {
			opts.Name = "aviary-dev-docs"
		} else if strings.Contains(joined, "pnpm dev") && opts.Name == "aviary" {
			opts.Name = "aviary-dev"
		}

		if err := installLaunchd(opts); err != nil {
			return trySudoRetry("install", err)
		}
		return nil
	case "windows":
		return installWindowsService(opts)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// UninstallService removes the service registration.
func UninstallService(name string) error {
	switch runtime.GOOS {
	case "linux":
		if err := uninstallSystemd(name); err != nil {
			return trySudoRetry("uninstall", err)
		}
		return nil
	case "darwin":
		if err := uninstallLaunchd(name); err != nil {
			return trySudoRetry("uninstall", err)
		}
		return nil
	case "windows":
		return uninstallWindowsService(name)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// trySudoRetry checks for permission-related errors and, if appropriate,
// re-executes the same aviary binary via sudo to retry the requested action.
func trySudoRetry(action string, origErr error) error {
	// Only retry on obvious permission errors and only on POSIX systems.
	if runtime.GOOS == "windows" {
		return origErr
	}
	if os.Getenv("AVIARY_TRIED_SUDO") == "1" {
		return origErr
	}
	lower := strings.ToLower(origErr.Error())
	// Retry on permission-like errors or common authentication prompts.
	if !(strings.Contains(lower, "permission") || strings.Contains(lower, "permission denied") || strings.Contains(lower, "access denied") || strings.Contains(lower, "interactive authentication") || strings.Contains(lower, "authentication required") || strings.Contains(lower, "polkit") || strings.Contains(lower, "authorization")) {
		return origErr
	}

	exe, err := os.Executable()
	if err != nil {
		return origErr
	}
	// Build sudo command: use `sudo -E` to preserve the current environment
	// (including the user's SHELL) so the re-executed installer can detect
	// the original user's shell rather than root's shell.
	_, _ = fmt.Fprintf(os.Stderr, "Attempting to get increased privileges to %s the system service.\n", action)

	// Re-run the same invocation under sudo preserving environment.
	// e.g. `sudo -E <exe> service install-dev-docs` so we don't lose the dev-specific command.
	sudoArgs := append([]string{"-E", exe}, os.Args[1:]...)
	cmd := exec.Command("sudo", sudoArgs...)
	// Ensure AVIARY_TRIED_SUDO is set for the child process environment.
	cmd.Env = append(os.Environ(), "AVIARY_TRIED_SUDO=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w; sudo retry failed: %v", origErr, err)
	}
	return nil
}

func installSystemd(opts ServiceOptions) error {
	// Build service unit; include User when provided. Don't override HOME or
	// other environment variables — running as the specified User will cause
	// systemd to set HOME appropriately for that account.
	userLine := ""
	if opts.User != "" {
		userLine = fmt.Sprintf("User=%s\n", opts.User)
	}

	unit := fmt.Sprintf(`[Unit]
Description=%s
After=network.target

[Service]
Type=simple
%sWorkingDirectory=%s
ExecStart=%s %s
Restart=on-failure

[Install]
WantedBy=multi-user.target
`, opts.Description, userLine, escapeSystemPath(opts.WorkingDir), escapeSystemPath(opts.Exec), strings.Join(escapeArgs(opts.Args), " "))

	path := filepath.Join("/etc/systemd/system", opts.Name+".service")
	if err := os.WriteFile(path, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("writing unit file: %w", err)
	}
	if err := runCmd("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	if err := runCmd("systemctl", "enable", opts.Name); err != nil {
		return fmt.Errorf("systemctl enable: %w", err)
	}
	if err := runCmd("systemctl", "start", opts.Name); err != nil {
		return fmt.Errorf("systemctl start: %w", err)
	}
	return nil
}

func uninstallSystemd(name string) error {
	_ = runCmd("systemctl", "stop", name)
	_ = runCmd("systemctl", "disable", name)
	path := filepath.Join("/etc/systemd/system", name+".service")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing unit file: %w", err)
	}
	if err := runCmd("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	return nil
}

func installLaunchd(opts ServiceOptions) error {
	// create a LaunchDaemon plist in /Library/LaunchDaemons
	plistName := "bot.aviary." + opts.Name
	plistPath := filepath.Join("/Library/LaunchDaemons", plistName+".plist")
	// program arguments: exec followed by args
	var b bytes.Buffer
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString(fmt.Sprintf("  <key>Label</key>\n  <string>%s</string>\n", plistName))
	b.WriteString("  <key>KeepAlive</key>\n  <true/>\n")
	b.WriteString("  <key>RunAtLoad</key>\n  <true/>\n")
	b.WriteString("  <key>WorkingDirectory</key>\n")
	b.WriteString(fmt.Sprintf("  <string>%s</string>\n", escapeXML(opts.WorkingDir)))
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	b.WriteString(fmt.Sprintf("    <string>%s</string>\n", escapeXML(opts.Exec)))
	for _, a := range opts.Args {
		b.WriteString(fmt.Sprintf("    <string>%s</string>\n", escapeXML(a)))
	}
	b.WriteString("  </array>\n</dict>\n</plist>\n")

	if err := os.WriteFile(plistPath, b.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing plist: %w", err)
	}
	if err := runCmd("launchctl", "load", plistPath); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
	}
	return nil
}

func uninstallLaunchd(name string) error {
	plistName := "bot.aviary." + name
	plistPath := filepath.Join("/Library/LaunchDaemons", plistName+".plist")
	_ = runCmd("launchctl", "unload", plistPath)
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing plist: %w", err)
	}
	return nil
}

func installWindowsService(opts ServiceOptions) error {
	if err := validateWindows(); err != nil {
		return err
	}
	// Use sc.exe to create a simple service. The binPath must be quoted.
	binPath := fmt.Sprintf("%s %s", opts.Exec, strings.Join(escapeArgs(opts.Args), " "))
	// sc requires the `binPath=` token and spacing exactly like this.
	cmd := exec.Command("sc", "create", opts.Name, "binPath=", binPath, "start=", "auto")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sc create failed: %w: %s", err, string(out))
	}
	// Start the service
	if err := runCmd("sc", "start", opts.Name); err != nil {
		return fmt.Errorf("sc start: %w", err)
	}
	return nil
}

func uninstallWindowsService(name string) error {
	_ = runCmd("sc", "stop", name)
	if err := runCmd("sc", "delete", name); err != nil {
		return fmt.Errorf("sc delete: %w", err)
	}
	return nil
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %v: %w: %s", name, args, err, string(out))
	}
	return nil
}

func escapeArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t\n\r'\"\\") {
			out[i] = fmt.Sprintf("'%s'", strings.ReplaceAll(a, "'", "'\\''"))
		} else {
			out[i] = a
		}
	}
	return out
}

func escapeSystemPath(p string) string {
	if strings.Contains(p, " ") {
		return fmt.Sprintf("\"%s\"", p)
	}
	return p
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

// Basic validation: ensure sc.exe exists on Windows
func validateWindows() error {
	if runtime.GOOS != "windows" {
		return nil
	}
	if _, err := exec.LookPath("sc"); err != nil {
		return errors.New("sc.exe not found in PATH")
	}
	return nil
}
