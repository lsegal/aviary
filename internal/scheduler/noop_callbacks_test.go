package scheduler

import "github.com/lsegal/aviary/internal/agent"

// discardStreamEvent is a stream consumer for tests that only assert on how a
// watched run ends, not on the events it produced.
func discardStreamEvent(agent.StreamEvent) {
	// Intentionally empty: the test does not inspect streamed events.
}

// discardLogf is a RunCompiled log callback for tests that do not check the
// run's progress messages.
func discardLogf(string, ...any) {
	// Intentionally empty: progress logging is irrelevant to the test.
}
