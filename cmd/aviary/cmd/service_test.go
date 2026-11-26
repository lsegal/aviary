package cmd

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/server"
)

// fakeServiceManager records the service-manager calls the handlers make.
type fakeServiceManager struct {
	installed []server.ServiceOptions
	calls     []string
	err       error
}

// stubServiceManager replaces the service-manager hooks with a recorder that
// returns err from every call.
func stubServiceManager(t *testing.T, err error) *fakeServiceManager {
	t.Helper()
	fake := &fakeServiceManager{err: err}
	origInstall, origUninstall, origStart, origStop := installService, uninstallService, startService, stopService
	t.Cleanup(func() {
		installService, uninstallService, startService, stopService = origInstall, origUninstall, origStart, origStop
	})
	installService = func(opts server.ServiceOptions) error {
		fake.installed = append(fake.installed, opts)
		fake.calls = append(fake.calls, "install "+opts.Name)
		return fake.err
	}
	uninstallService = func(name string) error {
		fake.calls = append(fake.calls, "uninstall "+name)
		return fake.err
	}
	startService = func(name string) error {
		fake.calls = append(fake.calls, "start "+name)
		return fake.err
	}
	stopService = func(name string) error {
		fake.calls = append(fake.calls, "stop "+name)
		return fake.err
	}
	return fake
}

// stubServiceEnv pins the working directory, current user, and OS the dev
// service handlers inspect.
func stubServiceEnv(t *testing.T, wd string, usr *user.User, userErr error, goos string) {
	t.Helper()
	origGetwd, origUser, origGOOS := serviceGetwd, serviceCurrentUser, serviceGOOS
	t.Cleanup(func() { serviceGetwd, serviceCurrentUser, serviceGOOS = origGetwd, origUser, origGOOS })
	serviceGetwd = func() (string, error) { return wd, nil }
	serviceCurrentUser = func() (*user.User, error) { return usr, userErr }
	serviceGOOS = goos
}

func TestServiceNameHandlersCallServiceManager(t *testing.T) {
	tests := []struct {
		name    string
		run     func(*cobra.Command, []string) error
		call    string
		before  string
		after   string
		service string
	}{
		{"uninstall", runUninstallService, "uninstall", "Uninstalling", "Service uninstalled", "aviary"},
		{"uninstall-dev", runUninstallDevService, "uninstall", "Uninstalling", "Service uninstalled", "aviary-dev"},
		{"uninstall-dev-docs", runUninstallDevDocsService, "uninstall", "Uninstalling", "Service uninstalled", "aviary-dev-docs"},
		{"start", runStartService, "start", "Starting", "Service started", "aviary"},
		{"stop", runStopService, "stop", "Stopping", "Service stopped", "aviary"},
		{"start-dev", runStartDevService, "start", "Starting", "Service started", "aviary-dev"},
		{"stop-dev", runStopDevService, "stop", "Stopping", "Service stopped", "aviary-dev"},
		{"start-dev-docs", runStartDevDocsService, "start", "Starting", "Service started", "aviary-dev-docs"},
		{"stop-dev-docs", runStopDevDocsService, "stop", "Stopping", "Service stopped", "aviary-dev-docs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := stubServiceManager(t, nil)
			out, err := captureStdout(t, func() error { return tt.run(nil, nil) })
			require.NoError(t, err)
			assert.Equal(t, []string{tt.call + " " + tt.service}, fake.calls)
			assert.Equal(t, tt.before+" service: "+tt.service+"\n"+tt.after+": "+tt.service+"\n", out)
		})
		t.Run(tt.name+" error", func(t *testing.T) {
			boom := errors.New("service manager failed")
			fake := stubServiceManager(t, boom)
			out, err := captureStdout(t, func() error { return tt.run(nil, nil) })
			require.ErrorIs(t, err, boom)
			assert.Equal(t, []string{tt.call + " " + tt.service}, fake.calls)
			assert.Equal(t, tt.before+" service: "+tt.service+"\n", out, "no success line after a failure")
		})
	}
}

