package domain

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunFailure_ErrorIsShortMessage(t *testing.T) {
	failure := &RunFailure{
		Err:          errors.New("agent exited with status 1"),
		Output:       "line one\nline two",
		BugReportURL: "https://example.com/issues/new?body=%20long%20text",
	}

	// Error stays the one-line message; output and the bug report link are
	// kept out of it (#1122).
	assert.Equal(t, "agent exited with status 1", failure.Error())
}

func TestRunFailure_UnwrapReachesInnerError(t *testing.T) {
	inner := errors.New("inner")
	failure := &RunFailure{Err: inner}

	assert.Same(t, inner, failure.Unwrap())
	assert.ErrorIs(t, failure, inner)
	assert.ErrorIs(t, fmt.Errorf("run: %w", failure), inner)
}

func TestFailureDetail(t *testing.T) {
	inner := errors.New("agent exited with status 1")
	const output = "last line\nfinal line"
	const url = "https://example.com/issues/new"

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil", err: nil, want: ""},
		{name: "plain error", err: errors.New("plain failure"), want: "plain failure"},
		{
			name: "run failure without output or bug report",
			err:  &RunFailure{Err: inner},
			want: "agent exited with status 1",
		},
		{
			name: "run failure with output only",
			err:  &RunFailure{Err: inner, Output: output},
			want: "agent exited with status 1\n\nAgent output:\n" + output,
		},
		{
			name: "run failure with bug report only",
			err:  &RunFailure{Err: inner, BugReportURL: url},
			want: "agent exited with status 1\n\nReport bug: " + url,
		},
		{
			name: "run failure with output and bug report",
			err:  &RunFailure{Err: inner, Output: output, BugReportURL: url},
			want: "agent exited with status 1\n\nAgent output:\n" + output + "\n\nReport bug: " + url,
		},
		{
			// errors.As finds the wrapped RunFailure, and the message is the
			// wrapping error's, not just the inner one.
			name: "wrapped run failure",
			err:  fmt.Errorf("job 42: %w", &RunFailure{Err: inner, Output: output, BugReportURL: url}),
			want: "job 42: agent exited with status 1\n\nAgent output:\n" + output + "\n\nReport bug: " + url,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FailureDetail(tt.err))
		})
	}
}
