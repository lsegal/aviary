package projects

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// Identity uniquely names one running aviary instance. It lives only in
// memory: a restarted instance gets a new identity, which is why ownership
// of previously claimed work must be renegotiated rather than assumed.
type Identity string

func newIdentity() (Identity, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("create instance identity: %w", err)
	}
	return Identity(strings.ToUpper(hex.EncodeToString(b))), nil
}

// The comment types live in package core so the browser driver can implement
// CommentClient without importing the root package.
type (
	// Comment is a comment on an issue or pull request.
	Comment = core.Comment
	// CommentPoster posts comments.
	CommentPoster = core.CommentPoster
	// CommentLister lists comments.
	CommentLister = core.CommentLister
	// CommentClient posts and lists comments.
	CommentClient = core.CommentClient
	// CommentReactor reacts to issue comments.
	CommentReactor = core.CommentReactor
	// ReviewReactor reacts to pull request review comments.
	ReviewReactor = core.ReviewReactor
	// ReviewCommentLister lists pull request review comments.
	ReviewCommentLister = core.ReviewCommentLister
)

// ownershipWaitDuration is the minimum grace period a reaping instance must
// wait after asking "does anyone have this?" before claiming abandoned work.
const ownershipWaitDuration = 2 * time.Minute

// staleClaimDuration is how old the newest claim from another instance must
// be before a periodic reap treats the work as abandoned and re-opens the
// "does anyone have this?" handshake. Reaps run repeatedly, so
// without this the handshake would be re-posted on every pass and spam
// issues that a live instance is still working.
const staleClaimDuration = 5 * time.Minute

const (
	askClaimBody        = "Does anyone have this?"
	startingClaimBody   = "Starting work on this issue"
	continuingClaimBody = "Continuing work on this issue"
	presenceClaimBody   = "I am working on this"
	releaseClaimBody    = "Releasing this issue"
	stoppingClaimBody   = "Stopping work on this issue"
)

type claimKind int

const (
	claimUnknown claimKind = iota
	claimAsking
	claimStarting
	claimContinuing
	claimPresence
	claimReleasing
	claimStopping
)

// identityPattern matches the trailing "/aviary:UUID" signature that every
// handoff comment must carry.
var identityPattern = regexp.MustCompile(`/aviary:(\S+)\s*$`)

// mentionedIdentity reports whether a comment directly addresses this aviary
// instance using the public @/aviary:ID meta-mention syntax. Delimiters prevent
// one instance ID from matching a longer ID that merely shares its prefix.
func mentionedIdentity(body string, id Identity) bool {
	if id == "" {
		return false
	}
	pattern := `(^|\s)@/aviary:` + regexp.QuoteMeta(string(id)) + `($|\s|[.,!?;:])`
	return regexp.MustCompile(pattern).MatchString(body)
}

// signComment appends the instance identity signature used to attribute a
// handoff comment to a specific aviary instance.
func signComment(body string, id Identity) string {
	return fmt.Sprintf("%s /aviary:%s", body, id)
}

// commenterAllowed reports whether login may trigger a direct-mention gh-fix
// run. An empty allowed list places no restriction on the author (issue
// #294 has the CLI default this to the authenticated gh user).
func commenterAllowed(login string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	if login == "" {
		return false
	}
	for _, candidate := range allowed {
		if strings.EqualFold(candidate, login) {
			return true
		}
	}
	return false
}

// authorizedDirectMention reports whether the most recent comment on
// repo#number is a safe, actionable direct mention of this aviary instance
// : it must be the very last comment, so a stale mention buried
// under newer conversation cannot silently retrigger a run; it must contain
// the @/aviary:ID mention syntax addressed to id; and it must be posted by an
// allowed commenter.
func authorizedDirectMention(ctx context.Context, comments CommentLister, repo string, number int, id Identity, allowed []string) (bool, error) {
	list, err := comments.ListComments(ctx, repo, number)
	if err != nil {
		return false, err
	}
	if len(list) == 0 {
		return false, nil
	}
	last := list[len(list)-1]
	return mentionedIdentity(last.Body, id) && commenterAllowed(last.Author, allowed), nil
}

