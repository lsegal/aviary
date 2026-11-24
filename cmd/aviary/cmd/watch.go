package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/logging"
	internalmcp "github.com/lsegal/aviary/internal/mcp"
	"github.com/lsegal/aviary/internal/projects"
	"github.com/lsegal/aviary/internal/server"
)

// watchOptions holds the watch command flags.
type watchOptions struct {
	agent             string
	concurrency       int
	interval          string
	delivery          string
	readyState        string
	filter            []string
	allIssues         bool
	allowedCommenters []string
	yolo              bool
	noMerge           bool
	noChangelog       bool
	browserVision     bool
	browserVisible    bool
	noWebUI           bool
	noTUI             bool
}

var watchOpts watchOptions

var watchCmd = &cobra.Command{
	Use:   "watch [TARGET ...]",
	Short: "Watch GitHub projects and fix ready issues with a CLI provider",
	Long: `Watch repositories, GitHub Project boards and Discussions boards, and run
gh-fix (or gh-discuss for new Discussion threads) in a CLI provider on every
issue matching the issue filter, up to a concurrency limit.

With no TARGET the current checkout's origin repository is watched. TARGET is
OWNER/REPO, a repository URL, a Project board URL, a Discussions URL, or the
projects: and discussions: shorthands.

A watch is a one-off session that never changes your Aviary config. It runs
its own Aviary server against a temporary config and data directory
(aviary-watch-<PID>-* in the system temp directory), holding the session's
config, state, sessions, jobs and clone bookkeeping. Only two things are
shared with your own Aviary, both read in place and never copied:

  - the browser profile, so your GitHub sign-in is reused;
  - provider credentials: CLI provider sign-ins, auth: references and tokens.

The session copies your providers, browser, approval and Code Loop settings;
flags override the Code Loop settings. The targets run under --agent, else the
agent of yours that owns them, else models.defaults. Use 'aviary project add'
to watch a project permanently.

The session's server listens on localhost only, on the first free port from
your server port (default 16677) up, and serves a minimal web UI with no token
prompt: Code Loop and Scheduled Tasks only. Chat, agent and channel settings,
System pages and their API are unavailable. A full-screen TUI shows the same
board in the terminal: status filters, scheduled tasks, board mode, job
actions, live logs and stats. --no-webui serves no web UI, and --no-tui prints
the Code Loop log instead of the TUI (the default when output is not a
terminal).

Ctrl+C, SIGTERM, SIGHUP, quitting the TUI or a fatal error ends the session:
running jobs are stopped and checkpointed, then the temporary directory is
removed. A directory left behind by a killed session is removed by the next
watch once its PID is gone.`,
	RunE: runWatch,
}

func init() {
	f := watchCmd.Flags()
	f.StringVar(&watchOpts.agent, "agent", "", "Aviary agent the targets run under; its model and fallbacks pick the jobs' CLI providers (default: the agent that owns the target, else models.defaults)")
	f.IntVar(&watchOpts.concurrency, "concurrency", 0, "jobs that run at once (default 3)")
	f.StringVar(&watchOpts.interval, "interval", "", "time between GitHub polls (default 20s for browser delivery, 30s otherwise)")
	f.StringVar(&watchOpts.delivery, "delivery", "", "browser, poll or webhook (default browser: read GitHub through Aviary's browser, signed in as you)")
	f.StringVar(&watchOpts.readyState, "ready-state", "", "project board status that marks an item ready (default Todo or Ready)")
	f.StringArrayVar(&watchOpts.filter, "filter", nil, `issue search filter, repeatable (default "assignee:@me")`)
	f.BoolVar(&watchOpts.allIssues, "all-issues", false, "drop the issue filter and pick up every open issue")
	f.StringSliceVar(&watchOpts.allowedCommenters, "allowed-commenters", nil, "GitHub logins whose comments steer jobs (default the authenticated gh user)")
	f.BoolVar(&watchOpts.yolo, "yolo", false, "run jobs in the full approval mode: no CLI sandboxes or permission checks")
	f.BoolVar(&watchOpts.noMerge, "no-merge", false, "leave finished pull requests ready for a human to merge")
	f.BoolVar(&watchOpts.noChangelog, "no-changelog", false, "skip changelog entries in gh-fix pull requests")
	f.BoolVar(&watchOpts.browserVision, "browser-vision", false, "browser delivery: read pages it no longer recognizes from capped screenshots")
	f.BoolVar(&watchOpts.browserVisible, "browser-visible", false, "browser delivery: show the browser it reads with, for debugging")
	f.BoolVar(&watchOpts.noWebUI, "no-webui", false, "don't serve the session's web UI")
	f.BoolVar(&watchOpts.noTUI, "no-tui", false, "print the Code Loop log instead of showing the TUI")
	rootCmd.AddCommand(watchCmd)
}

