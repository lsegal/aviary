package channels

import "sync"

const logSinkCap = 500

// LogSink is a thread-safe ring buffer of raw log lines that fans out to
// live subscribers. It is used to capture managed daemon stdout/stderr plus
// channel runtime logs and stream them to the web UI.
type LogSink struct {
	mu   sync.Mutex
	ring []string
	subs map[chan string]struct{}
}

func newLogSink() *LogSink {
	return &LogSink{
		ring: make([]string, 0, logSinkCap),
		subs: make(map[chan string]struct{}),
	}
}

// Write appends line to the ring buffer and fans it out to all current
// subscribers. Slow subscribers are dropped (non-blocking send).
func (s *LogSink) Write(line string) {
	s.mu.Lock()
	s.ring = append(s.ring, line)
	if len(s.ring) > logSinkCap {
		s.ring = s.ring[len(s.ring)-logSinkCap:]
	}
	subs := make([]chan string, 0, len(s.subs))
	for ch := range s.subs {
		subs = append(subs, ch)
	}
	s.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- line:
		default: // drop if the subscriber is not keeping up
		}
	}
}

// Subscribe returns a copy of the current history, a live channel of future
// lines, and an unsubscribe function the caller must invoke when done.
func (s *LogSink) Subscribe() (history []string, live <-chan string, unsub func()) {
	ch := make(chan string, 64)
	s.mu.Lock()
	history = append([]string(nil), s.ring...)
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return history, ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}
