package cliagent

import (
	"context"
	"os"
)

// discardEvent is the event callback for a caller that passed none: the turn
// still runs, and nobody is listening for its events.
func discardEvent(Event) {
	// Intentionally empty: there is no listener to deliver the event to.
}

// noCleanup is the cleanup returned when nothing was written that would need
// removing.
func noCleanup() {
	// Intentionally empty: nothing was written, so nothing needs removing.
}

// writeTempImages stores each of a turn's images in a temp file, returning
// their paths and a cleanup that removes them. On error the files already
// written are removed.
func writeTempImages(ctx context.Context, mediaURLs []string) ([]string, func(), error) {
	var images []string
	cleanup := func() {
		for _, path := range images {
			_ = os.Remove(path)
		}
	}
	for _, mediaURL := range mediaURLs {
		path, err := writeTempImage(ctx, mediaURL)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		images = append(images, path)
	}
	return images, cleanup, nil
}
