package cmd

import (
	"bytes"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
)

func TestWatchDirPID(t *testing.T) {
	for name, want := range map[string]int{
		"aviary-watch-123-abc": 123,
		"aviary-watch-1-":      1,
		"aviary-watch-x-abc":   0,
		"aviary-watch-123":     0,
		"other-123-abc":        0,
		"aviary-watch-0-abc":   0,
	} {
		got, ok := watchDirPID(name)
		if got != want || ok != (want > 0) {
			t.Errorf("watchDirPID(%q) = %d, %v; want %d", name, got, ok, want)
		}
	}
}

// deadPID returns the PID of a process that has exited.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatalf("running a short-lived process: %v", err)
	}
	return cmd.Process.Pid
}

func TestSweepStaleWatchDirs(t *testing.T) {
	parent := t.TempDir()
	stale := filepath.Join(parent, fmt.Sprintf("%s%d-abc", watchDirPrefix, deadPID(t)))
	// The test's parent process, `go test`, is alive throughout.
	live := filepath.Join(parent, fmt.Sprintf("%s%d-def", watchDirPrefix, os.Getppid()))
	own := filepath.Join(parent, fmt.Sprintf("%s%d-ghi", watchDirPrefix, os.Getpid()))
	other := filepath.Join(parent, "aviary-agent-test-1")
	for _, dir := range []string{stale, live, own, other} {
		if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	removed := sweepStaleWatchDirs(parent)
	if len(removed) != 1 || removed[0] != stale {
		t.Fatalf("removed %v, want only %s", removed, stale)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale dir still there: %v", err)
	}
	for _, dir := range []string{live, own, other} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("%s was removed: %v", dir, err)
		}
	}
}

func TestFreeWatchPortSkipsTakenPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close() //nolint:errcheck
	taken := ln.Addr().(*net.TCPAddr).Port
	port, err := freeWatchPort(taken)
	if err != nil {
		t.Fatal(err)
	}
	if port <= taken {
		t.Fatalf("freeWatchPort(%d) = %d, want a later free port", taken, port)
	}
}

func TestWatchSessionAgent(t *testing.T) {
	global := &config.Config{Agents: []config.AgentConfig{
		{
			Name:     "coder",
			Model:    "claude/opus",
			Channels: []config.ChannelConfig{{Type: "slack"}},
			Tasks:    []config.TaskConfig{{Name: "daily"}},
			Projects: config.AgentProjectsConfig{{Target: "o/a"}, {Target: "o/other"}},
		},
		{Name: "helper", Model: "codex/gpt", Projects: config.AgentProjectsConfig{{Target: "o/b"}}},
	}}
	agent, err := watchSessionAgent(global, "", []string{"o/a"})
	if err != nil || agent.Name != "coder" || agent.Model != "claude/opus" {
		t.Fatalf("owner = %+v, %v", agent, err)
	}
	if len(agent.Channels) != 0 || len(agent.Tasks) != 0 || len(agent.Projects) != 0 {
		t.Errorf("the session agent carries the user's channels, tasks or projects: %+v", agent)
	}
	if agent, _ := watchSessionAgent(global, "", []string{"o/a", "o/b"}); agent.Name != watchAgentName || agent.Model != "" {
		t.Errorf("targets of two agents ran under %+v, want the model-less watch agent", agent)
	}
	if agent, _ := watchSessionAgent(global, "", []string{"o/new"}); agent.Name != watchAgentName {
		t.Errorf("an unowned target ran under %+v", agent)
	}
	if agent, _ := watchSessionAgent(global, "helper", []string{"o/a"}); agent.Name != "helper" {
		t.Errorf("--agent helper ran under %+v", agent)
	}
	if _, err := watchSessionAgent(global, "missing", []string{"o/a"}); err == nil {
		t.Error("an unknown --agent was accepted")
	}
}

