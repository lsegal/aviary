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
	if dir == "" {
		t.Fatal("LogDir() returned empty string")
	}
	if !strings.HasSuffix(dir, "logs") {
		t.Fatalf("LogDir() should end with 'logs', got: %s", dir)
	}
}

func TestLogFilePath(t *testing.T) {
	setTestDataDir(t)
	p := LogFilePath()
	want := filepath.Join(LogDir(), "aviary.log")
	if p != want {
		t.Fatalf("LogFilePath() = %q; want %q", p, want)
	}
}

func TestLogDir_ContainsDataDir(t *testing.T) {
	setTestDataDir(t)
	dir := LogDir()
	dataDir := store.DataDir()
	if !strings.HasPrefix(dir, dataDir) {
		t.Fatalf("LogDir() %q should be under DataDir %q", dir, dataDir)
	}
}

func TestTeeHandler_Enabled(t *testing.T) {
	var bufA, bufB bytes.Buffer
	a := slog.NewTextHandler(&bufA, &slog.HandlerOptions{Level: slog.LevelWarn})
	b := slog.NewTextHandler(&bufB, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := &teeHandler{a: a, b: b}

	// Debug is enabled because b handles LevelDebug.
	if !h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected Enabled(Debug)=true when b allows debug")
	}
	// Warn is enabled by both.
	if !h.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("expected Enabled(Warn)=true")
	}
	// Error is enabled by both.
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("expected Enabled(Error)=true")
	}
}

func TestTeeHandler_Enabled_BothDisabled(t *testing.T) {
	var bufA, bufB bytes.Buffer
	// Both handlers only allow Error and above.
	a := slog.NewTextHandler(&bufA, &slog.HandlerOptions{Level: slog.LevelError})
	b := slog.NewTextHandler(&bufB, &slog.HandlerOptions{Level: slog.LevelError})
	h := &teeHandler{a: a, b: b}

	// Debug and Info should be disabled.
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected Enabled(Debug)=false when both handlers require Error+")
	}
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected Enabled(Info)=false when both handlers require Error+")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("expected Enabled(Error)=true")
	}
}

func TestTeeHandler_Handle_WritesToBoth(t *testing.T) {
	var bufA, bufB bytes.Buffer
	a := slog.NewTextHandler(&bufA, &slog.HandlerOptions{Level: slog.LevelDebug})
	b := slog.NewTextHandler(&bufB, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := &teeHandler{a: a, b: b}

	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "hello world", 0)
	if err := h.Handle(context.Background(), rec); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if !strings.Contains(bufA.String(), "hello world") {
		t.Errorf("handler A did not receive message, got: %q", bufA.String())
	}
	if !strings.Contains(bufB.String(), "hello world") {
		t.Errorf("handler B did not receive message, got: %q", bufB.String())
	}
}

func TestTeeHandler_Handle_ReturnsNil(t *testing.T) {
	var buf bytes.Buffer
	a := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	b := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := &teeHandler{a: a, b: b}

	rec := slog.NewRecord(time.Now(), slog.LevelError, "error message", 0)
	err := h.Handle(context.Background(), rec)
	if err != nil {
		t.Fatalf("Handle should return nil, got: %v", err)
	}
}

func TestTeeHandler_WithAttrs(t *testing.T) {
	var bufA, bufB bytes.Buffer
	a := slog.NewTextHandler(&bufA, &slog.HandlerOptions{Level: slog.LevelDebug})
	b := slog.NewTextHandler(&bufB, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := &teeHandler{a: a, b: b}

	h2 := h.WithAttrs([]slog.Attr{slog.String("key", "val")})
	if h2 == nil {
		t.Fatal("WithAttrs returned nil")
	}
	th2, ok := h2.(*teeHandler)
	if !ok {
		t.Fatalf("WithAttrs did not return *teeHandler, got %T", h2)
	}
	if th2.a == nil || th2.b == nil {
		t.Fatal("WithAttrs inner handlers are nil")
	}

	// Writing via th2 should include the attr in the output.
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "attributed", 0)
	_ = th2.Handle(context.Background(), rec)
	if !strings.Contains(bufA.String(), "val") {
		t.Errorf("expected attr value in output A, got: %q", bufA.String())
	}
	if !strings.Contains(bufB.String(), "val") {
		t.Errorf("expected attr value in output B, got: %q", bufB.String())
	}
}

func TestTeeHandler_WithGroup(t *testing.T) {
	var bufA, bufB bytes.Buffer
	a := slog.NewTextHandler(&bufA, &slog.HandlerOptions{Level: slog.LevelDebug})
	b := slog.NewTextHandler(&bufB, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := &teeHandler{a: a, b: b}

	h2 := h.WithGroup("mygroup")
	if h2 == nil {
		t.Fatal("WithGroup returned nil")
	}
	th2, ok := h2.(*teeHandler)
	if !ok {
		t.Fatalf("WithGroup did not return *teeHandler, got %T", h2)
	}
	if th2.a == nil || th2.b == nil {
		t.Fatal("WithGroup inner handlers are nil")
	}

	// After WithGroup, record with attrs should contain the group name in output.
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "grouped", 0)
	rec.AddAttrs(slog.String("field", "value"))
	_ = th2.Handle(context.Background(), rec)
	if !strings.Contains(bufA.String(), "mygroup") {
		t.Errorf("expected group name in output A, got: %q", bufA.String())
	}
	if !strings.Contains(bufB.String(), "mygroup") {
		t.Errorf("expected group name in output B, got: %q", bufB.String())
	}
}

func TestTeeHandler_WithAttrs_Empty(t *testing.T) {
	var buf bytes.Buffer
	a := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	b := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := &teeHandler{a: a, b: b}

	// Empty attrs should work without panic.
	h2 := h.WithAttrs([]slog.Attr{})
	if h2 == nil {
		t.Fatal("WithAttrs(empty) returned nil")
	}
}

func TestInit(t *testing.T) {
	// Use os.TempDir() directly (not t.TempDir()) so the directory persists
	// past test cleanup and avoids Windows file-lock issues with open log files.
	tmp, err := os.MkdirTemp("", "aviary-init-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	// Best-effort cleanup; may fail on Windows due to open file handle.
	defer os.RemoveAll(tmp) //nolint:errcheck

	store.SetDataDir(tmp)
	defer store.SetDataDir("")

	// Reset once so Init() actually runs.
	once = sync.Once{}

	err = Init()
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Log file should now exist.
	if _, serr := os.Stat(LogFilePath()); serr != nil {
		t.Errorf("expected log file to exist after Init(), got: %v", serr)
	}
}
