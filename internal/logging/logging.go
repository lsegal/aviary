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
	"time"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/lsegal/aviary/internal/store"
)

var (
	once       sync.Once
	initErr    error
	rotLogger  *lumberjack.Logger
	mu         sync.Mutex
	stopRotate chan struct{}
)

// LogDir returns Aviary's log directory.
func LogDir() string {
	return filepath.Join(store.DataDir(), "logs")
}

// LogFilePath returns the primary structured log file.
func LogFilePath() string {
	return filepath.Join(LogDir(), "aviary.log")
}

// Init configures global slog + stdlib log output to write to Aviary's log
// file. Console logging is disabled by default and can be enabled explicitly
// for long-running server processes.
func Init() error {
	once.Do(func() {
		if err := os.MkdirAll(LogDir(), 0o700); err != nil {
			initErr = err
			return
		}

		// Ensure the file exists immediately (lumberjack creates it lazily).
		f, err := os.OpenFile(LogFilePath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			initErr = err
			return
		}
		_ = f.Close()

		rotLogger = &lumberjack.Logger{
			Filename: LogFilePath(),
			MaxSize:  50, // megabytes
			Compress: true,
		}

		stop := make(chan struct{})
		stopRotate = stop
		go dailyRotate(rotLogger, stop)

		configureLocked(false)
	})

	return initErr
}

// dailyRotate forces a log rotation at midnight each day.
func dailyRotate(l *lumberjack.Logger, stop <-chan struct{}) {
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		timer := time.NewTimer(time.Until(next))
		select {
		case <-stop:
			timer.Stop()
			return
		case <-timer.C:
			_ = l.Rotate()
		}
	}
}

// EnableConsole mirrors slog + stdlib logs to stderr in addition to the log
// file. This is intended for the foreground server command, not short-lived
// CLI commands.
func EnableConsole() {
	mu.Lock()
	defer mu.Unlock()
	configureLocked(true)
}

// Shutdown closes any open log file and resets logging state. This is intended
// for use in tests so temporary log files can be removed.
func Shutdown() {
	mu.Lock()
	defer mu.Unlock()

	if stopRotate != nil {
		close(stopRotate)
		stopRotate = nil
	}

	if rotLogger != nil {
		_ = rotLogger.Close()
		rotLogger = nil
	}

	stdlog.SetOutput(os.Stderr)
	stderrHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(stderrHandler))

	once = sync.Once{}
	initErr = nil
}

func configureLocked(console bool) {
	if rotLogger == nil {
		return
	}

	fileHandler := slog.NewJSONHandler(rotLogger, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := slog.Handler(fileHandler)
	output := io.Writer(rotLogger)
	if console {
		stderrHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
		handler = &teeHandler{a: stderrHandler, b: fileHandler}
		output = io.MultiWriter(os.Stderr, rotLogger)
	}

	slog.SetDefault(slog.New(handler))
	stdlog.SetFlags(stdlog.LstdFlags | stdlog.Lmicroseconds)
	stdlog.SetOutput(output)
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
