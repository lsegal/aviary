package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
)

// documentResponse is a main-frame document response as the browser reports it.
func documentResponse(frame cdp.FrameID, status int64) *network.EventResponseReceived {
	return &network.EventResponseReceived{
		FrameID:  frame,
		Type:     network.ResourceTypeDocument,
		Response: &network.Response{Status: status},
	}
}

func TestTabRecordsMainFrameStatus(t *testing.T) {
	tab := &Tab{mainFrame: "main"}
	if got := tab.HTTPStatus(); got != 0 {
		t.Fatalf("status %d before any navigation, want 0", got)
	}
	tab.observe(documentResponse("main", 404))
	if got := tab.HTTPStatus(); got != 404 {
		t.Fatalf("status %d, want 404", got)
	}
}

// TestTabIgnoresSubframeStatus guards the status Aviary reads against
// being replaced by whatever an embedded frame on the page was served with.
func TestTabIgnoresSubframeStatus(t *testing.T) {
	tab := &Tab{mainFrame: "main"}
	tab.observe(documentResponse("main", 200))
	tab.observe(documentResponse("embedded", 500))
	if got := tab.HTTPStatus(); got != 200 {
		t.Fatalf("status %d, want the main frame's 200", got)
	}
}

func TestTabIgnoresNonDocumentResponses(t *testing.T) {
	tab := &Tab{mainFrame: "main"}
	tab.observe(documentResponse("main", 200))
	tab.observe(&network.EventResponseReceived{
		FrameID:  "main",
		Type:     network.ResourceTypeXHR,
		Response: &network.Response{Status: 503},
	})
	if got := tab.HTTPStatus(); got != 200 {
		t.Fatalf("status %d, want the document's 200", got)
	}
}

// TestTabTracksMainFrameChanges checks the tab follows its own frame
// across navigations rather than latching onto the id it started with.
func TestTabTracksMainFrameChanges(t *testing.T) {
	tab := &Tab{mainFrame: "main"}
	tab.observe(&page.EventFrameNavigated{Frame: &cdp.Frame{ID: "replacement"}})
	tab.observe(documentResponse("replacement", 200))
	if got := tab.HTTPStatus(); got != 200 {
		t.Fatalf("status %d, want 200 from the new main frame", got)
	}
}

func TestTabIgnoresSubframeNavigations(t *testing.T) {
	tab := &Tab{mainFrame: "main"}
	tab.observe(&page.EventFrameNavigated{Frame: &cdp.Frame{ID: "embedded", ParentID: "main"}})
	tab.observe(documentResponse("embedded", 500))
	tab.observe(documentResponse("main", 200))
	if got := tab.HTTPStatus(); got != 200 {
		t.Fatalf("status %d, want the main frame's 200", got)
	}
}

// TestTabClearsStatusBeforeNavigating checks a navigation that never
// gets a response cannot report the previous page's status.
func TestTabClearsStatusBeforeNavigating(t *testing.T) {
	tab := &Tab{mainFrame: "main"}
	tab.observe(documentResponse("main", 200))
	tab.clearStatus()
	if got := tab.HTTPStatus(); got != 0 {
		t.Fatalf("status %d after clearing, want 0", got)
	}
}

// TestReusesTabPerTarget checks a target is polled through the tab it
// already has, instead of opening a new one on every tick.
func TestReusesTabPerTarget(t *testing.T) {
	existing := &Tab{}
	browser := &Browser{tabs: map[string]*Tab{"owner/repo": existing}}
	tab, err := browser.Tab("owner/repo")
	if err != nil {
		t.Fatalf("Tab: %v", err)
	}
	if tab != existing {
		t.Fatal("Tab opened a new tab for a target that already had one")
	}
}

// closedTab is a tab whose close is observable without a browser behind it.
func closedTab(closed *bool) *Tab {
	return &Tab{cancel: func() { *closed = true }}
}

