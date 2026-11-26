package ngrok

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeNgrokEnv switches the test binary into a stand-in ngrok agent, so Start
// can be exercised end to end without ever running the real one.
const fakeNgrokEnv = "AVIARY_TEST_FAKE_NGROK"

const fakeTunnelURL = "https://fake-agent.ngrok-free.app"

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeNgrokEnv); mode != "" {
		runFakeNgrok(mode, os.Args[1:])
		return
	}
	os.Exit(m.Run())
}

// runFakeNgrok logs the JSON records the real agent would for mode, then
// stays up like a running tunnel until it is stopped.
func runFakeNgrok(mode string, args []string) {
	want := ngrokArgs("127.0.0.1:8080")
	if strings.Join(args, " ") != strings.Join(want, " ") {
		fmt.Printf(`{"lvl":"crit","msg":"unexpected arguments","err":%q}`+"\n", strings.Join(args, " "))
		os.Exit(2)
	}
	switch mode {
	case "tunnel":
		fmt.Println(`{"lvl":"info","msg":"starting web service","addr":"127.0.0.1:4040"}`)
		fmt.Println(`{"lvl":"info","msg":"started tunnel","url":"` + fakeTunnelURL + `/","addr":"http://127.0.0.1:8080"}`)
	case "error":
		fmt.Println(`{"lvl":"crit","msg":"command failed","err":"authentication failed: ERR_NGROK_105"}`)
	case "silent":
	}
	time.Sleep(time.Minute)
	os.Exit(0)
}

// startFakeNgrok runs Start against the test binary acting as ngrok in mode.
// The orphan sweep is stubbed out so a real agent on the machine is never
// touched.
func startFakeNgrok(ctx context.Context, t *testing.T, mode string, out io.Writer) (*Tunnel, error) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	stubProcessSweep(t,
		func() ([]processEntry, error) { return nil, nil },
		func(int) error { return errors.New("no process may be terminated") })
	t.Setenv(fakeNgrokEnv, mode)
	tunnel, err := Start(ctx, binary, "127.0.0.1:8080", out)
	if tunnel != nil {
		t.Cleanup(func() { _ = tunnel.Close() })
	}
	return tunnel, err
}

func shortenStartTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()
	previous := startTimeout
	startTimeout = timeout
	t.Cleanup(func() { startTimeout = previous })
}

func TestStartReturnsTheTunnelURLAndCloseStopsTheAgent(t *testing.T) {
	var out bytes.Buffer
	tunnel, err := startFakeNgrok(context.Background(), t, "tunnel", &out)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := tunnel.URL(); got != fakeTunnelURL {
		t.Fatalf("URL = %q, want %q", got, fakeTunnelURL)
	}
	if out.Len() != 0 {
		t.Fatalf("routine agent output was forwarded: %q", out.String())
	}
	if err := tunnel.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if tunnel.cmd.ProcessState == nil {
		t.Fatal("agent process was not reaped by Close")
	}
}

func TestStartTimesOutWithTheAgentsFailure(t *testing.T) {
	shortenStartTimeout(t, 500*time.Millisecond)
	var out bytes.Buffer
	tunnel, err := startFakeNgrok(context.Background(), t, "error", &out)
	if err == nil {
		t.Fatalf("start succeeded with URL %q, want a timeout", tunnel.URL())
	}
	if tunnel != nil {
		t.Fatalf("tunnel = %+v, want none on failure", tunnel)
	}
	if !strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), "ERR_NGROK_105") {
		t.Fatalf("error = %v, want a timeout carrying the agent's failure", err)
	}
	if !strings.Contains(out.String(), "ERR_NGROK_105") {
		t.Fatalf("fatal agent record was not shown to the user: %q", out.String())
	}
}

func TestStartTimesOutWithoutAgentOutput(t *testing.T) {
	shortenStartTimeout(t, 300*time.Millisecond)
	_, err := startFakeNgrok(context.Background(), t, "silent", nil)
	if err == nil || err.Error() != "wait for ngrok tunnel: timed out" {
		t.Fatalf("error = %v, want a bare timeout", err)
	}
}

func TestStartStopsWhenTheContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	started := time.Now()
	tunnel, err := startFakeNgrok(ctx, t, "silent", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if tunnel != nil {
		t.Fatalf("tunnel = %+v, want none after cancellation", tunnel)
	}
	if elapsed := time.Since(started); elapsed >= startTimeout {
		t.Fatalf("Start returned after %v, want it to stop on cancellation", elapsed)
	}
}

// A binary that resolves but cannot be executed reports the exec failure.
func TestStartReportsAMissingBinary(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "ngrok-missing")
	previous := lookPath
	lookPath = func(name string) (string, error) { return name, nil }
	t.Cleanup(func() { lookPath = previous })
	stubProcessSweep(t,
		func() ([]processEntry, error) { return nil, nil },
		func(int) error { return errors.New("no process may be terminated") })
	tunnel, err := Start(context.Background(), missing, "127.0.0.1:8080", nil)
	if err == nil {
		_ = tunnel.Close()
		t.Fatal("start succeeded with a missing binary")
	}
	if !strings.HasPrefix(err.Error(), "start ngrok:") {
		t.Fatalf("error = %v, want a start ngrok failure", err)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		t.Fatalf("error = %v, want an exec failure rather than an exit", err)
	}
}

func TestTunnelURL(t *testing.T) {
	if got := (&Tunnel{publicURL: fakeTunnelURL}).URL(); got != fakeTunnelURL {
		t.Fatalf("URL = %q, want %q", got, fakeTunnelURL)
	}
}

func TestWebhookURL(t *testing.T) {
	tests := []struct {
		name, publicURL, path, want, wantErr string
	}{
		{name: "invalid URL", publicURL: "://bad", path: "/hooks", wantErr: `invalid ngrok public URL "://bad"`},
		{name: "missing scheme", publicURL: "example.ngrok.app", path: "/hooks", wantErr: "invalid ngrok public URL"},
		{name: "missing host", publicURL: "https://", path: "/hooks", wantErr: "invalid ngrok public URL"},
		{name: "empty path", publicURL: "https://example.ngrok.app", path: "", want: "https://example.ngrok.app/webhook"},
		{name: "path without leading slash", publicURL: "https://example.ngrok.app/", path: "hooks/github", want: "https://example.ngrok.app/hooks/github"},
		{name: "trailing slash", publicURL: "https://example.ngrok.app//", path: "/hooks/", want: "https://example.ngrok.app/hooks"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := WebhookURL(test.publicURL, test.path)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("URL = %q, error = %v, want error containing %q", got, err, test.wantErr)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("URL = %q, error = %v, want %q", got, err, test.want)
			}
		})
	}
}

// Blank lines and Windows line endings are framing, not records.
func TestNgrokLogWatcherSkipsBlankLinesAndCarriageReturns(t *testing.T) {
	watcher := &ngrokLogWatcher{}
	_, _ = watcher.Write([]byte("\r\n   \n" + `{"lvl":"info","msg":"started tunnel","url":"` + fakeTunnelURL + `"}` + "\r\n"))
	if got := watcher.URL(); got != fakeTunnelURL {
		t.Fatalf("URL = %q, want %q", got, fakeTunnelURL)
	}
	if failure := watcher.Failure(); failure != "" {
		t.Fatalf("framing was taken for a failure: %q", failure)
	}
}
