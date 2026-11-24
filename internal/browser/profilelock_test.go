package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLockProfileRefusesASecondClaim(t *testing.T) {
	profile := t.TempDir()
	first, err := LockProfile(profile)
	if err != nil {
		t.Fatalf("first LockProfile() error = %v", err)
	}
	if _, err := LockProfile(profile); !errors.Is(err, ErrProfileLocked) {
		t.Fatalf("second LockProfile() error = %v, want ErrProfileLocked", err)
	}
	first.Release()
	again, err := LockProfile(profile)
	if err != nil {
		t.Fatalf("LockProfile() after release error = %v", err)
	}
	again.Release()
}

func TestClaimProfileGivesALaterInstanceAPrivateDirectory(t *testing.T) {
	profile := t.TempDir()
	lock, dataDir, err := ClaimProfile(profile)
	if err != nil {
		t.Fatalf("ClaimProfile() error = %v", err)
	}
	defer lock.Release()
	if lock == nil || dataDir != profile {
		t.Fatalf("first ClaimProfile() = (%v, %q), want the profile itself", lock, dataDir)
	}
	second, private, err := ClaimProfile(profile)
	if err != nil {
		t.Fatalf("second ClaimProfile() error = %v", err)
	}
	if second != nil {
		t.Fatalf("second ClaimProfile() took the profile lock")
	}
	if private == profile || private == "" {
		t.Fatalf("second ClaimProfile() dataDir = %q, want a private directory", private)
	}
	if info, err := os.Stat(private); err != nil || !info.IsDir() {
		t.Fatalf("private directory %q not created: %v", private, err)
	}
	(&OwnedBrowser{Profile: profile, DataDir: private}).Release()
	if _, err := os.Stat(private); !os.IsNotExist(err) {
		t.Fatalf("private directory %q survived release: %v", private, err)
	}
	if _, err := os.Stat(profile); err != nil {
		t.Fatalf("shared profile removed by a private release: %v", err)
	}
}

func TestClaimProfileSidestepsAviarysRunningBrowser(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Chromium's profile lock is a symlink only on Unix")
	}
	profile := t.TempDir()
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(profile, "SingletonLock")
	if err := os.Symlink(fmt.Sprintf("%s-%d", host, os.Getpid()), link); err != nil {
		t.Fatal(err)
	}
	lock, dataDir, err := ClaimProfile(profile)
	if err != nil {
		t.Fatalf("ClaimProfile() error = %v", err)
	}
	if lock != nil || dataDir == profile {
		t.Fatalf("ClaimProfile() = (%v, %q), want a private directory beside the running browser", lock, dataDir)
	}
	(&OwnedBrowser{Profile: profile, DataDir: dataDir}).Release()

	// A lock left by a browser that has exited is Chromium's to take over.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(host+"-999999999", link); err != nil {
		t.Fatal(err)
	}
	lock, dataDir, err = ClaimProfile(profile)
	if err != nil {
		t.Fatalf("ClaimProfile() error = %v", err)
	}
	defer lock.Release()
	if lock == nil || dataDir != profile {
		t.Fatalf("ClaimProfile() with a stale lock = (%v, %q), want the profile itself", lock, dataDir)
	}
}

// TestCheckProfileFreeRefusesAProfileAnOwnedBrowserHolds checks the detached
// browser is not launched onto a profile another run's owned browser holds,
// where Chrome would hand the launch over and the wait would only time out.
func TestCheckProfileFreeRefusesAProfileAnOwnedBrowserHolds(t *testing.T) {
	profile := t.TempDir()
	if err := checkProfileFree(profile); err != nil {
		t.Fatalf("checkProfileFree() on a free profile error = %v", err)
	}
	lock, err := LockProfile(profile)
	if err != nil {
		t.Fatalf("LockProfile() error = %v, want the check to have released its claim", err)
	}
	defer lock.Release()
	if err := checkProfileFree(profile); !errors.Is(err, ErrProfileLocked) {
		t.Fatalf("checkProfileFree() on a held profile error = %v, want ErrProfileLocked", err)
	}
	if err := checkProfileFree(filepath.Join(profile, "missing")); err != nil {
		t.Fatalf("checkProfileFree() on a missing profile error = %v", err)
	}
}

// TestLockProfileTreatsASecondClaimFromThisProcessAsItsOwn checks a claim on
// a profile this same process already holds is reported as this instance's,
// not another one's, and finds the browser running there (issue #1171).
func TestLockProfileTreatsASecondClaimFromThisProcessAsItsOwn(t *testing.T) {
	profile := t.TempDir()
	lock, err := LockProfile(profile)
	if err != nil {
		t.Fatalf("LockProfile() error = %v", err)
	}
	defer lock.Release()
	lock.SetDevTools("http://127.0.0.1:1234")

	// A second spelling of the same directory is the same profile.
	_, err = LockProfile(filepath.Join(profile, ".", "sub", ".."))
	var locked *ProfileLockedError
	if !errors.As(err, &locked) || !locked.Self {
		t.Fatalf("second LockProfile() error = %v, want a ProfileLockedError from this process", err)
	}
	if strings.Contains(err.Error(), "another aviary instance") {
		t.Fatalf("second LockProfile() error = %q, blames another instance", err)
	}
	if got := sameProcessBrowser(profile); got != "http://127.0.0.1:1234" {
		t.Fatalf("sameProcessBrowser() = %q, want the holder's DevTools endpoint", got)
	}
	lock.Release()
	if got := sameProcessBrowser(profile); got != "" {
		t.Fatalf("sameProcessBrowser() after release = %q, want none", got)
	}
}