// watchTargets resolves the command's targets, defaulting to the current
// checkout's origin repository.
func watchTargets(args []string) ([]string, error) {
	if len(args) == 0 {
		repo, ok := projects.OriginTarget()
		if !ok {
			return nil, errors.New("no TARGET given and the current directory has no GitHub origin remote")
		}
		args = []string{repo}
	}
	targets := make([]string, 0, len(args))
	for _, arg := range args {
		target, err := projects.ExpandTarget(arg)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

// watchSettings turns the flags into the settings the watch overrides.
func watchSettings() (config.ProjectsConfig, error) {
	settings := config.ProjectsConfig{
		Concurrency:       watchOpts.concurrency,
		Interval:          watchOpts.interval,
		ReadyState:        watchOpts.readyState,
		Filter:            watchOpts.filter,
		AllIssues:         watchOpts.allIssues,
		AllowedCommenters: watchOpts.allowedCommenters,
		Yolo:              watchOpts.yolo,
		NoMerge:           watchOpts.noMerge,
		NoChangelog:       watchOpts.noChangelog,
		BrowserVision:     watchOpts.browserVision,
		BrowserVisible:    watchOpts.browserVisible,
	}
	if watchOpts.concurrency < 0 {
		return settings, errors.New("--concurrency cannot be negative")
	}
	if watchOpts.interval != "" {
		if d, err := time.ParseDuration(watchOpts.interval); err != nil || d <= 0 {
			return settings, fmt.Errorf("--interval %q is not a positive duration", watchOpts.interval)
		}
	}
	switch delivery := strings.ToLower(watchOpts.delivery); delivery {
	case "", projects.DeliveryBrowser:
		settings.Delivery = delivery
	case projects.DeliveryPoll, projects.DeliveryWebhook:
		if watchOpts.browserVision || watchOpts.browserVisible {
			return settings, fmt.Errorf("--browser-vision and --browser-visible apply only to browser delivery, not %s", delivery)
		}
		settings.Delivery = delivery
	default:
		return settings, fmt.Errorf("--delivery must be browser, poll or webhook, not %q", watchOpts.delivery)
	}
	return settings, nil
}

func runWatch(cmd *cobra.Command, args []string) (err error) {
	targets, err := watchTargets(args)
	if err != nil {
		return err
	}
	settings, err := watchSettings()
	if err != nil {
		return err
	}
	// Past the flags, an error is the session's, not a usage mistake.
	cmd.SilenceUsage = true
	out := cmd.OutOrStdout()
	sources, global, err := resolveGlobalWatchSources(cfgFile)
	if err != nil {
		return err
	}
	for _, dir := range sweepStaleWatchDirs(os.TempDir()) {
		_, _ = fmt.Fprintf(out, "Removed %s, left behind by an earlier watch.\n", dir)
	}
	session, err := startWatchSession(os.TempDir(), global, sources, watchOpts.agent, targets, settings)
	// The session directory goes on every way out, a panic included.
	defer func() {
		if cleanupErr := endWatchSession(session); cleanupErr != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), cleanupErr)
			if err == nil {
				err = cleanupErr
			}
		}
	}()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	tui := !watchOpts.noTUI && isatty.IsTerminal(os.Stdout.Fd())
	return runWatchSession(ctx, out, session, targets, tui, !watchOpts.noWebUI)
}

// watchSignals end a watch session.
var watchSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

