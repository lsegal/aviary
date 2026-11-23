package update

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// HotReloadEnv is set by `forgo run --watch` (which scripts/dev-go.mjs runs)
// to the control directory its in-process agent and the watcher share. Each
// reload is a generation N: the watcher links link-N, which lists the reasons
// it refuses a reload on "refuse " lines, and the agent answers resp-N with
// "ok" or "err <reason>" once it has applied the image or failed to.
const HotReloadEnv = "FORGO_HOT_DIR"

// HotReloadDir returns forgo's hot reload control directory when the backend
// runs under `forgo run --watch` and its agent started, or "" otherwise.
func HotReloadDir() string {
	dir := os.Getenv(HotReloadEnv)
	if dir == "" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(dir, "agent.err")); err == nil {
		return ""
	}
	if _, err := os.Stat(filepath.Join(dir, "agent")); err != nil {
		return ""
	}
	return dir
}

// HotReloadGeneration returns the newest reload generation the watcher has
// linked in dir, or 0 before the first reload.
func HotReloadGeneration(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	latest := 0
	for _, e := range entries {
		g, ok := strings.CutPrefix(e.Name(), "link-")
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(g); err == nil && n > latest {
			latest = n
		}
	}
	return latest
}

// HotReloadResult is forgo's verdict on the reloads after a pull.
type HotReloadResult struct {
	// Applied is true when the newest reload went live in this process.
	Applied bool
	// Generation is the reload generation the verdict is about.
	Generation int
	// Reason says why the pulled code is not running when Applied is false.
	Reason string
}

// HotReloadWait tunes WaitHotReload.
type HotReloadWait struct {
	// Poll is how often the control directory is read.
	Poll time.Duration
	// Timeout bounds the whole wait.
	Timeout time.Duration
	// Answer bounds how long a linked generation may go unanswered; the
	// watcher itself gives the agent 30 seconds.
	Answer time.Duration
	// Settle is added to the time the verdict took to arrive to decide how
	// long to keep watching for a later generation: one that started on a
	// partly written tree is followed by another once the first finishes,
	// and that one takes about as long again.
	Settle time.Duration
}

// DefaultHotReloadWait is how long a dev update waits for forgo.
var DefaultHotReloadWait = HotReloadWait{
	Poll:    250 * time.Millisecond,
	Timeout: 5 * time.Minute,
	Answer:  45 * time.Second,
	Settle:  5 * time.Second,
}

