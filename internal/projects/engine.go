// Package projects is the Projects engine: it watches GitHub repositories,
// project boards and Discussions boards for ready issues and runs coding-CLI
// jobs on them through the gh-fix and gh-discuss skills.
package projects

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/projects/transcript"
)

const (
	stateFilePollInterval = 100 * time.Millisecond
	stateReloadDebounce   = 5 * time.Second
	pushFallbackInterval  = 15 * time.Minute
	webhookRetryLimit     = 3
	// workClosureInterval governs how often each actively-worked issue is
	// polled for closure/competing-claim signals. It matches the default
	// base poll interval rather than undercutting it, since every active
	// issue runs two of these loops in parallel and each poll costs several
	// GitHub API requests; a tighter interval was burning quota far faster
	// than the base issue-list poll.
	workClosureInterval  = 30 * time.Second
	projectProbeInterval = 30 * time.Second
	// reapPollInterval is the longest an instance goes without a reap pass
	// over abandoned work. Polling can be much less frequent than this in
	// webhook push mode, so reaping gets its own floor.
	reapPollInterval = 10 * time.Minute
	// pausedWatchInterval is the longest paused work goes without a wake
	// check. Paused work only resumes when aviary notices its blocker or pull
	// request closing on GitHub, so every paused record is read back on this
	// cadence, including ones whose issue the poll no longer lists. It is
	// never faster than the main poll interval.
	pausedWatchInterval = 10 * time.Minute
)

var errWorkClosedByUser = errors.New("work closed by user")

var errWorkStoppedFromWebUI = errors.New("work stopped from web UI")

type jobAction = core.JobAction

type jobActionRequest struct {
	action jobAction
	done   chan error
}

// errWorkClaimedByOther signals that another aviary instance posted a newer
// starting/continuing claim while this instance was actively working the
// same issue. Per the handoff protocol, the most recent claim
// always wins, so the losing instance cooperatively stops.
var errWorkClaimedByOther = errors.New("work claimed by another instance")

// errWorkUpdated signals that the issue an agent is working changed on GitHub
// while the run was in flight. Unlike the other cancellation
// causes it is not the end of the work: the run loop stops the agent's current
// prompt and immediately resumes the same session with workUpdate's
// instruction, so the closure, edited description, or new comment reaches the
// agent already holding the ticket.
var errWorkUpdated = errors.New("work updated")

// workUpdate is the errWorkUpdated cause, carrying the instruction to prompt
// the resumed session with.
type workUpdate struct {
	instruction string
	// summary is the short form logged when the run is interrupted; the
	// instruction itself is written for the agent, not for the log.
	summary string
	// closed marks the update a closure raised, so the cleanup run it starts
	// is not interrupted again by the same closure.
	closed bool
	// restart marks the update a rejected pull request raised.
	// The work is not over, so a run whose session cannot be resumed to hear
	// about it is started over in a fresh session rather than stopped.
	restart bool
}

func (u *workUpdate) Error() string { return errWorkUpdated.Error() + ": " + u.summary }

func (u *workUpdate) Unwrap() error { return errWorkUpdated }

// workUpdateFor returns the update a run was interrupted to deliver, if it was
// interrupted for one at all.
func workUpdateFor(cause error) (*workUpdate, bool) {
	var update *workUpdate
	if errors.As(cause, &update) {
		return update, true
	}
	return nil, false
}

// isCooperativeCancellation reports whether cause is one of the run
// cancellation reasons that reflect another party taking over the work
// rather than an actual runner failure.
func isCooperativeCancellation(cause error) bool {
	return errors.Is(cause, errWorkClosedByUser) || errors.Is(cause, errWorkClaimedByOther)
}

// Issue and its parts live in package core so the browser driver can produce
// them without importing the root package.
type (
	// Issue is a GitHub issue or Discussion thread a job works.
	Issue = core.Issue
	// IssueLabel is one of an issue's labels.
	IssueLabel = core.IssueLabel
	// IssueDependency is an issue an issue is blocked by.
	IssueDependency = core.IssueDependency
	// IssueSource lists a target's issues.
	IssueSource = core.IssueSource
)

// issueRepository resolves the OWNER/REPO an issue belongs to.
func issueRepository(target string, issue Issue) string {
	return core.IssueRepository(target, issue)
}

// Discussion is a top-level GitHub Discussion thread that has not yet
// received any reply.
type Discussion struct {
	Number    int
	Title     string
	Body      string
	CreatedAt time.Time
}

// DiscussionSource lists the Discussion threads that have no reply yet.
type DiscussionSource interface {
	ListUnansweredDiscussions(context.Context, string) ([]Discussion, error)
}

type (
	// PullRequestWorkState is the state of one pull request working an issue.
	PullRequestWorkState = core.PullRequestWorkState
	// OriginatingWorkState is the state of an issue and its pull requests.
	OriginatingWorkState = core.OriginatingWorkState
	// WorkClosureChecker reads an issue's OriginatingWorkState.
	WorkClosureChecker = core.WorkClosureChecker
)

// ProjectStateSource reads a board's state.
type ProjectStateSource = core.ProjectStateSource

// StackSource reports whether a repository allows stacked pull requests.
type StackSource = core.StackSource

// ForkSource names the fork pull requests come from when a repository is
// not writable.
type ForkSource = core.ForkSource

// IssueStatuser moves an issue's board item to a status.
type IssueStatuser interface {
	SetIssueStatus(context.Context, string, Issue, string) error
}

// AgentRunner runs a coding agent on an issue.
type AgentRunner interface {
	Run(context.Context, Issue) error
}

// AgentOutputRunner runs a coding agent on an issue, copying its output.
type AgentOutputRunner interface {
	RunWithOutput(context.Context, Issue, io.Writer) error
}

// AgentSession is the coding-agent session a job runs in.
type AgentSession struct {
	ID                string
	Agent             string
	CheckoutDirectory string
	Resume            bool
	// RunDir is the sanctioned temp directory a run without a checkout starts
	// in and clones under, chosen once per run by CommandRunner.
	RunDir string
	// Update carries a change made to the issue while the agent was working
	// it. A resumed session that has one is prompted with it
	// instead of the generic "continue", so the closure, edited description,
	// or new comment reaches the agent that is already holding the work
	// rather than being noticed only after the run ends.
	Update string
}

// SessionAgentRunner runs a coding agent in a session it can resume.
type SessionAgentRunner interface {
	RunSession(context.Context, Issue, AgentSession, func(AgentSession)) error
}

// SessionAgentOutputRunner runs a coding agent in a resumable session,
// copying its output.
type SessionAgentOutputRunner interface {
	RunSessionWithOutput(context.Context, Issue, AgentSession, func(AgentSession), io.Writer) error
}

// AgentIdentifier picks the agent for a new job.
type AgentIdentifier interface {
	AgentName() string
}

// TargetAgentIdentifier picks the agent for a new job on one target.
type TargetAgentIdentifier interface {
	AgentNameFor(target string) string
}

// UIReporter receives the engine's published state and log lines.
type UIReporter = core.UIReporter

// Engine is the Projects engine: it watches targets for ready issues, claims
// them, and runs coding-agent jobs on them up to a concurrency limit.
type Engine struct {
	Repo        string
	Targets     []string
	Interval    time.Duration
	UseWebhooks bool
	WebUIURL    string
	Events      <-chan WebhookEvent
	Concurrency int
	StatePath   string
	ReadyState  string
	noMerge     atomic.Bool
	// allowedCommenters is the live allowed-commenter list for the runner's
	// prompts. runner() reads it from job goroutines, so it is kept apart
	// from AllowedCommenters, which only the Run loop touches.
	allowedCommenters atomic.Pointer[[]string]
	noChangelog       atomic.Bool
	// autoUpdate is read by the updater each round.
	autoUpdate atomic.Bool
	// clones keeps the temp directories new clones go in and the record of
	// every clone this instance has made.
	clones cloneState
	Issues IssueSource
	// Discussions lists unanswered top-level Discussion threads for
	// Discussions-board targets. When nil, Discussions-board targets are
	// never polled.
	Discussions DiscussionSource
	Runner      AgentRunner
	// Active is the shutdown checkpoint's record of active runs (issue
	// #741). The runner records runs whose agent is running; the run loop
	// adds runs waiting out a scheduled wakeup, which have a checkout but no
	// agent process. Nil records nothing.
	Active *activeAgents
	Out    io.Writer
	// fallbackInterval overrides the push-mode polling fallback in tests.
	fallbackInterval time.Duration
	// closureInterval overrides active-work closure polling in tests.
	closureInterval time.Duration
	// wakeupDelayOverride replaces the delay of an agent's scheduled wakeup
	// in tests.
	wakeupDelayOverride time.Duration
	// checkpointGit replaces the git a paused run's checkpoint runs in tests.
	checkpointGit checkpointGit
	// Projects supplies the push-mode project board fingerprint. When nil,
	// board changes are only picked up by the fallback poll.
	Projects ProjectStateSource
	// probeInterval overrides the project board probe interval in tests.
	probeInterval time.Duration
	Status        IssueStatuser
	UI            UIReporter
	// Registry is the agent registry this run was configured with. The
	// settings API reports the agents it holds, so the dashboard's agent
	// selector offers a config-defined agent without a code change. A nil
	// registry falls back to the run's own.
	Registry *agents.Registry
	Quota    func(context.Context) map[string]string
	// Identity names this instance in cooperative handoff comments. It is
	// generated once at startup and never persisted.
	Identity Identity
	// AllowedCommenters restricts which GitHub logins may trigger a direct
	// @/aviary:ID mention run, and whose new comments are relayed
	// into a running agent. An empty list places no restriction;
	// the CLI defaults it to the authenticated gh user.
	AllowedCommenters []string
	// SelfLogin is the GitHub login this instance runs as. A new comment from
	// an allowed commenter that mentions it triggers a recheck of its issue
	//. Empty turns that trigger off.
	SelfLogin string
	// Comments drives the cooperative handoff handshake. When
	// nil, ownership negotiation is skipped and dispatch behaves as before.
	Comments CommentClient
	// Mentions is scanned on every poll for direct @/aviary:ID mentions when
	// no webhook delivers them, on an issue and on its pull request alike
	//. Nil leaves mentions to the webhook path alone.
	Mentions MentionSource
	// Webhooks re-reconciles push webhooks on every periodic poll so a
	// repository that joins a project board after startup gets a webhook
	// without a restart. Nil skips reconciliation, as in poll
	// mode where no webhooks are configured at all.
	Webhooks func(context.Context)
	// WebhookOnline reports whether push deliveries are reaching the webhook
	// server, such as whether the ngrok tunnel was alive at its last check
	//. Nil reports push mode as always online.
	WebhookOnline func() bool
	// Recover re-establishes the run's own long-lived connections after the
	// machine wakes from sleep, such as browser mode's browser.
	// Nil has nothing to recover.
	Recover func(context.Context)
	// GitHubSession reports browser delivery's GitHub sign-in, which the
	// board shows as a signed-out banner. Nil outside browser delivery.
	GitHubSession func() core.GitHubSession
	// Held reports whether a safe restart is holding new jobs. While it is,
	// polls dispatch nothing and leave waiting jobs as they are; running jobs
	// carry on. Nil never holds.
	Held func() bool
	// RestartNotice returns, once, the notice a safe restart left for the job
	// it stopped on issue, or "" when it stopped none there. The resumed
	// session is prompted with it. Nil has no notices.
	RestartNotice func(Issue) string
	// ownershipWait overrides the reap grace-period wait in tests.
	ownershipWait func(context.Context) bool
	// reapInterval overrides the periodic reap cadence in tests.
	reapInterval time.Duration
	// pausedInterval overrides pausedWatchInterval in tests.
	pausedInterval time.Duration
	// staleClaim overrides the age at which another instance's claim is
	// considered abandoned in tests.
	staleClaim time.Duration
	// handshakeMu guards handshakes, this instance's own record of the
	// ownership negotiations it has already run.
	handshakeMu sync.Mutex
	handshakes  map[string]handshakeRecord
	// negotiateMu guards negotiating, the set of issues whose ownership
	// handshake is waiting out its grace window in the background. The wait
	// lasts minutes, so it must not run on the poll loop; later
	// polls skip an issue that already has a handshake in flight instead of
	// posting a duplicate ask.
	negotiateMu sync.Mutex
	negotiating map[string]bool
	// negotiateWG tracks those background handshakes so a caller can wait
	// for them to drain.
	negotiateWG sync.WaitGroup
	// negotiatedOnce/negotiated nudge the Run loop to poll again as soon as a
	// background handshake wins its ticket, mirroring the
	// jobActionOnce/jobActions pattern above, so reclaimed work is dispatched
	// then rather than waiting out a slow tick or the next reap.
	negotiatedOnce sync.Once
	negotiated     chan struct{}
	logMu          sync.Mutex
	// repeatMu guards lastLogged, the state last reported under each
	// logChanged key. A poll loop ticking every few seconds must report a
	// summary, or a failure, once rather than on every tick.
	repeatMu   sync.Mutex
	lastLogged map[string]string
	// stacksMu guards stacks, the last answer to whether each repository has
	// GitHub stacked pull requests enabled. Blocked issues are
	// re-evaluated on every poll, so the answer is cached rather than asked
	// again each tick.
	stacksMu sync.Mutex
	stacks   map[string]stacksProbe
	// forksMu guards forks, the last answer to where each repository's fixes are
	// pushed, so permission is not read again for every dispatch.
	forksMu       sync.Mutex
	forks         map[string]forkProbe
	jobActionOnce sync.Once
	jobActions    chan jobActionRequest
	// scheduled carries scheduled prompt runs into the Run loop, and
	// scheduleRerun is what a board Retry on one calls.
	scheduled       scheduledRunChannel
	scheduleRerunMu sync.Mutex
	scheduleRerun   func(scheduleID string) error
	// settingsOnce/settingsRequests carry live settings updates
	// into the Run loop, mirroring the jobActionOnce/jobActions pattern above.
	settingsOnce     sync.Once
	settingsRequests chan settingsRequest
	// readyOnce/ready signal that Run has reached its dispatch select
	// statement. ApplySettings and handleJobAction check this before
	// blocking on settingsRequests/jobActions, which nothing reads until
	// Run gets there -- resetFailedWork and, in webhook mode, ngrok startup
	// and per-target webhook configuration can all still be running, and
	// none of that has a bound on how long it takes.
	readyOnce sync.Once
	ready     chan struct{}
	// notReadyWait overrides notReadyTimeout in tests.
	notReadyWait time.Duration
	// agentOverride holds a live-updated set of active agent specs (issue
	// #341, extended to a multiselect in issue #572). It is read from
	// goroutines the Run loop spawns, so it is a pointer stored atomically
	// rather than a plain field owned by the loop's goroutine.
	agentOverride atomic.Pointer[[]string]
	// handledIssues snapshots the issue keys this instance already has in
	// flight or has finished, taken from .aviary.json and the active set at
	// the top of every poll. Browser mode reads it through issueHandled to
	// decide which extracted issues still need a metadata fetch (issue
	// #381); the issue source runs on the poll's own goroutine but the
	// snapshot is published under workMu, so it is stored atomically.
	handledIssues atomic.Pointer[map[string]bool]
	// unblockInterval is how often the stuck-job watchdog runs, in
	// nanoseconds, or 0 when it is off. unblockReset restarts its
	// wait when the interval changes, and unblockOverride holds the unblock
	// call's agent and model override. All are read from the watchdog's own
	// goroutine, so they are stored atomically.
	unblockInterval  atomic.Int64
	unblockResetOnce sync.Once
	unblockReset     chan struct{}
	unblockOverride  atomic.Pointer[unblockConfig]
	// unblockRun replaces the unblock agent call in tests.
	unblockRun func(context.Context, agentSpec, string) (string, error)
	// unblockTrigger carries unblock rounds the prompt agent asks for into
	// the watchdog's own loop, so they reuse its round and history and never
	// overlap a timed round. An empty key means every stuck job.
	unblockTriggerOnce sync.Once
	unblockTrigger     chan string
	// Transcripts holds every job's structured transcript.
	Transcripts *transcriptStore
	// oneShotRun replaces one-off agent calls such as issue creation in tests.
	oneShotRun func(context.Context, agentSpec, string) (string, error)
	// published is the run's most recently published snapshot, and
	// recentLogs its last log lines.
	published  atomic.Pointer[core.Snapshot]
	recentLogs []string
	// settingsView is the settings snapshot the Run loop last published. The
	// settings modal reads it directly, so opening settings never waits on a
	// poll, startup, or anything else the loop is busy with.
	settingsView atomic.Pointer[SettingsSnapshot]
}

// publishHandledIssues records which issues are no longer dispatch candidates:
// anything with a run in flight, and anything this instance already completed.
// Failed work is deliberately absent, because it is dispatched again and so
// still needs its metadata. Callers hold the lock guarding work and active.
// recentLogLines is how many of the run's latest log lines are kept for
// status reports.
const recentLogLines = 40

func (w *Engine) publishHandledIssues(work map[string]workState, active map[string]string) {
	handled := make(map[string]bool, len(work)+len(active))
	for key := range active {
		handled[key] = true
	}
	for key, state := range work {
		if state.Status == "active" || state.Status == "completed" {
			handled[key] = true
		}
	}
	w.handledIssues.Store(&handled)
}

// issueHandled reports whether the most recent snapshot already accounts for
// issue, so a transport that pays per issue for metadata can skip it. An
// unpublished snapshot treats every issue as unhandled, which is correct for
// the first poll: nothing is in flight yet.
func (w *Engine) issueHandled(issue Issue) bool {
	handled := w.handledIssues.Load()
	if handled == nil {
		return false
	}
	return (*handled)[issueKey(issue)]
}

