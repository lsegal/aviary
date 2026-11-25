package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/config"
	internalmcp "github.com/lsegal/aviary/internal/mcp"
	"github.com/lsegal/aviary/internal/server"
	"github.com/lsegal/aviary/internal/store"
)

func TestWatchTargets(t *testing.T) {
	checkout := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "https://github.com/aviary-test/from-origin.git"}} {
		git := exec.Command("git", args...)
		git.Dir = checkout
		if out, err := git.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	plain := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(plain))
	tests := []struct {
		name      string
		dir       string
		args      []string
		want      []string
		wantError string
	}{
		{name: "origin repository by default", dir: checkout, want: []string{"aviary-test/from-origin"}},
		{name: "shorthand uses the origin", dir: checkout, args: []string{"projects:7", "o/r"}, want: []string{"https://github.com/aviary-test/from-origin/projects/7", "o/r"}},
		{name: "no origin and no target", dir: plain, wantError: "no TARGET given and the current directory has no GitHub origin remote"},
		{name: "bad shorthand", dir: plain, args: []string{"o/r", "projects:a/b/c/d"}, wantError: `target "projects:a/b/c/d" must be projects:OWNER/REPO/NUMBER`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(tt.dir)
			got, err := watchTargets(tt.args)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %v, want %q", err, tt.wantError)
				}
				return
			}
			if err != nil || !slices.Equal(got, tt.want) {
				t.Errorf("watchTargets(%v) = %v, %v; want %v", tt.args, got, err, tt.want)
			}
		})
	}
}

// setWatchOpts sets the watch flags until the test ends.
func setWatchOpts(t *testing.T, opts watchOptions) {
	t.Helper()
	old := watchOpts
	watchOpts = opts
	t.Cleanup(func() { watchOpts = old })
}

