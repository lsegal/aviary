package projects

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/transcript"
)

// ErrNotWatching is returned by calls that need a running engine when no
// project is watched.
var ErrNotWatching = errors.New("no projects are being watched; add one with project_add or aviary watch")

// Defaults for the job tools.
const (
	defaultJobListLimit   = 50
	defaultJobLogLines    = 200
	defaultJobEventsLimit = 200
	explainOutputLines    = 20
	statusLogLines        = 20
)

func (m *Manager) currentSnapshot() core.Snapshot {
	if snapshot := m.snapshot.Load(); snapshot != nil {
		return *snapshot
	}
	return core.Snapshot{}
}

// display is the state the read APIs show: the live snapshot, with the cache
// standing in for each project the engine has not polled yet. Only reads use
// it; every action goes through currentSnapshot, so cached state never starts,
// claims or restarts a job.
func (m *Manager) display() displayState {
	m.mu.Lock()
	settings, _ := m.settingsLocked()
	m.mu.Unlock()
	cached, cachedAt := m.cache.load()
	return mergeCached(m.snapshot.Load(), cached, cachedAt, settings.Targets)
}

// inFlightStatuses are the job statuses that still hold work: running,
// waiting out a scheduled wakeup, queued, or paused.
var inFlightStatuses = []string{"active", "waiting", "queued", "paused"}

// InFlightJobs lists the issue numbers of target's jobs that still hold work,
// which removing the project stops: the engine restarts without target, so its
// running jobs are checkpointed and none of them resume.
func (m *Manager) InFlightJobs(target string) []int {
	numbers := []int{}
	for _, job := range m.currentSnapshot().Jobs {
		if job.Target == target && slices.Contains(inFlightStatuses, job.Status) {
			numbers = append(numbers, job.Number)
		}
	}
	return numbers
}

// Status is project_status: identity, targets with issue counts, job counts,
// the jobs in each board state, delivery and the last check, quota per CLI,
// each CLI provider once with its models and quota, and recent log lines.
// Passing logOffset returns only the lines logged since that offset. Passing
// targets counts board states over only their jobs, as project_job_list does.
func (m *Manager) Status(_ context.Context, logOffset int, targets []string) map[string]any {
	display := m.display()
	snapshot := display.Snapshot
	m.mu.Lock()
	settings, settingsErr := m.settingsLocked()
	models := m.projectModelsLocked(settings)
	lastError := m.lastError
	running := m.running != nil
	m.mu.Unlock()
	logs, nextOffset := m.logsSince(logOffset, statusLogLines)
	if logOffset > 0 {
		logs, nextOffset = m.logsSince(logOffset, 0)
	}
	watched := snapshot.Targets
	if len(watched) == 0 {
		watched = settings.Targets
	}
	if watched == nil {
		watched = []string{}
	}
	// A running engine publishes the commenters it enforces, which include
	// the authenticated gh user it falls back to; otherwise report the
	// configured list.
	commenters := snapshot.AllowedCommenters
	if !running || len(commenters) == 0 {
		commenters = settings.AllowedCommenters
	}
	if commenters == nil {
		commenters = []string{}
	}
	status := map[string]any{
		"identity":     m.Identity(),
		"running":      running,
		"targets":      watched,
		"issue_counts": snapshot.IssueCounts,
		"states":       boardStateCounts(snapshot.Jobs, targets),
		"jobs": map[string]int{
			"running":    snapshot.Running,
			"queued":     snapshot.Queued,
			"paused":     snapshot.Paused,
			"completed":  snapshot.Completed,
			"merged_24h": snapshot.Merged24h,
			"failed":     snapshot.Failed,
		},
		"concurrency":        settings.Concurrency,
		"delivery":           settings.Delivery,
		"poll_interval":      core.FormatInterval(settings.Interval),
		"issue_filter":       issueFilter(settings),
		"allowed_commenters": commenters,
		"quotas":             snapshot.Quotas,
		"recent_logs":        logs,
		"log_offset":         nextOffset,
		// The boards hide safe merge while no_merge leaves pull requests
		// for a human.
		"no_merge": settings.NoMerge,
	}
	if engine := m.engine(); engine != nil {
		status["no_merge"] = engine.noMerge.Load()
	}
	if snapshot.UseWebhooks {
		status["webhook_online"] = snapshot.WebhookOnline
	}
	if settings.Delivery == DeliveryBrowser {
		// The board shows a signed-out session as a banner offering to sign
		// in to GitHub in Aviary's browser or to switch to gh polling.
		status["github"] = m.GitHubSession()
	}
	if !snapshot.LastPoll.IsZero() {
		status["last_check"] = snapshot.LastPoll
	}
	// Projects the engine has not polled since it started show the cache,
	// marked stale until their first live poll lands.
	status["stale"] = display.anyStale(targets)
	status["stale_targets"] = nonNil(display.Stale)
	status["loading_targets"] = nonNil(display.Loading)
	if !display.CachedAt.IsZero() {
		status["cached_at"] = display.CachedAt
	}
	if settingsErr != nil {
		status["error"] = settingsErr.Error()
	} else if lastError != "" {
		status["error"] = lastError
	}
	status["agents"] = allAgentSpecs(settings)
	// Each project's effective job model and where it comes from, such as
	// "claude/opus (project override)" or "claude/sonnet (from agent bot)".
	status["models"] = models
	status["model_labels"] = projectModelLabels(models)
	status["clis"] = statusCLIs(allAgentSpecs(settings), snapshot.Quotas)
	return status
}

