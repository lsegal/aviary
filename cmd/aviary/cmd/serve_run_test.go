package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/server"
	"github.com/lsegal/aviary/internal/startup"
	"github.com/lsegal/aviary/internal/store"
	"github.com/lsegal/aviary/internal/update"
)

// scriptedServer stands in for *server.Server in runStart.
type scriptedServer struct {
	serve      func(ctx context.Context) error
	reexecPath string
}

func (f *scriptedServer) ListenAndServe(ctx context.Context) error { return f.serve(ctx) }
func (f *scriptedServer) ReexecPath() string                       { return f.reexecPath }

// serveEnv is an isolated config and data dir for runStart and runStop.
type serveEnv struct {
	dir     string
	cfgPath string
	dataDir string
}

// setupServeEnv points cfgFile, the config base dir, the PID file, and the
// data dir at a temp dir, and restores the working directory, globals, and
// seams when the test ends.
func setupServeEnv(t *testing.T) *serveEnv {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	prevCfg, prevTrace, prevData := cfgFile, traceStartup, store.DataDir()
	prevNew, prevReexec := newServer, reexec
	t.Cleanup(func() {
		_ = os.Chdir(wd)
		cfgFile, traceStartup = prevCfg, prevTrace
		newServer, reexec = prevNew, prevReexec
		store.SetDataDir(prevData)
	})
	t.Setenv("AVIARY_CONFIG_BASE_DIR", os.Getenv("AVIARY_CONFIG_BASE_DIR"))
	t.Setenv("AVIARY_PID_FILE", "")
	t.Setenv(update.RestartFileEnv, "")

	dir := t.TempDir()
	env := &serveEnv{
		dir:     dir,
		cfgPath: filepath.Join(dir, "cfg", "aviary.yaml"),
		dataDir: filepath.Join(dir, "data"),
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(env.cfgPath), 0o750))
	require.NoError(t, config.Save(env.cfgPath, &config.Config{}))
	store.SetDataDir(env.dataDir)
	cfgFile = env.cfgPath
	return env
}

func (e *serveEnv) pidPath() string { return filepath.Join(filepath.Dir(e.cfgPath), "aviary.pid") }

func (e *serveEnv) writePID(t *testing.T, pid int) {
	t.Helper()
	require.NoError(t, os.WriteFile(e.pidPath(), []byte(strconv.Itoa(pid)+"\n"), 0o644))
}

// serveWith makes runStart serve with fn, counting the servers it builds.
func serveWith(fn func(ctx context.Context, n int) error) *int {
	calls := 0
	newServer = func(_ *config.Config, _ string) aviaryServer {
		calls++
		n := calls
		return &scriptedServer{serve: func(ctx context.Context) error { return fn(ctx, n) }, reexecPath: "/new/aviary"}
	}
	return &calls
}

// captureStderr runs fn and returns its stderr.
func captureStderr(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	stderr := os.Stderr
	os.Stderr = w
	runErr := fn()
	os.Stderr = stderr
	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out), runErr
}

func TestRunStartServesUntilStopAndRemovesPID(t *testing.T) {
	env := setupServeEnv(t)
	var pidDuringServe string
	calls := serveWith(func(_ context.Context, _ int) error {
		data, err := os.ReadFile(env.pidPath())
		require.NoError(t, err)
		pidDuringServe = strings.TrimSpace(string(data))
		return nil
	})

	out, err := captureStdout(t, func() error { return runStart(nil, nil) })
	require.NoError(t, err)

	assert.Equal(t, 1, *calls)
	assert.Equal(t, strconv.Itoa(os.Getpid()), pidDuringServe)
	assert.NoFileExists(t, env.pidPath(), "PID file is removed once serving stops")
	assert.Contains(t, out, "started on ")
	assert.Contains(t, out, "Your access token: ")
	assertPathHasSuffix(t, cfgFile, filepath.Join("cfg", "aviary.yaml"))
	cwd, err := os.Getwd()
	require.NoError(t, err)
	assertPathHasSuffix(t, cwd, "cfg")

	// The token now exists, so a second start does not print it again.
	out, err = captureStdout(t, func() error { return runStart(nil, nil) })
	require.NoError(t, err)
	assert.Contains(t, out, "started on ")
	assert.NotContains(t, out, "Your access token")
}

func TestRunStartRefusesWhenAnotherServerRuns(t *testing.T) {
	env := setupServeEnv(t)
	// The parent process (go test) is alive and is not this process.
	env.writePID(t, os.Getppid())
	calls := serveWith(func(context.Context, int) error { return nil })

	err := runStart(nil, nil)
	require.EqualError(t, err, "aviary is already running (PID "+strconv.Itoa(os.Getppid())+")")
	assert.Zero(t, *calls)
	assert.FileExists(t, env.pidPath(), "another server's PID file is left alone")
}

