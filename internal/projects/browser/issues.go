package browser

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// issuesPageLimit bounds how many pages of the issues list a single tick
// walks. Browser mode polls every few seconds, so following the pager to the
// end of a large backlog would spend the whole interval loading pages nobody
// dispatches from; the first pages hold the issues a poll actually acts on.
const issuesPageLimit = 3

// The issues page is a client-rendered React app, so an extraction that runs
// the instant navigation finishes can land before the list exists in the DOM.
// These bound the wait for it, mirroring the project board reader's. The budget
// is generous because it is only ever spent by a page that has not drawn: a
// list that is there is read on the first attempt, while GitHub's own render
// routinely takes longer than the five seconds this used to allow and was
// reported as an unreadable page for no better reason than that (issue #427).
const (
	defaultIssuesSettleAttempts = 60
	defaultIssuesSettleDelay    = 250 * time.Millisecond
)

// Page is the part of a browser tab the issue source drives. Tab
// satisfies it; tests supply a fake so the extraction logic can be exercised
// without a browser or a network.
type Page interface {
	Navigate(url string) error
	Reload() error
	Eval(js string, out any) error
	HTTPStatus() int
}

// IssueSource lists a repository's issues by reading its rendered GitHub
// issues page instead of calling the API: one tab per target, reloaded on each
// tick, and one Runtime.evaluate per page load that returns the rows. It spends
// no API quota and no agent tokens, and it never issues a GraphQL query.
//
// It implements IssueSource, so the watch loop needs no structural change to
// use it. Issue bodies, dependencies, and sub-issue state are deliberately not
// scraped here: those are hydrated by targeted REST calls just before dispatch
// (see hydrateIssues).
type IssueSource struct {
	// pageFor opens (or returns the already-open) tab for a target.
	pageFor func(target string) (Page, error)
	// filter and allIssues mirror GHCLI's, so -filter and -all-issues keep the
	// same meaning whichever transport is in use.
	filter    string
	allIssues bool
	logf      func(string, ...interface{})
	// profile is the browser profile directory, named in the signed-out error
	// so the user is told which profile to sign in rather than being left to
	// guess whether -browser-profile was in play.
	profile string
	// Hydration is the shared candidate/memo hydration helper. It is
	// embedded so the source's own hydrate, handled, and hydrated fields read
	// and write the one memo the project board reader shares (issue #395).
	*Hydration
	// vision is the bounded screenshot-to-agent fallback, or nil when
	// -browser-vision was not passed. It is consulted only for the
	// distinguishable extraction failure below, never for an empty or a
	// successful read.
	vision *Vision
	// settleAttempts and settleDelay bound the wait for the client-rendered
	// issues page to draw, the same way the project board reader waits for its
	// board. sleep is a test seam for that wait.
	settleAttempts int
	settleDelay    time.Duration
	sleep          func(context.Context, time.Duration) bool

	mu sync.Mutex
	// reported remembers which page URLs have already had an extraction
	// failure logged, so a page whose markup Aviary cannot read is reported
	// once rather than on every tick.
	reported map[string]bool
	// lastURL is the URL each target's tab was last pointed at, so an
	// unchanged poll reloads the tab instead of navigating it again.
	lastURL map[string]string
	// lastRows fingerprints the issues each target last yielded, so a tick is
	// logged when the list changes and stays silent when it does not.
	lastRows map[string]string
}

// IssueHydrator fills in the fields a rendered issues page does not
// carry. GHCLI satisfies it with targeted REST calls; tests supply a fake that
// counts requests.
type IssueHydrator interface {
	HydrateIssue(ctx context.Context, repo string, issue *core.Issue) error
}

// hydratedIssue is the memoized result of one hydration: exactly the
// fields a rendered page cannot carry and the dispatch path needs. Title and
// State are memoized too because the hydrator corrects them, and a cached tick
// must yield the same issue a freshly hydrated one does.
type hydratedIssue struct {
	Title        string
	Body         string
	State        string
	CreatedAt    time.Time
	DependsOn    []core.IssueDependency
	HasSubIssues bool
	// Fetched is when the hydration was read, so a blocked issue knows when
	// its dependency state is due to be read again.
	Fetched time.Time
}

// blockedRefreshInterval is how long a blocked issue's memoized dependency
// state is trusted. Closing a blocker does not change anything a rendered page
// shows for the dependent issue, so without a fresh read a paused issue never
// sees its last blocker close (issue #762).
const blockedRefreshInterval = time.Minute

