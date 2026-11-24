package domain

import (
	"errors"
	"strings"
)

// RunFailure is a failed agent run's error together with what the run's page
// shows beside it. Error is only the short message: the agent's output and
// the prefilled bug report are kept apart, so a failure reads as one line
// instead of a wall of percent-encoded URL (#1122).
type RunFailure struct {
	Err error
	// Output is the last lines the agent printed before it failed.
	Output string
	// BugReportURL opens a prefilled issue reporting the failure.
	BugReportURL string
}

func (f *RunFailure) Error() string { return f.Err.Error() }

func (f *RunFailure) Unwrap() error { return f.Err }

// FailureDetail renders err for a log: its message, then the output and bug
// report link a RunFailure carries, each on lines of their own.
func FailureDetail(err error) string {
	if err == nil {
		return ""
	}
	var failure *RunFailure
	if !errors.As(err, &failure) {
		return err.Error()
	}
	var b strings.Builder
	b.WriteString(err.Error())
	if failure.Output != "" {
		b.WriteString("\n\nAgent output:\n" + failure.Output)
	}
	if failure.BugReportURL != "" {
		b.WriteString("\n\nReport bug: " + failure.BugReportURL)
	}
	return b.String()
}
