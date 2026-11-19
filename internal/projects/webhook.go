package projects

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var issueReferencePattern = regexp.MustCompile(`(?:^|[\s([{,:;])#([1-9][0-9]*)\b`)

// WebhookEvent is one GitHub webhook delivery, reduced to what the engine
// acts on.
type WebhookEvent struct {
	Kind        string
	Action      string
	Repository  string
	Ref         string
	Before      string
	After       string
	CommitCount int
	IssueNumber int
	IssueTitle  string
	CommentBody string
	// CommentAuthor is the GitHub login that posted CommentBody, used to
	// gate direct-mention triggers to an allowed set of commenters (issue
	// #294).
	CommentAuthor string
	// CommentID is CommentBody's GitHub comment ID, used to react to the
	// comment that carried a direct mention.
	CommentID int64
	// CommentNodeID is CommentBody's GraphQL node ID. A pull_request_review
	// delivery's review body can only be reacted to through it.
	CommentNodeID string
	// MentionedIssues names same-repository issues referenced by a closed
	// issue or pull request. The run loop uses these references to treat the
	// resulting refresh as a continuation sweep, so unowned work goes through
	// the cooperative handoff instead of looking like a brand-new pickup.
	MentionedIssues []int
	// OnPullRequest reports that an issue_comment delivery's comment was
	// posted on a pull request rather than an issue, and ClosesIssues names
	// the issues that pull request's description closes, so a direct mention
	// posted there reaches the issue it fixes. Review comment
	// and review deliveries are always on a pull request, and IssueNumber
	// names it.
	OnPullRequest bool
	ClosesIssues  []int
	// DiscussionNumber and DiscussionTitle carry the thread a `discussion`
	// delivery names. They are kept separate from the issue fields because a
	// discussion and an issue can share a number, and the issue fields key
	// the follow-up refresh chain.
	DiscussionNumber int
	DiscussionTitle  string
}

// WebhookHandler accepts GitHub webhook deliveries, records useful delivery
// details, and asks the aviary to refresh its issue list. The payload is not
// used as the source of issue data: the next authenticated GitHub CLI query
// remains authoritative.
type WebhookHandler struct {
	Events      chan<- WebhookEvent
	Secret      string
	WebhookPath string
}

func (h WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.WebhookPath != "" && r.URL.Path != h.WebhookPath {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if h.Secret != "" && !validWebhookSignature(h.Secret, body, r.Header.Get("X-Hub-Signature-256")) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	switch r.Header.Get("X-GitHub-Event") {
	case "issues", "pull_request", "push", "ping", "projects_v2_item", "issue_comment", "pull_request_review", "pull_request_review_comment", "discussion":
		event := decodeWebhookEvent(r.Header.Get("X-GitHub-Event"), body)
		select {
		case h.Events <- event:
		default:
		}
		w.WriteHeader(http.StatusAccepted)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func decodeWebhookEvent(kind string, body []byte) WebhookEvent {
	event := WebhookEvent{Kind: kind}
	var payload struct {
		Action     string `json:"action"`
		Ref        string `json:"ref"`
		Before     string `json:"before"`
		After      string `json:"after"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Issue struct {
			Number      int              `json:"number"`
			Title       string           `json:"title"`
			Body        string           `json:"body"`
			PullRequest *json.RawMessage `json:"pull_request"`
		} `json:"issue"`
		PullRequest struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Body   string `json:"body"`
		} `json:"pull_request"`
		Discussion struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
		} `json:"discussion"`
		Comment struct {
			ID     int64  `json:"id"`
			NodeID string `json:"node_id"`
			Body   string `json:"body"`
			User   struct {
				Login string `json:"login"`
			} `json:"user"`
		} `json:"comment"`
		Review struct {
			ID     int64  `json:"id"`
			NodeID string `json:"node_id"`
			Body   string `json:"body"`
			User   struct {
				Login string `json:"login"`
			} `json:"user"`
		} `json:"review"`
		Commits []json.RawMessage `json:"commits"`
	}
	if json.Unmarshal(body, &payload) == nil {
		event.Action = payload.Action
		event.Repository = payload.Repository.FullName
		event.Ref = payload.Ref
		event.Before = payload.Before
		event.After = payload.After
		event.CommitCount = len(payload.Commits)
		event.IssueNumber = payload.Issue.Number
		event.IssueTitle = payload.Issue.Title
		event.CommentBody = payload.Comment.Body
		event.CommentAuthor = payload.Comment.User.Login
		event.CommentID = payload.Comment.ID
		body := payload.Issue.Body
		if kind == "pull_request" {
			body = payload.PullRequest.Body
		}
		event.MentionedIssues = mentionedIssueNumbers(body)
		if kind == "issue_comment" && payload.Issue.PullRequest != nil {
			event.OnPullRequest = true
			event.ClosesIssues = closingIssueNumbers(payload.Issue.Body, event.Repository)
		}
		switch kind {
		case "pull_request_review_comment", "pull_request_review":
			event.IssueNumber = payload.PullRequest.Number
			event.IssueTitle = payload.PullRequest.Title
			event.OnPullRequest = true
			event.ClosesIssues = closingIssueNumbers(payload.PullRequest.Body, event.Repository)
			event.CommentNodeID = payload.Comment.NodeID
			if kind == "pull_request_review" {
				event.CommentBody = payload.Review.Body
				event.CommentAuthor = payload.Review.User.Login
				event.CommentID = payload.Review.ID
				event.CommentNodeID = payload.Review.NodeID
			}
		}
		event.DiscussionNumber = payload.Discussion.Number
		event.DiscussionTitle = payload.Discussion.Title
	}
	return event
}

// mentionedIssueNumbers extracts same-repository #123 references in first
// appearance order. Qualified owner/repo#123 references are deliberately not
// matched because they can name work outside the delivery's repository.
func mentionedIssueNumbers(body string) []int {
	matches := issueReferencePattern.FindAllStringSubmatch(body, -1)
	numbers := make([]int, 0, len(matches))
	seen := make(map[int]bool, len(matches))
	for _, match := range matches {
		number, err := strconv.Atoi(match[1])
		if err != nil || number <= 0 || seen[number] {
			continue
		}
		seen[number] = true
		numbers = append(numbers, number)
	}
	return numbers
}

func validWebhookSignature(secret string, body []byte, header string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	expected := hmac.New(sha256.New, []byte(secret))
	_, _ = expected.Write(body)
	actual, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	return err == nil && hmac.Equal(actual, expected.Sum(nil))
}
