package browser

import "testing"

// extractCommentFixture loads one conversation fixture and returns what the
// comment extractor made of it, running the real script in a real browser
// against captured GitHub markup.
func extractCommentFixture(t *testing.T, tab *Tab, baseURL, fixture string) commentList {
	t.Helper()
	if err := tab.Navigate(baseURL + "/" + fixture); err != nil {
		t.Fatalf("navigate to %s: %v", fixture, err)
	}
	var list commentList
	if err := tab.Eval(commentsScript, &list); err != nil {
		t.Fatalf("evaluate comment extractor on %s: %v", fixture, err)
	}
	return list
}

// TestCommentScriptAgainstFixtures runs the comment extractor against captured
// GitHub conversation markup: a thread with comments, a conversation with none,
// and a page that has not rendered yet.
func TestCommentScriptAgainstFixtures(t *testing.T) {
	tab, baseURL := fixtureTab(t)

	t.Run("thread with comments", func(t *testing.T) {
		list := extractCommentFixture(t, tab, baseURL, "github-issue-comments.html")
		if !list.Recognized {
			t.Fatal("a rendered conversation was not recognised")
		}
		if len(list.Comments) != 2 {
			t.Fatalf("got %d comments, want 2: %+v", len(list.Comments), list.Comments)
		}
		// The issue body is rendered in the same markup the comments are, but
		// it is not a comment and the API does not return it as one.
		for _, comment := range list.Comments {
			if comment.Body == "5s is way too low." {
				t.Fatal("the issue body was extracted as a comment")
			}
		}
		first := list.Comments[0]
		if first.Author != "lsegal" {
			t.Fatalf("first author = %q, want lsegal", first.Author)
		}
		if want := "Starting work on this issue /aviary:7EC95FCB"; first.Body != want {
			t.Fatalf("first body = %q, want %q", first.Body, want)
		}
		if want := "2026-08-31T04:25:23.000Z"; first.CreatedAt != want {
			t.Fatalf("first timestamp = %q, want %q", first.CreatedAt, want)
		}
		second := list.Comments[1]
		if second.Author != "octocat" {
			t.Fatalf("second author = %q, want octocat", second.Author)
		}
		if want := "Does anyone have this? /aviary:AAAA1111"; second.Body != want {
			t.Fatalf("second body = %q, want %q", second.Body, want)
		}
		// Both bodies are asserted verbatim above, including the /Aviary:ID
		// signature: that is exactly what the root package's handoff parser is
		// handed, so an extraction that mangled either would be caught here
		// rather than only when a handshake failed to recognise a claim.
	})

	t.Run("no comments", func(t *testing.T) {
		list := extractCommentFixture(t, tab, baseURL, "github-issue-no-comments.html")
		if !list.Recognized {
			t.Fatal("a conversation with no comments was not recognised, so it would fall back to the API forever")
		}
		if len(list.Comments) != 0 {
			t.Fatalf("got %d comments from a conversation with none: %+v", len(list.Comments), list.Comments)
		}
	})

	t.Run("not rendered", func(t *testing.T) {
		list := extractCommentFixture(t, tab, baseURL, "github-issue-loading.html")
		if list.Recognized {
			t.Fatal("a page that has not drawn was recognised as an empty conversation")
		}
	})
}
