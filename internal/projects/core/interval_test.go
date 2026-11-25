package core

import (
	"testing"
	"time"
)

func TestFormatInterval(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{name: "zero", d: 0, want: "0s"},
		{name: "negative", d: -5 * time.Second, want: "0s"},
		{name: "rounds below half a millisecond to zero", d: 400 * time.Microsecond, want: "0s"},
		{name: "rounds half a millisecond up", d: 500 * time.Microsecond, want: "1ms"},
		{name: "sub-second milliseconds", d: 250 * time.Millisecond, want: "250ms"},
		{name: "rounds sub-millisecond remainder", d: 1500*time.Microsecond + 400*time.Nanosecond, want: "2ms"},
		{name: "rounds up to a whole second", d: 999*time.Millisecond + 600*time.Microsecond, want: "1s"},
		{name: "seconds", d: 20 * time.Second, want: "20s"},
		{name: "fractional seconds", d: 2500 * time.Millisecond, want: "2.5s"},
		{name: "whole minute", d: time.Minute, want: "1m"},
		{name: "minutes and fractional seconds", d: time.Minute + 1500*time.Millisecond, want: "1m1.5s"},
		{name: "whole hour", d: time.Hour, want: "1h"},
		{name: "hours and minutes", d: 90 * time.Minute, want: "1h30m"},
		{name: "hours and seconds without minutes", d: time.Hour + 5*time.Second, want: "1h5s"},
		{name: "all components", d: 2*time.Hour + 3*time.Minute + 4*time.Second, want: "2h3m4s"},
		{name: "hours beyond a day", d: 25 * time.Hour, want: "25h"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatInterval(tt.d); got != tt.want {
				t.Fatalf("FormatInterval(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}
