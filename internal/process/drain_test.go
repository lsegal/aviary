package process

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type blockingWriter struct {
	release chan struct{}
	mu      sync.Mutex
	got     strings.Builder
	err     error
}

func (w *blockingWriter) Write(p []byte) (int, error) {
	<-w.release
	w.mu.Lock()
	defer w.mu.Unlock()
	w.got.Write(p)
	return len(p), w.err
}

func TestOutputDrainWritesReturnWhileConsumerIsBlocked(t *testing.T) {
	out := &blockingWriter{release: make(chan struct{})}
	drain := NewOutputDrain(out)
	finished := make(chan struct{})
	go func() {
		for _, line := range []string{"one\n", "two\n", "three\n"} {
			if _, err := drain.Write([]byte(line)); err != nil {
				t.Errorf("Write() error = %v", err)
			}
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("Write blocked on the stalled consumer")
	}
	close(out.release)
	if err := drain.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := out.got.String(); got != "one\ntwo\nthree\n" {
		t.Fatalf("delivered %q, want every write in order", got)
	}
	if _, err := drain.Write([]byte("late")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Write after Close error = %v, want io.ErrClosedPipe", err)
	}
}

func TestOutputDrainCloseReportsConsumerError(t *testing.T) {
	failure := errors.New("disk full")
	out := &blockingWriter{release: make(chan struct{}), err: failure}
	close(out.release)
	drain := NewOutputDrain(out)
	_, _ = drain.Write([]byte("x"))
	if err := drain.Close(); !errors.Is(err, failure) {
		t.Fatalf("Close() error = %v, want %v", err, failure)
	}
}