// TestClosesIdleTabs checks a tab nothing has read for the idle timeout
// is closed instead of holding its page open for the rest of the run: the
// conversation of an issue whose agent has finished used to stay open until the
// run ended (issue #461).
func TestClosesIdleTabs(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	idled := false
	stale := closedTab(&idled)
	stale.resumeAt("https://github.com/owner/repo/issues/7")
	fresh := &Tab{}
	var logged []string
	browser := &Browser{
		tabs:   map[string]*Tab{"owner/repo": fresh, "comments": stale},
		used:   map[string]time.Time{"owner/repo": now.Add(-time.Minute), "comments": now.Add(-5 * time.Minute)},
		resume: map[string]string{},
		idle:   2 * time.Minute,
		now:    func() time.Time { return now },
		logf:   func(format string, v ...interface{}) { logged = append(logged, fmt.Sprintf(format, v...)) },
	}
	if _, err := browser.Tab("owner/repo"); err != nil {
		t.Fatalf("Tab: %v", err)
	}
	if !idled {
		t.Fatal("the idle tab was left open")
	}
	if _, ok := browser.tabs["comments"]; ok {
		t.Fatal("the idle tab is still held by the browser")
	}
	if _, ok := browser.tabs["owner/repo"]; !ok {
		t.Fatal("the tab being read was closed")
	}
	if got := browser.resume["comments"]; got != "https://github.com/owner/repo/issues/7" {
		t.Fatalf("remembered page %q, want the page the closed tab was showing", got)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "comments") || !strings.Contains(logged[0], "5m") {
		t.Fatalf("logged %q, want one line naming the tab and how long it was unread", logged)
	}
}

// TestKeepsLastTabOpen checks the reap never closes the browser's only
// tab: Chrome exits when its final page target closes, and the sign-in saved on
// shutdown is read off a tab of the still-running browser.
func TestKeepsLastTabOpen(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	closed := false
	only := closedTab(&closed)
	browser := &Browser{
		tabs: map[string]*Tab{"comments": only},
		used: map[string]time.Time{"comments": now.Add(-time.Hour)},
		idle: time.Minute,
		now:  func() time.Time { return now },
	}
	// Read a different tab so the idle one is not the tab being handed out;
	// opening one is not possible without a browser, so the reap is called as
	// the hand-out does.
	browser.mu.Lock()
	retired := browser.reapIdleTabs("owner/repo", now)
	browser.mu.Unlock()
	if len(retired) != 0 || closed {
		t.Fatal("the browser closed its only tab")
	}
}

// TestKeepsTabsWithoutIdleTimeout checks a browser that is not polling
// on a timer -- the one `Aviary auth` signs in through -- never retires a tab.
func TestKeepsTabsWithoutIdleTimeout(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	closed := false
	browser := &Browser{
		tabs: map[string]*Tab{"auth": closedTab(&closed), "owner/repo": {}},
		used: map[string]time.Time{"auth": now.Add(-time.Hour), "owner/repo": now},
		now:  func() time.Time { return now },
	}
	if _, err := browser.Tab("owner/repo"); err != nil {
		t.Fatalf("Tab: %v", err)
	}
	if closed || len(browser.tabs) != 2 {
		t.Fatal("a browser with no idle timeout closed a tab")
	}
}

// TestTabReloadResumesRetiredPage checks a tab reopened in place of a
// retired one navigates to the page it is standing in for, because a reader
// polling an unchanged URL asks for a reload and reloading a tab that has never
// been anywhere loads nothing.
func TestTabReloadResumesRetiredPage(t *testing.T) {
	tab := &Tab{}
	if got := tab.resumeTarget(); got != "" {
		t.Fatalf("resume target %q on a tab that was never pointed anywhere, want none", got)
	}
	tab.resumeAt("https://github.com/owner/repo/issues/7")
	if got := tab.resumeTarget(); got != "https://github.com/owner/repo/issues/7" {
		t.Fatalf("resume target %q, want the retired tab's page", got)
	}
	tab.markNavigated()
	if got := tab.resumeTarget(); got != "" {
		t.Fatalf("resume target %q after the tab navigated itself, want none", got)
	}
}

func TestTabRejectedAfterClose(t *testing.T) {
	allocCtx, cancel := context.WithCancel(context.Background())
	repoClosed, commentsClosed := false, false
	browser := &Browser{cancelAlloc: cancel, tabs: map[string]*Tab{
		"owner/repo": closedTab(&repoClosed),
		"comments":   closedTab(&commentsClosed),
	}}
	if err := browser.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !repoClosed || !commentsClosed {
		t.Fatal("Close left a tab open")
	}
	if allocCtx.Err() == nil {
		t.Fatal("Close kept the connection to the browser")
	}
	// Closing twice must be safe: shutdown paths call it defensively.
	if err := browser.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := browser.Tab("owner/repo"); err == nil {
		t.Fatal("expected an error opening a tab on a closed browser")
	}
}

