package scheduler

import (
	"context"
	"errors"
	"net"
	"regexp"
	"strings"
	"time"
)

// transientScriptErrorPattern matches what a compiled script's error says
// when it failed for a reason outside the script (#1568): a rate limit, or
// a network or server error. gh reports a rate limit as an HTTP 403 or 429
// that says "rate limit", and a GitHub outage as a 5xx.
var transientScriptErrorPattern = regexp.MustCompile(`(?i)` + strings.Join([]string{
	`rate.?limit`,
	`abuse detection`,
	`too many requests`,
	`\(HTTP 429\)`,
	`\(HTTP 50[234]\)`,
	`dial tcp`,
	`connection refused`,
	`connection reset`,
	`broken pipe`,
	`i/o timeout`,
	`tls handshake timeout`,
	`no such host`,
	`could not resolve host`,
	`network is unreachable`,
	`error connecting to`,
	`unexpected eof`,
}, "|"))

// IsTransientScriptError reports whether a compiled script's error was
// transient, such as a rate limit or a network error, so it says nothing
// about whether the script works: the run retries it after a backoff, and
// it never counts toward the script's failures.
func IsTransientScriptError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return transientScriptErrorPattern.MatchString(err.Error())
}

// scriptRetryDelays are the backoffs before each retry of a compiled script
// that failed with a transient error. Tests shorten them.
var scriptRetryDelays = []time.Duration{15 * time.Second, 45 * time.Second}

// retryTransientScript runs a compiled script with run, and runs it again
// after each of scriptRetryDelays while it fails with a transient error. It
// returns the last attempt's result, or ctx's error when ctx ends while it
// waits.
func retryTransientScript(ctx context.Context, logf func(string, ...any), run func() (string, error)) (string, error) {
	output, err := run()
	for _, delay := range scriptRetryDelays {
		if !IsTransientScriptError(err) || ctx.Err() != nil {
			break
		}
		logf("precompiled script hit a transient error (%s); retrying in %s", ProjectScriptFailureCause(err), delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return output, err
		case <-timer.C:
		}
		output, err = run()
	}
	return output, err
}