// WaitHotReload waits for forgo to reload the code a pull brought in, and
// reports whether the newest reload after generation since applied. A reload
// forgo refuses or fails, or no reload at all before the timeout (a pulled
// tree that does not compile never links), is reported with its reason.
func WaitHotReload(ctx context.Context, dir string, since int, opts HotReloadWait) HotReloadResult {
	start := time.Now()
	deadline := time.NewTimer(opts.Timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(opts.Poll)
	defer ticker.Stop()

	var (
		gen      int
		seen     time.Time
		verdict  *HotReloadResult
		decided  time.Time
		settleBy time.Duration
	)
	for {
		if latest := HotReloadGeneration(dir); latest > since && latest != gen {
			gen, seen, verdict = latest, time.Now(), nil
		}
		if gen > 0 && verdict == nil {
			if res, ok := hotReloadVerdict(dir, gen); ok {
				verdict = &res
			} else if time.Since(seen) > opts.Answer {
				verdict = &HotReloadResult{Generation: gen, Reason: fmt.Sprintf("forgo did not apply reload %d", gen)}
			}
			if verdict != nil {
				decided = time.Now()
				settleBy = decided.Sub(start) + opts.Settle
			}
		}
		if verdict != nil && time.Since(decided) >= settleBy {
			return *verdict
		}

		select {
		case <-ctx.Done():
			return HotReloadResult{Generation: gen, Reason: "stopped waiting for forgo: " + ctx.Err().Error()}
		case <-deadline.C:
			if verdict != nil {
				return *verdict
			}
			return HotReloadResult{Generation: gen, Reason: fmt.Sprintf("forgo did not reload within %s (the pulled code may not compile)", opts.Timeout)}
		case <-ticker.C:
		}
	}
}

// hotReloadVerdict reads generation gen's outcome, reporting false while the
// agent has not answered yet.
func hotReloadVerdict(dir string, gen int) (HotReloadResult, bool) {
	g := strconv.Itoa(gen)
	if resp, err := os.ReadFile(filepath.Join(dir, "resp-"+g)); err == nil {
		text := strings.TrimSpace(string(resp))
		if text == "ok" {
			return HotReloadResult{Applied: true, Generation: gen}, true
		}
		return HotReloadResult{Generation: gen, Reason: "forgo could not apply the reload: " + strings.TrimPrefix(text, "err ")}, true
	}
	link, err := os.ReadFile(filepath.Join(dir, "link-"+g))
	if err != nil {
		return HotReloadResult{}, false
	}
	var refused []string
	for _, line := range strings.Split(string(link), "\n") {
		if r, ok := strings.CutPrefix(line, "refuse "); ok {
			refused = append(refused, strings.TrimSpace(r))
		}
	}
	if len(refused) > 0 {
		return HotReloadResult{Generation: gen, Reason: "forgo cannot reload without restarting: " + strings.Join(refused, "; ")}, true
	}
	return HotReloadResult{}, false
}

// DevReloadPlan decides how pulled commits go live in a dev checkout.
type DevReloadPlan struct {
	// GoChanged is true when the pull changed Go sources forgo watches.
	GoChanged bool
	// RestartReason, when set, is why only a full restart runs the pulled
	// code; hot reload cannot.
	RestartReason string
}

// PlanDevReload inspects the files changed between from and to in the dev
// checkout dir. A change to go.mod or go.sum, or to a file a Go package embeds,
// needs a restart, since forgo watches only Go sources. Embedded files a dev
// build reads from the checkout (see DevAssets) are the exception. Otherwise Go changes
// hot-reload when hotReload (forgo is active) is set, and need a restart when
// it is not.
func PlanDevReload(ctx context.Context, dir, from, to string, hotReload bool) (DevReloadPlan, error) {
	out, err := runGit(ctx, dir, "diff", "--name-only", "--no-renames", from, to)
	if err != nil {
		return DevReloadPlan{}, err
	}
	var plan DevReloadPlan
	var other []string
	for _, f := range strings.Split(out, "\n") {
		f = strings.TrimSpace(f)
		switch {
		case f == "":
		case f == "go.mod" || f == "go.sum":
			plan.RestartReason = f + " changed"
			return plan, nil
		case strings.HasSuffix(f, ".go") || strings.HasSuffix(f, ".fgo"):
			plan.GoChanged = true
		default:
			other = append(other, f)
		}
	}
	for _, f := range other {
		if !liveAsset(f) && embeddedBy(dir, f) {
			plan.RestartReason = "embedded file " + f + " changed"
			return plan, nil
		}
	}
	if plan.GoChanged && !hotReload {
		plan.RestartReason = "forgo hot reload is not active"
	}
	return plan, nil
}

// embeddedBy reports whether a //go:embed directive in a package directory
// above the checkout-relative file f matches it.
func embeddedBy(root, f string) bool {
	for d := path.Dir(f); ; d = path.Dir(d) {
		rel := f
		if d != "." {
			rel = strings.TrimPrefix(f, d+"/")
		}
		for _, pattern := range embedPatterns(filepath.Join(root, filepath.FromSlash(d))) {
			if embedMatches(pattern, rel) {
				return true
			}
		}
		if d == "." {
			return false
		}
	}
}

// embedPatterns lists the //go:embed patterns of the Go files in dir.
func embedPatterns(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var patterns []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(file)
		for sc.Scan() {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "//go:embed "); ok {
				for _, p := range strings.Fields(rest) {
					patterns = append(patterns, strings.TrimPrefix(strings.Trim(p, "\"`"), "all:"))
				}
			}
		}
		_ = file.Close()
	}
	return patterns
}

// embedMatches reports whether an embed pattern names rel, either directly or
// as a directory containing it.
func embedMatches(pattern, rel string) bool {
	parts := strings.Split(rel, "/")
	for i := 1; i <= len(parts); i++ {
		if ok, _ := path.Match(pattern, strings.Join(parts[:i], "/")); ok {
			return true
		}
	}
	return false
}