// TestAdoptsHeadedWindowOnce checks only the first tab of a headed
// browser attaches to the window Chrome opened by itself. Creating a target on
// top of that window is what left `Aviary auth` showing an empty "New Tab"
// window beside the login window (issue #412), and a headless run has no window
// to adopt at all.
func TestAdoptsHeadedWindowOnce(t *testing.T) {
	for _, test := range []struct {
		name    string
		browser *Browser
		want    bool
	}{
		{
			name:    "headed first tab",
			browser: &Browser{config: Config{Headed: true}, cmd: &process{}, tabs: map[string]*Tab{}},
			want:    true,
		},
		{
			name:    "headed later tab",
			browser: &Browser{config: Config{Headed: true}, cmd: &process{}, tabs: map[string]*Tab{"owner/repo": {}}},
			want:    false,
		},
		{
			name:    "headless",
			browser: &Browser{config: Config{}, cmd: &process{}, tabs: map[string]*Tab{}},
			want:    false,
		},
		{
			name:    "no launched process",
			browser: &Browser{config: Config{Headed: true}, tabs: map[string]*Tab{}},
			want:    false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.browser.adoptsExistingWindow(); got != test.want {
				t.Fatalf("adoptsExistingWindow() = %t, want %t", got, test.want)
			}
		})
	}
}

// TestTabStopsWaitingForAStuckCommand checks a tab whose command has
// stopped returning fails the next read instead of joining it in blocking
// forever. The poll loop reads GitHub on the goroutine that dispatches work, so
// a command that never comes back used to stop a watch picking anything up for
// the rest of the run (issue #472).
func TestTabStopsWaitingForAStuckCommand(t *testing.T) {
	tab := &Tab{}
	stalled := make(chan struct{}, 1)
	tab.watchStalls(func() { stalled <- struct{}{} })
	// Take the tab as a command that never returns would.
	tab.gate() <- struct{}{}
	release, err := tab.acquire(10 * time.Millisecond)
	if err == nil {
		release()
		t.Fatal("a read of a stuck tab succeeded")
	}
	if !strings.Contains(err.Error(), "stuck") {
		t.Fatalf("error %v, want one saying the tab is stuck", err)
	}
	select {
	case <-stalled:
	default:
		t.Fatal("the stuck tab was not reported as stalled")
	}
	// A command issued behind it fails the same way, within the tab's own
	// command timeout.
	tab.timeout = 10 * time.Millisecond
	if err := tab.Eval("1", nil); err == nil || !strings.Contains(err.Error(), "stuck") {
		t.Fatalf("Eval on a stuck tab: %v, want one saying the tab is stuck", err)
	}
}

// TestTabAdmitsOneCommandAtATime checks the deadline did not cost the
// tab its serialization: a single CDP target cannot service two commands at
// once.
func TestTabAdmitsOneCommandAtATime(t *testing.T) {
	tab := &Tab{}
	release, err := tab.acquire(time.Second)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	release()
	if _, err := tab.acquire(10 * time.Millisecond); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
}

// TestDropsUnresponsiveTab checks a stalled tab is dropped so the next
// read of that target opens a fresh one, on the page the old one was showing,
// rather than queueing behind a command that is never coming back.
func TestDropsUnresponsiveTab(t *testing.T) {
	closed := false
	stuck := closedTab(&closed)
	stuck.resumeAt("https://github.com/owner/repo/issues/7")
	var logged []string
	browser := &Browser{
		tabs:   map[string]*Tab{"comments": stuck},
		used:   map[string]time.Time{"comments": time.Now()},
		resume: map[string]string{},
		logf:   func(format string, v ...interface{}) { logged = append(logged, fmt.Sprintf(format, v...)) },
	}
	browser.discardStalledTab("comments", stuck)
	if _, ok := browser.tabs["comments"]; ok {
		t.Fatal("the unresponsive tab is still held by the browser")
	}
	if got := browser.resume["comments"]; got != "https://github.com/owner/repo/issues/7" {
		t.Fatalf("remembered page %q, want the page the dropped tab was showing", got)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "comments") {
		t.Fatalf("logged %q, want one line naming the dropped tab", logged)
	}
	// Dropping the same tab twice must not drop its replacement.
	replacement := &Tab{}
	browser.tabs["comments"] = replacement
	browser.discardStalledTab("comments", stuck)
	if browser.tabs["comments"] != replacement {
		t.Fatal("a repeated drop took the replacement tab with it")
	}
}

