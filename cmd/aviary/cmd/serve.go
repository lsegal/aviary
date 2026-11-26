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
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/buildinfo"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/logging"
	"github.com/lsegal/aviary/internal/server"
	"github.com/lsegal/aviary/internal/startup"
	"github.com/lsegal/aviary/internal/store"
	"github.com/lsegal/aviary/internal/update"
)

var (
	serveDaemon  bool
	traceStartup bool
)

// startServer runs the server in the foreground; tests replace it.
var startServer = runStart

// startBackground starts the detached server process and returns its PID;
// tests replace it.
var startBackground = func(c *exec.Cmd) (int, error) {
	if err := c.Start(); err != nil {
		return 0, err
	}
	return c.Process.Pid, nil
}

// aviaryServer is the part of *server.Server that runStart drives.
type aviaryServer interface {
	ListenAndServe(ctx context.Context) error
	ReexecPath() string
}

// newServer builds the server runStart serves; tests replace it.
var newServer = func(cfg *config.Config, tok string) aviaryServer { return server.New(cfg, tok) }

// reexec replaces the process with a new server binary; tests replace it.
var reexec = update.Reexec

var serveCmd = &cobra.Command{
	Use:   "serve [start|stop]",
	Short: "Manage the Aviary server (start/stop)",
	Long:  `Start or stop the Aviary server. Running 'aviary serve' with no subcommand starts the server.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Default to start when no subcommand provided.
		if len(args) == 0 || args[0] == "start" {
			// Errors from here on are runtime failures, not usage errors.
			cmd.SilenceUsage = true
			if serveDaemon {
				return startDaemon()
			}
			return startServer(nil, nil)
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
	serveCmd.PersistentFlags().BoolVar(&traceStartup, "trace-startup", false, "Print the per-phase startup trace once boot finishes")
	rootCmd.AddCommand(serveCmd)
	// Also add `serve stop` as an explicit subcommand for help clarity.
	serveCmd.AddCommand(stopCmd)
}

// startDaemon launches 'aviary serve start' as a background process
// detached from the terminal, running in the config's directory.
func startDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving executable: %w", err)
	}
	c := exec.Command(exe, "serve", "start")
	c.Env = os.Environ()
	c.Dir = "."
	// Run next to the config so the background process writes its PID there
	// (not in /tmp), honoring an explicit --config flag.
	if resolvedCfgPath, err := resolveConfigPath(cfgFile); err == nil {
		cfgDir := filepath.Dir(resolvedCfgPath)
		_ = os.MkdirAll(cfgDir, 0o750)
		c.Env = append(c.Env, "AVIARY_CONFIG_BASE_DIR="+cfgDir)
		c.Dir = cfgDir
	}
	if null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0); err == nil {
		defer null.Close() //nolint:errcheck
		c.Stdin, c.Stdout, c.Stderr = null, null, null
	}
	pid, err := startBackground(c)
	if err != nil {
		return fmt.Errorf("starting background process: %w", err)
	}
	fmt.Printf("Aviary %s started in background (PID %d)\n", buildinfo.Version, pid)
	return nil
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
	resolved, err := useConfigBaseDir(path)
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
	return resolved, nil
}

// useConfigBaseDir makes the directory of the config at path (default:
// config.DefaultPath) the config base dir, as the server for that config
// does, so the PID file this process reads is the one that server writes.
func useConfigBaseDir(path string) (string, error) {
	resolved, err := resolveConfigPath(path)
	if err != nil {
		return "", err
	}
	if err := os.Setenv("AVIARY_CONFIG_BASE_DIR", filepath.Dir(resolved)); err != nil {
		return "", fmt.Errorf("setting config base dir: %w", err)
	}
	return resolved, nil
}

// configServerRunning reports whether the server for the config at path is
// running, reading the PID file that server writes.
func configServerRunning(path string) (bool, int, error) {
	if _, err := useConfigBaseDir(path); err != nil {
		return false, 0, err
	}
	return server.IsRunning()
}

// runStart is the former command implementation from start.go.
func runStart(_ *cobra.Command, _ []string) error {
	trace := startup.Begin()
	if err := checkNotAlreadyRunning(); err != nil {
		return err
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

	cfg, err := loadStartConfig(trace, resolvedCfgPath)
	if err != nil {
		return err
	}

	// Load or generate auth token.
	endPhase := trace.Phase("auth token load")
	tok, isNew, err := server.LoadOrGenerateToken()
	endPhase()
	if err != nil {
		return fmt.Errorf("loading token: %w", err)
	}

	// Write PID file.
	if err := server.WritePID(); err != nil {
		return fmt.Errorf("writing PID: %w", err)
	}
	defer server.RemovePID() //nolint:errcheck

	// Print startup banner.
	_, _ = fmt.Fprintf(os.Stdout, "Aviary %s started on %s\n", buildinfo.Version, config.ServerURL(cfg.Server))
	if isNew {
		_, _ = fmt.Fprintf(os.Stdout, "Your access token: %s\n", tok)
		_, _ = fmt.Fprintf(os.Stdout, "Save this token — you'll need it to access the web panel.\n")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logging.EnableConsole()

	stopSignals := cancelOnSignal(ctx, cancel)
	defer stopSignals()

	if traceStartup {
		traced := make(chan struct{})
		go func() {
			defer close(traced)
			printStartupTrace(ctx)
		}()
		defer func() {
			cancel()
			<-traced
		}()
	}

	return serveUntilDone(ctx, cfg, tok, resolvedCfgPath)
}

// checkNotAlreadyRunning fails when another server for the config is running.
func checkNotAlreadyRunning() error {
	running, pid, err := configServerRunning(cfgFile)
	if err != nil {
		return fmt.Errorf("checking server status: %w", err)
	}
	// A stored PID equal to our own is stale: a container whose config dir
	// outlives it runs the next server under the same PID, usually 1.
	if running && pid != os.Getpid() {
		return fmt.Errorf("aviary is already running (PID %d)", pid)
	}
	return nil
}

// loadStartConfig loads the config (including file-based tasks from agents'
// tasks/ directories) and validates it, printing every issue and failing
// fast on any error.
func loadStartConfig(trace *startup.Trace, path string) (*config.Config, error) {
	endPhase := trace.Phase("config load")
	cfg, err := config.Load(path)
	endPhase()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	endPhase = trace.Phase("config validate")
	st := authStore()
	issues := config.Validate(cfg, func(k string) (string, error) { return st.Get(k) })
	endPhase()
	nerrs := 0
	for _, issue := range issues {
		fmt.Fprintf(os.Stderr, "[%s] %s: %s\n", issue.Level, issue.Field, issue.Message)
		if issue.Level == config.LevelError {
			nerrs++
		}
	}
	if nerrs > 0 {
		return nil, fmt.Errorf("%d configuration error(s) found — run 'aviary doctor' for full details", nerrs)
	}
	return cfg, nil
}

// cancelOnSignal cancels ctx on SIGINT/SIGTERM for a graceful shutdown. The
// returned func stops listening for the signals.
func cancelOnSignal(ctx context.Context, cancel context.CancelFunc) func() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			_, _ = fmt.Fprintln(os.Stdout, "\nShutting down...")
			cancel()
		case <-ctx.Done():
		}
	}()
	return func() { signal.Stop(sigCh) }
}

// serveUntilDone runs the server, restarting it when config changes require
// it and re-executing the binary when an upgrade does.
func serveUntilDone(ctx context.Context, cfg *config.Config, tok, cfgPath string) error {
	for {
		srv := newServer(cfg, tok)
		err := srv.ListenAndServe(ctx)
		if err == nil || ctx.Err() != nil {
			return shutdownErr(err)
		}
		if errors.Is(err, server.ErrReexecRequired) {
			return restartProcess(srv.ReexecPath())
		}
		if !errors.Is(err, server.ErrRestartRequired) {
			return err
		}
		endPhase := startup.Begin().Phase("config load")
		var loadErr error
		cfg, loadErr = config.Load(cfgPath)
		endPhase()
		if loadErr != nil {
			return fmt.Errorf("reloading config: %w", loadErr)
		}
		slog.Info("server: restarting with new config")
	}
}

// restartProcess hands the server over to a new process: through the dev
// supervisor when it rebuilds, else by re-executing path.
func restartProcess(path string) error {
	// Release the PID file first: the new process checks it on start.
	_ = server.RemovePID()
	if update.RequestRebuild() {
		// scripts/dev-go.mjs rebuilds from the checkout and runs it.
		slog.Info("server: restarting through the dev supervisor")
		return nil
	}
	slog.Info("server: restarting")
	return reexec(path, os.Args[1:], os.Environ())
}

// printStartupTrace prints the boot's per-phase trace once every subsystem
// has finished initializing.
func printStartupTrace(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if r := startup.Current().Snapshot(); r.Ready {
			_, _ = fmt.Fprintf(os.Stdout, "Startup trace\n%s", r.Detail())
			return
		}
	}
}

// shutdownErr is what serving returns once a signal asked it to stop. A
// restart the server was in the middle of is moot then, so its sentinel is
// not an error.
func shutdownErr(err error) error {
	if errors.Is(err, server.ErrReexecRequired) || errors.Is(err, server.ErrRestartRequired) {
		return nil
	}
	return err
}

// runStop is the former command implementation from stop.go.
func runStop(_ *cobra.Command, _ []string) error {
	running, pid, err := configServerRunning(cfgFile)
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
