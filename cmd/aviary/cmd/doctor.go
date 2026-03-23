package cmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/buildinfo"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/server"
	"github.com/lsegal/aviary/internal/store"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Validate configuration and credentials",
	Long: `Check aviary.yaml for configuration errors and verify that required
credentials exist in the auth store. Prints system environment information
suitable for pasting into a bug report.

Exits with status 1 if any errors are found.`,
	RunE: runDoctor,
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorDisableVersion, "disable-version-check", false, "Skip the GitHub release version check and upgrade prompt")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(_ *cobra.Command, _ []string) error {
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}

	// ── System information ────────────────────────────────────────────────────
	_, _ = fmt.Fprintln(os.Stdout, "System information")
	_, _ = fmt.Fprintln(os.Stdout, "──────────────────")
	_, _ = fmt.Fprintf(os.Stdout, "  Aviary version : %s\n", buildinfo.Version)
	_, _ = fmt.Fprintf(os.Stdout, "  Go version     : %s\n", runtime.Version())
	_, _ = fmt.Fprintf(os.Stdout, "  OS/Arch        : %s/%s\n", runtime.GOOS, runtime.GOARCH)

	wd, _ := os.Getwd()
	_, _ = fmt.Fprintf(os.Stdout, "  Working dir    : %s\n", wd)
	_, _ = fmt.Fprintf(os.Stdout, "  Config file    : %s\n", cfgPath)
	_, _ = fmt.Fprintf(os.Stdout, "  Data dir       : %s\n", store.DataDir())

	// Server status + memory
	running, pid, _ := server.IsRunning()
	if running {
		sampler := server.NewProcSampler()
		sampler.Sample([]int{pid})
		stats, _ := sampler.Get(pid)
		memStr := "unavailable"
		if stats.RSSBytes > 0 {
			memStr = fmt.Sprintf("%d MB", stats.RSSBytes/1024/1024)
		}
		_, _ = fmt.Fprintf(os.Stdout, "  Server         : running (PID %d, mem %s)\n", pid, memStr)
	} else {
		_, _ = fmt.Fprintln(os.Stdout, "  Server         : not running")
	}

	// ── Config file ───────────────────────────────────────────────────────────
	_, _ = fmt.Fprintln(os.Stdout)
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		_, _ = fmt.Fprintln(os.Stdout, "[WARN] config: file not found; using built-in defaults")
		_, _ = fmt.Fprintln(os.Stdout, "       Run 'aviary configure' to create one.")
		_, _ = fmt.Fprintln(os.Stdout)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	// ── Configured agents ─────────────────────────────────────────────────────
	_, _ = fmt.Fprintln(os.Stdout, "Configured agents")
	_, _ = fmt.Fprintln(os.Stdout, "─────────────────")
	if len(cfg.Agents) == 0 {
		_, _ = fmt.Fprintln(os.Stdout, "  (none)")
	}
	for _, a := range cfg.Agents {
		model := a.Model
		if model == "" {
			model = "(default)"
		}
		perm := "standard"
		if a.Permissions != nil && a.Permissions.Preset != "" {
			perm = string(a.Permissions.Preset)
		}
		wdir := a.WorkingDir
		if wdir == "" {
			wdir = wd
		}
		_, _ = fmt.Fprintf(os.Stdout, "  %-20s model=%-30s permissions=%s  dir=%s\n",
			a.Name, model, perm, wdir)
	}

	// ── Validation ────────────────────────────────────────────────────────────
	_, _ = fmt.Fprintln(os.Stdout)
	_, _ = fmt.Fprintln(os.Stdout, "Validation")
	_, _ = fmt.Fprintln(os.Stdout, "──────────")
	st := authStore()
	issues := config.Validate(cfg, func(key string) (string, error) {
		return st.Get(key)
	})

	nerrs, nwarns := 0, 0
	for _, issue := range issues {
		_, _ = fmt.Fprintf(os.Stdout, "  [%s] %s: %s\n", issue.Level, issue.Field, issue.Message)
		if issue.Level == config.LevelError {
			nerrs++
		} else {
			nwarns++
		}
	}
	if len(issues) == 0 {
		_, _ = fmt.Fprintln(os.Stdout, "  (no issues)")
	}

	// ── Model credentials ────────────────────────────────────────────────────
	_, _ = fmt.Fprintln(os.Stdout)
	_, _ = fmt.Fprintln(os.Stdout, "Model credentials")
	_, _ = fmt.Fprintln(os.Stdout, "─────────────────")
	factory := llm.NewFactory(func(ref string) (string, error) {
		key := strings.TrimPrefix(ref, "auth:")
		return st.Get(key)
	})
	providerModels := config.UniqueProviderModels(cfg)
	if len(providerModels) == 0 {
		_, _ = fmt.Fprintln(os.Stdout, "  (no models configured)")
	}
	for provider, model := range providerModels {
		_, _ = fmt.Fprintf(os.Stdout, "  %-12s ", provider)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		pingErr := factory.PingModel(ctx, model)
		cancel()
		if pingErr != nil {
			_, _ = fmt.Fprintf(os.Stdout, "[ERROR] %v\n", pingErr)
			nerrs++
		} else {
			_, _ = fmt.Fprintln(os.Stdout, "[OK]")
		}
	}

	_, _ = fmt.Fprintf(os.Stdout, "\n%d error(s), %d warning(s)\n", nerrs, nwarns)
	if !doctorDisableVersion {
		if err := maybeRunDoctorVersionCheck(); err != nil {
			return err
		}
	}
	if nerrs > 0 {
		os.Exit(1)
	}
	return nil
}
