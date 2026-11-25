package logging

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/store"

	"github.com/stretchr/testify/assert"
)

func setTestDataDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	store.SetDataDir(tmp)
	t.Cleanup(func() { store.SetDataDir("") })
}

func TestLogDir(t *testing.T) {
	setTestDataDir(t)
	assert.Equal(t, filepath.Join(store.DataDir(), "logs"), LogDir())
}

func TestLogFilePath(t *testing.T) {
	setTestDataDir(t)
	p := LogFilePath()
	want := filepath.Join(LogDir(), "aviary.log")
	assert.Equal(t, want, p)

}

func TestTeeHandler_Enabled(t *testing.T) {
	tests := []struct {
		name           string
		levelA, levelB slog.Level
		query          slog.Level
		want           bool
	}{
		{"debug enabled by b", slog.LevelWarn, slog.LevelDebug, slog.LevelDebug, true},
		{"warn enabled by both", slog.LevelWarn, slog.LevelDebug, slog.LevelWarn, true},
		{"error enabled by both", slog.LevelWarn, slog.LevelDebug, slog.LevelError, true},
		{"debug disabled by both", slog.LevelError, slog.LevelError, slog.LevelDebug, false},
		{"info disabled by both", slog.LevelError, slog.LevelError, slog.LevelInfo, false},
		{"error at both thresholds", slog.LevelError, slog.LevelError, slog.LevelError, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: tt.levelA})
			b := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: tt.levelB})
			h := &teeHandler{a: a, b: b}
			assert.Equal(t, tt.want, h.Enabled(context.Background(), tt.query))
		})
	}
}

func TestTeeHandler_Handle_WritesToBoth(t *testing.T) {
	var bufA, bufB bytes.Buffer
	a := slog.NewTextHandler(&bufA, &slog.HandlerOptions{Level: slog.LevelDebug})
	b := slog.NewTextHandler(&bufB, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := &teeHandler{a: a, b: b}

	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "hello world", 0)
	err := h.Handle(context.Background(), rec)
	assert.NoError(t, err)

	assert.True(t, strings.Contains(bufA.String(), "hello world"))
	assert.True(t, strings.Contains(bufB.String(), "hello world"))

}

func TestTeeHandler_WithAttrs(t *testing.T) {
	var bufA, bufB bytes.Buffer
	a := slog.NewTextHandler(&bufA, &slog.HandlerOptions{Level: slog.LevelDebug})
	b := slog.NewTextHandler(&bufB, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := &teeHandler{a: a, b: b}

	h2 := h.WithAttrs([]slog.Attr{slog.String("key", "val")})
	assert.NotNil(t, h2)

	th2, ok := h2.(*teeHandler)
	assert.True(t, ok)
	assert.NotNil(t, th2.a)
	assert.NotNil(t, th2.b)

	// Writing via th2 should include the attr in the output.
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "attributed", 0)
	_ = th2.Handle(context.Background(), rec)
	assert.True(t, strings.Contains(bufA.String(), "val"))
	assert.True(t, strings.Contains(bufB.String(), "val"))

}

func TestTeeHandler_WithGroup(t *testing.T) {
	var bufA, bufB bytes.Buffer
	a := slog.NewTextHandler(&bufA, &slog.HandlerOptions{Level: slog.LevelDebug})
	b := slog.NewTextHandler(&bufB, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := &teeHandler{a: a, b: b}

	h2 := h.WithGroup("mygroup")
	assert.NotNil(t, h2)

	th2, ok := h2.(*teeHandler)
	assert.True(t, ok)
	assert.NotNil(t, th2.a)
	assert.NotNil(t, th2.b)

	// After WithGroup, record with attrs should contain the group name in output.
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "grouped", 0)
	rec.AddAttrs(slog.String("field", "value"))
	_ = th2.Handle(context.Background(), rec)
	assert.True(t, strings.Contains(bufA.String(), "mygroup"))
	assert.True(t, strings.Contains(bufB.String(), "mygroup"))

}

func TestTeeHandler_WithAttrs_Empty(t *testing.T) {
	var buf bytes.Buffer
	a := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	b := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := &teeHandler{a: a, b: b}

	// Empty attrs should work without panic.
	h2 := h.WithAttrs([]slog.Attr{})
	assert.NotNil(t, h2)

}

func TestInit(t *testing.T) {
	tmp := t.TempDir()
	store.SetDataDir(tmp)
	defer store.SetDataDir("")

	// Reset once so Init() actually runs.
	once = sync.Once{}

	err := Init()
	assert.NoError(t, err)

	// Log file should now exist.
	_, serr := os.Stat(LogFilePath())
	assert.Nil(t, serr)

	// Close logging so the test temp dir can be cleaned up on Windows.
	Shutdown()

}

// captureStderr runs fn with os.Stderr redirected to a pipe and returns
// everything written to it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	assert.NoError(t, err)
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	fn()

	assert.NoError(t, w.Close())
	os.Stderr = origStderr
	out, err := io.ReadAll(r)
	assert.NoError(t, err)
	assert.NoError(t, r.Close())
	return string(out)
}

func TestInit_DoesNotWriteToStderr(t *testing.T) {
	setTestDataDir(t)
	Shutdown()
	t.Cleanup(Shutdown)

	out := captureStderr(t, func() {
		assert.NoError(t, Init())
		slog.Info("file only")
	})
	assert.Empty(t, out)
}

func TestEnableConsole_WritesToStderr(t *testing.T) {
	setTestDataDir(t)
	Shutdown()
	t.Cleanup(Shutdown)

	out := captureStderr(t, func() {
		assert.NoError(t, Init())
		EnableConsole()
		slog.Info("console enabled")
	})
	assert.Contains(t, out, "console enabled")
}

func TestInitMemory_KeepsLogsOutOfTheDataDir(t *testing.T) {
	setTestDataDir(t)
	Shutdown()
	t.Cleanup(Shutdown)
	assert.NoError(t, InitMemory())
	slog.Info("kept in memory", "component", "test")

	data, end, ok := MemoryLog()
	assert.True(t, ok)
	assert.Contains(t, string(data), `"msg":"kept in memory"`)
	assert.Equal(t, int64(len(data)), end)
	_, err := os.Stat(LogDir())
	assert.True(t, os.IsNotExist(err), "InitMemory created %s", LogDir())

	// A later Init leaves logs in memory.
	assert.NoError(t, Init())
	_, _, ok = MemoryLog()
	assert.True(t, ok)

	Shutdown()
	_, _, ok = MemoryLog()
	assert.False(t, ok)
}

func TestMemoryLog_KeepsTheLatestWholeLines(t *testing.T) {
	m := newMemoryLog(10)
	_, _ = m.Write([]byte("aaaa\nbbbb\n"))
	_, _ = m.Write([]byte("cccc\n"))
	data, end := m.snapshot()
	assert.Equal(t, "bbbb\ncccc\n", string(data))
	assert.Equal(t, int64(15), end)
}
