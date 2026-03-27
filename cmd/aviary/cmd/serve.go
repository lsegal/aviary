package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/buildinfo"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/logging"
	"github.com/lsegal/aviary/internal/server"
	"github.com/lsegal/aviary/internal/store"
)

var serveDaemon bool

var serveCmd = &cobra.Command{
	Use:   "serve [start|stop]",
	Short: "Manage the Aviary server (start/stop)",
	Long:  `Start or stop the Aviary server. Running 'aviary serve' with no subcommand starts the server.`,
	RunE: func(_ *cobra.Command, args []string) error {
		// Default to start when no subcommand provided.
		if len(args) == 0 || args[0] == "start" {
			if serveDaemon {
				// Launch background process and exit.
				exe, err := os.Executable()
				if err != nil {
					return fmt.Errorf("resolving executable: %w", err)
				}

				// Determine config dir so the background process writes its PID
				// next to the config (not in /tmp). Use resolveConfigPath to
				// resolve any explicit --config flag; fall back to defaults.
				resolvedCfgPath, rerr := resolveConfigPath(cfgFile)
				cfgDir := ""
				if rerr == nil {
					cfgDir = filepath.Dir(resolvedCfgPath)
					_ = os.MkdirAll(cfgDir, 0o750)
				}

				// Build args to run the server in the background without -d.
				procArgs := []string{exe, "serve", "start"}

				// Prepare proc attributes: detach from controlling terminal.
				files := []*os.File{nil, nil, nil}
				null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
				if err == nil {
					files = []*os.File{null, null, null}
				}
				// Prepare proc attributes: detach from controlling terminal.
				env := os.Environ()
				if cfgDir != "" {
					env = append(env, "AVIARY_CONFIG_BASE_DIR="+cfgDir)
				}
				attr := &os.ProcAttr{
					Dir: func() string {
						if cfgDir != "" {
							return cfgDir
						}
						return "."
					}(),
					Env:   env,
					Files: files,
				}
				if runtime.GOOS == "windows" {
					// On Windows, use Start with creation flags via exec.Command
					cmd := exec.Command(exe, "serve", "start")
					if cfgDir != "" {
						cmd.Env = append(os.Environ(), "AVIARY_CONFIG_BASE_DIR="+cfgDir)
					} else {
						cmd.Env = os.Environ()
					}
					cmd.Dir = func() string {
						if cfgDir != "" {
							return cfgDir
						}
						return "."
					}()
					cmd.Stdout = null
					cmd.Stderr = null
					if err := cmd.Start(); err != nil {
						return fmt.Errorf("starting background process: %w", err)
					}
					fmt.Printf("Aviary %s started in background (PID %d)\n", buildinfo.Version, cmd.Process.Pid)
					return nil
				}

				proc, err := os.StartProcess(exe, procArgs, attr)
				if err != nil {
					return fmt.Errorf("starting background process: %w", err)
				}
				fmt.Printf("Aviary %s started in background (PID %d)\n", buildinfo.Version, proc.Pid)
				return nil
			}
			return runStart(nil, nil)
		}
		if args[0] == "stop" {
			return stopCmd.RunE(stopCmd, []string{})
		}
		return fmt.Errorf("unknown subcommand %q", args[0])
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Aviary server",
	RunE:  func(_ *cobra.Command, _ []string) error { return runStop(nil, nil) },
}

func init() {
	serveCmd.PersistentFlags().BoolVarP(&serveDaemon, "daemon", "d", false, "Run in background (daemonize) for start")
	rootCmd.AddCommand(serveCmd)
	// Also add `serve stop` as an explicit subcommand for help clarity.
	serveCmd.AddCommand(stopCmd)
}

func resolveConfigPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = config.DefaultPath()
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving config path: %w", err)
	}
	return abs, nil
}