func TestRunStartIgnoresStalePIDEqualToOwn(t *testing.T) {
	env := setupServeEnv(t)
	env.writePID(t, os.Getpid())
	calls := serveWith(func(context.Context, int) error { return nil })

	_, err := captureStdout(t, func() error { return runStart(nil, nil) })
	require.NoError(t, err)
	assert.Equal(t, 1, *calls)
}

func TestRunStartStatusErrors(t *testing.T) {
	t.Run("unreadable PID", func(t *testing.T) {
		env := setupServeEnv(t)
		require.NoError(t, os.WriteFile(env.pidPath(), []byte("not-a-pid"), 0o644))
		err := runStart(nil, nil)
		require.ErrorContains(t, err, "checking server status: parsing PID")
	})
	t.Run("invalid config path", func(t *testing.T) {
		env := setupServeEnv(t)
		cfgFile = filepath.Join(env.dir, "bad\x00dir", "aviary.yaml")
		err := runStart(nil, nil)
		require.ErrorContains(t, err, "checking server status: setting config base dir")
	})
}

func TestRunStartSetupErrors(t *testing.T) {
	t.Run("data dir", func(t *testing.T) {
		env := setupServeEnv(t)
		blocker := filepath.Join(env.dir, "file")
		require.NoError(t, os.WriteFile(blocker, nil, 0o644))
		store.SetDataDir(filepath.Join(blocker, "data"))
		err := runStart(nil, nil)
		require.ErrorContains(t, err, "initializing data directory")
	})
	t.Run("config load", func(t *testing.T) {
		env := setupServeEnv(t)
		require.NoError(t, os.WriteFile(env.cfgPath, []byte("agents: [\n"), 0o600))
		err := runStart(nil, nil)
		require.ErrorContains(t, err, "loading config")
	})
	t.Run("token", func(t *testing.T) {
		env := setupServeEnv(t)
		// A directory where the token file belongs cannot be read as one.
		require.NoError(t, os.MkdirAll(filepath.Join(env.dataDir, "token"), 0o750))
		_, err := captureStderr(t, func() error { return runStart(nil, nil) })
		require.ErrorContains(t, err, "loading token")
	})
}

func TestRunStartValidation(t *testing.T) {
	t.Run("errors stop startup", func(t *testing.T) {
		env := setupServeEnv(t)
		require.NoError(t, config.Save(env.cfgPath, &config.Config{Server: config.ServerConfig{Port: 70000}}))
		calls := serveWith(func(context.Context, int) error { return nil })

		stderr, err := captureStderr(t, func() error { return runStart(nil, nil) })
		require.EqualError(t, err, "1 configuration error(s) found — run 'aviary doctor' for full details")
		assert.Contains(t, stderr, "server.port: port 70000 is out of range")
		assert.Zero(t, *calls)
	})
	t.Run("warnings are printed and startup continues", func(t *testing.T) {
		env := setupServeEnv(t)
		require.NoError(t, config.Save(env.cfgPath, &config.Config{Agents: []config.AgentConfig{{Name: "bot"}}}))
		calls := serveWith(func(context.Context, int) error { return nil })

		var stderr string
		_, err := captureStdout(t, func() error {
			var serr error
			stderr, serr = captureStderr(t, func() error { return runStart(nil, nil) })
			return serr
		})
		require.NoError(t, err)
		assert.Contains(t, stderr, "agents[0].model: no model configured")
		assert.Equal(t, 1, *calls)
	})
}

func TestRunStartRestartReloadsConfig(t *testing.T) {
	env := setupServeEnv(t)
	var ports []int
	newServer = func(cfg *config.Config, _ string) aviaryServer {
		ports = append(ports, cfg.Server.Port)
		n := len(ports)
		return &scriptedServer{serve: func(context.Context) error {
			if n == 1 {
				require.NoError(t, config.Save(env.cfgPath, &config.Config{Server: config.ServerConfig{Port: 17777}}))
				return server.ErrRestartRequired
			}
			return nil
		}}
	}

	_, err := captureStdout(t, func() error { return runStart(nil, nil) })
	require.NoError(t, err)
	assert.Equal(t, []int{0, 17777}, ports)
}

func TestRunStartRestartReloadError(t *testing.T) {
	env := setupServeEnv(t)
	calls := serveWith(func(context.Context, int) error {
		require.NoError(t, os.WriteFile(env.cfgPath, []byte("agents: [\n"), 0o600))
		return server.ErrRestartRequired
	})

	_, err := captureStdout(t, func() error { return runStart(nil, nil) })
	require.ErrorContains(t, err, "reloading config")
	assert.Equal(t, 1, *calls)
	assert.NoFileExists(t, env.pidPath())
}

