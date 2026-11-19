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

// Identity uniquely names one running glorp instance. It lives only in
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
	Comment             = core.Comment
	CommentPoster       = core.CommentPoster
	CommentLister       = core.CommentLister
	CommentClient       = core.CommentClient
	CommentReactor      = core.CommentReactor
	ReviewReactor       = core.ReviewReactor
	ReviewCommentLister = core.ReviewCommentLister
)

// ownershipWaitDuration is the minimum grace period a reaping instance must
// wait after asking "does anyone have this?" before claiming abandoned work.
const ownershipWaitDuration = 2 * time.Minute

// staleClaimDuration is how old the newest claim from another instance must
// be before a periodic reap treats the work as abandoned and re-opens the
// "does anyone have this?" handshake. Reaps run repeatedly (issue #239), so
// without this the handshake would be re-posted on every pass and spam
// issues that a live instance is still working.
const staleClaimDuration = 5 * time.Minute

const (
	askClaimBody        = "Does anyone have this?"
	startingClaimBody   = "Starting work on this issue"
	continuingClaimBody = "Continuing work on this issue"
	presenceClaimBody   = "I am working on this"
	releaseClaimBody    = "Releasing this issue"
)

type claimKind int

const (
	claimUnknown claimKind = iota
	claimAsking
	claimStarting
	claimContinuing
	claimPresence
	claimReleasing
)

// identityPattern matches the trailing "/glorp:UUID" signature that every
// handoff comment must carry.
var identityPattern = regexp.MustCompile(`/glorp:(\S+)\s*$`)

// mentionedIdentity reports whether a comment directly addresses this glorp
// instance using the public @/glorp:ID meta-mention syntax. Delimiters prevent
// one instance ID from matching a longer ID that merely shares its prefix.
func mentionedIdentity(body string, id Identity) bool {
	if id == "" {
		return false
	}
	pattern := `(^|\s)@/glorp:` + regexp.QuoteMeta(string(id)) + `($|\s|[.,!?;:])`
	return regexp.MustCompile(pattern).MatchString(body)
}

// signComment appends the instance identity signature used to attribute a
// handoff comment to a specific glorp instance.
func signComment(body string, id Identity) string {
	return fmt.Sprintf("%s /glorp:%s", body, id)
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
// repo#number is a safe, actionable direct mention of this glorp instance
// (issue #294): it must be the very last comment, so a stale mention buried
// under newer conversation cannot silently retrigger a run; it must contain
// the @/glorp:ID mention syntax addressed to id; and it must be posted by an
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
// instance that released the work (issue #434) holds nothing, so its withdrawn
// claim is not reported as ownership even though the claim comment is still on
// the ticket.
//
// Recency is decided by timestamp and, for comments sharing one, by their
// order in the list, which GitHub returns in creation order. Two comments
// posted in quick succession can carry the same timestamp -- Windows's wall
// clock has a resolution of roughly 15ms, and a withdrawal posted right after
// the claim it withdraws lands well inside that -- so ordering on time alone
// let the superseded claim win (issue #443). The returned position lets a
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
// already claimed and re-open the handshake on it (issue #425).
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
}

// claimStanding reads the target's comments once and reports who currently
// holds it. Work with no claim at all, or one older than staleClaimDuration,
// is fair game for the "does anyone have this?" handshake.
func (w *Glorp) claimStanding(ctx context.Context, target ownershipTarget) (claimStanding, error) {
	var standing claimStanding
	if w.Comments == nil {
		return standing, nil
	}
	comments, err := w.Comments.ListComments(ctx, target.Repo, target.Number)
	if err != nil {
		return standing, err
	}
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
// (issue #432).
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
func (w *Glorp) recordHandshake(target ownershipTarget, claimed bool) {
	w.handshakeMu.Lock()
	defer w.handshakeMu.Unlock()
	if w.handshakes == nil {
		w.handshakes = make(map[string]handshakeRecord)
	}
	w.handshakes[handshakeKey(target)] = handshakeRecord{At: time.Now(), Claimed: claimed}
}

// forgetHandshake drops the record of a negotiation whose outcome no longer
// holds, so the next reap renegotiates the target instead of reusing a
// decision this instance has since walked back (issue #434).
func (w *Glorp) forgetHandshake(target ownershipTarget) {
	w.handshakeMu.Lock()
	defer w.handshakeMu.Unlock()
	delete(w.handshakes, handshakeKey(target))
}

// settledHandshake reports a negotiation this instance already ran on target
// recently enough that re-running it would only repost the same ask and the
// same claim. Records older than the staleness window are dropped so genuinely
// abandoned work is renegotiated rather than held forever.
func (w *Glorp) settledHandshake(target ownershipTarget) (handshakeRecord, time.Duration, bool) {
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
func (w *Glorp) negotiateOwnership(ctx context.Context, target ownershipTarget) (bool, error) {
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
// contested on every later poll (issue #434). Posting the withdrawal makes the
// released ticket readable as unclaimed by every instance, and dropping the
// local handshake record makes this one renegotiate rather than reuse a
// decision it has just walked back.
func (w *Glorp) releaseOwnership(ctx context.Context, target ownershipTarget, reason string) {
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

// awaitOwnershipWindow blocks for the handoff grace period, or returns
// false early if the context is cancelled first. Tests override
// ownershipWait to avoid real sleeps.
func (w *Glorp) awaitOwnershipWindow(ctx context.Context) bool {
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
// repeated poll cannot stack identical ownership claims (issue #633).
func (w *Glorp) lastCommentMatches(ctx context.Context, repo string, number int, body string) (bool, error) {
	if w.Comments == nil {
		return false, nil
	}
	comments, err := w.Comments.ListComments(ctx, repo, number)
	if err != nil || len(comments) == 0 {
		return false, err
	}
	return comments[len(comments)-1].Body == body, nil
}