// parseClaim classifies a comment body as one of the handoff protocol
// messages and extracts the signing identity. It returns ok=false for
// comments that are not part of the handoff protocol.
func parseClaim(body string) (kind claimKind, id Identity, ok bool) {
	trimmed := strings.TrimSpace(body)
	match := identityPattern.FindStringSubmatch(trimmed)
	if match == nil {
		return claimUnknown, "", false
	}
	id = Identity(match[1])
	text := strings.TrimSpace(strings.TrimSuffix(trimmed, match[0]))
	switch {
	case strings.HasPrefix(text, askClaimBody):
		kind = claimAsking
	case strings.HasPrefix(text, startingClaimBody):
		kind = claimStarting
	case strings.HasPrefix(text, continuingClaimBody):
		kind = claimContinuing
	case strings.HasPrefix(text, presenceClaimBody):
		kind = claimPresence
	case strings.HasPrefix(text, releaseClaimBody):
		kind = claimReleasing
	case strings.HasPrefix(text, stoppingClaimBody):
		kind = claimStopping
	default:
		return claimUnknown, "", false
	}
	return kind, id, true
}

// claimedByOther reports whether any comment created at or after the given
// time is a starting, continuing, or presence claim signed by an identity
// other than self, and returns that identity so the caller can name the
// winning instance in its logs. Per the handoff protocol, the most recent
// claim wins, so any such comment means another instance has already taken
// ownership.
func claimedByOther(comments []Comment, after time.Time, self Identity) (Identity, bool) {
	for _, comment := range comments {
		if comment.CreatedAt.Before(after) {
			continue
		}
		kind, id, ok := parseClaim(comment.Body)
		if !ok || id == self {
			continue
		}
		switch kind {
		case claimStarting, claimContinuing, claimPresence:
			return id, true
		}
	}
	return "", false
}

// latestClaimMatching returns the creation time, position, and signing
// identity of the most recent standing claim whose signer satisfies match. A
// claim stands only while it is that signer's newest handoff comment: an
// instance that released the work holds nothing, so its withdrawn
// claim is not reported as ownership even though the claim comment is still on
// the ticket.
//
// Recency is decided by timestamp and, for comments sharing one, by their
// order in the list, which GitHub returns in creation order. Two comments
// posted in quick succession can carry the same timestamp -- Windows's wall
// clock has a resolution of roughly 15ms, and a withdrawal posted right after
// the claim it withdraws lands well inside that -- so ordering on time alone
// let the superseded claim win. The returned position lets a
// caller break the same tie against a claim signed by another identity.
func latestClaimMatching(comments []Comment, match func(Identity) bool) (time.Time, int, Identity, bool) {
	type standingClaim struct {
		at       time.Time
		index    int
		released bool
	}
	newest := make(map[Identity]standingClaim)
	order := make([]Identity, 0, len(comments))
	for index, comment := range comments {
		kind, id, ok := parseClaim(comment.Body)
		if !ok || !match(id) {
			continue
		}
		// A shutdown's "Stopping work" message is not a withdrawal here: it
		// only frees the work once the ownership count reaches zero (issue
		// #420), so a claim it leaves standing still stands.
		switch kind {
		case claimStarting, claimContinuing, claimPresence, claimReleasing:
		default:
			continue
		}
		prev, seen := newest[id]
		if !seen {
			order = append(order, id)
		} else if comment.CreatedAt.Before(prev.at) {
			continue
		}
		newest[id] = standingClaim{at: comment.CreatedAt, index: index, released: kind == claimReleasing}
	}
	var latest time.Time
	var latestIndex int
	var owner Identity
	found := false
	for _, id := range order {
		claim := newest[id]
		if claim.released {
			continue
		}
		if !found || claimNewer(claim.at, claim.index, latest, latestIndex) {
			latest, latestIndex, owner, found = claim.at, claim.index, id, true
		}
	}
	return latest, latestIndex, owner, found
}

