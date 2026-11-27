package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	internalmcp "github.com/lsegal/aviary/internal/mcp"
	"github.com/lsegal/aviary/internal/store"
)

func TestProjectToolArgsFillsThePositionalField(t *testing.T) {
	for _, test := range []struct {
		positional string
		args, want []string
	}{
		{"number", []string{"7", "--target", "o/r"}, []string{"--number", "7", "--target", "o/r"}},
		{"number", []string{"--number", "7"}, []string{"--number", "7"}},
		{"", []string{"--status", "active"}, []string{"--status", "active"}},
		{"target", nil, nil},
	} {
		if got := projectToolArgs(test.positional, test.args); !reflect.DeepEqual(got, test.want) {
			t.Errorf("projectToolArgs(%q, %v) = %v, want %v", test.positional, test.args, got, test.want)
		}
	}
}

// TestEveryProjectToolHasACommand checks the aviary project tree mirrors every
// project_* MCP tool.
func TestEveryProjectToolHasACommand(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test"}, nil)
	internalmcp.Register(server)
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "client"}, nil)
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close() //nolint:errcheck
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	commands := map[string]bool{}
	for _, spec := range projectToolCommands {
		commands[spec.tool] = true
	}
	tools := map[string]bool{}
	for _, tool := range listed.Tools {
		if strings.HasPrefix(tool.Name, "project_") {
			tools[tool.Name] = true
			if !commands[tool.Name] {
				t.Errorf("tool %s has no aviary project command", tool.Name)
			}
		}
	}
	for tool := range commands {
		if !tools[tool] {
			t.Errorf("aviary project calls %s, which is not a registered tool", tool)
		}
	}
}

func TestProjectCommandTree(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"project", "job", "get"})
	if err != nil || cmd.Name() != "get" || !cmd.DisableFlagParsing {
		t.Fatalf("project job get = %v, %v", cmd, err)
	}
}

func TestWatchSettingsFromFlags(t *testing.T) {
	t.Cleanup(func() { watchOpts = watchOptions{} })
	if err := watchCmd.Flags().Parse([]string{"--concurrency", "2", "--interval", "1m", "--delivery", "Webhook", "--no-merge", "--filter", "label:bug"}); err != nil {
		t.Fatal(err)
	}
	settings, err := watchSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Concurrency != 2 || settings.Interval != "1m" || settings.Delivery != "webhook" || !settings.NoMerge {
		t.Fatalf("settings = %+v", settings)
	}
	if len(settings.Filter) != 1 || settings.Filter[0] != "label:bug" {
		t.Fatalf("filter = %v", settings.Filter)
	}
	watchOpts.delivery = "carrier pigeon"
	if _, err := watchSettings(); err == nil {
		t.Fatal("an unknown delivery mode was accepted")
	}
	watchOpts.delivery, watchOpts.interval = "", "soon"
	if _, err := watchSettings(); err == nil {
		t.Fatal("an invalid interval was accepted")
	}
	watchOpts.interval = ""
	watchOpts.delivery, watchOpts.browserVisible, watchOpts.browserVision = "Browser", true, true
	settings, err = watchSettings()
	if err != nil || settings.Delivery != "browser" || !settings.BrowserVisible || !settings.BrowserVision {
		t.Fatalf("browser delivery settings = %+v, %v", settings, err)
	}
	watchOpts.delivery = "poll"
	if _, err := watchSettings(); err == nil {
		t.Fatal("browser-only switches were accepted for gh polling")
	}
}

func TestWatchModelEnvFillsWhatFlagsLeaveUnset(t *testing.T) {
	env := map[string]string{watchModelEnv: "gemini/pro", watchFallbacksEnv: " codex/gpt-5.6 , ,claude/sonnet"}
	getenv := func(k string) string { return env[k] }
	got := withWatchModelEnv(watchAgentChoice{Name: "bot"}, getenv)
	if got.Name != "bot" || got.Model != "gemini/pro" || !slices.Equal(got.Fallbacks, []string{"codex/gpt-5.6", "claude/sonnet"}) {
		t.Fatalf("env choice = %+v", got)
	}
	got = withWatchModelEnv(watchAgentChoice{Model: "claude:opus", Fallbacks: []string{"codex:gpt-5.6"}}, getenv)
	if got.Model != "claude:opus" || !slices.Equal(got.Fallbacks, []string{"codex:gpt-5.6"}) {
		t.Fatalf("flags should win over env: %+v", got)
	}
	for _, text := range []string{"--model", "--fallback", watchModelEnv, watchFallbacksEnv, "projects.model"} {
		if !strings.Contains(watchCmd.Long, text) {
			t.Errorf("aviary watch --help does not document %s", text)
		}
	}
}

func TestWatchTargetsExpandShorthands(t *testing.T) {
	targets, err := watchTargets([]string{"o/r", "https://github.com/o/s"})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0] != "o/r" {
		t.Fatalf("targets = %v", targets)
	}
}

