package projects

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// promptCallTimeout bounds one prompt agent call, so a call that hangs still
// answers its request in the prompt area (issue #801).
const promptCallTimeout = 15 * time.Minute

// promptActionTimeout bounds one job action the prompt agent asked for. A stop
// or pause answers once the run has exited, which takes a moment.
const promptActionTimeout = 5 * time.Minute

// promptSettingsTimeout bounds reading the live settings for the prompt, so a
// run that is still starting does not hold the request up.
const promptSettingsTimeout = 5 * time.Second

// promptLogLines is how many of the run's latest log lines glorp keeps for
// the prompt agent, and promptOutputLines how much of each job's scrollback
// it is shown.
const (
	promptLogLines    = 40
	promptOutputLines = 15
)

// promptHistory is how many entries the prompt area keeps, and
// promptContextEntries how many earlier answered ones a request carries, so a
// follow-up such as "retry it" can refer back.
const (
	promptHistory        = 20
	promptContextEntries = 3
)

// maxPromptRequest is the longest request the prompt area accepts.
const maxPromptRequest = 4000

// maxPromptResponse is how much of the agent's output an answer keeps: its
// end, where the agent's reply is.
const maxPromptResponse = 8000

// promptAction is one job action or unblock round the prompt agent's answer
// asks glorp to perform.
type promptAction struct {
	Kind string
	Key  string
}

func (w *Glorp) promptSettings() unblockConfig {
	if config := w.promptOverride.Load(); config != nil {
		return *config
	}
	return unblockConfig{}
}

// SetPromptAgent sets the prompt agent's agent and model override (issue
// #801). The next request runs with it.
func (w *Glorp) SetPromptAgent(config unblockConfig) {
	config.Agent, config.Model = strings.TrimSpace(config.Agent), strings.TrimSpace(config.Model)
	w.promptOverride.Store(&config)
}

// promptSpec resolves the agent and model a prompt request runs with: the
// override when one is set, otherwise the coding agent and its model. A model
// given for one call, as the MCP ask tool takes (issue #807), replaces the
// configured model for that call only.
func (w *Glorp) promptSpec(model string) (agentSpec, error) {
	config := w.promptSettings()
	if model = strings.TrimSpace(model); model != "" {
		config.Model = model
	}
	return w.oneShotSpec(config)
}

// Prompts returns the prompt area's conversation, oldest first.
func (w *Glorp) Prompts() []core.PromptEntry {
	w.promptMu.Lock()
	defer w.promptMu.Unlock()
	return clonePrompts(w.prompts)
}

func clonePrompts(entries []core.PromptEntry) []core.PromptEntry {
	list := make([]core.PromptEntry, len(entries))
	for i, entry := range entries {
		entry.Actions = append([]string(nil), entry.Actions...)
		list[i] = entry
	}
	return list
}

// reportPrompts shows the conversation in every dashboard with a prompt area.
func (w *Glorp) reportPrompts() {
	if reporter, ok := w.UI.(core.PromptReporter); ok {
		reporter.Prompts(w.Prompts())
	}
}

// SubmitPrompt starts answering a request made in a dashboard's prompt area
// (issue #801) and returns its pending entry at once. The agent call runs on
// its own goroutine, outside any job and off the Run loop, so neither the
// dashboards nor the loop ever wait on it; its answer lands in the prompt area
// when it is ready.
func (w *Glorp) SubmitPrompt(ctx context.Context, request string) (core.PromptEntry, error) {
	entry, earlier, err := w.startPrompt(request)
	if err != nil {
		return core.PromptEntry{}, err
	}
	go w.answerPrompt(ctx, entry, earlier, "", nil)
	return entry, nil
}

// AskPrompt answers a request from the MCP ask tool (issue #807) with the
// same prompt agent, conversation, and actions as the prompt area, and waits
// for the answer. The request and its answer show in the prompt area too. A
// non-empty model overrides the configured prompt-agent model for this call.
func (w *Glorp) AskPrompt(ctx context.Context, request, model string) (core.PromptEntry, error) {
	if _, err := w.promptSpec(model); err != nil {
		return core.PromptEntry{}, fmt.Errorf("the prompt agent cannot run: %w", err)
	}
	entry, earlier, err := w.startPrompt(request)
	if err != nil {
		return core.PromptEntry{}, err
	}
	return w.answerPrompt(ctx, entry, earlier, model, nil), nil
}

