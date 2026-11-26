package projects

// ignoreSessionUpdate is a RunSession update callback for tests that assert on
// the run's result or side effects, not on intermediate session snapshots.
func ignoreSessionUpdate(AgentSession) {
	// Intentionally empty: the test does not track session updates.
}
