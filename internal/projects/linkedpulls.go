package projects

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// linkedPullRequests is the limited watch glorp derives from the open pull
// requests that work on an issue but live in another repository, such as a
// fork (issue #723). The configured targets and the mention scans only cover
// the issue repositories, so without it a review, a mention, or a merge on such
// a pull request goes unseen once the agent has released its slot. It names
// only those pull requests, never their repositories' other issues, so nothing
// else in those repositories is watched or dispatched.
type linkedPullRequests struct {
	mu sync.Mutex
	// byIssue maps an issue's "repo#number" key to its open pull requests
	// hosted outside the issue's repository, as "repo#number" keys.
	byIssue map[string][]string
}

func newLinkedPullRequests() *linkedPullRequests {
	return &linkedPullRequests{byIssue: make(map[string][]string)}
}

// record replaces what is known about the pull requests linked to issue
// repo#number with what state reports. Only open, unmerged pull requests in
// another repository are kept: a pull request in the issue's own repository is
// already covered, and a merged or closed one has nothing left to watch.
func (l *linkedPullRequests) record(repo string, number int, state OriginatingWorkState) {
	if l == nil || repo == "" || number <= 0 {
		return
	}
	var pulls []string
	for _, pullRequest := range state.PullRequests {
		if pullRequest.InRepo(repo) || pullRequest.Number <= 0 || pullRequest.Merged || !strings.EqualFold(pullRequest.State, "open") {
			continue
		}
		pulls = append(pulls, pullRequest.Repository+"#"+strconv.Itoa(pullRequest.Number))
	}
	key := repo + "#" + strconv.Itoa(number)
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(pulls) == 0 || strings.EqualFold(state.IssueState, "closed") {
		delete(l.byIssue, key)
		return
	}
	l.byIssue[key] = pulls
}

// issues returns the "repo#number" keys of the issues pull request
// repo#number is linked to, or nil when it is not a linked pull request.
func (l *linkedPullRequests) issues(repo string, number int) []string {
	if l == nil {
		return nil
	}
	pull := repo + "#" + strconv.Itoa(number)
	l.mu.Lock()
	defer l.mu.Unlock()
	var keys []string
	for issue, pulls := range l.byIssue {
		if slices.ContainsFunc(pulls, func(candidate string) bool { return strings.EqualFold(candidate, pull) }) {
			keys = append(keys, issue)
		}
	}
	slices.Sort(keys)
	return keys
}

// repos returns the repositories hosting linked pull requests, sorted.
func (l *linkedPullRequests) repos() []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var repos []string
	for _, pulls := range l.byIssue {
		for _, pull := range pulls {
			repo, _, _ := strings.Cut(pull, "#")
			if !slices.ContainsFunc(repos, func(candidate string) bool { return strings.EqualFold(candidate, repo) }) {
				repos = append(repos, repo)
			}
		}
	}
	slices.Sort(repos)
	return repos
}

// linkedPullRequestRecorder is a WorkClosureChecker that records the linked
// pull requests of every issue state it reads, so every check glorp already
// makes on active, paused, and completed work keeps the derived watch current.
type linkedPullRequestRecorder struct {
	WorkClosureChecker
	linked *linkedPullRequests
}

func (r linkedPullRequestRecorder) OriginatingWorkState(ctx context.Context, repo string, number int) (OriginatingWorkState, error) {
	state, err := r.WorkClosureChecker.OriginatingWorkState(ctx, repo, number)
	if err == nil {
		r.linked.record(repo, number, state)
	}
	return state, err
}

// recordLinkedPullRequests wraps checker so the states it reads feed linked.
// A nil checker stays nil.
func recordLinkedPullRequests(checker WorkClosureChecker, linked *linkedPullRequests) WorkClosureChecker {
	if checker == nil {
		return nil
	}
	return linkedPullRequestRecorder{WorkClosureChecker: checker, linked: linked}
}

// linkedIssueKeys maps keys naming issues in a linked pull request's own
// repository to the issues that pull request is linked to. A comment on a
// linked pull request addresses the issue it works, not whatever issue its
// own repository numbers the same, so the result replaces keys when the pull
// request is linked and is keys unchanged otherwise.
func (s *mentionScan) linkedIssueKeys(repo string, number int, keys []string) []string {
	if linked := s.linked.issues(repo, number); len(linked) > 0 {
		return linked
	}
	return keys
}