// claimNewer reports whether the claim at (at, index) supersedes the one at
// (other, otherIndex). Both positions must index the same comment list, whose
// order breaks a tie between claims sharing a timestamp.
func claimNewer(at time.Time, index int, other time.Time, otherIndex int) bool {
	if at.Equal(other) {
		return index > otherIndex
	}
	return at.After(other)
}

// ownershipCount reads a ticket's handoff comments in order as a reference
// count on its ownership (issue #420): every starting or continuing claim adds
// one, and every "Stopping work" or "Releasing" message takes one away. Claims
// stack, so one stop cannot free work that was claimed twice.
func ownershipCount(comments []Comment) int {
	return ownershipCountBy(comments, func(Identity) bool { return true })
}

// ownershipCountBy is ownershipCount over only the handoff comments whose
// signer satisfies match, such as the share of the count one instance holds.
func ownershipCountBy(comments []Comment, match func(Identity) bool) int {
	count := 0
	for _, comment := range comments {
		kind, id, ok := parseClaim(comment.Body)
		if !ok || !match(id) {
			continue
		}
		switch kind {
		case claimStarting, claimContinuing:
			count++
		case claimStopping, claimReleasing:
			count--
		}
	}
	return count
}

// ownershipRescinded reports that a ticket's owners stopped work on it on a
// graceful shutdown and nobody holds it since: it carries a "Stopping work"
// message and its ownership count is zero or below. Such work is free to pick
// up at once, with no handshake, grace wait, or staleness period. Tickets
// with no stop message keep the older claim rules, so a killed instance's
// claims are still left to the stale-claim reap.
func ownershipRescinded(comments []Comment) bool {
	stopped := false
	for _, comment := range comments {
		if kind, _, ok := parseClaim(comment.Body); ok && kind == claimStopping {
			stopped = true
			break
		}
	}
	return stopped && ownershipCount(comments) <= 0
}

// latestClaimByOther returns the creation time, position, and signing identity
// of the most recent starting, continuing, or presence claim signed by an
// identity other than self.
func latestClaimByOther(comments []Comment, self Identity) (time.Time, int, Identity, bool) {
	return latestClaimMatching(comments, func(id Identity) bool { return id != self })
}

// latestClaimBySelf returns the creation time and position of the most recent
// starting, continuing, or presence claim this instance signed itself.
func latestClaimBySelf(comments []Comment, self Identity) (time.Time, int, bool) {
	at, index, _, ok := latestClaimMatching(comments, func(id Identity) bool { return self != "" && id == self })
	return at, index, ok
}

// claimStanding summarizes the ownership claims standing on a target: the
// newest one signed by another instance, and the newest one this instance
// signed itself. A reap needs both, because the last claim posted wins and an
// instance that only looked at foreign claims would be blind to work it had
// already claimed and re-open the handshake on it.
type claimStanding struct {
	// Owner is the newest foreign claimant, when there is one.
	Owner        Identity
	OwnerAge     time.Duration
	OwnerClaimed bool
	// OwnerFresh reports that a foreign claim is recent enough that a
	// periodic reap should leave the work alone.
	OwnerFresh bool
	SelfAge    time.Duration
	// SelfHolds reports that this instance posted the newest claim on the
	// target and did so recently enough to still own the work.
	SelfHolds bool
	// Rescinded reports that the work's owners stopped it on a graceful
	// shutdown and its ownership count is zero or below, so it is free to
	// pick up without any handshake or staleness wait (issue #420).
	Rescinded bool
}

