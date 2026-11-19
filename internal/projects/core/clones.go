package core

// CloneEntry is one checkout directory aviary has on record for a job (issue
// #830), as the settings dashboards list it.
type CloneEntry struct {
	Path string `json:"path"`
	// Repository is the OWNER/REPO the clone was made from, and Issue the
	// issue it was made for. Either is empty or zero when it is not known.
	Repository string `json:"repository,omitempty"`
	Issue      int    `json:"issue,omitempty"`
	// PullRequest is the job's pull request once one is known, hosted in
	// PullRequestRepository when that differs from Repository.
	PullRequest           int    `json:"pullRequest,omitempty"`
	PullRequestRepository string `json:"pullRequestRepository,omitempty"`
	// Job is the work key (target#number) the clone belongs to.
	Job string `json:"job,omitempty"`
	// InUse reports that the clone may still be needed, so clearing keeps it.
	// Reason says why it is in use or why its status could not be verified.
	InUse  bool   `json:"inUse"`
	Reason string `json:"reason,omitempty"`
	// Size is the clone's bytes on disk: measured by listing, and right
	// before removal by a clear. Nil means it could not be measured in time.
	Size *int64 `json:"size,omitempty"`
}

// ClonesSnapshot lists every recorded clone and the temp directories new
// clones are placed in, in the order they are tried. TotalSize sums the
// clones whose size was measured; Unmeasured counts the ones left out.
type ClonesSnapshot struct {
	Roots      []string     `json:"roots"`
	Clones     []CloneEntry `json:"clones"`
	TotalSize  int64        `json:"totalSize"`
	Unmeasured int          `json:"unmeasured,omitempty"`
}

// ClearClonesResult reports what clearing unused clones removed, how many
// bytes that reclaimed, how many clones it kept, and any removal failures.
// Reclaimed is the sum of the removed entries' sizes; failed removals count
// nothing.
type ClearClonesResult struct {
	Removed   []CloneEntry `json:"removed"`
	Kept      int          `json:"kept"`
	Reclaimed int64        `json:"reclaimed"`
	Errors    []string     `json:"errors,omitempty"`
}
