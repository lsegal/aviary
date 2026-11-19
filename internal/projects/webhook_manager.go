package projects

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/lsegal/aviary/internal/projects/ngrok"
	"github.com/lsegal/aviary/internal/projects/process"
)

var errProjectWebhookUnavailable = errors.New("project push webhook unavailable")

// errWebhookPartiallyConfigured reports that at least one, but not every,
// webhook a target needs could be configured. Push mode still works for the
// repositories that succeeded, so this is informational rather than fatal.
var errWebhookPartiallyConfigured = errors.New("some push webhooks could not be configured")

type managedHook struct {
	ID     int      `json:"id"`
	Events []string `json:"events"`
	Config struct {
		URL string `json:"url"`
	} `json:"config"`
}

type webhookSpec struct {
	apiPath string
	name    string
	events  []string
}

// ConfigureWebhook ensures every webhook the target needs exists, and reports
// the names of the ones it had to create. Existing webhooks pointing at the
// endpoint are left untouched, so calling this repeatedly against the same
// target is safe.
func (g GHCLI) ConfigureWebhook(ctx context.Context, value, endpoint, secret string) ([]string, error) {
	target, err := parseTarget(value)
	if err != nil {
		return nil, err
	}
	specs, err := g.webhookSpecs(ctx, target)
	if err != nil {
		return nil, err
	}
	var created []string
	failures := make([]error, 0, len(specs))
	for _, spec := range specs {
		added, err := g.configureWebhookSpec(ctx, spec, endpoint, secret)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if added {
			created = append(created, spec.name)
		}
	}
	if len(failures) == 0 {
		return created, nil
	}
	if len(failures) == len(specs) {
		return created, errors.Join(failures...)
	}
	return created, fmt.Errorf("%w (%d of %d): %w", errWebhookPartiallyConfigured, len(failures), len(specs), errors.Join(failures...))
}

// configureWebhookSpec creates the spec's webhook when the endpoint is not
// already registered, reporting whether it created one.
func (g GHCLI) configureWebhookSpec(ctx context.Context, spec webhookSpec, endpoint, secret string) (bool, error) {
	output, err := g.apiList(ctx, spec.apiPath)
	if err != nil {
		return false, webhookAccessError("list", spec, err)
	}
	var hooks []managedHook
	if err := json.Unmarshal(output, &hooks); err != nil {
		return false, fmt.Errorf("decode webhooks for %s: %w", spec.name, err)
	}
	found := false
	for _, hook := range hooks {
		if hook.Config.URL == endpoint {
			found = true
			// A repository target and a Discussions-board target for the same
			// repository share one webhook, so an existing hook may be missing
			// the events this spec needs. Widen it instead of leaving the
			// second target without deliveries.
			if missing := missingWebhookEvents(hook.Events, spec.events); len(missing) > 0 {
				if err := g.widenWebhookEvents(ctx, spec, hook, missing); err != nil {
					return false, err
				}
			}
			continue
		}
		if ngrok.IsURL(hook.Config.URL) {
			if _, err := g.api(ctx, fmt.Sprintf("%s/%d", spec.apiPath, hook.ID), "DELETE"); err != nil {
				return false, webhookAccessError(fmt.Sprintf("remove old ngrok webhook %d from", hook.ID), spec, err)
			}
		}
	}
	if found {
		return false, nil
	}
	config := map[string]interface{}{
		"url":          endpoint,
		"content_type": "json",
		"insecure_ssl": "0",
	}
	if secret != "" {
		config["secret"] = secret
	}
	body, err := json.Marshal(map[string]interface{}{
		"name":   "web",
		"active": true,
		"events": spec.events,
		"config": config,
	})
	if err != nil {
		return false, err
	}
	if _, err := g.api(ctx, spec.apiPath, "POST", string(body)); err != nil {
		return false, webhookAccessError("create", spec, err)
	}
	return true, nil
}

// missingWebhookEvents returns the events a spec needs that an existing hook
// is not subscribed to. A hook subscribed to the wildcard "*" already
// receives everything.
func missingWebhookEvents(existing, wanted []string) []string {
	subscribed := make(map[string]bool, len(existing))
	for _, event := range existing {
		if event == "*" {
			return nil
		}
		subscribed[event] = true
	}
	var missing []string
	for _, event := range wanted {
		if !subscribed[event] {
			missing = append(missing, event)
		}
	}
	return missing
}

// widenWebhookEvents adds the missing events to an existing webhook, leaving
// the events it already carries in place so the other targets sharing it keep
// their deliveries.
func (g GHCLI) widenWebhookEvents(ctx context.Context, spec webhookSpec, hook managedHook, missing []string) error {
	events := append(append([]string{}, hook.Events...), missing...)
	sort.Strings(events)
	body, err := json.Marshal(map[string]interface{}{"events": events})
	if err != nil {
		return err
	}
	if _, err := g.api(ctx, fmt.Sprintf("%s/%d", spec.apiPath, hook.ID), "PATCH", string(body)); err != nil {
		return webhookAccessError(fmt.Sprintf("add %s events to", strings.Join(missing, ", ")), spec, err)
	}
	return nil
}