// Hydration is the candidate selection and memoization both browser
// page readers hydrate through: the repository issues page (issue #381) and
// the Projects v2 board (issue #395). One instance is shared by both, keyed by
// target, so the request budget below is a property of the whole run rather
// than of either reader.
type Hydration struct {
	// hydrate fetches the dispatch-only fields a rendered page does not
	// render. Nil leaves every extracted issue unhydrated, which is what the
	// extraction-only tests want.
	hydrate IssueHydrator
	// handled reports issues this run already owns: work it has in flight or
	// has already completed. Those are not dispatch candidates, so they are
	// never worth a fetch. Nil treats every extracted issue as a candidate.
	handled func(core.Issue) bool

	mu sync.Mutex
	// hydrated memoizes the hydrated fields per target and per "repo#number",
	// so an issue costs its REST calls once for the life of the run. Entries
	// for issues that are no longer candidates are dropped, which is what
	// makes an issue that leaves and re-enters the candidate set hydrate
	// again.
	hydrated map[string]map[string]hydratedIssue
	// now reads the clock; nil uses time.Now. Tests override it.
	now func() time.Time
}

// NewHydration builds the shared hydration helper for a run.
func NewHydration(hydrate IssueHydrator, handled func(core.Issue) bool) *Hydration {
	return &Hydration{hydrate: hydrate, handled: handled, hydrated: map[string]map[string]hydratedIssue{}}
}

// NewIssueSource builds the issue source browser mode polls with,
// reading each target through its own tab of the shared browser.
func NewIssueSource(browser *Browser, hydrate IssueHydrator, handled func(core.Issue) bool, filter string, allIssues bool, vision *Vision, logf func(string, ...interface{})) *IssueSource {
	return &IssueSource{
		pageFor: func(target string) (Page, error) {
			tab, err := browser.Tab(target)
			if err != nil {
				return nil, err
			}
			return tab, nil
		},
		filter:    filter,
		allIssues: allIssues,
		vision:    vision,
		logf:      logf,
		profile:   browser.Profile(),
		Hydration: NewHydration(hydrate, handled),
		reported:  map[string]bool{},
		lastURL:   map[string]string{},
		lastRows:  map[string]string{},
	}
}

// ErrExtraction marks the failure of reading an issues page that loaded
// but whose contents Aviary did not recognise. Callers match it with errors.Is
// to tell "GitHub rendered something we cannot read" (which usually means the
// browser profile is signed out, or GitHub's markup moved) apart from an
// ordinary navigation or protocol error.
var ErrExtraction = errors.New("issue list extraction failed")

// ExtractionError is the distinguishable error the issue source returns
// for a page it could not read, carrying the URL that produced it.
type ExtractionError struct {
	URL    string
	Reason string
}

func (e *ExtractionError) Error() string {
	return fmt.Sprintf("read issue list at %s: %s", e.URL, e.Reason)
}

// Is reports ErrExtraction so callers can match the category without
// depending on this concrete type.
func (e *ExtractionError) Is(target error) bool { return target == ErrExtraction }

// issuesURL builds the issues-page URL for a repository, carrying the
// same filter the API path searches with as the page's own ?q= value, built by
// the same core.IssueSearchTerms helper. The "@me" qualifiers are left as they are:
// the browser is signed in as the user, which is exactly who "@me" means to
// GitHub's own search.
func issuesURL(repo, filter string, allIssues bool) string {
	query := strings.Join(core.IssueSearchTerms(filter, allIssues), " ")
	return "https://github.com/" + repo + "/issues?" + url.Values{"q": {query}}.Encode()
}

// issueRow is one row as the page script reports it.
type issueRow struct {
	Number     int      `json:"number"`
	Repository string   `json:"repository"`
	Title      string   `json:"title"`
	State      string   `json:"state"`
	Labels     []string `json:"labels"`
}

// issueList is the page script's result: the rows it found, whether it
// recognised the page at all, whether the page named a list container, and the
// next page to follow if there is one.
type issueList struct {
	Rows       []issueRow `json:"rows"`
	Recognized bool       `json:"recognized"`
	Empty      bool       `json:"empty"`
	Container  bool       `json:"container"`
	Next       string     `json:"next"`
}