// claimStanding reads the target's comments once and reports who currently
// holds it. Work with no claim at all, or one older than staleClaimDuration,
// is fair game for the "does anyone have this?" handshake.
func (w *Engine) claimStanding(ctx context.Context, target ownershipTarget) (claimStanding, error) {
	var standing claimStanding
	if w.Comments == nil {
		return standing, nil
	}
	comments, err := w.Comments.ListComments(ctx, target.Repo, target.Number)
	if err != nil {
		return standing, err
	}
	standing.Rescinded = ownershipRescinded(comments)
	stale := w.staleClaimAfter()
	var ownerAt time.Time
	var ownerIndex int
	if claimedAt, index, owner, ok := latestClaimByOther(comments, w.Identity); ok {
		standing.Owner = owner
		standing.OwnerAge = time.Since(claimedAt)
		standing.OwnerClaimed = true
		standing.OwnerFresh = standing.OwnerAge < stale
		ownerAt, ownerIndex = claimedAt, index
	}
	if claimedAt, index, ok := latestClaimBySelf(comments, w.Identity); ok {
		standing.SelfAge = time.Since(claimedAt)
		standing.SelfHolds = standing.SelfAge < stale &&
			(!standing.OwnerClaimed || claimNewer(claimedAt, index, ownerAt, ownerIndex))
	}
	return standing, nil
}

// handshakeRecord remembers an ownership negotiation this instance ran to
// completion, and what it decided. The reap's other guards are all derived
// from GitHub's own comments; this one is local, so a negotiation cannot be
// re-opened just because a comment read disagrees with what already happened
// .
type handshakeRecord struct {
	At time.Time
	// Claimed reports that the negotiation ended with this instance owning
	// the work, rather than standing down for another instance.
	Claimed bool
}

// handshakeKey names the target a handshake record belongs to. A negotiation
// that moved from the issue to a pull request opened for it is a different
// negotiation, so the number is part of the key.
func handshakeKey(target ownershipTarget) string {
	return fmt.Sprintf("%s#%d", target.Repo, target.Number)
}

// recordHandshake stores the outcome of a negotiation this instance just ran,
// so later reaps on the same target reuse the decision instead of asking the
// same question again.
func (w *Engine) recordHandshake(target ownershipTarget, claimed bool) {
	w.handshakeMu.Lock()
	defer w.handshakeMu.Unlock()
	if w.handshakes == nil {
		w.handshakes = make(map[string]handshakeRecord)
	}
	w.handshakes[handshakeKey(target)] = handshakeRecord{At: time.Now(), Claimed: claimed}
}

// forgetHandshake drops the record of a negotiation whose outcome no longer
// holds, so the next reap renegotiates the target instead of reusing a
// decision this instance has since walked back.
func (w *Engine) forgetHandshake(target ownershipTarget) {
	w.handshakeMu.Lock()
	defer w.handshakeMu.Unlock()
	delete(w.handshakes, handshakeKey(target))
}

// settledHandshake reports a negotiation this instance already ran on target
// recently enough that re-running it would only repost the same ask and the
// same claim. Records older than the staleness window are dropped so genuinely
// abandoned work is renegotiated rather than held forever.
func (w *Engine) settledHandshake(target ownershipTarget) (handshakeRecord, time.Duration, bool) {
	w.handshakeMu.Lock()
	defer w.handshakeMu.Unlock()
	record, ok := w.handshakes[handshakeKey(target)]
	if !ok {
		return handshakeRecord{}, 0, false
	}
	age := time.Since(record.At)
	if age >= w.staleClaimAfter() {
		delete(w.handshakes, handshakeKey(target))
		return handshakeRecord{}, 0, false
	}
	return record, age, true
}

// ownershipTarget identifies where a handoff negotiation should take place:
// the issue itself, or an already open draft pull request linked to it.
type ownershipTarget struct {
	Repo     string
	Number   int
	Continue bool
}

// describe names the target the way handoff log lines refer to it, so a
// reader can tell whether a handshake happened on the issue itself or on an
// already open pull request continuing that work.
func (t ownershipTarget) describe() string {
	if t.Continue {
		return fmt.Sprintf("pull request %s#%d", t.Repo, t.Number)
	}
	return fmt.Sprintf("issue %s#%d", t.Repo, t.Number)
}

