package saferestart

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testGate(grace, maxWait, admitWait time.Duration) *Gate {
	g := NewGate()
	g.SetTimings(grace, maxWait, admitWait)
	return g
}

// lockAsync runs Lock in the background and returns its result channel.
type lockResult struct {
	unlock    func()
	abandoned int
	err       error
}

func lockAsync(g *Gate, opts LockOptions) <-chan lockResult {
	ch := make(chan lockResult, 1)
	go func() {
		unlock, abandoned, err := g.Lock(context.Background(), opts)
		ch <- lockResult{unlock, abandoned, err}
	}()
	return ch
}

func TestGate_CountsConcurrentCalls(t *testing.T) {
	g := NewGate()
	const n = 50
	releases := make(chan func(), n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			release, err := g.Acquire(context.Background())
			assert.NoError(t, err)
			releases <- release
		})
	}
	wg.Wait()
	close(releases)
	assert.Equal(t, n, g.Active())
	for release := range releases {
		wg.Go(func() {
			release()
			release() // a second release is a no-op
		})
	}
	wg.Wait()
	assert.Equal(t, 0, g.Active())
}

// call holds the gate the way the MCP wrapper does, for the length of fn.
func call(g *Gate, fn func() error) error {
	release, err := g.Acquire(context.Background())
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

func TestGate_ErrorPanicAndCancelReleaseTheCall(t *testing.T) {
	g := NewGate()

	assert.Error(t, call(g, func() error { return errors.New("boom") }))
	assert.Equal(t, 0, g.Active(), "an error releases the call")

	assert.Panics(t, func() { _ = call(g, func() error { panic("boom") }) })
	assert.Equal(t, 0, g.Active(), "a panic releases the call")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		done <- call(g, func() error {
			<-ctx.Done()
			return ctx.Err()
		})
	}()
	require.Eventually(t, func() bool { return g.Active() == 1 }, time.Second, time.Millisecond)
	cancel()
	assert.ErrorIs(t, <-done, context.Canceled)
	assert.Equal(t, 0, g.Active(), "a canceled call releases the gate")
}

func TestGate_RestartWaitsForInFlightCalls(t *testing.T) {
	g := testGate(time.Hour, time.Hour, time.Second)
	release, err := g.Acquire(context.Background())
	require.NoError(t, err)

	waits := make(chan int, 4)
	locked := lockAsync(g, LockOptions{OnWait: func(n int) { waits <- n }})
	assert.Equal(t, 1, <-waits, "the deferred restart reports the active call count")
	select {
	case <-locked:
		t.Fatal("restart took the lock while a call was in flight")
	case <-time.After(50 * time.Millisecond):
	}

	release()
	res := <-locked
	require.NoError(t, res.err)
	assert.Equal(t, 0, res.abandoned)
	res.unlock()
}

func TestGate_CallsDuringRestartWaitThenProceed(t *testing.T) {
	g := testGate(time.Hour, time.Hour, time.Second)
	unlock, _, err := g.Lock(context.Background(), LockOptions{})
	require.NoError(t, err)

	admitted := make(chan error, 1)
	go func() {
		release, err := g.Acquire(context.Background())
		if release != nil {
			release()
		}
		admitted <- err
	}()
	select {
	case <-admitted:
		t.Fatal("a call started while the restart lock was held")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	assert.NoError(t, <-admitted, "the waiting call proceeds once the lock is released")
}

func TestGate_CallsDuringRestartGetRetryableError(t *testing.T) {
	g := testGate(time.Hour, time.Hour, 20*time.Millisecond)
	unlock, _, err := g.Lock(context.Background(), LockOptions{})
	require.NoError(t, err)
	defer unlock()

	_, err = g.Acquire(context.Background())
	assert.ErrorIs(t, err, ErrRestarting)
	assert.Equal(t, "server restarting, try again", err.Error())
	assert.Equal(t, 0, g.Active())
}

func TestGate_GracePeriodBoundsNewCalls(t *testing.T) {
	g := testGate(50*time.Millisecond, time.Hour, 10*time.Millisecond)
	first, err := g.Acquire(context.Background())
	require.NoError(t, err)

	locked := lockAsync(g, LockOptions{})
	require.Eventually(t, func() bool {
		g.mu.Lock()
		defer g.mu.Unlock()
		return !g.pending.IsZero()
	}, time.Second, time.Millisecond)

	second, err := g.Acquire(context.Background())
	require.NoError(t, err, "calls are still admitted during the grace period")
	assert.Equal(t, 2, g.Active())

	time.Sleep(60 * time.Millisecond)
	_, err = g.Acquire(context.Background())
	assert.ErrorIs(t, err, ErrRestarting, "after the grace period new calls no longer hold off the restart")

	first()
	second()
	res := <-locked
	require.NoError(t, res.err)
	res.unlock()
}

func TestGate_MaxWaitBoundsLongCalls(t *testing.T) {
	g := testGate(time.Millisecond, 30*time.Millisecond, time.Millisecond)
	release, err := g.Acquire(context.Background())
	require.NoError(t, err)
	defer release()

	res := <-lockAsync(g, LockOptions{})
	require.NoError(t, res.err)
	assert.Equal(t, 1, res.abandoned, "the restart proceeds past a call that outlasts the max wait")
	res.unlock()
}

func TestGate_ForceSkipsTheWait(t *testing.T) {
	g := testGate(time.Hour, time.Hour, time.Millisecond)
	release, err := g.Acquire(context.Background())
	require.NoError(t, err)
	defer release()

	unlock, abandoned, err := g.Lock(context.Background(), LockOptions{Force: true})
	require.NoError(t, err)
	assert.Equal(t, 1, abandoned)
	unlock()
}

func TestGate_CancelReopens(t *testing.T) {
	g := testGate(time.Millisecond, time.Hour, 10*time.Millisecond)
	release, err := g.Acquire(context.Background())
	require.NoError(t, err)
	defer release()

	cancel := make(chan struct{})
	locked := lockAsync(g, LockOptions{Cancel: cancel})
	require.Eventually(t, func() bool {
		_, err := g.Acquire(context.Background())
		return errors.Is(err, ErrRestarting)
	}, time.Second, time.Millisecond)

	close(cancel)
	assert.ErrorIs(t, (<-locked).err, ErrCanceled)
	next, err := g.Acquire(context.Background())
	require.NoError(t, err, "a canceled restart admits calls again")
	next()
}

func TestGate_StaleUnlockLeavesNewerRestartAlone(t *testing.T) {
	g := testGate(time.Hour, time.Hour, 10*time.Millisecond)
	first, _, err := g.Lock(context.Background(), LockOptions{})
	require.NoError(t, err)
	second, _, err := g.Lock(context.Background(), LockOptions{})
	require.NoError(t, err)

	first()
	_, err = g.Acquire(context.Background())
	assert.ErrorIs(t, err, ErrRestarting, "the replaced restart's unlock does not reopen the gate")

	second()
	release, err := g.Acquire(context.Background())
	require.NoError(t, err)
	release()
}
