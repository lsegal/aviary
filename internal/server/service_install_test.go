package server

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const serviceHelperMarker = "aviary-service-helper"

// TestServiceHelperProcess is re-executed by fakeServiceExec in place of the
// real service manager commands. It prints the requested output and exits with
// the requested code. Arguments are used instead of environment variables
// because trySudoRetry replaces the child's environment.
func TestServiceHelperProcess(*testing.T) {
	i := slices.Index(os.Args, serviceHelperMarker)
	if i < 0 || i+2 >= len(os.Args) {
		return
	}
	code, _ := strconv.Atoi(os.Args[i+1])
	fmt.Print(os.Args[i+2])
	os.Exit(code)
}

// fakeServiceExec records every command the installer would run. Commands
// whose command line starts with a key in fail exit 1 with that key's output.
type fakeServiceExec struct {
	calls []string
	cmds  []*exec.Cmd
	fail  map[string]string
}

func (f *fakeServiceExec) command(name string, args ...string) *exec.Cmd {
	line := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, line)
	code, out := "0", ""
	for prefix, o := range f.fail {
		if strings.HasPrefix(line, prefix) {
			code, out = "1", o
		}
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestServiceHelperProcess$", "--", serviceHelperMarker, code, out)
	f.cmds = append(f.cmds, cmd)
	return cmd
}

// stubServicePlatform replaces every platform hook for the duration of the
// test. The home directory, user lookups, and launchd directory all resolve
// inside temp dirs, and the sudo retry is disabled unless a test re-enables it.
func stubServicePlatform(t *testing.T, goos string) (*fakeServiceExec, string) {
	t.Helper()
	saved := struct {
		goos       string
		command    func(string, ...string) *exec.Cmd
		lookPath   func(string) (string, error)
		lookupUser func(string) (*osuser.User, error)
		home       func() (string, error)
		executable func() (string, error)
		launchd    string
	}{serviceGOOS, execCommand, lookPath, lookupUser, userHomeDir, osExecutable, launchDaemonsDir}
	t.Cleanup(func() {
		serviceGOOS, execCommand, lookPath, lookupUser = saved.goos, saved.command, saved.lookPath, saved.lookupUser
		userHomeDir, osExecutable, launchDaemonsDir = saved.home, saved.executable, saved.launchd
	})

	home := t.TempDir()
	fake := &fakeServiceExec{fail: map[string]string{}}
	serviceGOOS = goos
	execCommand = fake.command
	lookPath = func(file string) (string, error) { return file, nil }
	lookupUser = func(name string) (*osuser.User, error) {
		return &osuser.User{Username: name, HomeDir: filepath.Join(home, name)}, nil
	}
	userHomeDir = func() (string, error) { return home, nil }
	osExecutable = func() (string, error) { return "/opt/aviary/bin/aviary", nil }
	launchDaemonsDir = t.TempDir()
	t.Setenv("SUDO_USER", "")
	t.Setenv("AVIARY_TRIED_SUDO", "1")
	return fake, home
}

func assertCalls(t *testing.T, fake *fakeServiceExec, want ...string) {
	t.Helper()
	if !slices.Equal(fake.calls, want) {
		t.Fatalf("commands = %q, want %q", fake.calls, want)
	}
}

func assertErrContains(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil, want error containing %q", parts)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Fatalf("err = %q, want it to contain %q", err, p)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestEscapeArgs(t *testing.T) {
	got := escapeArgs([]string{"serve", "has space", "tab\there", "line\nbreak", "cr\rx", `back\slash`, `dq"x`, "it's"})
	want := []string{"serve", "'has space'", "'tab\there'", "'line\nbreak'", "'cr\rx'", `'back\slash'`, `'dq"x'`, `'it'\''s'`}
	if !slices.Equal(got, want) {
		t.Fatalf("escapeArgs = %q, want %q", got, want)
	}
	if got := escapeArgs(nil); len(got) != 0 {
		t.Fatalf("escapeArgs(nil) = %q, want empty", got)
	}
}

