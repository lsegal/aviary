//go:build !windows

package process

import (
	"context"
	"syscall"
	"testing"
	"time"
)

// The first shutdown signal runs prepare before the context is canceled, so
// the shutdown checkpoint (issue #741) finishes while agents still exist.
func TestShutdownContextPreparesBeforeCanceling(t *testing.T) {
	contexts := make(chan context.Context, 1)
	prepared := make(chan error, 1)
	ctx, stop := ShutdownContext(func() {
		ctx := <-contexts
		prepared <- ctx.Err()
	})
	defer stop()
	contexts <- ctx
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-prepared:
		if err != nil {
			t.Fatalf("context was already canceled during prepare: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SIGHUP did not run prepare")
	}
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("context was not canceled after prepare")
	}
}
