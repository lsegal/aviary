package projects

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/projects/browser"
)

// browserDeliveryEngine is an engine whose issues come through the browser
// delivery client, as on a board with delivery: browser, with gh answering
// through run.
func browserDeliveryEngine(run func(context.Context, ...string) ([]byte, error)) *Engine {
	return &Engine{
		Repo:       "o/r",
		Issues:     browser.NewSignInGuard(&fakeSource{}, nil),
		Candidates: GHCLI{runCommand: run},
	}
}

func TestQueueCandidatesListWithGHInBrowserDelivery(t *testing.T) {
	calls := 0
	w := browserDeliveryEngine(func(_ context.Context, args ...string) ([]byte, error) {
		calls++
		if args[0] == "pr" {
			return []byte(`[{"number":7,"title":"Fix it","url":"u","mergeable":"MERGEABLE","reviewDecision":"APPROVED","author":{"login":"alice"},"statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"}]}]`), nil
		}
		return []byte(`{"data":{"repository":{"discussions":{"nodes":[{"number":3,"title":"How?","url":"d","author":{"login":"bob"},"category":{"name":"Q&A"},"comments":{"totalCount":2}}]}}}}`), nil
	})
	result, err := w.QueueCandidates(context.Background(), "o/r", "", SkillSafeMerge)
	if err != nil {
		t.Fatal(err)
	}
	pulls, _ := result["pull_requests"].([]OpenPullRequest)
	if len(pulls) != 1 || pulls[0] != (OpenPullRequest{Number: 7, Title: "Fix it", Author: "alice", URL: "u", CI: "passing", Review: "approved", Mergeable: "mergeable"}) {
		t.Fatalf("pull_requests = %+v", result["pull_requests"])
	}
	if _, ok := result["unavailable"]; ok {
		t.Fatalf("unavailable = %v", result["unavailable"])
	}
	result, err = w.QueueCandidates(context.Background(), "o/r", "", SkillDiscuss)
	if err != nil {
		t.Fatal(err)
	}
	discussions, _ := result["discussions"].([]OpenDiscussion)
	if len(discussions) != 1 || discussions[0] != (OpenDiscussion{Number: 3, Title: "How?", Author: "bob", URL: "d", Category: "Q&A", Comments: 2}) {
		t.Fatalf("discussions = %+v", result["discussions"])
	}
	// Reopening the dialog soon after reuses both listings.
	for _, skill := range []string{SkillSafeMerge, SkillDiscuss} {
		if _, err := w.QueueCandidates(context.Background(), "o/r", "", skill); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("gh ran %d times, want 2", calls)
	}
}

func TestQueueCandidatesNoticeWhenGHUnavailable(t *testing.T) {
	calls := 0
	w := browserDeliveryEngine(func(context.Context, ...string) ([]byte, error) {
		calls++
		return nil, errors.New("gh: To get started with GitHub CLI, please run: gh auth login")
	})
	for _, c := range []struct{ skill, list, noun string }{
		{SkillSafeMerge, "pull_requests", "pull requests"},
		{SkillDiscuss, "discussions", "discussions"},
	} {
		result, err := w.QueueCandidates(context.Background(), "o/r", "", c.skill)
		if err != nil {
			t.Fatalf("%s: %v", c.skill, err)
		}
		if _, ok := result[c.list]; ok {
			t.Errorf("%s: listed %v though gh failed", c.skill, result[c.list])
		}
		notice, _ := result["unavailable"].(string)
		if !strings.Contains(notice, "Can't list "+c.noun) || !strings.Contains(notice, "gh auth login") {
			t.Errorf("%s: unavailable = %q", c.skill, notice)
		}
		encoded, _ := json.Marshal(result)
		if strings.Contains(string(encoded), "SignInGuard") {
			t.Errorf("%s: result names an internal type: %s", c.skill, encoded)
		}
	}
	// A failed listing is not cached, so signing in takes effect on the next
	// open.
	if _, err := w.QueueCandidates(context.Background(), "o/r", "", SkillSafeMerge); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("gh ran %d times, want 3", calls)
	}
}

func TestQueueCandidatesNoticeWithoutClient(t *testing.T) {
	w := &Engine{Repo: "o/r", Issues: browser.NewSignInGuard(&fakeSource{}, nil)}
	result, err := w.QueueCandidates(context.Background(), "o/r", "", SkillSafeMerge)
	if err != nil {
		t.Fatal(err)
	}
	if notice, _ := result["unavailable"].(string); !strings.Contains(notice, "Can't list pull requests") {
		t.Fatalf("unavailable = %q", notice)
	}
}