// TestTabCloseDoesNotWaitForACommand checks shutdown is not held up by a
// command that has stopped returning: closing a tab cancels what it is doing
// rather than waiting it out.
func TestTabCloseDoesNotWaitForACommand(t *testing.T) {
	closed := false
	tab := closedTab(&closed)
	tab.gate() <- struct{}{}
	done := make(chan struct{})
	go func() { tab.close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("closing a tab waited for the command in flight")
	}
	if !closed {
		t.Fatal("the tab was not cancelled")
	}
}

// TestRetiresTabThatLostItsConnection checks a tab whose socket to the browser
// closed -- the browser died, or the connection did not survive the machine
// sleeping -- is dropped so the next read opens a fresh one. chromedp cancels
// such a tab's context, which is not a deadline, so it used to stay in place
// and fail every read for the rest of the run (issue #683).
func TestRetiresTabThatLostItsConnection(t *testing.T) {
	tabCtx, cancelTab := context.WithCancel(context.Background())
	cancelTab()
	tab := &Tab{ctx: tabCtx}
	tab.paceWith(context.Background(), nil)
	stalled := make(chan struct{}, 2)
	tab.watchStalls(func() { stalled <- struct{}{} })
	if err := tab.run(); !errors.Is(err, errTabConnectionLost) {
		t.Fatalf("run: %v, want the lost connection reported", err)
	}
	select {
	case <-stalled:
	default:
		t.Fatal("the disconnected tab was not retired")
	}
	// A command waiting its turn on the same tab notices too.
	tab.gate() <- struct{}{}
	if _, err := tab.acquire(time.Second); !errors.Is(err, errTabConnectionLost) {
		t.Fatalf("acquire: %v, want the lost connection reported", err)
	}
	select {
	case <-stalled:
	default:
		t.Fatal("the disconnected tab was not retired while waiting for it")
	}
}

// TestKeepsTabsWhenTheRunShutsDown checks a tab cancelled because the run is
// ending is not mistaken for one that lost its connection.
func TestKeepsTabsWhenTheRunShutsDown(t *testing.T) {
	run, stop := context.WithCancel(context.Background())
	stop()
	tab := &Tab{ctx: run}
	tab.paceWith(run, nil)
	tab.watchStalls(func() { t.Fatal("a tab was retired during shutdown") })
	if err := tab.run(); errors.Is(err, errTabConnectionLost) {
		t.Fatalf("run: %v, want a shutdown error", err)
	}
	// A command waiting its turn during shutdown gets the shutdown error too.
	tab.gate() <- struct{}{}
	if _, err := tab.acquire(time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire: %v, want the run's cancellation", err)
	}
}

// TestRestartIfUnresponsive checks a browser that still answers is kept, and
// that a reader which saw a browser another reader already replaced is told
// to try again rather than restarting it a second time.
func TestRestartIfUnresponsive(t *testing.T) {
	previous := probeWebSocketURL
	defer func() { probeWebSocketURL = previous }()
	probeWebSocketURL = func(context.Context, string) (string, error) { return "ws://live", nil }
	launched := &process{Port: 9222}
	b := &Browser{ctx: context.Background(), cmd: launched}
	if b.restartIfUnresponsive(launched) {
		t.Fatal("a browser that still answers was restarted")
	}
	if b.Recover() {
		t.Fatal("Recover restarted a browser that still answers")
	}
	probeWebSocketURL = func(context.Context, string) (string, error) {
		t.Fatal("a browser already replaced was probed")
		return "", nil
	}
	b.cmd = &process{Port: 9333}
	if !b.restartIfUnresponsive(launched) {
		t.Fatal("a reader of a replaced browser was not told to retry")
	}
	b.closed = true
	if b.restartIfUnresponsive(launched) {
		t.Fatal("a reader of a suspended browser was told to retry")
	}
}

// startFakeBrowser starts a Browser on the stand-in browser, so the process
// bookkeeping is exercised end to end without Chrome installed. The stand-in
// serves its DevTools version endpoint but no DevTools socket, so a tab can
// never be opened on it.
func startFakeBrowser(t *testing.T) (*Browser, Config) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	t.Setenv(fakeBrowserEnv, "1")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	config := Config{Binary: executable, Profile: t.TempDir()}
	b, err := Start(ctx, config)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b, config
}