// startPrompt adds a pending entry for request to the prompt area and returns
// it with the entries before it.
func (w *Glorp) startPrompt(request string) (core.PromptEntry, []core.PromptEntry, error) {
	request = strings.TrimSpace(request)
	if request == "" {
		return core.PromptEntry{}, nil, fmt.Errorf("the request is empty")
	}
	if len(request) > maxPromptRequest {
		return core.PromptEntry{}, nil, fmt.Errorf("the request is longer than %d characters", maxPromptRequest)
	}
	w.promptMu.Lock()
	w.promptNext++
	entry := core.PromptEntry{ID: w.promptNext, Request: request, Status: "pending", Started: time.Now()}
	earlier := clonePrompts(w.prompts)
	w.prompts = append(w.prompts, entry)
	if len(w.prompts) > promptHistory {
		w.prompts = w.prompts[len(w.prompts)-promptHistory:]
	}
	w.promptMu.Unlock()
	w.reportPrompts()
	return entry, earlier, nil
}

// finishPrompt records an entry's answer, shows it, and returns it.
func (w *Glorp) finishPrompt(entry core.PromptEntry) core.PromptEntry {
	entry.Finished = time.Now()
	w.promptMu.Lock()
	for i := range w.prompts {
		if w.prompts[i].ID == entry.ID {
			w.prompts[i] = entry
		}
	}
	w.promptMu.Unlock()
	w.reportPrompts()
	return entry
}

// promptRunner is how a prompt agent call runs: the test stub when one is
// set, otherwise the coding agent's runner.
func (w *Glorp) promptRunner() (func(context.Context, agentSpec, string) (string, error), error) {
	if w.promptRun != nil {
		return w.promptRun, nil
	}
	runner, ok := w.runner().(CommandRunner)
	if !ok {
		return nil, fmt.Errorf("no coding agent is configured")
	}
	return runner.runUnblock, nil
}

// answerPrompt runs the prompt agent for one request, then performs the job
// actions and unblock rounds its answer asks for through glorp's own paths,
// and returns the answered entry. route is set when the request is a comment
// glorp routed to the agent (issue #842) rather than one the owner typed.
func (w *Glorp) answerPrompt(ctx context.Context, entry core.PromptEntry, earlier []core.PromptEntry, model string, route *commentRoute) core.PromptEntry {
	spec, err := w.promptSpec(model)
	if err != nil {
		entry.Status, entry.Response = "failed", fmt.Sprintf("the prompt agent cannot run: %v", err)
		return w.finishPrompt(entry)
	}
	entry.Agent = spec.String()
	run, err := w.promptRunner()
	if err != nil {
		entry.Status, entry.Response = "failed", fmt.Sprintf("the prompt agent cannot run: %v", err)
		return w.finishPrompt(entry)
	}
	w.logf("prompt agent (%s) answering request %d: %s", spec, entry.ID, truncate(entry.Request, 120))
	callCtx, cancel := context.WithTimeout(ctx, promptCallTimeout)
	defer cancel()
	output, err := run(callCtx, spec, w.promptText(ctx, entry.Request, earlier, route))
	if ctx.Err() != nil {
		entry.Status, entry.Response = "failed", "glorp stopped before the prompt agent answered"
		return w.finishPrompt(entry)
	}
	actions := parsePromptActions(output)
	entry.Response = promptResponse(output)
	entry.Status = "done"
	if err != nil {
		entry.Status = "failed"
		if entry.Response == "" {
			entry.Response = fmt.Sprintf("the prompt agent failed: %v, and produced no output", err)
		} else {
			entry.Response += fmt.Sprintf("\n\n(the prompt agent exited with %v)", err)
		}
	}
	for _, action := range actions {
		entry.Actions = append(entry.Actions, w.applyPromptAction(ctx, action, entry.Response, route))
	}
	w.logf("prompt agent answered request %d (%s)", entry.ID, entry.Status)
	return w.finishPrompt(entry)
}

