package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/buildinfo"
	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/clidoctor"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/server"
	"github.com/lsegal/aviary/internal/startup"
	"github.com/lsegal/aviary/internal/store"
	"github.com/lsegal/aviary/internal/update"
	"github.com/lsegal/aviary/internal/version"
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

// doctorServerStatus describes the server for the config at cfgPath: its PID
// and memory from the PID file that server writes, in the config's own
// directory. A server without a live PID there, such as one whose PID file
// was removed, is still reported running when it answers on its port.
func doctorServerStatus(cfgPath string, port int) string {
	if running, pid, _ := configServerRunning(cfgPath); running {
		sampler := server.NewProcSampler()
		sampler.Sample([]int{pid})
		stats, _ := sampler.Get(pid)
		memStr := "unavailable"
		if stats.RSSBytes > 0 {
			memStr = fmt.Sprintf("%d MB", stats.RSSBytes/1024/1024)
		}
		return fmt.Sprintf("running (PID %d, mem %s)", pid, memStr)
	}
	if port == 0 {
		port = 16677
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("localhost", strconv.Itoa(port)), time.Second)
	if err != nil {
		return "not running"
	}
	_ = conn.Close()
	return fmt.Sprintf("running (answering on port %d; no live PID in %s)", port, server.PIDPath())
}

// doctorBootSummary is the running server's boot summary from the report it
// saves, or "" when no server for cfgPath is running or it saved none.
func doctorBootSummary(cfgPath string) string {
	running, pid, _ := configServerRunning(cfgPath)
	if !running {
		return ""
	}
	r, err := startup.Load()
	if err != nil || r.PID != pid {
		return ""
	}
	return r.Summary()
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
	_, _ = fmt.Fprintf(os.Stdout, "  Install type   : %s\n", update.DescribeInstall())
	_, _ = fmt.Fprintf(os.Stdout, "  Go version     : %s\n", runtime.Version())
	_, _ = fmt.Fprintf(os.Stdout, "  OS/Arch        : %s/%s\n", runtime.GOOS, runtime.GOARCH)

	wd, _ := os.Getwd()
	_, _ = fmt.Fprintf(os.Stdout, "  Working dir    : %s\n", wd)
	_, _ = fmt.Fprintf(os.Stdout, "  Config file    : %s\n", cfgPath)
	_, _ = fmt.Fprintf(os.Stdout, "  Data dir       : %s\n", store.DataDir())

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "  Server         : %s\n", doctorServerStatus(cfgPath, cfg.Server.Port))
	if boot := doctorBootSummary(cfgPath); boot != "" {
		_, _ = fmt.Fprintf(os.Stdout, "  Startup        : %s\n", boot)
	}

	// ── Config file ───────────────────────────────────────────────────────────
	_, _ = fmt.Fprintln(os.Stdout)
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		_, _ = fmt.Fprintln(os.Stdout, "[WARN] config: file not found; using built-in defaults")
		_, _ = fmt.Fprintln(os.Stdout, "       Run 'aviary configure' to create one.")
		_, _ = fmt.Fprintln(os.Stdout)
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

	// The CLI registry drives both the CLI backends probe and the
	// Code Loop agents gates, so the doctor reports from the same definitions
	// System → Providers & Auth and the Code Loop doctor read.
	registry, registryErr := clidefs.FromConfig(cfg.Models.CLIDefinitions)

	// ── CLI backends ─────────────────────────────────────────────────────────
	if backends := config.CLIBackends(cfg); len(backends) > 0 && registryErr == nil {
		_, _ = fmt.Fprintln(os.Stdout)
		_, _ = fmt.Fprintln(os.Stdout, "CLI backends")
		_, _ = fmt.Fprintln(os.Stdout, "────────────")
		doctor := clidoctor.New(registry)
		for _, b := range backends {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			ok, line := describeCLIBackend(doctor.Probe(ctx, string(b)))
			cancel()
			level := "OK"
			if !ok {
				level = "ERROR"
				nerrs++
			}
			_, _ = fmt.Fprintf(os.Stdout, "  %-12s [%s] %s\n", b, level, line)
		}
	}

	// ── Code Loop agents ──────────────────────────────────────────────────────
	// Agents gated out of Code Loop dispatch are listed with the reason, so a
	// job that fails to start on one is explained before it is dispatched.
	_, _ = fmt.Fprintln(os.Stdout)
	_, _ = fmt.Fprintln(os.Stdout, "Code Loop agents")
	_, _ = fmt.Fprintln(os.Stdout, "───────────────")
	if registryErr != nil {
		_, _ = fmt.Fprintf(os.Stdout, "  [ERROR] %v\n", registryErr)
		nerrs++
	} else {
		writeProjectsAgentGates(os.Stdout, registry)
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

// describeCLIBackend renders one CLI backend's probe for the doctor. A
// backend is usable only when its CLI is installed and signed in, since a
// chat turn through a signed-out CLI fails before it starts.
func describeCLIBackend(report clidoctor.Report) (bool, string) {
	if !report.Installed() {
		return false, fmt.Sprintf("%s CLI not found in PATH", report.Binary)
	}
	line := report.Binary
	if v := version.Find(report.Version); v != "" {
		line += " " + v
	} else if report.Version != "" {
		line += " " + report.Version
	}
	switch report.Auth {
	case clidoctor.SignedIn:
		line += ", signed in"
	case clidoctor.SignedOut:
		line += ", not signed in"
	default:
		line += ", sign-in unknown"
	}
	if report.AuthDetail != "" {
		line += " (" + report.AuthDetail + ")"
	}
	if report.TracksQuota {
		line += "; quota " + report.QuotaText()
	}
	if report.VersionNote != "" {
		line += "; version " + report.VersionNote
	}
	return report.Auth == clidoctor.SignedIn && report.VersionNote == "", line
}

// writeProjectsAgentGates lists every agent the registry gates out of Code Loop
// dispatch, with the reason its definition gives.
func writeProjectsAgentGates(out io.Writer, registry *clidefs.Registry) {
	gated := 0
	for _, name := range registry.Names() {
		definition, _ := registry.Lookup(name)
		if definition.ProjectsUnavailable == "" {
			continue
		}
		gated++
		_, _ = fmt.Fprintf(out, "  %-12s [INFO] unavailable for Code Loop: %s\n", name, definition.ProjectsUnavailable)
	}
	if gated == 0 {
		_, _ = fmt.Fprintln(out, "  (every agent can run Code Loop jobs)")
	}
}
