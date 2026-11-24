package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

func fastLockRetries(t *testing.T, delays ...time.Duration) {
	t.Helper()
	orig := lockRetryDelays
	lockRetryDelays = delays
	t.Cleanup(func() { lockRetryDelays = orig })
}

func TestGitPull_ConcurrentTriggersPullOnce(t *testing.T) {
	checkout, _ := setupCheckout(t)
	const n = 5
	results := make([]GitPullResult, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = GitPull(WithCaller(context.Background(), "test"), checkout)
		}()
	}
	wg.Wait()
	pulls := 0
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("GitPull %d: %v", i, errs[i])
		}
		pulls += results[i].Commits
	}
	if pulls != 1 {
		t.Fatalf("pulled %d commit(s) across %d triggers, want exactly one pull: %+v", pulls, n, results)
	}
}

func TestGitPull_WaitsForAnotherProcessesUpdaterLock(t *testing.T) {
	checkout, _ := setupCheckout(t)
	other := flock.New(filepath.Join(checkout, ".git", updaterLockName))
	if ok, err := other.TryLock(); err != nil || !ok {
		t.Fatalf("taking the updater lock: %v %v", ok, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	before := gitT(t, checkout, "rev-parse", "HEAD")
	if _, err := GitPull(ctx, checkout); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GitPull while another process holds the lock: err = %v, want a deadline", err)
	}
	if after := gitT(t, checkout, "rev-parse", "HEAD"); after != before {
		t.Fatal("pulled while another process held the updater lock")
	}

	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = other.Unlock()
	}()
	res, err := GitPull(context.Background(), checkout)
	if err != nil || res.Commits != 1 {
		t.Fatalf("GitPull after the lock was released = %+v, %v", res, err)
	}
}

func TestReadGitState_PollingLeavesTheIndexAlone(t *testing.T) {
	checkout, _ := setupCheckout(t)
	index := filepath.Join(checkout, ".git", "index")
	// A changed mtime on a tracked file makes git status refresh the
	// index, taking index.lock, unless optional locks are off.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(checkout, "go.mod"), future, future); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := ReadGitState(context.Background(), checkout); err != nil {
		t.Fatalf("ReadGitState: %v", err)
	}
	if _, err := gitCheckResult(context.Background(), checkout); err != nil {
		t.Fatalf("gitCheckResult: %v", err)
	}
	after, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("polling rewrote the index (mtime %v → %v), so it took index.lock", before.ModTime(), after.ModTime())
	}

	// Control: plain git status does refresh this index.
	gitT(t, checkout, "status", "--porcelain")
	if again, _ := os.Stat(index); again.ModTime().Equal(before.ModTime()) {
		t.Skip("this git does not refresh the index on status; the check above proves nothing here")
	}
}

func TestGitPull_RetriesWhileAnotherGitHoldsIndexLock(t *testing.T) {
	checkout, _ := setupCheckout(t)
	fastLockRetries(t, 50*time.Millisecond, 100*time.Millisecond, 200*time.Millisecond, 400*time.Millisecond, 800*time.Millisecond)
	lock := filepath.Join(checkout, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(250 * time.Millisecond)
		_ = os.Remove(lock)
	}()
	res, err := GitPull(context.Background(), checkout)
	if err != nil || res.Commits != 1 {
		t.Fatalf("GitPull = %+v, %v, want the pull to succeed once the lock is gone", res, err)
	}
}

func TestGitPull_ReportsBusyAndStaleIndexLocks(t *testing.T) {
	checkout, _ := setupCheckout(t)
	fastLockRetries(t, time.Millisecond, time.Millisecond)
	lock := filepath.Join(checkout, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := GitPull(context.Background(), checkout)
	if !errors.Is(err, ErrGitBusy) || ErrorCode(err) != ErrCodeGitBusy {
		t.Fatalf("GitPull with a fresh index.lock: err = %v, want ErrGitBusy", err)
	}
	if strings.Contains(err.Error(), "exit status") {
		t.Fatalf("busy error %q carries raw git output", err)
	}
	if err := RemoveStaleIndexLock(context.Background(), checkout); !errors.Is(err, ErrGitBusy) {
		t.Fatalf("RemoveStaleIndexLock on a fresh lock: err = %v, want it left alone", err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("a fresh lock was removed: %v", err)
	}

	old := time.Now().Add(-2 * staleIndexLockAge)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	_, err = GitPull(context.Background(), checkout)
	var stale *StaleLockError
	if !errors.As(err, &stale) || ErrorCode(err) != ErrCodeGitStaleLock {
		t.Fatalf("GitPull with an old index.lock: err = %v, want a StaleLockError", err)
	}
	if err := RemoveStaleIndexLock(context.Background(), checkout); err != nil {
		t.Fatalf("RemoveStaleIndexLock: %v", err)
	}
	if res, err := GitPull(context.Background(), checkout); err != nil || res.Commits != 1 {
		t.Fatalf("GitPull after removing the stale lock = %+v, %v", res, err)
	}
}

func TestRunGit_InterruptsInsteadOfKilling(t *testing.T) {
	checkout, _ := setupCheckout(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// A cancelled read-only command still fails; the write path ignores the
	// cancellation and runs to completion.
	if _, err := runGit(ctx, checkout, "status"); err == nil {
		t.Fatal("runGit with a cancelled context succeeded")
	}
	if _, err := runGitWrite(ctx, checkout, "fetch", "--quiet"); err != nil {
		t.Fatalf("runGitWrite with a cancelled caller: %v", err)
	}
}