// projectModelLabels renders each project's effective job model for
// project_status.
func projectModelLabels(models map[string]ProjectModel) map[string]string {
	labels := make(map[string]string, len(models))
	for target, model := range models {
		labels[target] = model.String()
	}
	return labels
}

// boardStateCounts counts the board entries of targets, or of every project
// without targets, in each board state, so the board's counts match its
// filters. A schedule counts once, and only while it is starting, running or
// failed: its finished runs never count as done.
func boardStateCounts(jobs []JobSnapshot, targets []string) map[string]int {
	counts := make(map[string]int, len(core.BoardStates))
	for _, state := range core.BoardStates {
		counts[state] = 0
	}
	for _, entry := range boardEntries(jobs) {
		if len(targets) > 0 && !slices.Contains(targets, entry.job.Target) {
			continue
		}
		if _, ok := counts[entry.state]; ok {
			counts[entry.state]++
		}
	}
	return counts
}

// ListJobs is project_job_list: every job, running and errored ones first,
// filtered by projects, engine status, board states, job kinds and a search
// query, and paged. A schedule is one entry, its latest run with the
// schedule's last_run, next_run and recent_runs, rather than a job per fire.
// No targets means every project. total counts the jobs that match them all;
// with a query, unfiltered_total counts those matching all but the query.
func (m *Manager) ListJobs(targets []string, status string, states, kinds []string, query string, offset, limit int) map[string]any {
	if limit <= 0 {
		limit = defaultJobListLimit
	}
	query = strings.ToLower(strings.TrimSpace(query))
	display := m.display()
	var tasks map[string]domain.ScheduledTask
	var matched []map[string]any
	unfiltered := 0
	for _, entry := range boardEntries(display.Jobs) {
		job := entry.job
		if len(targets) > 0 && !slices.Contains(targets, job.Target) {
			continue
		}
		if status != "" && job.Status != status {
			continue
		}
		if len(states) > 0 && !slices.Contains(states, entry.state) {
			continue
		}
		if len(kinds) > 0 && !slices.Contains(kinds, job.Kind) {
			continue
		}
		unfiltered++
		if query != "" && !jobMatches(job, query) {
			continue
		}
		summary := displayJobSummary(display, job)
		if entry.runs != nil {
			if tasks == nil {
				tasks = m.scheduledTasks()
			}
			summary["state"] = entry.state
			addScheduleRuns(summary, entry, tasks)
		}
		matched = append(matched, summary)
	}
	total := len(matched)
	offset = min(max(offset, 0), total)
	page := matched[offset:min(offset+limit, total)]
	if page == nil {
		page = []map[string]any{}
	}
	list := map[string]any{"jobs": page, "total": total, "offset": offset, "limit": limit, "stale": display.anyStale(targets)}
	if !display.CachedAt.IsZero() {
		list["cached_at"] = display.CachedAt
	}
	if query != "" {
		list["unfiltered_total"] = unfiltered
	}
	return list
}