// TestWatchSettingsFromFlags covers the delivery modes; these are the
// numbers it leaves out.
func TestWatchSettingsRejectsBadNumbers(t *testing.T) {
	tests := []struct {
		name      string
		opts      watchOptions
		wantError string
	}{
		{name: "negative concurrency", opts: watchOptions{concurrency: -1}, wantError: "--concurrency cannot be negative"},
		{name: "zero interval", opts: watchOptions{interval: "0s"}, wantError: `--interval "0s" is not a positive duration`},
		{name: "negative interval", opts: watchOptions{interval: "-5m"}, wantError: `--interval "-5m" is not a positive duration`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setWatchOpts(t, tt.opts)
			if _, err := watchSettings(); err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestRunWatchFailsBeforeTheSession(t *testing.T) {
	configHome := t.TempDir()
	globalDir := writeGlobalAviary(t, configHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	store.SetDataDir(globalDir)
	t.Cleanup(func() { store.SetDataDir(dataDir) })
	badConfig := filepath.Join(t.TempDir(), "aviary.yaml")
	if err := os.WriteFile(badConfig, []byte("agents: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, tmp)
	}
	stale := filepath.Join(tmp, fmt.Sprintf("%s%d-abc", watchDirPrefix, deadPID(t)))
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	oldCfgFile := cfgFile
	t.Cleanup(func() { cfgFile = oldCfgFile })

	tests := []struct {
		name      string
		targets   []string
		flags     []string
		opts      watchOptions
		config    string
		wantError string
		// usage is whether the error is a usage mistake cobra shows the
		// usage for; out is what the command prints.
		usage bool
		out   string
	}{
		{name: "bad target", targets: []string{"projects:a/b/c/d"}, wantError: "must be projects:OWNER/REPO/NUMBER", usage: true},
		{name: "bad flag", targets: []string{"o/r"}, opts: watchOptions{concurrency: -2}, wantError: "--concurrency cannot be negative", usage: true},
		{name: "bad model", targets: []string{"o/r"}, flags: []string{"--model", "opus"}, wantError: `--model "opus" is not a model`, usage: true},
		{name: "unreadable config", targets: []string{"o/r"}, config: badConfig, wantError: "reading your Aviary config"},
		{
			name: "unknown agent", targets: []string{"o/r"}, flags: []string{"--agent", "nosuch"}, config: filepath.Join(globalDir, "aviary.yaml"),
			wantError: `--agent "nosuch" is not an agent in your Aviary config`, out: fmt.Sprintf("Removed %s, left behind by an earlier watch.\n", stale),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setWatchOpts(t, tt.opts)
			cfgFile = tt.config
			cmd := &cobra.Command{}
			cmd.Flags().AddFlagSet(watchAgentFlags(t, tt.flags...))
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			err := runWatch(cmd, tt.targets)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want %q", err, tt.wantError)
			}
			if cmd.SilenceUsage == tt.usage {
				t.Errorf("SilenceUsage = %v for %q", cmd.SilenceUsage, err)
			}
			if out.String() != tt.out {
				t.Errorf("output = %q, want %q", out.String(), tt.out)
			}
			if got := store.DataDir(); got != dataDir && got != globalDir {
				t.Errorf("the data dir is left at %s", got)
			}
		})
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("the stale session dir is still there: %v", err)
	}
}

// freeLoopbackPort returns a port nothing listens on at 127.0.0.1.
func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// newServableWatchSession starts a watch session with no targets on a free
// port from a user config in a temp dir, ended when the test ends.
func newServableWatchSession(t *testing.T) *watchSession {
	t.Helper()
	configHome := t.TempDir()
	globalDir := writeGlobalAviary(t, configHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv(watchPortFileEnv, "")
	t.Setenv(watchWebURLEnv, "")
	store.SetDataDir(globalDir)
	t.Cleanup(func() { store.SetDataDir(dataDir) })
	sources, global, err := resolveGlobalWatchSources(filepath.Join(globalDir, "aviary.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	global.Server.Port = freeLoopbackPort(t)
	session, err := startWatchSession(t.TempDir(), global, sources, watchAgentChoice{}, nil, config.ProjectsConfig{Delivery: "poll", Interval: "1h"})
	t.Cleanup(func() { endWatchSession(session, io.Discard) })
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestRunWatchSessionSetupErrors(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, s *watchSession)
		wantError string
	}{
		{name: "unreadable config", setup: func(t *testing.T, s *watchSession) {
			if err := os.WriteFile(s.ConfigPath, []byte("agents: [\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, wantError: "loading the watch session config"},
		{name: "unreadable token", setup: func(t *testing.T, s *watchSession) {
			if err := os.Mkdir(filepath.Join(s.Dir, "token"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, wantError: "creating the session token"},
		{name: "unwritable PID file", setup: func(t *testing.T, s *watchSession) {
			t.Setenv("AVIARY_PID_FILE", filepath.Join(s.ConfigPath, "aviary.pid"))
		}, wantError: "writing PID"},
		{name: "unwritable port file", setup: func(t *testing.T, s *watchSession) {
			t.Setenv(watchPortFileEnv, filepath.Join(s.Dir, "missing", "port"))
		}, wantError: "writing " + watchPortFileEnv},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := newServableWatchSession(t)
			tt.setup(t, session)
			var out bytes.Buffer
			err := runWatchSession(context.Background(), &out, session, []string{"o/r"}, false, true)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want %q", err, tt.wantError)
			}
			if strings.Contains(out.String(), "Watching") {
				t.Errorf("the session started serving: %q", out.String())
			}
			if server.MinimalMode() {
				t.Error("the session left the server in minimal mode")
			}
		})
	}
}

func TestRunWatchSessionEnds(t *testing.T) {
	tests := []struct {
		name  string
		tui   bool
		webUI bool
		// keys is what the user types into the TUI; portTaken makes the
		// session's server fail to start.
		keys      string
		portTaken bool
		wantOut   []string
		noOut     []string
	}{
		{
			name: "server fails without the TUI", webUI: true, portTaken: true,
			wantOut: []string{"Watching o/r, x/y in an isolated session (Ctrl+C stops it).\n", "Code Loop: http://127.0.0.1:%d/agents/watch/projects\n"},
		},
		{
			name: "server fails without the TUI or web UI", portTaken: true,
			wantOut: []string{"Watching o/r, x/y in an isolated session"}, noOut: []string{"Code Loop:"},
		},
		{name: "server fails under the TUI", tui: true, webUI: true, portTaken: true, noOut: []string{"Watching"}},
		{name: "quitting the TUI", tui: true, webUI: true, keys: "q", noOut: []string{"Watching"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := newServableWatchSession(t)
			if tt.portTaken {
				ln, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", session.Port))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = ln.Close() })
			}
			var screen bytes.Buffer
			setWatchTUITerminal(t, strings.NewReader(tt.keys), &screen)
			out := &lockedBuffer{}
			err := runWatchSession(context.Background(), out, session, []string{"o/r", "x/y"}, tt.tui, tt.webUI)
			if tt.portTaken {
				if want := fmt.Sprintf("listening on 127.0.0.1:%d", session.Port); err == nil || !strings.Contains(err.Error(), want) {
					t.Errorf("error = %v, want %q", err, want)
				}
			} else if err != nil {
				t.Errorf("error = %v", err)
			}
			got := out.String()
			if !strings.HasSuffix(got, "Watch session ended.\n") || strings.Count(got, "Watch session ended.") != 1 {
				t.Errorf("output does not end the session once: %q", got)
			}
			for _, want := range tt.wantOut {
				if strings.Contains(want, "%d") {
					want = fmt.Sprintf(want, session.Port)
				}
				if !strings.Contains(got, want) {
					t.Errorf("output misses %q: %q", want, got)
				}
			}
			for _, no := range tt.noOut {
				if strings.Contains(got, no) {
					t.Errorf("output has %q: %q", no, got)
				}
			}
			if drew := strings.Contains(screen.String(), watchModifyOtherKeysOn); drew != tt.tui {
				t.Errorf("TUI drawn = %v, want %v", drew, tt.tui)
			}
			if server.MinimalMode() {
				t.Error("the session left the server in minimal mode")
			}
		})
	}
}

func TestWatchStreamCalls(t *testing.T) {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test"}, nil)
	sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "reply"}, func(ctx context.Context, req *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		_ = req.Session.NotifyProgress(ctx, &sdkmcp.ProgressNotificationParams{ProgressToken: req.Params.GetProgressToken(), Progress: 1, Message: "thinking"})
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "hello "}, &sdkmcp.TextContent{Text: "there"}}}, struct{}{}, nil
	})
	sdkmcp.AddTool(srv, &sdkmcp.Tool{Name: "refuse"}, func(context.Context, *sdkmcp.CallToolRequest, struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return &sdkmcp.CallToolResult{IsError: true, Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "  provider unavailable\n"}}}, struct{}{}, nil
	})
	client, err := internalmcp.NewInProcessClient(context.Background(), srv)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	stream := watchStreamCalls(client)

	tests := []struct {
		tool         string
		want         string
		wantError    string
		wantProgress []string
	}{
		{tool: "reply", want: "hello there", wantProgress: []string{"thinking"}},
		{tool: "refuse", wantError: "provider unavailable"},
		{tool: "nosuch", wantError: "nosuch"},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			var progress []string
			got, err := stream(context.Background(), tt.tool, map[string]any{}, func(message string) {
				mu.Lock()
				defer mu.Unlock()
				progress = append(progress, message)
			})
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) || strings.TrimSpace(err.Error()) != err.Error() {
					t.Errorf("error = %q, want %q", err, tt.wantError)
				}
			} else if err != nil || got != tt.want {
				t.Errorf("stream = %q, %v; want %q", got, err, tt.want)
			}
			mu.Lock()
			defer mu.Unlock()
			if !slices.Equal(progress, tt.wantProgress) {
				t.Errorf("progress = %q, want %q", progress, tt.wantProgress)
			}
		})
	}
}