func TestEscapeSystemPath(t *testing.T) {
	for in, want := range map[string]string{
		"/usr/bin/aviary":       "/usr/bin/aviary",
		"/Program Files/aviary": `"/Program Files/aviary"`,
		"":                      "",
	} {
		if got := escapeSystemPath(in); got != want {
			t.Errorf("escapeSystemPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEscapeXML(t *testing.T) {
	if got, want := escapeXML(`a & <b> "c"`), "a &amp; &lt;b&gt; &quot;c&quot;"; got != want {
		t.Fatalf("escapeXML = %q, want %q", got, want)
	}
	if got := escapeXML("&lt;"); got != "&amp;lt;" {
		t.Fatalf("escapeXML escapes & first: got %q", got)
	}
}

func TestValidateWindows(t *testing.T) {
	stubServicePlatform(t, "linux")
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if err := validateWindows(); err != nil {
		t.Fatalf("non-windows validateWindows = %v, want nil", err)
	}

	serviceGOOS = "windows"
	assertErrContains(t, validateWindows(), "sc.exe not found in PATH")

	var looked string
	lookPath = func(file string) (string, error) { looked = file; return `C:\Windows\System32\sc.exe`, nil }
	if err := validateWindows(); err != nil {
		t.Fatalf("validateWindows = %v, want nil", err)
	}
	if looked != "sc" {
		t.Fatalf("looked up %q, want sc", looked)
	}
}

func TestRunCmd(t *testing.T) {
	fake, _ := stubServicePlatform(t, "linux")
	if err := runCmd("systemctl", "--user", "status"); err != nil {
		t.Fatalf("runCmd = %v, want nil", err)
	}
	fake.fail["systemctl"] = "unit not found"
	assertErrContains(t, runCmd("systemctl", "--user", "status"), "systemctl [--user status]", "exit status 1", "unit not found")
	assertCalls(t, fake, "systemctl --user status", "systemctl --user status")
}

func TestRunCmdRealProcessFailure(t *testing.T) {
	err := runCmd(filepath.Join(t.TempDir(), "missing-binary"))
	assertErrContains(t, err, "missing-binary")
}

func TestInstallServiceSystemd(t *testing.T) {
	fake, home := stubServicePlatform(t, "linux")
	err := InstallService(ServiceOptions{
		Name:        "aviary",
		Description: "Aviary server",
		Exec:        "/opt/my apps/aviary",
		Args:        []string{"serve", "--config", "/etc/my aviary.yaml"},
		WorkingDir:  "/srv/my data",
	})
	if err != nil {
		t.Fatalf("InstallService: %v", err)
	}

	unit := readFile(t, filepath.Join(home, ".config", "systemd", "user", "aviary.service"))
	for _, want := range []string{
		"Description=Aviary server\n",
		"WorkingDirectory=\"/srv/my data\"\n",
		"ExecStart=\"/opt/my apps/aviary\" serve --config '/etc/my aviary.yaml'\n",
		"Restart=on-failure\n",
		"WantedBy=graphical.target\n",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q:\n%s", want, unit)
		}
	}
	assertCalls(t, fake,
		"systemctl --user daemon-reload",
		"systemctl --user enable aviary",
		"systemctl --user start aviary")
}

func TestInstallServiceDevNames(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		for _, tc := range []struct{ name, args, want string }{
			{"aviary", "-c pnpm docs:dev", "aviary-dev-docs"},
			{"aviary", "-c pnpm dev", "aviary-dev"},
			{"custom", "-c pnpm dev", "custom"},
			{"aviary", "serve", "aviary"},
		} {
			t.Run(goos+"/"+tc.want+"/"+tc.args, func(t *testing.T) {
				fake, _ := stubServicePlatform(t, goos)
				opts := ServiceOptions{Name: tc.name, Exec: "/bin/sh", Args: strings.SplitN(tc.args, " ", 2)}
				if err := InstallService(opts); err != nil {
					t.Fatalf("InstallService: %v", err)
				}
				last := fake.calls[len(fake.calls)-1]
				want := "systemctl --user start " + tc.want
				if goos == "darwin" {
					want = "launchctl load " + filepath.Join(launchDaemonsDir, "bot.aviary."+tc.want+".plist")
				}
				if last != want {
					t.Fatalf("last command = %q, want %q", last, want)
				}
			})
		}
	}
}

func TestInstallServiceInfersUser(t *testing.T) {
	for _, tc := range []struct {
		name, user, sudoUser, workingDir, home, want string
	}{
		{name: "explicit user", user: "zoe", sudoUser: "alice", want: "zoe"},
		{name: "root becomes sudo user", user: "root", sudoUser: "alice", want: "alice"},
		{name: "sudo user root ignored", sudoUser: "root", workingDir: "/home/bob/aviary", want: "bob"},
		{name: "linux working dir", workingDir: "/home/bob/aviary", want: "bob"},
		{name: "mac working dir", workingDir: "/Users/carol", want: "carol"},
		{name: "empty home segment", workingDir: "/home/", home: "/Users/erin", want: "erin"},
		{name: "linux home dir", workingDir: "/srv/aviary", home: "/home/dave", want: "dave"},
		{name: "mac home dir", home: "/Users/erin/", want: "erin"},
		{name: "root home under /home", home: "/home/root", want: ""},
		{name: "root home under /Users", home: "/Users/root", want: ""},
		{name: "home elsewhere", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, home := stubServicePlatform(t, "linux")
			t.Setenv("SUDO_USER", tc.sudoUser)
			// Each test home only feeds user inference; when no user is
			// inferred, the unit is written under the temp home instead.
			calls := 0
			userHomeDir = func() (string, error) {
				calls++
				if calls == 1 && tc.home != "" {
					return tc.home, nil
				}
				return home, nil
			}
			var looked []string
			base := lookupUser
			lookupUser = func(name string) (*osuser.User, error) {
				looked = append(looked, name)
				return base(name)
			}

			err := InstallService(ServiceOptions{Name: "aviary", Exec: "/bin/aviary", User: tc.user, WorkingDir: tc.workingDir})
			if err != nil {
				t.Fatalf("InstallService: %v", err)
			}
			unitHome := home
			if tc.want == "" {
				if len(looked) != 0 {
					t.Fatalf("looked up users %q, want none", looked)
				}
			} else {
				if !slices.Equal(looked, []string{tc.want}) {
					t.Fatalf("looked up users %q, want [%s]", looked, tc.want)
				}
				unitHome = filepath.Join(home, tc.want)
			}
			if _, err := os.Stat(filepath.Join(unitHome, ".config", "systemd", "user", "aviary.service")); err != nil {
				t.Fatalf("unit file not written under %s: %v", unitHome, err)
			}
			if len(fake.calls) != 3 {
				t.Fatalf("commands = %q, want 3 systemctl calls", fake.calls)
			}
		})
	}
}

