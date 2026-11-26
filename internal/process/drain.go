package process

import (
	"io"
	"sync"
)

// OutputDrain is a child's stdout/stderr writer that never makes the child's
// output wait on whatever consumes it. Writes are queued and handed to the
// underlying writer by a goroutine of its own, so the pipe exec.Cmd copies
// from is always read as fast as the child writes it.
//
// Without it, a consumer that is slow for a moment -- a UI refresh that reads
// CLI quotas, say -- stalls exec.Cmd's copy goroutine. A child that exits
// during that stall leaves its last output, such as an agent's final result
// event, in the pipe, and cmd.WaitDelay expires and discards it before it is
// read. The run is then reported as not exiting cleanly even though the child
// finished (#1420).
type OutputDrain struct {
	out     io.Writer
	mu      sync.Mutex
	ready   *sync.Cond
	pending [][]byte
	closed  bool
	err     error
	done    chan struct{}
}

// NewOutputDrain starts draining into out. Close it once the child has been
// waited on to deliver everything it wrote.
func NewOutputDrain(out io.Writer) *OutputDrain {
	d := &OutputDrain{out: out, done: make(chan struct{})}
	d.ready = sync.NewCond(&d.mu)
	go d.run()
	return d
}

// Write queues a copy of p and returns at once. Writes after Close fail with
// io.ErrClosedPipe.
func (d *OutputDrain) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return 0, io.ErrClosedPipe
	}
	if len(p) > 0 {
		d.pending = append(d.pending, append([]byte(nil), p...))
		d.ready.Signal()
	}
	return len(p), nil
}

// Close waits until every queued write has reached the underlying writer and
// returns the first error that writer reported.
func (d *OutputDrain) Close() error {
	d.mu.Lock()
	d.closed = true
	d.ready.Signal()
	d.mu.Unlock()
	<-d.done
	return d.err
}

func (d *OutputDrain) run() {
	defer close(d.done)
	for {
		d.mu.Lock()
		for len(d.pending) == 0 && !d.closed {
			d.ready.Wait()
		}
		batch := d.pending
		d.pending = nil
		closed := d.closed
		d.mu.Unlock()
		for _, chunk := range batch {
			if _, err := d.out.Write(chunk); err != nil && d.err == nil {
				d.err = err
			}
		}
		if closed && len(batch) == 0 {
			return
		}
	}
}