func TestRunInstallServiceUsesExecutable(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "bin", "aviary")
	orig := serviceExecutable
	t.Cleanup(func() { serviceExecutable = orig })
	serviceExecutable = func() (string, error) { return exe, nil }
	fake := stubServiceManager(t, nil)

	out, err := captureStdout(t, func() error { return runInstallService(nil, nil) })
	require.NoError(t, err)
	require.Len(t, fake.installed, 1)
	assert.Equal(t, server.ServiceOptions{
		Name:        "aviary",
		DisplayName: "Aviary",
		Description: "Aviary agent server",
		Exec:        exe,
		Args:        []string{"serve", "start", "-d"},
		WorkingDir:  filepath.Dir(exe),
	}, fake.installed[0])
	assert.Equal(t, "Installing service: aviary\nService installed and started: aviary\n", out)
}

func TestRunInstallServiceErrors(t *testing.T) {
	orig := serviceExecutable
	t.Cleanup(func() { serviceExecutable = orig })

	t.Run("executable lookup", func(t *testing.T) {
		serviceExecutable = func() (string, error) { return "", errors.New("no exe") }
		fake := stubServiceManager(t, nil)
		err := runInstallService(nil, nil)
		require.EqualError(t, err, "locating executable: no exe")
		assert.Empty(t, fake.calls, "nothing is installed without an executable")
	})
	t.Run("install", func(t *testing.T) {
		serviceExecutable = func() (string, error) { return filepath.Join(t.TempDir(), "aviary"), nil }
		boom := errors.New("install failed")
		stubServiceManager(t, boom)
		out, err := captureStdout(t, func() error { return runInstallService(nil, nil) })
		require.ErrorIs(t, err, boom)
		assert.Equal(t, "Installing service: aviary\n", out)
	})
}

// devServices lists the development service installers, which share
// everything but their identity and the pnpm command they run.
var devServices = []struct {
	run         func(*cobra.Command, []string) error
	name        string
	displayName string
	description string
	command     string
}{
	{
		run: runInstallDevService, name: "aviary-dev", displayName: "Aviary (dev pnpm dev)",
		description: "Runs `pnpm dev` for aviary (development)", command: "pnpm dev",
	},
	{
		run: runInstallDevDocsService, name: "aviary-dev-docs", displayName: "Aviary (docs:dev)",
		description: "Runs `pnpm docs:dev` for aviary (development)", command: "pnpm docs:dev",
	},
}

func TestRunInstallDevServicesOptions(t *testing.T) {
	current := &user.User{Username: "builder", HomeDir: filepath.Join("/srv", "builder")}
	tests := []struct {
		name     string
		wd       string
		usr      *user.User
		userErr  error
		goos     string
		shell    string
		home     string
		wantExec string
		wantWD   string
		wantUser string
	}{
		{
			name: "linux home path user wins over sudo user", wd: "/home/alice/aviary", usr: &user.User{Username: "root", HomeDir: "/root"},
			goos: "linux", shell: "/bin/bash",
			wantExec: "/bin/bash", wantWD: "/home/alice/aviary", wantUser: "alice",
		},
		{
			name: "unix shell", wd: "/home/alice/aviary", usr: current, goos: "linux", shell: "/bin/bash",
			wantExec: "/bin/bash", wantWD: "/home/alice/aviary", wantUser: "alice",
		},
		{
			name: "macOS users path", wd: "/Users/bob/src/aviary", usr: current, goos: "darwin",
			wantExec: "/bin/zsh", wantWD: "/Users/bob/src/aviary", wantUser: "bob",
		},
		{
			name: "default shell", wd: "/opt/aviary", usr: current, goos: "darwin",
			wantExec: "/bin/zsh", wantWD: "/opt/aviary", wantUser: "builder",
		},
		{
			name: "other path keeps current user", wd: "/opt/aviary", usr: current, goos: "linux", shell: "/bin/fish",
			wantExec: "/bin/fish", wantWD: "/opt/aviary", wantUser: "builder",
		},
		{
			name: "no working dir falls back to user home", wd: "", usr: current, goos: "linux", shell: "/bin/sh",
			wantExec: "/bin/sh", wantWD: current.HomeDir, wantUser: "builder",
		},
		{
			name: "unknown user falls back to HOME", wd: "", userErr: errors.New("no user"), goos: "linux", shell: "/bin/sh", home: filepath.Join("/var", "home"),
			wantExec: "/bin/sh", wantWD: filepath.Join("/var", "home"), wantUser: "",
		},
		{
			name: "windows runs through cmd", wd: `C:\src\aviary`, usr: current, goos: "windows",
			wantExec: "cmd.exe", wantWD: `C:\src\aviary`, wantUser: "builder",
		},
	}
	for _, svc := range devServices {
		t.Run(svc.name, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Setenv("SHELL", tt.shell)
					t.Setenv("HOME", tt.home)
					stubServiceEnv(t, tt.wd, tt.usr, tt.userErr, tt.goos)
					fake := stubServiceManager(t, nil)

					wantArgs := []string{"-lc", "source ~/.zshrc; " + svc.command}
					if tt.goos == "windows" {
						wantArgs = []string{"/C", svc.command}
					}

					out, err := captureStdout(t, func() error { return svc.run(nil, nil) })
					require.NoError(t, err)
					require.Len(t, fake.installed, 1)
					assert.Equal(t, server.ServiceOptions{
						Name:        svc.name,
						DisplayName: svc.displayName,
						Description: svc.description,
						Exec:        tt.wantExec,
						Args:        wantArgs,
						WorkingDir:  tt.wantWD,
						User:        tt.wantUser,
					}, fake.installed[0])
					assert.Equal(t, "Installing service: "+svc.name+"\nService installed and started: "+svc.name+"\n", out)
				})
			}
		})
	}
}

