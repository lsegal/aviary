package wake

import (
	"context"
	"testing"
	"time"
)

// TestDetectReportsAClockJump checks a wall-clock gap far longer than the
// check interval -- the machine slept through it -- is reported with roughly
// how long it slept, while ordinary ticks are not.
func TestDetectReportsAClockJump(t *testing.T) {
	interval := 15 * time.Second
	// The detector reads the clock once to start and once per tick, so it is
	// handed each reading in turn: three ordinary ticks, one after two hours
	// asleep, and one ordinary tick after waking.
	clock := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	readings := make(chan time.Time, 6)
	readings <- clock
	for _, step := range []time.Duration{interval + time.Second, interval, interval - time.Second, interval + 2*time.Hour, interval} {
		clock = clock.Add(step)
		readings <- clock
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	wakes := make(chan time.Duration, 5)
	done := make(chan struct{})
	go func() {
		defer close(done)
		Detect(ctx, ticks, interval, func() time.Time { return <-readings }, func(slept time.Duration) { wakes <- slept })
	}()
	for i := 0; i < 5; i++ {
		ticks <- time.Time{}
	}
	cancel()
	<-done
	close(wakes)
	var got []time.Duration
	for slept := range wakes {
		got = append(got, slept)
	}
	if len(got) != 1 || got[0] != 2*time.Hour {
		t.Fatalf("wakes = %v, want one 2h wake", got)
	}
}

// TestSubscribersShareOneDetector checks every subscriber hears the same wake
// from the one shared detector, and that it stops once nobody listens.
func TestSubscribersShareOneDetector(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a, b := Subscribe(ctx), Subscribe(ctx)
	shared.mu.Lock()
	running := shared.cancel != nil
	shared.mu.Unlock()
	if !running {
		t.Fatal("the shared detector did not start")
	}
	broadcast(3 * time.Minute)
	for _, ch := range []<-chan time.Duration{a, b} {
		if slept := <-ch; slept != 3*time.Minute {
			t.Fatalf("slept %s, want 3m", slept)
		}
	}
	cancel()
	deadline := time.Now().Add(5 * time.Second)
	for {
		shared.mu.Lock()
		stopped := shared.cancel == nil && len(shared.subs) == 0
		shared.mu.Unlock()
		if stopped {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the shared detector kept running with no subscribers")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
