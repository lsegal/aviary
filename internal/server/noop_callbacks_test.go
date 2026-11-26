package server

import (
	"context"

	"github.com/lsegal/aviary/internal/channels"
)

// ignoreChannelMessage is a msgFn for tests that only need channel runtime to
// be wired up; incoming messages are never routed to an agent.
func ignoreChannelMessage(string, string, string, channels.Channel, channels.IncomingMessage) {
	// Intentionally empty: the test does not deliver channel messages.
}

// skipShippedSkills stands in for skill install/sync during test boots so
// nothing is written into the CLIs' skill directories.
func skipShippedSkills(context.Context) {
	// Intentionally empty: tests must not touch real skill directories.
}
