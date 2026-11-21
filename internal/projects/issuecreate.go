package projects

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/lsegal/aviary/internal/projects/core"
)

// maxIssueDescription is the longest description the create-issue tool takes.
const maxIssueDescription = 4000

// CreateIssue files an issue on a repository through the gh-create skill
// , run by the prompt agent with this instance's identity so the
// issue carries gh-create's created-by marker. A non-empty model
// overrides the configured prompt-agent model for this call. It waits for the
// skill to finish and reports the new issue's URL.
//
// A request identical to one still filing joins it, and one identical to an
// issue filed within filedIssueMemory gets that issue back, so a caller that
// gave up waiting and asks again never files a duplicate (issue #710). The
// filing outlives a caller that stops waiting.
func (w *Engine) CreateIssue(ctx context.Context, request core.IssueCreate) (core.IssueCreateResult, error) {
	text := strings.TrimSpace(request.Text)
	switch {
	case !core.ValidRepo(request.Repository):
		return core.IssueCreateResult{}, fmt.Errorf("repository %q is not OWNER/REPO", request.Repository)
	case text == "":
		return core.IssueCreateResult{}, fmt.Errorf("the issue description is empty")
	case len(text) > maxIssueDescription:
		return core.IssueCreateResult{}, fmt.Errorf("the issue description is longer than %d characters", maxIssueDescription)
	}
	config := unblockConfig{}
	if model := strings.TrimSpace(request.Model); model != "" {
		config.Model = model
	}
	spec, err := w.oneShotSpec(config)
	if err != nil {
		return core.IssueCreateResult{}, fmt.Errorf("the coding agent cannot run: %w", err)
	}
	run, err := w.oneShotRunner()
	if err != nil {
		return core.IssueCreateResult{}, fmt.Errorf("the coding agent cannot run: %w", err)
	}
	key := issueFilingKey{repository: strings.ToLower(request.Repository), text: text, model: config.Model}
	filing, started := w.filings.start(key, time.Now())
	if started {
		prompt := createIssuePrompt(request.Repository, text, w.Identity)
		w.logf("prompt agent (%s) filing an issue on %s: %s", spec, request.Repository, truncate(text, 120))
		go func() {
			callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), issueCreateTimeout)
			defer cancel()
			output, runErr := run(callCtx, spec, prompt)
			result, err := createdIssueResult(output, runErr, request.Repository, spec)
			if err == nil {
				w.logf("prompt agent filed %s", result.URL)
			}
			w.filings.finish(key, filing, result, err, time.Now())
		}()
	}
	select {
	case <-filing.done:
		return filing.result, filing.err
	case <-ctx.Done():
		return core.IssueCreateResult{}, fmt.Errorf("gh-create is still filing this issue on %s; call project_issue_create again with the same repository and text to wait for its URL, and do not file it any other way", request.Repository)
	}
}

// createdIssueResult reads the issue gh-create reports filing on repository
// out of its output, or says why it filed none.
func createdIssueResult(output string, runErr error, repository string, spec agentSpec) (core.IssueCreateResult, error) {
	url := createdIssueURL(output, repository)
	if url == "" {
		detail := lastLines(strings.TrimSpace(output), 15)
		if runErr != nil {
			return core.IssueCreateResult{}, fmt.Errorf("gh-create failed: %v: %s", runErr, detail)
		}
		return core.IssueCreateResult{}, fmt.Errorf("gh-create reported no new issue on %s: %s", repository, detail)
	}
	return core.IssueCreateResult{URL: url, Agent: spec.String()}, nil
}

// filedIssueMemory is how long a filed issue answers an identical request.
const filedIssueMemory = time.Hour

// issueFilingKey identifies one create-issue request.
type issueFilingKey struct {
	repository, text, model string
}

// issueFiling is one gh-create run; done closes once result or err is set.
type issueFiling struct {
	done     chan struct{}
	result   core.IssueCreateResult
	err      error
	finished time.Time
}

// issueFilings holds the filings still running and the ones that filed an
// issue within filedIssueMemory.
type issueFilings struct {
	mu      sync.Mutex
	filings map[issueFilingKey]*issueFiling
}

// start returns the filing for key, starting one unless an identical request
// is still filing or filed an issue recently. started reports whether the
// caller has to run it.
func (f *issueFilings) start(key issueFilingKey, now time.Time) (filing *issueFiling, started bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, old := range f.filings {
		if !old.finished.IsZero() && now.Sub(old.finished) > filedIssueMemory {
			delete(f.filings, k)
		}
	}
	if old := f.filings[key]; old != nil {
		return old, false
	}
	if f.filings == nil {
		f.filings = map[issueFilingKey]*issueFiling{}
	}
	filing = &issueFiling{done: make(chan struct{})}
	f.filings[key] = filing
	return filing, true
}

// finish records filing's outcome. A failed filing is forgotten, so asking
// again retries it.
func (f *issueFilings) finish(key issueFilingKey, filing *issueFiling, result core.IssueCreateResult, err error, now time.Time) {
	f.mu.Lock()
	filing.result, filing.err, filing.finished = result, err, now
	if err != nil && f.filings[key] == filing {
		delete(f.filings, key)
	}
	f.mu.Unlock()
	close(filing.done)
}

// createIssuePrompt invokes gh-create for repository with the description and
// this instance's identity, the way a job's dispatch invokes gh-fix.
func createIssuePrompt(repository, text string, identity Identity) string {
	prompt := "/gh-create " + repository + " " + text
	if identity != "" {
		prompt += " identity:/aviary:" + string(identity)
	}
	return prompt
}

// issueURLPattern matches one whole GitHub issue URL, capturing its
// OWNER/REPO.
var issueURLPattern = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)/issues/[0-9]+$`)

// createdIssueURL is the last issue URL on repository in gh-create's output,
// which is the one it reports filing; issues it only mentions on the way come
// earlier. Each whitespace-separated word, stripped of the punctuation and
// Markdown around it, is matched whole, so a URL is never read out of the
// middle of a longer one.
func createdIssueURL(output, repository string) string {
	words := strings.FieldsFunc(output, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("()<>[]\"'`*,;", r)
	})
	for i := len(words) - 1; i >= 0; i-- {
		word := strings.TrimRight(words[i], ".:!?")
		if match := issueURLPattern.FindStringSubmatch(word); match != nil && strings.EqualFold(match[1], repository) {
			return match[0]
		}
	}
	return ""
}

// issueCreateTimeout bounds one gh-create call.
const issueCreateTimeout = 15 * time.Minute

// oneShotRunner is the call that runs one prompt on the coding agent outside
// any job, such as filing an issue.
func (w *Engine) oneShotRunner() (func(context.Context, agentSpec, string) (string, error), error) {
	if w.oneShotRun != nil {
		return w.oneShotRun, nil
	}
	runner, ok := w.runner().(CommandRunner)
	if !ok {
		return nil, fmt.Errorf("no coding agent is configured")
	}
	return runner.runUnblock, nil
}