// applyPromptAction performs one action from the prompt agent's answer with
// exactly the semantics of the dashboards' controls, and says how it went.
// A comment action posts response, the agent's answer, in reply to the
// routed comment, and only route's own job can be replied for.
func (w *Glorp) applyPromptAction(ctx context.Context, action promptAction, response string, route *commentRoute) string {
	if action.Kind == "comment" {
		if route == nil || action.Key != route.JobKey {
			return fmt.Sprintf("comment %s failed: replying is only for a comment glorp routed to the prompt agent, on that comment's job", action.Key)
		}
		url, err := w.postPromptReply(ctx, *route, response)
		if err != nil {
			return fmt.Sprintf("comment %s failed: %v", action.Key, err)
		}
		w.logf("%s prompt agent replied to comment %s: %s", route.JobKey, route.Comment.URL(), url)
		return fmt.Sprintf("comment %s: posted %s", action.Key, url)
	}
	if action.Kind == "unblock" {
		key := action.Key
		if strings.EqualFold(key, "all") {
			key = ""
		}
		if err := w.TriggerUnblock(key); err != nil {
			return fmt.Sprintf("unblock %s failed: %v", action.Key, err)
		}
		w.logf("prompt agent requested an unblock round for %s", action.Key)
		return fmt.Sprintf("unblock %s: round requested; the stuck-job watchdog logs what it finds and does", action.Key)
	}
	job, ok := w.publishedJob(action.Key)
	if !ok && route != nil && action.Key == route.JobKey {
		// A routed comment's job is known to the run loop even when no
		// dashboard has published a snapshot of it.
		var target string
		var number int
		if target, number, ok = parseIssueWorkKey(action.Key); ok {
			job = JobSnapshot{Target: target, Number: number}
		}
	}
	if !ok {
		return fmt.Sprintf("%s %s failed: glorp has no such job", action.Kind, action.Key)
	}
	kind := action.Kind
	if kind == "resume" {
		// Resuming a paused job is what Retry does to it (issues #763 and
		// #771), so only a paused job can be resumed.
		if job.Status != "paused" {
			return fmt.Sprintf("resume %s failed: the job is %s, not paused", action.Key, job.Status)
		}
		kind = "retry"
	}
	w.logf("prompt agent requested %s of %s", action.Kind, action.Key)
	actionCtx, cancel := context.WithTimeout(ctx, promptActionTimeout)
	defer cancel()
	if err := w.handleJobAction(actionCtx, jobAction{Action: kind, Target: job.Target, Number: job.Number}); err != nil {
		return fmt.Sprintf("%s %s failed: %v", action.Kind, action.Key, err)
	}
	return fmt.Sprintf("%s %s: done", action.Kind, action.Key)
}

// publishedJob finds a job in the latest published snapshot by its key.
func (w *Glorp) publishedJob(key string) (JobSnapshot, bool) {
	snapshot := w.published.Load()
	if snapshot == nil {
		return JobSnapshot{}, false
	}
	for _, job := range snapshot.Jobs {
		if stuckJobKey(job) == key {
			return job, true
		}
	}
	return JobSnapshot{}, false
}

// promptActionPattern reads one action line of the prompt agent's answer,
// tolerating the list markers and code quotes a model may wrap it in.
var promptActionPattern = regexp.MustCompile("(?mi)^[\\s>*`-]*GLORP[ \\t]+(retry|pause|resume|stop|unblock|comment)[ \\t]+([^\\s`]+)[`\\s]*$")

// parsePromptActions reads the actions the prompt agent's answer asks for, in
// order, each job action at most once.
func parsePromptActions(output string) []promptAction {
	var actions []promptAction
	for _, match := range promptActionPattern.FindAllStringSubmatch(output, -1) {
		action := promptAction{Kind: strings.ToLower(match[1]), Key: match[2]}
		if !slices.Contains(actions, action) {
			actions = append(actions, action)
		}
	}
	return actions
}

// promptResponse is the agent's answer as the prompt area shows it: without
// its action lines, which glorp reports on separately, and trimmed to its end.
func promptResponse(output string) string {
	text := strings.TrimSpace(promptActionPattern.ReplaceAllString(strings.ReplaceAll(output, "\r\n", "\n"), ""))
	if len(text) > maxPromptResponse {
		text = "…" + text[len(text)-maxPromptResponse:]
	}
	return text
}