func TestPrintWatchLogs(t *testing.T) {
	tests := []struct {
		name string
		// replies are project_status's results in turn; the session ends
		// after the last.
		replies []string
		errs    []error
		want    string
		offsets []int
	}{
		{name: "prints lines and asks from the new offset", replies: []string{`{"recent_logs":["one","two"],"log_offset":2}`, `{"recent_logs":["three"],"log_offset":3}`}, want: "one\ntwo\nthree\n", offsets: []int{0, 2}},
		{name: "tool error prints nothing", replies: []string{""}, errs: []error{errors.New("down")}, offsets: []int{0}},
		{name: "unreadable status prints nothing", replies: []string{"not json"}, offsets: []int{0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var offsets []int
			call := func(_ context.Context, name string, args map[string]any) (string, error) {
				i := len(offsets)
				offsets = append(offsets, args["log_offset"].(int))
				if name != "project_status" || i >= len(tt.replies) {
					return "", fmt.Errorf("unexpected call %s #%d", name, i)
				}
				if i == len(tt.replies)-1 {
					cancel()
				}
				var err error
				if i < len(tt.errs) {
					err = tt.errs[i]
				}
				return tt.replies[i], err
			}
			var out bytes.Buffer
			printWatchLogs(ctx, &out, call)
			if out.String() != tt.want {
				t.Errorf("printed %q, want %q", out.String(), tt.want)
			}
			if !slices.Equal(offsets, tt.offsets) {
				t.Errorf("log offsets = %v, want %v", offsets, tt.offsets)
			}
		})
	}
}