// ListIssues reads the target repository's issues page and returns the issues
// on it. A page with no results yields an empty slice rather than an error; a
// page that loaded but could not be read yields ErrExtraction.
func (s *IssueSource) ListIssues(ctx context.Context, target string) ([]core.Issue, error) {
	parsed, err := core.ParseTarget(target)
	if err != nil {
		return nil, err
	}
	if parsed.Repo == "" || parsed.IsProject || parsed.IsDiscussion {
		return nil, fmt.Errorf("browser mode lists issues for an OWNER/REPO target only, not %q", target)
	}
	page, err := s.pageFor(target)
	if err != nil {
		return nil, err
	}
	next := issuesURL(parsed.Repo, s.filter, s.allIssues)
	var issues []core.Issue
	seen := map[string]bool{}
	for visited := 0; visited < issuesPageLimit && next != ""; visited++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		current := next
		list, err := s.readPage(ctx, target, page, current, visited == 0)
		if err != nil {
			recovered := s.recoverWithVision(ctx, target, page, current, err)
			if len(recovered) == 0 {
				return nil, err
			}
			for _, ref := range recovered {
				key := parsed.Repo + "#" + strconv.Itoa(ref.Number)
				if seen[key] {
					continue
				}
				seen[key] = true
				issues = append(issues, issueFromRow(issueRow{Number: ref.Number}, parsed.Repo))
			}
			break
		}
		for _, row := range list.Rows {
			if row.Number <= 0 {
				continue
			}
			key := row.Repository + "#" + strconv.Itoa(row.Number)
			if seen[key] {
				continue
			}
			seen[key] = true
			issues = append(issues, issueFromRow(row, parsed.Repo))
		}
		next = nextIssuesURL(list.Next)
	}
	if err := s.hydrateIssues(ctx, target, issues); err != nil {
		return nil, err
	}
	s.logChange(target, issues)
	return issues, nil
}

