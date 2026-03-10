package channels

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestLogSink_Write verifies lines are stored in the ring buffer.
func TestLogSink_Write(t *testing.T) {
	s := newLogSink()

	s.Write("line1")
	s.Write("line2")
	s.Write("line3")

	history, _, unsub := s.Subscribe()
	defer unsub()
	assert.Equal(t, 3, len(history))
	assert.Equal(t, "line1", history[0])
	assert.Equal(t, "line3", history[2])

}

// TestLogSink_RingBuffer verifies the ring wraps at capacity.
func TestLogSink_RingBuffer(t *testing.T) {
	s := newLogSink()

	// Write more than capacity.
	for i := 0; i < logSinkCap+10; i++ {
		s.Write("line")
	}

	history, _, unsub := s.Subscribe()
	defer unsub()
	assert.Equal(t, logSinkCap, len(history))

}

// TestLogSink_Subscribe_ReceivesLiveWrites verifies live delivery.
func TestLogSink_Subscribe_ReceivesLiveWrites(t *testing.T) {
	s := newLogSink()

	_, live, unsub := s.Subscribe()
	defer unsub()

	s.Write("fresh line")

	var got string
	select {
	case got = <-live:
	case <-time.After(1 * time.Second):
	}
	assert.Equal(t, "fresh line", got)
}

// TestLogSink_Subscribe_History verifies history is returned at subscribe time.
func TestLogSink_Subscribe_History(t *testing.T) {
	s := newLogSink()
	s.Write("old1")
	s.Write("old2")

	history, _, unsub := s.Subscribe()
	defer unsub()
	assert.Equal(t, 2, len(history))

}

// TestLogSink_Unsubscribe verifies the unsubscribe function removes the subscriber.
func TestLogSink_Unsubscribe(_ *testing.T) {
	s := newLogSink()
	_, _, unsub := s.Subscribe()

	// Unsubscribe should not panic.
	unsub()
	unsub() // double-unsubscribe should also be safe (no panic).
}

// TestLogSink_MultipleSubscribers verifies multiple subscribers each receive writes.
func TestLogSink_MultipleSubscribers(t *testing.T) {
	s := newLogSink()

	_, live1, unsub1 := s.Subscribe()
	defer unsub1()
	_, live2, unsub2 := s.Subscribe()
	defer unsub2()

	s.Write("broadcast")

	for i, ch := range []<-chan string{live1, live2} {
		_ = i
		var got string
		select {
		case got = <-ch:
		case <-time.After(1 * time.Second):
		}
		assert.Equal(t, "broadcast", got)
	}
}