func TestInstallSystemdErrors(t *testing.T) {
	t.Run("user lookup", func(t *testing.T) {
		fake, _ := stubServicePlatform(t, "linux")
		lookupUser = func(string) (*osuser.User, error) { return nil, errors.New("no such user") }
		assertErrContains(t, installSystemd(ServiceOptions{Name: "aviary", User: "ghost"}), "looking up user ghost", "no such user")
		assertCalls(t, fake)
	})
	t.Run("home dir", func(t *testing.T) {
		stubServicePlatform(t, "linux")
		userHomeDir = func() (string, error) { return "", errors.New("no home") }
		assertErrContains(t, installSystemd(ServiceOptions{Name: "aviary"}), "getting home dir", "no home")
	})
	t.Run("create unit dir", func(t *testing.T) {
		_, home := stubServicePlatform(t, "linux")
		if err := os.WriteFile(filepath.Join(home, ".config"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		assertErrContains(t, installSystemd(ServiceOptions{Name: "aviary"}), "creating systemd user dir")
	})
	t.Run("write unit", func(t *testing.T) {
		_, home := stubServicePlatform(t, "linux")
		if err := os.MkdirAll(filepath.Join(home, ".config", "systemd", "user", "aviary.service"), 0o755); err != nil {
			t.Fatal(err)
		}
		assertErrContains(t, installSystemd(ServiceOptions{Name: "aviary"}), "writing unit file")
	})
	for _, tc := range []struct{ fail, want string }{
		{"systemctl --user daemon-reload", "systemctl daemon-reload"},
		{"systemctl --user enable", "systemctl enable"},
		{"systemctl --user start", "systemctl start"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			fake, _ := stubServicePlatform(t, "linux")
			fake.fail[tc.fail] = "failed: " + tc.want
			assertErrContains(t, installSystemd(ServiceOptions{Name: "aviary"}), tc.want+":", "failed: "+tc.want)
			if last := fake.calls[len(fake.calls)-1]; !strings.HasPrefix(last, tc.fail) {
				t.Fatalf("stopped after %q, want %q", last, tc.fail)
			}
		})
	}
}

func TestUninstallServiceSystemd(t *testing.T) {
	fake, home := stubServicePlatform(t, "linux")
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	unit := filepath.Join(unitDir, "aviary.service")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unit, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Stop and disable failures are ignored: the unit may not be running.
	fake.fail["systemctl --user stop"] = "not loaded"
	fake.fail["systemctl --user disable"] = "not enabled"

	if err := UninstallService("aviary"); err != nil {
		t.Fatalf("UninstallService: %v", err)
	}
	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatalf("unit file still present: %v", err)
	}
	assertCalls(t, fake,
		"systemctl --user stop aviary",
		"systemctl --user disable aviary",
		"systemctl --user daemon-reload")

	// A second uninstall with the unit already gone still succeeds.
	if err := UninstallService("aviary"); err != nil {
		t.Fatalf("UninstallService on missing unit: %v", err)
	}
}