func TestRunStartReexec(t *testing.T) {
	t.Run("re-executes the new binary", func(t *testing.T) {
		env := setupServeEnv(t)
		serveWith(func(context.Context, int) error { return server.ErrReexecRequired })
		var gotExe string
		var gotArgs []string
		var pidGone bool
		boom := errors.New("exec failed")
		reexec = func(exe string, args, _ []string) error {
			gotExe, gotArgs = exe, args
			_, statErr := os.Stat(env.pidPath())
			pidGone = os.IsNotExist(statErr)
			return boom
		}

		_, err := captureStdout(t, func() error { return runStart(nil, nil) })
		require.ErrorIs(t, err, boom)
		assert.Equal(t, "/new/aviary", gotExe)
		assert.Equal(t, os.Args[1:], gotArgs)
		assert.True(t, pidGone, "PID file is released before the new process starts")
	})
	t.Run("asks the dev supervisor to rebuild", func(t *testing.T) {
		env := setupServeEnv(t)
		restartFile := filepath.Join(env.dir, "restart")
		t.Setenv(update.RestartFileEnv, restartFile)
		serveWith(func(context.Context, int) error { return server.ErrReexecRequired })
		reexec = func(string, []string, []string) error {
			t.Fatal("reexec must not run under the dev supervisor")
			return nil
		}

		_, err := captureStdout(t, func() error { return runStart(nil, nil) })
		require.NoError(t, err)
		assert.FileExists(t, restartFile)
		assert.NoFileExists(t, env.pidPath())
	})
}

func TestRunStartReturnsServeError(t *testing.T) {
	env := setupServeEnv(t)
	boom := errors.New("listen failed")
	calls := serveWith(func(context.Context, int) error { return boom })

	_, err := captureStdout(t, func() error { return runStart(nil, nil) })
	require.ErrorIs(t, err, boom)
	assert.Equal(t, 1, *calls)
	assert.NoFileExists(t, env.pidPath())
}

func TestRunStartShutsDownOnSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Interrupt cannot be sent to a process on Windows")
	}
	setupServeEnv(t)
	serveWith(func(ctx context.Context, _ int) error {
		self, err := os.FindProcess(os.Getpid())
		require.NoError(t, err)
		require.NoError(t, self.Signal(os.Interrupt))
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
			t.Error("interrupt did not cancel the server context")
		}
		// A restart cut short by the shutdown is not an error.
		return server.ErrRestartRequired
	})

	out, err := captureStdout(t, func() error { return runStart(nil, nil) })
	require.NoError(t, err)
	assert.Contains(t, out, "Shutting down...")
}

func TestRunStartPrintsStartupTrace(t *testing.T) {
	setupServeEnv(t)
	traceStartup = true
	serveWith(func(context.Context, int) error {
		startup.Current().MarkMCPReady()
		// Give printStartupTrace a few ticks to see the ready boot.
		time.Sleep(500 * time.Millisecond)
		return nil
	})

	out, err := captureStdout(t, func() error { return runStart(nil, nil) })
	require.NoError(t, err)
	assert.Contains(t, out, "Startup trace\n")
	assert.Contains(t, out, "config load")
}

func TestPrintStartupTraceStopsWithContext(t *testing.T) {
	startup.Begin().Pending("never-ready")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	out, err := captureStdout(t, func() error {
		go func() {
			printStartupTrace(ctx)
			close(done)
		}()
		time.Sleep(250 * time.Millisecond)
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("printStartupTrace did not return after cancel")
		}
		return nil
	})
	require.NoError(t, err)
	assert.Empty(t, out, "nothing is printed before the boot is ready")
}

func TestRunStop(t *testing.T) {
	t.Run("no PID file", func(t *testing.T) {
		setupServeEnv(t)
		out, err := captureStdout(t, func() error { return runStop(nil, nil) })
		require.NoError(t, err)
		assert.Equal(t, "Aviary is not running.\n", out)
	})
	t.Run("stale PID file is removed", func(t *testing.T) {
		env := setupServeEnv(t)
		env.writePID(t, exitedPID(t))
		out, err := captureStdout(t, func() error { return runStop(nil, nil) })
		require.NoError(t, err)
		assert.Equal(t, "Aviary is not running.\n", out)
		assert.NoFileExists(t, env.pidPath())
	})
	t.Run("status error", func(t *testing.T) {
		env := setupServeEnv(t)
		cfgFile = filepath.Join(env.dir, "bad\x00dir", "aviary.yaml")
		err := runStop(nil, nil)
		require.ErrorContains(t, err, "checking server status")
	})
	t.Run("interrupts a running server", func(t *testing.T) {
		env := setupServeEnv(t)
		child := startHelperProcess(t)
		env.writePID(t, child.Process.Pid)

		out, err := captureStdout(t, func() error { return runStop(nil, nil) })
		if runtime.GOOS == "windows" {
			require.ErrorContains(t, err, "sending interrupt to PID "+strconv.Itoa(child.Process.Pid))
			return
		}
		require.NoError(t, err)
		assert.Equal(t, "Sent stop signal to Aviary (PID "+strconv.Itoa(child.Process.Pid)+").\n", out)
		waited := make(chan error, 1)
		go func() { waited <- child.Wait() }()
		select {
		case <-waited:
		case <-time.After(10 * time.Second):
			t.Fatal("server process did not exit after the stop signal")
		}
	})
}