// acquireSlot blocks until sem has a free slot. Unlike sem.acquire, it also
// services live settings updates while waiting, since it runs
// on the same goroutine as the Run loop's settingsRequestsChan case: raising
// --concurrency is most useful exactly when every slot is taken, and a plain
// sem.acquire here would block that goroutine and deadlock the very request
// meant to free it.
func (w *Engine) acquireSlot(ctx context.Context, sem *concurrencySemaphore) error {
	for {
		select {
		case <-sem.tokens:
			return nil
		case request := <-w.settingsRequestsChan():
			request.done <- w.applySettingsRequest(request.update, sem)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (w *Engine) jobActionRequests() chan jobActionRequest {
	w.jobActionOnce.Do(func() { w.jobActions = make(chan jobActionRequest) })
	return w.jobActions
}

// readyChan lazily creates the channel Run closes once it reaches its
// dispatch select statement.
func (w *Engine) readyChan() chan struct{} {
	w.readyOnce.Do(func() { w.ready = make(chan struct{}) })
	return w.ready
}

// notReadyTimeout bounds how long ApplySettings and handleJobAction wait for
// Run to reach its dispatch loop before failing with core.ErrNotReady.
// Ordinary startup (loading work state, resetFailedWork's GitHub calls, and
// in webhook mode the ngrok tunnel and per-target webhook configuration in
// main.go's run()) reaches the loop well within this, so a request that
// merely arrived first still succeeds once Run gets there -- this is not a
// fail-immediately check. It only bounds the genuinely stuck case, which
// used to block forever with no feedback; the dashboard treats
// the resulting error as a signal to retry, so this only needs to be short
// enough that a retry loop notices, not a hard startup deadline.
const notReadyTimeout = 3 * time.Second

func (w *Engine) checkReady(ctx context.Context) error {
	wait := notReadyTimeout
	if w.notReadyWait > 0 {
		wait = w.notReadyWait
	}
	select {
	case <-w.readyChan():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
		return core.ErrNotReady
	}
}

func (w *Engine) handleJobAction(ctx context.Context, action jobAction) error {
	if err := w.checkReady(ctx); err != nil {
		return err
	}
	request := jobActionRequest{action: action, done: make(chan error, 1)}
	select {
	case w.jobActionRequests() <- request:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-request.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// awaitStoppedRun replies to a stop or retry request from the web UI once the
// run it cancelled has exited, so the request does not report success while
// the agent is still writing, pushing, or posting.
func (w *Engine) awaitStoppedRun(ctx context.Context, number int, exited <-chan struct{}, done chan<- error) {
	select {
	case <-exited:
		w.logf("issue #%d stopped from web UI; its agent has exited", number)
		done <- nil
	case <-ctx.Done():
		done <- fmt.Errorf("job is still stopping: %w", ctx.Err())
	}
}

func (w *Engine) periodicPollInterval() time.Duration {
	if w.UseWebhooks {
		if w.fallbackInterval > 0 {
			return w.fallbackInterval
		}
		return pushFallbackInterval
	}
	return w.Interval
}

// projectBoardProbeInterval is how often push mode checks project targets for
// board-only changes that produce no webhook delivery.
func (w *Engine) projectBoardProbeInterval() time.Duration {
	if w.probeInterval > 0 {
		return w.probeInterval
	}
	return projectProbeInterval
}

// projectProbeTargets lists the targets whose boards need fingerprint probing.
// Only push mode needs it; poll mode already refreshes every Interval, and a
// board that pushes its own changes needs no probe at all.
func (w *Engine) projectProbeTargets(targets []string) []string {
	if !w.UseWebhooks || w.Projects == nil {
		return nil
	}
	var probed []string
	for _, target := range targets {
		if isProjectTarget(target) && !boardPushesChanges(target) {
			probed = append(probed, target)
		}
	}
	return probed
}

// pushedBoardTargets lists the project targets whose board changes arrive as
// webhook deliveries, so they are deliberately left unprobed.
func (w *Engine) pushedBoardTargets(targets []string) []string {
	if !w.UseWebhooks {
		return nil
	}
	var pushed []string
	for _, target := range targets {
		if boardPushesChanges(target) {
			pushed = append(pushed, target)
		}
	}
	return pushed
}

// boardPushesChanges reports whether GitHub delivers projects_v2_item events
// for a project target. An organization-owned project gets an organization
// webhook covering every board edit, and push mode refuses to
// start when that hook cannot be installed, so probing one is pure duplicate
// polling. User-owned projects get no such event, and a repository-scoped
// project URL does not say who owns the board, so both keep probing.
func boardPushesChanges(repo string) bool {
	target, err := parseTarget(repo)
	return err == nil && target.IsProject && target.ProjectOwnerType == "orgs"
}

// watchDescription names the refresh strategy in the startup log. Push mode
// never polls at Interval, so reporting that interval invites the question of
// why a board still looks polled.
func (w *Engine) watchDescription() string {
	if !w.UseWebhooks {
		return fmt.Sprintf("polling every %s", formatInterval(w.Interval))
	}
	return fmt.Sprintf("webhook push with a %s fallback poll", formatInterval(w.periodicPollInterval()))
}

// reapPollTick is how often a reap pass runs when ordinary polling is slower
// than reapPollInterval. It returns 0 when polling already runs at least that
// often, in which case every poll doubles as a reap pass.
func (w *Engine) reapPollTick() time.Duration {
	interval := reapPollInterval
	if w.reapInterval > 0 {
		interval = w.reapInterval
	}
	if w.periodicPollInterval() <= interval {
		return 0
	}
	return interval
}

// pausedWatchEvery is how often every paused record is checked for a wake:
// pausedWatchInterval, or the main poll interval when that is slower.
func (w *Engine) pausedWatchEvery() time.Duration {
	interval := pausedWatchInterval
	if w.pausedInterval > 0 {
		interval = w.pausedInterval
	}
	if w.Interval > interval {
		return w.Interval
	}
	return interval
}

// pausedWatchTick is how often a paused watch runs on its own when ordinary
// polling is slower than pausedWatchEvery (webhook push mode). It returns 0
// when polling already runs at least that often, in which case the poll that
// finds the watch due runs it.
func (w *Engine) pausedWatchTick() time.Duration {
	if w.periodicPollInterval() <= w.pausedWatchEvery() {
		return 0
	}
	return w.pausedWatchEvery()
}

func (w *Engine) staleClaimAfter() time.Duration {
	if w.staleClaim > 0 {
		return w.staleClaim
	}
	return staleClaimDuration
}

func (w *Engine) activeWorkClosureInterval() time.Duration {
	if w.closureInterval > 0 {
		return w.closureInterval
	}
	return workClosureInterval
}

// negotiatedNudges is the channel a settled background handshake signals the
// Run loop on. It is buffered so a handshake never blocks on a loop that is
// busy, and depth one because the nudge only asks for one more poll however
// many handshakes settled.
func (w *Engine) negotiatedNudges() chan struct{} {
	w.negotiatedOnce.Do(func() { w.negotiated = make(chan struct{}, 1) })
	return w.negotiated
}

// nudgePoll asks the Run loop to poll again, dropping the nudge when one is
// already queued.
func (w *Engine) nudgePoll() {
	select {
	case w.negotiatedNudges() <- struct{}{}:
	default:
	}
}

// beginNegotiation reserves the background ownership handshake for key,
// reporting false when one is already in flight. Polling continues while a
// handshake waits out its grace window, so every poll in that window sees the
// same issue as contested again; without this guard each one would post
// another "Does anyone have this?" on the same ticket.
func (w *Engine) beginNegotiation(key string) bool {
	w.negotiateMu.Lock()
	defer w.negotiateMu.Unlock()
	if w.negotiating[key] {
		return false
	}
	if w.negotiating == nil {
		w.negotiating = make(map[string]bool)
	}
	w.negotiating[key] = true
	return true
}

// endNegotiation releases the reservation taken by beginNegotiation.
func (w *Engine) endNegotiation(key string) {
	w.negotiateMu.Lock()
	defer w.negotiateMu.Unlock()
	delete(w.negotiating, key)
}

// negotiationInFlight reports whether a background handshake for key is still
// running, so the reap can leave it alone rather than re-announcing it.
func (w *Engine) negotiationInFlight(key string) bool {
	w.negotiateMu.Lock()
	defer w.negotiateMu.Unlock()
	return w.negotiating[key]
}

// awaitNegotiations blocks until every background handshake this instance
// started has finished.
func (w *Engine) awaitNegotiations() {
	w.negotiateWG.Wait()
}

type pendingIssue struct {
	issue   Issue
	session AgentSession
	// contested marks a dispatch candidate that this instance has no local
	// record of handling as its own resumed work (a reclaim of another
	// instance's apparent work, or a stale local record), so its ownership
	// must be negotiated through the comment protocol before dispatch.
	contested bool
	// mentioned marks a candidate dispatched because the ticket's newest
	// comment is a direct @/aviary:ID mention of this instance. That is an
	// explicit instruction addressed to this instance, so it overrides the
	// claims standing on the ticket rather than being filtered by them.
	mentioned bool
	// woken marks paused work whose wake condition fired. It
	// goes back through the claim protocol even when its session resumes, so
	// when several instances see the same wake only the first to claim it
	// takes it.
	woken bool
	// claim names the target this instance holds an ownership claim on,
	// posted by the handoff handshake before the dispatch it announces has
	// happened. A dispatch that is skipped after that point must withdraw the
	// claim rather than leave the ticket owned by an idle instance (issue
	// #434).
	claim *ownershipTarget
}

// releasePendingClaim withdraws the ownership claim a won handshake already
// posted for a candidate this instance turns out not to dispatch. Without it
// the ticket stays claimed by an instance with no work record and no running
// agent: other instances read the claim and stand down for work nobody is
// doing. Candidates that hold no claim of this instance's own
// have nothing to withdraw.
func (w *Engine) releasePendingClaim(ctx context.Context, pending pendingIssue, reason string) {
	if pending.claim == nil {
		return
	}
	w.releaseOwnership(ctx, *pending.claim, reason)
}

// standDownForStandingClaims filters the candidates this poll believes are
// uncontested against the handoff claims already standing on their tickets.
//
// Whether an issue is contested is decided from this instance's own records
// and the project board alone: an issue nobody here has a record of, whose
// card is not at "In Progress", reads as untouched work. The claims other
// instances post on the ticket itself are not part of that decision, so an
// issue another instance announced "Starting work on this issue" on minutes
// ago was dispatched as a first-time pickup, and both instances worked the
// same ticket and opened their own pull request for it. The
// contested path already reads those claims before reaping; this is the same
// read for the path that skips negotiation entirely.
//
// A candidate claimed by another instance within staleClaimDuration is
// dropped from the batch and left marked as seen. An older foreign claim is
// routed through the ownership handshake instead of being dispatched outright:
// it asks the other instance whether it is still working before taking over.
// Work this instance holds the newest claim on is its own and passes through.
// A comment read that fails leaves the candidate alone rather than stalling
// dispatch on an unreachable API.
func (w *Engine) standDownForStandingClaims(ctx context.Context, checker WorkClosureChecker, newIssues []pendingIssue, seen map[string]bool) []pendingIssue {
	if w.Comments == nil || len(newIssues) == 0 {
		return newIssues
	}
	keep := make([]bool, len(newIssues))
	var wg sync.WaitGroup
	for i, pending := range newIssues {
		// Resumed sessions are this instance's own work, contested candidates
		// are negotiated by the reap that follows, and a direct mention is an
		// instruction addressed to this instance by name.
		if (pending.session.Resume && !pending.woken) || pending.contested || pending.mentioned {
			keep[i] = true
			continue
		}
		wg.Add(1)
		go func(i int, issue Issue) {
			defer wg.Done()
			target := ownershipTargetFor(ctx, checker, issue)
			standing, err := w.claimStanding(ctx, target)
			if err != nil {
				w.logf("issue #%d claim check failed; picking it up as uncontested: %v", issue.Number, err)
				keep[i] = true
				return
			}
			if standing.SelfHolds || !standing.OwnerClaimed {
				keep[i] = true
				return
			}
			if !standing.OwnerFresh {
				newIssues[i].contested = true
				keep[i] = true
				w.logf("issue #%d was claimed by instance %s %s ago (older than %s); asking before pickup on %s", issue.Number, standing.Owner, standing.OwnerAge.Round(time.Second), w.staleClaimAfter(), target.describe())
				return
			}
			w.logf("issue #%d not picked up as uncontested; instance %s claimed it %s ago (within %s) on %s", issue.Number, standing.Owner, standing.OwnerAge.Round(time.Second), w.staleClaimAfter(), target.describe())
		}(i, pending.issue)
	}
	wg.Wait()
	filtered := newIssues[:0]
	for i, pending := range newIssues {
		if keep[i] {
			filtered = append(filtered, pending)
			continue
		}
		// Keeping the seen marker is what makes the next poll read this issue
		// as contested and negotiate for it, rather than rediscovering it as
		// brand new work and dispatching it outright.
		seen[issueKey(pending.issue)] = true
	}
	return filtered
}

// negotiateContestedIssues runs the handoff handshake for every candidate
// issue marked contested (no local record of being this instance's own
// resumed work). Uncontested issues pass through untouched.
//
// The handshake's grace window lasts minutes, so it runs in the background
// and its issue leaves this batch: an issue being negotiated, one whose
// negotiation loses, and one whose negotiation errors are all dropped but
// stay marked as seen, so a later poll retries them as contested work rather
// than dispatching them as if nothing had ever claimed them. A won handshake
// leaves this instance's own claim on the ticket, which is what the next poll
// reads to dispatch the work.
//
// The first reap after startup is aggressive: every contested candidate no
// other instance holds a live claim on is asked about immediately, rather
// than waiting for the periodic timer. Later reaps additionally
// skip anything this instance already claimed or already negotiated, so they
// do not re-post "does anyone have this?" on every pass. Both reaps stand
// down for a foreign claim newer than staleClaimDuration: the handshake only
// reads answers posted after its own ask, and a live instance that announced
// itself minutes ago does not re-announce during the grace window, so asking
// unconditionally on startup took work another instance was running (issue
// #514).
func (w *Engine) negotiateContestedIssues(ctx context.Context, checker WorkClosureChecker, newIssues []pendingIssue, seen map[string]bool, aggressive bool) []pendingIssue {
	if w.Comments == nil || len(newIssues) == 0 {
		return newIssues
	}
	keep := make([]bool, len(newIssues))
	contested := make([]Issue, 0, len(newIssues))
	for _, pending := range newIssues {
		if pending.session.Resume || !pending.contested {
			continue
		}
		// A handshake already waiting out its grace window in the background
		// is this reap's answer for that issue; announcing it again on every
		// poll of that window would only repeat itself.
		if w.negotiationInFlight(issueKey(pending.issue)) {
			continue
		}
		contested = append(contested, pending.issue)
	}
	if len(contested) > 0 {
		mode := "periodic reap, skipping anything claimed within " + w.staleClaimAfter().String()
		if aggressive {
			mode = "first reap after startup, asking about anything not freshly claimed"
		}
		w.logf("reaping %d contested issue(s) as %s (%s): %s", len(contested), w.Identity, mode, issueNumbers(contested))
	}
	var wg sync.WaitGroup
	for i, pending := range newIssues {
		if pending.session.Resume || !pending.contested {
			keep[i] = true
			continue
		}
		if w.negotiationInFlight(issueKey(pending.issue)) {
			continue
		}
		wg.Add(1)
		go func(i int, issue Issue) {
			defer wg.Done()
			target := ownershipTargetFor(ctx, checker, issue)
			reason := "it reappeared with no local record of this instance owning it"
			if projectItemInProgress(issue.Target, issue) {
				reason = "it sits at In Progress with no local record of this instance owning it"
			}
			w.logf("issue #%d looks claimed: %s; negotiating on %s", issue.Number, reason, target.describe())
			// Both reaps read the claims standing on the ticket before they
			// ask anything. The first reap after startup used to skip
			// straight to the handshake, and the handshake only reads answers
			// posted after its own ask: an instance that announced itself
			// minutes earlier does not re-announce during the grace window,
			// so nothing answered and a freshly started instance claimed and
			// dispatched work another instance was actively running (issue
			// #514). What the aggressive reap skips is the guards below that
			// exist to stop it re-asking on every periodic pass, not the
			// stand-down for a live claim.
			standing, err := w.claimStanding(ctx, target)
			if err != nil {
				if !aggressive {
					w.logf("issue #%d reap check failed: %v", issue.Number, err)
					return
				}
				// The first reap is the only pass that runs before the
				// periodic timer, so an unreachable comment read negotiates
				// rather than leaving genuinely abandoned work waiting for
				// the next one. The handshake it runs is itself an ask that
				// a live instance can answer.
				w.logf("issue #%d reap check failed; negotiating anyway on the first reap after startup: %v", issue.Number, err)
			} else {
				// Another instance holds the newest claim and posted it
				// recently enough to still be running the work, so this reap
				// stands down whether or not it is the first one.
				if !standing.SelfHolds && standing.OwnerFresh {
					w.logf("issue #%d claimed by instance %s %s ago (within %s); skipping reap", issue.Number, standing.Owner, standing.OwnerAge.Round(time.Second), w.staleClaimAfter())
					return
				}
				if !aggressive {
					// This instance already holds the newest claim, so the
					// handshake it would run has already been run and won.
					// Asking again would spam the ticket with an ask/claim
					// pair on every pass and stall each one for the grace
					// period.
					if standing.SelfHolds {
						w.logf("issue #%d already claimed by this instance %s ago; dispatching without re-asking", issue.Number, standing.SelfAge.Round(time.Second))
						keep[i] = true
						newIssues[i].claim = &target
						return
					}
					// The checks above are all derived from the ticket's own
					// comments. This one is local: a negotiation this instance
					// already ran on this target settled who owns the work, and
					// re-running it would repost the same ask and the same claim
					// on every pass, stalling each one for the grace window. That
					// is the comment spam of issue #432, so the decision is reused
					// until the record goes stale even when the comments read as
					// if nothing had ever been negotiated.
					if record, age, ok := w.settledHandshake(target); ok {
						if record.Claimed {
							w.logf("issue #%d handshake already settled %s ago in this instance's favour; dispatching without re-asking", issue.Number, age.Round(time.Second))
							keep[i] = true
							newIssues[i].claim = &target
						} else {
							w.logf("issue #%d handshake already settled %s ago for another instance; standing down without re-asking", issue.Number, age.Round(time.Second))
						}
						return
					}
				}
				if !standing.OwnerClaimed {
					w.logf("issue #%d has no claim from another instance; treating it as abandoned", issue.Number)
				} else {
					w.logf("issue #%d last claimed by instance %s %s ago (older than %s); treating it as abandoned", issue.Number, standing.Owner, standing.OwnerAge.Round(time.Second), w.staleClaimAfter())
				}
			}
			// The handshake waits out a grace window measured in minutes.
			// Running it here would stop the poll loop for that whole window,
			// so nothing else is read or dispatched meanwhile.
			// It is handed to a goroutine instead, and the poll returns
			// without this issue: once the handshake settles, the next poll
			// reads its own claim (or its handshake record) and dispatches
			// without asking again.
			key := issueKey(issue)
			if !w.beginNegotiation(key) {
				return
			}
			w.negotiateWG.Add(1)
			w.logf("issue #%d negotiating ownership in the background; polling continues while it waits", issue.Number)
			go func() {
				defer w.negotiateWG.Done()
				claimed, err := w.negotiateOwnership(ctx, target)
				if err != nil {
					w.endNegotiation(key)
					w.logf("issue #%d ownership handoff failed: %v", issue.Number, err)
					return
				}
				w.recordHandshake(target, claimed)
				// The negotiation ends after its outcome is recorded, so no
				// poll sees neither, but before the nudge goes out rather than
				// in a deferred call after it. The nudged poll is the only one
				// before the next tick, and while the key still reads as in
				// flight that poll drops the issue as still being negotiated,
				// leaving won work waiting out the whole interval.
				w.endNegotiation(key)
				if claimed {
					w.logf("issue #%d picked up after handoff; dispatching on the next poll", issue.Number)
					w.nudgePoll()
				} else {
					w.logf("issue #%d ownership claimed by another instance; standing down", issue.Number)
				}
			}()
		}(i, pending.issue)
	}
	wg.Wait()
	filtered := newIssues[:0]
	for i, pending := range newIssues {
		if keep[i] {
			filtered = append(filtered, pending)
		} else {
			// Keep the seen marker: it is what makes the next poll treat this
			// issue as contested again. Clearing it would make the issue look
			// brand new, and the next poll would dispatch it outright, taking
			// work from the instance this one just stood down for.
			seen[issueKey(pending.issue)] = true
		}
	}
	return filtered
}

// pollDiscussions dispatches the gh-discuss skill for newly discovered
// unanswered top-level Discussion threads on any Discussions-board targets.
// Discussion dispatch is intentionally isolated from issue dispatch: it does
// not use the persisted work state, project status, or comment-based
// handoff protocol, since GitHub's own reply count on a discussion already
// tells the next poll whether it still needs an answer. active only guards
// against dispatching the same discussion twice while an agent run for it
// is still in flight.
func (w *Engine) pollDiscussions(ctx context.Context, n int, targets []string, sem *concurrencySemaphore, wg *sync.WaitGroup, workMu *sync.Mutex, active map[string]string) {
	if w.Discussions == nil {
		return
	}
	// Snapshot the in-flight counts before this poll claims anything, so the
	// rotation below is led by work that is genuinely still running rather
	// than by the candidates this poll is about to queue.
	workMu.Lock()
	counts := activeCountsByTarget(active, parseDiscussionWorkKey)
	workMu.Unlock()
	var pending []pendingIssue
	for _, target := range targets {
		if !isDiscussionTarget(target) {
			continue
		}
		discussions, err := w.Discussions.ListUnansweredDiscussions(ctx, target)
		if err != nil {
			w.logChanged("discussions:"+target, err.Error(), "poll #%d failed while listing discussions for %s: %v", n, target, err)
			continue
		}
		for _, discussion := range discussions {
			key := discussionWorkKey(target, discussion.Number)
			workMu.Lock()
			_, inFlight := active[key]
			if !inFlight {
				active[key] = "discussion"
			}
			workMu.Unlock()
			if inFlight {
				continue
			}
			issue := Issue{Number: discussion.Number, Title: discussion.Title, Body: discussion.Body, CreatedAt: discussion.CreatedAt, Target: target, Repository: issueRepository(target, Issue{})}
			pending = append(pending, pendingIssue{issue: issue})
		}
	}
	if len(pending) == 0 {
		return
	}
	// Every target's candidates are collected before any of them blocks on a
	// slot, so one Discussions target's backlog cannot take every slot ahead
	// of another target's threads.
	pending = balanceAcrossTargets(pending, counts)
	for index, candidate := range pending {
		issue := candidate.issue
		if err := w.acquireSlot(ctx, sem); err != nil {
			// Hand back this candidate's claim and every one still behind it;
			// they were never dispatched, so a later poll must see them as
			// unanswered again.
			workMu.Lock()
			for _, remaining := range pending[index:] {
				delete(active, discussionWorkKey(remaining.issue.Target, remaining.issue.Number))
			}
			workMu.Unlock()
			return
		}
		w.logf("discussion #%d queued", issue.Number)
		wg.Add(1)
		go func(i Issue, key string) {
			defer wg.Done()
			defer sem.release()
			defer func() {
				workMu.Lock()
				delete(active, key)
				workMu.Unlock()
			}()
			w.logf("discussion #%d started", i.Number)
			if err := w.runner().Run(ctx, i); err != nil {
				w.logf("discussion #%d failed: %v", i.Number, err)
				return
			}
			w.logf("discussion #%d completed", i.Number)
		}(issue, discussionWorkKey(issue.Target, issue.Number))
	}
}

// issueWatch describes the run watchForIssueUpdates is watching for, so a
// resumed run does not re-deliver what interrupted its predecessor.
type issueWatch struct {
	// since is when the run started; only comments posted after it are news
	// to the agent.
	since time.Time
	// resumable reports whether this run can be interrupted and resumed in
	// place. It is a function rather than a flag because an agent that names
	// its own session only reports it after launch, so a run can become
	// resumable partway through. A run that is not resumable keeps the
	// historical behaviour: a closure stops it outright rather than asking it
	// to clean up in a session that cannot be reopened, and an edit or a
	// comment leaves it alone entirely.
	resumable func() bool
	// closed reports that the issue was already closed when this run began,
	// which is true of the cleanup run a closure itself started. Without it
	// that run would be interrupted by the very closure it was resumed to
	// handle.
	closed bool
	// nudge asks for an immediate check instead of waiting out the tick, so a
	// webhook delivery reaches the running agent as promptly in push mode as
	// the poll notices it in poll mode.
	nudge <-chan struct{}
	// observed, when set, is handed every state read back for the issue, so
	// the dashboards can show the pull request the run is on.
	observed func(OriginatingWorkState)
}

// canRelay reports whether this run can be resumed in place right now.
func (watch issueWatch) canRelay() bool {
	return watch.resumable != nil && watch.resumable()
}

// watchForIssueUpdates polls the issue an agent is working and interrupts the
// run when GitHub says something about it changed. A run that can
// be resumed is interrupted with a workUpdate the run loop replays into the
// same session; one that cannot keeps the original behaviour of stopping on a
// closure. Closure and description changes are read from the closure checker,
// new comments from the comment client, so browser mode watches the rendered
// pages the rest of the run already reads and poll mode watches the API.
func (w *Engine) watchForIssueUpdates(ctx context.Context, checker WorkClosureChecker, issue Issue, watch issueWatch, cancel context.CancelCauseFunc, ready chan<- struct{}) {
	repo := issueRepository(issue.Target, issue)
	var previous OriginatingWorkState
	var err error
	if checker != nil {
		previous, err = checker.OriginatingWorkState(ctx, repo, issue.Number)
		if err != nil && ctx.Err() == nil {
			w.logf("issue #%d initial closure check failed: %v", issue.Number, err)
		}
		if err == nil && watch.observed != nil {
			watch.observed(previous)
		}
	}
	seen := w.commentsSeen(ctx, repo, issue.Number)
	close(ready)
	if reason := closedWorkReason(OriginatingWorkState{}, previous, issue.Number); checker != nil && err == nil && !watch.closed && strings.EqualFold(previous.IssueState, "closed") && reason != "" {
		cause := fmt.Errorf("%w: %s", errWorkClosedByUser, reason)
		w.logf("issue #%d stopping agent: %s", issue.Number, reason)
		cancel(cause)
		return
	}
	ticker := time.NewTicker(w.activeWorkClosureInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-watch.nudge:
		case <-ticker.C:
		}
		if checker != nil {
			current, checkErr := checker.OriginatingWorkState(ctx, repo, issue.Number)
			if checkErr != nil {
				if ctx.Err() == nil {
					w.logf("issue #%d closure check failed: %v", issue.Number, checkErr)
				}
			} else {
				if watch.observed != nil {
					watch.observed(current)
				}
				if reason := closedWorkReason(previous, current, issue.Number); reason != "" {
					if !watch.canRelay() {
						cause := fmt.Errorf("%w: %s", errWorkClosedByUser, reason)
						w.logf("issue #%d stopping agent: %s", issue.Number, reason)
						cancel(cause)
						return
					}
					w.jobSystem(issueKey(issue), reason+"; relayed into the session")
					w.logf("issue #%d interrupting agent: %s; asking it to clean up in the same session", issue.Number, reason)
					cancel(&workUpdate{summary: reason, closed: true, instruction: closedWorkCleanupPrompt(issue, reason)})
					return
				}
				if number, reason := rejectedPullRequest(previous, current); reason != "" {
					w.jobSystem(issueKey(issue), "restarted: "+reason)
					w.logf("issue #%d restarting agent: %s while the issue is still open; starting a fresh attempt", issue.Number, reason)
					cancel(&workUpdate{summary: reason, restart: true, instruction: rejectedPullRequestPrompt(issue, number)})
					return
				}
				if watch.canRelay() && previous.IssueBody != "" && current.IssueBody != previous.IssueBody {
					w.jobSystem(issueKey(issue), "issue description edited; relayed into the session")
					w.logf("issue #%d interrupting agent: its description changed; relaying it into the same session", issue.Number)
					cancel(&workUpdate{summary: fmt.Sprintf("issue #%d description changed", issue.Number), instruction: changedDescriptionPrompt(issue)})
					return
				}
				previous = current
			}
		}
		if w.Comments == nil || !watch.canRelay() {
			continue
		}
		comments, listErr := w.Comments.ListComments(ctx, repo, issue.Number)
		if listErr != nil {
			if ctx.Err() == nil {
				w.logf("issue #%d comment check failed: %v", issue.Number, listErr)
			}
			continue
		}
		// Only allowed commenters may steer a running agent:
		// anyone else's comment is marked seen and logged once, never relayed.
		added := 0
		var relayed []transcript.Event
		for _, comment := range comments {
			key := commentKey(comment)
			if seen[key] || !relayableComment(comment, watch.since) {
				continue
			}
			if !commenterAllowed(comment.Author, w.AllowedCommenters) {
				seen[key] = true
				w.logf("issue #%d ignoring new comment: %s is not an allowed commenter", issue.Number, comment.Author)
				continue
			}
			added++
			relayed = append(relayed, transcript.Event{Kind: transcript.KindCommentRelay, Author: comment.Author, Text: comment.Body})
		}
		if mentioned := w.pullRequestMentions(ctx, repo, issue, previous.PullRequests, watch.since, seen); len(mentioned) > 0 {
			w.jobEvents(issueKey(issue), relayed...)
			w.jobSystem(issueKey(issue), "mentioned on pull request "+formatIssueRefs(mentioned)+"; relayed into the session")
			w.logf("issue #%d interrupting agent: instance %s was mentioned on pull request %s; relaying it into the same session", issue.Number, w.Identity, formatIssueRefs(mentioned))
			cancel(&workUpdate{summary: fmt.Sprintf("issue #%d was mentioned on pull request %s", issue.Number, formatIssueRefs(mentioned)), instruction: pullRequestMentionPrompt(issue, mentioned, added)})
			return
		}
		if added == 0 {
			continue
		}
		w.jobEvents(issueKey(issue), relayed...)
		w.logf("issue #%d interrupting agent: %d new comment(s); relaying them into the same session", issue.Number, added)
		cancel(&workUpdate{summary: fmt.Sprintf("issue #%d has %d new comment(s)", issue.Number, added), instruction: newCommentsPrompt(issue, added)})
		return
	}
}

// commentsSeen snapshots the conversation a run starts from, so only what is
// posted after it counts as news for that run.
func (w *Engine) commentsSeen(ctx context.Context, repo string, number int) map[string]bool {
	seen := map[string]bool{}
	if w.Comments == nil {
		return seen
	}
	comments, err := w.Comments.ListComments(ctx, repo, number)
	if err != nil {
		if ctx.Err() == nil {
			w.logf("issue #%d initial comment check failed: %v", number, err)
		}
		return seen
	}
	for _, comment := range comments {
		seen[commentKey(comment)] = true
	}
	return seen
}

// pullRequestMentions lists the pull requests linked to the issue a run is
// working that gained a direct mention of this instance since the run began
// . The run's own watcher only reads the issue's conversation, so
// without this a mention posted on the pull request the agent opened got its
// eyes reaction and was never heard by the agent. Only mentions from allowed
// commenters count: the agent itself posts on its pull request, and relaying
// that would restart it on its own chatter. Review comments and review
// bodies on those pull requests are read as well. Every comment
// read is added to seen, so one mention is relayed, or logged as skipped,
// once.
func (w *Engine) pullRequestMentions(ctx context.Context, repo string, issue Issue, pullRequests []PullRequestWorkState, since time.Time, seen map[string]bool) []int {
	if w.Identity == "" {
		return nil
	}
	var mentioned []int
	for _, pullRequest := range pullRequests {
		if pullRequest.Number <= 0 || (pullRequest.Number == issue.Number && pullRequest.InRepo(repo)) {
			continue
		}
		pullRepo := pullRequest.RepoOr(repo)
		comments, err := w.Comments.ListComments(ctx, pullRepo, pullRequest.Number)
		if err != nil {
			if ctx.Err() == nil {
				w.logf("issue #%d pull request #%d comment check failed: %v", issue.Number, pullRequest.Number, err)
			}
			continue
		}
		if lister, ok := w.Comments.(ReviewCommentLister); ok {
			reviews, err := lister.ListReviewComments(ctx, pullRepo, pullRequest.Number)
			if err != nil {
				if ctx.Err() == nil {
					w.logf("issue #%d pull request #%d review check failed: %v", issue.Number, pullRequest.Number, err)
				}
			} else {
				comments = append(comments, reviews...)
			}
		}
		found := false
		for _, comment := range comments {
			key := commentKey(comment)
			if seen[key] || !relayableComment(comment, since) {
				continue
			}
			seen[key] = true
			if !mentionedIdentity(comment.Body, w.Identity) {
				found = found || w.commentRequestsRecheck(comment.Body, comment.Author)
				continue
			}
			if !commenterAllowed(comment.Author, w.AllowedCommenters) {
				w.logf("issue #%d ignoring direct mention of instance %s on pull request #%d: %s is not an allowed commenter", issue.Number, w.Identity, pullRequest.Number, comment.Author)
				continue
			}
			found = true
		}
		if found {
			mentioned = append(mentioned, pullRequest.Number)
		}
	}
	return mentioned
}

// pullRequestMentionPrompt tells an agent that a human mentioned this instance
// on the pull request it is working, so the comment is acted on rather than
// only acknowledged.
func pullRequestMentionPrompt(issue Issue, pullRequests []int, issueComments int) string {
	noun := "pull request"
	if len(pullRequests) > 1 {
		noun = "pull requests"
	}
	prompt := fmt.Sprintf("A comment addressed to you was posted on %s %s for issue #%d while you were working on it.\n\nReread the %s conversation, reviews, and inline review comments from GitHub and treat that comment as an instruction for this work. Act on it: change the implementation and push new commits to the pull request when it asks for a change, reply on the pull request when it asks a question, or both, before continuing the rest of the work.", noun, formatIssueRefs(pullRequests), issue.Number, noun)
	if issueComments > 0 {
		prompt += fmt.Sprintf(" Issue #%d also has %d new comment(s); reread its conversation too.", issue.Number, issueComments)
	}
	return prompt
}

func commentKey(comment Comment) string {
	return comment.CreatedAt.UTC().Format(time.RFC3339Nano) + "\x00" + comment.Author + "\x00" + comment.Body
}

// relayableComment reports whether a comment is worth interrupting an agent
// for. The handoff handshake posts claims on the same
// conversation, and every instance watching the issue reads them, so relaying
// those would restart the agent on aviary's own bookkeeping.
func relayableComment(comment Comment, since time.Time) bool {
	if _, _, ok := parseClaim(comment.Body); ok {
		return false
	}
	return !comment.CreatedAt.IsZero() && comment.CreatedAt.After(since)
}

func closedWorkCleanupPrompt(issue Issue, reason string) string {
	return fmt.Sprintf("Stop working on issue #%d: %s.\n\nDo not implement it any further, do not merge any pull request for it, and do not reopen it. Clean up what this session already started instead: close the pull request you opened for it if it is still open, delete the branch and the isolated clone if they are no longer needed, and report what you cleaned up.", issue.Number, reason)
}

// rejectedPullRequestPrompt tells an agent whose pull request a human closed
// without merging, while the issue stayed open, that the attempt was rejected
// and the work starts over. The closed pull request is never
// reopened or reused, matching gh-fix, which only resumes open pull requests.
func rejectedPullRequestPrompt(issue Issue, pullRequest int) string {
	return fmt.Sprintf("Pull request #%d for issue #%d was closed without merging while the issue is still open. That attempt was rejected.\n\nDo not merge, reopen, or reuse pull request #%d or its branch. Read the issue and the closed pull request's conversation from GitHub for why it was rejected, then start a fresh implementation: a new branch from the current default branch, a new draft pull request that closes issue #%d, and the rest of the gh-fix workflow through merge.", pullRequest, issue.Number, pullRequest, issue.Number)
}

func changedDescriptionPrompt(issue Issue) string {
	return fmt.Sprintf("The description of issue #%d changed while you were working on it.\n\nReread the issue from GitHub, then reconcile the work in progress with what it now asks for before continuing. Say what changed and what you adjusted.", issue.Number)
}

func newCommentsPrompt(issue Issue, added int) string {
	noun := "comment was"
	if added > 1 {
		noun = "comments were"
	}
	return fmt.Sprintf("%d new %s added to issue #%d while you were working on it.\n\nReread the issue's conversation from GitHub, treat the new comments as part of the request, and take them into account before continuing.", added, noun, issue.Number)
}

// unclosedWorkPrompt asks an agent whose run ended with the issue still open to
// carry the work the rest of the way. Finishing a prompt is not
// finishing the ticket: an agent that timed out, stopped early, or reported
// success without merging exits exactly as one that merged does, so the
// resumed session is told what is still missing rather than a bare "continue".
func unclosedWorkPrompt(issue Issue) string {
	return fmt.Sprintf("Your run for issue #%d ended, but GitHub still reports the issue as open, so the work is not finished.\n\nPick the work back up where it stopped: check the issue and its pull request on GitHub, finish whatever remains (CI failures, review, merge), and do not stop until the issue is closed. If it genuinely cannot be closed, say exactly what is blocking it.", issue.Number)
}

// confirmIssueClosed asks GitHub whether the issue an agent just finished is
// actually done. That is either the issue itself being closed, or a pull
// request linked to it sitting open, unmerged, and past draft: a run that
// deliberately withholds its merge for a human (a DONOTMERGE directive)
// finishes in exactly that shape on purpose, and it must not be mistaken for
// one that stopped early and kept alive forever waiting for a hold nothing
// here can lift. It reports whether the issue is done and
// whether GitHub answered at all, so a check that could not be made is never
// mistaken for an issue that is still open. A merge GitHub has not finished
// processing is allowed for by re-reading once after the closure interval
// before the work is treated as unfinished. The last state read is returned
// too, so the keepalive loop can tell whether the turn made any progress. When
// the run is done because its
// ready pull request is held rather than because the issue closed, it also
// returns the paused record for that pull request: stacked when
// the pull request is built on a base other than the default branch, so it can
// only merge after its blocker does, and otherwise held for a
// human to merge or close.
func (w *Engine) confirmIssueClosed(ctx context.Context, checker WorkClosureChecker, issue Issue) (closed, answered bool, held *core.PausedWork, last OriginatingWorkState) {
	repo := issueRepository(issue.Target, issue)
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return false, false, nil, last
			case <-time.After(w.activeWorkClosureInterval()):
			}
		}
		state, err := checker.OriginatingWorkState(ctx, repo, issue.Number)
		if err != nil {
			if ctx.Err() != nil {
				return false, false, nil, last
			}
			w.logf("issue #%d completion check failed: %v", issue.Number, err)
			continue
		}
		answered, last = true, state
		if strings.EqualFold(state.IssueState, "closed") {
			return true, true, nil, last
		}
		if pullRequestHeldForReview(state) {
			held = &core.PausedWork{Kind: core.PausedPullRequest, PullRequests: core.OpenPullRequests(state)}
			if pullRequestPausedOnStack(state) {
				held.Kind = core.PausedStacked
			}
			return true, true, held, last
		}
	}
	return false, answered, nil, last
}

// pullRequestHeldForReview reports whether state carries a pull request that
// is open, unmerged, and no longer a draft. That is what a gh-fix run leaves
// behind when it marks its fix ready but withholds the merge for a human, and
// it is otherwise indistinguishable from work still in flight, so the
// keepalive loop needs this to tell the two apart.
func pullRequestHeldForReview(state OriginatingWorkState) bool {
	for _, pullRequest := range state.PullRequests {
		if strings.EqualFold(pullRequest.State, "open") && !pullRequest.Merged && !pullRequest.IsDraft {
			return true
		}
	}
	return false
}

// pullRequestPausedOnStack reports whether state carries a ready pull request
// that is stacked on another branch. Such a pull request is not held for a
// human: it is waiting for its blocker to merge so it can be retargeted onto
// the default branch and merged, which a later run has to come back and do
// .
func pullRequestPausedOnStack(state OriginatingWorkState) bool {
	for _, pullRequest := range state.PullRequests {
		if strings.EqualFold(pullRequest.State, "open") && !pullRequest.Merged && !pullRequest.IsDraft && pullRequest.Stacked {
			return true
		}
	}
	return false
}

// pausedWorkPrompt is the update a paused stacked run is resumed with once its
// blocker is no longer open, so the agent picks the waiting pull request back
// up instead of starting the fix over.
func pausedWorkPrompt(issue Issue) string {
	return fmt.Sprintf("Your pull request for issue #%d was paused, stacked on its blocker's branch and waiting for that blocker to merge. The blocker is no longer open.\n\nPick the work back up: rebase or retarget the pull request onto the default branch as the gh-fix stacking steps describe (or unstack it if the blocker's pull request closed without merging), drive CI to green on the new head, and merge it so issue #%d closes.", issue.Number, issue.Number)
}

// pausedRecord returns what paused work waits on. A record saved before paused
// work had kinds is a stacked pull request waiting on its blocker.
func pausedRecord(state workState) core.PausedWork {
	if state.Paused == nil || state.Paused.Kind == "" {
		return core.PausedWork{Kind: core.PausedStacked}
	}
	return *state.Paused
}

// pausedWake reads the GitHub state that paused work's wake check needs and
// decides it. An issue that only waits on its blockers is decided
// from the issue as the poll listed it. Work that left a pull request behind
// also reads that pull request back, and a stacked one still blocked reads its
// blockers' pull requests too. It also returns the rejected pull request when
// the work restarts, and the work's own state when it was read. A read that fails is left out, so it never wakes work.
func (w *Engine) pausedWake(ctx context.Context, checker WorkClosureChecker, paused core.PausedWork, issue Issue) (core.PausedWake, string, int, *OriginatingWorkState) {
	check := core.PausedCheck{Issue: issue}
	if checker != nil && paused.Kind != core.PausedBlocked && paused.Kind != core.PausedManual {
		repo := issueRepository(issue.Target, issue)
		state, err := checker.OriginatingWorkState(ctx, repo, issue.Number)
		if err == nil {
			check.Own = &state
		} else if ctx.Err() == nil {
			w.logChanged("paused-check-"+issueKey(issue), err.Error(), "issue #%d paused-work check failed: %v", issue.Number, err)
		}
		if blocked, _ := issueBlocked(issue); blocked && paused.Kind == core.PausedStacked {
			check.Blockers = make(map[int]OriginatingWorkState)
			for _, number := range core.OpenDependencies(issue) {
				if blocker, err := checker.OriginatingWorkState(ctx, repo, number); err == nil {
					check.Blockers[number] = blocker
				}
			}
		}
	}
	wake, reason := core.WakePaused(paused, check)
	rejected := 0
	if wake == core.PausedRestart && check.Own != nil {
		rejected, _ = core.RejectedPullRequest(*check.Own)
	}
	return wake, reason, rejected, check.Own
}

// IssueHydrator reads one issue back from GitHub with its state and
// dependencies, so paused work whose issue the poll does not list can still
// be woken when its blockers close.
type IssueHydrator interface {
	HydrateIssue(ctx context.Context, repo string, issue *Issue) error
}

// unlistedPausedIssues reads back the paused work whose issue the poll did not
// list, so the paused watch can decide its wake exactly as it does for listed
// work. An issue that cannot be read is left out; the poll still
// finishes it if its own state shows it closed or merged.
func (w *Engine) unlistedPausedIssues(ctx context.Context, hydrator IssueHydrator, keys []string, targets []string) []Issue {
	var issues []Issue
	for _, key := range keys {
		target, number, ok := parseIssueWorkKey(key)
		if !ok || !slices.Contains(targets, target) || isDiscussionTarget(target) {
			continue
		}
		issue := Issue{Number: number, Target: target, Repository: issueRepository(target, Issue{})}
		if err := hydrator.HydrateIssue(ctx, issue.Repository, &issue); err != nil {
			if ctx.Err() == nil {
				w.logChanged("paused-watch-"+key, err.Error(), "issue #%d paused watch could not read the issue: %v", number, err)
			}
			continue
		}
		issues = append(issues, issue)
	}
	return issues
}

// pausedJobReason is what the dashboards say paused work waits on when it
// pauses at the end of a run.
func pausedJobReason(paused core.PausedWork) string {
	if paused.Kind == core.PausedManual {
		return paused.ManualReason()
	}
	if paused.Kind == core.PausedStacked {
		return "its stacked pull request waits on its blocker to merge"
	}
	return "its pull request waits to be merged or closed"
}

// completedWorkRejected reports a completed issue, still open, whose pull
// request was closed without merging after its run ended. A run
// only completes with its issue still open when it left a ready pull request
// for a human, so no open or merged pull request remaining means that one was
// rejected. A failed check reports nothing, leaving the issue suppressed until
// a later poll can read it.
func (w *Engine) completedWorkRejected(ctx context.Context, checker WorkClosureChecker, issue Issue) (int, string) {
	state, err := checker.OriginatingWorkState(ctx, issueRepository(issue.Target, issue), issue.Number)
	if err != nil {
		if ctx.Err() == nil {
			w.logChanged("rejected-check-"+issueKey(issue), err.Error(), "issue #%d completed-work check failed: %v", issue.Number, err)
		}
		return 0, ""
	}
	return completedPullRequestRejected(state)
}

// completedPullRequestRejected reports the pull request closed without merging
// that rejects completed work (core.RejectedPullRequest).
func completedPullRequestRejected(state OriginatingWorkState) (int, string) {
	return core.RejectedPullRequest(state)
}

// closedWorkReason reports why work must stop because its issue was closed
// without a merge. A pull request closed while the issue is still open is not
// a reason to stop; rejectedPullRequest reports it instead.
func closedWorkReason(previous, current OriginatingWorkState, issueNumber int) string {
	if strings.EqualFold(current.IssueState, "closed") && !strings.EqualFold(previous.IssueState, "closed") {
		for _, pullRequest := range current.PullRequests {
			if pullRequest.Merged {
				return ""
			}
		}
		return fmt.Sprintf("issue #%d was closed without a merge", issueNumber)
	}
	return ""
}

// rejectedPullRequest reports a pull request closed without merging since
// previous while the issue stayed open, which rejects that attempt and
// restarts the work rather than ending it. A closed issue always
// wins: work on it stops, so a pull request closed alongside it, or by the
// cleanup the closure asked for, never restarts it.
func rejectedPullRequest(previous, current OriginatingWorkState) (int, string) {
	if strings.EqualFold(current.IssueState, "closed") {
		return 0, ""
	}
	type pullRequestKey struct {
		repo   string
		number int
	}
	keyOf := func(pullRequest PullRequestWorkState) pullRequestKey {
		return pullRequestKey{strings.ToLower(pullRequest.Repository), pullRequest.Number}
	}
	previousPullRequests := make(map[pullRequestKey]PullRequestWorkState, len(previous.PullRequests))
	for _, pullRequest := range previous.PullRequests {
		previousPullRequests[keyOf(pullRequest)] = pullRequest
	}
	for _, pullRequest := range current.PullRequests {
		old, existed := previousPullRequests[keyOf(pullRequest)]
		if !pullRequest.Merged && strings.EqualFold(pullRequest.State, "closed") && (!existed || !strings.EqualFold(old.State, "closed")) {
			return pullRequest.Number, fmt.Sprintf("pull request #%d was closed without merging", pullRequest.Number)
		}
	}
	return 0, ""
}

// watchForCompetingClaim periodically polls comments on the negotiated
// ownership target (the issue, or an open pull request already linked to
// it) while an agent is actively working, looking for a newer
// starting/continuing claim signed by a different instance identity. Per
// the handoff protocol described in issue #214, the last instance to post
// such a claim wins, so detecting one here means this instance lost the
// ticket mid-run and must cooperatively cancel.
func (w *Engine) watchForCompetingClaim(ctx context.Context, target ownershipTarget, issueNumber int, since time.Time, cancel context.CancelCauseFunc) {
	if w.Comments == nil {
		return
	}
	ticker := time.NewTicker(w.activeWorkClosureInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			comments, err := w.Comments.ListComments(ctx, target.Repo, target.Number)
			if err != nil {
				if ctx.Err() == nil {
					w.logf("issue #%d competing claim check failed: %v", issueNumber, err)
				}
				continue
			}
			if owner, ok := claimedByOther(comments, since, w.Identity); ok {
				cause := fmt.Errorf("%w: issue #%d claimed by another instance", errWorkClaimedByOther, issueNumber)
				w.logf("issue #%d stopping agent: instance %s claimed it on %s; letting it go", issueNumber, owner, target.describe())
				cancel(cause)
				return
			}
		}
	}
}

