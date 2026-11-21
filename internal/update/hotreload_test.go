package update

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var fastWait = HotReloadWait{Poll: 2 * time.Millisecond, Timeout: time.Second, Answer: 200 * time.Millisecond, Settle: 20 * time.Millisecond}

func writeCtl(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHotReloadDir_RequiresARunningAgent(t *testing.T) {
	t.Setenv(HotReloadEnv, "")
	if got := HotReloadDir(); got != "" {
		t.Fatalf("without forgo: HotReloadDir() = %q", got)
	}
	dir := t.TempDir()
	t.Setenv(HotReloadEnv, dir)
	if got := HotReloadDir(); got != "" {
		t.Fatalf("agent not started: HotReloadDir() = %q", got)
	}
	writeCtl(t, dir, map[string]string{"agent": "pid 1\n"})
	if got := HotReloadDir(); got != dir {
		t.Fatalf("agent started: HotReloadDir() = %q, want %q", got, dir)
	}
	writeCtl(t, dir, map[string]string{"agent.err": "hot reload is not supported on this architecture\n"})
	if got := HotReloadDir(); got != "" {
		t.Fatalf("agent failed: HotReloadDir() = %q", got)
	}
}

func TestHotReloadGeneration_NewestLink(t *testing.T) {
	dir := t.TempDir()
	if got := HotReloadGeneration(dir); got != 0 {
		t.Fatalf("before any reload = %d", got)
	}
	writeCtl(t, dir, map[string]string{"link-2": "", "link-10": "", "resp-11": "ok", "syms-12": "", "link-x": ""})
	if got := HotReloadGeneration(dir); got != 10 {
		t.Fatalf("generation = %d, want 10", got)
	}
}

func TestWaitHotReload_Verdicts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		files   map[string]string
		applied bool
		reason  string
	}{
		{"applied", map[string]string{"link-2": "patch 1 2\n", "resp-2": "ok\n"}, true, ""},
		{"refused", map[string]string{"link-2": "refuse the type main.Point changed shape\nrefuse init changed\n"}, false, "forgo cannot reload without restarting: the type main.Point changed shape; init changed"},
		{"agent error", map[string]string{"link-2": "patch 1 2\n", "resp-2": "err image does not fit in the reserved region; restart the program\n"}, false, "forgo could not apply the reload: image does not fit"},
		{"unanswered", map[string]string{"link-2": "patch 1 2\n"}, false, "forgo did not apply reload 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeCtl(t, dir, map[string]string{"link-1": "", "resp-1": "ok\n"})
			go func() {
				time.Sleep(10 * time.Millisecond)
				writeCtl(t, dir, tc.files)
			}()
			res := WaitHotReload(context.Background(), dir, 1, fastWait)
			if res.Applied != tc.applied || res.Generation != 2 || !strings.Contains(res.Reason, tc.reason) {
				t.Fatalf("result = %+v, want applied=%v generation 2 reason %q", res, tc.applied, tc.reason)
			}
		})
	}
}

// A reload that started on a partly pulled tree is followed by another; the
// later generation's verdict is the one that counts.
func TestWaitHotReload_LaterGenerationWins(t *testing.T) {
	dir := t.TempDir()
	go func() {
		time.Sleep(10 * time.Millisecond)
		writeCtl(t, dir, map[string]string{"link-1": "patch 1 2\n", "resp-1": "ok\n"})
		time.Sleep(30 * time.Millisecond)
		writeCtl(t, dir, map[string]string{"link-2": "refuse the signature of server.New changed\n"})
	}()
	res := WaitHotReload(context.Background(), dir, 0, fastWait)
	if res.Applied || res.Generation != 2 || !strings.Contains(res.Reason, "server.New") {
		t.Fatalf("result = %+v, want generation 2's refusal", res)
	}
}

func TestWaitHotReload_NoReloadTimesOut(t *testing.T) {
	dir := t.TempDir()
	writeCtl(t, dir, map[string]string{"link-1": "", "resp-1": "ok\n"})
	opts := fastWait
	opts.Timeout = 50 * time.Millisecond
	res := WaitHotReload(context.Background(), dir, 1, opts)
	if res.Applied || !strings.Contains(res.Reason, "did not reload within") {
		t.Fatalf("result = %+v, want a timeout, not the earlier generation's ok", res)
	}
}

func TestPlanDevReload(t *testing.T) {
	checkout, _ := setupCheckout(t)
	write := func(name, body string) {
		t.Helper()
		full := filepath.Join(checkout, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("skills/registry.go", "package skills\n\n//go:embed */*\nvar fs embed.FS\n")
	write("skills/a/SKILL.md", "a")
	write("web/app.ts", "a")
	gitT(t, checkout, "add", "-A")
	gitT(t, checkout, "commit", "-q", "-m", "base")
	base := gitT(t, checkout, "rev-parse", "HEAD")

	change := func(files map[string]string) string {
		t.Helper()
		gitT(t, checkout, "checkout", "-q", base)
		for name, body := range files {
			write(name, body)
		}
		gitT(t, checkout, "add", "-A")
		gitT(t, checkout, "commit", "-q", "-m", "change")
		return gitT(t, checkout, "rev-parse", "HEAD")
	}

	for _, tc := range []struct {
		name   string
		files  map[string]string
		hot    bool
		goSrc  bool
		reason string
	}{
		{"go only", map[string]string{"internal/x/x.go": "package x\n"}, true, true, ""},
		{"go without forgo", map[string]string{"internal/x/x.go": "package x\n"}, false, true, "forgo hot reload is not active"},
		{"go.mod", map[string]string{"internal/x/x.go": "package x\n", "go.mod": "module github.com/lsegal/aviary\n\ngo 1.25\n"}, true, false, "go.mod changed"},
		{"embedded file", map[string]string{"skills/a/SKILL.md": "b"}, true, false, "embedded file skills/a/SKILL.md changed"},
		{"frontend only", map[string]string{"web/app.ts": "b"}, false, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			to := change(tc.files)
			plan, err := PlanDevReload(context.Background(), checkout, base, to, tc.hot)
			if err != nil {
				t.Fatal(err)
			}
			if plan.GoChanged != tc.goSrc || plan.RestartReason != tc.reason {
				t.Fatalf("plan = %+v, want GoChanged=%v RestartReason=%q", plan, tc.goSrc, tc.reason)
			}
		})
	}
}

func TestGitPullMessage_SaysHowTheCodeGoesLive(t *testing.T) {
	res := GitPullResult{From: "a", To: "b", Commits: 1, Supervised: true, HotReload: true}
	for _, tc := range []struct {
		plan DevReloadPlan
		want string
	}{
		{DevReloadPlan{GoChanged: true}, "hot-reloads the new code"},
		{DevReloadPlan{}, "No backend code changed."},
		{DevReloadPlan{GoChanged: true, RestartReason: "go.mod changed"}, "Aviary restarts to build and run the new code (go.mod changed)."},
	} {
		if got := GitPullMessage(res, tc.plan); !strings.Contains(got, tc.want) {
			t.Errorf("GitPullMessage(%+v) = %q, want %q", tc.plan, got, tc.want)
		}
	}
}