// promptText builds the prompt agent's prompt: its scope and tools, the live
// state of the instance and its jobs read now, the last few exchanges, and
// the request.
func (w *Glorp) promptText(ctx context.Context, request string, earlier []core.PromptEntry, route *commentRoute) string {
	var snapshot core.Snapshot
	if published := w.published.Load(); published != nil {
		snapshot = *published
	}
	settingsCtx, cancel := context.WithTimeout(ctx, promptSettingsTimeout)
	settings, settingsErr := w.CurrentSettings(settingsCtx)
	cancel()
	w.logMu.Lock()
	logs := append([]string(nil), w.recentLogs...)
	w.logMu.Unlock()
	var recent []core.PromptEntry
	for _, entry := range earlier {
		if entry.Status != "pending" {
			recent = append(recent, entry)
		}
	}
	if len(recent) > promptContextEntries {
		recent = recent[len(recent)-promptContextEntries:]
	}
	return promptAgentPrompt(promptState{
		Identity:    w.Identity,
		SelfLogin:   w.SelfLogin,
		Version:     version,
		Snapshot:    snapshot,
		Settings:    settings,
		SettingsErr: settingsErr,
		Agents:      w.registry().Names(),
		Logs:        logs,
		Earlier:     recent,
		Request:     request,
		Route:       route,
		Now:         time.Now(),
	})
}

// promptState is everything one prompt agent call is told.
type promptState struct {
	Identity    Identity
	SelfLogin   string
	Version     string
	Snapshot    core.Snapshot
	Settings    core.SettingsSnapshot
	SettingsErr error
	Agents      []string
	Logs        []string
	Earlier     []core.PromptEntry
	Request     string
	// Route is the comment glorp routed to the agent, when the request is
	// one (issue #842).
	Route *commentRoute
	Now   time.Time
}