// runWatchSession serves the session until a signal arrives, the TUI quits
// or the server fails, then stops it: Code Loop checkpoints running jobs and
// agents stop before this returns.
func runWatchSession(ctx context.Context, out io.Writer, session *watchSession, targets []string, tui, webUI bool) error {
	if err := logging.Init(); err != nil {
		_, _ = fmt.Fprintf(out, "warning: failed to initialize logging: %v\n", err)
	}
	cfg, err := config.Load(session.ConfigPath)
	if err != nil {
		return fmt.Errorf("loading the watch session config: %w", err)
	}
	tok, _, err := server.LoadOrGenerateToken()
	if err != nil {
		return fmt.Errorf("creating the session token: %w", err)
	}
	if err := server.WritePID(); err != nil {
		return fmt.Errorf("writing PID: %w", err)
	}
	defer server.RemovePID() //nolint:errcheck

	server.SetMinimalMode(true)
	server.SetWebUIEnabled(webUI)
	defer server.SetMinimalMode(false)
	defer server.SetWebUIEnabled(true)
	if webUI && !tui {
		if problem := server.WebAssetsProblem(); problem != "" {
			_, _ = fmt.Fprintf(out, "warning: %s\n", problem)
		}
	}

	removePortFile, err := writeWatchPortFile(session.Port)
	if err != nil {
		return err
	}
	defer removePortFile()

	ctx, stop := signal.NotifyContext(ctx, watchSignals...)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	srv := server.New(cfg, tok)
	served := make(chan error, 1)
	go func() { served <- serveWatch(ctx, srv, tok, session.ConfigPath) }()

	webURL := ""
	if webUI {
		webURL = fmt.Sprintf("%s/agents/%s/projects", watchWebBase(session.Port), url.PathEscape(session.Agent))
	}
	title := strings.Join(targets, ", ")
	if !tui {
		_, _ = fmt.Fprintf(out, "Watching %s in an isolated session (Ctrl+C stops it).\n", title)
		if webURL != "" {
			_, _ = fmt.Fprintf(out, "Code Loop: %s\n", webURL)
		}
	}
	call, closeCalls, err := watchToolCalls(ctx)
	if err != nil {
		cancel()
		serveErr := <-served
		if ctx.Err() != nil && errors.Is(err, context.Canceled) {
			// A signal arrived while the session was starting.
			err = serveErr
		}
		return err
	}
	defer closeCalls()

	if tui {
		tuiDone := make(chan error, 1)
		go func() { tuiDone <- runWatchTUI(ctx, call, title, webURL) }()
		select {
		case err = <-tuiDone:
			cancel()
			if serveErr := <-served; err == nil {
				err = serveErr
			}
		case err = <-served:
			cancel()
			<-tuiDone
		}
	} else {
		logsDone := make(chan struct{})
		go func() {
			printWatchLogs(ctx, out, call)
			close(logsDone)
		}()
		err = <-served
		cancel()
		<-logsDone
	}
	_, _ = fmt.Fprintln(out, "Watch session ended.")
	return err
}

// Environment variables scripts/dev-watch.mjs (pnpm dev:watch) sets to put
// the Vite dev server in front of a watch session.
const (
	// watchPortFileEnv names a file the session writes its server's port
	// to once it is chosen, and removes when the session ends, so the dev
	// server can proxy to whichever free port the session took.
	watchPortFileEnv = "AVIARY_WATCH_PORT_FILE"
	// watchWebURLEnv is the base URL the session's web UI is opened at in
	// place of the session's own server.
	watchWebURLEnv = "AVIARY_WATCH_WEB_URL"
)

// writeWatchPortFile writes port to the file watchPortFileEnv names, if any,
// and returns a func removing it.
func writeWatchPortFile(port int) (func(), error) {
	path := os.Getenv(watchPortFileEnv)
	if path == "" {
		return func() {}, nil
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(port)+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("writing %s: %w", watchPortFileEnv, err)
	}
	return func() { _ = os.Remove(path) }, nil
}

// watchWebBase is the base URL the session's web UI is opened at.
func watchWebBase(port int) string {
	if base := strings.TrimRight(os.Getenv(watchWebURLEnv), "/"); base != "" {
		return base
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// serveWatch runs the session's server until ctx ends, starting it again
// when a config change needs a restart.
func serveWatch(ctx context.Context, srv *server.Server, tok, configPath string) error {
	for {
		err := srv.ListenAndServe(ctx)
		if err == nil || ctx.Err() != nil {
			return shutdownErr(err)
		}
		if !errors.Is(err, server.ErrRestartRequired) {
			return err
		}
		cfg, loadErr := config.Load(configPath)
		if loadErr != nil {
			return fmt.Errorf("reloading the watch session config: %w", loadErr)
		}
		srv = server.New(cfg, tok)
	}
}

// watchToolCalls connects an in-process MCP client to the session's server,
// for the TUI and the log printer.
func watchToolCalls(ctx context.Context) (watchToolCaller, func(), error) {
	client, err := internalmcp.NewInProcessClient(ctx, internalmcp.NewServer())
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to the watch session: %w", err)
	}
	call := func(ctx context.Context, name string, args map[string]any) (string, error) {
		return client.CallToolText(ctx, name, args)
	}
	return call, func() { _ = client.Close() }, nil
}

// printWatchLogs prints the Code Loop log until ctx ends.
func printWatchLogs(ctx context.Context, out io.Writer, call watchToolCaller) {
	offset := 0
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		raw, err := call(ctx, "project_status", map[string]any{"log_offset": offset})
		if err == nil {
			var status struct {
				Logs   []string `json:"recent_logs"`
				Offset float64  `json:"log_offset"`
			}
			if json.Unmarshal([]byte(raw), &status) == nil {
				for _, line := range status.Logs {
					_, _ = fmt.Fprintln(out, line)
				}
				offset = int(status.Offset)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
