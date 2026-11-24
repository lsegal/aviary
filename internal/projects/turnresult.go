package projects

import (
	"bytes"
	"sync"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/transcript"
)

// turnResultDetector watches an agent's raw stream for the final result event
// its definition ends a turn with. A run that reached a successful one finished
// its work, so the process cleanup timing out after it is not a failure
// (#1122).
type turnResultDetector struct {
	*lineWriter
	output  clidefs.Output
	mu      sync.Mutex
	outcome transcript.TurnOutcome
}

func newTurnResultDetector(output clidefs.Output) *turnResultDetector {
	d := &turnResultDetector{lineWriter: &lineWriter{}, output: output}
	d.line = d.scan
	return d
}

func (d *turnResultDetector) scan(line []byte) error {
	if outcome := d.output.TurnResult(bytes.TrimSpace(line)); outcome != transcript.TurnUnfinished {
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