// ownershipTargetFor resolves whether an issue already has an open,
// unmerged pull request associated with it. When it does, the handoff
// protocol negotiates on the pull request instead of the issue and resumes
// that branch rather than starting over.
func ownershipTargetFor(ctx context.Context, checker WorkClosureChecker, issue Issue) ownershipTarget {
	repo := issueRepository(issue.Target, issue)
	target := ownershipTarget{Repo: repo, Number: issue.Number}
	if checker == nil {
		return target
	}
	state, err := checker.OriginatingWorkState(ctx, repo, issue.Number)
	if err != nil {
		return target
	}
	for _, pullRequest := range state.PullRequests {
		if pullRequest.Merged || strings.EqualFold(pullRequest.State, "closed") {
			continue
		}
		target.Repo = pullRequest.RepoOr(repo)
		target.Number = pullRequest.Number
		target.Continue = true
		return target
	}
	return target
}

// negotiateOwnership runs the "does anyone have this?" handshake before
// reaping work that already looks claimed (a reappearing issue or an open
// draft pull request with no local record of ownership). It asks,
// waits at least ownershipWaitDuration, and only claims the work if no
// other instance answered or claimed it first. The last instance to post a
// starting/continuing claim always wins.
func (w *Engine) negotiateOwnership(ctx context.Context, target ownershipTarget) (bool, error) {
	if w.Comments == nil {
		return true, nil
	}
	askedAt := time.Now()
	w.logf("%s asking %q as %s; waiting %s for another instance to answer", target.describe(), askClaimBody, w.Identity, ownershipWaitDuration)
	if err := w.Comments.PostComment(ctx, target.Repo, target.Number, signComment(askClaimBody, w.Identity)); err != nil {
		return false, err
	}
	if !w.awaitOwnershipWindow(ctx) {
		return false, ctx.Err()
	}
	comments, err := w.Comments.ListComments(ctx, target.Repo, target.Number)
	if err != nil {
		return false, err
	}
	if owner, ok := claimedByOther(comments, askedAt, w.Identity); ok {
		w.logf("%s answered by instance %s during the handoff window; letting it go", target.describe(), owner)
		return false, nil
	}
	claimBody := startingClaimBody
	if target.Continue {
		claimBody = continuingClaimBody
	}
	w.logf("%s unanswered after %s; claiming it as %s (%q)", target.describe(), ownershipWaitDuration, w.Identity, claimBody)
	return true, w.Comments.PostComment(ctx, target.Repo, target.Number, signComment(claimBody, w.Identity))
}

// releaseOwnership withdraws a claim this instance holds on target but is not
// going to work after all. The claim is posted before the dispatch it
// announces happens, so a dispatch that is skipped afterwards would otherwise
// leave the ticket owned by an instance that is doing nothing with it: other
// instances stand down for work nobody is doing, and the ticket stays
// contested on every later poll. Posting the withdrawal makes the
// released ticket readable as unclaimed by every instance, and dropping the
// local handshake record makes this one renegotiate rather than reuse a
// decision it has just walked back.
func (w *Engine) releaseOwnership(ctx context.Context, target ownershipTarget, reason string) {
	w.forgetHandshake(target)
	if w.Comments == nil {
		return
	}
	if err := w.Comments.PostComment(ctx, target.Repo, target.Number, signComment(releaseClaimBody, w.Identity)); err != nil {
		w.logf("%s claim not withdrawn after %s; the ticket may still read as claimed by %s: %v", target.describe(), reason, w.Identity, err)
		return
	}
	w.logf("%s released by %s (%q); %s", target.describe(), w.Identity, releaseClaimBody, reason)
}

// ownershipTargetsFor lists every ticket an issue's ownership may be claimed
// on: the issue itself, which uncontested pickups claim, and the open pull
// request continuing it, which a handshake negotiates on instead.
func ownershipTargetsFor(ctx context.Context, checker WorkClosureChecker, issue Issue) []ownershipTarget {
	issueTarget := ownershipTarget{Repo: issueRepository(issue.Target, issue), Number: issue.Number}
	targets := []ownershipTarget{issueTarget}
	if pull := ownershipTargetFor(ctx, checker, issue); pull != issueTarget {
		targets = append(targets, pull)
	}
	return targets
}

