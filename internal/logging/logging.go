// Package logging provides structured logging utilities for Aviary.
package logging

import (
	"context"
	"io"
	stdlog "log"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/lsegal/aviary/internal/store"
)

var (
	once    sync.Once
	initErr error
)

// LogDir returns Aviary's log directory.
func LogDir() string {
	return filepath.Join(store.DataDir(), "logs")
}

// LogFilePath returns the primary structured log file.
func LogFilePath() string {
	return filepath.Join(LogDir(), "aviary.log")
}

// Init configures global slog + stdlib log output so all processes write to the
// same filesystem log file.
func Init() error {
	once.Do(func() {
		if err := os.MkdirAll(LogDir(), 0o700); err != nil {
			initErr = err
			return
		}

		f, err := os.OpenFile(LogFilePath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			initErr = err
			return
		}

		stderrHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
		fileHandler := slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})
		slog.SetDefault(slog.New(&teeHandler{a: stderrHandler, b: fileHandler}))

		stdlog.SetFlags(stdlog.LstdFlags | stdlog.Lmicroseconds)
		stdlog.SetOutput(io.MultiWriter(os.Stderr, f))
	})

	return initErr
}

type teeHandler struct {
	a slog.Handler
	b slog.Handler
}

func (t *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return t.a.Enabled(ctx, level) || t.b.Enabled(ctx, level)
}

func (t *teeHandler) Handle(ctx context.Context, rec slog.Record) error {
	_ = t.a.Handle(ctx, rec)
	_ = t.b.Handle(ctx, rec)
	return nil
}

func (t *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &teeHandler{a: t.a.WithAttrs(attrs), b: t.b.WithAttrs(attrs)}
}

func (t *teeHandler) WithGroup(name string) slog.Handler {
	return &teeHandler{a: t.a.WithGroup(name), b: t.b.WithGroup(name)}
}