// snapshotDir reads every file under dir, keyed by its relative path.
func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if d.IsDir() {
			files[rel+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// writeGlobalAviary writes a user's Aviary config dir under configHome with
// an agent, a project watch, a channel and stored provider credentials.
func writeGlobalAviary(t *testing.T, configHome string) string {
	t.Helper()
	dir := filepath.Join(configHome, "aviary")
	if err := os.MkdirAll(filepath.Join(dir, "auth"), 0o700); err != nil {
		t.Fatal(err)
	}
	yaml := `server:
  port: 16677
agents:
  - name: coder
    model: claude/opus
    projects:
      - target: someone/existing
models:
  providers:
    anthropic:
      auth: auth:anthropic:default
projects:
  concurrency: 2
`
	if err := os.WriteFile(filepath.Join(dir, "aviary.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	creds := `{"anthropic:default": "sk-secret-value"}`
	if err := os.WriteFile(filepath.Join(dir, "auth", "credentials.json"), []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestStartWatchSessionIsolatesConfig(t *testing.T) {
	configHome := t.TempDir()
	globalDir := writeGlobalAviary(t, configHome)
	before := snapshotDir(t, globalDir)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	store.SetDataDir(globalDir)
	t.Cleanup(func() { store.SetDataDir(dataDir) })

	sources, global, err := resolveGlobalWatchSources(filepath.Join(globalDir, "aviary.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	session, err := startWatchSession(t.TempDir(), global, sources, "", []string{"aviary-test/repo"}, config.ProjectsConfig{Concurrency: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got := config.DefaultPath(); got != session.ConfigPath {
		t.Errorf("DefaultPath = %s during the session, want %s", got, session.ConfigPath)
	}
	if got := store.SubDir(store.DirAuth); got != filepath.Join(globalDir, "auth") {
		t.Errorf("auth dir = %s, want the shared %s", got, filepath.Join(globalDir, "auth"))
	}
	cfg, err := config.Load(session.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Agents) != 1 || cfg.Agents[0].Name != watchAgentName || len(cfg.Agents[0].Projects) != 1 || cfg.Agents[0].Projects[0].Target != "aviary-test/repo" {
		t.Errorf("session agents = %+v", cfg.Agents)
	}
	if !cfg.Server.DisableTokenAuth || !cfg.Server.NoTLS || cfg.Server.Port == 0 {
		t.Errorf("session server = %+v", cfg.Server)
	}
	if cfg.Projects.Concurrency != 5 {
		t.Errorf("concurrency = %d, want the flag's 5", cfg.Projects.Concurrency)
	}
	if cfg.Browser.ProfileDir != filepath.Join(globalDir, "browser") {
		t.Errorf("browser profile = %q, want the shared %s", cfg.Browser.ProfileDir, filepath.Join(globalDir, "browser"))
	}
	// Writing through Aviary's default paths lands in the session.
	if err := config.Save("", cfg); err != nil {
		t.Fatal(err)
	}
	for path, data := range snapshotDir(t, session.Dir) {
		if strings.Contains(data, "sk-secret-value") {
			t.Errorf("%s in the session dir holds a secret", path)
		}
	}
	if err := endWatchSession(session); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(session.Dir); !os.IsNotExist(err) {
		t.Errorf("session dir still there: %v", err)
	}
	if got := snapshotDir(t, globalDir); fmt.Sprint(got) != fmt.Sprint(before) {
		t.Errorf("the global config dir changed:\nbefore %v\nafter  %v", before, got)
	}
}

// The helper process runs `aviary watch` against the user's Aviary config
// dir and temp dir its parent test names, as a real session would.
func TestWatchHelperProcess(t *testing.T) {
	if os.Getenv("AVIARY_WATCH_HELPER") != "1" {
		t.Skip("helper process for the watch session tests")
	}
	_ = os.Unsetenv("AVIARY_CONFIG_BASE_DIR")
	store.SetDataDir("")
	rootCmd.SetArgs(strings.Fields(os.Getenv("AVIARY_WATCH_ARGS")))
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

// watchProcess is a running `aviary watch` helper process.
type watchProcess struct {
	cmd        *exec.Cmd
	out        *bytes.Buffer
	tmp        string
	globalDir  string
	before     map[string]string
	sessionDir string
	port       int
}

// startWatchProcess starts `aviary watch` with args in a helper process
// whose user has an Aviary config, and waits for its session to start.
func startWatchProcess(t *testing.T, args ...string) *watchProcess {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("signals are Unix-only")
	}
	root := t.TempDir()
	configHome := filepath.Join(root, "config")
	tmp := filepath.Join(root, "tmp")
	home := filepath.Join(root, "home")
	for _, dir := range []string{tmp, home} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	p := &watchProcess{out: &bytes.Buffer{}, tmp: tmp, globalDir: writeGlobalAviary(t, configHome)}
	// Keep the default port taken, so the session has to fall back.
	ln, err := net.Listen("tcp", "127.0.0.1:16677")
	if err == nil {
		t.Cleanup(func() { _ = ln.Close() })
	}
	p.before = snapshotDir(t, p.globalDir)
	all := append([]string{"watch", "aviary-test/watch-isolation", "--delivery", "poll", "--interval", "1h"}, args...)
	p.cmd = exec.Command(os.Args[0], "-test.run=^TestWatchHelperProcess$")
	p.cmd.Env = append(os.Environ(),
		"AVIARY_WATCH_HELPER=1",
		"AVIARY_WATCH_ARGS="+strings.Join(all, " "),
		"XDG_CONFIG_HOME="+configHome,
		"TMPDIR="+tmp,
		"HOME="+home,
		"GH_TOKEN=",
	)
	p.cmd.Stdout = p.out
	p.cmd.Stderr = p.out
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.cmd.Process.Kill() })
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if p.sessionDir == "" {
			matches, _ := filepath.Glob(filepath.Join(tmp, watchDirPrefix+strconv.Itoa(p.cmd.Process.Pid)+"-*"))
			if len(matches) == 1 {
				p.sessionDir = matches[0]
			}
		}
		if p.sessionDir != "" && p.port == 0 {
			if cfg, err := config.Load(filepath.Join(p.sessionDir, "aviary.yaml")); err == nil && cfg.Server.Port != 0 {
				p.port = cfg.Server.Port
			}
		}
		if p.port != 0 {
			if resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", p.port)); err == nil {
				_ = resp.Body.Close()
				return p
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("watch session did not start; output:\n%s", p.out)
	return nil
}

// stop signals the process and checks it ended its session cleanly.
func (p *watchProcess) stop(t *testing.T, sig syscall.Signal) {
	t.Helper()
	if err := p.cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("watch exited with %v; output:\n%s", err, p.out)
		}
	case <-time.After(60 * time.Second):
		t.Fatalf("watch did not stop after %v; output:\n%s", sig, p.out)
	}
	if _, err := os.Stat(p.sessionDir); !os.IsNotExist(err) {
		t.Errorf("session dir %s is still there after %v: %v", p.sessionDir, sig, err)
	}
	if got := snapshotDir(t, p.globalDir); fmt.Sprint(got) != fmt.Sprint(p.before) {
		t.Errorf("the global config dir changed during the watch:\nbefore %v\nafter  %v", p.before, got)
	}
}

func TestWatchSessionCleansUpOnSIGINT(t *testing.T) {
	p := startWatchProcess(t, "--no-tui")
	if p.port == 16677 {
		t.Errorf("the session took the taken default port")
	}
	if !strings.Contains(p.out.String(), fmt.Sprintf("http://localhost:%d/", p.port)) {
		t.Errorf("the session did not print its URL; output:\n%s", p.out)
	}
	p.stop(t, syscall.SIGINT)
}

func TestWatchSessionCleansUpOnSIGTERM(t *testing.T) {
	p := startWatchProcess(t, "--no-tui")
	p.stop(t, syscall.SIGTERM)
}

func TestWatchSessionCleansUpOnSIGHUP(t *testing.T) {
	p := startWatchProcess(t, "--no-tui")
	p.stop(t, syscall.SIGHUP)
}

func TestWatchNoWebUIAndNoTUI(t *testing.T) {
	p := startWatchProcess(t, "--no-tui", "--no-webui")
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", p.port))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET / = %d with --no-webui, want 404", resp.StatusCode)
	}
	out := p.out.String()
	if !strings.Contains(out, "Watching aviary-test/watch-isolation in an isolated session") {
		t.Errorf("--no-tui did not print the log header; output:\n%s", out)
	}
	if strings.Contains(out, "Code Loop: http") {
		t.Errorf("--no-webui printed a web UI URL; output:\n%s", out)
	}
	if strings.Contains(out, "\x1b[?1049h") {
		t.Errorf("--no-tui entered the TUI's alternate screen")
	}
	p.stop(t, syscall.SIGINT)
}

func TestWatchMinimalModeBlocksOtherAPIs(t *testing.T) {
	p := startWatchProcess(t, "--no-tui")
	base := fmt.Sprintf("http://127.0.0.1:%d", p.port)
	call := func(tool string) int {
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":{}}}`, tool)
		req, _ := http.NewRequest(http.MethodPost, base+"/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	for _, tool := range []string{"agent_add", "session_send", "channel_create", "server_restart", "auth_get"} {
		if code := call(tool); code != http.StatusForbidden {
			t.Errorf("%s = %d, want 403", tool, code)
		}
	}
	resp, err := http.Get(base + "/api/daemons")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("/api/daemons = %d, want 403", resp.StatusCode)
	}
	resp, err = http.Get(base + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	var health bytes.Buffer
	_, _ = health.ReadFrom(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(health.String(), `"minimal":true`) || !strings.Contains(health.String(), `"auth_disabled":true`) {
		t.Errorf("health = %s, want minimal and auth_disabled", health.String())
	}
	p.stop(t, syscall.SIGINT)
}
