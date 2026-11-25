package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/logging"
	"github.com/lsegal/aviary/internal/store"
)

// syncBuffer is a bytes.Buffer safe for concurrent writes and reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func useLogsTestDataDir(t *testing.T) string {
	t.Helper()
	prev := store.DataDir()
	dir := t.TempDir()
	store.SetDataDir(dir)
	t.Cleanup(func() { store.SetDataDir(prev) })
	return dir
}

func useFastLogsFollow(t *testing.T) {
	t.Helper()
	prev := logsFollowInterval
	logsFollowInterval = 10 * time.Millisecond
	t.Cleanup(func() { logsFollowInterval = prev })
}

func setLogsFlags(t *testing.T, follow bool, lines int) {
	t.Helper()
	prevFollow, prevLines := logsFollow, logsLines
	logsFollow, logsLines = follow, lines
	t.Cleanup(func() { logsFollow, logsLines = prevFollow, prevLines })
}

func newLogsTestCmd(ctx context.Context, out *syncBuffer) *cobra.Command {
	c := &cobra.Command{}
	c.SetContext(ctx)
	c.SetOut(out)
	return c
}

func appendFile(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()
		t.Fatalf("write %s: %v", path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
}

func waitForOutput(t *testing.T, out *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if out.String() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("output = %q, want %q", out.String(), want)
}

func TestPrintTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aviary.log")
	if err := os.WriteFile(path, []byte("one\n  two  \n\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		lines int
		want  string
	}{
		{name: "more lines than file", lines: 50, want: "one\ntwo\nthree\nfour\n"},
		{name: "fewer lines than file", lines: 3, want: "three\nfour\n"},
		{name: "zero lines", lines: 0, want: ""},
		{name: "negative lines", lines: -1, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := printTail(&out, path, tt.lines); err != nil {
				t.Fatalf("printTail: %v", err)
			}
			if got := out.String(); got != tt.want {
				t.Fatalf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrintTailMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.log")
	var out bytes.Buffer
	if err := printTail(&out, path, 10); !os.IsNotExist(err) {
		t.Fatalf("printTail error = %v, want not-exist", err)
	}
	if out.Len() != 0 {
		t.Fatalf("output = %q, want empty", out.String())
	}
	if err := printTail(&out, path, 0); err != nil {
		t.Fatalf("printTail with zero lines should not read the file: %v", err)
	}
}

func TestFollowFile(t *testing.T) {
	useFastLogsFollow(t)
	path := filepath.Join(t.TempDir(), "aviary.log")
	if err := os.WriteFile(path, []byte("already here\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() { done <- followFile(newLogsTestCmd(ctx, out), path) }()

	// followFile records the starting offset before its first poll.
	time.Sleep(100 * time.Millisecond)

	// Existing content is skipped; complete lines appear as they are appended.
	appendFile(t, path, "first\n\n  second  \n")
	waitForOutput(t, out, "first\nsecond\n")

	// A partial line is held until its newline arrives.
	appendFile(t, path, "par")
	time.Sleep(50 * time.Millisecond)
	if got := out.String(); got != "first\nsecond\n" {
		t.Fatalf("partial line printed early: %q", got)
	}
	appendFile(t, path, "tial\n")
	waitForOutput(t, out, "first\nsecond\npartial\n")

	// Truncation restarts from the beginning of the file and drops any
	// buffered partial line.
	appendFile(t, path, "dangling")
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(path, []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForOutput(t, out, "first\nsecond\npartial\nnew\n")

	// A temporarily missing file (rotation) is retried until it reappears;
	// a replacement shorter than the old offset is read from the start.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(path, []byte("r2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForOutput(t, out, "first\nsecond\npartial\nnew\nr2\n")

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("followFile returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("followFile did not return after cancellation")
	}
}

func TestFollowFileMissingAtStart(t *testing.T) {
	useFastLogsFollow(t)
	path := filepath.Join(t.TempDir(), "aviary.log")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() { done <- followFile(newLogsTestCmd(ctx, out), path) }()

	time.Sleep(100 * time.Millisecond)
	appendFile(t, path, "hello\n")
	waitForOutput(t, out, "hello\n")

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("followFile returned %v, want nil", err)
	}
}

func TestLogsCmdPrintsTailAndCreatesLogFile(t *testing.T) {
	useLogsTestDataDir(t)
	setLogsFlags(t, false, 2)

	out := &syncBuffer{}
	if err := logsCmd.RunE(newLogsTestCmd(context.Background(), out), nil); err != nil {
		t.Fatalf("logs: %v", err)
	}
	if out.String() != "" {
		t.Fatalf("output = %q, want empty for a new log file", out.String())
	}
	info, err := os.Stat(logging.LogFilePath())
	if err != nil {
		t.Fatalf("log file not created: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("new log file size = %d, want 0", info.Size())
	}

	appendFile(t, logging.LogFilePath(), "a\nb\nc\n")
	out = &syncBuffer{}
	if err := logsCmd.RunE(newLogsTestCmd(context.Background(), out), nil); err != nil {
		t.Fatalf("logs: %v", err)
	}
	if got, want := out.String(), "c\n"; got != want {
		// Trailing newline makes the final split element empty, so -n 2
		// yields the last line only.
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestLogsCmdFollowsUntilCancelled(t *testing.T) {
	useLogsTestDataDir(t)
	useFastLogsFollow(t)
	setLogsFlags(t, true, 10)
	if err := os.MkdirAll(logging.LogDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	appendFile(t, logging.LogFilePath(), "old\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() { done <- logsCmd.RunE(newLogsTestCmd(ctx, out), nil) }()

	waitForOutput(t, out, "old\n")
	time.Sleep(100 * time.Millisecond)
	appendFile(t, logging.LogFilePath(), "fresh\n")
	waitForOutput(t, out, "old\nfresh\n")

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("logs returned %v, want nil", err)
	}
}

func TestLogsCmdLogDirError(t *testing.T) {
	dir := useLogsTestDataDir(t)
	setLogsFlags(t, false, 10)
	// A regular file where the logs directory should be makes MkdirAll fail.
	if err := os.WriteFile(filepath.Join(dir, "logs"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := &syncBuffer{}
	if err := logsCmd.RunE(newLogsTestCmd(context.Background(), out), nil); err == nil {
		t.Fatal("logs succeeded, want MkdirAll error")
	}
	if out.String() != "" {
		t.Fatalf("output = %q, want empty", out.String())
	}
}

func TestLogsCmdUnreadableLogFile(t *testing.T) {
	useLogsTestDataDir(t)
	setLogsFlags(t, true, 10)
	// A directory at the log file path exists but cannot be read as a file.
	if err := os.MkdirAll(logging.LogFilePath(), 0o700); err != nil {
		t.Fatal(err)
	}

	out := &syncBuffer{}
	if err := logsCmd.RunE(newLogsTestCmd(context.Background(), out), nil); err == nil {
		t.Fatal("logs succeeded, want read error")
	}
	if out.String() != "" {
		t.Fatalf("output = %q, want empty", out.String())
	}
}