func TestRunInstallDevServicesError(t *testing.T) {
	for _, svc := range devServices {
		t.Run(svc.name, func(t *testing.T) {
			stubServiceEnv(t, "/opt/aviary", &user.User{Username: "builder"}, nil, "linux")
			boom := errors.New("install failed")
			stubServiceManager(t, boom)
			out, err := captureStdout(t, func() error { return svc.run(nil, nil) })
			require.ErrorIs(t, err, boom)
			assert.Equal(t, "Installing service: "+svc.name+"\n", out)
		})
	}
}

// useStatusPIDFile points runStatus at a temp config and PID file.
func useStatusPIDFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "aviary.pid")
	t.Setenv("AVIARY_CONFIG_BASE_DIR", "")
	t.Setenv("AVIARY_PID_FILE", pidFile)
	orig := cfgFile
	t.Cleanup(func() { cfgFile = orig })
	cfgFile = filepath.Join(dir, "aviary.yaml")
	return pidFile
}

func TestRunStatusRunning(t *testing.T) {
	pidFile := useStatusPIDFile(t)
	require.NoError(t, os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600))

	out, err := captureStdout(t, func() error { return runStatus(nil, nil) })
	require.NoError(t, err)
	assert.Equal(t, "Service 'aviary' is running (PID "+strconv.Itoa(os.Getpid())+").\nPID file: "+pidFile+"\n", out)
	assert.FileExists(t, pidFile, "a live server's PID file is kept")
}

func TestRunStatusNotRunning(t *testing.T) {
	pidFile := useStatusPIDFile(t)

	out, err := captureStdout(t, func() error { return runStatus(nil, nil) })
	require.NoError(t, err)
	assert.Equal(t, "Service 'aviary' is not running.\n", out)
	assert.NoFileExists(t, pidFile)
}

func TestRunStatusRemovesStalePIDFile(t *testing.T) {
	pidFile := useStatusPIDFile(t)
	// A process that has already exited leaves a stale PID behind.
	child := exec.Command(os.Args[0], "-test.run=^$")
	require.NoError(t, child.Run())
	require.False(t, server.ProcessAlive(child.Process.Pid))
	require.NoError(t, os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)+"\n"), 0o600))

	out, err := captureStdout(t, func() error { return runStatus(nil, nil) })
	require.NoError(t, err)
	assert.Equal(t, "Service 'aviary' is not running.\n", out)
	assert.NoFileExists(t, pidFile, "the stale PID file is cleaned up")
}

func TestRunStatusReportsUnreadablePIDFile(t *testing.T) {
	pidFile := useStatusPIDFile(t)
	require.NoError(t, os.WriteFile(pidFile, []byte("not-a-pid\n"), 0o600))

	err := runStatus(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checking server status: parsing PID")
	assert.FileExists(t, pidFile, "an unreadable PID file is left for inspection")
}