// TestLockProfileReclaimsALockLeftByAnExitedRun checks a lock record left by a
// process that has exited does not block the profile: the claim goes ahead and
// replaces the record with this process's own.
func TestLockProfileReclaimsALockLeftByAnExitedRun(t *testing.T) {
	profile := t.TempDir()
	host, _ := os.Hostname()
	stale, _ := json.Marshal(ProfileOwner{PID: deadPID(t), Hostname: host, Started: time.Now().Add(-time.Hour)})
	if err := os.WriteFile(filepath.Join(profile, profileOwnerName), stale, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, profileLockName), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := InspectProfile(profile)
	if err != nil {
		t.Fatalf("InspectProfile() error = %v", err)
	}
	if status.Held || !status.Reclaimed || status.Owner == nil {
		t.Fatalf("InspectProfile() = %+v, want a free profile with the stale record reclaimed", status)
	}
	if _, err := os.Stat(filepath.Join(profile, profileOwnerName)); !os.IsNotExist(err) {
		t.Fatalf("stale lock record survived inspection: %v", err)
	}

	if err := os.WriteFile(filepath.Join(profile, profileOwnerName), stale, 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := LockProfile(profile)
	if err != nil {
		t.Fatalf("LockProfile() over a stale record error = %v", err)
	}
	defer lock.Release()
	if owner := readProfileOwner(profile); owner == nil || owner.PID != os.Getpid() {
		t.Fatalf("lock record after the claim = %+v, want this process", owner)
	}
	lock.Release()
	if _, err := os.Stat(filepath.Join(profile, profileOwnerName)); !os.IsNotExist(err) {
		t.Fatalf("lock record survived release: %v", err)
	}
}

// TestLockProfileNamesAnotherLiveInstance checks a profile another running
// process holds is refused with that process named: its PID, start time,
// config directory and DevTools endpoint.
func TestLockProfileNamesAnotherLiveInstance(t *testing.T) {
	profile := t.TempDir()
	pid := holdProfileInChild(t, profile)

	_, err := LockProfile(profile)
	var locked *ProfileLockedError
	if !errors.As(err, &locked) || locked.Self || !errors.Is(err, ErrProfileLocked) {
		t.Fatalf("LockProfile() error = %v, want another instance's ProfileLockedError", err)
	}
	if locked.Owner == nil || locked.Owner.PID != pid {
		t.Fatalf("LockProfile() owner = %+v, want pid %d", locked.Owner, pid)
	}
	for _, want := range []string{"another aviary instance", fmt.Sprintf("pid %d", pid), "started ", "config dir ", "DevTools http://127.0.0.1:4567"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("LockProfile() error = %q, want it to contain %q", err, want)
		}
	}
	if err := checkProfileFree(profile); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("pid %d", pid)) {
		t.Errorf("checkProfileFree() error = %v, want it to name pid %d", err, pid)
	}
	status, err := InspectProfile(profile)
	if err != nil || !status.Held || status.Owner == nil || status.Owner.PID != pid {
		t.Errorf("InspectProfile() = (%+v, %v), want the profile held by pid %d", status, err, pid)
	}
}

// TestManagerReusesThisInstancesBrowserOnTheProfile checks the browser tools
// drive a browser this process already runs on the profile, such as Code
// Loop's, instead of launching one Chrome would only hand over to it.
func TestManagerReusesThisInstancesBrowserOnTheProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			_, _ = fmt.Fprint(w, `{"webSocketDebuggerUrl":"ws://owned/devtools/browser/1"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	oldLaunch := launchChromeWithModeFn
	t.Cleanup(func() { launchChromeWithModeFn = oldLaunch })
	launchChromeWithModeFn = func(*Manager, bool) error {
		t.Error("launched a browser on a profile this instance's browser already runs on")
		return errors.New("unexpected launch")
	}

	profile := t.TempDir()
	lock, err := LockProfile(profile)
	if err != nil {
		t.Fatalf("LockProfile() error = %v", err)
	}
	defer lock.Release()
	lock.SetDevTools(srv.URL)

	port, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager("", port, profile, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL, err := m.ensureChrome(ctx)
	if err != nil {
		t.Fatalf("ensureChrome() error = %v", err)
	}
	if wsURL != "ws://owned/devtools/browser/1" || m.debugURL() != srv.URL {
		t.Fatalf("ensureChrome() = %q via %q, want the owned browser at %s", wsURL, m.debugURL(), srv.URL)
	}
}

// deadPID returns the PID of a process that has exited.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run short-lived process: %v", err)
	}
	return cmd.Process.Pid
}

// holdProfileEnv names the profile TestHelperHoldProfile holds when this test
// binary is run as the child of holdProfileInChild.
const holdProfileEnv = "AVIARY_TEST_HOLD_BROWSER_PROFILE"

// holdProfileInChild runs another process that holds profile, standing in for
// a second Aviary instance, until the test ends. It returns that process's PID.
func holdProfileInChild(t *testing.T, profile string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperHoldProfile$")
	cmd.Env = append(os.Environ(), holdProfileEnv+"="+profile)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "held" {
		t.Fatalf("child did not take the profile: %q, %v", line, err)
	}
	return cmd.Process.Pid
}

// TestHelperHoldProfile is the child of holdProfileInChild, not a test of its
// own: it holds the profile until its standard input closes.
func TestHelperHoldProfile(t *testing.T) {
	profile := os.Getenv(holdProfileEnv)
	if profile == "" {
		t.Skip("run only as holdProfileInChild's child")
	}
	lock, err := LockProfile(profile)
	if err != nil {
		t.Fatalf("LockProfile() error = %v", err)
	}
	defer lock.Release()
	lock.SetDevTools("http://127.0.0.1:4567")
	fmt.Println("held")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}
