package projects

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
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
// filing outlives a caller that stops waiting. A different request from the
// same session on the same repository, made while one is filing or within
// sessionFilingMemory of one filing, is refused with that filing's issue
// unless it is marked distinct, since it is most likely the same request
// reworded (issue #1174).
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
	attachments, err := checkAttachments(request.Attachments)
	if err != nil {
		return core.IssueCreateResult{}, err
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
	key := issueFilingKey{repository: strings.ToLower(request.Repository), text: text, model: config.Model, attachments: strings.Join(attachments, "\n")}
	asker := issueAsker{session: request.Session, distinct: request.Distinct, text: text}
	filing, started, err := w.filings.start(key, asker, time.Now())
	if err != nil {
		return core.IssueCreateResult{}, err
	}
	if started {
		prompt := createIssuePrompt(request.Repository, text, attachments, w.Identity)
		w.logf("prompt agent (%s) filing an issue on %s with %d attachments: %s", spec, request.Repository, len(attachments), truncate(text, 120))
		go func() {
			callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), issueCreateTimeout)
			defer cancel()
			output, runErr := run(callCtx, spec, prompt)
			result, err := createdIssueResult(output, runErr, request.Repository, spec)
			if err == nil {
				result.Attachments = attachments
				w.logf("prompt agent filed %s", result.URL)
			}
			w.filings.finish(key, filing, result, err, time.Now())
		}()
	}
	select {
	case <-filing.done:
		return filing.result, filing.err
	case <-ctx.Done():
		return core.IssueCreateResult{}, fmt.Errorf("gh-create is still filing this issue on %s; call project_issue_create again with the same repository, text and attachments to wait for its URL, and do not file it any other way", request.Repository)
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

// sessionFilingMemory is how long an issue a session filed refuses a
// different request from that session on the same repository.
const sessionFilingMemory = 10 * time.Minute

// sessionFilingKey identifies the filings one session makes on a repository.
type sessionFilingKey struct {
	session, repository string
}

// issueFilingKey identifies one create-issue request. The same text with
// different media is a different issue.
type issueFilingKey struct {
	repository, text, model, attachments string
}

// issueFiling is one gh-create run; done closes once result or err is set.
type issueFiling struct {
	done     chan struct{}
	text     string
	started  time.Time
	result   core.IssueCreateResult
	err      error
	finished time.Time
}

// issueFilings holds the filings still running and the ones that filed an
// issue within filedIssueMemory.
type issueFilings struct {
	mu      sync.Mutex
	filings map[issueFilingKey]*issueFiling
	// bySession is the latest filing each session started on a repository.
	bySession map[sessionFilingKey]*issueFiling
}

// issueAsker is who asks for a filing: the session, when there is one,
// whether it marked the request distinct, and the request's text.
type issueAsker struct {
	session  string
	distinct bool
	text     string
}

// sessionDuplicate refuses a request from key's session, naming the filing it
// most likely repeats, when that session is still filing a different issue on
// the repository or filed one within sessionFilingMemory. Called with f.mu
// held.
func (f *issueFilings) sessionDuplicate(key sessionFilingKey, now time.Time) error {
	old := f.bySession[key]
	if old == nil {
		return nil
	}
	summary := truncate(strings.Join(strings.Fields(old.text), " "), 120)
	const distinct = "Do not file it again or any other way. Only if the user asked for a separate issue, call project_issue_create again with distinct: true"
	switch {
	case old.finished.IsZero():
		return fmt.Errorf("an issue from this session is already being filed on %s (started %s ago): %q. Call project_issue_create again with exactly that text to wait for its URL. %s", key.repository, now.Sub(old.started).Round(time.Second), summary, distinct)
	case old.err == nil && now.Sub(old.finished) <= sessionFilingMemory:
		return fmt.Errorf("issue %s was filed from this session %s ago: %q. %s", old.result.URL, now.Sub(old.finished).Round(time.Second), summary, distinct)
	}
	return nil
}

// start returns the filing for key, starting one unless an identical request
// is still filing or filed an issue recently. started reports whether the
// caller has to run it. A different request from a session that is filing or
// just filed an issue on the repository is refused, unless marked distinct.
func (f *issueFilings) start(key issueFilingKey, asker issueAsker, now time.Time) (filing *issueFiling, started bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, old := range f.filings {
		if !old.finished.IsZero() && now.Sub(old.finished) > filedIssueMemory {
			delete(f.filings, k)
		}
	}
	for k, old := range f.bySession {
		if !old.finished.IsZero() && (old.err != nil || now.Sub(old.finished) > sessionFilingMemory) {
			delete(f.bySession, k)
		}
	}
	if old := f.filings[key]; old != nil {
		return old, false, nil
	}
	sessionKey := sessionFilingKey{session: asker.session, repository: key.repository}
	if asker.session != "" && !asker.distinct {
		if err := f.sessionDuplicate(sessionKey, now); err != nil {
			return nil, false, err
		}
	}
	if f.filings == nil {
		f.filings = map[issueFilingKey]*issueFiling{}
	}
	filing = &issueFiling{done: make(chan struct{}), text: asker.text, started: now}
	f.filings[key] = filing
	if asker.session != "" {
		if f.bySession == nil {
			f.bySession = map[sessionFilingKey]*issueFiling{}
		}
		f.bySession[sessionKey] = filing
	}
	return filing, true, nil
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

// createIssuePrompt invokes gh-create for repository with the description,
// an attach: argument per file to upload and embed, and this instance's
// identity, the way a job's dispatch invokes gh-fix.
func createIssuePrompt(repository, text string, attachments []string, identity Identity) string {
	prompt := "/gh-create " + repository + " " + text
	for _, path := range attachments {
		if strings.ContainsAny(path, " \t\n\"") {
			path = strconv.Quote(path)
		}
		prompt += " attach:" + path
	}
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