// failProbes makes every DevTools liveness check fail, as it does for a
// browser that stopped answering while the machine slept.
func failProbes(t *testing.T) {
	t.Helper()
	previous := probeWebSocketURL
	t.Cleanup(func() { probeWebSocketURL = previous })
	probeWebSocketURL = func(context.Context, string) (string, error) {
		return "", errors.New("connection refused")
	}
}

func TestStartReportsALaunchFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-browser")
	b, err := Start(context.Background(), Config{Binary: missing, Profile: t.TempDir()})
	if err == nil {
		_ = b.Close()
		t.Fatal("Start succeeded with a browser binary that does not exist")
	}
	if b != nil {
		t.Fatal("Start returned a Browser alongside its error")
	}
	if got := b.Profile(); got != "" {
		t.Fatalf("Profile() of no browser = %q, want empty", got)
	}
}

// TestSuspendAndResumeStartsANewProcess checks the sign-in window's handoff:
// Suspend stops the process and refuses reads, and Resume starts a new process
// on the same profile that readers can use again, carrying the sign-in into it.
func TestSuspendAndResumeStartsANewProcess(t *testing.T) {
	b, config := startFakeBrowser(t)
	if got := b.Profile(); got != config.Profile {
		t.Fatalf("Profile() = %q, want %q", got, config.Profile)
	}
	first := b.cmd
	b.restored = true

	if err := b.Suspend(); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	if _, err := b.Tab("owner/repo"); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Tab on a suspended browser: %v, want it refused as closed", err)
	}
	if err := b.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if b.closed || b.cmd == first {
		t.Fatal("Resume did not start a new browser process")
	}
	if b.restored {
		t.Fatal("the new process was not marked for its sign-in to be restored")
	}
	if b.tabs == nil || b.used == nil || b.resume == nil {
		t.Fatal("Resume left the tab bookkeeping of the closed process unset")
	}
	if got := b.Profile(); got != config.Profile {
		t.Fatalf("Profile() after Resume = %q, want %q", got, config.Profile)
	}

	// Resuming a browser that is already running keeps its process.
	second := b.cmd
	if err := b.Resume(); err != nil {
		t.Fatalf("second Resume: %v", err)
	}
	if b.cmd != second {
		t.Fatal("Resume replaced a browser that was already running")
	}
}

// TestRecoverRestartsABrowserThatStoppedAnswering is issue #683 end to end: a
// browser whose DevTools endpoint stops answering is replaced, and a
// replacement that cannot start leaves the browser closed and says why.
func TestRecoverRestartsABrowserThatStoppedAnswering(t *testing.T) {
	b, _ := startFakeBrowser(t)
	var logged []string
	b.SetTabIdleTimeout(0, func(format string, v ...interface{}) { logged = append(logged, fmt.Sprintf(format, v...)) })
	failProbes(t)

	first := b.cmd
	if !b.Recover() {
		t.Fatal("Recover kept a browser that stopped answering")
	}
	if b.closed || b.cmd == first {
		t.Fatal("Recover did not start a new browser process")
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "stopped responding") {
		t.Fatalf("logged %q, want one line saying the browser stopped responding", logged)
	}

	logged = nil
	b.config.Binary = filepath.Join(t.TempDir(), "no-such-browser")
	if b.Recover() {
		t.Fatal("Recover reported a restart whose browser could not start")
	}
	if !b.closed {
		t.Fatal("a browser whose restart failed is not left closed")
	}
	if len(logged) != 2 || !strings.HasPrefix(logged[1], "restart browser: ") {
		t.Fatalf("logged %q, want the restart failure reported", logged)
	}
}

// TestTabRetriesOnARestartedBrowser checks a tab that cannot be opened because
// the browser stopped answering is tried once more on a new process, and that
// the failure still names the target when the new process cannot open it
// either.
func TestTabRetriesOnARestartedBrowser(t *testing.T) {
	previous := tabOpenTimeout
	tabOpenTimeout = 5 * time.Second
	t.Cleanup(func() { tabOpenTimeout = previous })
	b, _ := startFakeBrowser(t)
	failProbes(t)

	first := b.cmd
	_, err := b.Tab("owner/repo")
	if err == nil || !strings.Contains(err.Error(), "open browser tab for owner/repo") {
		t.Fatalf("Tab: %v, want the failure to name the target", err)
	}
	if b.cmd == first {
		t.Fatal("a tab the browser could not open did not restart the browser")
	}
	if len(b.tabs) != 0 {
		t.Fatalf("a tab that failed to open was kept: %v", b.tabs)
	}
}