// promptAgentPrompt asks the prompt agent to answer one request about this
// glorp instance and its jobs, and nothing else (issue #801).
func promptAgentPrompt(state promptState) string {
	var b strings.Builder
	id := string(state.Identity)
	source := "The owner typed a request into glorp's dashboard."
	if state.Route != nil {
		source = "glorp routed a new comment on a job's issue to you to decide what it calls for."
	}
	fmt.Fprintf(&b, "You are the prompt agent of the glorp instance /glorp:%s. glorp dispatches GitHub issues to coding agents as jobs. %s Your only subject is this glorp instance and its jobs.\n\n", id, source)
	b.WriteString("Scope:\n")
	b.WriteString("- You answer questions about this instance (its version, configuration, agents, and quotas) and its jobs (their state, output, and why one is paused, blocked, waiting, or stuck), and you act on its jobs only in the ways listed below.\n")
	b.WriteString("- Refuse anything else in one plain sentence and do nothing for it: writing or editing code or files in any repository, git or other shell work, and general questions unrelated to this instance. Never improvise a way around this.\n")
	b.WriteString("- You are not a job. Never claim an issue, run /gh-fix or /gh-discuss, resume or write to a job's session, or change a job's checkout.\n\n")
	b.WriteString("Answering:\n")
	b.WriteString("- Answer from the live state below, which glorp read when the request arrived. Before you explain why a job is paused, blocked, or stuck, verify its issue and pull request state with read-only gh commands now: an earlier report on the thread, or in the job's output, may be stale. Say what you verified.\n")
	b.WriteString("- Keep the answer short and plain.\n\n")
	b.WriteString("Actions glorp performs for you after your reply, with exactly the semantics of the dashboard's buttons. To ask for one, put a line of its form on its own at the end of your reply, using a job key exactly as listed below:\n")
	b.WriteString("GLORP retry KEY    reruns the job; on a running job it stops the agent first, and on a paused job it clears the pause\n")
	b.WriteString("GLORP pause KEY    pauses an active, waiting, or queued job by owner until it is retried\n")
	b.WriteString("GLORP resume KEY   resumes a paused job, the way Retry does\n")
	b.WriteString("GLORP stop KEY     stops a running job, or clears a paused one and releases its claim\n")
	b.WriteString("GLORP unblock all  runs a stuck-job watchdog round now over every stuck job\n")
	b.WriteString("GLORP unblock KEY  runs a stuck-job watchdog round now over that job alone\n")
	if state.Route == nil {
		b.WriteString("Only add an action line when the owner asked for that action. glorp reports how each one went below your reply, so do not claim it already happened.\n\n")
	} else {
		b.WriteString("Only add an action line the routed comment below calls for. glorp reports how each one went below your reply, so do not claim it already happened.\n\n")
	}
	b.WriteString("Actions you perform yourself:\n")
	fmt.Fprintf(&b, "- Filing an issue: when the owner describes an issue to file, run the gh-create skill as /gh-create OWNER/REPO <the owner's description> identity:/glorp:%s, choosing OWNER/REPO from the owner's words or this instance's targets, and ask which repository when that is unclear. Reply with the new issue's URL.\n", id)
	if state.Route == nil {
		fmt.Fprintf(&b, "- Updating an issue or pull request: post one comment on it with gh that mentions its author as @LOGIN and this instance as @/glorp:%s, and carries the update. That mention is how the owning job receives it. Never edit an issue or pull request body or title to perform an update. Reply with the comment's URL.\n", id)
	} else {
		writeCommentRouting(&b, *state.Route)
	}
	if state.SelfLogin != "" {
		fmt.Fprintf(&b, "glorp runs as @%s on GitHub.\n", state.SelfLogin)
	}
	b.WriteString("\nInstance:\n")
	fmt.Fprintf(&b, "- identity: /glorp:%s\n", id)
	fmt.Fprintf(&b, "- glorp version: %s\n", state.Version)
	fmt.Fprintf(&b, "- now: %s\n", state.Now.Format(time.RFC3339))
	snapshot := state.Snapshot
	if len(snapshot.Targets) > 0 {
		fmt.Fprintf(&b, "- targets: %s\n", strings.Join(formatTargets(snapshot.Targets, snapshot.IssueCounts), ", "))
	}
	fmt.Fprintf(&b, "- jobs: %d running, %d queued, %d paused, %d completed, %d failed; concurrency %d\n", snapshot.Running, snapshot.Queued, snapshot.Paused, snapshot.Completed, snapshot.Failed, snapshot.Concurrency)
	fmt.Fprintf(&b, "- %s\n", deliveryText(snapshot))
	if !snapshot.LastPoll.IsZero() {
		fmt.Fprintf(&b, "- last poll: %s\n", snapshot.LastPoll.Format(time.RFC3339))
	}
	if snapshot.WebUIURL != "" {
		fmt.Fprintf(&b, "- web dashboard: %s\n", snapshot.WebUIURL)
	}
	fmt.Fprintf(&b, "- %s\n", quotaText(snapshot))
	fmt.Fprintf(&b, "- registered agents: %s\n", strings.Join(state.Agents, ", "))
	if state.SettingsErr != nil {
		fmt.Fprintf(&b, "- settings: unavailable (%v)\n", state.SettingsErr)
	} else {
		settings := state.Settings
		fmt.Fprintf(&b, "- coding agents: %s\n", strings.Join(settings.ConfiguredAgents, ", "))
		fmt.Fprintf(&b, "- no merge: %t; no changelog: %t; auto-update while busy: %t\n", settings.NoMerge, settings.NoChangelog, settings.AutoUpdate)
		if len(settings.AllowedCommenters) > 0 {
			fmt.Fprintf(&b, "- allowed commenters: %s\n", strings.Join(settings.AllowedCommenters, ", "))
		}
		if len(settings.TempDirs) > 0 {
			fmt.Fprintf(&b, "- temp directories for clones (in order): %s\n", strings.Join(settings.TempDirs, ", "))
		} else {
			fmt.Fprintf(&b, "- temp directory for clones: %s (system default)\n", settings.TempDirDefault)
		}
		fmt.Fprintf(&b, "- stuck-job watchdog interval: %s; unblock agent: %s\n", settings.UnblockInterval, overrideText(settings.UnblockAgent, settings.UnblockModel, settings.UnblockDefault))
		fmt.Fprintf(&b, "- prompt agent: %s\n", overrideText(settings.PromptAgent, settings.PromptModel, settings.PromptDefault))
	}
	b.WriteString("\nJobs:\n")
	if len(snapshot.Jobs) == 0 {
		b.WriteString("(none)\n")
	}
	for _, job := range snapshot.Jobs {
		fmt.Fprintf(&b, "\n- key: %s\n", stuckJobKey(job))
		if job.Title != "" {
			fmt.Fprintf(&b, "  title: %s\n", job.Title)
		}
		fmt.Fprintf(&b, "  status: %s\n", job.Status)
		if job.WaitReason != "" {
			fmt.Fprintf(&b, "  waits on: %s\n", job.WaitReason)
		}
		if !job.WakeAt.IsZero() {
			fmt.Fprintf(&b, "  wakes at: %s\n", job.WakeAt.Format(time.RFC3339))
		}
		if url := job.IssueURL(); url != "" {
			fmt.Fprintf(&b, "  issue: %s\n", url)
		}
		if url := job.PullRequestURL(); url != "" {
			fmt.Fprintf(&b, "  pull request: %s\n", url)
		}
		if job.HasProgress() {
			fmt.Fprintf(&b, "  plan: %d of %d tasks done\n", job.TasksDone, job.TasksTotal)
		}
		if summary := jobAgentSummary(job); summary != "" {
			fmt.Fprintf(&b, "  agent: %s\n", summary)
		}
		if !job.Started.IsZero() {
			fmt.Fprintf(&b, "  started: %s\n", job.Started.Format(time.RFC3339))
		}
		if job.CheckoutDirectory != "" {
			fmt.Fprintf(&b, "  checkout: %s\n", job.CheckoutDirectory)
		}
		if job.SessionID != "" {
			fmt.Fprintf(&b, "  session: %s\n", job.SessionID)
		}
		if tail := lastLines(job.Log, promptOutputLines); tail != "" {
			b.WriteString("  last agent output:\n")
			b.WriteString(indentLines(tail, "    ") + "\n")
		}
	}
	if len(state.Logs) > 0 {
		b.WriteString("\nRecent glorp log:\n")
		b.WriteString(indentLines(strings.Join(state.Logs, "\n"), "  ") + "\n")
	}
	if len(state.Earlier) > 0 {
		b.WriteString("\nEarlier requests in this prompt area, for context only:\n")
		for _, entry := range state.Earlier {
			fmt.Fprintf(&b, "\n- request: %s\n", entry.Request)
			if entry.Response != "" {
				b.WriteString("  answer:\n" + indentLines(lastLines(entry.Response, promptOutputLines), "    ") + "\n")
			}
			for _, action := range entry.Actions {
				fmt.Fprintf(&b, "  action: %s\n", action)
			}
		}
	}
	if state.Route != nil {
		fmt.Fprintf(&b, "\nglorp's request:\n%s\n", state.Request)
	} else {
		fmt.Fprintf(&b, "\nThe owner's request:\n%s\n", state.Request)
	}
	return b.String()
}