func TestServeStopSubcommand(t *testing.T) {
	setupServeEnv(t)
	out, err := captureStdout(t, func() error { return serveCmd.RunE(serveCmd, []string{"stop"}) })
	require.NoError(t, err)
	assert.Equal(t, "Aviary is not running.\n", out)
}

func TestServeUnknownSubcommand(t *testing.T) {
	err := serveCmd.RunE(serveCmd, []string{"restart"})
	require.EqualError(t, err, `unknown subcommand "restart"`)
}

func TestServeDaemonStartsDetachedProcess(t *testing.T) {
	env := setupServeEnv(t)
	prevStart, prevDaemon := startBackground, serveDaemon
	t.Cleanup(func() { startBackground, serveDaemon = prevStart, prevDaemon })
	serveDaemon = true
	cfgFile = filepath.Join(env.dir, "daemon", "aviary.yaml")

	var got *exec.Cmd
	startBackground = func(c *exec.Cmd) (int, error) {
		got = c
		return 4242, nil
	}
	out, err := captureStdout(t, func() error { return serveCmd.RunE(serveCmd, []string{"start"}) })
	require.NoError(t, err)
	assert.Contains(t, out, "started in background (PID 4242)")

	require.NotNil(t, got)
	exe, err := os.Executable()
	require.NoError(t, err)
	assert.Equal(t, []string{exe, "serve", "start"}, got.Args)
	cfgDir := filepath.Join(env.dir, "daemon")
	assert.Equal(t, cfgDir, got.Dir)
	assert.DirExists(t, cfgDir, "the config dir is created for the PID file")
	assert.Equal(t, "AVIARY_CONFIG_BASE_DIR="+cfgDir, got.Env[len(got.Env)-1])
	assert.NotNil(t, got.Stdin)
	assert.NotNil(t, got.Stdout)
	assert.NotNil(t, got.Stderr)

	startBackground = func(*exec.Cmd) (int, error) { return 0, errors.New("fork failed") }
	err = serveCmd.RunE(serveCmd, nil)
	require.EqualError(t, err, "starting background process: fork failed")
}

func TestStartBackgroundStartsProcess(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	c := exec.Command(exe, "-test.run=^$")
	pid, err := startBackground(c)
	require.NoError(t, err)
	assert.Equal(t, c.Process.Pid, pid)
	require.NoError(t, c.Wait())

	_, err = startBackground(exec.Command(filepath.Join(t.TempDir(), "missing")))
	require.Error(t, err)
}

func TestChdirToConfigDirErrors(t *testing.T) {
	env := setupServeEnv(t)
	_, err := chdirToConfigDir(filepath.Join(env.dir, "bad\x00dir", "aviary.yaml"))
	require.ErrorContains(t, err, "setting config base dir")

	blocker := filepath.Join(env.dir, "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o644))
	_, err = chdirToConfigDir(filepath.Join(blocker, "aviary.yaml"))
	require.ErrorContains(t, err, "creating config dir")
}

// exitedPID returns the PID of a process that has already exited.
func exitedPID(t *testing.T) int {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	c := exec.Command(exe, "-test.run=^$")
	require.NoError(t, c.Run())
	return c.Process.Pid
}

// startHelperProcess starts a copy of the test binary that sleeps until it
// is signalled, standing in for a running server.
func startHelperProcess(t *testing.T) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	c := exec.Command(exe, "-test.run=^TestServeHelperProcess$")
	c.Env = append(os.Environ(), "AVIARY_SERVE_HELPER_PROCESS=1")
	require.NoError(t, c.Start())
	t.Cleanup(func() {
		_ = c.Process.Kill()
		_ = c.Wait()
	})
	return c
}

func TestServeHelperProcess(t *testing.T) {
	if os.Getenv("AVIARY_SERVE_HELPER_PROCESS") != "1" {
		t.Skip("helper process for TestRunStop")
	}
	time.Sleep(time.Minute)
}