func TestUninstallSystemdErrors(t *testing.T) {
	t.Run("home dir", func(t *testing.T) {
		stubServicePlatform(t, "linux")
		userHomeDir = func() (string, error) { return "", errors.New("no home") }
		assertErrContains(t, uninstallSystemd("aviary"), "getting home dir")
	})
	t.Run("remove unit", func(t *testing.T) {
		_, home := stubServicePlatform(t, "linux")
		dir := filepath.Join(home, ".config", "systemd", "user", "aviary.service")
		if err := os.MkdirAll(filepath.Join(dir, "child"), 0o755); err != nil {
			t.Fatal(err)
		}
		assertErrContains(t, uninstallSystemd("aviary"), "removing unit file")
	})
	t.Run("daemon-reload", func(t *testing.T) {
		fake, _ := stubServicePlatform(t, "linux")
		fake.fail["systemctl --user daemon-reload"] = "bus error"
		assertErrContains(t, uninstallSystemd("aviary"), "systemctl daemon-reload", "bus error")
	})
}

func TestInstallServiceLaunchd(t *testing.T) {
	fake, _ := stubServicePlatform(t, "darwin")
	err := InstallService(ServiceOptions{
		Name:       "aviary",
		Exec:       "/Applications/A&B/aviary",
		Args:       []string{"serve", `--label="<x>"`},
		WorkingDir: "/Users/alice/R&D",
	})
	if err != nil {
		t.Fatalf("InstallService: %v", err)
	}
	path := filepath.Join(launchDaemonsDir, "bot.aviary.aviary.plist")
	plist := readFile(t, path)
	for _, want := range []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		"<key>Label</key>\n  <string>bot.aviary.aviary</string>\n",
		"<key>KeepAlive</key>\n  <true/>\n",
		"<key>RunAtLoad</key>\n  <true/>\n",
		"<key>WorkingDirectory</key>\n  <string>/Users/alice/R&amp;D</string>\n",
		"<array>\n    <string>/Applications/A&amp;B/aviary</string>\n    <string>serve</string>\n    <string>--label=&quot;&lt;x&gt;&quot;</string>\n  </array>\n",
		"</dict>\n</plist>\n",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q:\n%s", want, plist)
		}
	}
	assertCalls(t, fake, "launchctl load "+path)
}

func TestInstallLaunchdErrors(t *testing.T) {
	t.Run("write plist", func(t *testing.T) {
		fake, _ := stubServicePlatform(t, "darwin")
		launchDaemonsDir = filepath.Join(launchDaemonsDir, "missing")
		assertErrContains(t, installLaunchd(ServiceOptions{Name: "aviary"}), "writing plist")
		assertCalls(t, fake)
	})
	t.Run("load", func(t *testing.T) {
		fake, _ := stubServicePlatform(t, "darwin")
		fake.fail["launchctl load"] = "service already loaded"
		assertErrContains(t, installLaunchd(ServiceOptions{Name: "aviary"}), "launchctl load", "service already loaded")
	})
}

