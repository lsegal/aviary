package projects

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestWebhookHandlerTriggersSupportedEvents(t *testing.T) {
	events := make(chan WebhookEvent, 1)
	h := WebhookHandler{Events: events, WebhookPath: "/webhook"}
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"action":"opened"}`))
	req.Header.Set("X-GitHub-Event", "issues")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusAccepted)
	}
	select {
	case event := <-events:
		if event.Kind != "issues" || event.Action != "opened" {
			t.Fatalf("event = %#v", event)
		}
	default:
		t.Fatal("webhook did not trigger a refresh")
	}
}

func TestWebhookHandlerTriggersPullRequestEvents(t *testing.T) {
	events := make(chan WebhookEvent, 1)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"action":"closed"}`))
	req.Header.Set("X-GitHub-Event", "pull_request")
	res := httptest.NewRecorder()
	WebhookHandler{Events: events, WebhookPath: "/webhook"}.ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusAccepted)
	}
	select {
	case event := <-events:
		if event.Kind != "pull_request" || event.Action != "closed" {
			t.Fatalf("event = %#v", event)
		}
	default:
		t.Fatal("pull request webhook did not trigger a refresh")
	}
}

func TestWebhookHandlerTriggersProjectItemEvents(t *testing.T) {
	events := make(chan WebhookEvent, 1)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"action":"edited"}`))
	req.Header.Set("X-GitHub-Event", "projects_v2_item")
	res := httptest.NewRecorder()
	WebhookHandler{Events: events, WebhookPath: "/webhook"}.ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusAccepted)
	}
	select {
	case event := <-events:
		if event.Kind != "projects_v2_item" || event.Action != "edited" {
			t.Fatalf("event = %#v", event)
		}
	default:
		t.Fatal("project item webhook did not trigger a refresh")
	}
}

func TestWebhookHandlerTriggersIssueCommentEvents(t *testing.T) {
	events := make(chan WebhookEvent, 1)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"action":"created","repository":{"full_name":"o/r"},"issue":{"number":7},"comment":{"body":"Does anyone have this? /aviary:ABC"}}`))
	req.Header.Set("X-GitHub-Event", "issue_comment")
	res := httptest.NewRecorder()
	WebhookHandler{Events: events, WebhookPath: "/webhook"}.ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusAccepted)
	}
	select {
	case event := <-events:
		if event.Kind != "issue_comment" || event.Action != "created" || event.Repository != "o/r" || event.IssueNumber != 7 || event.CommentBody != "Does anyone have this? /aviary:ABC" {
			t.Fatalf("event = %#v", event)
		}
	default:
		t.Fatal("issue comment webhook did not trigger a refresh")
	}
}

func TestWebhookHandlerValidatesSignature(t *testing.T) {
	secret := "test-secret"
	body := []byte(`{"ref":"refs/heads/main"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", fmt.Sprintf("sha256=%x", mac.Sum(nil)))
	res := httptest.NewRecorder()
	WebhookHandler{Events: make(chan WebhookEvent, 1), Secret: secret, WebhookPath: "/webhook"}.ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusAccepted)
	}

	bad := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	bad.Header.Set("X-GitHub-Event", "push")
	bad.Header.Set("X-Hub-Signature-256", "sha256=00")
	badRes := httptest.NewRecorder()
	WebhookHandler{Events: make(chan WebhookEvent, 1), Secret: secret, WebhookPath: "/webhook"}.ServeHTTP(badRes, bad)
	if badRes.Code != http.StatusUnauthorized {
		t.Fatalf("invalid signature status = %d, want %d", badRes.Code, http.StatusUnauthorized)
	}
}

func TestDecodeWebhookEventIncludesPushDetails(t *testing.T) {
	event := decodeWebhookEvent("push", []byte(`{"ref":"refs/heads/main","before":"abc","after":"def","repository":{"full_name":"o/r"},"commits":[{},{}]}`))
	if event.Kind != "push" || event.Repository != "o/r" || event.Ref != "refs/heads/main" || event.Before != "abc" || event.After != "def" || event.CommitCount != 2 {
		t.Fatalf("event = %#v", event)
	}
}

func TestDecodeWebhookEventIncludesIssueDetails(t *testing.T) {
	event := decodeWebhookEvent("issues", []byte(`{"action":"opened","repository":{"full_name":"o/r"},"issue":{"number":54,"title":"new bug","body":"Continues #7 and #7 after #12, not other/repo#99."}}`))
	if event.Kind != "issues" || event.Action != "opened" || event.Repository != "o/r" || event.IssueNumber != 54 || event.IssueTitle != "new bug" {
		t.Fatalf("event = %#v", event)
	}
	if !reflect.DeepEqual(event.MentionedIssues, []int{7, 12}) {
		t.Fatalf("mentioned issues = %v, want [7 12]", event.MentionedIssues)
	}
}

func TestDecodeWebhookEventIncludesPullRequestMentions(t *testing.T) {
	event := decodeWebhookEvent("pull_request", []byte(`{"action":"closed","repository":{"full_name":"o/r"},"pull_request":{"body":"Unblocks #7."}}`))
	if !reflect.DeepEqual(event.MentionedIssues, []int{7}) {
		t.Fatalf("mentioned issues = %v, want [7]", event.MentionedIssues)
	}
}

func TestDecodeWebhookEventIncludesCommentDetails(t *testing.T) {
	event := decodeWebhookEvent("issue_comment", []byte(`{"action":"created","repository":{"full_name":"o/r"},"issue":{"number":54},"comment":{"id":9001,"body":"Does anyone have this? /aviary:ABC","user":{"login":"lsegal"}}}`))
	if event.Kind != "issue_comment" || event.Action != "created" || event.Repository != "o/r" || event.IssueNumber != 54 || event.CommentBody != "Does anyone have this? /aviary:ABC" || event.CommentAuthor != "lsegal" || event.CommentID != 9001 {
		t.Fatalf("event = %#v", event)
	}
}

// A new Discussions thread arrives as a `discussion` delivery, which is the
// push-mode counterpart to polling a Discussions-board target.
func TestWebhookHandlerTriggersDiscussionEvents(t *testing.T) {
	events := make(chan WebhookEvent, 1)
	body := `{"action":"created","repository":{"full_name":"o/r"},"discussion":{"number":7,"title":"How do I run it?"}}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "discussion")
	res := httptest.NewRecorder()
	WebhookHandler{Events: events, WebhookPath: "/webhook"}.ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusAccepted)
	}
	select {
	case event := <-events:
		if event.Kind != "discussion" || event.Action != "created" || event.Repository != "o/r" {
			t.Fatalf("event = %#v", event)
		}
		if event.DiscussionNumber != 7 || event.DiscussionTitle != "How do I run it?" {
			t.Fatalf("event = %#v", event)
		}
		// A discussion must not be mistaken for an issue: the two number
		// spaces overlap and the issue fields key the follow-up refresh chain.
		if event.IssueNumber != 0 {
			t.Fatalf("discussion delivery set IssueNumber = %d", event.IssueNumber)
		}
	default:
		t.Fatal("discussion webhook did not trigger a refresh")
	}
}
