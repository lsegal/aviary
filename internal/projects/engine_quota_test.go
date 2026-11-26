package projects

import (
	"context"
	"io"
	"testing"
	"time"
)

// timedOutputRunner reports how long its single job-output write took.
type timedOutputRunner struct {
	took chan time.Duration
}

func (timedOutputRunner) AgentName() string                { return "codex" }
func (timedOutputRunner) Run(context.Context, Issue) error { return nil }

func (r timedOutputRunner) RunWithOutput(_ context.Context, _ Issue, output io.Writer) error {
	start := time.Now()
	_, err := io.WriteString(output, "agent output\n")
	r.took <- time.Since(start)
	return err
}

func TestJobOutputWriteDoesNotWaitOnQuotaReader(t *testing.T) {
	release := make(chan struct{})
	reading := make(chan struct{}, 1)
	runner := timedOutputRunner{took: make(chan time.Duration, 1)}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1,
		Issues: &fakeSource{batches: [][]Issue{{{Number: 1, Title: "bug"}}}},
		Runner: runner, UI: &snapshotReporter{},
		Quota: func(ctx context.Context) map[string]string {
			select {
			case reading <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-ctx.Done():
			}
			return map[string]string{"codex": "week 50% left"}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		close(release)
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()

	select {
	case <-reading:
	case <-time.After(5 * time.Second):
		t.Fatal("quota reader was never called")
	}
	select {
	case took := <-runner.took:
		if took > time.Second {
			t.Fatalf("job-output write took %v while a quota reader blocked", took)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("job-output write blocked on the quota reader")
	}
}

func TestPublishShowsCachedQuotaReadings(t *testing.T) {
	reporter := &snapshotReporter{}
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1,
		Issues: &fakeSource{batches: [][]Issue{{}}}, Runner: fakeOutputRunner{}, UI: reporter,
		Quota: func(context.Context) map[string]string {
			return map[string]string{"codex": "week 50% left"}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		reporter.mu.Lock()
		found := false
		for _, snapshot := range reporter.snapshots {
			if snapshot.Quotas["codex"] == "week 50% left" {
				found = true
			}
		}
		reporter.mu.Unlock()
		if found {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatal("no snapshot showed the cached quota reading")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