// rescindOwnership posts "Stopping work on this issue" on every ticket of
// issue's that this instance holds a claim on, as the last step of a graceful
// shutdown once the issue's work is checkpointed (issue #420). That message is
// the -1 of the ownership count, so the next instance to look finds the work
// free at once instead of waiting out a handshake on a claim nobody serves.
func (w *Engine) rescindOwnership(ctx context.Context, issue Issue) {
	if w.Comments == nil || w.Identity == "" {
		return
	}
	checker, _ := w.Issues.(WorkClosureChecker)
	for _, target := range ownershipTargetsFor(ctx, checker, issue) {
		comments, err := w.Comments.ListComments(ctx, target.Repo, target.Number)
		if err != nil {
			w.logf("%s ownership not rescinded on shutdown; reading its claims failed: %v", target.describe(), err)
			continue
		}
		if ownershipCountBy(comments, func(id Identity) bool { return id == w.Identity }) <= 0 {
			continue
		}
		if err := w.Comments.PostComment(ctx, target.Repo, target.Number, signComment(stoppingClaimBody, w.Identity)); err != nil {
			w.logf("%s ownership not rescinded on shutdown: %v", target.describe(), err)
			continue
		}
		w.logf("%s ownership rescinded by %s on shutdown (%q)", target.describe(), w.Identity, stoppingClaimBody)
	}
}

// reclaimRescinded re-announces ownership of resumed work whose claims a
// graceful shutdown rescinded. A restarted instance resumes its sessions
// without the handshake, so without a fresh claim the work would read as free
// and another instance would pick up the same issue (issue #420).
func (w *Engine) reclaimRescinded(ctx context.Context, checker WorkClosureChecker, issue Issue) {
	if w.Comments == nil {
		return
	}
	for _, target := range ownershipTargetsFor(ctx, checker, issue) {
		comments, err := w.Comments.ListComments(ctx, target.Repo, target.Number)
		if err != nil {
			w.logf("%s failed to check for rescinded ownership: %v", target.describe(), err)
			continue
		}
		if !ownershipRescinded(comments) {
			continue
		}
		if err := w.Comments.PostComment(ctx, target.Repo, target.Number, signComment(continuingClaimBody, w.Identity)); err != nil {
			w.logf("%s failed to reclaim rescinded ownership: %v", target.describe(), err)
			continue
		}
		w.logf("%s resumed after its ownership was rescinded; reclaimed it as %s (%q)", target.describe(), w.Identity, continuingClaimBody)
	}
}

// awaitOwnershipWindow blocks for the handoff grace period, or returns
// false early if the context is cancelled first. Tests override
// ownershipWait to avoid real sleeps.
func (w *Engine) awaitOwnershipWindow(ctx context.Context) bool {
	if w.ownershipWait != nil {
		return w.ownershipWait(ctx)
	}
	timer := time.NewTimer(ownershipWaitDuration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// claimComment builds the initial, uncontested ownership comment for an
// issue that no other instance appears to have touched yet.
func claimComment(id Identity, continuing bool) string {
	body := startingClaimBody
	if continuing {
		body = continuingClaimBody
	}
	return signComment(body, id)
}

// lastCommentMatches reports whether body is already the newest comment on a
// ticket. Dispatch uses it before announcing an uncontested pickup so a
// repeated poll cannot stack identical ownership claims.
func (w *Engine) lastCommentMatches(ctx context.Context, repo string, number int, body string) (bool, error) {
	if w.Comments == nil {
		return false, nil
	}
	comments, err := w.Comments.ListComments(ctx, repo, number)
	if err != nil || len(comments) == 0 {
		return false, err
	}
	return comments[len(comments)-1].Body == body, nil
}