func TestUninstallServiceLaunchd(t *testing.T) {
	fake, _ := stubServicePlatform(t, "darwin")
	path := filepath.Join(launchDaemonsDir, "bot.aviary.aviary.plist")
	if err := os.WriteFile(path, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake.fail["launchctl unload"] = "could not find service"

	if err := UninstallService("aviary"); err != nil {
		t.Fatalf("UninstallService: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("plist still present: %v", err)
	}
	assertCalls(t, fake, "launchctl unload "+path)

	if err := UninstallService("aviary"); err != nil {
		t.Fatalf("UninstallService on missing plist: %v", err)
	}
}

func TestUninstallLaunchdRemoveError(t *testing.T) {
	stubServicePlatform(t, "darwin")
	if err := os.MkdirAll(filepath.Join(launchDaemonsDir, "bot.aviary.aviary.plist", "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	assertErrContains(t, uninstallLaunchd("aviary"), "removing plist")
}

func TestInstallServiceWindows(t *testing.T) {
	fake, _ := stubServicePlatform(t, "windows")
	// SUDO_USER inference still runs but has no effect on Windows services.
	t.Setenv("SUDO_USER", "alice")
	err := InstallService(ServiceOptions{Name: "aviary", Exec: `C:\aviary\aviary.exe`, Args: []string{"serve", "my config.yaml"}})
	if err != nil {
		t.Fatalf("InstallService: %v", err)
	}
	assertCalls(t, fake,
		`sc create aviary binPath= C:\aviary\aviary.exe serve 'my config.yaml' start= auto`,
		"sc start aviary")
	if got := fake.cmds[0].Args[len(fake.cmds[0].Args)-3:]; got[0] != serviceHelperMarker {
		t.Fatalf("sc create should run through the helper, got %q", got)
	}
}

func TestInstallWindowsServiceErrors(t *testing.T) {
	t.Run("validate", func(t *testing.T) {
		fake, _ := stubServicePlatform(t, "windows")
		lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
		assertErrContains(t, InstallService(ServiceOptions{Name: "aviary"}), "sc.exe not found in PATH")
		assertCalls(t, fake)
	})
	t.Run("create", func(t *testing.T) {
		fake, _ := stubServicePlatform(t, "windows")
		fake.fail["sc create"] = "[SC] CreateService FAILED 1073"
		assertErrContains(t, installWindowsService(ServiceOptions{Name: "aviary", Exec: "aviary.exe"}), "sc create failed", "exit status 1", "CreateService FAILED 1073")
		assertCalls(t, fake, "sc create aviary binPath= aviary.exe  start= auto")
	})
	t.Run("start", func(t *testing.T) {
		fake, _ := stubServicePlatform(t, "windows")
		fake.fail["sc start"] = "[SC] StartService FAILED 1053"
		assertErrContains(t, installWindowsService(ServiceOptions{Name: "aviary", Exec: "aviary.exe"}), "sc start:", "StartService FAILED 1053")
	})
}

func TestUninstallServiceWindows(t *testing.T) {
	fake, _ := stubServicePlatform(t, "windows")
	fake.fail["sc stop"] = "service not started"
	if err := UninstallService("aviary"); err != nil {
		t.Fatalf("UninstallService: %v", err)
	}
	assertCalls(t, fake, "sc stop aviary", "sc delete aviary")

	fake.fail["sc delete"] = "[SC] OpenService FAILED 1060"
	assertErrContains(t, UninstallService("aviary"), "sc delete:", "OpenService FAILED 1060")
}

func TestStartStopService(t *testing.T) {
	for _, tc := range []struct{ goos, start, stop string }{
		{"linux", "systemctl --user start aviary", "systemctl --user stop aviary"},
		{"darwin", "launchctl start bot.aviary.aviary", "launchctl stop bot.aviary.aviary"},
		{"windows", "sc start aviary", "sc stop aviary"},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			fake, _ := stubServicePlatform(t, tc.goos)
			if err := StartService("aviary"); err != nil {
				t.Fatalf("StartService: %v", err)
			}
			if err := StopService("aviary"); err != nil {
				t.Fatalf("StopService: %v", err)
			}
			assertCalls(t, fake, tc.start, tc.stop)

			start, _, _ := strings.Cut(tc.start, " aviary")
			stop, _, _ := strings.Cut(tc.stop, " aviary")
			fake.fail[start] = "start refused"
			fake.fail[stop] = "stop refused"
			assertErrContains(t, StartService("aviary"), "start refused")
			assertErrContains(t, StopService("aviary"), "stop refused")
		})
	}
}

func TestServiceUnsupportedOS(t *testing.T) {
	fake, _ := stubServicePlatform(t, "plan9")
	for name, err := range map[string]error{
		"install":   InstallService(ServiceOptions{Name: "aviary"}),
		"uninstall": UninstallService("aviary"),
		"start":     StartService("aviary"),
		"stop":      StopService("aviary"),
	} {
		if err == nil || err.Error() != "unsupported OS: plan9" {
			t.Errorf("%s err = %v, want unsupported OS: plan9", name, err)
		}
	}
	assertCalls(t, fake)
}

func TestTrySudoRetry(t *testing.T) {
	permErr := errors.New("open /Library/LaunchDaemons/x.plist: permission denied")

	t.Run("skipped", func(t *testing.T) {
		for _, tc := range []struct {
			name, goos, tried string
			err               error
			exeErr            error
		}{
			{name: "windows", goos: "windows", err: permErr},
			{name: "already tried", goos: "linux", tried: "1", err: permErr},
			{name: "not a permission error", goos: "linux", err: errors.New("disk full")},
			{name: "executable unknown", goos: "darwin", err: permErr, exeErr: errors.New("no exe")},
		} {
			t.Run(tc.name, func(t *testing.T) {
				fake, _ := stubServicePlatform(t, tc.goos)
				t.Setenv("AVIARY_TRIED_SUDO", tc.tried)
				if tc.exeErr != nil {
					osExecutable = func() (string, error) { return "", tc.exeErr }
				}
				if got := trySudoRetry("install", tc.err); got != tc.err {
					t.Fatalf("trySudoRetry = %v, want original error %v", got, tc.err)
				}
				assertCalls(t, fake)
			})
		}
	})

	t.Run("retries permission-like errors", func(t *testing.T) {
		for _, msg := range []string{
			"Permission denied",
			"Access denied",
			"Interactive authentication required.",
			"Authentication required",
			"polkit agent unavailable",
			"Authorization not available",
		} {
			t.Run(msg, func(t *testing.T) {
				fake, _ := stubServicePlatform(t, "linux")
				t.Setenv("AVIARY_TRIED_SUDO", "")
				if err := trySudoRetry("uninstall", errors.New(msg)); err != nil {
					t.Fatalf("trySudoRetry = %v, want nil after successful sudo", err)
				}
				want := strings.Join(append([]string{"sudo", "-E", "/opt/aviary/bin/aviary"}, os.Args[1:]...), " ")
				assertCalls(t, fake, want)
				if env := fake.cmds[0].Env; !slices.Contains(env, "AVIARY_TRIED_SUDO=1") {
					t.Fatalf("sudo env missing AVIARY_TRIED_SUDO=1")
				}
				if fake.cmds[0].Stdout != os.Stdout || fake.cmds[0].Stderr != os.Stderr {
					t.Fatalf("sudo output not attached to the terminal")
				}
			})
		}
	})

	t.Run("sudo fails", func(t *testing.T) {
		fake, _ := stubServicePlatform(t, "darwin")
		t.Setenv("AVIARY_TRIED_SUDO", "")
		fake.fail["sudo"] = ""
		err := trySudoRetry("install", permErr)
		if !errors.Is(err, permErr) {
			t.Fatalf("err = %v, want it to wrap %v", err, permErr)
		}
		assertErrContains(t, err, "sudo retry failed", "exit status 1")
	})
}

func TestServiceInstallFailuresRetryWithSudo(t *testing.T) {
	for _, tc := range []struct {
		goos, fail string
		run        func() error
	}{
		{"linux", "systemctl --user daemon-reload", func() error { return InstallService(ServiceOptions{Name: "aviary"}) }},
		{"linux", "systemctl --user daemon-reload", func() error { return UninstallService("aviary") }},
		{"darwin", "launchctl load", func() error { return InstallService(ServiceOptions{Name: "aviary"}) }},
	} {
		t.Run(tc.goos+"/"+tc.fail, func(t *testing.T) {
			fake, _ := stubServicePlatform(t, tc.goos)
			t.Setenv("AVIARY_TRIED_SUDO", "")
			fake.fail[tc.fail] = "Interactive authentication required."
			if err := tc.run(); err != nil {
				t.Fatalf("err = %v, want nil after sudo retry", err)
			}
			if last := fake.calls[len(fake.calls)-1]; !strings.HasPrefix(last, "sudo -E /opt/aviary/bin/aviary") {
				t.Fatalf("last command = %q, want sudo retry", last)
			}
		})
	}

	t.Run("darwin uninstall", func(t *testing.T) {
		stubServicePlatform(t, "darwin")
		if err := os.MkdirAll(filepath.Join(launchDaemonsDir, "bot.aviary.aviary.plist", "child"), 0o755); err != nil {
			t.Fatal(err)
		}
		// The remove error is not permission related, so it is returned as-is.
		assertErrContains(t, UninstallService("aviary"), "removing plist")
	})
}
