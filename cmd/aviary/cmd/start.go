package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/server"
	"github.com/lsegal/aviary/internal/store"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Aviary server",
	Long:  `Start the Aviary server over HTTPS on the configured port (default: 16677).`,
	RunE:  runStart,
}

func init() {
	rootCmd.AddCommand(startCmd)
}

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

	// Load config.
	cfg, err := config.Load(cfgFile)
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
	_, _ = fmt.Fprintf(os.Stdout, "Aviary started on https://localhost:%d\n", port)
	if isNew {
		_, _ = fmt.Fprintf(os.Stdout, "Your access token: %s\n", tok)
		_, _ = fmt.Fprintf(os.Stdout, "Save this token — you'll need it to access the web panel.\n")
	}

	// Start server.
	srv := server.New(cfg, tok)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGINT/SIGTERM for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		_, _ = fmt.Fprintln(os.Stdout, "\nShutting down...")
		cancel()
	}()

	return srv.ListenAndServe(ctx)
}
