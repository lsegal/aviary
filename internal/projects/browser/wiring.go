package browser

import "github.com/lsegal/aviary/internal/projects/core"

// The accessors here report how a reader was built. The root package assembles
// browser mode's sources out of these types and asserts that the run really
// reads through the ones it configured, which it cannot see from the outside
// otherwise: everything a reader is given is settled at construction and never
// changes afterwards.

// Filter and AllIssues report the search the issues page is read with, mirroring
// the run's own -filter and -all-issues.
func (s *IssueSource) Filter() string { return s.filter }

// AllIssues reports whether the filter is dropped in favour of every open issue.
func (s *IssueSource) AllIssues() bool { return s.allIssues }

// Vision reports the screenshot fallback attached to this source, nil when
// the browser vision fallback was not passed.
func (s *IssueSource) Vision() *Vision { return s.vision }

// ReadsPages reports whether the source has a tab to read pages in.
func (s *IssueSource) ReadsPages() bool { return s.pageFor != nil }

// Hydrates reports whether the source can fill in the dispatch-only fields a
// rendered page does not carry.
func (s *IssueSource) Hydrates() bool {
	return s.Hydration != nil && s.hydrate != nil && s.handled != nil
}

// ReadsPages reports whether the comment source has a tab to read conversations
// in.
func (c *CommentSource) ReadsPages() bool { return c.pageFor != nil }

// API reports the client the source posts through and falls back to.
func (c *CommentSource) API() core.CommentClient { return c.api }

// Source reports the reader the guard wraps.
func (g SignInGuard) Source() core.IssueSource { return g.source }

// Recovery reports the sign-in recovery a signed-out read is handed to.
func (g SignInGuard) Recovery() *SignInRecovery { return g.recovery }