// TestSetLoadQueuePacesOpenTabs checks a queue attached after tabs are open
// paces them too, since tabs are opened lazily and may predate it.
func TestSetLoadQueuePacesOpenTabs(t *testing.T) {
	tab := &Tab{}
	b := &Browser{ctx: context.Background(), tabs: map[string]*Tab{"owner/repo": tab}}
	if b.LoadQueue() != nil {
		t.Fatal("a browser with no queue reported one")
	}
	queue := NewLoadQueue(time.Minute, nil)
	b.SetLoadQueue(queue)
	if b.LoadQueue() != queue {
		t.Fatal("LoadQueue() did not report the attached queue")
	}
	if tab.queue != queue || tab.paceCtx != b.ctx {
		t.Fatal("an open tab was not paced by the attached queue")
	}
}

// TestReapsIdleTabsWithoutALogger checks a browser given no logger still
// retires an idle tab on the real clock, and keeps one read recently.
func TestReapsIdleTabsWithoutALogger(t *testing.T) {
	idled, recent := false, false
	b := &Browser{
		tabs: map[string]*Tab{"owner/repo": {}, "comments": closedTab(&idled), "board": closedTab(&recent)},
		used: map[string]time.Time{
			"owner/repo": time.Now(),
			"comments":   time.Now().Add(-time.Hour),
			"board":      time.Now(),
		},
		idle: time.Minute,
	}
	if _, err := b.Tab("owner/repo"); err != nil {
		t.Fatalf("Tab: %v", err)
	}
	if !idled {
		t.Fatal("the idle tab was left open")
	}
	if _, ok := b.tabs["board"]; !ok || recent {
		t.Fatal("a tab read within the idle timeout was closed")
	}
}

// TestTabCommandsWithoutABrowserFail checks a tab with no browser behind it
// fails each command at once instead of hanging, while still keeping the
// bookkeeping a later reload relies on.
func TestTabCommandsWithoutABrowserFail(t *testing.T) {
	const page = "https://github.com/owner/repo/issues/7"
	tab := &Tab{status: 200}
	if err := tab.Navigate(page); err == nil {
		t.Fatal("Navigate succeeded with no browser")
	}
	if got := tab.HTTPStatus(); got != 0 {
		t.Fatalf("status %d after a failed navigation, want the old page's cleared", got)
	}
	if got := tab.currentURL(); got != page {
		t.Fatalf("current URL %q, want the page it was sent to", got)
	}
	if got := tab.resumeTarget(); got != "" {
		t.Fatalf("resume target %q after navigating, want a reload to reload", got)
	}

	tab.status = 200
	if err := tab.Reload(); err == nil {
		t.Fatal("Reload succeeded with no browser")
	}
	if got := tab.HTTPStatus(); got != 0 || tab.currentURL() != page {
		t.Fatalf("after Reload: status %d, URL %q; want 0 and %q", got, tab.currentURL(), page)
	}

	// A reopened tab's reload navigates to the page it stands in for.
	reopened := &Tab{}
	reopened.resumeAt(page)
	if err := reopened.Reload(); err == nil {
		t.Fatal("Reload succeeded with no browser")
	}
	if reopened.resumeTarget() != "" || reopened.currentURL() != page {
		t.Fatal("the reopened tab's reload did not navigate to the page it stands in for")
	}

	if err := tab.Eval("1 + 1", nil); err == nil {
		t.Fatal("Eval succeeded with no browser")
	}
	if image, err := tab.Screenshot(); err == nil || image != nil {
		t.Fatalf("Screenshot = %d bytes, %v; want no image and an error", len(image), err)
	}
}

// TestTabAdmitsAWaitingCommandOnceThePreviousOneFinishes checks a command
// queued behind a slow one runs when that one finishes, rather than failing.
func TestTabAdmitsAWaitingCommandOnceThePreviousOneFinishes(t *testing.T) {
	tab := &Tab{}
	tab.watchStalls(func() { t.Error("a tab whose command finished was retired") })
	gate := tab.gate()
	gate <- struct{}{}
	go func() {
		time.Sleep(20 * time.Millisecond)
		<-gate
	}()
	release, err := tab.acquire(5 * time.Second)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	release()
}
