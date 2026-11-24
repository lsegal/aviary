package projects

import (
	"bytes"
	"sync"

	"github.com/lsegal/aviary/internal/transcript"
)

// turnResultDetector watches an agent's raw stream for the final result event
// its format ends a turn with. A run that reached a successful one finished
// its work, so the process cleanup timing out after it is not a failure
// (#1122).
type turnResultDetector struct {
	*lineWriter
	format  string
	mu      sync.Mutex
	outcome transcript.TurnOutcome
}

func newTurnResultDetector(format string) *turnResultDetector {
	d := &turnResultDetector{lineWriter: &lineWriter{}, format: format}
	d.line = d.scan
	return d
}

func (d *turnResultDetector) scan(line []byte) error {
	if outcome := transcript.TurnResult(d.format, bytes.TrimSpace(line)); outcome != transcript.TurnUnfinished {
		d.mu.Lock()
		d.outcome = outcome
		d.mu.Unlock()
	}
	return nil
}

// succeeded reports whether the last final result event the stream carried
// was a success.
func (d *turnResultDetector) succeeded() bool {
	if d == nil {
		return false
	}
	_ = d.Flush()
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.outcome == transcript.TurnSucceeded
}