// hydrateIssues fills in the body and dependency state a rendered issues page
// or project board does not carry, for the issues that could actually be
// dispatched from this tick.
//
// The request budget this keeps is deliberate and is the whole point of the
// browser transport: hydration is O(new candidate issues), never O(list) and
// never O(ticks). An issue is fetched when it is a dispatch candidate (not
// already in flight and not already completed by this run, per the handled
// predicate the watch loop backs with .Aviary.json and its in-flight set) and
// has not been fetched yet; the result is memoized for the life of the run. A
// steady-state tick whose extraction is unchanged therefore makes zero
// requests, and the requests it does make are plain REST GETs -- never a
// GraphQL query.
//
// The one exception is a blocked issue: its dependency state is read again
// once blockedRefreshInterval has passed, because a blocker closing is the
// only thing that can wake it and the page never shows it (issue #762).
func (s *Hydration) hydrateIssues(ctx context.Context, target string, issues []core.Issue) error {
	if s == nil || s.hydrate == nil {
		return nil
	}
	candidates := make(map[string]bool, len(issues))
	for i := range issues {
		issue := &issues[i]
		// issueKey and the handled predicate both read Target, which the
		// watch loop only stamps after ListIssues returns.
		issue.Target = target
		if s.handled != nil && s.handled(*issue) {
			continue
		}
		repo := core.IssueRepository(target, *issue)
		key := repo + "#" + strconv.Itoa(issue.Number)
		candidates[key] = true
		if cached, ok := s.cachedHydration(target, key); ok && !s.dueForRefresh(cached) {
			applyHydration(issue, cached)
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.hydrate.HydrateIssue(ctx, repo, issue); err != nil {
			return err
		}
		s.storeHydration(target, key, hydratedIssue{
			Title:        issue.Title,
			Body:         issue.Body,
			State:        issue.State,
			CreatedAt:    issue.CreatedAt,
			DependsOn:    issue.DependsOn,
			HasSubIssues: issue.HasSubIssues,
			Fetched:      s.clock(),
		})
	}
	s.pruneHydration(target, candidates)
	return nil
}

// dueForRefresh reports whether a memoized hydration must be read again: only
// a blocked issue's, and only once blockedRefreshInterval has passed, which
// keeps an unblocked steady-state tick free of requests.
func (s *Hydration) dueForRefresh(cached hydratedIssue) bool {
	if blocked, _ := core.IssueBlocked(core.Issue{DependsOn: cached.DependsOn, HasSubIssues: cached.HasSubIssues}); !blocked {
		return false
	}
	return s.clock().Sub(cached.Fetched) >= blockedRefreshInterval
}

func (s *Hydration) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// cachedHydration returns a target's memoized hydration for an issue, if it
// still has one.
func (s *Hydration) cachedHydration(target, key string) (hydratedIssue, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cached, ok := s.hydrated[target][key]
	return cached, ok
}

// storeHydration memoizes one issue's hydrated fields for the life of the run.
func (s *Hydration) storeHydration(target, key string, hydrated hydratedIssue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hydrated == nil {
		s.hydrated = map[string]map[string]hydratedIssue{}
	}
	if s.hydrated[target] == nil {
		s.hydrated[target] = map[string]hydratedIssue{}
	}
	s.hydrated[target][key] = hydrated
}

// pruneHydration forgets the issues that are no longer candidates for this
// target, so one that later re-enters the candidate set is fetched afresh
// rather than dispatched from a stale body.
func (s *Hydration) pruneHydration(target string, candidates map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.hydrated[target] {
		if !candidates[key] {
			delete(s.hydrated[target], key)
		}
	}
}

// applyHydration copies a memoized hydration back onto a freshly
// extracted row. Only the fields a page cannot carry are written back, so the
// ones the extraction legitimately owns -- a board item's ProjectStatus and
// ProjectItemID above all -- survive hydration untouched.
func applyHydration(issue *core.Issue, hydrated hydratedIssue) {
	issue.Body = hydrated.Body
	issue.CreatedAt = hydrated.CreatedAt
	issue.DependsOn = hydrated.DependsOn
	issue.HasSubIssues = hydrated.HasSubIssues
	if hydrated.Title != "" {
		issue.Title = hydrated.Title
	}
	if hydrated.State != "" {
		issue.State = hydrated.State
	}
}

// readPage points the tab at one page of the list and runs the extractor in it.
// The first page of a tick reloads when the tab is already there, so a poll
// that changes nothing costs a reload rather than a fresh navigation.
func (s *IssueSource) readPage(ctx context.Context, target string, page Page, pageURL string, first bool) (issueList, error) {
	if err := s.visit(target, page, pageURL, first); err != nil {
		return issueList{}, fmt.Errorf("load issue list at %s: %w", pageURL, err)
	}
	if status := page.HTTPStatus(); status >= 400 {
		// A 404 or 403 on a repository the run was asked to watch is how a
		// private repository looks to a signed-out profile, so the page is
		// asked which it was before the status is reported as a dead target
		// (issue #379).
		if signedOutStatus(page, status) {
			return issueList{}, &SignedOutError{URL: pageURL, Profile: s.profile, Status: status}
		}
		return issueList{}, fmt.Errorf("load issue list at %s: GitHub returned HTTP %d", pageURL, status)
	}
	// The issues page draws its rows client-side, so the first evaluation after
	// a navigation or a reload usually lands before React has rendered
	// anything: no rows and no empty-state marker, which is indistinguishable
	// from markup Aviary cannot read (issue #415). The read is retried until the
	// page recognises itself -- as a list or as an honestly empty one -- so a
	// page that has drawn costs a single evaluation and only a page that never
	// draws waits out the whole budget.
	var list issueList
	for attempt := 0; ; attempt++ {
		var read issueList
		if err := page.Eval(issueRowsScript, &read); err != nil {
			return issueList{}, fmt.Errorf("read issue list at %s: %w", pageURL, err)
		}
		list = read
		if list.Recognized || attempt >= s.attempts()-1 {
			break
		}
		if !s.pause(ctx) {
			return issueList{}, ctx.Err()
		}
	}
	// A page that yielded no rows is where a signed-out profile hides: with
	// "@me" in the filter GitHub renders a genuine, correctly-empty result, so
	// the extraction succeeds and the run reports "0 issues" on every poll
	// forever instead of saying the one thing that would fix it (issue #402).
	// The probe runs only here, so a read that found issues costs nothing.
	if len(list.Rows) == 0 && signedOutPage(page) {
		return issueList{}, &SignedOutError{URL: pageURL, Profile: s.profile}
	}
	// A list container that still holds no rows once the render wait above is
	// over is an empty list, not markup Aviary failed to read: a repository with
	// no ready issues was reported as an extraction failure on every tick of a
	// five-second poll, purely because the blankslate beside the list carried
	// none of the markers the script knows (issue #413). The wait runs first, so
	// a page that had merely not drawn yet is never mistaken for an empty one.
	if !list.Recognized && list.Container && len(list.Rows) == 0 {
		list.Recognized, list.Empty = true, true
	}
	if !list.Recognized {
		return issueList{}, s.extractionFailed(pageURL, fmt.Sprintf("the issue list did not render within %s (GitHub may be failing to serve the page, or its markup may have changed)", time.Duration(s.attempts())*s.delay()))
	}
	return list, nil
}

// visit navigates the tab, or reloads it when the first page of a tick is the
// URL the tab already shows.
func (s *IssueSource) visit(target string, page Page, pageURL string, first bool) error {
	s.mu.Lock()
	unchanged := first && s.lastURL[target] == pageURL
	if first {
		s.lastURL[target] = pageURL
	}
	s.mu.Unlock()
	if unchanged {
		return page.Reload()
	}
	return page.Navigate(pageURL)
}

// attempts is how many times one page load is read before it is reported as
// unreadable.
func (s *IssueSource) attempts() int {
	if s.settleAttempts > 0 {
		return s.settleAttempts
	}
	return defaultIssuesSettleAttempts
}

// delay is how long the reader waits between those attempts.
func (s *IssueSource) delay() time.Duration {
	if s.settleDelay > 0 {
		return s.settleDelay
	}
	return defaultIssuesSettleDelay
}

// pause waits between settle attempts, reporting false when the run is being
// shut down so a cancelled poll stops instead of sitting out the whole wait.
func (s *IssueSource) pause(ctx context.Context) bool {
	if s.sleep != nil {
		return s.sleep(ctx, s.delay())
	}
	timer := time.NewTimer(s.delay())
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// extractionFailed builds the extraction error and logs it the first time a
// given URL produces one, so a persistently unreadable page does not repeat
// itself on every tick of a five-second loop.
func (s *IssueSource) extractionFailed(pageURL, reason string) error {
	s.mu.Lock()
	report := !s.reported[pageURL]
	s.reported[pageURL] = true
	s.mu.Unlock()
	if report && s.logf != nil {
		s.logf("could not read the issue list at %s: %s", pageURL, reason)
	}
	return &ExtractionError{URL: pageURL, Reason: reason}
}

// recoverWithVision asks the screenshot fallback to read the page the DOM
// extractor could not. Only the distinguishable extraction failure qualifies: a
// navigation error, an HTTP error, or an empty-but-recognised list is never
// worth a screenshot. A repository's issues page names one repository, so the
// agent is asked for bare numbers here; the board source asks for qualified
// ones. The fallback answers with issue numbers alone, so the issues it
// recovers carry no title or labels; the dispatch path hydrates them from the
// API, and the extractor is still expected to be fixed in code.
func (s *IssueSource) recoverWithVision(ctx context.Context, target string, page Page, pageURL string, cause error) []VisionRef {
	if s.vision == nil || !errors.Is(cause, ErrExtraction) {
		return nil
	}
	shooter, ok := page.(screenshotter)
	if !ok {
		return nil
	}
	return s.vision.Recover(ctx, target, pageURL, cause.Error(), shooter.Screenshot, false)
}

// logChange emits one line when a target's issue list differs from the previous
// tick's, and nothing at all when the reload found the same issues.
func (s *IssueSource) logChange(target string, issues []core.Issue) {
	fingerprint := issuesFingerprint(issues)
	s.mu.Lock()
	previous, had := s.lastRows[target]
	s.lastRows[target] = fingerprint
	s.mu.Unlock()
	if had && previous == fingerprint {
		return
	}
	if s.logf != nil {
		s.logf("browser read %d issue(s) from %s", len(issues), target)
	}
}

// issuesFingerprint reduces a tick's issues to a value that changes
// whenever the list does, including when only a title, state, or label moved.
func issuesFingerprint(issues []core.Issue) string {
	keys := make([]string, 0, len(issues))
	for _, issue := range issues {
		labels := make([]string, 0, len(issue.Labels))
		for _, label := range issue.Labels {
			labels = append(labels, label.Name)
		}
		keys = append(keys, fmt.Sprintf("%s#%d/%s/%s/%s", issue.Repository, issue.Number, issue.State, issue.Title, strings.Join(labels, ",")))
	}
	sort.Strings(keys)
	return strings.Join(keys, "\n")
}

// issueFromRow converts one extracted row into an Issue, defaulting the
// repository to the watched one when the row's link did not carry it.
func issueFromRow(row issueRow, repo string) core.Issue {
	issue := core.Issue{
		Number:     row.Number,
		Repository: row.Repository,
		Title:      strings.TrimSpace(row.Title),
		State:      row.State,
	}
	if issue.Repository == "" {
		issue.Repository = repo
	}
	if issue.State == "" {
		issue.State = "open"
	}
	for _, label := range row.Labels {
		if label = strings.TrimSpace(label); label != "" {
			issue.Labels = append(issue.Labels, core.IssueLabel{Name: label})
		}
	}
	return issue
}

// nextIssuesURL accepts the pager's target only when it is another
// github.com issues page, so a mis-read control cannot send the tab somewhere
// unrelated.
func nextIssuesURL(candidate string) string {
	if candidate == "" {
		return ""
	}
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || !strings.HasSuffix(parsed.Path, "/issues") {
		return ""
	}
	return parsed.String()
}
