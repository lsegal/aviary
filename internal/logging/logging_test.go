package logging

import (
	"bytes"
	"context"
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
	dir := LogDir()
	assert.NotEqual(t, "", dir)
	assert.True(t, strings.HasSuffix(dir, "logs"))

}

func TestLogFilePath(t *testing.T) {
	setTestDataDir(t)
	p := LogFilePath()
	want := filepath.Join(LogDir(), "aviary.log")
	assert.Equal(t, want, p)

}

func TestLogDir_ContainsDataDir(t *testing.T) {
	setTestDataDir(t)
	dir := LogDir()
	dataDir := store.DataDir()
	assert.True(t, strings.HasPrefix(dir, dataDir))

}

func TestTeeHandler_Enabled(t *testing.T) {
	var bufA, bufB bytes.Buffer
	a := slog.NewTextHandler(&bufA, &slog.HandlerOptions{Level: slog.LevelWarn})
	b := slog.NewTextHandler(&bufB, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := &teeHandler{a: a, b: b}
	assert.

		// Debug is enabled because b handles LevelDebug.
		True(t, h.Enabled(context.Background(), slog.LevelDebug))
	assert.True(t, // Warn is enabled by both.
		h.Enabled(context.Background(), slog.LevelWarn))
	assert.True(t, // Error is enabled by both.
		h.Enabled(context.Background(), slog.LevelError))

}

func TestTeeHandler_Enabled_BothDisabled(t *testing.T) {
	var bufA, bufB bytes.Buffer
	// Both handlers only allow Error and above.
	a := slog.NewTextHandler(&bufA, &slog.HandlerOptions{Level: slog.LevelError})
	b := slog.NewTextHandler(&bufB, &slog.HandlerOptions{Level: slog.LevelError})
	h := &teeHandler{a: a, b: b}
	assert.

		// Debug and Info should be disabled.
		False(t, h.Enabled(context.Background(), slog.LevelDebug))
	assert.False(t, h.Enabled(context.Background(), slog.LevelInfo))
	assert.True(t, h.Enabled(context.Background(), slog.LevelError))

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

func TestTeeHandler_Handle_ReturnsNil(t *testing.T) {
	var buf bytes.Buffer
	a := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	b := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := &teeHandler{a: a, b: b}

	rec := slog.NewRecord(time.Now(), slog.LevelError, "error message", 0)
	err := h.Handle(context.Background(), rec)
	assert.NoError(t, err)

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
