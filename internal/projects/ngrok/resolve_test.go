package ngrok

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// stubLookPath makes the test decide which executables are installed.
func stubLookPath(t *testing.T, installed ...string) {
	t.Helper()
	present := map[string]bool{}
	for _, name := range installed {
		present[name] = true
	}
	previous := lookPath
	lookPath = func(name string) (string, error) {
		if present[name] {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { lookPath = previous })
}

// An installed agent keeps working exactly as it did before issue #498, npx or
// no npx: the fallback must not add a download to a machine that needs none.
func TestResolveCommandPrefersAnInstalledNgrok(t *testing.T) {
	stubLookPath(t, "ngrok", "npx")
	invocation, err := resolveCommand(DefaultBinary)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if invocation.name != "ngrok" || invocation.viaNpx || len(invocation.prefix) != 0 {
		t.Fatalf("invocation = %+v, want the installed ngrok", invocation)
	}
	if invocation.timeout() != startTimeout {
		t.Fatalf("timeout = %v, want %v", invocation.timeout(), startTimeout)
	}
}

func TestResolveCommandFallsBackToNpx(t *testing.T) {
	stubLookPath(t, "npx")
	invocation, err := resolveCommand(DefaultBinary)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if invocation.name != "npx" || !invocation.viaNpx {
		t.Fatalf("invocation = %+v, want the npx fallback", invocation)
	}
	got := invocation.args("127.0.0.1:8080")
	want := []string{"--yes", "ngrok", "http", "--log=stdout", "--log-format=json", "--log-level=info", "127.0.0.1:8080"}
	if len(got) != len(want) {
		t.Fatalf("arguments = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argument %d = %q, want %q (full: %q)", i, got[i], want[i], got)
		}
	}
	// The first npx run downloads the package and the agent before ngrok says
	// anything, which does not fit in the budget an installed agent gets.
	if invocation.timeout() <= startTimeout {
		t.Fatalf("timeout = %v, want more than the installed budget %v", invocation.timeout(), startTimeout)
	}
}

// An empty binary is the same request as the default one, so it must not be
// looked up as a program literally named "".
func TestResolveCommandTreatsAnEmptyBinaryAsTheDefault(t *testing.T) {
	stubLookPath(t, "npx")
	invocation, err := resolveCommand("")
	if err != nil || !invocation.viaNpx {
		t.Fatalf("invocation = %+v, error = %v, want the npx fallback", invocation, err)
	}
}

// -ngrok-binary names the agent the user means to run. Substituting a
// downloaded one would turn a typo into a silently different tunnel.
func TestResolveCommandDoesNotSubstituteForAnExplicitBinary(t *testing.T) {
	stubLookPath(t, "npx")
	invocation, err := resolveCommand("/opt/ngrok-custom")
	if err == nil {
		t.Fatalf("invocation = %+v, want an error naming the missing binary", invocation)
	}
	if !strings.Contains(err.Error(), "/opt/ngrok-custom") {
		t.Fatalf("error = %v, want it to name the missing binary", err)
	}
}

func TestResolveCommandWithoutNgrokOrNpx(t *testing.T) {
	stubLookPath(t)
	_, err := resolveCommand(DefaultBinary)
	if err == nil {
		t.Fatal("resolve succeeded with neither ngrok nor npx installed")
	}
	if !strings.Contains(err.Error(), "npx") || !strings.Contains(err.Error(), "ngrok") {
		t.Fatalf("error = %v, want it to name both ways out", err)
	}
}

// Start must report a resolution failure rather than trying to exec it.
func TestStartReportsAnUnresolvableCommand(t *testing.T) {
	stubLookPath(t)
	tunnel, err := Start(context.Background(), DefaultBinary, "127.0.0.1:8080", nil)
	if err == nil {
		_ = tunnel.Close()
		t.Fatal("start succeeded with no way to run ngrok")
	}
	if errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("error = %v, want the explanatory message rather than the raw lookup failure", err)
	}
}

// npx puts its own progress on the streams glorp reads the tunnel URL from.
func TestNgrokLogWatcherIgnoresNpmProgress(t *testing.T) {
	watcher := &ngrokLogWatcher{}
	watcher.Write([]byte("npm warn deprecated uuid@8.3.2: no longer supported\n"))
	watcher.Write([]byte("npm notice New major version of npm available!\n"))
	if failure := watcher.Failure(); failure != "" {
		t.Fatalf("npm chatter was taken for an ngrok failure: %q", failure)
	}
	// A package that cannot be fetched is why no tunnel came up, so it stays.
	watcher.Write([]byte("npm error 404 Not Found - GET https://registry.npmjs.org/ngrok\n"))
	if failure := watcher.Failure(); !strings.Contains(failure, "404") {
		t.Fatalf("failure = %q, want the npm error kept", failure)
	}
}
