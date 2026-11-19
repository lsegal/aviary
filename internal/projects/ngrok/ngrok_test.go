package ngrok

import (
	"bytes"
	"strings"
	"testing"
)

func TestNgrokArgsKeepTerminalDashboardDisabled(t *testing.T) {
	got := ngrokArgs("127.0.0.1:8080")
	want := []string{"http", "--log=stdout", "--log-format=json", "--log-level=info", "127.0.0.1:8080"}
	if len(got) != len(want) {
		t.Fatalf("arguments = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argument %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNgrokLogWatcherReadsTunnelURLFromOwnProcess(t *testing.T) {
	var out bytes.Buffer
	watcher := &ngrokLogWatcher{out: &out}
	// A second agent on the machine pushes this process's web API off 4040,
	// which is exactly the case that used to make glorp adopt the other
	// agent's tunnel (issue #361). The URL still comes from this log.
	watcher.Write([]byte(`{"lvl":"warn","msg":"cannot bind default web address, trying alternatives","addr":"127.0.0.1:4040"}` + "\n"))
	watcher.Write([]byte(`{"lvl":"info","msg":"starting web service","addr":"127.0.0.1:4041"}` + "\n"))
	if watcher.URL() != "" {
		t.Fatalf("URL = %q before the tunnel started", watcher.URL())
	}
	watcher.Write([]byte(`{"lvl":"info","msg":"started tunnel","url":"https://mine.ngrok-free.app/"`))
	watcher.Write([]byte(`,"addr":"http://localhost:8080"}` + "\n"))
	if got := watcher.URL(); got != "https://mine.ngrok-free.app" {
		t.Fatalf("URL = %q, want https://mine.ngrok-free.app", got)
	}
	watcher.Write([]byte(`{"lvl":"info","msg":"started tunnel","url":"https://other.ngrok-free.app"}` + "\n"))
	if got := watcher.URL(); got != "https://mine.ngrok-free.app" {
		t.Fatalf("URL = %q after a later tunnel record, want the first one", got)
	}
	if out.Len() != 0 {
		t.Fatalf("routine ngrok output was forwarded: %q", out.String())
	}
}

func TestNgrokLogWatcherReportsFailures(t *testing.T) {
	var out bytes.Buffer
	watcher := &ngrokLogWatcher{out: &out}
	// A recoverable error is remembered for diagnostics but kept off the UI,
	// which only ever showed critical output.
	watcher.Write([]byte(`{"lvl":"eror","msg":"unable to check host info","err":"exec: \"ioreg\" not found"}` + "\n"))
	if out.Len() != 0 {
		t.Fatalf("recoverable ngrok error was forwarded: %q", out.String())
	}
	watcher.Write([]byte(`{"lvl":"crit","msg":"command failed","err":"authentication failed\nERR_NGROK_108"}` + "\n"))
	watcher.Write([]byte("ngrok exploded\n"))
	if !strings.Contains(watcher.Failure(), "unable to check host info") {
		t.Fatalf("recoverable error was not captured: %q", watcher.Failure())
	}
	failure := watcher.Failure()
	if !strings.Contains(failure, "ERR_NGROK_108") || !strings.Contains(failure, "ngrok exploded") {
		t.Fatalf("failure = %q", failure)
	}
	if !strings.Contains(out.String(), "ERR_NGROK_108") {
		t.Fatalf("failure was not forwarded to the user: %q", out.String())
	}
	if err := ngrokStartError(failure); !strings.Contains(err.Error(), "ERR_NGROK_108") {
		t.Fatalf("start error = %v", err)
	}
	if err := ngrokStartError(""); err.Error() != "wait for ngrok tunnel: timed out" {
		t.Fatalf("start error without ngrok output = %v", err)
	}
}

func TestWebhookURLUsesNgrokHost(t *testing.T) {
	got, err := WebhookURL("https://example.ngrok.app/", "hooks")
	if err != nil || got != "https://example.ngrok.app/hooks" {
		t.Fatalf("URL = %q, error = %v", got, err)
	}
}

func TestNgrokURLIdentifiesStaleTunnel(t *testing.T) {
	if !IsURL("https://old.ngrok-free.app/webhook") {
		t.Fatal("ngrok URL was not identified")
	}
	if IsURL("https://hooks.example.com/webhook") {
		t.Fatal("unrelated webhook URL was identified as ngrok")
	}
}