// webhookConfigurer configures the push webhooks a target needs, reporting the
// names of the webhooks it newly created.
type webhookConfigurer interface {
	ConfigureWebhook(ctx context.Context, target, endpoint, secret string) ([]string, error)
}

// webhookReconciler re-runs webhook configuration for every target while the
// daemon runs. The repositories backing a project board are enumerated when
// the board's webhooks are configured, so a repository added to the board
// after startup would otherwise have no webhook until aviary restarts (issue
// #238). ConfigureWebhook only creates a webhook whose endpoint is not already
// registered, so reconciling repeatedly neither duplicates webhooks nor churns
// existing ones. Failures are reported and non-fatal: the repositories that
// could be configured keep receiving pushes, and the rest stay on the
// periodic poller.
type webhookReconciler struct {
	gh       webhookConfigurer
	targets  []string
	endpoint string
	secret   string
	logf     func(string, ...interface{})
	// reported remembers the last failure logged per target so a persistent
	// error is reported once instead of on every cycle. The reconciler runs
	// only from the daemon's poll loop, so this needs no locking.
	reported map[string]string
}

func newWebhookReconciler(gh webhookConfigurer, targets []string, endpoint, secret string, logf func(string, ...interface{})) *webhookReconciler {
	return &webhookReconciler{gh: gh, targets: targets, endpoint: endpoint, secret: secret, logf: logf, reported: map[string]string{}}
}

func (r *webhookReconciler) reconcile(ctx context.Context) {
	for _, target := range r.targets {
		created, err := r.gh.ConfigureWebhook(ctx, target, r.endpoint, r.secret)
		for _, name := range created {
			r.logf("configured GitHub webhook for %s", name)
		}
		if ctx.Err() != nil {
			return
		}
		message := ""
		if err != nil {
			message = fmt.Sprintf("webhook reconciliation for %s: %v", target, err)
		}
		if r.reported[target] == message {
			continue
		}
		r.reported[target] = message
		if message != "" {
			r.logf("%s", message)
		}
	}
}

// webhookSpecs lists every webhook a target needs. A repository target needs
// exactly one. An organization-owned project needs its own projects_v2_item
// hook for board changes, plus a narrow issue hook on each repository backing
// the board: projects_v2_item says nothing about an issue being edited,
// commented on, or closed, so without those the runs working the board's
// issues learn about a change only on the watcher's next poll instead of
// being nudged immediately. GitHub publishes no
// projects_v2 webhook for user-owned projects, so those fall back to a
// repository webhook on each repository currently backing the board's items;
// that pushes new issues immediately instead of leaving the whole target to
// the periodic poller. Board-only changes, such as dragging an
// existing issue onto the board or moving a card between columns, still
// surface through periodic synchronization.
func (g GHCLI) webhookSpecs(ctx context.Context, target target) ([]webhookSpec, error) {
	if target.IsDiscussion {
		return []webhookSpec{discussionWebhookSpec(target.Repo)}, nil
	}
	if !target.IsProject {
		return []webhookSpec{repositoryWebhookSpec(target.Repo)}, nil
	}
	ownerType, err := g.projectOwnerType(ctx, target)
	if err != nil {
		return nil, err
	}
	target.ProjectOwnerType = ownerType
	if ownerType == "orgs" {
		specs := []webhookSpec{{
			apiPath: "orgs/" + target.Owner + "/hooks",
			name:    "organization project " + target.Owner,
			events:  []string{"projects_v2_item"},
		}}
		repos, err := g.projectRepositories(ctx, target)
		if err != nil {
			// The board's repositories are re-enumerated on every
			// reconciliation cycle, so a listing that fails now costs the
			// issue hooks a cycle rather than the projects_v2_item
			// subscription the whole target depends on.
			return specs, nil
		}
		for _, repo := range repos {
			specs = append(specs, projectIssueWebhookSpec(repo))
		}
		return specs, nil
	}
	repos, err := g.projectRepositories(ctx, target)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return nil, fmt.Errorf("%w: GitHub only provides push events for organization-owned Projects and the project owned by %s has no issues to watch; using periodic synchronization", errProjectWebhookUnavailable, target.Owner)
	}
	specs := make([]webhookSpec, 0, len(repos))
	for _, repo := range repos {
		specs = append(specs, repositoryWebhookSpec(repo))
	}
	return specs, nil
}

func repositoryWebhookSpec(repo string) webhookSpec {
	return webhookSpec{
		apiPath: "repos/" + repo + "/hooks",
		name:    repo,
		events:  []string{"issues", "pull_request", "push", "ping", "issue_comment", "pull_request_review", "pull_request_review_comment"},
	}
}

