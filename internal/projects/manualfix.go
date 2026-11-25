package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/store"
)

// fixRequest is an issue asked to be fixed on a manual-mode project (issue
// #1307). Manual projects are never searched, so the poll reads each
// requested issue back by number instead, and dispatches it as if a search
// had listed it.
type fixRequest struct {
	Target      string    `json:"target"`
	Repository  string    `json:"repository"`
	Number      int       `json:"number"`
	RequestedAt time.Time `json:"requested_at"`
}

func (r fixRequest) key() string { return r.Target + "#" + strconv.Itoa(r.Number) }

// fixRequests is the persisted list of fix requests, saved beside the work
// state so requested issues are still followed after a restart.
type fixRequests struct {
	mu     sync.Mutex
	path   string
	loaded bool
	items  []fixRequest
}

func (f *fixRequests) loadLocked() {
	if f.loaded {
		return
	}
	f.loaded = true
	if f.path == "" {
		return
	}
	if saved, err := store.ReadJSON[[]fixRequest](f.path); err == nil {
		f.items = saved
	}
}

func (f *fixRequests) saveLocked() error {
	if f.path == "" {
		return nil
	}
	if len(f.items) == 0 {
		if err := os.Remove(f.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return store.WriteJSON(f.path, f.items)
}

// add records request unless its issue is already requested.
func (f *fixRequests) add(request fixRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadLocked()
	if slices.ContainsFunc(f.items, func(existing fixRequest) bool { return existing.key() == request.key() }) {
		return nil
	}
	f.items = append(f.items, request)
	return f.saveLocked()
}

// forTarget returns target's requests, in the order they were made.
func (f *fixRequests) forTarget(target string) []fixRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadLocked()
	var requests []fixRequest
	for _, request := range f.items {
		if request.Target == target {
			requests = append(requests, request)
		}
	}
	return requests
}

// remove drops the request keyed key.
func (f *fixRequests) remove(key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadLocked()
	before := len(f.items)
	f.items = slices.DeleteFunc(f.items, func(existing fixRequest) bool { return existing.key() == key })
	if len(f.items) == before {
		return nil
	}
	return f.saveLocked()
}

// fixRequests returns the engine's fix requests, saved beside its work
// state.
func (w *Engine) fixRequests() *fixRequests {
	w.fixesOnce.Do(func() {
		path := ""
		if w.StatePath != "" {
			path = filepath.Join(filepath.Dir(w.StatePath), "fix-requests.json")
		}
		w.fixes = &fixRequests{path: path}
	})
	return w.fixes
}

// RequestFix asks for issue number of a manual-mode target to be fixed, and
// checks for it at once. repository is the OWNER/REPO the issue lives in,
// needed only when target is not a repository.
func (w *Engine) RequestFix(target, repository string, number int) error {
	if repository == "" {
		repository = issueRepository(target, Issue{})
	}
	if err := w.fixRequests().add(fixRequest{Target: target, Repository: repository, Number: number, RequestedAt: time.Now()}); err != nil {
		return err
	}
	w.nudgePoll()
	return nil
}

// requestedFixIssues reads back target's requested issues for the poll. A
// request is dropped once its issue closes or its work completes; status
// holds the work state of every issue not running right now.
func (w *Engine) requestedFixIssues(ctx context.Context, hydrator IssueHydrator, target string, status map[string]string) []Issue {
	var issues []Issue
	for _, request := range w.fixRequests().forTarget(target) {
		key := request.key()
		if status[key] == "completed" {
			_ = w.fixRequests().remove(key)
			continue
		}
		issue := Issue{Number: request.Number, Target: target, Repository: request.Repository}
		if err := hydrator.HydrateIssue(ctx, issue.Repository, &issue); err != nil {
			if ctx.Err() == nil {
				w.logChanged("fix-request-"+key, err.Error(), "issue #%d requested fix could not read the issue: %v", request.Number, err)
			}
			continue
		}
		if issue.State == "closed" {
			w.logf("issue #%d is closed; dropping its requested fix", request.Number)
			_ = w.fixRequests().remove(key)
			continue
		}
		if isProjectTarget(target) && issue.ProjectStatus == "" {
			// A requested board issue is ready by request, not by its
			// board column, which a manual project never reads.
			issue.ProjectStatus = projectReadyState(w.ReadyState, "")
		}
		issues = append(issues, issue)
	}
	return issues
}

// watchedTargets drops the manual-mode targets from targets.
func (w *Engine) watchedTargets(targets []string) []string {
	return slices.DeleteFunc(slices.Clone(targets), w.Manual.Has)
}

// watchedIssues drops the issues of manual-mode targets from issues.
func (w *Engine) watchedIssues(issues []Issue) []Issue {
	return slices.DeleteFunc(slices.Clone(issues), func(issue Issue) bool { return w.Manual.Has(issue.Target) })
}