// jobSearchKinds are the skill names the dashboards show for each job kind.
var jobSearchKinds = map[string]string{
	core.JobKindIssue:      "gh-fix",
	core.JobKindDiscussion: "gh-discuss",
	core.JobKindMerge:      "gh-safe-merge",
	core.JobKindDiscuss:    "gh-discuss",
}

// jobMatches reports whether a lowercased query appears in any of a job's
// fields: its numbers, title, repository, status, skill, agent and model,
// session, schedule, wait reason, or output.
func jobMatches(job core.JobSnapshot, query string) bool {
	fields := []string{
		fmt.Sprintf("%s#%d", job.Target, job.Number),
		fmt.Sprintf("#%d", job.Number),
		job.Repository,
		job.Title,
		job.Status,
		job.Kind,
		jobSearchKinds[job.Kind],
		job.Agent + "/" + job.Model,
		job.SessionID,
		job.ScheduleID,
		job.WaitReason,
		job.Log,
	}
	if job.PullRequest > 0 {
		fields = append(fields, fmt.Sprintf("#%d", job.PullRequest), job.PullRequestRepository)
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

// GetJob is project_job_get: one job's state, session, transcript events and
// scrollback. lines and events limit the scrollback lines and transcript
// events returned; 0 uses the defaults and a negative value returns all.
func (m *Manager) GetJob(target string, number, lines, events int) (map[string]any, error) {
	display := m.display()
	jobs := display.Jobs
	job, err := findJob(jobs, target, number)
	if err != nil {
		return nil, err
	}
	if lines == 0 {
		lines = defaultJobLogLines
	}
	if events == 0 {
		events = defaultJobEventsLimit
	}
	detail := displayJobSummary(display, job)
	detail["checkout_directory"] = job.CheckoutDirectory
	state, holder := checkoutState(job, jobs)
	detail["checkout_state"] = state
	if holder != nil {
		detail["checkout_reused_by"] = holder.Number
	}
	detail["session_id"] = job.SessionID
	detail["effort"] = job.Effort
	if model, ok := m.ProjectModels()[job.Target]; ok && model.runsOn(job.Agent, job.Model) {
		// A job on its project's models says where they come from; one on a
		// scheduled task's own model does not.
		detail["model_source"] = model.Source
	}
	scrollback := job.Log
	if lines > 0 {
		scrollback = lastLogLines(scrollback, lines)
	}
	detail["scrollback"] = scrollback
	log := m.transcripts.For(job.Target + "#" + fmt.Sprint(job.Number))
	detail["events"] = log.Events(max(events, 0))
	detail["raw_output"] = log.Raw(64 * 1024)
	return detail, nil
}

// ExplainJob is project_job_explain: why a job is where it is and what moves
// it on.
func (m *Manager) ExplainJob(target string, number int) (map[string]any, error) {
	display := m.display()
	job, err := findJob(display.Jobs, target, number)
	if err != nil {
		return nil, err
	}
	detail := displayJobSummary(display, job)
	detail["explanation"] = explainJob(job, display.Snapshot)
	if display.jobStale(job) {
		detail["explanation"] = "This is its state when Code Loop last ran, shown until the first poll since the restart confirms it. " + detail["explanation"].(string)
	}
	detail["last_output"] = lastLogLines(job.Log, explainOutputLines)
	return detail, nil
}

// JobAction is project_job_retry, _pause, _resume, _stop and _dismiss. It
// refuses what the job's status does not allow. Stop waits for the agent to
// exit. Dismiss takes a failed scheduled run off the board's Failed state.
func (m *Manager) JobAction(ctx context.Context, action, target string, number int) (map[string]any, error) {
	engine := m.engine()
	if engine == nil {
		return nil, ErrNotWatching
	}
	job, err := findJob(m.currentSnapshot().Jobs, target, number)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%s#%d", job.Target, job.Number)
	available := core.JobActionsFor(job)
	switch action {
	case "resume":
		if job.Status != "paused" {
			return nil, fmt.Errorf("%s is %s, not paused; there is nothing to resume", key, job.Status)
		}
		action = "retry"
	case "retry":
		if !available.Retry {
			return nil, fmt.Errorf("%s cannot be retried: %s", key, available.RetryReason)
		}
	case "pause":
		if !available.Pause {
			return nil, fmt.Errorf("%s cannot be paused while %s", key, job.Status)
		}
	case "stop":
		if !available.Stop {
			return nil, fmt.Errorf("%s cannot be stopped while %s", key, job.Status)
		}
	case "dismiss":
		if job.Kind != core.JobKindSchedule || job.Status != "failed" {
			return nil, fmt.Errorf("%s is not a failed scheduled run; only those can be dismissed", key)
		}
	default:
		return nil, fmt.Errorf("unknown job action %q", action)
	}
	if err := engine.handleJobAction(ctx, core.JobAction{Action: action, Target: job.Target, Number: job.Number}); err != nil {
		if errors.Is(err, core.ErrNotReady) {
			return nil, errors.New("the engine is still starting; try again")
		}
		return nil, err
	}
	result := map[string]any{"ok": true, "action": action, "job": nil}
	if current, err := findJob(m.currentSnapshot().Jobs, job.Target, job.Number); err == nil {
		result["job"] = jobSummary(current)
	}
	return result, nil
}

// Refresh is project_refresh: poll GitHub now. Polls cover every project, so
// target only checks that it is watched.
func (m *Manager) Refresh(target string) (map[string]any, error) {
	engine := m.engine()
	if engine == nil {
		return nil, ErrNotWatching
	}
	if target != "" && !slices.Contains(engine.Targets, target) {
		return nil, fmt.Errorf("%s is not watched", target)
	}
	engine.nudgePoll()
	return map[string]any{"requested": true, "targets": engine.Targets}, nil
}

// CloneList is project_clone_list.
func (m *Manager) CloneList(ctx context.Context) (core.ClonesSnapshot, error) {
	engine := m.engine()
	if engine == nil {
		return core.ClonesSnapshot{}, ErrNotWatching
	}
	return engine.ListClones(ctx)
}

// CloneClear is project_clone_clear.
func (m *Manager) CloneClear(ctx context.Context) (core.ClearClonesResult, error) {
	engine := m.engine()
	if engine == nil {
		return core.ClearClonesResult{}, ErrNotWatching
	}
	return engine.ClearClones(ctx)
}

// UnblockRun is project_unblock_run: a stuck-job watchdog round now, over
// every stuck job or the one named.
func (m *Manager) UnblockRun(target string, number int) (map[string]any, error) {
	engine := m.engine()
	if engine == nil {
		return nil, ErrNotWatching
	}
	if target != "" && number <= 0 {
		return nil, errors.New("a target needs a job number")
	}
	key := ""
	if number > 0 {
		job, err := findJob(m.currentSnapshot().Jobs, target, number)
		if err != nil {
			return nil, err
		}
		key = fmt.Sprintf("%s#%d", job.Target, job.Number)
	}
	if err := engine.TriggerUnblock(key); err != nil {
		return nil, err
	}
	message := "requested an unblock round over every stuck job; the watchdog logs what it finds and does"
	if key != "" {
		message = fmt.Sprintf("requested an unblock round for %s; the watchdog logs what it finds and does", key)
	}
	return map[string]any{"requested": true, "job": key, "message": message}, nil
}

// IssueComment is project_issue_comment: post an update that mentions the
// issue's author and this instance, so the owning job hears it.
func (m *Manager) IssueComment(ctx context.Context, repository string, number int, text string, attachments []string) (map[string]any, error) {
	engine := m.engine()
	if engine == nil {
		return nil, ErrNotWatching
	}
	if number <= 0 {
		return nil, errors.New("an issue or pull request number is required")
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("the update text is empty")
	}
	if repository == "" {
		resolved, err := updateRepository(m.currentSnapshot(), number)
		if err != nil {
			return nil, err
		}
		repository = resolved
	}
	result, err := engine.PostIssueUpdate(ctx, core.IssueUpdate{Repository: repository, Number: number, Text: text, Attachments: attachments})
	if err != nil {
		return nil, err
	}
	return map[string]any{"url": result.URL, "author": result.Author, "repository": repository, "number": number, "attachments": attachedList(result.Attachments)}, nil
}

// IssueCreate is project_issue_create: file an issue through gh-create with
// this instance's identity, uploading and embedding attachments.
func (m *Manager) IssueCreate(ctx context.Context, repository, text, model string, attachments []string) (map[string]any, error) {
	engine := m.engine()
	if engine == nil {
		return nil, ErrNotWatching
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("the issue description is empty")
	}
	if repository == "" {
		resolved, err := onlyRepository(m.currentSnapshot())
		if err != nil {
			return nil, errors.New("name the repository to file the issue on")
		}
		repository = resolved
	}
	result, err := engine.CreateIssue(ctx, core.IssueCreate{Repository: repository, Text: text, Model: model, Attachments: attachments})
	if err != nil {
		return nil, err
	}
	return map[string]any{"url": result.URL, "agent": result.Agent, "repository": repository, "attachments": attachedList(result.Attachments)}, nil
}

// attachedList reports the files a filing attached, as an empty list rather
// than null when there were none, so the result always says.
func attachedList(files []string) []string {
	if files == nil {
		return []string{}
	}
	return files
}

// explainJob says in words why a job is where it is and what moves it on.
func explainJob(job core.JobSnapshot, snapshot core.Snapshot) string {
	switch job.Status {
	case "active":
		text := "Its agent is running"
		if !job.Started.IsZero() {
			text += fmt.Sprintf(", started %s ago", core.FormatInterval(time.Since(job.Started).Truncate(time.Second)))
		}
		return text + ". If its last output has not changed for a while it may be stuck; the stuck-job watchdog covers a silent job, and project_unblock_run runs a round for it now."
	case "waiting":
		text := "Its agent scheduled a wakeup and is waiting for it"
		if !job.WakeAt.IsZero() {
			text += fmt.Sprintf(", due %s", job.WakeAt.Format(time.RFC3339))
		}
		if job.WaitReason != "" {
			text += ": " + job.WaitReason
		}
		return text + "."
	case "paused":
		if job.WaitReason == core.StalledPauseReason {
			return "Its agent kept ending turns with the issue still open and no new commits, pull requests, or comments, so Code Loop stopped resuming it. Nothing wakes it on its own, and the stuck-job watchdog leaves it alone: project_job_resume requeues it once the blocker is fixed, and project_job_stop clears it."
		}
		if job.WaitReason == core.ManualPauseReason {
			return "The owner paused it. Nothing wakes it on its own, and the stuck-job watchdog leaves it alone: project_job_resume requeues it, and project_job_stop clears it."
		}
		text := "It is paused, holding no agent slot"
		if job.WaitReason != "" {
			text += ": " + job.WaitReason
		}
		return text + ". Code Loop wakes it on its own once that changes on GitHub. The stuck-job watchdog covers it, project_unblock_run runs a round for it now, and project_job_resume clears the pause and requeues it at once."
	case "queued":
		return fmt.Sprintf("It is queued for an agent slot; %d of %d slots are running.", snapshot.Running, snapshot.Concurrency)
	case "stopping":
		return "It was stopped and Code Loop is waiting for its agent to exit."
	}
	if job.WaitReason != "" {
		return fmt.Sprintf("Its status is %s: %s.", job.Status, job.WaitReason)
	}
	return fmt.Sprintf("Its status is %s.", job.Status)
}

// updateRepository picks the repository an update without one goes to: that
// of the one job with the number, or else the only watched repository.
func updateRepository(snapshot core.Snapshot, number int) (string, error) {
	var found []string
	for _, job := range snapshot.Jobs {
		if job.Number != number {
			continue
		}
		repository := job.Repository
		if repository == "" && core.ValidRepo(job.Target) {
			repository = job.Target
		}
		if repository != "" && !slices.Contains(found, repository) {
			found = append(found, repository)
		}
	}
	if len(found) == 1 {
		return found[0], nil
	}
	if len(found) > 1 {
		return "", fmt.Errorf("jobs in %s all have #%d; name the repository", strings.Join(found, " and "), number)
	}
	if repository, err := onlyRepository(snapshot); err == nil {
		return repository, nil
	}
	return "", fmt.Errorf("name the repository of #%d", number)
}

// onlyRepository is the watched repository when exactly one is watched.
func onlyRepository(snapshot core.Snapshot) (string, error) {
	var repositories []string
	for _, target := range snapshot.Targets {
		if core.ValidRepo(target) {
			repositories = append(repositories, target)
		}
	}
	if len(repositories) != 1 {
		return "", errors.New("projects do not watch exactly one repository")
	}
	return repositories[0], nil
}

// findJob picks the job number names, in target when given.
func findJob(jobs []core.JobSnapshot, target string, number int) (core.JobSnapshot, error) {
	if number <= 0 {
		return core.JobSnapshot{}, errors.New("a job number is required")
	}
	var matches []core.JobSnapshot
	for _, job := range jobs {
		if job.Number == number && (target == "" || job.Target == target) {
			matches = append(matches, job)
		}
	}
	switch {
	case len(matches) == 1:
		return matches[0], nil
	case len(matches) > 1:
		return core.JobSnapshot{}, fmt.Errorf("more than one project has job #%d; name its target", number)
	case target != "":
		return core.JobSnapshot{}, fmt.Errorf("no job %s#%d", target, number)
	}
	return core.JobSnapshot{}, fmt.Errorf("no job #%d", number)
}

// jobSummary is a job as project_job_list reports it: its state and the
// actions it accepts, without its scrollback.
func jobSummary(job core.JobSnapshot) map[string]any {
	actions := core.JobActionsFor(job)
	summary := map[string]any{
		"key":    fmt.Sprintf("%s#%d", job.Target, job.Number),
		"target": job.Target,
		"number": job.Number,
		"title":  job.Title,
		"status": job.Status,
		"state":  job.BoardState(),
		"kind":   job.Kind,
		"agent":  job.Agent,
		"model":  job.Model,
		"actions": map[string]any{
			"retry":  actions.Retry,
			"pause":  actions.Pause,
			"resume": job.Status == "paused",
			"stop":   actions.Stop,
		},
	}
	if !actions.Retry {
		summary["retry_unavailable"] = actions.RetryReason
	}
	if job.Repository != "" {
		summary["repository"] = job.Repository
	}
	if job.ScheduleID != "" {
		summary["schedule_id"] = job.ScheduleID
		if job.Summary != "" {
			summary["summary"] = job.Summary
		}
		if !job.Finished.IsZero() {
			summary["finished"] = job.Finished
		}
		if runFinished(job) {
			summary["result"] = runResult(job)
		}
	}
	if core.Queued(job.Kind) {
		// A merge or discuss job's last line says why it ended, such as
		// the conflict or failing check that stopped a merge.
		if job.Summary != "" {
			summary["summary"] = job.Summary
		}
		if !job.Finished.IsZero() {
			summary["finished"] = job.Finished
		}
		if job.QueuePosition > 0 {
			summary["queue_position"] = job.QueuePosition
		}
	}
	if url := job.IssueURL(); url != "" {
		summary["issue_url"] = url
	}
	if job.PullRequest > 0 {
		summary["pull_request"] = job.PullRequest
		summary["pull_request_url"] = job.PullRequestURL()
		if job.PullRequestState != "" {
			summary["pull_request_state"] = job.PullRequestState
		}
	}
	if job.HasProgress() {
		summary["progress"] = map[string]int{"done": job.TasksDone, "total": job.TasksTotal, "percent": job.Progress}
	}
	if !job.Started.IsZero() {
		summary["started"] = job.Started
	}
	if job.WaitReason != "" {
		summary["wait_reason"] = job.WaitReason
	}
	if !job.WakeAt.IsZero() {
		summary["wake_at"] = job.WakeAt
	}
	return summary
}

// displayJobSummary is jobSummary for a job of display, which marks a job
// shown from the cache as stale and offers no actions on it until a live poll
// confirms its state.
func displayJobSummary(display displayState, job core.JobSnapshot) map[string]any {
	summary := jobSummary(job)
	if display.jobStale(job) {
		summary["stale"] = true
		summary["actions"] = map[string]any{"retry": false, "pause": false, "resume": false, "stop": false}
		summary["retry_unavailable"] = "Waiting for the first poll since the restart"
	}
	return summary
}

// nonNil returns list, or an empty list for nil, so JSON shows [] rather than
// null.
func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// lastLogLines returns the last n lines of a scrollback.
func lastLogLines(log string, n int) string {
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(log, "\r\n", "\n"), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// Transcript returns a job's transcript events, for callers that render them.
func (m *Manager) Transcript(target string, number int) []transcript.Event {
	return m.transcripts.For(fmt.Sprintf("%s#%d", target, number)).Events(0)
}

// issueFilter is the issue search that selects work, empty when every open
// issue is picked up. Board targets also require the ready board status.
func issueFilter(s engineSettings) string {
	if s.AllIssues {
		return ""
	}
	return s.Filter
}