// projectIssueWebhookSpec is the repository webhook a project-board target
// needs beside its own board hook. It subscribes only to the issue traffic
// the board's in-flight runs have to hear about, rather than the full
// repository set: a board target dispatches from the board, so pushes and
// pull-request activity are not its business. Pull request reviews are the
// exception, because a direct mention can be posted in one. A
// repository that is also
// watched as a repository target shares the one hook and keeps its wider
// event set, because this narrower spec asks for nothing it is missing.
func projectIssueWebhookSpec(repo string) webhookSpec {
	return webhookSpec{
		apiPath: "repos/" + repo + "/hooks",
		name:    repo + " project issues",
		events:  []string{"issues", "issue_comment", "pull_request_review", "pull_request_review_comment", "ping"},
	}
}

// discussionWebhookSpec is the webhook a Discussions-board target needs. New
// threads arrive as `discussion` deliveries; the issue events a repository
// target subscribes to cannot make a discussion dispatchable.
func discussionWebhookSpec(repo string) webhookSpec {
	return webhookSpec{
		apiPath: "repos/" + repo + "/hooks",
		name:    repo + " discussions",
		events:  []string{"discussion", "ping"},
	}
}

func (g GHCLI) projectOwnerType(ctx context.Context, target target) (string, error) {
	if target.ProjectOwnerType != "" {
		return target.ProjectOwnerType, nil
	}
	output, err := g.api(ctx, "users/"+target.Owner, "")
	if err != nil {
		return "", fmt.Errorf("identify project owner %s: %w", target.Owner, err)
	}
	var owner struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(output, &owner); err != nil {
		return "", fmt.Errorf("decode project owner %s: %w", target.Owner, err)
	}
	if owner.Type == "Organization" {
		return "orgs", nil
	}
	return "users", nil
}

// projectRepositories lists the distinct repositories backing a project's
// items. Filters are deliberately ignored: a webhook has to be in place before
// an issue becomes ready, so every repository on the board is watched.
func (g GHCLI) projectRepositories(ctx context.Context, target target) ([]string, error) {
	items, err := g.listProjectItems(ctx, target, "", true)
	if err != nil {
		return nil, fmt.Errorf("list repositories for project owned by %s: %w", target.Owner, err)
	}
	seen := make(map[string]bool, len(items))
	repos := make([]string, 0, len(items))
	for _, item := range items {
		if item.Content == nil {
			continue
		}
		repo := strings.TrimSpace(item.Content.Repository)
		if repo == "" || seen[repo] {
			continue
		}
		seen[repo] = true
		repos = append(repos, repo)
	}
	sort.Strings(repos)
	return repos, nil
}

func webhookAccessError(action string, spec webhookSpec, err error) error {
	wrapped := fmt.Errorf("%s webhooks for %s: %w", action, spec.name, err)
	if strings.HasPrefix(spec.apiPath, "orgs/") {
		return fmt.Errorf("%w; organization project push requires organization-owner access and the admin:org_hook scope (run `gh auth refresh -s admin:org_hook`)", wrapped)
	}
	return wrapped
}

// api calls a single-object REST endpoint. A GET reads one page only, so it
// refuses a list response with errListReadOnePage; list endpoints go through
// apiList.
func (g GHCLI) api(ctx context.Context, path, method string, body ...string) ([]byte, error) {
	args := []string{"api", path}
	if method != "" {
		args = append(args, "--method", method)
	}
	if len(body) > 0 {
		args = append(args, "--input", "-")
	}
	output, err := g.runAPI(ctx, args, body...)
	if err == nil && method == "" {
		err = rejectListBody(path, output)
	}
	return output, err
}

// apiList reads every page of a list endpoint, so a repository or
// organization with more than one page of webhooks still has aviary's own
// webhook found instead of a duplicate created.
func (g GHCLI) apiList(ctx context.Context, path string) ([]byte, error) {
	return g.runAPI(ctx, []string{"api", path, "--paginate"})
}

func (g GHCLI) runAPI(ctx context.Context, args []string, body ...string) ([]byte, error) {
	// Bounded like every other `gh` call: webhook reconciliation runs on the
	// poll loop, so a call stuck on a connection that died while the machine
	// slept would otherwise stop the watch for good.
	runCtx, cancel := context.WithTimeout(ctx, ghCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, g.Binary, args...)
	// Cancelling kills `gh`, but a child it left holding the output pipe would
	// keep the read waiting anyway, so the pipe is closed behind it.
	cmd.WaitDelay = ghCommandWaitDelay
	if len(body) > 0 {
		cmd.Stdin = strings.NewReader(body[0])
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := process.Output(cmd)
	if err = g.timedOut(ctx, runCtx, args, err); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return nil, fmt.Errorf("%w: %s", err, detail)
		}
		return nil, err
	}
	return output, nil
}
