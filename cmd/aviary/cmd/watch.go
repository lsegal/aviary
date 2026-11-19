package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects"
	"github.com/lsegal/aviary/internal/server"
)

// watchOptions holds the watch command flags.
type watchOptions struct {
	agents            []string
	concurrency       int
	interval          string
	delivery          string
	readyLabel        string
	readyState        string
	filter            []string
	allIssues         bool
	allowedCommenters []string
	yolo              bool
	noMerge           bool
	noChangelog       bool
	browserVision     bool
	browserVisible    bool
}

var watchOpts watchOptions

var watchCmd = &cobra.Command{
	Use:   "watch [TARGET ...]",
	Short: "Watch GitHub projects and fix ready issues with a coding CLI",
	Long: `Watch repositories, GitHub Project boards and Discussions boards, and run
gh-fix (or gh-discuss for new Discussion threads) in a coding CLI on every
issue labeled ready, up to a concurrency limit.

With no TARGET the current checkout's origin repository is watched. TARGET is
OWNER/REPO, a repository URL, a Project board URL, a Discussions URL, or the
projects: and discussions: shorthands.

Nothing is written to aviary.yaml: the watch lasts until Ctrl+C. When the
Aviary server is running the watch is added to it; otherwise the server is
started in the foreground for the watch. Flags override the projects: section
of aviary.yaml while the watch runs. Use 'aviary project add' to watch a
project permanently.`,
	RunE: runWatch,
}

func init() {
	f := watchCmd.Flags()
	f.StringArrayVar(&watchOpts.agents, "agent", nil, "coding CLI as AGENT/MODEL:LEVEL, such as codex or claude/opus (repeat for a fallback order)")
	f.IntVar(&watchOpts.concurrency, "concurrency", 0, "jobs that run at once (default 3)")
	f.StringVar(&watchOpts.interval, "interval", "", "time between GitHub polls (default 20s for browser delivery, 30s otherwise)")
	f.StringVar(&watchOpts.delivery, "delivery", "", "browser, poll or webhook (default browser: read GitHub through Aviary's browser, signed in as you)")
	f.StringVar(&watchOpts.readyLabel, "ready-label", "", `label a repository issue needs (default "ready")`)
	f.StringVar(&watchOpts.readyState, "ready-state", "", "project board status that marks an item ready (default Todo or Ready)")
	f.StringArrayVar(&watchOpts.filter, "filter", nil, `issue search filter, repeatable (default "assignee:@me author:@me")`)
	f.BoolVar(&watchOpts.allIssues, "all-issues", false, "drop the issue filter; the ready label still applies")
	f.StringSliceVar(&watchOpts.allowedCommenters, "allowed-commenters", nil, "GitHub logins whose comments steer jobs (default the authenticated gh user)")
	f.BoolVar(&watchOpts.yolo, "yolo", false, "disable the coding CLIs' sandboxes and permission checks")
	f.BoolVar(&watchOpts.noMerge, "no-merge", false, "leave finished pull requests ready for a human to merge")
	f.BoolVar(&watchOpts.noChangelog, "no-changelog", false, "skip changelog entries in gh-fix pull requests")
	f.BoolVar(&watchOpts.browserVision, "browser-vision", false, "browser delivery: read pages it no longer recognizes from capped screenshots")
	f.BoolVar(&watchOpts.browserVisible, "browser-visible", false, "browser delivery: show the browser it reads with, for debugging")
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
func watchSettings(cmd *cobra.Command) (config.ProjectsConfig, error) {
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
	if cmd.Flags().Changed("ready-label") {
		label := watchOpts.readyLabel
		settings.ReadyLabel = &label
	}
	return settings, nil
}

func runWatch(cmd *cobra.Command, args []string) error {
	targets, err := watchTargets(args)
	if err != nil {
		return err
	}
	settings, err := watchSettings(cmd)
	if err != nil {
		return err
	}
	watch := projects.Watch{Targets: targets, Agents: watchOpts.agents, Settings: settings}
	running, _, err := server.IsRunning()
	if err != nil {
		return fmt.Errorf("checking server status: %w", err)
	}
	if !running {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Watching %s; starting the Aviary server in the foreground (Ctrl+C stops both).\n", strings.Join(targets, ", "))
		server.SetStartupWatches([]projects.Watch{watch})
		return runStart(nil, nil)
	}
	return attachWatch(cmd.Context(), cmd.OutOrStdout(), watch)
}

// attachWatch adds the watch to the running server, prints its log until
// Ctrl+C, and then removes the watch again.
func attachWatch(ctx context.Context, out io.Writer, watch projects.Watch) error {
	if ctx == nil {
		ctx = context.Background()
	}
	d := activeDispatcher()
	payload := func(target string) map[string]any {
		data, _ := json.Marshal(watch.Settings)
		var settings map[string]any
		_ = json.Unmarshal(data, &settings)
		args := map[string]any{"target": target, "runtime": true}
		if len(watch.Agents) > 0 {
			args["agents"] = watch.Agents
		}
		if len(settings) > 0 {
			args["settings"] = settings
		}
		return args
	}
	var added []string
	defer func() {
		for _, target := range added {
			if _, err := d.CallTool(context.Background(), "project_remove", map[string]any{"target": target, "runtime": true}); err != nil {
				_, _ = fmt.Fprintf(out, "stop watching %s: %v\n", target, err)
			}
		}
	}()
	for _, target := range watch.Targets {
		if _, err := d.CallTool(ctx, "project_add", payload(target)); err != nil {
			return fmt.Errorf("watch %s: %w", target, err)
		}
		added = append(added, target)
	}
	_, _ = fmt.Fprintf(out, "Watching %s on the running Aviary server (Ctrl+C stops the watch).\n", strings.Join(watch.Targets, ", "))

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	offset := 0.0
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		raw, err := d.CallTool(ctx, "project_status", map[string]any{"log_offset": int(offset)})
		if err == nil {
			var status struct {
				Logs   []string `json:"recent_logs"`
				Offset float64  `json:"log_offset"`
			}
			if json.Unmarshal([]byte(raw), &status) == nil {
				for _, line := range status.Logs {
					_, _ = fmt.Fprintln(out, line)
				}
				offset = status.Offset
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
