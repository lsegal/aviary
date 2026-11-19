package projects

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// maxIssueDescription is the longest description the create-issue tool takes.
const maxIssueDescription = 4000

// CreateIssue files an issue on a repository through the gh-create skill
// , run by the prompt agent with this instance's identity so the
// issue carries gh-create's created-by marker. A non-empty model
// overrides the configured prompt-agent model for this call. It waits for the
// skill to finish and reports the new issue's URL.
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
	prompt := createIssuePrompt(request.Repository, text, w.Identity)
	w.logf("prompt agent (%s) filing an issue on %s: %s", spec, request.Repository, truncate(text, 120))
	callCtx, cancel := context.WithTimeout(ctx, issueCreateTimeout)
	defer cancel()
	output, runErr := run(callCtx, spec, prompt)
	url := createdIssueURL(output, request.Repository)
	if url == "" {
		detail := lastLines(strings.TrimSpace(output), 15)
		if runErr != nil {
			return core.IssueCreateResult{}, fmt.Errorf("gh-create failed: %v: %s", runErr, detail)
		}
		return core.IssueCreateResult{}, fmt.Errorf("gh-create reported no new issue on %s: %s", request.Repository, detail)
	}
	w.logf("prompt agent filed %s", url)
	return core.IssueCreateResult{URL: url, Agent: spec.String()}, nil
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

// issueURLPattern matches a GitHub issue URL, capturing its OWNER/REPO.
var issueURLPattern = regexp.MustCompile(`https://github\.com/([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)/issues/[0-9]+`)

// createdIssueURL is the last issue URL on repository in gh-create's output,
// which is the one it reports filing; issues it only mentions on the way come
// earlier.
func createdIssueURL(output, repository string) string {
	matches := issueURLPattern.FindAllStringSubmatch(output, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		if strings.EqualFold(matches[i][1], repository) {
			return matches[i][0]
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