// overrideText describes an agent override for the prompt: the override when
// one is set, otherwise the coding agent it falls back to.
func overrideText(agent, model, fallback string) string {
	switch {
	case agent != "" && model != "":
		return agent + " with model " + model
	case agent != "":
		return agent
	case model != "":
		return "the coding agent with model " + model
	case fallback != "":
		return "the coding agent (" + fallback + ")"
	}
	return "the coding agent"
}

// writeCommentRouting tells the prompt agent how to handle a comment glorp
// routed to it (issue #842): restart the job, or reply without mentioning
// anyone, since a mention would be detected as a new comment and loop.
func writeCommentRouting(b *strings.Builder, route commentRoute) {
	b.WriteString("\nThis request is a routed comment, not the owner typing:\n")
	fmt.Fprintf(b, "- A new comment from @%s arrived at %s, on the issue of job %s (%s#%d). glorp did not restart the job for it; you decide what it calls for.\n", route.Comment.Author, route.Comment.URL(), route.JobKey, route.Repository, route.Number)
	b.WriteString("- Read the comment, the issue, and its pull request with read-only gh commands, alongside the job's state below.\n")
	fmt.Fprintf(b, "- When the comment calls for the work itself to change, resume, or be redone, restart the job: end your reply with the line GLORP retry %s\n", route.JobKey)
	fmt.Fprintf(b, "- When a reply is all it needs, write that reply as your answer, addressed to the comment's reader, and end it with the line GLORP comment %s. glorp posts your answer, without its action lines, as a comment on the thread the comment is on.\n", route.JobKey)
	b.WriteString("- Never @mention anyone in that reply, neither a person nor a glorp instance: a mention would be detected as a new comment and start this over. glorp breaks any mention it finds anyway.\n")
	b.WriteString("- Never post, edit, or delete comments yourself with gh; glorp posts your reply for you. Ask for at most one of the two actions.\n")
}
