package browser

import (
	"testing"

	"github.com/lsegal/aviary/internal/projects/core"
)

// TestIssueSourceReportsItsWiring checks the issue source's accessors report
// exactly what it was built with, and that its page reader opens tabs from the
// browser it was given.
func TestIssueSourceReportsItsWiring(t *testing.T) {
	existing := &Tab{}
	vision := NewVision(nil, nil)
	source := NewIssueSource(&Browser{tabs: map[string]*Tab{"owner/repo": existing}}, IssueSourceOptions{
		Hydrate: &fakeIssueHydrator{}, Handled: func(core.Issue) bool { return false }, Filter: "label:agent", AllIssues: true, Vision: vision,
	})

	if got := source.Filter(); got != "label:agent" {
		t.Errorf("Filter() = %q, want %q", got, "label:agent")
	}
	if !source.AllIssues() {
		t.Error("AllIssues() = false, want true")
	}
	if source.Vision() != vision {
		t.Error("Vision() did not return the fallback the source was built with")
	}
	if !source.ReadsPages() {
		t.Fatal("ReadsPages() = false for a source built on a browser")
	}
	page, err := source.pageFor("owner/repo")
	if err != nil {
		t.Fatalf("pageFor: %v", err)
	}
	if page != existing {
		t.Error("pageFor did not read through the browser's tab for the target")
	}
	if !source.Hydrates() {
		t.Error("Hydrates() = false for a source with a hydrator and a handled check")
	}
}

// TestIssueSourceDefaults checks a source built without a filter, the
// all-issues switch, or the vision fallback reports none of them.
func TestIssueSourceDefaults(t *testing.T) {
	source := NewIssueSource(&Browser{}, IssueSourceOptions{})
	if got := source.Filter(); got != "" {
		t.Errorf("Filter() = %q, want empty", got)
	}
	if source.AllIssues() {
		t.Error("AllIssues() = true, want false")
	}
	if source.Vision() != nil {
		t.Error("Vision() is non-nil without a fallback")
	}
	if (&IssueSource{}).ReadsPages() {
		t.Error("ReadsPages() = true for a source with no page reader")
	}
}

// TestIssueSourceHydratesOnlyWhenFullyWired checks Hydrates needs the shared
// hydration, its hydrator, and its handled check all to be set.
func TestIssueSourceHydratesOnlyWhenFullyWired(t *testing.T) {
	handled := func(core.Issue) bool { return false }
	tests := []struct {
		name   string
		source *IssueSource
		want   bool
	}{
		{"no hydration", &IssueSource{}, false},
		{"no hydrator", &IssueSource{Hydration: NewHydration(nil, handled)}, false},
		{"no handled check", &IssueSource{Hydration: NewHydration(&fakeIssueHydrator{}, nil)}, false},
		{"fully wired", &IssueSource{Hydration: NewHydration(&fakeIssueHydrator{}, handled)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.source.Hydrates(); got != tt.want {
				t.Errorf("Hydrates() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCommentSourceReportsItsWiring checks the comment source reports the API
// client it falls back to and reads pages through the browser it was given.
func TestCommentSourceReportsItsWiring(t *testing.T) {
	existing := &Tab{}
	api := newFakeCommentClient()
	source := NewCommentSource(&Browser{tabs: map[string]*Tab{"conversation": existing}}, api, nil)

	if source.API() != api {
		t.Error("API() did not return the client the source was built with")
	}
	if !source.ReadsPages() {
		t.Fatal("ReadsPages() = false for a source built on a browser")
	}
	page, err := source.pageFor("conversation")
	if err != nil {
		t.Fatalf("pageFor: %v", err)
	}
	if page != existing {
		t.Error("pageFor did not read through the browser's tab")
	}

	bare := &CommentSource{}
	if bare.ReadsPages() {
		t.Error("ReadsPages() = true for a source with no page reader")
	}
	if bare.API() != nil {
		t.Error("API() is non-nil for a source built without a client")
	}
}

// TestSignInGuardReportsItsWiring checks the guard reports the reader it wraps
// and the recovery it hands signed-out reads to.
func TestSignInGuardReportsItsWiring(t *testing.T) {
	source := &stubIssueSource{}
	recovery := &SignInRecovery{}
	guard := NewSignInGuard(source, recovery)

	if guard.Source() != source {
		t.Error("Source() did not return the wrapped reader")
	}
	if guard.Recovery() != recovery {
		t.Error("Recovery() did not return the recovery the guard was built with")
	}

	var empty SignInGuard
	if empty.Source() != nil || empty.Recovery() != nil {
		t.Error("a zero guard reports a reader or recovery it was never given")
	}
}