func chdirToConfigDir(path string) (string, error) {
	resolved, err := resolveConfigPath(path)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(resolved)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("creating config dir: %w", err)
	}
	if err := os.Chdir(dir); err != nil {
		return "", fmt.Errorf("changing to config dir: %w", err)
	}
	if err := os.Setenv("AVIARY_CONFIG_BASE_DIR", dir); err != nil {
		return "", fmt.Errorf("setting config base dir: %w", err)
	}
	return resolved, nil
}

// runStart is the former command implementation from start.go.
func runStart(_ *cobra.Command, _ []string) error {
	// Check if already running.
	running, pid, err := server.IsRunning()
	if err != nil {
		return fmt.Errorf("checking server status: %w", err)
	}
	if running {
		return fmt.Errorf("aviary is already running (PID %d)", pid)
	}

	// Ensure data directories exist.
	if err := store.EnsureDirs(); err != nil {
		return fmt.Errorf("initializing data directory: %w", err)
	}

	resolvedCfgPath, err := chdirToConfigDir(cfgFile)
	if err != nil {
		return err
	}
	cfgFile = resolvedCfgPath

	// Load config (including file-based tasks from agents' tasks/ directories).
	cfg, err := config.Load(resolvedCfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Validate config — fail fast on any errors, print all issues before starting.
	st := authStore()
	if issues := config.Validate(cfg, func(k string) (string, error) { return st.Get(k) }); len(issues) != 0 {
		nerrs := 0
		for _, issue := range issues {
			fmt.Fprintf(os.Stderr, "[%s] %s: %s\n", issue.Level, issue.Field, issue.Message)
			if issue.Level == config.LevelError {
				nerrs++
			}
		}
		if nerrs > 0 {
			return fmt.Errorf("%d configuration error(s) found — run 'aviary doctor' for full details", nerrs)
		}
	}

	// Load or generate auth token.
	tok, isNew, err := server.LoadOrGenerateToken()
	if err != nil {
		return fmt.Errorf("loading token: %w", err)
	}

	// Write PID file.
	if err := server.WritePID(); err != nil {
		return fmt.Errorf("writing PID: %w", err)
	}
	defer server.RemovePID() //nolint:errcheck

	// Print startup banner.
	port := cfg.Server.Port
	if port == 0 {
		port = 16677
	}
	_, _ = fmt.Fprintf(os.Stdout, "Aviary %s started on https://localhost:%d\n", buildinfo.Version, port)
	if isNew {
		_, _ = fmt.Fprintf(os.Stdout, "Your access token: %s\n", tok)
		_, _ = fmt.Fprintf(os.Stdout, "Save this token — you'll need it to access the web panel.\n")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logging.EnableConsole()

	// Handle SIGINT/SIGTERM for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		_, _ = fmt.Fprintln(os.Stdout, "\nShutting down...")
		cancel()
	}()

	// Start server; restart when config changes require it.
	for {
		srv := server.New(cfg, tok)
		err := srv.ListenAndServe(ctx)
		if err == nil || ctx.Err() != nil {
			return err
		}
		if errors.Is(err, server.ErrRestartRequired) {
			var loadErr error
			cfg, loadErr = config.Load(resolvedCfgPath)
			if loadErr != nil {
				return fmt.Errorf("reloading config: %w", loadErr)
			}
			slog.Info("server: restarting with new config")
			continue
		}
		return err
	}
}

// runStop is the former command implementation from stop.go.
func runStop(_ *cobra.Command, _ []string) error {
	running, pid, err := server.IsRunning()
	if err != nil {
		return fmt.Errorf("checking server status: %w", err)
	}
	if !running {
		if pid != 0 {
			// PID file exists but process is gone — clean up.
			_ = server.RemovePID()
		}
		fmt.Println("Aviary is not running.")
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("finding process %d: %w", pid, err)
	}

	if err := proc.Signal(os.Interrupt); err != nil {
		return fmt.Errorf("sending interrupt to PID %d: %w", pid, err)
	}

	fmt.Printf("Sent stop signal to Aviary (PID %d).\n", pid)
	return nil
}
