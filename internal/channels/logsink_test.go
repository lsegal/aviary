package channels

import (
	"testing"
	"time"
)

// TestLogSink_Write verifies lines are stored in the ring buffer.
func TestLogSink_Write(t *testing.T) {
	s := newLogSink()

	s.Write("line1")
	s.Write("line2")
	s.Write("line3")

	history, _, unsub := s.Subscribe()
	defer unsub()

	if len(history) != 3 {
		t.Fatalf("expected 3 lines in history, got %d", len(history))
	}
	if history[0] != "line1" || history[2] != "line3" {
		t.Errorf("history content mismatch: %v", history)
	}
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

	if len(history) != logSinkCap {
		t.Fatalf("expected ring to be capped at %d, got %d", logSinkCap, len(history))
	}
}

// TestLogSink_Subscribe_ReceivesLiveWrites verifies live delivery.
func TestLogSink_Subscribe_ReceivesLiveWrites(t *testing.T) {
	s := newLogSink()

	_, live, unsub := s.Subscribe()
	defer unsub()

	s.Write("fresh line")

	select {
	case got := <-live:
		if got != "fresh line" {
			t.Errorf("live received %q; want %q", got, "fresh line")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for live write")
	}
}

// TestLogSink_Subscribe_History verifies history is returned at subscribe time.
func TestLogSink_Subscribe_History(t *testing.T) {
	s := newLogSink()
	s.Write("old1")
	s.Write("old2")

	history, _, unsub := s.Subscribe()
	defer unsub()

	if len(history) != 2 {
		t.Fatalf("expected 2 history lines, got %d", len(history))
	}
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
		select {
		case got := <-ch:
			if got != "broadcast" {
				t.Errorf("subscriber %d received %q; want %q", i+1, got, "broadcast")
			}
		case <-time.After(1 * time.Second):
			t.Fatalf("subscriber %d timeout", i+1)
		}
	}
}