func TestApplyGlobalFlagsHonoursRootFlags(t *testing.T) {
	prevServer, prevToken, prevData, prevDispatcher := serverURL, token, dataDir, dispatcher
	t.Cleanup(func() {
		serverURL, token, dataDir, dispatcher = prevServer, prevToken, prevData, prevDispatcher
		store.SetDataDir(prevData)
	})
	dir := t.TempDir()
	rest := applyGlobalFlags([]string{"--server", "https://localhost:1", "7", "--token=abc", "--data-dir", dir, "--target", "o/r"})
	if want := []string{"7", "--target", "o/r"}; !reflect.DeepEqual(rest, want) {
		t.Fatalf("rest = %v, want %v", rest, want)
	}
	if serverURL != "https://localhost:1" || token != "abc" || dataDir != dir {
		t.Fatalf("flags = %q %q %q", serverURL, token, dataDir)
	}
	if dispatcher == prevDispatcher {
		t.Fatal("the dispatcher was not rebuilt")
	}
}

// runProjectToolCmd runs a generated aviary project subcommand for spec with
// args and returns its output.
func runProjectToolCmd(t *testing.T, spec projectToolCommand, args ...string) (string, error) {
	t.Helper()
	cmd := newProjectToolCommand(spec)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetContext(context.Background())
	// RunE directly: Execute would run cobra.OnInitialize, which rebuilds the
	// dispatcher from the real config.
	err := cmd.RunE(cmd, args)
	return out.String(), err
}

func TestProjectToolCommandUse(t *testing.T) {
	cmd := newProjectToolCommand(projectToolCommand{[]string{"job", "get"}, "project_job_get", "number", "Show one job"})
	if cmd.Use != "get [number] [--field value ...]" || cmd.Short != "Show one job" {
		t.Fatalf("use = %q, short = %q", cmd.Use, cmd.Short)
	}
	if !strings.Contains(cmd.Long, "aviary tool project_job_get --help") {
		t.Fatalf("long = %q", cmd.Long)
	}
	cmd = newProjectToolCommand(projectToolCommand{[]string{"status"}, "project_status", "", "Show status"})
	if cmd.Use != "status [--field value ...]" {
		t.Fatalf("use = %q", cmd.Use)
	}
}

func TestProjectToolCommandRun(t *testing.T) {
	startToolServer(t)
	echo := projectToolCommand{[]string{"echo"}, "echo", "message", "Echo"}

	tests := []struct {
		name     string
		spec     projectToolCommand
		args     []string
		want     string
		wantErr  string
		wantJSON map[string]any
	}{
		{name: "help shows the tool's fields", spec: echo, args: []string{"--help"}, want: "aviary tool echo [--count <value>] --message <value>"},
		{name: "positional fills the field", spec: echo, args: []string{"hi", "--count", "2"}, wantJSON: map[string]any{"message": "hi", "count": float64(2)}},
		{name: "field flags", spec: echo, args: []string{"--message", "hey", "--count=3"}, wantJSON: map[string]any{"message": "hey", "count": float64(3)}},
		{name: "unknown tool", spec: projectToolCommand{[]string{"missing"}, "missing", "", "Missing"}, wantErr: `tool "missing" is not available`},
		{name: "argument parse error", spec: echo, args: []string{"hi", "stray"}, wantErr: `unexpected argument "stray"`},
		{name: "positional without a positional field", spec: projectToolCommand{[]string{"echo"}, "echo", "", "Echo"}, args: []string{"hi"}, wantErr: `unexpected argument "hi"`},
		{name: "failed call", spec: projectToolCommand{[]string{"fail"}, "fail", "", "Fail"}, wantErr: "tool exploded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runProjectToolCmd(t, tt.spec, tt.args...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				if out != "" {
					t.Fatalf("a failed command printed %q", out)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.want != "" && !strings.Contains(out, tt.want) {
				t.Fatalf("output %q does not contain %q", out, tt.want)
			}
			if tt.wantJSON != nil {
				var got map[string]any
				if err := json.Unmarshal([]byte(out), &got); err != nil {
					t.Fatalf("output %q: %v", out, err)
				}
				if !reflect.DeepEqual(got, tt.wantJSON) {
					t.Fatalf("payload = %v, want %v", got, tt.wantJSON)
				}
			}
		})
	}
}

// TestProjectToolCommandHonoursGlobalServerFlag checks a root --server flag,
// which flag parsing would otherwise swallow, redirects the call.
func TestProjectToolCommandHonoursGlobalServerFlag(t *testing.T) {
	url := startToolServer(t)
	prevServer, prevToken := serverURL, token
	t.Cleanup(func() { serverURL, token = prevServer, prevToken })
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(dead.Close)
	useRemoteDispatcher(t, dead.URL)

	out, err := runProjectToolCmd(t, projectToolCommand{[]string{"echo"}, "echo", "message", "Echo"}, "--server", url, "--token=test-token", "hi")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != `{"message":"hi"}` {
		t.Fatalf("output = %q", out)
	}
}

func TestApplyGlobalFlagsKeepsFlagsItCannotApply(t *testing.T) {
	prevServer, prevDispatcher := serverURL, dispatcher
	t.Cleanup(func() { serverURL, dispatcher = prevServer, prevDispatcher })
	serverURL = "https://localhost:2"
	rest := applyGlobalFlags([]string{"-server", "x", "--number", "7", "--server"})
	if want := []string{"-server", "x", "--number", "7", "--server"}; !reflect.DeepEqual(rest, want) {
		t.Fatalf("rest = %v, want %v", rest, want)
	}
	if serverURL != "https://localhost:2" || dispatcher != prevDispatcher {
		t.Fatalf("unapplied flags changed state: server %q", serverURL)
	}
}
