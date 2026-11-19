package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CommentKind names which of GitHub's comment APIs a comment belongs to. Each
// has its own IDs, listings, webhook deliveries, and reaction endpoint.
type CommentKind int

const (
	// ConversationComment is an ordinary comment on an issue or on a pull
	// request's conversation tab.
	ConversationComment CommentKind = iota
	// ReviewComment is an inline comment on a pull request's diff (issue
	// #695).
	ReviewComment
	// ReviewBody is the body of a submitted pull request review (issue #695).
	ReviewBody
)

// RecentComment is one comment found by a repository-wide scan of recent
// issue and pull request comments (issue #689), or of pull request review
// comments and review bodies (issue #695).
type RecentComment struct {
	ID int64
	// Kind says which API ID belongs to. The zero value is a conversation
	// comment.
	Kind CommentKind
	// NodeID is the GraphQL node ID, the only handle a review body can be
	// reacted to through.
	NodeID string
	// Number is the issue or pull request the comment was posted on.
	Number int
	Body   string
	Author string
	// CreatedAt is when the comment was first posted, which UpdatedAt stops
	// saying as soon as it is edited.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// routed is the comment as one routed to the prompt agent, posted in repo.
func (c RecentComment) routed(repo string) routedComment {
	return routedComment{Kind: c.Kind, Repository: repo, Number: c.Number, ID: c.ID, Author: c.Author}
}

// MentionSource is what the poll modes scan for direct @/glorp:ID mentions.
// They receive no issue_comment webhook deliveries, so without it a mention
// posted on an issue or its pull request is never seen (issue #689).
type MentionSource interface {
	// RecentComments lists the comments posted or edited on any issue or
	// pull request in repo since the given time, oldest first.
	RecentComments(ctx context.Context, repo string, since time.Time) ([]RecentComment, error)
	// ClosingIssues names the issues pull request repo#number closes, so a
	// mention posted on a pull request reaches the issue it fixes. A number
	// that is not a pull request closes nothing.
	ClosingIssues(ctx context.Context, repo string, number int) ([]int, error)
}

// ReviewMentionSource is what the poll modes scan for direct mentions posted
// in pull request reviews. Inline review comments and review bodies are
// separate GitHub APIs from the conversation RecentComments reads, so without
// it a mention posted in a review is never seen (issue #695).
type ReviewMentionSource interface {
	// RecentReviewComments lists the inline review comments and submitted
	// review bodies posted or edited on any open pull request in repo since
	// the given time, oldest first.
	RecentReviewComments(ctx context.Context, repo string, since time.Time) ([]RecentComment, error)
}

// mentionScanOverlap is how far each scan reaches back past the newest
// comment the last one saw. GitHub can surface a comment in the listing a
// little after its timestamp, and the overlap costs nothing because comments
// already handled are skipped by ID.
const mentionScanOverlap = time.Minute

// agentCommentGrace is how long after an agent finishes a comment from glorp's
// own login is still attributed to that agent rather than to a human.
const agentCommentGrace = 30 * time.Second

// markdownNonMentions matches the parts of a comment where GitHub does not
// treat @name as a mention: fenced code, inline code, and quoted lines.
var markdownNonMentions = regexp.MustCompile("(?s)```.*?```|`[^`\\n]*`|(?m:^[ \\t]*>.*$)")

// anyInstanceMention matches an @/glorp:ID mention of any instance.
var anyInstanceMention = regexp.MustCompile(`(^|\s)@/glorp:\S`)

// mentionedLogin reports whether body mentions the GitHub user login with an
// ordinary @login. A longer login or a @login/team reference does not match.
func mentionedLogin(body, login string) bool {
	if login == "" {
		return false
	}
	pattern := `(?i)(^|[^\w/-])@` + regexp.QuoteMeta(login) + `($|[^\w/-])`
	return regexp.MustCompile(pattern).MatchString(markdownNonMentions.ReplaceAllString(body, " "))
}

// commentRequestsRecheck reports whether a comment that does not mention this
// instance still triggers a recheck of its issue (issue #708): it must come
// from an allowed commenter and mention the login glorp runs as. An empty
// allowed list names nobody here, handshake comments are glorp's own, and a
// comment addressed to another instance is not an instruction to this one.
func (w *Glorp) commentRequestsRecheck(body, author string) bool {
	if len(w.AllowedCommenters) == 0 || !commenterAllowed(author, w.AllowedCommenters) || !mentionedLogin(body, w.SelfLogin) {
		return false
	}
	if _, _, ok := parseClaim(body); ok {
		return false
	}
	return !anyInstanceMention.MatchString(body)
}

// queueCommentRecheck queues a run for each key a login-mention comment
// addresses and returns the keys it queued. Only issues this instance watches
// qualify. A comment from glorp's own login posted while its agent ran on the
// issue is left out: agents post as that login, so it may be the agent's own,
// and a human's is relayed by that run's watcher.
func (w *Glorp) queueCommentRecheck(scan *mentionScan, keys []string, comment routedComment, at time.Time, listed, directMentions map[string]bool) []string {
	author := comment.Author
	var queued []string
	for _, key := range keys {
		if !listed[key] {
			w.logf("%s ignoring comment mentioning %s: it is not among the open issues this instance watches", key, w.SelfLogin)
			continue
		}
		if strings.EqualFold(author, w.SelfLogin) && scan.agentWasRunning(key, at) {
			w.logf("%s not queuing a recheck for a comment from %s posted during this instance's agent run", key, author)
			continue
		}
		if !directMentions[key] {
			directMentions[key] = true
			scan.rechecks[key] = true
		}
		scan.comments[key] = comment
		w.logf("%s comment from %s mentions %s; queuing a threaded gh-fix run", key, author, w.SelfLogin)
		queued = append(queued, key)
	}
	return queued
}

// handleScannedRecheck acts on one scanned comment that requests a recheck. It
// reports false only when the issues it addresses could not be read and the
// comment should be read again on the next scan.
func (w *Glorp) handleScannedRecheck(ctx context.Context, scan *mentionScan, repo string, comment RecentComment, listed, directMentions map[string]bool) bool {
	numbers := []int{comment.Number}
	linked := scan.linked.issues(repo, comment.Number)
	if len(linked) == 0 && (comment.Kind != ConversationComment || !listed[repo+"#"+strconv.Itoa(comment.Number)]) {
		closing, err := w.Mentions.ClosingIssues(ctx, repo, comment.Number)
		if err != nil {
			w.logf("%s#%d failed to read which issues it closes: %v", repo, comment.Number, err)
			return false
		}
		numbers = closing
	}
	keys := make([]string, 0, len(numbers))
	for _, number := range numbers {
		keys = append(keys, repo+"#"+strconv.Itoa(number))
	}
	if len(linked) > 0 {
		keys = linked
	}
	if len(w.queueCommentRecheck(scan, keys, comment.routed(repo), comment.CreatedAt, listed, directMentions)) > 0 {
		w.reactToMention(ctx, repo, comment.Kind, comment.Number, comment.ID, comment.NodeID)
	}
	return true
}

// commentOnGitHub reports whether repo#number carries a comment of kind from
// author with exactly body, so a webhook delivery is reverified rather than
// trusted from its payload.
func (w *Glorp) commentOnGitHub(ctx context.Context, kind CommentKind, repo string, number int, body, author string) (bool, error) {
	if w.Comments == nil {
		return true, nil
	}
	if kind != ConversationComment {
		lister, ok := w.Comments.(ReviewCommentLister)
		if !ok {
			return true, nil
		}
		return reviewMentionOnGitHub(ctx, lister, repo, number, body, author)
	}
	comments, err := w.Comments.ListComments(ctx, repo, number)
	if err != nil {
		return false, err
	}
	for _, comment := range comments {
		if comment.Body == body && strings.EqualFold(comment.Author, author) {
			return true, nil
		}
	}
	return false, nil
}

// mentionScan is the run loop's memory of the direct mentions it has already
// read. It is only touched from the run loop's own goroutine.
type mentionScan struct {
	start time.Time
	// since is where the next scan of each repository starts.
	since map[string]time.Time
	// handled is every mention comment already acted on, so one comment is
	// reacted to and dispatched at most once however many scans return it.
	// The kind is part of the key because each comment API numbers its own.
	handled map[mentionScanKey]bool
	// seeded names the cursors that have completed a scan. Whatever the first
	// scan of a cursor returns is already there when watching starts, so it
	// seeds seen rather than counting as new.
	seeded map[string]bool
	// seen is every comment already considered for a login-mention recheck, so
	// a comment is new only the first time it appears (issue #708).
	seen map[mentionScanKey]bool
	// rechecks names the "repo#number" keys in the direct-mention queue only
	// because a comment mentioned the login, not this instance. It is not
	// addressed to one instance, so unlike a direct mention it stays subject to
	// the claims standing on the ticket. Webhook deliveries use it as well.
	rechecks map[string]bool
	// comments is the latest comment that queued each "repo#number" key in
	// the direct-mention queue, so a job's issue can route it to the prompt
	// agent instead of restarting the job (issue #842).
	comments map[string]routedComment
	// linked is the derived watch on pull requests that work on an issue
	// from another repository, such as a fork (issue #723).
	linked *linkedPullRequests

	// agentMu guards agentRunning and agentEnded, which run goroutines update.
	agentMu      sync.Mutex
	agentRunning map[string]bool
	agentEnded   map[string]time.Time
}

type mentionScanKey struct {
	kind CommentKind
	id   int64
}

func newMentionScan(start time.Time) *mentionScan {
	return &mentionScan{
		start: start, since: make(map[string]time.Time), handled: make(map[mentionScanKey]bool),
		seeded: make(map[string]bool), seen: make(map[mentionScanKey]bool), rechecks: make(map[string]bool), comments: make(map[string]routedComment),
		agentRunning: make(map[string]bool), agentEnded: make(map[string]time.Time),
		linked: newLinkedPullRequests(),
	}
}

// agentStarted records that this instance began running an agent on key.
func (s *mentionScan) agentStarted(key string) {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	s.agentRunning[key] = true
}

// agentStopped records that the agent running on key has finished.
func (s *mentionScan) agentStopped(key string, at time.Time) {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	delete(s.agentRunning, key)
	s.agentEnded[key] = at
}

// agentWasRunning reports whether this instance had an agent on key at the
// given time, allowing agentCommentGrace for clocks and listing delay.
func (s *mentionScan) agentWasRunning(key string, at time.Time) bool {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	if s.agentRunning[key] {
		return true
	}
	ended, ok := s.agentEnded[key]
	return ok && !at.After(ended.Add(agentCommentGrace))
}

// mentionScanRepos lists the repositories a poll scans for mentions: every
// watched repository plus every repository a listed issue came from, which is
// how a project board's repositories are reached.
func mentionScanRepos(targets []string, issues []Issue) []string {
	seen := make(map[string]bool)
	var repos []string
	add := func(repo string) {
		if repo != "" && !seen[repo] {
			seen[repo] = true
			repos = append(repos, repo)
		}
	}
	for _, value := range targets {
		parsed, err := parseTarget(value)
		if err == nil && !parsed.IsProject && !parsed.IsDiscussion {
			add(parsed.Repo)
		}
	}
	for _, issue := range issues {
		add(issue.Repository)
	}
	slices.Sort(repos)
	return repos
}

// scanDirectMentions finds direct mentions of this instance posted since the
// last scan and records each authorized one in directMentions under the
// issue it addresses, the same way the webhook path records an issue_comment
// delivery. A mention on a pull request is recorded under the issues the
// pull request closes. Conversation comments and, when the source can list
// them, review comments and review bodies (issue #695) are scanned from
// cursors of their own, so one listing failing never skips the other's
// window. listed is the "repo#number" keys the poll just listed.
func (w *Glorp) scanDirectMentions(ctx context.Context, scan *mentionScan, repos []string, listed map[string]bool, directMentions map[string]bool) {
	reviews, _ := w.Mentions.(ReviewMentionSource)
	for _, repo := range repos {
		w.scanMentionStream(ctx, scan, repo, repo, "recent comments", w.Mentions.RecentComments, nil, listed, directMentions)
		if reviews != nil {
			w.scanMentionStream(ctx, scan, repo, repo+" reviews", "recent pull request reviews", reviews.RecentReviewComments, nil, listed, directMentions)
		}
	}
	w.scanLinkedPullRequests(ctx, scan, repos, listed, directMentions)
}

// scanLinkedPullRequests scans the repositories hosting linked pull requests
// (issue #723) that covered does not already reach, and acts only on comments
// posted on those pull requests. covered is the repositories scanned in full
// or receiving webhook deliveries. A comment on any other issue or pull
// request in such a repository is never acted on, so the derived watch never
// dispatches that repository's own issues.
func (w *Glorp) scanLinkedPullRequests(ctx context.Context, scan *mentionScan, covered []string, listed map[string]bool, directMentions map[string]bool) {
	reviews, _ := w.Mentions.(ReviewMentionSource)
	for _, repo := range scan.linked.repos() {
		if slices.ContainsFunc(covered, func(candidate string) bool { return strings.EqualFold(candidate, repo) }) {
			continue
		}
		only := func(number int) bool { return len(scan.linked.issues(repo, number)) > 0 }
		w.scanMentionStream(ctx, scan, repo, repo, "linked pull request comments", w.Mentions.RecentComments, only, listed, directMentions)
		if reviews != nil {
			w.scanMentionStream(ctx, scan, repo, repo+" reviews", "linked pull request reviews", reviews.RecentReviewComments, only, listed, directMentions)
		}
	}
}

// scanMentionStream reads one listing of repo's recent comments from the
// cursor stored under cursor and acts on each new mention in it. When only is
// set, comments on numbers it rejects are skipped.
func (w *Glorp) scanMentionStream(ctx context.Context, scan *mentionScan, repo, cursor, label string, list func(context.Context, string, time.Time) ([]RecentComment, error), only func(int) bool, listed map[string]bool, directMentions map[string]bool) {
	since, ok := scan.since[cursor]
	if !ok {
		// An identity is new on every start, so nothing posted before this
		// run can mention it.
		since = scan.start.Add(-mentionScanOverlap)
	}
	comments, err := list(ctx, repo, since)
	if err != nil {
		w.logChanged("mention-scan-"+cursor, err.Error(), "%s failed to scan %s for direct mentions: %v", repo, label, err)
		return
	}
	w.forgetLogged("mention-scan-" + cursor)
	next := since
	seeding := !scan.seeded[cursor]
	stopped := false
	for _, comment := range comments {
		if candidate := comment.UpdatedAt.Add(-mentionScanOverlap); candidate.After(next) {
			next = candidate
		}
		key := mentionScanKey{kind: comment.Kind, id: comment.ID}
		if only != nil && !only(comment.Number) {
			continue
		}
		if isPromptReply(comment.Body) {
			// The prompt agent's own replies are never triggers, so routing
			// a comment to it cannot loop (issue #842).
			scan.seen[key] = true
			continue
		}
		if !mentionedIdentity(comment.Body, w.Identity) {
			if scan.handled[key] || scan.seen[key] {
				continue
			}
			// An edit brings an old comment back into the listing, so one
			// posted before this watch began is history whatever its ID.
			fresh := !seeding && !comment.CreatedAt.Before(scan.start)
			if fresh && w.commentRequestsRecheck(comment.Body, comment.Author) && !w.handleScannedRecheck(ctx, scan, repo, comment, listed, directMentions) {
				if next.After(comment.UpdatedAt.Add(-mentionScanOverlap)) {
					next = comment.UpdatedAt.Add(-mentionScanOverlap)
				}
				stopped = true
				break
			}
			scan.seen[key] = true
			continue
		}
		if scan.handled[key] {
			continue
		}
		if !w.handleScannedMention(ctx, scan, repo, comment, listed, directMentions) {
			// Verification failed rather than ruling the mention out, so the
			// next scan tries it again from where this one started.
			if next.After(comment.UpdatedAt.Add(-mentionScanOverlap)) {
				next = comment.UpdatedAt.Add(-mentionScanOverlap)
			}
			stopped = true
			break
		}
		scan.handled[key] = true
	}
	scan.since[cursor] = next
	if !stopped {
		scan.seeded[cursor] = true
	}
}

// handleScannedMention acts on one scanned mention of this instance, applying
// the same checks as the matching webhook delivery: it reacts with eyes,
// requires the mention to come from an allowed commenter, and records the
// addressed issue in directMentions. A conversation comment must also still
// be the newest comment on its thread. A review comment or review body is
// always on a pull request, so it addresses the issues that pull request
// closes, or nothing. It reports false only when the mention could not be
// verified and should be read again on the next scan.
func (w *Glorp) handleScannedMention(ctx context.Context, scan *mentionScan, repo string, comment RecentComment, listed map[string]bool, directMentions map[string]bool) bool {
	w.reactToMention(ctx, repo, comment.Kind, comment.Number, comment.ID, comment.NodeID)
	authorized := commenterAllowed(comment.Author, w.AllowedCommenters)
	if authorized && w.Comments != nil && comment.Kind == ConversationComment {
		var err error
		authorized, err = authorizedDirectMention(ctx, w.Comments, repo, comment.Number, w.Identity, w.AllowedCommenters)
		if err != nil {
			w.logf("%s#%d failed to verify direct mention of instance %s: %v", repo, comment.Number, w.Identity, err)
			return false
		}
	}
	if !authorized {
		if comment.Kind == ConversationComment {
			w.logf("%s#%d ignoring direct mention of instance %s: not the last comment or not from an allowed commenter", repo, comment.Number, w.Identity)
		} else {
			w.logf("%s#%d ignoring direct mention of instance %s in %s: %s is not an allowed commenter", repo, comment.Number, w.Identity, mentionLocation(comment.Kind), comment.Author)
		}
		return true
	}
	if linked := scan.linked.issues(repo, comment.Number); len(linked) > 0 {
		// A linked pull request in another repository addresses the issue
		// it works, wherever that issue lives (issue #723).
		w.logf("%s pull request #%d directly mentioned instance %s%s; it is linked to %s", repo, comment.Number, w.Identity, mentionLocationSuffix(comment.Kind), strings.Join(linked, ", "))
		for _, key := range linked {
			directMentions[key] = true
			delete(scan.rechecks, key)
			scan.comments[key] = comment.routed(repo)
			w.logf("issue %s directly mentioned instance %s; queuing a threaded gh-fix run", key, w.Identity)
		}
		return true
	}
	issues := []int{comment.Number}
	if comment.Kind != ConversationComment || !listed[repo+"#"+strconv.Itoa(comment.Number)] {
		closing, err := w.Mentions.ClosingIssues(ctx, repo, comment.Number)
		if err != nil {
			w.logf("%s#%d failed to read which issues it closes: %v", repo, comment.Number, err)
			return false
		}
		if len(closing) > 0 {
			issues = closing
			w.logf("%s pull request #%d directly mentioned instance %s%s; it closes %s", repo, comment.Number, w.Identity, mentionLocationSuffix(comment.Kind), formatIssueRefs(closing))
		} else if comment.Kind != ConversationComment {
			w.logf("%s pull request #%d directly mentioned instance %s in %s, but it closes no issue to run", repo, comment.Number, w.Identity, mentionLocation(comment.Kind))
			return true
		}
	}
	for _, number := range issues {
		directMentions[repo+"#"+strconv.Itoa(number)] = true
		delete(scan.rechecks, repo+"#"+strconv.Itoa(number))
		scan.comments[repo+"#"+strconv.Itoa(number)] = comment.routed(repo)
		w.logf("issue #%d directly mentioned instance %s; queuing a threaded gh-fix run", number, w.Identity)
	}
	return true
}

// reactToMention acknowledges a mention with the eyes reaction through the
// endpoint its kind of comment needs: a review body is reached only through
// its GraphQL node ID. A failure is logged and otherwise ignored, since the
// reaction only tells a human the mention was read.
func (w *Glorp) reactToMention(ctx context.Context, repo string, kind CommentKind, number int, id int64, nodeID string) {
	var err error
	switch kind {
	case ConversationComment:
		reactor, ok := w.Comments.(CommentReactor)
		if !ok || id == 0 {
			return
		}
		err = reactor.AddReaction(ctx, repo, id, "eyes")
	case ReviewComment:
		reactor, ok := w.Comments.(ReviewReactor)
		if !ok || id == 0 {
			return
		}
		err = reactor.AddReviewCommentReaction(ctx, repo, id, "eyes")
	case ReviewBody:
		reactor, ok := w.Comments.(ReviewReactor)
		if !ok || nodeID == "" {
			return
		}
		err = reactor.AddReviewReaction(ctx, repo, nodeID, "eyes")
	}
	if err != nil {
		w.logf("%s#%d failed to react to mention of instance %s%s: %v", repo, number, w.Identity, mentionLocationSuffix(kind), err)
	}
}

// mentionLocation names where a comment of kind was posted, for log lines.
func mentionLocation(kind CommentKind) string {
	switch kind {
	case ReviewComment:
		return "a review comment"
	case ReviewBody:
		return "a review"
	default:
		return "a comment"
	}
}

// mentionLocationSuffix is mentionLocation as a phrase appended to a log line
// about a mention, empty for an ordinary conversation comment.
func mentionLocationSuffix(kind CommentKind) string {
	if kind == ConversationComment {
		return ""
	}
	return " in " + mentionLocation(kind)
}

// reviewMentionOnGitHub reports whether pull request repo#number carries a
// review comment or review body from author with exactly body, so a review
// delivery's mention is reverified against GitHub rather than trusted from
// the payload (issue #695). A review has no "last comment" the way a
// conversation does, so being on GitHub as delivered is what is checked.
func reviewMentionOnGitHub(ctx context.Context, lister ReviewCommentLister, repo string, number int, body, author string) (bool, error) {
	comments, err := lister.ListReviewComments(ctx, repo, number)
	if err != nil {
		return false, err
	}
	for _, comment := range comments {
		if comment.Body == body && strings.EqualFold(comment.Author, author) {
			return true, nil
		}
	}
	return false, nil
}

func formatIssueRefs(numbers []int) string {
	refs := make([]string, len(numbers))
	for i, number := range numbers {
		refs[i] = "#" + strconv.Itoa(number)
	}
	return strings.Join(refs, ", ")
}

// closingReferencePattern matches a GitHub closing keyword followed by an
// issue reference, optionally qualified with its OWNER/REPO.
var closingReferencePattern = regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s+([\w.-]+/[\w.-]+)?#([1-9][0-9]*)\b`)

// closingIssueNumbers lists the repo issues body closes, in first appearance
// order. A reference qualified with another repository is not repo's issue.
func closingIssueNumbers(body, repo string) []int {
	var numbers []int
	seen := make(map[int]bool)
	for _, match := range closingReferencePattern.FindAllStringSubmatch(body, -1) {
		if match[1] != "" && !strings.EqualFold(match[1], repo) {
			continue
		}
		number, err := strconv.Atoi(match[2])
		if err != nil || seen[number] {
			continue
		}
		seen[number] = true
		numbers = append(numbers, number)
	}
	return numbers
}

// RecentComments lists repo's issue and pull request comments updated since
// the given time through the repository-wide comment listing, so a whole
// repository costs one request per scan however many issues it has.
func (g GHCLI) RecentComments(ctx context.Context, repo string, since time.Time) ([]RecentComment, error) {
	query := url.Values{}
	query.Set("since", since.UTC().Format(time.RFC3339))
	query.Set("sort", "updated")
	query.Set("direction", "asc")
	query.Set("per_page", "100")
	output, err := g.apiGETPaginated(ctx, g.isPublicRepo(ctx, repo), "repos/"+repo+"/issues/comments?"+query.Encode())
	if err != nil {
		return nil, fmt.Errorf("list recent comments in %s: %w: %s", repo, err, strings.TrimSpace(string(output)))
	}
	var raw []struct {
		ID        int64     `json:"id"`
		Body      string    `json:"body"`
		IssueURL  string    `json:"issue_url"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("decode recent comments in %s: %w", repo, err)
	}
	comments := make([]RecentComment, 0, len(raw))
	for _, comment := range raw {
		number, err := strconv.Atoi(comment.IssueURL[strings.LastIndex(comment.IssueURL, "/")+1:])
		if err != nil {
			continue
		}
		comments = append(comments, RecentComment{ID: comment.ID, Number: number, Body: comment.Body, Author: comment.User.Login, CreatedAt: comment.CreatedAt, UpdatedAt: comment.UpdatedAt})
	}
	return comments, nil
}

// ClosingIssues reads repo#number and, when it is a pull request, returns the
// issues its description closes.
func (g GHCLI) ClosingIssues(ctx context.Context, repo string, number int) ([]int, error) {
	output, err := g.apiGETObject(ctx, g.isPublicRepo(ctx, repo), "repos/"+repo+"/issues/"+strconv.Itoa(number))
	if err != nil {
		return nil, fmt.Errorf("read #%d: %w: %s", number, err, strings.TrimSpace(string(output)))
	}
	var issue struct {
		Body        string           `json:"body"`
		PullRequest *json.RawMessage `json:"pull_request"`
	}
	if err := json.Unmarshal(output, &issue); err != nil {
		return nil, fmt.Errorf("decode #%d: %w", number, err)
	}
	if issue.PullRequest == nil {
		return nil, nil
	}
	return closingIssueNumbers(issue.Body, repo), nil
}

// RecentReviewComments lists the inline review comments and submitted review
// bodies posted on repo's open pull requests since the given time (issue
// #695). Inline comments have a repository-wide listing like conversation
// comments do. Reviews have none, so they are read from each open pull
// request updated since then, which submitting a review counts as.
func (g GHCLI) RecentReviewComments(ctx context.Context, repo string, since time.Time) ([]RecentComment, error) {
	public := g.isPublicRepo(ctx, repo)
	query := url.Values{}
	query.Set("since", since.UTC().Format(time.RFC3339))
	query.Set("sort", "updated")
	query.Set("direction", "asc")
	query.Set("per_page", "100")
	output, err := g.apiGETPaginated(ctx, public, "repos/"+repo+"/pulls/comments?"+query.Encode())
	if err != nil {
		return nil, fmt.Errorf("list recent review comments in %s: %w: %s", repo, err, strings.TrimSpace(string(output)))
	}
	var raw []struct {
		ID             int64     `json:"id"`
		NodeID         string    `json:"node_id"`
		Body           string    `json:"body"`
		PullRequestURL string    `json:"pull_request_url"`
		CreatedAt      time.Time `json:"created_at"`
		UpdatedAt      time.Time `json:"updated_at"`
		User           struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("decode recent review comments in %s: %w", repo, err)
	}
	comments := make([]RecentComment, 0, len(raw))
	for _, comment := range raw {
		number, err := strconv.Atoi(comment.PullRequestURL[strings.LastIndex(comment.PullRequestURL, "/")+1:])
		if err != nil {
			continue
		}
		comments = append(comments, RecentComment{ID: comment.ID, Kind: ReviewComment, NodeID: comment.NodeID, Number: number, Body: comment.Body, Author: comment.User.Login, CreatedAt: comment.CreatedAt, UpdatedAt: comment.UpdatedAt})
	}
	query = url.Values{}
	query.Set("state", "open")
	query.Set("sort", "updated")
	query.Set("direction", "desc")
	query.Set("per_page", "100")
	output, err = g.apiGETPaginated(ctx, public, "repos/"+repo+"/pulls?"+query.Encode())
	if err != nil {
		return nil, fmt.Errorf("list recently updated pull requests in %s: %w: %s", repo, err, strings.TrimSpace(string(output)))
	}
	var pulls []struct {
		Number    int       `json:"number"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	if err := json.Unmarshal(output, &pulls); err != nil {
		return nil, fmt.Errorf("decode recently updated pull requests in %s: %w", repo, err)
	}
	for _, pull := range pulls {
		if pull.UpdatedAt.Before(since) {
			// The listing is newest first, so no later pull request has a
			// review submitted since the cursor.
			break
		}
		reviews, err := g.pullRequestReviews(ctx, public, repo, pull.Number)
		if err != nil {
			return nil, err
		}
		for _, review := range reviews {
			if review.SubmittedAt.Before(since) {
				continue
			}
			comments = append(comments, RecentComment{ID: review.ID, Kind: ReviewBody, NodeID: review.NodeID, Number: pull.Number, Body: review.Body, Author: review.User.Login, CreatedAt: review.SubmittedAt, UpdatedAt: review.SubmittedAt})
		}
	}
	slices.SortStableFunc(comments, func(a, b RecentComment) int { return a.UpdatedAt.Compare(b.UpdatedAt) })
	return comments, nil
}

// ListReviewComments lists the inline review comments and submitted review
// bodies on pull request repo#number, oldest first (issue #695).
func (g GHCLI) ListReviewComments(ctx context.Context, repo string, number int) ([]Comment, error) {
	public := g.isPublicRepo(ctx, repo)
	output, err := g.apiGETPaginated(ctx, public, "repos/"+repo+"/pulls/"+strconv.Itoa(number)+"/comments")
	if err != nil {
		return nil, fmt.Errorf("list review comments on #%d: %w: %s", number, err, strings.TrimSpace(string(output)))
	}
	var raw []struct {
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"created_at"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("decode review comments on #%d: %w", number, err)
	}
	comments := make([]Comment, 0, len(raw))
	for _, comment := range raw {
		comments = append(comments, Comment{Body: comment.Body, Author: comment.User.Login, CreatedAt: comment.CreatedAt})
	}
	reviews, err := g.pullRequestReviews(ctx, public, repo, number)
	if err != nil {
		return nil, err
	}
	for _, review := range reviews {
		comments = append(comments, Comment{Body: review.Body, Author: review.User.Login, CreatedAt: review.SubmittedAt})
	}
	slices.SortStableFunc(comments, func(a, b Comment) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return comments, nil
}

type pullRequestReview struct {
	ID          int64     `json:"id"`
	NodeID      string    `json:"node_id"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submitted_at"`
	User        struct {
		Login string `json:"login"`
	} `json:"user"`
}

// pullRequestReviews lists the submitted reviews on repo#number that carry a
// body. A pending review is visible only to its author, and a review with no
// body says nothing a mention could be in.
func (g GHCLI) pullRequestReviews(ctx context.Context, public bool, repo string, number int) ([]pullRequestReview, error) {
	output, err := g.apiGETPaginated(ctx, public, "repos/"+repo+"/pulls/"+strconv.Itoa(number)+"/reviews?per_page=100")
	if err != nil {
		return nil, fmt.Errorf("list reviews on #%d: %w: %s", number, err, strings.TrimSpace(string(output)))
	}
	var raw []pullRequestReview
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("decode reviews on #%d: %w", number, err)
	}
	reviews := raw[:0]
	for _, review := range raw {
		if strings.EqualFold(review.State, "PENDING") || review.SubmittedAt.IsZero() || strings.TrimSpace(review.Body) == "" {
			continue
		}
		reviews = append(reviews, review)
	}
	return reviews, nil
}

// AddReviewCommentReaction adds an emoji reaction to an inline pull request
// review comment, acknowledging a direct mention posted in one (issue #695).
func (g GHCLI) AddReviewCommentReaction(ctx context.Context, repo string, commentID int64, content string) error {
	output, err := g.run(ctx, "api", "repos/"+repo+"/pulls/comments/"+strconv.FormatInt(commentID, 10)+"/reactions", "-f", "content="+content)
	if err != nil {
		return fmt.Errorf("react to review comment %d: %w: %s", commentID, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// addReactionMutation reacts to any reactable GraphQL node. The REST API has
// no reaction endpoint for a pull request review's body, so this is the only
// way to acknowledge a mention posted in one.
const addReactionMutation = `mutation($subject: ID!, $content: ReactionContent!) { addReaction(input: {subjectId: $subject, content: $content}) { reaction { content } } }`

// AddReviewReaction adds an emoji reaction to the body of a submitted pull
// request review, named by its GraphQL node ID (issue #695). content uses the
// REST spelling, such as "eyes".
func (g GHCLI) AddReviewReaction(ctx context.Context, repo string, nodeID string, content string) error {
	output, err := g.run(ctx, "api", "graphql", "-f", "query="+addReactionMutation, "-f", "subject="+nodeID, "-f", "content="+strings.ToUpper(content))
	if err != nil {
		return fmt.Errorf("react to review %s in %s: %w: %s", nodeID, repo, err, strings.TrimSpace(string(output)))
	}
	return nil
}
