package scheduler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIsTransientScriptError(t *testing.T) {
	for text, want := range map[string]bool{
		"<string>:12: gh: API rate limit exceeded for user ID 1. (HTTP 403)":  true,
		"gh: You have exceeded a secondary rate limit (HTTP 403)":             true,
		"gh: Too Many Requests (HTTP 429)":                                    true,
		"gh: Bad Gateway (HTTP 502)":                                          true,
		`Get "https://api.github.com/x": dial tcp 1.2.3.4:443: i/o timeout`:   true,
		"error connecting to api.github.com":                                  true,
		"read tcp 10.0.0.1:5000->1.2.3.4:443: read: connection reset by peer": true,
		"<string>:52: failed to read main SHA: gh: Not Found (HTTP 404)":      false,
		"gh: Resource not accessible by integration (HTTP 403)":               false,
		"<string>:1: attempt to index a nil value":                            false,
	} {
		assert.Equal(t, want, IsTransientScriptError(errors.New(text)), text)
	}
	assert.False(t, IsTransientScriptError(nil))
	assert.False(t, IsTransientScriptError(context.Canceled))
}

func shortRetryDelays(t *testing.T) {
	t.Helper()
	prev := scriptRetryDelays
	t.Cleanup(func() { scriptRetryDelays = prev })
	scriptRetryDelays = []time.Duration{time.Millisecond, time.Millisecond}
}

func TestRetryTransientScriptBacksOffAndRetries(t *testing.T) {
	shortRetryDelays(t)
	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }

	calls := 0
	out, err := retryTransientScript(context.Background(), logf, func() (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("gh: API rate limit exceeded (HTTP 403)")
		}
		return "done", nil
	})
	assert.NoError(t, err)
	assert.Equal(t, "done", out)
	assert.Equal(t, 2, calls)
	assert.Len(t, logs, 1)
	assert.Contains(t, logs[0], "transient error (gh 403); retrying")

	// It gives up after its retries, returning the transient error.
	calls = 0
	_, err = retryTransientScript(context.Background(), logf, func() (string, error) {
		calls++
		return "", errors.New("dial tcp: no such host")
	})
	assert.Error(t, err)
	assert.Equal(t, 1+len(scriptRetryDelays), calls)

	// A script error is not retried.
	calls = 0
	_, err = retryTransientScript(context.Background(), logf, func() (string, error) {
		calls++
		return "", errors.New("gh: Not Found (HTTP 404)")
	})
	assert.Error(t, err)
	assert.Equal(t, 1, calls)
}

func TestRetryTransientScriptStopsWhenTheRunStops(t *testing.T) {
	prev := scriptRetryDelays
	t.Cleanup(func() { scriptRetryDelays = prev })
	scriptRetryDelays = []time.Duration{time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = retryTransientScript(ctx, func(string, ...any) {
			// no-op: this test does not inspect retry log output
		}, func() (string, error) {
			calls++
			return "", errors.New("gh: rate limit (HTTP 429)")
		})
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the retry kept waiting after the run stopped")
	}
	assert.Equal(t, 1, calls)
}
