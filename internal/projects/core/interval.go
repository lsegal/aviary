package core

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// FormatInterval renders a poll interval for display. Go's own duration string
// spells whole hours as `1h0m0s`, and the web UI used to divide the same
// nanosecond value into whole seconds (`3600s`), so one run read differently
// depending on which status bar was open (issue #449). Zero components are
// dropped so short intervals stay as short as they read (`20s`, not `0h0m20s`),
// and the web dashboard's formatInterval in webui/src/dashboard.js mirrors this
// exactly; the two are covered by the same table of cases on each side.
func FormatInterval(d time.Duration) string {
	d = d.Round(time.Millisecond)
	if d <= 0 {
		return "0s"
	}
	if d < time.Second {
		return strconv.FormatInt(int64(d/time.Millisecond), 10) + "ms"
	}
	var text strings.Builder
	if hours := d / time.Hour; hours > 0 {
		fmt.Fprintf(&text, "%dh", hours)
		d -= hours * time.Hour
	}
	if minutes := d / time.Minute; minutes > 0 {
		fmt.Fprintf(&text, "%dm", minutes)
		d -= minutes * time.Minute
	}
	if d > 0 {
		text.WriteString(strconv.FormatFloat(d.Seconds(), 'f', -1, 64) + "s")
	}
	return text.String()
}
