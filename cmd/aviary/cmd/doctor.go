package cmd

import (
	"cmp"
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

	"github.com/lsegal/aviary/internal/browser"
	"github.com/lsegal/aviary/internal/buildinfo"
	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/clidoctor"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/server"
	"github.com/lsegal/aviary/internal/sessiontarget"
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

Also lists orphaned sessions: task and channel sessions whose task, workflow
or channel was deleted. --fix archives them as regular chats.

Exits with status 1 if any errors are found.`,
	RunE: runDoctor,
}

// newDoctorCLIProbe builds the doctor that probes CLI backends, and
// doctorPingModel checks one provider model's credentials. Tests replace
// them so the doctor's output can be checked without real CLIs or network.
var (
	newDoctorCLIProbe = clidoctor.New
	doctorPingModel   = func(ctx context.Context, factory *llm.Factory, model string) error {
		return factory.PingModel(ctx, model)
	}
)

func init() {
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "Archive orphaned sessions as regular chats")
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

	wd, cfg, err := writeDoctorSystemInfo(cfgPath)
	if err != nil {
		return err
	}

	// ── Config file ───────────────────────────────────────────────────────────
	_, _ = fmt.Fprintln(os.Stdout)
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		_, _ = fmt.Fprintln(os.Stdout, "[WARN] config: file not found; using built-in defaults")
		_, _ = fmt.Fprintln(os.Stdout, "       Run 'aviary configure' to create one.")
		_, _ = fmt.Fprintln(os.Stdout)
	}

	writeDoctorAgents(cfg, wd)

	st := authStore()
	nerrs, nwarns := writeDoctorValidation(cfg, st.Get)
	orphanErrs, orphanWarns := writeDoctorOrphans(cfg)
	nerrs += orphanErrs
	nwarns += orphanWarns
	nerrs += writeDoctorModelCredentials(cfg, st.Get)

	// The CLI registry drives both the CLI backends probe and the
	// Code Loop agents gates, so the doctor reports from the same definitions
	// System → Providers & Auth and the Code Loop doctor read.
	registry, registryErr := clidefs.FromConfig(cfg.Models.CLIDefinitions)
	if registryErr == nil {
		nerrs += writeDoctorCLIBackends(cfg, registry)
	}
	nerrs += writeDoctorCodeLoopAgents(registry, registryErr)
	nerrs += writeDoctorBrowserProfile(cfg)

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

// writeDoctorSection prints a blank line followed by an underlined heading.
func writeDoctorSection(title, underline string) {
	_, _ = fmt.Fprintln(os.Stdout)
	_, _ = fmt.Fprintln(os.Stdout, title)
	_, _ = fmt.Fprintln(os.Stdout, underline)
}

// writeDoctorSystemInfo prints the system information block and returns the
// working directory and the loaded config.
func writeDoctorSystemInfo(cfgPath string) (string, *config.Config, error) {
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
		return wd, nil, fmt.Errorf("parsing config: %w", err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "  Server         : %s\n", doctorServerStatus(cfgPath, cfg.Server.Port))
	if boot := doctorBootSummary(cfgPath); boot != "" {
		_, _ = fmt.Fprintf(os.Stdout, "  Startup        : %s\n", boot)
	}
	return wd, cfg, nil
}

// writeDoctorAgents lists the configured agents with their effective model,
// permission preset and working directory.
func writeDoctorAgents(cfg *config.Config, wd string) {
	_, _ = fmt.Fprintln(os.Stdout, "Configured agents")
	_, _ = fmt.Fprintln(os.Stdout, "─────────────────")
	if len(cfg.Agents) == 0 {
		_, _ = fmt.Fprintln(os.Stdout, "  (none)")
	}
	for _, a := range cfg.Agents {
		model := cmp.Or(a.Model, "(default)")
		perm := "standard"
		if a.Permissions != nil && a.Permissions.Preset != "" {
			perm = string(a.Permissions.Preset)
		}
		wdir := cmp.Or(a.WorkingDir, wd)
		_, _ = fmt.Fprintf(os.Stdout, "  %-20s model=%-30s permissions=%s  dir=%s\n",
			a.Name, model, perm, wdir)
	}
}

// writeDoctorValidation prints config validation issues and returns the
// error and warning counts.
func writeDoctorValidation(cfg *config.Config, getSecret func(string) (string, error)) (nerrs, nwarns int) {
	writeDoctorSection("Validation", "──────────")
	issues := config.Validate(cfg, getSecret)
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
	return nerrs, nwarns
}

// writeDoctorOrphans prints orphaned sessions and returns the error and
// warning counts.
func writeDoctorOrphans(cfg *config.Config) (nerrs, nwarns int) {
	writeDoctorSection("Orphaned sessions", "─────────────────")
	orphans, err := writeOrphanedSessions(os.Stdout, cfg, doctorFix)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stdout, "  [ERROR] %v\n", err)
		return 1, 0
	}
	return 0, orphans
}

// writeDoctorModelCredentials pings each configured provider model and
// returns how many failed.
func writeDoctorModelCredentials(cfg *config.Config, getSecret func(string) (string, error)) int {
	writeDoctorSection("Model credentials", "─────────────────")
	factory := llm.NewFactory(func(ref string) (string, error) {
		key := strings.TrimPrefix(ref, "auth:")
		return getSecret(key)
	})
	providerModels := config.UniqueProviderModels(cfg)
	if len(providerModels) == 0 {
		_, _ = fmt.Fprintln(os.Stdout, "  (no models configured)")
	}
	nerrs := 0
	for provider, model := range providerModels {
		_, _ = fmt.Fprintf(os.Stdout, "  %-12s ", provider)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		pingErr := doctorPingModel(ctx, factory, model)
		cancel()
		if pingErr != nil {
			_, _ = fmt.Fprintf(os.Stdout, "[ERROR] %v\n", pingErr)
			nerrs++
		} else {
			_, _ = fmt.Fprintln(os.Stdout, "[OK]")
		}
	}
	return nerrs
}

// writeDoctorCLIBackends probes each configured CLI backend and returns how
// many failed. It prints nothing when no CLI backends are configured.
func writeDoctorCLIBackends(cfg *config.Config, registry *clidefs.Registry) int {
	backends := config.CLIBackends(cfg)
	if len(backends) == 0 {
		return 0
	}
	writeDoctorSection("CLI backends", "────────────")
	doctor := newDoctorCLIProbe(registry)
	nerrs := 0
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
	return nerrs
}

// writeDoctorCodeLoopAgents lists agents gated out of Code Loop dispatch with
// the reason, so a job that fails to start on one is explained before it is
// dispatched. It returns 1 when the CLI registry could not be built.
func writeDoctorCodeLoopAgents(registry *clidefs.Registry, registryErr error) int {
	writeDoctorSection("Code Loop agents", "───────────────")
	if registryErr != nil {
		_, _ = fmt.Fprintf(os.Stdout, "  [ERROR] %v\n", registryErr)
		return 1
	}
	writeProjectsAgentGates(os.Stdout, registry)
	return 0
}

// writeDoctorBrowserProfile reports the browser profile directory status and
// returns 1 when it could not be inspected.
func writeDoctorBrowserProfile(cfg *config.Config) int {
	writeDoctorSection("Browser profile", "───────────────")
	profile := strings.TrimSpace(cfg.Browser.ProfileDir)
	if profile == "" {
		profile = browser.DefaultProfileDir()
	}
	_, _ = fmt.Fprintf(os.Stdout, "  Directory : %s\n", profile)
	status, statusErr := browser.InspectProfile(profile)
	if statusErr != nil {
		_, _ = fmt.Fprintf(os.Stdout, "  [ERROR] %v\n", statusErr)
		return 1
	}
	_, _ = fmt.Fprintf(os.Stdout, "  %s\n", describeBrowserProfile(status))
	return 0
}

// writeOrphanedSessions lists the task and channel sessions whose owner was
// deleted, which would otherwise show in Chats as if it still existed, and
// archives them as regular chats when fix is set. It returns how many it
// left in place.
func writeOrphanedSessions(w io.Writer, cfg *config.Config, fix bool) (int, error) {
	orphans, err := sessiontarget.FindOrphans(cfg)
	if err != nil {
		return 0, err
	}
	if len(orphans) == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")
		return 0, nil
	}
	if fix {
		if err := sessiontarget.ReleaseOrphans(orphans, false); err != nil {
			return 0, err
		}
	}
	for _, o := range orphans {
		if fix {
			_, _ = fmt.Fprintf(w, "  [FIXED] %s/%s: archived as %q\n", o.Agent, o.Title, o.Title+" (archived)")
		} else {
			_, _ = fmt.Fprintf(w, "  [WARN] %s/%s: its task, workflow or channel no longer exists\n", o.Agent, o.Title)
		}
	}
	if fix {
		return 0, nil
	}
	_, _ = fmt.Fprintln(w, "  Run 'aviary doctor --fix' to archive them as regular chats.")
	return len(orphans), nil
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

// describeBrowserProfile renders the browser profile lock's state for the
// doctor. The lock is the operating system's own, so it cannot outlive the
// process holding it: a holder named here is still running, and stopping it is
// what frees the profile. A record left behind by a run that exited is cleared
// by the inspection itself.
func describeBrowserProfile(status browser.ProfileStatus) string {
	switch {
	case status.Held && status.Owner != nil:
		return fmt.Sprintf("[INFO] lock held by a running Aviary instance (%s); stop that instance to free the profile", status.Owner)
	case status.Held:
		return "[INFO] lock held by a running Aviary instance that left no details; stop that instance to free the profile"
	}
	line := "[OK] lock free"
	if status.Reclaimed && status.Owner != nil {
		line += fmt.Sprintf("; cleared a stale lock record left by an exited run (%s)", status.Owner)
	}
	if status.ChromeRunning {
		line += "; Aviary's browser is running on it"
	}
	return line
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