type workState struct {
	Status            string `json:"status"`
	SessionID         string `json:"sessionId,omitempty"`
	Agent             string `json:"agent,omitempty"`
	CheckoutDirectory string `json:"checkoutDirectory,omitempty"`
	// Owner is the identity of the aviary instance that most recently claimed
	// this ticket through the handoff protocol, cached to help future reaps
	// recognize their own prior work.
	Owner string `json:"owner,omitempty"`
	// Paused describes what work with Status "paused" waits on.
	// A "paused" record without it predates paused kinds and is a stacked
	// pull request waiting on its blocker.
	Paused *core.PausedWork `json:"paused,omitempty"`
}

// UnmarshalJSON reads a work state, including one saved while paused work was
// still called parked: a "parked" status and its "parked" record
// are read as paused, so existing state files keep working.
func (s *workState) UnmarshalJSON(data []byte) error {
	type plain workState
	var decoded struct {
		plain
		Parked *core.PausedWork `json:"parked,omitempty"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*s = workState(decoded.plain)
	if s.Status == "parked" {
		s.Status = "paused"
	}
	if s.Paused == nil {
		s.Paused = decoded.Parked
	}
	return nil
}

// activeCountsByTarget counts, per configured target, the work this instance
// currently has in flight, counting only the keys parse accepts. Issues and
// discussions are dispatched on separate paths and each rotates over its own
// candidates, so each caller passes the parser for its own key shape and a
// backlog on one path never leads the other's rotation.
func activeCountsByTarget(active map[string]string, parse func(string) (string, int, bool)) map[string]int {
	counts := make(map[string]int, len(active))
	for key := range active {
		if target, _, ok := parse(key); ok {
			counts[target]++
		}
	}
	return counts
}

// balanceAcrossTargets reorders the dispatch candidates a poll produced so the
// watched targets are drawn from in turn instead of one target's whole backlog
// being queued ahead of another's. A poll lists the targets in the
// order they were configured and appends each one's issues to a single slice,
// and the dispatch loop blocks on a concurrency slot in that same order, so
// `aviary watch repoA repoB` with 20 open issues in repoA and 3 in repoB spends
// every slot on repoA until it drains and never reaches repoB.
//
// Each target keeps its own order, and the rotation is led by whichever target
// has the least work already in flight (ties broken by configured order), so a
// target that already filled slots on an earlier poll yields the next ones to a
// target that has none. Discussion dispatch rotates over its own candidates
// through this same function.
//
// Before rotating, candidates are split into priority tiers by label (issue
// #681): issues labeled `critical` come first, then `high`, then everything
// else. Each tier is balanced across targets on its own and the tiers are
// concatenated, so a critical issue in one target goes ahead of normal work in
// every target while each tier keeps its existing order.
func balanceAcrossTargets(pending []pendingIssue, inFlight map[string]int) []pendingIssue {
	if len(pending) < 2 {
		return pending
	}
	var tiers [issuePriorityTiers][]pendingIssue
	for _, candidate := range pending {
		tier := issuePriority(candidate.issue)
		tiers[tier] = append(tiers[tier], candidate)
	}
	balanced := make([]pendingIssue, 0, len(pending))
	for _, tier := range tiers {
		balanced = append(balanced, balanceTierAcrossTargets(tier, inFlight)...)
	}
	return balanced
}

// Priority tiers, in dispatch order. issuePriorityTiers counts them.
const (
	issuePriorityCritical = iota
	issuePriorityHigh
	issuePriorityNormal
	issuePriorityTiers
)

// issuePriority returns the dispatch tier an issue's labels place it in,
// matching the `critical` and `high` labels case-insensitively. An issue
// carrying both is critical.
func issuePriority(issue Issue) int {
	priority := issuePriorityNormal
	for _, label := range issue.Labels {
		name := strings.TrimSpace(label.Name)
		switch {
		case strings.EqualFold(name, "critical"):
			return issuePriorityCritical
		case strings.EqualFold(name, "high"):
			priority = issuePriorityHigh
		}
	}
	return priority
}

// balanceTierAcrossTargets rotates one priority tier's candidates across
// their targets, as balanceAcrossTargets describes.
func balanceTierAcrossTargets(pending []pendingIssue, inFlight map[string]int) []pendingIssue {
	if len(pending) < 2 {
		return pending
	}
	order := make([]string, 0, len(pending))
	queues := make(map[string][]pendingIssue, len(pending))
	for _, candidate := range pending {
		target := candidate.issue.Target
		if _, ok := queues[target]; !ok {
			order = append(order, target)
		}
		queues[target] = append(queues[target], candidate)
	}
	if len(order) < 2 {
		return pending
	}
	slices.SortStableFunc(order, func(a, b string) int { return inFlight[a] - inFlight[b] })
	balanced := make([]pendingIssue, 0, len(pending))
	for len(balanced) < len(pending) {
		for _, target := range order {
			queue := queues[target]
			if len(queue) == 0 {
				continue
			}
			balanced = append(balanced, queue[0])
			queues[target] = queue[1:]
		}
	}
	return balanced
}

func issueKey(issue Issue) string {
	target := issue.Target
	if target == "" {
		target = issue.Repository
	}
	return target + "#" + strconv.Itoa(issue.Number)
}

// parseIssueWorkKey splits a key built by issueKey back into its configured
// target and issue number. Discussion keys carry an extra "#discussion"
// segment and name a thread rather than an issue, so they are rejected.
func parseIssueWorkKey(key string) (target string, number int, ok bool) {
	index := strings.LastIndex(key, "#")
	if index < 0 {
		return "", 0, false
	}
	target = key[:index]
	if strings.HasSuffix(target, "#discussion") {
		return "", 0, false
	}
	number, err := strconv.Atoi(key[index+1:])
	if err != nil || number <= 0 {
		return "", 0, false
	}
	return target, number, true
}

// discussionWorkKey names an in-flight discussion thread. The extra segment is
// what keeps a discussion out of the issue dispatch rotation and its own work
// key distinct from the issue that happens to share its number.
func discussionWorkKey(target string, number int) string {
	return target + "#discussion#" + strconv.Itoa(number)
}

// parseDiscussionWorkKey splits a key built by discussionWorkKey back into its
// configured target and discussion number. Issue keys carry no "#discussion"
// segment and are rejected, mirroring parseIssueWorkKey.
func parseDiscussionWorkKey(key string) (target string, number int, ok bool) {
	index := strings.LastIndex(key, "#")
	if index < 0 {
		return "", 0, false
	}
	target = strings.TrimSuffix(key[:index], "#discussion")
	if target == key[:index] {
		return "", 0, false
	}
	number, err := strconv.Atoi(key[index+1:])
	if err != nil || number <= 0 {
		return "", 0, false
	}
	return target, number, true
}

// activeIssuesForRepo lists, in ascending order, the issue numbers this
// instance is actively working in repo. Work keys are scoped by configured
// target, which may be a project URL rather than a repository, so each key
// is resolved back to its repository before comparison instead of being
// string-matched against repo directly.
func activeIssuesForRepo(active map[string]string, repo string) []int {
	var numbers []int
	for key := range active {
		target, number, ok := parseIssueWorkKey(key)
		if !ok {
			continue
		}
		if !strings.EqualFold(issueRepository(target, Issue{}), repo) {
			continue
		}
		numbers = append(numbers, number)
	}
	slices.Sort(numbers)
	return numbers
}

// manuallyPausedIssuesForRepo returns the issue numbers in repo whose work
// the owner paused from a dashboard.
func manuallyPausedIssuesForRepo(work map[string]workState, repo string) []int {
	var numbers []int
	for key, state := range work {
		if state.Status != "paused" || pausedRecord(state).Kind != core.PausedManual {
			continue
		}
		target, number, ok := parseIssueWorkKey(key)
		if !ok || !strings.EqualFold(issueRepository(target, Issue{}), repo) {
			continue
		}
		numbers = append(numbers, number)
	}
	slices.Sort(numbers)
	return numbers
}

type taskState struct {
	mu        sync.Mutex
	running   int
	queued    int
	completed int
	failed    int
}

type jobOutputWriter struct {
	write func(string)
}

func (w jobOutputWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.write(string(p))
	}
	return len(p), nil
}

func (s *taskState) snapshot() (running, queued, completed, failed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running, s.queued, s.completed, s.failed
}

func (w *Engine) logf(format string, args ...interface{}) {
	w.logMu.Lock()
	defer w.logMu.Unlock()
	line := fmt.Sprintf("%s "+format, append([]interface{}{time.Now().Format("2006-01-02 15:04:05")}, args...)...)
	// Run defaults Out to io.Discard, but handoff logging also runs from code
	// paths exercised without it; a missing writer must not panic.
	if w.Out != nil {
		_, _ = fmt.Fprintln(w.Out, line)
	}
	w.recentLogs = append(w.recentLogs, line)
	if len(w.recentLogs) > recentLogLines {
		w.recentLogs = w.recentLogs[len(w.recentLogs)-recentLogLines:]
	}
	if w.UI != nil {
		w.UI.Log(line)
	}
}

// logChanged logs a line only when the state it reports differs from the state
// last logged under the same key, and reports whether it logged. Polling
// otherwise repeats an unchanged summary, and an unchanged failure, on every
// tick.
func (w *Engine) logChanged(key, state, format string, args ...interface{}) bool {
	w.repeatMu.Lock()
	if previous, ok := w.lastLogged[key]; ok && previous == state {
		w.repeatMu.Unlock()
		return false
	}
	if w.lastLogged == nil {
		w.lastLogged = map[string]string{}
	}
	w.lastLogged[key] = state
	w.repeatMu.Unlock()
	w.logf(format, args...)
	return true
}

// forgetLogged drops the state remembered for key so the next logChanged with
// it logs again, and reports whether anything was remembered. That answer is
// how a poll that succeeded knows a failure was reported for it earlier.
func (w *Engine) forgetLogged(key string) bool {
	w.repeatMu.Lock()
	defer w.repeatMu.Unlock()
	_, ok := w.lastLogged[key]
	delete(w.lastLogged, key)
	return ok
}

// pollStallIntervals is how many poll intervals may pass with no poll
// completing before the run reports that it has stopped making progress. A
// poll that finds nothing new logs nothing, so a wedged loop and a quiet one
// read the same in the terminal; this is what tells them apart.
const pollStallIntervals = 4

// The wake check compares how much wall-clock time passed between two of its
// ticks with how much should have. A process does not run while the machine
// sleeps, so the first tick after waking lands long after the one before it.
// They are variables only so tests need not spend them.
var (
	wakeCheckInterval = 15 * time.Second
	wakeJumpThreshold = time.Minute
)

// watchForWake reports each time the machine resumes from sleep, with roughly
// how long it slept, so the run can re-establish connections that did not
// survive it instead of staying broken until it is restarted.
func watchForWake(ctx context.Context) <-chan time.Duration {
	wakes := make(chan time.Duration, 1)
	ticker := time.NewTicker(wakeCheckInterval)
	go func() {
		defer ticker.Stop()
		detectWakes(ctx, ticker.C, wakeCheckInterval, time.Now, wakes)
	}()
	return wakes
}

// detectWakes is watchForWake's loop. Wall-clock time is compared with its
// monotonic reading stripped, because on most platforms the monotonic clock
// stops while the machine sleeps and would hide the gap. A clock set forward
// by hand looks the same, which costs no more than one extra refresh. A wake
// is dropped rather than queued when the last one has not been handled yet.
func detectWakes(ctx context.Context, ticks <-chan time.Time, interval time.Duration, now func() time.Time, wakes chan<- time.Duration) {
	last := now().Round(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
		current := now().Round(0)
		if slept := current.Sub(last) - interval; slept >= wakeJumpThreshold {
			select {
			case wakes <- slept:
			default:
			}
		}
		last = current
	}
}

// watchPollProgress reports a run whose poll loop has stopped completing polls,
// and reports it once rather than on every check. Every GitHub read the loop
// makes is bounded, so a stall this long means something else is holding it;
// saying so is what keeps a watch that has stopped dispatching from looking
// merely idle.
func (w *Engine) watchPollProgress(ctx context.Context, interval time.Duration, started time.Time, lastPoll func() time.Time) {
	if interval <= 0 {
		return
	}
	stalledAfter := interval * pollStallIntervals
	if stalledAfter < time.Minute {
		stalledAfter = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		last := lastPoll()
		if last.IsZero() {
			last = started
		}
		if idle := time.Since(last); idle >= stalledAfter {
			w.logChanged("poll-stall", "stalled", "no poll has completed in %s; the watch is not picking up new work", formatInterval(idle.Round(time.Second)))
		} else if w.forgetLogged("poll-stall") {
			w.logf("polling resumed; the stall reported above is over")
		}
	}
}

// Run watches the targets and dispatches jobs until ctx is cancelled.
func (w *Engine) Run(ctx context.Context) error {
	// Ownership handshakes wait out their grace window off the poll loop, so
	// a run that is shutting down waits for them here rather than leaving
	// them posting comments after it has returned. Cancelling ctx cuts the
	// wait short, so this does not hold a shutdown open for the full window.
	defer w.awaitNegotiations()
	// Published before anything slow, so settings read from here on even
	// while startup is still loading state or calling GitHub.
	w.publishSettings()
	targets := append([]string(nil), w.Targets...)
	if len(targets) == 0 && w.Repo != "" {
		targets = []string{w.Repo}
	}
	if len(targets) == 0 {
		return fmt.Errorf("at least one target is required")
	}
	for _, target := range targets {
		if _, err := parseTarget(target); err != nil {
			return err
		}
	}
	if w.Interval <= 0 {
		return fmt.Errorf("interval must be positive")
	}
	if w.Concurrency <= 0 {
		return fmt.Errorf("concurrency must be positive")
	}
	if w.Out == nil {
		w.Out = io.Discard
	}
	watchCtx, stopWatching := context.WithCancel(ctx)
	defer stopWatching()
	stateChanges := watchStateFile(watchCtx, w.StatePath)
	work, err := loadScopedWorkState(w.StatePath, targets)
	if err != nil {
		return err
	}
	closureChecker, _ := w.Issues.(WorkClosureChecker)
	if err := w.resetFailedWork(context.Background(), work); err != nil {
		return err
	}
	seen := make(map[string]bool, len(work))
	restored := make(map[string]bool, len(work))
	for key := range work {
		seen[key] = true
		restored[key] = true
	}
	w.logf("watching %s (instance %s, %s, concurrency %d; %d handled issue(s) loaded)", strings.Join(targets, ", "), w.Identity, w.watchDescription(), w.Concurrency, len(seen))
	sem := newConcurrencySemaphore(w.Concurrency)
	var wg sync.WaitGroup
	var tasks taskState
	var workMu sync.Mutex
	active := make(map[string]string)
	cancellations := make(map[string]context.CancelCauseFunc)
	// nudges lets a webhook delivery about an issue an agent is working ask
	// that run's watcher to check GitHub now rather than on its next tick, so
	// push mode relays the change as promptly as it dispatches new work
	//. The delivery is only a nudge: what actually interrupts the
	// run is still read back from GitHub.
	nudges := make(map[string]chan struct{})
	// runExits is closed once a run's goroutine has finished, after its agent
	// process has exited and its bookkeeping is saved, so a stop from the web
	// UI can wait for that instead of replying as soon as it cancels. stopping
	// reserves an issue between the stop request and that exit, so no poll
	// dispatches a second run beside the one still unwinding.
	runExits := make(map[string]chan struct{})
	stopping := make(map[string]bool)
	// pausing marks a stopping run that the owner paused, so its exit records
	// a manual pause instead of a failure.
	pausing := make(map[string]bool)
	directMentions := make(map[string]bool)
	mentions := newMentionScan(time.Now())
	// Every issue state read for active, parked, and completed work also
	// keeps the derived watch on its linked pull requests current (issue
	// #723).
	closureChecker = recordLinkedPullRequests(closureChecker, mentions.linked)
	// Every one of those reads also notes the merged pull requests, which
	// status counts over the last day. They are saved beside the work state
	// so the count survives a restart (issue #413).
	merges := newRecentMerges(mergesPathFor(w.StatePath), time.Now())
	closureChecker = recordRecentMerges(closureChecker, merges)
	// webhookRepos are the repositories configured with webhooks when the
	// watch started. A linked pull request anywhere else delivers nothing, so
	// push mode scans it on each poll instead.
	webhookRepos := mentionScanRepos(targets, nil)
	workFinished := make(chan struct{}, 1)
	jobs := make(map[string]JobSnapshot)
	issueCounts := make(map[string]int)
	// lastPoll is when the most recent poll of GitHub finished, which the UIs
	// report so a run that has stopped logging every tick can
	// still be seen checking. Guarded by jobMu, since publish
	// reads it from whichever goroutine reports a job change.
	var lastPoll time.Time
	var jobMu sync.Mutex
	// Clearing unused clones reads the live work and jobs, so a clone a job
	// still needs is never removed.
	w.setCloneView(func() (map[string]workState, map[string]JobSnapshot) {
		workMu.Lock()
		workCopy := make(map[string]workState, len(work))
		for key, state := range work {
			workCopy[key] = state
		}
		workMu.Unlock()
		jobMu.Lock()
		jobsCopy := make(map[string]JobSnapshot, len(jobs))
		for key, job := range jobs {
			jobsCopy[key] = job
		}
		jobMu.Unlock()
		return workCopy, jobsCopy
	})
	defer w.setCloneView(nil)
	publish := func() {
		if w.UI == nil {
			return
		}
		running, queued, completed, failed := tasks.snapshot()
		jobMu.Lock()
		list := publishedJobs(jobs)
		counts := make(map[string]int, len(issueCounts))
		for target, count := range issueCounts {
			counts[target] = count
		}
		paused := 0
		for _, job := range jobs {
			if job.Status == "paused" {
				paused++
			}
		}
		polled := lastPoll
		watched := targets
		jobMu.Unlock()
		var quotas map[string]string
		if w.Quota != nil {
			quotas = w.Quota(ctx)
		}
		online := w.UseWebhooks
		if online && w.WebhookOnline != nil {
			online = w.WebhookOnline()
		}
		snapshot := EngineSnapshot{Identity: string(w.Identity), Targets: watched, IssueCounts: counts, Running: running, Queued: queued, Completed: completed, Merged24h: merges.count(time.Now()), Failed: failed, Paused: paused, Concurrency: w.Concurrency, AllowedCommenters: append([]string(nil), w.AllowedCommenters...), Interval: w.Interval, UseWebhooks: w.UseWebhooks, WebhookOnline: online, WebUIURL: w.WebUIURL, LastPoll: polled, Quotas: quotas, Jobs: list, VisibleJobs: maxVisibleJobs}
		if w.GitHubSession != nil {
			snapshot.GitHub = w.GitHubSession()
		}
		w.published.Store(&snapshot)
		w.UI.Snapshot(snapshot)
	}
	pollNumber := 0
	// observed records the "repo#number" keys returned by the most recent
	// poll. Webhook follow-up refreshes exist only to outlast GitHub's issue
	// index lag, so once the delivered issue shows up here the
	// remaining refreshes have nothing left to catch. Only the run loop's own
	// goroutine reads or writes it.
	var observed map[string]bool
	// retriedPaused holds the paused job a web UI retry just unpaused while
	// the poll that requeues it runs, so a blocker that is still open does not
	// pause it again: the retry overrides what it waited on.
	// Only the run loop's own goroutine reads or writes it.
	retriedPaused := make(map[string]bool)
	// retryAfterStop holds running jobs a web UI retry stopped, to requeue
	// once their run exits. serveJobAction answers dashboard job actions, both
	// from the run loop and while a poll waits for a slot.
	retryAfterStop := make(map[string]bool)
	var serveJobAction func(request jobActionRequest, dispatching bool)
	// startScheduled queues a scheduled prompt run as a board job.
	var startScheduled func(request scheduledRunRequest)
	var serveScheduledAction func(request jobActionRequest, key string, job JobSnapshot)
	// acquireDispatchSlot waits for a free slot for the queued job key, as
	// acquireSlot does, while also answering job actions, so the owner can
	// pause the queued job or stop a running one to free its slot (issue
	// #771). It reports whether the queued job was paused instead.
	acquireDispatchSlot := func(key string) (bool, error) {
		for {
			select {
			case <-sem.tokens:
				return false, nil
			case request := <-w.settingsRequestsChan():
				request.done <- w.applySettingsRequest(request.update, sem)
			case request := <-w.scheduledRunRequests():
				startScheduled(request)
			case request := <-w.jobActionRequests():
				serveJobAction(request, true)
				workMu.Lock()
				paused := work[key].Status == "paused"
				workMu.Unlock()
				if paused {
					return true, nil
				}
			case <-ctx.Done():
				return false, ctx.Err()
			}
		}
	}
	// lastPausedWatch is when the paused watch last read back every paused
	// record, including ones the poll did not list, and forcePausedWatch asks
	// the next poll to run it regardless. Only the run loop's own
	// goroutine reads or writes them.
	var lastPausedWatch time.Time
	forcePausedWatch := false
	issueHydrator, _ := w.Issues.(IssueHydrator)
	pollOnce := func(sweep map[string]bool) error {
		if w.Held != nil && w.Held() {
			w.logChanged("restart-hold", "held", "a safe restart is holding new jobs; running jobs carry on")
			return nil
		}
		if w.forgetLogged("restart-hold") {
			w.logf("the safe restart's hold on new jobs was released")
		}
		pollNumber++
		n := pollNumber
		var running, queued int
		// Publish before listing: browser mode consults this snapshot while
		// extracting, to avoid fetching metadata for work already in flight
		// or already finished.
		workMu.Lock()
		w.publishHandledIssues(work, active)
		workMu.Unlock()
		issues := make([]Issue, 0)
		for _, target := range targets {
			if isDiscussionTarget(target) {
				continue
			}
			batch, err := w.Issues.ListIssues(ctx, target)
			if err != nil {
				return fmt.Errorf("listing %s: %w", target, err)
			}
			jobMu.Lock()
			issueCounts[target] = len(batch)
			jobMu.Unlock()
			for i := range batch {
				batch[i].Target = target
				if batch[i].Repository == "" {
					batch[i].Repository = issueRepository(target, batch[i])
				}
				issues = append(issues, batch[i])
			}
		}
		w.logChanged("poll-issues", strconv.Itoa(len(issues)), "poll #%d found %d open issue(s)", n, len(issues))
		w.pollDiscussions(ctx, n, targets, sem, &wg, &workMu, active)
		observed = make(map[string]bool, len(issues))
		for _, issue := range issues {
			if issue.Repository != "" && issue.Number > 0 {
				observed[issue.Repository+"#"+strconv.Itoa(issue.Number)] = true
			}
		}
		// Without webhooks nothing delivers an issue_comment, so direct
		// mentions are found by scanning each repository's recent comments
		//.
		if !w.UseWebhooks && w.Mentions != nil {
			w.scanDirectMentions(ctx, mentions, mentionScanRepos(targets, issues), observed, directMentions)
		} else if w.Mentions != nil {
			w.scanLinkedPullRequests(ctx, mentions, webhookRepos, observed, directMentions)
		}
		// The paused watch: on its own cadence, paused work whose issue the
		// poll did not list is read back and decided below like any listed
		// paused work, so its wake never depends on the dispatch feed (issue
		// #769). A watch a little early is still due, so a reap or webhook
		// poll that lands just before the watch's tick runs it instead.
		if every := w.pausedWatchEvery(); issueHydrator != nil && (forcePausedWatch || time.Since(lastPausedWatch) >= every-every/10) {
			forcePausedWatch = false
			lastPausedWatch = time.Now()
			listed := make(map[string]bool, len(issues))
			for _, issue := range issues {
				listed[issueKey(issue)] = true
			}
			var keys []string
			workMu.Lock()
			for key, state := range work {
				// A manual pause is read back too: nothing on GitHub wakes
				// it, but its issue closing finishes it.
				if _, running := active[key]; state.Status == "paused" && !running && !listed[key] {
					keys = append(keys, key)
				}
			}
			workMu.Unlock()
			slices.Sort(keys)
			issues = append(issues, w.unlistedPausedIssues(ctx, issueHydrator, keys, targets)...)
		}
		// pauseJob shows paused work on the dashboards, apart from both active
		// and completed work.
		// A paused job whose own state was read shows its pull request
		//.
		pauseJob := func(issue Issue, key, reason string, own *OriginatingWorkState) {
			jobMu.Lock()
			job, ok := jobs[key]
			if !ok {
				job = JobSnapshot{Number: issue.Number, Target: issue.Target, Repository: issueRepository(issue.Target, issue), Title: issue.Title}
			}
			if job.Title == "" {
				job.Title = issue.Title
			}
			changed := job.Status != "paused" || job.WaitReason != reason
			if own != nil && job.SetPullRequest(*own, issueRepository(issue.Target, issue)) {
				changed = true
			}
			job.Status, job.WaitReason = "paused", reason
			jobs[key] = job
			jobMu.Unlock()
			if changed {
				publish()
			}
		}
		// finishPaused ends paused work whose pull request merged or whose
		// issue closed. A run that left a pull request behind is booked as
		// completed then, not when it paused; an issue that was only waiting
		// on its blockers never ran, so its record is dropped.
		finishPaused := func(issue Issue, key string, paused core.PausedWork, reason string) {
			ran := paused.Kind != core.PausedBlocked
			workMu.Lock()
			if ran {
				state := work[key]
				state.Status, state.Paused = "completed", nil
				work[key] = state
			} else {
				delete(work, key)
			}
			workMu.Unlock()
			jobMu.Lock()
			if job, ok := jobs[key]; ok && ran {
				job.Status, job.WaitReason = "complete", ""
				job.Finish()
				jobs[key] = job
			} else {
				delete(jobs, key)
			}
			jobMu.Unlock()
			if ran {
				tasks.mu.Lock()
				tasks.completed++
				tasks.mu.Unlock()
				if w.Status != nil {
					if statusErr := w.Status.SetIssueStatus(ctx, issue.Target, issue, "Done"); statusErr != nil {
						w.logf("issue #%d failed to update project status: %v", issue.Number, statusErr)
					}
				}
			}
			w.logf("issue #%d unpaused: %s; its paused work is finished", issue.Number, reason)
			publish()
		}
		observedWork := make(map[string]bool, len(issues))
		for _, issue := range issues {
			observedWork[issueKey(issue)] = true
		}
		newIssues := make([]pendingIssue, 0)
		for _, issue := range issues {
			key := issueKey(issue)
			workMu.Lock()
			pausedState := work[key]
			_, running := active[key]
			workMu.Unlock()
			// Paused work is out of the queue and holds no slot until its
			// wake condition, read from GitHub here, fires.
			wasPaused := pausedState.Status == "paused" && !running
			var paused core.PausedWork
			wake, wakeReason, wakeRejected := core.PausedStay, "", 0
			var pausedOwn *OriginatingWorkState
			if wasPaused {
				paused = pausedRecord(pausedState)
				wake, wakeReason, wakeRejected, pausedOwn = w.pausedWake(ctx, closureChecker, paused, issue)
				if wake == core.PausedStay && paused.Kind == core.PausedBlocked {
					if pullRequest, ok := w.stackableOnDependency(ctx, closureChecker, issue); ok {
						w.logChanged("stack-"+key, strconv.Itoa(pullRequest), "issue #%d %s; stacking it on pull request #%d", issue.Number, wakeReason, pullRequest)
						wake, wakeReason = core.PausedResume, fmt.Sprintf("its blocker has pull request #%d to stack on", pullRequest)
					}
				}
				mentioned := directMentions[issue.Repository+"#"+strconv.Itoa(issue.Number)]
				switch {
				case wake == core.PausedStay && mentioned && paused.Kind == core.PausedPullRequest:
					// A direct mention is an instruction addressed to this
					// instance, not review activity, so it is acted on below
					// as it was before the pull request was paused.
					w.logf("issue #%d paused work has a direct mention of instance %s; dispatching it", issue.Number, w.Identity)
				case wake == core.PausedStay:
					w.logChanged("paused-"+key, wakeReason, "issue #%d paused: %s", issue.Number, wakeReason)
					pauseJob(issue, key, wakeReason, pausedOwn)
					continue
				case wake == core.PausedComplete:
					finishPaused(issue, key, paused, wakeReason)
					continue
				}
			} else if blocked, reason := issueBlocked(issue); blocked && !retriedPaused[key] {
				if pullRequest, ok := w.stackableOnDependency(ctx, closureChecker, issue); ok {
					w.logChanged("stack-"+key, strconv.Itoa(pullRequest), "issue #%d %s; stacking it on pull request #%d", issue.Number, reason, pullRequest)
				} else if issue.HasSubIssues || running || pausedState.Status == "completed" {
					w.logf("issue #%d not picked up: %s", issue.Number, reason)
					continue
				} else {
					// A dependency-blocked issue is paused until its blockers
					// close, keeping any session it already has.
					workMu.Lock()
					state := work[key]
					state.Status = "paused"
					state.Paused = &core.PausedWork{Kind: core.PausedBlocked, WaitingOn: core.OpenDependencies(issue)}
					work[key] = state
					workMu.Unlock()
					w.logChanged("paused-"+key, reason, "issue #%d not picked up: %s; paused until it closes", issue.Number, reason)
					pauseJob(issue, key, reason, nil)
					continue
				}
			}
			mentionKey := issue.Repository + "#" + strconv.Itoa(issue.Number)
			directMention := directMentions[mentionKey]
			addressed := directMention && !mentions.rechecks[mentionKey]
			swept := sweep[key] || sweep[mentionKey]
			workMu.Lock()
			state := work[key]
			// hadLocalRecord captures, before any reset below, whether this
			// instance already had a record of this issue (persisted from a
			// prior run or seen earlier this run). Its absence means the
			// issue was picked up fresh, so no other instance can plausibly
			// believe it already owns it.
			hadLocalRecord := restored[key] || seen[key]
			reconcileCompleted := isProjectTarget(issue.Target) && state.Status == "completed"
			staleRestoredState := (restored[key] || reconcileCompleted) && !workStateMatchesRemote(issue.Target, issue, state)
			if staleRestoredState {
				staleStatus := state.Status
				delete(work, key)
				delete(seen, key)
				delete(restored, key)
				state = work[key]
				w.logf("issue #%d reset stale local %s state", issue.Number, staleStatus)
			}
			_, isActive := active[key]
			isStopping := stopping[key]
			wasActive := work[key].Status == "active"
			wasFailed := work[key].Status == "failed"
			wasCompleted := work[key].Status == "completed"
			resumable := work[key].SessionID != "" && work[key].Agent != ""
			nudge := nudges[key]
			workMu.Unlock()
			if isStopping {
				// A mention stays queued too, and is dispatched by the poll
				// that follows the stopped run's exit.
				w.logf("issue #%d not picked up: its stopped run is still exiting", issue.Number)
				continue
			}
			// A completed run's ready pull request can still be closed without
			// merging after the run ended. That rejects the attempt exactly as
			// it does mid-run, so the completed record stops
			// suppressing the issue and the work restarts.
			rejected := 0
			if wake == core.PausedRestart {
				rejected = wakeRejected
				w.logf("issue #%d unpaused: %s while the issue is still open; starting a fresh attempt", issue.Number, wakeReason)
			}
			if wasCompleted && !isActive && closureChecker != nil {
				if number, reason := w.completedWorkRejected(ctx, closureChecker, issue); reason != "" {
					rejected = number
					wasCompleted = false
					w.logf("issue #%d restarting completed work: %s while the issue is still open; starting a fresh attempt", issue.Number, reason)
				}
			}
			if directMention && isActive {
				if resumable {
					// The run's watcher reads the issue and the pull requests it
					// opened, and relays the mention into the same session (issue
					// #693). Keeping it queued as well would start a second, fresh
					// run on the same comment once this one finishes.
					delete(directMentions, mentionKey)
					delete(mentions.rechecks, mentionKey)
					if nudge != nil {
						select {
						case nudge <- struct{}{}:
						default:
						}
					}
					w.logf("issue #%d direct mention of instance %s concerns the run in progress; relaying it into that run", issue.Number, w.Identity)
				} else {
					w.logChanged("mention-active-"+key, "waiting", "issue #%d direct mention of instance %s waits for the run in progress to finish: that run's session cannot be resumed to relay it", issue.Number, w.Identity)
				}
			}
			if issue.Number > 0 && (wasFailed || rejected > 0 || wasPaused || (staleRestoredState && remoteIssueAllowsDispatch(issue.Target, issue, w.ReadyState)) || shouldDispatchIssue(issue.Target, issue, isActive, wasActive, wasCompleted, seen[key], w.ReadyState) || (swept && !isActive && remoteIssueAllowsDispatch(issue.Target, issue, w.ReadyState)) || (directMention && !isActive)) {
				seen[key] = true
				delete(restored, key)
				delete(directMentions, mentionKey)
				delete(mentions.rechecks, mentionKey)
				// A known local failure or an active resume is unambiguously
				// this instance's own work; anything else reappearing after
				// having a local record is a reclaim that must be negotiated
				// through the comment protocol before dispatch. A project item
				// sitting at "In Progress" that this instance has no record of
				// is another instance's apparent work (typically stranded by
				// one that died mid-run), so it must be negotiated too.
				contested := (hadLocalRecord && !wasFailed && !wasActive && !wasPaused && rejected == 0) || (!hadLocalRecord && (projectItemInProgress(issue.Target, issue) || swept))
				session := AgentSession{
					ID: state.SessionID, Agent: state.Agent, CheckoutDirectory: state.CheckoutDirectory,
					// Persisted work is not an active worker after a daemon restart or
					// a prior failure. If it has a complete session identity, resume it
					// so the agent can recover the existing draft PR and worktree.
					Resume: state.SessionID != "" && state.Agent != "",
				}
				// Work state persisted by an earlier run must not pin the issue
				// to an agent the current configuration no longer dispatches to
				//. Drop the stale identity so the dispatch below
				// selects a currently configured agent and starts a fresh
				// session instead of resuming with the retired one.
				if session.Agent != "" && !w.agentStillConfigured(session.Agent) {
					w.logf("issue #%d discarded persisted agent %q; it is no longer configured", issue.Number, session.Agent)
					session.ID, session.Agent, session.Resume = "", "", false
				}
				if session.Resume && wasActive && w.RestartNotice != nil {
					if notice := w.RestartNotice(issue); notice != "" {
						session.Update = notice
						w.logf("issue #%d resuming after a safe restart in its own session", issue.Number)
					}
				}
				if wake == core.PausedResume && paused.Kind != core.PausedStacked {
					w.logf("issue #%d unpaused: %s; dispatching it", issue.Number, wakeReason)
				} else if wake == core.PausedResume {
					w.logf("issue #%d unpaused: %s; resuming its stacked pull request", issue.Number, wakeReason)
					if session.Resume {
						session.Update = pausedWorkPrompt(issue)
					}
				}
				if rejected > 0 {
					// Like a rejection mid-run, a resumable session is told the
					// attempt was rejected and one that cannot be resumed starts
					// over; neither reuses the closed pull request.
					if session.Resume {
						session.Update = rejectedPullRequestPrompt(issue, rejected)
					} else {
						session = AgentSession{}
					}
				}
				if directMention {
					// A direct mention is a new threaded instruction. Start a fresh
					// gh-fix invocation so it receives the identity argument and
					// rereads the issue and PR conversation from GitHub.
					session = AgentSession{}
					contested = false
				}
				newIssues = append(newIssues, pendingIssue{issue: issue, contested: contested, session: session, mentioned: addressed, woken: wasPaused})
			}
		}
		// Paused work whose issue the poll no longer lists cannot be woken by
		// it, so its own pull requests are read back once per poll to finish
		// it when its issue closed or its pull request merged.
		if closureChecker != nil {
			workMu.Lock()
			unlisted := make(map[string]core.PausedWork)
			for key, state := range work {
				if _, running := active[key]; state.Status == "paused" && !running && !observedWork[key] {
					unlisted[key] = pausedRecord(state)
				}
			}
			workMu.Unlock()
			for key, paused := range unlisted {
				target, number, ok := parseIssueWorkKey(key)
				if !ok || !slices.Contains(targets, target) {
					continue
				}
				issue := Issue{Number: number, Target: target, Repository: issueRepository(target, Issue{})}
				state, err := closureChecker.OriginatingWorkState(ctx, issue.Repository, number)
				if err != nil {
					continue
				}
				if wake, reason := core.WakePaused(paused, core.PausedCheck{Issue: issue, Own: &state}); wake == core.PausedComplete {
					finishPaused(issue, key, paused, reason)
				}
			}
		}
		// A job the owner paused whose issue the poll no longer lists still
		// shows on the dashboards until the owner retries it or
		// its issue closes.
		workMu.Lock()
		var manual []string
		for key, state := range work {
			if _, running := active[key]; state.Status == "paused" && !running && !observedWork[key] && pausedRecord(state).Kind == core.PausedManual {
				manual = append(manual, key)
			}
		}
		workMu.Unlock()
		for _, key := range manual {
			if target, number, ok := parseIssueWorkKey(key); ok && slices.Contains(targets, target) {
				workMu.Lock()
				reason := pausedRecord(work[key]).ManualReason()
				workMu.Unlock()
				pauseJob(Issue{Number: number, Target: target, Repository: issueRepository(target, Issue{})}, key, reason, nil)
			}
		}
		// A mention whose issue the poll did not list can never be dispatched,
		// so say why it is waiting rather than leaving it queued silently
		//.
		for mentionKey := range directMentions {
			if !observed[mentionKey] {
				w.logChanged("mention-unlisted-"+mentionKey, "waiting", "%s direct mention of instance %s is waiting: it is not among the open issues this instance watches (closed, filtered out, a pull request that closes no issue, or not indexed yet)", mentionKey, w.Identity)
			}
		}
		newIssues = w.standDownForStandingClaims(ctx, closureChecker, newIssues, seen)
		newIssues = w.negotiateContestedIssues(ctx, closureChecker, newIssues, seen, n == 1)
		workMu.Lock()
		inFlight := activeCountsByTarget(active, parseIssueWorkKey)
		workMu.Unlock()
		newIssues = balanceAcrossTargets(newIssues, inFlight)
		workMu.Lock()
		err = saveScopedWorkState(w.StatePath, work, targets)
		workMu.Unlock()
		if err != nil {
			return fmt.Errorf("saving state: %w", err)
		}
		if len(newIssues) == 0 {
			return nil
		}
		issuesToLog := make([]Issue, len(newIssues))
		for i := range newIssues {
			issuesToLog[i] = newIssues[i].issue
		}
		w.logf("poll #%d discovered %d new issue(s): %s", n, len(newIssues), issueNumbers(issuesToLog))
		for _, pending := range newIssues {
			issue := pending.issue
			session := pending.session
			if !session.Resume {
				session.Agent = ""
				if identified, ok := w.runner().(TargetAgentIdentifier); ok {
					session.Agent = identified.AgentNameFor(issue.Target)
				} else if identified, ok := w.runner().(AgentIdentifier); ok {
					session.Agent = identified.AgentName()
				}
				// Some agents accept a caller-provided session ID; the rest
				// assign their own and report it after launch, and their
				// definition says which (agents.Definition.Session).
				if agentAssignsSessionID(session.Agent) {
					session.ID, err = newSessionID()
					if err != nil {
						w.releasePendingClaim(ctx, pending, "creating its session identifier failed, so it was never dispatched")
						return err
					}
				}
			}
			// A repository the user cannot write to has its fix pushed to the
			// user's fork instead. The issue, its claim, and its
			// status all stay on the original repository; only the branch and
			// pull request come from the fork, which joins the watched targets
			// so its activity is watched without a restart.
			fork, forkErr := w.forkFor(ctx, issueRepository(issue.Target, issue))
			if forkErr != nil {
				w.logf("issue #%d not dispatched; failed to read write permission on its repository: %v", issue.Number, forkErr)
				w.releasePendingClaim(ctx, pending, "reading write permission on its repository failed, so it was never dispatched")
				continue
			}
			if fork != "" {
				issue.Fork = fork
				if !slices.Contains(targets, fork) {
					// Dispatch goroutines read targets under workMu and the
					// dashboard snapshot under jobMu, so the list is replaced
					// rather than appended to in place, under both.
					workMu.Lock()
					jobMu.Lock()
					targets = append(append([]string(nil), targets...), fork)
					jobMu.Unlock()
					workMu.Unlock()
					w.logf("issue #%d will open its pull request from fork %s (no write permission on %s); added %s to the watched targets", issue.Number, fork, issueRepository(issue.Target, issue), fork)
				}
			}
			if w.Status != nil {
				if err := w.Status.SetIssueStatus(ctx, issue.Target, issue, "In Progress"); err != nil {
					w.logf("issue #%d not dispatched; failed to set project status: %v", issue.Number, err)
					w.releasePendingClaim(ctx, pending, "setting the project status failed, so it was never dispatched")
					continue
				}
			}
			// Contested issues already posted their starting/continuing claim
			// during negotiation. Uncontested first-time pickups still need to
			// announce ownership so other instances know it's spoken for.
			if w.Comments != nil && (!session.Resume || pending.woken) && !pending.contested {
				repo := issueRepository(issue.Target, issue)
				claim := claimComment(w.Identity, false)
				alreadyLast, err := w.lastCommentMatches(ctx, repo, issue.Number, claim)
				if err != nil {
					w.logf("issue #%d failed to check its latest ownership claim: %v", issue.Number, err)
				}
				if alreadyLast {
					w.logf("issue #%d picked up uncontested; its ownership claim is already the latest comment", issue.Number)
				} else if err := w.Comments.PostComment(ctx, repo, issue.Number, claim); err != nil {
					w.logf("issue #%d failed to post ownership claim: %v", issue.Number, err)
				} else {
					w.logf("issue #%d picked up uncontested; claimed it as %s (%q)", issue.Number, w.Identity, startingClaimBody)
				}
			}
			workMu.Lock()
			key := issueKey(issue)
			active[key] = session.ID
			mentions.agentStarted(issue.Repository + "#" + strconv.Itoa(issue.Number))
			spec, _ := parseAgentSpec(session.Agent)
			jobMu.Lock()
			jobs[key] = JobSnapshot{Kind: core.JobKindIssue, Number: issue.Number, Target: issue.Target, Repository: issueRepository(issue.Target, issue), Title: issue.Title, Status: "queued", CheckoutDirectory: session.CheckoutDirectory, SessionID: session.ID, Agent: spec.Name, Model: spec.Model, Effort: spec.Level, Started: time.Now()}
			jobMu.Unlock()
			work[key] = workState{Status: "active", SessionID: session.ID, Agent: session.Agent, CheckoutDirectory: session.CheckoutDirectory, Owner: string(w.Identity)}
			err = saveScopedWorkState(w.StatePath, work, targets)
			workMu.Unlock()
			if err != nil {
				return err
			}
			tasks.mu.Lock()
			tasks.queued++
			queued = tasks.queued
			running = tasks.running
			tasks.mu.Unlock()
			w.logf("issue #%d queued (tasks: %d running, %d queued)", issue.Number, running, queued)
			publish()
			paused, err := acquireDispatchSlot(key)
			if err != nil || paused {
				tasks.mu.Lock()
				tasks.queued--
				tasks.mu.Unlock()
				if err != nil {
					return ctx.Err()
				}
				continue
			}
			tasks.mu.Lock()
			tasks.queued--
			tasks.running++
			queued = tasks.queued
			running = tasks.running
			tasks.mu.Unlock()
			jobMu.Lock()
			job := jobs[issueKey(issue)]
			job.Status = "active"
			jobs[issueKey(issue)] = job
			jobMu.Unlock()
			if session.Resume {
				w.jobSystem(issueKey(issue), "resumed session "+session.ID+" on "+session.Agent)
			} else {
				w.jobSystem(issueKey(issue), "claimed and started on "+session.Agent)
			}
			publish()
			startedRunning, startedQueued := running, queued
			wg.Add(1)
			go func(i Issue, agentSession AgentSession, running, queued int) {
				defer wg.Done()
				defer sem.release()
				defer func() {
					select {
					case workFinished <- struct{}{}:
					default:
					}
				}()
				key := issueKey(i)
				nudge := make(chan struct{}, 1)
				exited := make(chan struct{})
				workMu.Lock()
				nudges[key] = nudge
				runExits[key] = exited
				workMu.Unlock()
				// Registered after workFinished so the reservation is gone
				// before the dispatch loop hears the run finished and polls,
				// which is what reruns a retry.
				defer func() {
					workMu.Lock()
					delete(runExits, key)
					delete(stopping, key)
					delete(pausing, key)
					workMu.Unlock()
					close(exited)
				}()
				defer func() {
					workMu.Lock()
					delete(nudges, key)
					workMu.Unlock()
				}()
				w.logf("issue #%d started (tasks: %d running, %d queued)", i.Number, running, queued)
				jobOutput := jobOutputWriter{write: func(text string) {
					jobMu.Lock()
					job := jobs[issueKey(i)]
					job.Log += text
					jobs[issueKey(i)] = job
					jobMu.Unlock()
					publish()
				}}
				updateSession := func(update AgentSession) {
					if update.ID == "" && update.CheckoutDirectory == "" {
						return
					}
					workMu.Lock()
					key := issueKey(i)
					state := work[key]
					if update.ID != "" {
						state.SessionID = update.ID
						active[key] = update.ID
					}
					if update.CheckoutDirectory != "" {
						state.CheckoutDirectory = update.CheckoutDirectory
					}
					work[key] = state
					saveErr := saveScopedWorkState(w.StatePath, work, targets)
					workMu.Unlock()
					if saveErr != nil {
						w.logf("issue #%d failed to save agent session: %v", i.Number, saveErr)
					}
					if update.CheckoutDirectory != "" {
						w.recordClone(key, i.Target, i, update.CheckoutDirectory)
					}
					jobMu.Lock()
					job := jobs[key]
					if update.ID != "" {
						job.SessionID = update.ID
					}
					if update.CheckoutDirectory != "" {
						job.CheckoutDirectory = update.CheckoutDirectory
					}
					jobs[key] = job
					jobMu.Unlock()
					publish()
				}
				var runErr error
				// A run that changes nothing about the ticket runs once. One
				// interrupted because the issue changed under it is resumed in
				// place with what changed, so the agent already holding the
				// work receives it rather than the change waiting out the run
				//.
				alreadyClosed := false
				var paused *core.PausedWork
				keepalives := 0
				// stalled counts the keepalives in a row whose turn changed
				// nothing on GitHub, measured against progress, the
				// fingerprint the previous turn left.
				stalled := 0
				progress := ""
				// wait is what the next pass waits out before resuming the
				// session: the wakeup the agent's last turn left pending, or
				// the keepalive's backoff.
				var wait *runWait
				wakeups := 0
				setWaiting := func(from, to, reason string, wakeAt time.Time) {
					jobMu.Lock()
					job := jobs[key]
					if job.Status == from {
						job.Status, job.WaitReason, job.WakeAt = to, reason, wakeAt
						jobs[key] = job
					}
					jobMu.Unlock()
					publish()
				}
				// announced holds the pull requests this job's transcript has
				// already recorded as opened, so a checklist update, a
				// draft-to-ready change, or a poll that briefly misses the
				// pull request does not record it again (#405). It is guarded
				// by jobMu.
				announced := map[string]bool{}
				for {
					runCtx, cancelRun := context.WithCancelCause(ctx)
					workMu.Lock()
					cancellations[key] = cancelRun
					workMu.Unlock()
					watch := issueWatch{since: time.Now(), closed: alreadyClosed, nudge: nudge, resumable: func() bool {
						workMu.Lock()
						defer workMu.Unlock()
						state := work[key]
						return state.SessionID != "" && state.Agent != ""
					}, observed: func(state OriginatingWorkState) {
						jobMu.Lock()
						job, ok := jobs[key]
						changed := ok && job.SetPullRequest(state, issueRepository(i.Target, i))
						label := ""
						if changed {
							jobs[key] = job
							if label = job.PullRequestLabel(); announced[label] {
								label = ""
							}
							announced[label] = true
						}
						jobMu.Unlock()
						if changed {
							if label != "" {
								w.jobEvents(key, transcript.Event{Kind: transcript.KindSystem, Text: "pull request opened: " + label, URL: job.PullRequestURL()})
							}
							publish()
						}
					}}
					var closureReady <-chan struct{}
					if closureChecker != nil || w.Comments != nil {
						ready := make(chan struct{})
						closureReady = ready
						go w.watchForIssueUpdates(runCtx, closureChecker, i, watch, cancelRun, ready)
					}
					if closureReady != nil {
						select {
						case <-closureReady:
						case <-runCtx.Done():
						}
					}
					if w.Comments != nil {
						target := ownershipTargetFor(runCtx, closureChecker, i)
						go w.watchForCompetingClaim(runCtx, target, i.Number, time.Now(), cancelRun)
					}
					waitInterrupted := false
					if pending := wait; pending != nil && runCtx.Err() == nil {
						wait = nil
						delay := pending.Delay
						setWaiting("active", "waiting", pending.Reason, time.Now().Add(delay))
						_, _ = fmt.Fprintf(jobOutput, "Waiting %s for %s before resuming the session.\n", formatWakeupDelay(delay), pending.For)
						jobMu.Lock()
						waitingCheckout := jobs[key].CheckoutDirectory
						jobMu.Unlock()
						waitingRun := w.Active.add(i, waitingCheckout)
						due, closed := w.awaitWakeup(runCtx, closureChecker, i, delay)
						w.Active.remove(waitingRun)
						setWaiting("waiting", "active", "", time.Time{})
						if closed {
							w.logf("issue #%d closed while its agent waited for %s; cancelling the wait", i.Number, pending.For)
							cancelRun(nil)
							workMu.Lock()
							delete(cancellations, key)
							workMu.Unlock()
							break
						}
						waitInterrupted = !due
					}
					activeRunner := w.runner()
					if cause := context.Cause(runCtx); isCooperativeCancellation(cause) || errors.Is(cause, errWorkStoppedFromWebUI) || errors.Is(cause, errWorkUpdated) {
						runErr = cause
					} else if waitInterrupted {
						runErr = cause
					} else if w.UI != nil {
						if runner, ok := activeRunner.(SessionAgentOutputRunner); ok {
							runErr = runner.RunSessionWithOutput(runCtx, i, agentSession, updateSession, jobOutput)
						} else if runner, ok := activeRunner.(AgentOutputRunner); ok {
							runErr = runner.RunWithOutput(runCtx, i, jobOutput)
						} else {
							runErr = activeRunner.Run(runCtx, i)
						}
					} else if runner, ok := activeRunner.(SessionAgentRunner); ok {
						runErr = runner.RunSession(runCtx, i, agentSession, updateSession)
					} else {
						runErr = activeRunner.Run(runCtx, i)
					}
					cause := context.Cause(runCtx)
					if isCooperativeCancellation(cause) || errors.Is(cause, errWorkStoppedFromWebUI) || errors.Is(cause, errWorkUpdated) {
						runErr = cause
					}
					var scheduled *scheduledWakeup
					if errors.As(runErr, &scheduled) {
						runErr = nil
					} else {
						wakeups = 0
					}
					cancelRun(nil)
					workMu.Lock()
					delete(cancellations, key)
					state := work[key]
					workMu.Unlock()
					update, updated := workUpdateFor(cause)
					if !updated || ctx.Err() != nil {
						if scheduled != nil && ctx.Err() == nil {
							// Print mode exits when the turn ends, so the
							// wakeup the agent asked for would never fire and
							// the run would look finished while it waits on
							// CI. The session is kept alive and
							// resumed with the agent's own prompt instead.
							wakeups++
							if wakeups > maxConsecutiveWakeups {
								runErr = fmt.Errorf("agent scheduled %d wakeups in a row without finishing; stopping the run", maxConsecutiveWakeups)
								break
							}
							if state.SessionID != "" && state.Agent != "" {
								w.logf("issue #%d agent scheduled a wakeup in %s (%s); keeping session %s alive until then", i.Number, formatWakeupDelay(scheduled.Delay), scheduled.Reason, state.SessionID)
								wait = &runWait{Delay: w.wakeupDelay(*scheduled), Reason: scheduled.Reason, For: "the scheduled wakeup"}
								agentSession = AgentSession{ID: state.SessionID, Agent: state.Agent, CheckoutDirectory: state.CheckoutDirectory, Resume: true, Update: wakeupPrompt(i, *scheduled)}
								publish()
								continue
							}
							w.logf("issue #%d agent scheduled a wakeup, but its session cannot be resumed; treating the run as finished", i.Number)
						}
						if runErr != nil || ctx.Err() != nil || closureChecker == nil {
							break
						}
						// The agent finishing its prompt is not the work being
						// done: one that timed out or stopped
						// early returns exactly as one that merged its pull
						// request does. The run only counts as complete once
						// GitHub says the issue is closed; while it is still
						// open the work is kept alive and continued instead of
						// being reported as finished.
						closed, answered, held, observed := w.confirmIssueClosed(ctx, closureChecker, i)
						if closed || !answered || ctx.Err() != nil {
							// A run that finished with a ready pull request
							// still open is paused on that pull request, so its
							// slot is freed and a later poll wakes it on a merge
							// or a close. One stacked on the blocker
							// the issue was dispatched with waits for that
							// blocker instead; stacked on anything
							// else, it is a hold for a human like any other.
							if closed && held != nil {
								if held.Kind == core.PausedStacked {
									if dependency, ok := stackableDependency(i); ok {
										held.WaitingOn = []int{dependency.Number}
									} else {
										held.Kind = core.PausedPullRequest
									}
								}
								paused = held
							}
							break
						}
						// An agent that keeps ending its turn on a blocker it
						// cannot get past (auth, permissions, a broken tool)
						// would otherwise be resumed forever, each turn a full
						// prompt. A turn that committed, opened a pull request,
						// or commented is progress; enough turns in a row
						// without any pause the job for the owner instead.
						if current := core.WorkProgress(observed); keepalives > 0 && current == progress {
							stalled++
						} else {
							stalled = 0
							progress = current
						}
						if stalled >= maxStalledKeepalives {
							w.logf("issue #%d keepalive: the agent ended %d turns in a row with the issue still open and no new commits, pull requests, or comments; pausing it until it is retried", i.Number, stalled+1)
							paused = &core.PausedWork{Kind: core.PausedManual, Reason: core.StalledPauseReason}
							break
						}
						keepalives++
						delay := w.keepaliveDelay(stalled)
						wait = &runWait{Delay: delay, Reason: "keepalive backoff after a turn that left the issue open", For: "the keepalive backoff"}
						if state.SessionID == "" || state.Agent == "" {
							w.logf("issue #%d keepalive: the agent finished but the issue is still open, and its session cannot be resumed; restarting the agent in %s (attempt %d)", i.Number, formatWakeupDelay(delay), keepalives)
							agentSession.Resume = false
							agentSession.Update = ""
						} else {
							w.logf("issue #%d keepalive: the agent finished but the issue is still open; continuing session %s in %s (attempt %d)", i.Number, state.SessionID, formatWakeupDelay(delay), keepalives)
							agentSession = AgentSession{ID: state.SessionID, Agent: state.Agent, CheckoutDirectory: state.CheckoutDirectory, Resume: true, Update: unclosedWorkPrompt(i)}
						}
						runErr = nil
						publish()
						continue
					}
					// The session identity is read back from the work state so
					// a run whose agent reported its own session ID after
					// launch is resumed with that one rather than the
					// placeholder it was dispatched with.
					if state.SessionID == "" || state.Agent == "" {
						if update.restart {
							// A rejected pull request leaves the issue open, so
							// the work starts over in a fresh session rather
							// than ending.
							w.logf("issue #%d cannot resume its session to deliver the update; restarting the agent for a fresh attempt", i.Number)
							agentSession.Resume = false
							agentSession.Update = ""
							runErr = nil
							publish()
							continue
						}
						w.logf("issue #%d cannot resume its session to deliver the update; stopping instead", i.Number)
						break
					}
					alreadyClosed = alreadyClosed || update.closed
					agentSession = AgentSession{ID: state.SessionID, Agent: state.Agent, CheckoutDirectory: state.CheckoutDirectory, Resume: true, Update: update.instruction}
					runErr = nil
					w.logf("issue #%d resuming session %s with the update", i.Number, state.SessionID)
					publish()
				}
				workMu.Lock()
				manualPause := pausing[key] && errors.Is(runErr, errWorkStoppedFromWebUI)
				workMu.Unlock()
				if manualPause {
					// The owner paused the run: it is held, keeping its session
					// and its claim on the issue, until the owner retries it.
					// Its project item stays in progress.
					workMu.Lock()
					delete(active, key)
					mentions.agentStopped(i.Repository+"#"+strconv.Itoa(i.Number), time.Now())
					jobMu.Lock()
					job := jobs[key]
					job.Status, job.WaitReason = "paused", core.ManualPauseReason
					jobs[key] = job
					jobMu.Unlock()
					state := work[key]
					state.Status, state.Paused = "paused", &core.PausedWork{Kind: core.PausedManual}
					work[key] = state
					_ = saveScopedWorkState(w.StatePath, work, targets)
					workMu.Unlock()
					tasks.mu.Lock()
					tasks.running--
					running, queued, completed, failed := tasks.running, tasks.queued, tasks.completed, tasks.failed
					tasks.mu.Unlock()
					w.logf("issue #%d paused by owner; its run stopped and frees its slot (tasks: %d running, %d queued, %d completed, %d failed)", i.Number, running, queued, completed, failed)
					publish()
				} else if runErr != nil {
					if w.Status != nil {
						if statusErr := w.Status.SetIssueStatus(context.Background(), i.Target, i, projectReadyState(w.ReadyState, i.ProjectStatus)); statusErr != nil {
							w.logf("issue #%d failed to reset project status: %v", i.Number, statusErr)
						}
					}
					workMu.Lock()
					key := issueKey(i)
					delete(active, key)
					mentions.agentStopped(i.Repository+"#"+strconv.Itoa(i.Number), time.Now())
					jobMu.Lock()
					job := jobs[key]
					job.Status = "failed"
					job.Log += runErr.Error()
					jobs[key] = job
					jobMu.Unlock()
					w.jobEvents(key, transcript.Event{Kind: transcript.KindSystem, Text: "failed: " + firstLine(runErr.Error()), IsError: true})
					state := work[key]
					state.Status = "failed"
					work[key] = state
					_ = saveScopedWorkState(w.StatePath, work, targets)
					workMu.Unlock()
					if errors.Is(runErr, errWorkClaimedByOther) && state.CheckoutDirectory != "" {
						if rmErr := os.RemoveAll(state.CheckoutDirectory); rmErr != nil {
							w.logf("issue #%d failed to remove checkout directory %s: %v", i.Number, state.CheckoutDirectory, rmErr)
						} else {
							w.logf("issue #%d removed checkout directory %s after losing ownership", i.Number, state.CheckoutDirectory)
						}
					}
					tasks.mu.Lock()
					tasks.running--
					tasks.failed++
					running, queued, completed, failed := tasks.running, tasks.queued, tasks.completed, tasks.failed
					tasks.mu.Unlock()
					w.logf("issue #%d failed: %v (tasks: %d running, %d queued, %d completed, %d failed)", i.Number, runErr, running, queued, completed, failed)
					publish()
				} else {
					// Paused work is not done, so its project item stays in
					// progress rather than moving to Done.
					if w.Status != nil && paused == nil {
						if statusErr := w.Status.SetIssueStatus(context.Background(), i.Target, i, "Done"); statusErr != nil {
							w.logf("issue #%d failed to update project status: %v", i.Number, statusErr)
						}
					}
					workMu.Lock()
					key := issueKey(i)
					delete(active, key)
					mentions.agentStopped(i.Repository+"#"+strconv.Itoa(i.Number), time.Now())
					jobMu.Lock()
					job := jobs[key]
					job.Status = "complete"
					if paused != nil {
						job.Status = "paused"
						job.WaitReason = pausedJobReason(*paused)
					} else {
						job.Finish()
					}
					jobs[key] = job
					jobMu.Unlock()
					if paused != nil {
						w.jobSystem(key, "paused: "+job.WaitReason)
					} else {
						w.jobSystem(key, "completed")
					}
					state := work[key]
					state.Status = "completed"
					state.Paused = nil
					if paused != nil {
						state.Status = "paused"
						state.Paused = paused
					}
					work[key] = state
					_ = saveScopedWorkState(w.StatePath, work, targets)
					workMu.Unlock()
					tasks.mu.Lock()
					tasks.running--
					if paused == nil {
						tasks.completed++
					}
					running, queued, completed, failed := tasks.running, tasks.queued, tasks.completed, tasks.failed
					tasks.mu.Unlock()
					if paused != nil && paused.Kind == core.PausedManual {
						w.logf("issue #%d paused: %s; freeing its slot (tasks: %d running, %d queued, %d completed, %d failed)", i.Number, paused.ManualReason(), running, queued, completed, failed)
					} else if paused != nil && paused.Kind == core.PausedStacked {
						w.logf("issue #%d paused: its stacked pull request is ready and waits on its blocker to merge; freeing its slot (tasks: %d running, %d queued, %d completed, %d failed)", i.Number, running, queued, completed, failed)
					} else if paused != nil {
						w.logf("issue #%d paused: its pull request is ready and waits to be merged or closed; freeing its slot (tasks: %d running, %d queued, %d completed, %d failed)", i.Number, running, queued, completed, failed)
					} else {
						w.logf("issue #%d completed (tasks: %d running, %d queued, %d completed, %d failed)", i.Number, running, queued, completed, failed)
					}
					publish()
				}
			}(issue, session, startedRunning, startedQueued)
		}
		running, queued, _, _ = tasks.snapshot()
		w.logf("poll #%d complete; dispatched %d issue(s) (tasks: %d running, %d queued)", n, len(newIssues), running, queued)
		return nil
	}
	// poll runs one poll and reports the end of a run of failures, so a poll
	// loop that recovers says so once even though the failures it recovered
	// from were only reported once.
	poll := func(sweep map[string]bool) error {
		err := pollOnce(sweep)
		if err == nil {
			// Record and publish the check itself, not just what it dispatched:
			// a poll that finds nothing new logs nothing, so this timestamp is
			// what tells the user the run is still alive.
			jobMu.Lock()
			lastPoll = time.Now()
			jobMu.Unlock()
			publish()
			if w.forgetLogged("poll-error") {
				w.logf("poll #%d succeeded; the failure reported above is resolved", pollNumber)
			}
		}
		return err
	}
	// reportPollError reports a failed poll once. Every poll that fails does so
	// through here, so the same failure repeating on every tick is
	// reported when it starts rather than on every tick, and the listing or
	// state error that caused it is reported by this line alone instead of
	// being logged a second time where it was raised.
	reportPollError := func(kind string, err error) {
		if kind != "" {
			kind += " "
		}
		w.logChanged("poll-error", err.Error(), "%spoll #%d error: %v; retrying in %s", kind, pollNumber, err, w.periodicPollInterval())
	}
	// respondToOwnershipAsk answers a "Does anyone have this?" handoff comment
	// the moment its webhook delivery arrives, rather than
	// waiting for the next poll to notice the issue is still active locally.
	respondToOwnershipAsk := func(ctx context.Context, event WebhookEvent) {
		if event.Kind != "issue_comment" || event.Action != "created" || w.Comments == nil {
			return
		}
		kind, id, ok := parseClaim(event.CommentBody)
		if !ok || kind != claimAsking || id == w.Identity {
			return
		}
		workMu.Lock()
		numbers := activeIssuesForRepo(active, event.Repository)
		paused := manuallyPausedIssuesForRepo(work, event.Repository)
		workMu.Unlock()
		// A job the owner paused keeps the issue reserved, so this instance
		// still answers for it.
		owned := slices.Contains(numbers, event.IssueNumber) || slices.Contains(paused, event.IssueNumber)
		// Once a draft pull request exists the handshake moves to it
		// (ownershipTargetFor), so the ask names the pull request number and
		// never matches an active issue number. Checking the open pull
		// requests of the work already in flight is what keeps a busy
		// instance from silently losing its branch.
		if !owned && closureChecker != nil {
			for _, number := range numbers {
				state, err := closureChecker.OriginatingWorkState(ctx, event.Repository, number)
				if err != nil {
					w.logf("issue #%d failed to check whether pull request #%d continues it: %v", number, event.IssueNumber, err)
					continue
				}
				for _, pullRequest := range state.PullRequests {
					if pullRequest.Number != event.IssueNumber || !pullRequest.InRepo(event.Repository) || pullRequest.Merged || strings.EqualFold(pullRequest.State, "closed") {
						continue
					}
					owned = true
					w.logf("issue #%d instance %s asked who owns pull request #%d; it continues our active work", number, id, event.IssueNumber)
					break
				}
				if owned {
					break
				}
			}
		}
		if !owned {
			w.logf("issue #%d instance %s asked who owns it; not ours, staying quiet", event.IssueNumber, id)
			return
		}
		w.logf("issue #%d instance %s asked who owns it; answering %q as %s", event.IssueNumber, id, presenceClaimBody, w.Identity)
		if err := w.Comments.PostComment(ctx, event.Repository, event.IssueNumber, signComment(presenceClaimBody, w.Identity)); err != nil {
			w.logf("issue #%d failed to respond to ownership ask: %v", event.IssueNumber, err)
		}
	}
	// awaitPausedRun replies to a pause once the run it stopped has exited
	// and recorded the pause, after pushing the run's work with the shutdown
	// checkpoint.
	awaitPausedRun := func(key, target string, number int, exited <-chan struct{}, done chan<- error) {
		select {
		case <-exited:
		case <-ctx.Done():
			done <- fmt.Errorf("job is still stopping: %w", ctx.Err())
			return
		}
		workMu.Lock()
		state := work[key]
		workMu.Unlock()
		if state.Status != "paused" || pausedRecord(state).Kind != core.PausedManual {
			done <- fmt.Errorf("job finished before it could be paused")
			return
		}
		git := w.checkpointGit
		if git == nil {
			git = runCheckpointGit
		}
		checkpointCtx, cancel := context.WithTimeout(ctx, checkpointRunBudget)
		result := checkpointCheckout(checkpointCtx, git, checkpointRun{Target: target, Number: number, Checkout: state.CheckoutDirectory})
		cancel()
		if result.Detail != "" {
			w.logf("issue #%d paused from web UI; its agent has exited; checkpoint %s: %s", number, result.Outcome, result.Detail)
		} else {
			w.logf("issue #%d paused from web UI; its agent has exited; checkpoint %s", number, result.Outcome)
		}
		done <- nil
	}
	// serveJobAction answers a dashboard's retry, stop, or pause on one job.
	// dispatching is set while a poll waits for a slot to dispatch queued
	// work, when a retry that needs a poll of its own is refused.
	serveJobAction = func(request jobActionRequest, dispatching bool) {
		action := request.action
		key := action.Target + "#" + strconv.Itoa(action.Number)
		jobMu.Lock()
		job, exists := jobs[key]
		jobMu.Unlock()
		if !exists {
			request.done <- fmt.Errorf("job not found")
			return
		}
		if job.Kind == core.JobKindSchedule {
			serveScheduledAction(request, key, job)
			return
		}
		switch action.Action {
		case "stop":
			if !core.JobActionAvailability(job.Status).Stop {
				request.done <- fmt.Errorf("job cannot be stopped while %s", job.Status)
				return
			}
			if job.Status == "paused" {
				// A paused job has no run to stop, so Stop clears it instead:
				// its record and card are dropped and its claim on the issue
				// is released. A manual pause whose open issue the poll no
				// longer lists can only be ended this way, since Retry has
				// nothing to requeue.
				w.logf("issue #%d stop requested from web UI; clearing its pause and releasing the issue", action.Number)
				workMu.Lock()
				if cancellations[key] != nil {
					workMu.Unlock()
					request.done <- fmt.Errorf("job is still running")
					return
				}
				delete(work, key)
				delete(active, key)
				err := saveScopedWorkState(w.StatePath, work, targets)
				workMu.Unlock()
				// Without a record the issue reads as untouched work, so it is
				// dispatched normally if a later poll lists it again.
				delete(seen, key)
				delete(restored, key)
				jobMu.Lock()
				delete(jobs, key)
				jobMu.Unlock()
				publish()
				issue := Issue{Number: job.Number, Target: job.Target, Repository: job.Repository, Title: job.Title}
				if w.Status != nil {
					if statusErr := w.Status.SetIssueStatus(ctx, issue.Target, issue, projectReadyState(w.ReadyState, "")); statusErr != nil {
						w.logf("issue #%d failed to reset project status: %v", issue.Number, statusErr)
					}
				}
				w.releaseOwnership(ctx, ownershipTargetFor(ctx, closureChecker, issue), "its paused job was cleared from web UI")
				if err != nil {
					err = fmt.Errorf("saving state: %w", err)
				}
				request.done <- err
				return
			}
			workMu.Lock()
			cancel, exited := cancellations[key], runExits[key]
			if cancel != nil {
				stopping[key] = true
			}
			workMu.Unlock()
			if cancel == nil {
				request.done <- fmt.Errorf("job is not running")
				return
			}
			w.logf("issue #%d stop requested", action.Number)
			w.jobSystem(key, "stopped by the owner")
			jobMu.Lock()
			job.Status = "stopping"
			jobs[key] = job
			jobMu.Unlock()
			publish()
			cancel(errWorkStoppedFromWebUI)
			// Cancelling only signals the agent, so the reply waits for
			// the run to exit, off the dispatch loop so it keeps serving
			// everything else meanwhile.
			go w.awaitStoppedRun(ctx, action.Number, exited, request.done)
		case "retry":
			switch job.Status {
			case "active", "waiting":
				workMu.Lock()
				cancel, exited := cancellations[key], runExits[key]
				if cancel != nil {
					stopping[key] = true
				}
				workMu.Unlock()
				if cancel == nil {
					request.done <- fmt.Errorf("job is not running")
					return
				}
				w.logf("issue #%d retry requested from web UI; stopping and rerunning gh-fix", action.Number)
				retryAfterStop[key] = true
				jobMu.Lock()
				job.Status = "stopping"
				jobs[key] = job
				jobMu.Unlock()
				publish()
				cancel(errWorkStoppedFromWebUI)
				go w.awaitStoppedRun(ctx, action.Number, exited, request.done)
				return
			}
			if dispatching && (job.Status == "failed" || job.Status == "complete" || job.Status == "paused") {
				// Requeuing takes a poll, and the poll already running is
				// waiting for a slot to dispatch queued work.
				request.done <- fmt.Errorf("job cannot be retried while aviary waits for a free slot to dispatch queued work; try again once that work starts")
				return
			}
			switch job.Status {
			case "failed", "complete":
				w.logf("issue #%d retry requested from web UI; rerunning gh-fix", action.Number)
				workMu.Lock()
				state := work[key]
				state.Status = "failed"
				work[key] = state
				workMu.Unlock()
				jobMu.Lock()
				job.Status = "failed"
				jobs[key] = job
				jobMu.Unlock()
				request.done <- poll(nil)
			case "paused":
				// Retrying overrides whatever the paused job waits on: its
				// pause is dropped, its session is kept as a failed run's
				// is, and it is requeued. A held pull request is left as it
				// is on GitHub; the rerun picks it up.
				w.logf("issue #%d retry requested from web UI; clearing its pause and rerunning gh-fix", action.Number)
				workMu.Lock()
				paused := work[key]
				state := paused
				state.Status, state.Paused = "failed", nil
				work[key] = state
				workMu.Unlock()
				pausedJob := job
				jobMu.Lock()
				job.Status, job.WaitReason = "failed", ""
				jobs[key] = job
				jobMu.Unlock()
				retriedPaused[key] = true
				err := poll(nil)
				delete(retriedPaused, key)
				workMu.Lock()
				_, requeued := active[key]
				if !requeued {
					// Nothing was requeued, so the pause is put back rather
					// than reporting a retry that changed nothing.
					work[key] = paused
					if saveErr := saveScopedWorkState(w.StatePath, work, targets); saveErr != nil && err == nil {
						err = fmt.Errorf("saving state: %w", saveErr)
					}
				}
				workMu.Unlock()
				if !requeued {
					jobMu.Lock()
					if current, ok := jobs[key]; ok && current.Status == "failed" {
						jobs[key] = pausedJob
					}
					jobMu.Unlock()
					publish()
					if err == nil {
						err = fmt.Errorf("job was not requeued, so it stays paused")
					}
				}
				request.done <- err
			default:
				request.done <- fmt.Errorf("job cannot be retried while %s", job.Status)
			}
		case "pause":
			if !core.JobActionAvailability(job.Status).Pause {
				request.done <- fmt.Errorf("job cannot be paused while %s", job.Status)
				return
			}
			if job.Status == "queued" {
				// A queued job has not started, so pausing it only takes it
				// out of the queue: nothing is dispatched or cancelled, and
				// later polls pass it over until it is retried.
				w.logf("issue #%d pause requested from web UI; holding it out of the queue until it is retried", action.Number)
				workMu.Lock()
				state := work[key]
				state.Status, state.Paused = "paused", &core.PausedWork{Kind: core.PausedManual}
				work[key] = state
				delete(active, key)
				mentions.agentStopped(job.Repository+"#"+strconv.Itoa(job.Number), time.Now())
				err := saveScopedWorkState(w.StatePath, work, targets)
				workMu.Unlock()
				jobMu.Lock()
				job.Status, job.WaitReason = "paused", core.ManualPauseReason
				jobs[key] = job
				jobMu.Unlock()
				publish()
				if err != nil {
					err = fmt.Errorf("saving state: %w", err)
				}
				request.done <- err
				return
			}
			workMu.Lock()
			cancel, exited := cancellations[key], runExits[key]
			if cancel != nil {
				stopping[key] = true
				pausing[key] = true
			}
			workMu.Unlock()
			if cancel == nil {
				request.done <- fmt.Errorf("job is not running")
				return
			}
			// Pausing a running job stops it the way Stop does, then holds
			// it, still reserved, until it is retried.
			w.logf("issue #%d pause requested from web UI; stopping its run and pausing it until it is retried", action.Number)
			jobMu.Lock()
			job.Status = "stopping"
			jobs[key] = job
			jobMu.Unlock()
			publish()
			cancel(errWorkStoppedFromWebUI)
			go awaitPausedRun(key, job.Target, action.Number, exited, request.done)
		default:
			request.done <- fmt.Errorf("unknown job action %q", action.Action)
		}
	}
	// serveScheduledAction answers a dashboard action on a scheduled job:
	// Stop cancels its run, Retry fires its schedule again, and Pause is
	// refused, since a scheduled run has no issue to resume it from.
	serveScheduledAction = func(request jobActionRequest, key string, job JobSnapshot) {
		available := core.JobActionsFor(job)
		switch request.action.Action {
		case "stop":
			if !available.Stop {
				request.done <- fmt.Errorf("job cannot be stopped while %s", job.Status)
				return
			}
			workMu.Lock()
			cancel, exited := cancellations[key], runExits[key]
			workMu.Unlock()
			if cancel == nil {
				request.done <- fmt.Errorf("job is not running")
				return
			}
			w.logf("scheduled run %s stop requested", key)
			w.jobSystem(key, "stopped by the owner")
			jobMu.Lock()
			job.Status = "stopping"
			jobs[key] = job
			jobMu.Unlock()
			publish()
			cancel(errWorkStoppedFromWebUI)
			go func() {
				select {
				case <-exited:
					request.done <- nil
				case <-ctx.Done():
					request.done <- fmt.Errorf("job is still stopping: %w", ctx.Err())
				}
			}()
		case "retry":
			if !available.Retry {
				request.done <- fmt.Errorf("job cannot be retried: %s", available.RetryReason)
				return
			}
			w.logf("scheduled run %s retry requested; firing schedule %s again", key, job.ScheduleID)
			request.done <- w.rerunSchedule(job.ScheduleID)
		case "pause":
			request.done <- fmt.Errorf("scheduled jobs cannot be paused; stop it instead")
		default:
			request.done <- fmt.Errorf("unknown job action %q", request.action.Action)
		}
	}
	startScheduled = func(request scheduledRunRequest) {
		run := request.run
		number := int(run.Fire.Unix())
		jobMu.Lock()
		for {
			if _, taken := jobs[run.Target+"#"+strconv.Itoa(number)]; !taken {
				break
			}
			number++
		}
		key := run.Target + "#" + strconv.Itoa(number)
		session := AgentSession{Agent: run.Agent}
		if session.Agent == "" {
			if identified, ok := w.runner().(TargetAgentIdentifier); ok {
				session.Agent = identified.AgentNameFor(run.Target)
			} else if identified, ok := w.runner().(AgentIdentifier); ok {
				session.Agent = identified.AgentName()
			}
		}
		spec, _ := parseAgentSpec(session.Agent)
		jobs[key] = JobSnapshot{Kind: core.JobKindSchedule, ScheduleID: run.ScheduleID, Number: number, Target: run.Target, Repository: run.Repository, Title: run.Name, Status: "queued", Agent: spec.Name, Model: spec.Model, Effort: spec.Level, Started: time.Now()}
		jobMu.Unlock()
		issue := Issue{Number: number, Target: run.Target, Repository: run.Repository, Title: run.Name, Prompt: run.Prompt, ScheduleID: run.ScheduleID}
		runCtx, cancelRun := context.WithCancelCause(ctx)
		stopWatching := context.AfterFunc(request.ctx, func() { cancelRun(context.Cause(request.ctx)) })
		exited := make(chan struct{})
		workMu.Lock()
		cancellations[key] = cancelRun
		runExits[key] = exited
		workMu.Unlock()
		tasks.mu.Lock()
		tasks.queued++
		tasks.mu.Unlock()
		w.logf("scheduled run %s queued for schedule %s", key, run.ScheduleID)
		publish()
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := ScheduledRunResult{Job: number}
			defer func() {
				stopWatching()
				cancelRun(nil)
				workMu.Lock()
				delete(cancellations, key)
				delete(runExits, key)
				workMu.Unlock()
				close(exited)
				request.done <- result
			}()
			acquired := sem.acquire(runCtx) == nil
			tasks.mu.Lock()
			tasks.queued--
			if acquired {
				tasks.running++
			}
			tasks.mu.Unlock()
			var runErr error
			if acquired {
				defer func() {
					sem.release()
					select {
					case workFinished <- struct{}{}:
					default:
					}
				}()
				if agentAssignsSessionID(session.Agent) {
					session.ID, runErr = newSessionID()
				}
				jobMu.Lock()
				job := jobs[key]
				job.Status, job.SessionID = "active", session.ID
				jobs[key] = job
				jobMu.Unlock()
				w.jobSystem(key, "scheduled run started on "+session.Agent)
				publish()
				output := jobOutputWriter{write: func(text string) {
					jobMu.Lock()
					job := jobs[key]
					job.Log += text
					jobs[key] = job
					jobMu.Unlock()
					publish()
				}}
				updateSession := func(update AgentSession) {
					jobMu.Lock()
					job := jobs[key]
					if update.ID != "" {
						job.SessionID = update.ID
					}
					if update.CheckoutDirectory != "" {
						job.CheckoutDirectory = update.CheckoutDirectory
					}
					jobs[key] = job
					jobMu.Unlock()
					publish()
				}
				if runErr == nil {
					switch runner := w.runner().(type) {
					case SessionAgentOutputRunner:
						runErr = runner.RunSessionWithOutput(runCtx, issue, session, updateSession, output)
					case AgentOutputRunner:
						runErr = runner.RunWithOutput(runCtx, issue, output)
					default:
						runErr = runner.Run(runCtx, issue)
					}
				}
			} else {
				runErr = context.Cause(runCtx)
			}
			jobMu.Lock()
			job := jobs[key]
			result = scheduledRunOutcome(runErr, context.Cause(runCtx), job.Log)
			result.Job = number
			job.Status = scheduledJobStatus(result)
			if result.Status != ScheduledRunComplete {
				job.Log += result.Summary
			}
			jobs[key] = job
			jobMu.Unlock()
			tasks.mu.Lock()
			if acquired {
				tasks.running--
			}
			if result.Status == ScheduledRunComplete {
				tasks.completed++
			} else {
				tasks.failed++
			}
			tasks.mu.Unlock()
			w.jobSystem(key, "scheduled run "+result.Status)
			w.logf("scheduled run %s %s: %s", key, result.Status, result.Summary)
			publish()
		}()
	}
	// Signal readiness now, immediately before the first dispatch. Initial
	// dispatch already services settingsRequestsChan and job actions itself
	// through acquireDispatchSlot while it blocks on a full concurrency
	// semaphore, so requests are handled correctly from here on even though
	// the main select loop below is still a moment away. Everything above --
	// loading work state, resetFailedWork's GitHub calls, and in webhook
	// mode the ngrok tunnel and per-target webhook configuration in
	// main.go's run() -- can take a real amount of time, and a settings or
	// job-action request that lands during that window must not block on a
	// channel nothing is reading yet.
	close(w.readyChan())
	if err := poll(nil); err != nil {
		if ctx.Err() != nil {
			wg.Wait()
			w.logf("stopped during initial poll")
			return nil
		}
		reportPollError("initial", err)
	}
	publish()
	// Board-only project changes never reach a webhook, so push mode probes a
	// cheap board fingerprint instead. Seeding it here (rather than on the
	// first tick) means an idle board costs one small request per probe and
	// never triggers a redundant full poll at startup.
	probedTargets := w.projectProbeTargets(targets)
	boardState := make(map[string]string, len(probedTargets))
	probeBoards := func(ctx context.Context) bool {
		changed := false
		for _, target := range probedTargets {
			state, err := w.Projects.ProjectState(ctx, target)
			if err != nil {
				if ctx.Err() == nil {
					w.logChanged("board-probe:"+target, err.Error(), "project board probe for %s failed: %v", target, err)
				}
				continue
			}
			previous, known := boardState[target]
			boardState[target] = state
			if known && previous != state {
				w.logf("project board change detected for %s; refreshing", target)
				changed = true
			}
		}
		return changed
	}
	probeBoards(ctx)
	var ticker *time.Ticker
	go w.watchPollProgress(watchCtx, w.periodicPollInterval(), time.Now(), func() time.Time {
		jobMu.Lock()
		defer jobMu.Unlock()
		return lastPoll
	})
	go w.watchStuckJobs(watchCtx, func() []JobSnapshot {
		jobMu.Lock()
		defer jobMu.Unlock()
		return publishedJobs(jobs)
	})
	wakes := watchForWake(watchCtx)
	var tick <-chan time.Time
	var retryTimer *time.Timer
	var retry <-chan time.Time
	retriesRemaining := 0
	// pendingWebhookIssue names the issue whose delivery scheduled the current
	// follow-up chain, so the chain can stop early once a refresh sees it.
	pendingWebhookIssue := ""
	// pendingWebhookSweep preserves referenced continuation work across the
	// follow-up chain, including while GitHub's issue/dependency indexes lag.
	var pendingWebhookSweep map[string]bool
	var stateReloadTimer *time.Timer
	var stateReload <-chan time.Time
	defer func() {
		if stateReloadTimer != nil {
			stateReloadTimer.Stop()
		}
	}()
	// Keep periodic reconciliation active in webhook mode so issue state that
	// changes without a delivery can still be recovered.
	periodicInterval := w.periodicPollInterval()
	ticker = time.NewTicker(periodicInterval)
	defer ticker.Stop()
	tick = ticker.C
	var boardTick <-chan time.Time
	if len(probedTargets) > 0 {
		boardTicker := time.NewTicker(w.projectBoardProbeInterval())
		defer boardTicker.Stop()
		boardTick = boardTicker.C
		w.logf("probing %d project board(s) for board-only changes every %s", len(probedTargets), w.projectBoardProbeInterval())
	}
	if pushed := w.pushedBoardTargets(targets); len(pushed) > 0 {
		w.logf("not probing %d project board(s) that push board changes over webhooks: %s", len(pushed), strings.Join(pushed, ", "))
	}
	// Reaping abandoned work rides along with polling, so when polling is
	// slower than reapPollInterval (webhook push mode) it gets its own,
	// faster ticker.
	var reap <-chan time.Time
	if reapTick := w.reapPollTick(); reapTick > 0 {
		reapTicker := time.NewTicker(reapTick)
		defer reapTicker.Stop()
		reap = reapTicker.C
		w.logf("reaping abandoned work every %s", reapTick)
	}
	// Paused work is watched on its own cadence, so when polling is slower
	// than that (webhook push mode) the watch gets its own ticker (issue
	// #769).
	var pausedWatch <-chan time.Time
	if pausedTick := w.pausedWatchTick(); pausedTick > 0 {
		pausedTicker := time.NewTicker(pausedTick)
		defer pausedTicker.Stop()
		pausedWatch = pausedTicker.C
		w.logf("watching paused work every %s", pausedTick)
	}
	for {
		select {
		case request := <-w.settingsRequestsChan():
			request.done <- w.applySettingsRequest(request.update, sem)
		case request := <-w.jobActionRequests():
			serveJobAction(request, false)
		case request := <-w.scheduledRunRequests():
			startScheduled(request)
		case <-ctx.Done():
			w.logf("shutdown requested; waiting for running tasks to finish")
			wg.Wait()
			running, queued, completed, failed := tasks.snapshot()
			w.logf("stopped (tasks: %d running, %d queued, %d completed, %d failed)", running, queued, completed, failed)
			return nil
		case <-w.negotiatedNudges():
			// A background handshake just won its ticket. Poll now so the
			// reclaimed work is dispatched instead of waiting out a tick that
			// may be an hour away, or the next reap.
			if err := poll(nil); err != nil && ctx.Err() == nil {
				reportPollError("handoff", err)
			}
		case <-tick:
			if w.Webhooks != nil {
				w.Webhooks(ctx)
				// The tunnel check can change whether webhooks are online,
				// and a poll that fails will not publish it.
				publish()
			}
			if err := poll(nil); err != nil {
				if ctx.Err() != nil {
					w.logf("shutdown requested during poll; waiting for running tasks to finish")
					wg.Wait()
					w.logf("stopped")
					return nil
				}
				reportPollError("", err)
			}
		case slept := <-wakes:
			// Connections made before the machine slept may not have
			// survived it, and the poll it slept through is overdue, so
			// both are dealt with now rather than on the next tick, which
			// can be an hour away.
			w.logf("machine woke from about %s of sleep; checking connections and refreshing", formatInterval(slept.Round(time.Second)))
			if w.Recover != nil {
				w.Recover(ctx)
			}
			if w.Webhooks != nil {
				w.Webhooks(ctx)
				// The tunnel check can change whether webhooks are online,
				// and a poll that fails will not publish it.
				publish()
			}
			if err := poll(nil); err != nil && ctx.Err() == nil {
				reportPollError("wake", err)
			}
		case <-boardTick:
			if !probeBoards(ctx) {
				continue
			}
			if err := poll(nil); err != nil && ctx.Err() == nil {
				reportPollError("project board", err)
			}
		case <-reap:
			w.logf("periodic reap started")
			if err := poll(nil); err != nil && ctx.Err() == nil {
				reportPollError("reap", err)
			}
		case <-pausedWatch:
			// A poll that ran the watch moments ago already covered it.
			if time.Since(lastPausedWatch) < w.pausedWatchEvery()/2 {
				continue
			}
			forcePausedWatch = true
			if err := poll(nil); err != nil && ctx.Err() == nil {
				reportPollError("paused watch", err)
			}
		case event := <-w.Events:
			w.logWebhookEvent(event)
			respondToOwnershipAsk(ctx, event)
			// A delivery about an issue an agent is already working is not new
			// work to dispatch; it is a change that agent needs to hear about,
			// so its watcher is asked to check GitHub now.
			if webhookEventUpdatesActiveWork(event) {
				workMu.Lock()
				pending := nudgesForIssue(nudges, event.Repository, event.IssueNumber)
				workMu.Unlock()
				for _, nudge := range pending {
					select {
					case nudge <- struct{}{}:
						w.logf("issue #%d %s delivery concerns work in flight; checking it now", event.IssueNumber, webhookEventLabel(event))
					default:
					}
				}
			}
			// A direct mention is intentionally not an ordinary comment refresh:
			// it must survive a lagging issue search (or a transient list
			// failure) until the mentioned issue is actually dispatched. The
			// normal webhook retry chain stops as soon as an issue is merely
			// observed, which is insufficient here because local completed or
			// active state can still defer the fresh threaded run.
			followQueuedRun := func(key string, number int) {
				if !directMentions[key] {
					return
				}
				pendingWebhookIssue = key
				retriesRemaining = webhookRetryLimit
				if retryTimer == nil {
					retryTimer = time.NewTimer(w.Interval)
					retry = retryTimer.C
					w.logf("issue #%d direct mention is still pending; scheduling follow-up refresh", number)
				} else {
					w.logf("issue #%d direct mention is still pending; using the scheduled follow-up refresh", number)
				}
			}
			if kind, ok := webhookMentionKind(event); ok && mentionedIdentity(event.CommentBody, w.Identity) {
				// Acknowledge the mention was read regardless of whether it goes on
				// to authorize a run: a human watching the thread should
				// see the eyes reaction even when the mention is stale or from an
				// unauthorized commenter, since either way this instance saw it.
				w.reactToMention(ctx, event.Repository, kind, event.IssueNumber, event.CommentID, event.CommentNodeID)
				// A mention in the webhook payload alone is not enough to trigger a
				// run: the mentioning comment must also come from an
				// allowed commenter and be on GitHub as delivered, reverified there
				// rather than trusted from the delivery. A conversation comment
				// must also still be the thread's last comment.
				authorized := commenterAllowed(event.CommentAuthor, w.AllowedCommenters)
				if authorized && w.Comments != nil {
					var err error
					if kind == ConversationComment {
						authorized, err = authorizedDirectMention(ctx, w.Comments, event.Repository, event.IssueNumber, w.Identity, w.AllowedCommenters)
					} else if lister, ok := w.Comments.(ReviewCommentLister); ok {
						authorized, err = reviewMentionOnGitHub(ctx, lister, event.Repository, event.IssueNumber, event.CommentBody, event.CommentAuthor)
					}
					if err != nil {
						w.logf("issue #%d failed to verify direct mention of instance %s%s: %v", event.IssueNumber, w.Identity, mentionLocationSuffix(kind), err)
						authorized = false
					}
				}
				keys := webhookMentionKeys(event)
				if event.OnPullRequest {
					keys = mentions.linkedIssueKeys(event.Repository, event.IssueNumber, keys)
				}
				if !authorized && kind == ConversationComment {
					w.logf("issue #%d ignoring direct mention of instance %s: not the last comment or not from an allowed commenter", event.IssueNumber, w.Identity)
				} else if !authorized {
					w.logf("pull request #%d ignoring direct mention of instance %s in %s: not from an allowed commenter or not found on GitHub", event.IssueNumber, w.Identity, mentionLocation(kind))
				} else if event.OnPullRequest && len(keys) == 0 {
					w.logf("pull request #%d directly mentioned instance %s%s, but it closes no issue to run", event.IssueNumber, w.Identity, mentionLocationSuffix(kind))
				} else if len(keys) > 0 {
					key := keys[0]
					for _, mentioned := range keys {
						directMentions[mentioned] = true
						delete(mentions.rechecks, mentioned)
						mentions.comments[mentioned] = mentionComment{Kind: kind, Repository: event.Repository, Number: event.IssueNumber, ID: event.CommentID, Author: event.CommentAuthor}
					}
					if event.OnPullRequest {
						w.logf("pull request #%d directly mentioned instance %s%s; refreshing for a threaded gh-fix run on %s", event.IssueNumber, w.Identity, mentionLocationSuffix(kind), formatIssueRefs(event.ClosesIssues))
					} else {
						w.logf("issue #%d directly mentioned instance %s; refreshing for a threaded gh-fix run", event.IssueNumber, w.Identity)
					}
					if err := poll(nil); err != nil && ctx.Err() == nil {
						reportPollError("direct-mention", err)
					}
					followQueuedRun(key, event.IssueNumber)
					continue
				}
			}
			if kind, ok := webhookMentionKind(event); ok && !mentionedIdentity(event.CommentBody, w.Identity) && w.commentRequestsRecheck(event.CommentBody, event.CommentAuthor) {
				// The delivery marks the comment as new, so only its author, its
				// mention of the login, and its presence on GitHub need checking
				//.
				verified, err := w.commentOnGitHub(ctx, kind, event.Repository, event.IssueNumber, event.CommentBody, event.CommentAuthor)
				if err != nil {
					w.logf("issue #%d failed to verify comment mentioning %s: %v", event.IssueNumber, w.SelfLogin, err)
				} else if !verified {
					w.logf("issue #%d ignoring comment mentioning %s: not found on GitHub", event.IssueNumber, w.SelfLogin)
				} else {
					keys := webhookMentionKeys(event)
					if event.OnPullRequest {
						keys = mentions.linkedIssueKeys(event.Repository, event.IssueNumber, keys)
					}
					if slices.ContainsFunc(keys, func(key string) bool { return !observed[key] }) {
						if err := poll(nil); err != nil && ctx.Err() == nil {
							reportPollError("comment-recheck", err)
						}
					}
					if queued := w.queueCommentRecheck(mentions, keys, mentionComment{Kind: kind, Repository: event.Repository, Number: event.IssueNumber, ID: event.CommentID, Author: event.CommentAuthor}, time.Now(), observed, directMentions); len(queued) > 0 {
						w.reactToMention(ctx, event.Repository, kind, event.IssueNumber, event.CommentID, event.CommentNodeID)
						if err := poll(nil); err != nil && ctx.Err() == nil {
							reportPollError("comment-recheck", err)
						}
						followQueuedRun(queued[0], event.IssueNumber)
						continue
					}
				}
			}
			// The payload alone decides whether this delivery could have
			// changed dispatchable work. Pushes, pull request activity, and
			// ordinary comments never do, so they no longer cost a refresh.
			if !webhookEventNeedsRefresh(event) {
				w.logf("webhook %s delivery cannot change issue state; skipping refresh", webhookEventLabel(event))
				continue
			}
			sweep := webhookContinuationSweep(event)
			if len(sweep) > 0 {
				keys := slices.Collect(maps.Keys(sweep))
				slices.Sort(keys)
				w.logf("webhook %s delivery sweeping referenced work: %s", webhookEventLabel(event), strings.Join(keys, ", "))
				if pendingWebhookSweep == nil {
					pendingWebhookSweep = make(map[string]bool, len(sweep))
				}
				for key := range sweep {
					pendingWebhookSweep[key] = true
				}
			}
			if err := poll(sweep); err != nil {
				if ctx.Err() != nil {
					wg.Wait()
					return nil
				}
				reportPollError("webhook-triggered", err)
			}
			// Keep an already scheduled follow-up refresh. GitHub may deliver
			// another webhook while its issue index is still catching up; resetting
			// the timer in that case can make the refresh observe the previous
			// issue and miss the newest one until another delivery arrives.
			if retryTimer == nil {
				if key, ok := webhookIssueKey(event); ok && observed[key] {
					// The refresh above already saw the delivered issue, so
					// there is no index lag left for follow-ups to outlast.
					pendingWebhookSweep = nil
					continue
				}
				pendingWebhookIssue, _ = webhookIssueKey(event)
				retryTimer = time.NewTimer(w.Interval)
				retry = retryTimer.C
				retriesRemaining = webhookRetryLimit
			}
		case <-retry:
			retryTimer = nil
			w.logf("webhook follow-up refresh started")
			if err := poll(pendingWebhookSweep); err != nil && ctx.Err() == nil {
				reportPollError("webhook follow-up", err)
			}
			retriesRemaining--
			if pendingWebhookIssue != "" && !directMentions[pendingWebhookIssue] && observed[pendingWebhookIssue] {
				w.logf("webhook follow-up refreshes complete; issue %s observed", pendingWebhookIssue)
				retriesRemaining = 0
			}
			if retriesRemaining > 0 {
				retryTimer = time.NewTimer(w.Interval)
				retry = retryTimer.C
			} else {
				pendingWebhookIssue = ""
				pendingWebhookSweep = nil
				retry = nil
			}
		case <-workFinished:
			for key := range retryAfterStop {
				jobMu.Lock()
				status := jobs[key].Status
				jobMu.Unlock()
				if status == "failed" {
					delete(retryAfterStop, key)
					if err := poll(nil); err != nil && ctx.Err() == nil {
						reportPollError("retry", err)
					}
				}
			}
			if len(directMentions) > 0 {
				if err := poll(nil); err != nil && ctx.Err() == nil {
					reportPollError("queued direct-mention", err)
				}
			}
		case <-stateChanges:
			if stateReloadTimer == nil {
				stateReloadTimer = time.NewTimer(stateReloadDebounce)
				stateReload = stateReloadTimer.C
			} else {
				if !stateReloadTimer.Stop() {
					select {
					case <-stateReloadTimer.C:
					default:
					}
				}
				stateReloadTimer.Reset(stateReloadDebounce)
			}
		case <-stateReload:
			stateReloadTimer = nil
			stateReload = nil
			reloaded, loadErr := loadScopedWorkState(w.StatePath, targets)
			if loadErr != nil {
				w.logf("state reload failed: %v", loadErr)
				continue
			}
			workMu.Lock()
			for key, session := range active {
				state := work[key]
				state.Status = "active"
				state.SessionID = session
				reloaded[key] = state
			}
			work = reloaded
			seen = make(map[string]bool, len(work))
			for key := range work {
				seen[key] = true
			}
			workMu.Unlock()
			w.logf("state reloaded; scheduling resync")
			if err := poll(nil); err != nil && ctx.Err() == nil {
				reportPollError("state reload", err)
			}
		}
	}
}

func watchStateFile(ctx context.Context, path string) <-chan struct{} {
	if path == "" {
		return nil
	}
	changes := make(chan struct{}, 1)
	previous := stateFileFingerprint(path)
	go func() {
		defer close(changes)
		for {
			current := stateFileFingerprint(path)
			if current != previous {
				select {
				case changes <- struct{}{}:
				default:
				}
				previous = current
			}
			timer := time.NewTimer(stateFilePollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return changes
}

func stateFileFingerprint(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// webhookEventNeedsRefresh reports whether a delivery could have changed which
// issues are dispatchable, using only the payload aviary already decoded. A
// closed pull request can unblock referenced work even when it does not close
// an issue, so it refreshes immediately. Push, ping, other pull request
// activity, and comments do not. Unrecognized event kinds refresh so a newly
// subscribed event is never silently dropped.
func webhookEventNeedsRefresh(event WebhookEvent) bool {
	switch event.Kind {
	case "push", "ping", "issue_comment", "pull_request_review", "pull_request_review_comment":
		return false
	case "pull_request":
		return event.Action == "closed"
	case "issues":
		switch event.Action {
		case "opened", "reopened", "closed", "deleted", "transferred", "labeled", "unlabeled":
			return true
		default:
			// edited, assigned, milestoned, locked, pinned, and friends leave
			// every dispatch input (labels, state, dependencies) untouched.
			return false
		}
	case "projects_v2_item":
		// Only reordering leaves an item's status untouched; every other
		// action can move it into or out of the ready state.
		return event.Action != "reordered"
	case "discussion":
		switch event.Action {
		case "created", "reopened", "transferred":
			return true
		default:
			// A discussion is dispatchable purely on existing with no reply
			// yet, so edits, labels, pins, and answer changes cannot make a
			// thread newly answerable.
			return false
		}
	default:
		return true
	}
}

// webhookEventUpdatesActiveWork reports whether a delivery could have changed
// the issue itself under an agent already working it. These are
// deliberately the deliveries webhookEventNeedsRefresh ignores: an edited
// description and a new comment change nothing about which issues are
// dispatchable, but they change what the agent holding this one was asked to
// do. A closure is both, and counts here as well.
func webhookEventUpdatesActiveWork(event WebhookEvent) bool {
	if event.Repository == "" || event.IssueNumber <= 0 {
		return false
	}
	switch event.Kind {
	case "issue_comment":
		return event.Action == "created" || event.Action == "edited"
	case "issues":
		return event.Action == "edited" || event.Action == "closed"
	default:
		return false
	}
}

// nudgesForIssue returns the nudge channels of the runs working repo#number.
// The keys are target scoped, so a project board target watching the same
// issue is matched by the repository it resolves to rather than by the key.
func nudgesForIssue(nudges map[string]chan struct{}, repo string, number int) []chan struct{} {
	if repo == "" || number <= 0 {
		return nil
	}
	var matched []chan struct{}
	for key, nudge := range nudges {
		target, keyNumber, ok := parseIssueWorkKey(key)
		if !ok || keyNumber != number || !strings.EqualFold(issueRepository(target, Issue{}), repo) {
			continue
		}
		matched = append(matched, nudge)
	}
	return matched
}

// webhookContinuationSweep identifies referenced issues that should be
// reconsidered as continuation work when an issue or pull request closes.
func webhookContinuationSweep(event WebhookEvent) map[string]bool {
	if event.Repository == "" || event.Action != "closed" || (event.Kind != "issues" && event.Kind != "pull_request") {
		return nil
	}
	sweep := make(map[string]bool, len(event.MentionedIssues))
	for _, number := range event.MentionedIssues {
		if number <= 0 || (event.Kind == "issues" && number == event.IssueNumber) {
			continue
		}
		sweep[event.Repository+"#"+strconv.Itoa(number)] = true
	}
	return sweep
}

// webhookMentionKind reports whether a delivery is a newly posted comment a
// direct mention could be in, and which kind: an issue or pull request
// conversation comment, an inline review comment, or a submitted review's
// body.
func webhookMentionKind(event WebhookEvent) (CommentKind, bool) {
	switch {
	case event.Kind == "issue_comment" && event.Action == "created":
		return ConversationComment, true
	case event.Kind == "pull_request_review_comment" && event.Action == "created":
		return ReviewComment, true
	case event.Kind == "pull_request_review" && event.Action == "submitted":
		return ReviewBody, true
	default:
		return ConversationComment, false
	}
}

// webhookIssueKey returns the "repo#number" key the delivery refers to, when it
// names one.
// webhookMentionKeys names the "repo#number" keys a direct mention in an
// issue_comment delivery addresses: the issue it was posted on, or for a
// comment on a pull request, the issues that pull request closes (issue
// #689).
func webhookMentionKeys(event WebhookEvent) []string {
	if !event.OnPullRequest {
		if key, ok := webhookIssueKey(event); ok {
			return []string{key}
		}
		return nil
	}
	if event.Repository == "" {
		return nil
	}
	keys := make([]string, 0, len(event.ClosesIssues))
	for _, number := range event.ClosesIssues {
		keys = append(keys, event.Repository+"#"+strconv.Itoa(number))
	}
	return keys
}

func webhookIssueKey(event WebhookEvent) (string, bool) {
	if event.Repository == "" || event.IssueNumber <= 0 {
		return "", false
	}
	return event.Repository + "#" + strconv.Itoa(event.IssueNumber), true
}

// webhookEventLabel names a delivery for logging, including its action when the
// payload carried one.
func webhookEventLabel(event WebhookEvent) string {
	if event.Action == "" {
		return event.Kind
	}
	return event.Kind + "/" + event.Action
}

func (w *Engine) logWebhookEvent(event WebhookEvent) {
	switch event.Kind {
	case "push":
		w.logf("webhook push received (repository: %s, ref: %s, before: %s, after: %s, commits: %d)", event.Repository, event.Ref, event.Before, event.After, event.CommitCount)
	case "issues":
		w.logf("webhook issues received (repository: %s, action: %s, issue: #%d %q)", event.Repository, event.Action, event.IssueNumber, event.IssueTitle)
	case "projects_v2_item":
		w.logf("webhook project item received (action: %s)", event.Action)
	case "issue_comment":
		w.logf("webhook issue comment received (repository: %s, action: %s, issue: #%d)", event.Repository, event.Action, event.IssueNumber)
	case "pull_request_review_comment":
		w.logf("webhook review comment received (repository: %s, action: %s, pull request: #%d)", event.Repository, event.Action, event.IssueNumber)
	case "pull_request_review":
		w.logf("webhook review received (repository: %s, action: %s, pull request: #%d)", event.Repository, event.Action, event.IssueNumber)
	case "discussion":
		w.logf("webhook discussion received (repository: %s, action: %s, discussion: #%d %q)", event.Repository, event.Action, event.DiscussionNumber, event.DiscussionTitle)
	default:
		w.logf("webhook %s received", event.Kind)
	}
}

func (w *Engine) resetFailedWork(ctx context.Context, work map[string]workState) error {
	for key, state := range work {
		if state.Status != "failed" {
			continue
		}
		separator := strings.LastIndexByte(key, '#')
		if separator <= 0 {
			return fmt.Errorf("invalid failed work key %q", key)
		}
		target := key[:separator]
		number, err := strconv.Atoi(key[separator+1:])
		if err != nil {
			return fmt.Errorf("invalid failed work key %q: %w", key, err)
		}
		issue := Issue{Number: number, Target: target}
		if w.Status != nil {
			readyState := projectReadyState(w.ReadyState, "")
			if err := w.Status.SetIssueStatus(ctx, target, issue, readyState); err != nil {
				if isProjectTarget(target) && errors.Is(err, errProjectIssueNotFound) {
					w.logf("reset failed issue #%d skipped because it is no longer in project", number)
					continue
				}
				w.logf("reset failed issue #%d project status: %v", number, err)
				continue
			}
			w.logf("reset failed issue #%d to %s", number, readyState)
			continue
		}
		w.logf("reset failed issue #%d", number)
	}
	return nil
}

func issueNumbers(issues []Issue) string {
	numbers := make([]string, len(issues))
	for i, issue := range issues {
		numbers[i] = fmt.Sprintf("#%d", issue.Number)
	}
	return strings.Join(numbers, ", ")
}

func workStateMatchesRemote(target string, issue Issue, state workState) bool {
	switch state.Status {
	case "active":
		if isProjectTarget(target) {
			return strings.EqualFold(strings.TrimSpace(issue.ProjectStatus), "In Progress")
		}
		// Repository work state persists only in this instance's own
		// .aviary.json, and there is no remote label left to cross-check.
		// Trust it only when it carries enough identity to actually resume
		// (a session ID and agent); an incomplete record can't be resumed
		// reliably, so treat it as stale and let a fresh pickup negotiate
		// ownership through the comment protocol instead.
		return state.SessionID != "" && state.Agent != ""
	case "completed":
		if !isProjectTarget(target) {
			// Repository issue queries only return open issues, so an issue present
			// in the batch cannot still be completed remotely.
			return false
		}
		status := strings.TrimSpace(issue.ProjectStatus)
		return strings.EqualFold(status, "Done") || strings.EqualFold(status, "Completed")
	default:
		return true
	}
}

func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("create agent session: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

type projectItem struct {
	ID      string          `json:"id"`
	Content *projectContent `json:"content"`
	Status  string          `json:"status"`
}

type projectContent struct {
	Issue
	Type string `json:"type"`
}

type projectList struct {
	Items []projectItem `json:"items"`
}

func decodeProjectIssues(data []byte, err error) ([]Issue, error) {
	items, decodeErr := decodeProjectItems(data, err)
	if decodeErr != nil {
		return nil, decodeErr
	}
	return issuesFromProjectItems(items), nil
}

func issuesFromProjectItems(items []projectItem) []Issue {
	issues := make([]Issue, 0, len(items))
	for _, item := range items {
		if item.Content != nil && item.Content.Type == "Issue" {
			issue := item.Content.Issue
			issue.ProjectStatus = item.Status
			issue.ProjectItemID = item.ID
			issues = append(issues, issue)
		}
	}
	return issues
}

func isProjectTarget(repo string) bool { return core.IsProjectTarget(repo) }

func isDiscussionTarget(repo string) bool { return core.IsDiscussionTarget(repo) }

func decodeProjectItems(data []byte, err error) ([]projectItem, error) {
	if err != nil {
		detail := strings.TrimSpace(string(data))
		if strings.Contains(detail, "missing required scopes") && strings.Contains(detail, "read:project") {
			return nil, fmt.Errorf("list project items: %w; authenticate with the read:project scope using `gh auth refresh -s read:project`", err)
		}
		if detail != "" {
			return nil, fmt.Errorf("list project items: %w: %s", err, detail)
		}
		return nil, fmt.Errorf("list project items: %w", err)
	}
	var result projectList
	if err := json.Unmarshal(data, &result); err == nil && result.Items != nil {
		return result.Items, nil
	}
	var items []projectItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("decode project items: %w", err)
	}
	return items, nil
}

func decodeProjectFields(data []byte, err error) ([]projectField, error) {
	if err != nil {
		if detail := strings.TrimSpace(string(data)); detail != "" {
			return nil, fmt.Errorf("list project fields: %w: %s", err, detail)
		}
		return nil, fmt.Errorf("list project fields: %w", err)
	}
	var result projectFields
	if decodeErr := json.Unmarshal(data, &result); decodeErr == nil && result.Fields != nil {
		return result.Fields, nil
	}
	var fields []projectField
	if decodeErr := json.Unmarshal(data, &fields); decodeErr != nil {
		return nil, fmt.Errorf("decode project fields: %w", decodeErr)
	}
	return fields, nil
}
func loadState(path string) (map[int]bool, error) {
	if path == "" {
		return map[int]bool{}, nil
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[int]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}
	s := make(map[int]bool, len(raw))
	for key, value := range raw {
		var number int
		if _, err := fmt.Sscanf(key, "%d", &number); err != nil {
			return nil, fmt.Errorf("decode state issue %q: %w", key, err)
		}
		var present bool
		if err := json.Unmarshal(value, &present); err == nil {
			s[number] = present
			continue
		}
		var state workState
		if err := json.Unmarshal(value, &state); err != nil {
			return nil, fmt.Errorf("decode state issue %q: %w", key, err)
		}
		s[number] = state.Status != ""
	}
	return s, nil
}
func saveState(path string, seen map[int]bool) error {
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(seen, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}

func loadWorkState(path string) (map[int]workState, error) {
	result := make(map[int]workState)
	if path == "" {
		return result, nil
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}
	for key, value := range raw {
		var number int
		if _, err := fmt.Sscanf(key, "%d", &number); err != nil {
			return nil, fmt.Errorf("decode state issue %q: %w", key, err)
		}
		var legacy bool
		if json.Unmarshal(value, &legacy) == nil {
			if legacy {
				result[number] = workState{Status: "completed"}
			}
			continue
		}
		var state workState
		if err := json.Unmarshal(value, &state); err != nil {
			return nil, fmt.Errorf("decode state issue %q: %w", key, err)
		}
		result[number] = state
	}
	return result, nil
}

func saveWorkState(path string, state map[int]workState) error {
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}

func loadScopedWorkState(path string, targets []string) (map[string]workState, error) {
	result := make(map[string]workState)
	if path == "" {
		return result, nil
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}
	watched := make(map[string]bool, len(targets))
	for _, target := range targets {
		watched[target] = true
	}
	for key, value := range raw {
		var state workState
		if json.Unmarshal(value, &state) != nil {
			var legacy bool
			if err := json.Unmarshal(value, &legacy); err != nil {
				return nil, fmt.Errorf("decode state issue %q: %w", key, err)
			}
			if legacy {
				state = workState{Status: "completed"}
			} else {
				continue
			}
		}
		if _, err := strconv.Atoi(key); err == nil {
			if len(targets) > 0 {
				result[targets[0]+"#"+key] = state
			}
		} else {
			separator := strings.LastIndexByte(key, '#')
			if len(watched) > 0 && (separator <= 0 || !watched[key[:separator]]) {
				continue
			}
			result[key] = state
		}
	}
	return result, nil
}

func saveScopedWorkState(path string, state map[string]workState, targets []string) error {
	if path == "" {
		return nil
	}
	var value interface{} = state
	if len(targets) == 1 {
		legacy := make(map[int]workState, len(state))
		prefix := targets[0] + "#"
		for key, work := range state {
			if strings.HasPrefix(key, prefix) {
				number, err := strconv.Atoi(strings.TrimPrefix(key, prefix))
				if err == nil {
					legacy[number] = work
					continue
				}
				return fmt.Errorf("invalid scoped state key %q", key)
			}
		}
		value = legacy
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}

// stacksProbeTTL is how long a repository's stacked pull request setting is
// trusted before it is read again, so enabling the feature on a repository is
// noticed without a restart.
const stacksProbeTTL = time.Hour

type stacksProbe struct {
	enabled bool
	at      time.Time
}

// stacksEnabled reports whether repo has GitHub stacked pull requests
// enabled, caching the answer for stacksProbeTTL. A source that cannot answer
// or a failed read counts as disabled, which keeps the old wait-for-blocker
// behavior.
func (w *Engine) stacksEnabled(ctx context.Context, repo string) bool {
	source, ok := w.Issues.(StackSource)
	if !ok || repo == "" {
		return false
	}
	w.stacksMu.Lock()
	probe, cached := w.stacks[repo]
	w.stacksMu.Unlock()
	if cached && time.Since(probe.at) < stacksProbeTTL {
		return probe.enabled
	}
	enabled, err := source.StacksEnabled(ctx, repo)
	if err != nil {
		w.logChanged("stacks-"+repo, err.Error(), "could not read stacked pull request setting for %s: %v", repo, err)
		return false
	}
	w.stacksMu.Lock()
	if w.stacks == nil {
		w.stacks = make(map[string]stacksProbe)
	}
	w.stacks[repo] = stacksProbe{enabled: enabled, at: time.Now()}
	w.stacksMu.Unlock()
	return enabled
}

// forkProbeTTL is how long a repository's answer to where its fixes are pushed
// is trusted before write permission is read again, so being granted (or
// losing) write access is noticed without a restart.
const forkProbeTTL = time.Hour

type forkProbe struct {
	fork string
	at   time.Time
}

// forkFor returns the OWNER/REPO of the fork an issue in repo pushes its fix
// to, or an empty string when the user can write to repo itself.
// The answer is cached for forkProbeTTL. A source that cannot answer keeps the
// same-repository flow, while a failed read is returned so the issue is not
// dispatched to push where it cannot.
func (w *Engine) forkFor(ctx context.Context, repo string) (string, error) {
	source, ok := w.Issues.(ForkSource)
	if !ok || repo == "" {
		return "", nil
	}
	w.forksMu.Lock()
	probe, cached := w.forks[repo]
	w.forksMu.Unlock()
	if cached && time.Since(probe.at) < forkProbeTTL {
		return probe.fork, nil
	}
	fork, err := source.ForkFor(ctx, repo)
	if err != nil {
		return "", err
	}
	w.forksMu.Lock()
	if w.forks == nil {
		w.forks = make(map[string]forkProbe)
	}
	w.forks[repo] = forkProbe{fork: fork, at: time.Now()}
	w.forksMu.Unlock()
	return fork, nil
}

// stackableOnDependency reports whether a blocked issue can be dispatched
// anyway by stacking its pull request on its blocker's branch,
// and which open pull request it would stack on. That needs stacked pull
// requests enabled on the repository, exactly one open blocker in that same
// repository (core.StackableDependency), and an open pull request for that
// blocker to build on; otherwise the issue keeps waiting for its blocker.
func (w *Engine) stackableOnDependency(ctx context.Context, checker WorkClosureChecker, issue Issue) (int, bool) {
	if checker == nil {
		return 0, false
	}
	dependency, ok := stackableDependency(issue)
	if !ok {
		return 0, false
	}
	repo := issue.Repository
	if !w.stacksEnabled(ctx, repo) {
		return 0, false
	}
	state, err := checker.OriginatingWorkState(ctx, repo, dependency.Number)
	if err != nil {
		w.logChanged("stack-"+issueKey(issue), err.Error(), "issue #%d could not read pull requests for #%d: %v", issue.Number, dependency.Number, err)
		return 0, false
	}
	for _, pullRequest := range state.PullRequests {
		// Only a pull request in this repository can be a stacking base.
		if pullRequest.InRepo(repo) && strings.EqualFold(pullRequest.State, "open") && !pullRequest.Merged {
			return pullRequest.Number, true
		}
	}
	return 0, false
}

// The dispatch predicates live in package core, shared with the browser
// driver's board reader.
func issueBlocked(issue Issue) (bool, string) { return core.IssueBlocked(issue) }

func stackableDependency(issue Issue) (IssueDependency, bool) { return core.StackableDependency(issue) }

func shouldDispatchIssue(repo string, issue Issue, isActive, wasActive, wasCompleted, seen bool, readyState string) bool {
	return core.ShouldDispatchIssue(repo, issue, isActive, wasActive, wasCompleted, seen, readyState)
}

func projectItemInProgress(target string, issue Issue) bool {
	return core.ProjectItemInProgress(target, issue)
}

func remoteIssueAllowsDispatch(target string, issue Issue, readyState string) bool {
	return core.RemoteIssueAllowsDispatch(target, issue, readyState)
}

func projectReadyState(configured, current string) string {
	return core.ProjectReadyState(configured, current)
}
