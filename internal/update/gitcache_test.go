package update

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// resetGitCaches forgets the cached checkout and HEAD for one test.
func resetGitCaches(t *testing.T) {
	t.Helper()
	reset := func() {
		checkoutMu.Lock()
		checkoutFound, checkoutTop = false, ""
		checkoutMu.Unlock()
		headMu.Lock()
		headDir, headSHA, headReadAt = "", "", time.Time{}
		headMu.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

func TestGitCheckout_DetectsTheCheckoutOncePerProcess(t *testing.T) {
	checkout, _ := setupCheckout(t)
	withGitMode(t, "dev", "", false)
	checkoutDir = cachedCheckout
	resetGitCaches(t)
	t.Setenv(CheckoutEnv, checkout)

	before := gitRuns.Load()
	for range 20 {
		if got, want := GitCheckout(), mustEvalSymlinks(t, checkout); got != want {
			t.Fatalf("GitCheckout() = %q, want %q", got, want)
		}
	}
	if runs := gitRuns.Load() - before; runs != 1 {
		t.Fatalf("20 GitCheckout calls ran git %d times, want 1", runs)
	}
}

func TestCurrentVersion_ReusesHEADUntilItExpires(t *testing.T) {
	checkout, _ := setupCheckout(t)
	withGitMode(t, "dev", checkout, false)
	resetGitCaches(t)
	origTTL := headTTL
	t.Cleanup(func() { headTTL = origTTL })
	headTTL = time.Hour

	first := CurrentVersion(context.Background())
	if want := gitT(t, checkout, "rev-parse", "HEAD"); first != want {
		t.Fatalf("CurrentVersion() = %q, want HEAD %q", first, want)
	}

	// Dashboards polling /api/health and every WebSocket heartbeat call
	// CurrentVersion; none of them may run git while the HEAD is fresh.
	before := gitRuns.Load()
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() { CurrentVersion(context.Background()) })
	}
	wg.Wait()
	if runs := gitRuns.Load() - before; runs != 0 {
		t.Fatalf("50 version lookups ran git %d times, want 0", runs)
	}

	commitFile(t, checkout, "b.txt", "b")
	newHead := gitT(t, checkout, "rev-parse", "HEAD")
	if got := CurrentVersion(context.Background()); got != first {
		t.Fatalf("CurrentVersion() = %q before the HEAD expired, want cached %q", got, first)
	}
	headTTL = 20 * time.Millisecond
	time.Sleep(30 * time.Millisecond)
	if got := CurrentVersion(context.Background()); got != newHead {
		t.Fatalf("CurrentVersion() = %q after the TTL, want new HEAD %q", got, newHead)
	}
}

func TestCurrentVersion_UpdateCheckRefreshesHEAD(t *testing.T) {
	checkout, _ := setupCheckout(t)
	withGitMode(t, "dev", checkout, false)
	resetGitCaches(t)
	origTTL := headTTL
	t.Cleanup(func() { headTTL = origTTL })
	headTTL = time.Hour

	CurrentVersion(context.Background())
	commitFile(t, checkout, "b.txt", "b")
	newHead := gitT(t, checkout, "rev-parse", "HEAD")
	check, err := Check(context.Background(), nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if check.CurrentVersion != newHead {
		t.Fatalf("check.CurrentVersion = %q, want new HEAD %q", check.CurrentVersion, newHead)
	}
	if got := CurrentVersion(context.Background()); got != newHead {
		t.Fatalf("CurrentVersion() after a check = %q, want new HEAD %q", got, newHead)
	}
}

func TestRunGit_LogsOnlyFailuresWithTheirCaller(t *testing.T) {
	checkout, _ := setupCheckout(t)
	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(orig) })

	ctx := WithCaller(context.Background(), "GET /api/health")
	if _, err := runGit(ctx, checkout, "rev-parse", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("a successful git command logged %q, want nothing", buf.String())
	}
	if _, err := runGit(ctx, checkout, "rev-parse", "no-such-ref"); err == nil {
		t.Fatal("rev-parse of a missing ref succeeded")
	}
	if log := buf.String(); !strings.Contains(log, `caller="GET /api/health"`) {
		t.Fatalf("failure log %q does not name its caller", log)
	}
}

func TestThrottledCheck_RunsOneCheckAtATime(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)
	var runs atomic.Int32
	release := make(chan struct{})
	check := func() (CheckResult, error) {
		runs.Add(1)
		<-release
		return CheckResult{LatestVersion: "1.2.3"}, nil
	}
	var wg sync.WaitGroup
	results := make([]CheckResult, 8)
	for i := range results {
		wg.Go(func() { results[i], _ = throttledCheck(context.Background(), false, check) })
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := runs.Load(); n != 1 {
		t.Fatalf("8 concurrent checks ran %d times, want 1", n)
	}
	for i, r := range results {
		if r.LatestVersion != "1.2.3" {
			t.Fatalf("caller %d got %+v, want the shared result", i, r)
		}
	}
}

func TestThrottledCheck_BacksOffAfterErrors(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)
	var runs int
	failing := func() (CheckResult, error) {
		runs++
		return CheckResult{}, errors.New("network down")
	}
	for range 5 {
		if _, err := throttledCheck(context.Background(), false, failing); err == nil {
			t.Fatal("throttledCheck hid the failure")
		}
	}
	if runs != 1 {
		t.Fatalf("5 checks within the backoff ran %d times, want 1", runs)
	}

	cacheMu.Lock()
	cacheExpiry = time.Time{}
	cacheMu.Unlock()
	throttledCheck(context.Background(), false, failing) //nolint:errcheck
	cacheMu.RLock()
	wait, failures := time.Until(cacheExpiry), checkFailures
	cacheMu.RUnlock()
	if failures != 2 || wait <= checkErrorBackoff {
		t.Fatalf("after 2 failures: backoff %s (failures %d), want more than %s", wait, failures, checkErrorBackoff)
	}
}

func TestThrottledCheck_DoesNotCacheACallerCancellation(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	throttledCheck(ctx, false, func() (CheckResult, error) { return CheckResult{}, ctx.Err() }) //nolint:errcheck
	var ran bool
	if _, err := throttledCheck(context.Background(), false, func() (CheckResult, error) {
		ran = true
		return CheckResult{}, nil
	}); err != nil || !ran {
		t.Fatalf("check after a cancelled one: ran=%v err=%v, want a fresh run", ran, err)
	}
}

func TestCheckBackoff_DoublesUpToTheCheckInterval(t *testing.T) {
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, checkTTL, checkTTL}
	for i, w := range want {
		if got := checkBackoff(i + 1); got != w {
			t.Fatalf("checkBackoff(%d) = %s, want %s", i+1, got, w)
		}
	}
}

func mustEvalSymlinks(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
