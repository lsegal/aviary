package projects

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/process"
	"github.com/lsegal/aviary/internal/projects/core"
)

// defaultUnblockInterval is how often the stuck-job watchdog looks for stuck
// jobs unless --unblock-interval says otherwise.
const defaultUnblockInterval = 15 * time.Minute

// minUnblockInterval is the shortest interval the watchdog accepts, so a typo
// cannot start an agent call every few seconds. Zero turns the watchdog off.
const minUnblockInterval = time.Minute

// unblockCallTimeout bounds one unblock agent call, so a call that hangs
// cannot stop the watchdog for good.
const unblockCallTimeout = 30 * time.Minute

// unblockOutputLines is how much of a stalled job's scrollback the unblock
// agent is shown.
const unblockOutputLines = 20

// unblockHistoryLines is how much of a failed unblock call's output each
// stuck job's history keeps, short because the history is repeated in every
// later round's prompt.
const unblockHistoryLines = 5

// unblockConfig is the unblock call's agent and model override. Empty fields
// fall back to the coding agent and its model.
type unblockConfig struct {
	Agent string
	Model string
}

// stuckJob is one job a watchdog round found stuck, with what was already
// tried for it in earlier rounds.
type stuckJob struct {
	Key   string
	Job   JobSnapshot
	Why   string
	Tried []string
}

// unblockAction is one line of the unblock agent's closing report.
type unblockAction struct {
	Key    string
	Kind   string
	Detail string
}

// validateUnblockInterval rejects an interval the watchdog cannot run with.
func validateUnblockInterval(interval time.Duration) error {
	if interval != 0 && interval < minUnblockInterval {
		return fmt.Errorf("unblock interval must be 0 (off) or at least %s", formatInterval(minUnblockInterval))
	}
	return nil
}

// parseUnblockInterval reads an interval the way --unblock-interval does,
// also accepting a bare 0 to turn the watchdog off.
func parseUnblockInterval(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "0" {
		return 0, nil
	}
	interval, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("unblock interval %q is not a duration such as 5m", value)
	}
	return interval, validateUnblockInterval(interval)
}

// validateUnblockAgent checks the unblock override against the registry. An
// empty agent means the coding agent, whose models are checked when a round
// resolves it, since the coding agent can change while the watch runs.
func validateUnblockAgent(registry *clidefs.Registry, config unblockConfig) error {
	if config.Agent == "" {
		return nil
	}
	spec, err := parseAgentSpecIn(registry, config.Agent)
	if err != nil {
		return err
	}
	if config.Model == "" {
		return nil
	}
	definition, _ := registry.Lookup(spec.Name)
	if !definition.AcceptsModel(config.Model) {
		return definition.ModelError()
	}
	return nil
}

// UnblockInterval is how often the watchdog runs, or 0 when it is off.
func (w *Engine) UnblockInterval() time.Duration {
	return time.Duration(w.unblockInterval.Load())
}

// SetUnblockInterval changes the watchdog's interval, restarting its wait so
// the new interval takes effect at once.
func (w *Engine) SetUnblockInterval(interval time.Duration) {
	w.unblockInterval.Store(int64(interval))
	select {
	case w.unblockResets() <- struct{}{}:
	default:
	}
}

func (w *Engine) unblockResets() chan struct{} {
	w.unblockResetOnce.Do(func() { w.unblockReset = make(chan struct{}, 1) })
	return w.unblockReset
}

func (w *Engine) unblockSettings() unblockConfig {
	if config := w.unblockOverride.Load(); config != nil {
		return *config
	}
	return unblockConfig{}
}

// SetUnblockAgent sets the unblock call's agent and model override.
func (w *Engine) SetUnblockAgent(config unblockConfig) {
	config.Agent, config.Model = strings.TrimSpace(config.Agent), strings.TrimSpace(config.Model)
	w.unblockOverride.Store(&config)
}

// unblockSpec resolves the agent and model the unblock call runs with: the
// override when one is set, otherwise the coding agent aviary dispatches jobs
// to and its model.
func (w *Engine) unblockSpec() (agentSpec, error) {
	return w.oneShotSpec(w.unblockSettings())
}

// oneShotSpec resolves an out-of-band agent call's override, falling back to
// the coding agent and its model for whatever the override leaves empty.
func (w *Engine) oneShotSpec(config unblockConfig) (agentSpec, error) {
	registry := w.registry()
	var spec agentSpec
	if config.Agent != "" {
		parsed, err := parseAgentSpecIn(registry, config.Agent)
		if err != nil {
			return agentSpec{}, err
		}
		spec = parsed
	} else {
		runner, ok := w.runner().(CommandRunner)
		if !ok {
			return agentSpec{}, fmt.Errorf("no coding agent is configured")
		}
		spec = runner.specForSession(AgentSession{})
	}
	if config.Model != "" {
		definition, ok := registry.Lookup(spec.Name)
		if !ok {
			return agentSpec{}, llm.UnknownCLIProviderError(registry, spec.Name)
		}
		if !definition.AcceptsModel(config.Model) {
			return agentSpec{}, definition.ModelError()
		}
		spec.Model = config.Model
	}
	return spec, nil
}

// watchStuckJobs is the stuck-job watchdog. Every interval it
// looks for jobs that are paused or whose scrollback has not changed since the
// last look, and runs one out-of-band unblock agent call covering all of them.
// Rounds never overlap: the next interval starts once a call has finished.
func (w *Engine) watchStuckJobs(ctx context.Context, jobs func() []JobSnapshot) {
	if interval := w.UnblockInterval(); interval > 0 {
		w.logf("checking for stuck jobs every %s", formatInterval(interval))
	}
	baseline := map[string]string{}
	baselineAt := time.Now()
	history := map[string][]string{}
	for {
		interval := w.UnblockInterval()
		var due <-chan time.Time
		var timer *time.Timer
		if interval > 0 {
			timer = time.NewTimer(interval)
			due = timer.C
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-w.unblockResets():
			// The interval changed: the jobs seen so far were not watched
			// for a full interval of the new length.
			if timer != nil {
				timer.Stop()
			}
			baseline, baselineAt = scrollbacks(jobs()), time.Now()
			continue
		case key := <-w.unblockTriggers():
			// A round asked for from the prompt area runs now, through the
			// same round and history as a timed one.
			if timer != nil {
				timer.Stop()
			}
			current := jobs()
			since := time.Since(baselineAt).Truncate(time.Second)
			stuck, err := requestedStuckJobs(current, baseline, history, since, key)
			if err != nil {
				w.logf("stuck-job watchdog: %v", err)
				continue
			}
			if len(stuck) == 0 {
				w.logf("stuck-job watchdog: the unblock round requested from the prompt area found no stuck jobs")
				continue
			}
			w.runUnblockRound(ctx, stuck, history, since)
			baseline, baselineAt = scrollbacks(jobs()), time.Now()
			continue
		case <-due:
		}
		current := jobs()
		stuck := detectStuckJobs(current, baseline, history, interval)
		if len(stuck) == 0 {
			baseline = scrollbacks(current)
			continue
		}
		w.runUnblockRound(ctx, stuck, history, interval)
		// Every job gets a full interval after the round to show progress.
		baseline = scrollbacks(jobs())
	}
}

func (w *Engine) unblockTriggers() chan string {
	w.unblockTriggerOnce.Do(func() { w.unblockTrigger = make(chan string, 1) })
	return w.unblockTrigger
}

// TriggerUnblock asks the watchdog for an unblock round now, over every stuck
// job or, given a job key, over that job alone. It never waits:
// the round runs on the watchdog's goroutine and logs what it finds and does,
// as a timed round does.
func (w *Engine) TriggerUnblock(key string) error {
	select {
	case w.unblockTriggers() <- strings.TrimSpace(key):
		return nil
	default:
		return fmt.Errorf("an unblock round is already requested and has not started yet")
	}
}

// requestedStuckJobs lists the jobs a requested round covers. With no key it
// is the jobs a timed round would cover, judged against the last look. With a
// key it is that job alone, stuck or not, since the owner named it. A running
// job silent for less than the shortest watchdog interval is not stuck yet.
func requestedStuckJobs(jobs []JobSnapshot, baseline map[string]string, history map[string][]string, since time.Duration, key string) ([]stuckJob, error) {
	if since < minUnblockInterval {
		baseline = nil
	}
	stuck := detectStuckJobs(jobs, baseline, history, since)
	if key == "" {
		return stuck, nil
	}
	for _, item := range stuck {
		if item.Key == key {
			return []stuckJob{item}, nil
		}
	}
	for _, job := range jobs {
		if stuckJobKey(job) != key {
			continue
		}
		if isDiscussionTarget(job.Target) {
			return nil, fmt.Errorf("the unblock round requested for %s cannot run: it is a discussion, not an issue job", key)
		}
		why := "named from the prompt area while " + job.Status
		if job.WaitReason != "" {
			why += ": " + job.WaitReason
		}
		return []stuckJob{{Key: key, Job: job, Why: why}}, nil
	}
	return nil, fmt.Errorf("the unblock round requested for %s cannot run: aviary has no such job", key)
}

// scrollbacks records each job's scrollback, to compare a later look with.
func scrollbacks(jobs []JobSnapshot) map[string]string {
	logs := make(map[string]string, len(jobs))
	for _, job := range jobs {
		logs[stuckJobKey(job)] = job.Log
	}
	return logs
}

func stuckJobKey(job JobSnapshot) string {
	return fmt.Sprintf("%s#%d", job.Target, job.Number)
}

// detectStuckJobs lists the jobs a round covers: paused jobs, except ones the
// owner paused on purpose, and active jobs whose scrollback is the same as at
// the previous look, a full interval ago. A job not seen at the previous look
// has not been watched for a full interval yet, so it is never stuck. History
// is trimmed to the jobs still stuck.
func detectStuckJobs(jobs []JobSnapshot, baseline map[string]string, history map[string][]string, interval time.Duration) []stuckJob {
	var stuck []stuckJob
	for _, job := range jobs {
		if isDiscussionTarget(job.Target) {
			continue
		}
		key := stuckJobKey(job)
		why := ""
		switch job.Status {
		case "paused":
			if core.HeldForOwner(job.WaitReason) {
				continue
			}
			why = "paused"
			if job.WaitReason != "" {
				why += ": " + job.WaitReason
			}
		case "active":
			previous, seen := baseline[key]
			if !seen || previous != job.Log {
				continue
			}
			why = fmt.Sprintf("active, but its agent has written no output for at least %s", formatInterval(interval))
		default:
			continue
		}
		stuck = append(stuck, stuckJob{Key: key, Job: job, Why: why, Tried: append([]string(nil), history[key]...)})
	}
	slices.SortFunc(stuck, func(a, b stuckJob) int { return strings.Compare(a.Key, b.Key) })
	for key := range history {
		if !slices.ContainsFunc(stuck, func(item stuckJob) bool { return item.Key == key }) {
			delete(history, key)
		}
	}
	return stuck
}

// runUnblockRound makes the round's single unblock agent call, logs what it
// detected and what the agent did for each item, and records those actions as
// the items' history for the next round.
func (w *Engine) runUnblockRound(ctx context.Context, stuck []stuckJob, history map[string][]string, interval time.Duration) {
	described := make([]string, len(stuck))
	for i, item := range stuck {
		described[i] = fmt.Sprintf("%s (%s)", item.Key, item.Why)
	}
	w.logf("stuck-job watchdog found %d stuck job(s): %s", len(stuck), strings.Join(described, "; "))
	spec, err := w.unblockSpec()
	if err != nil {
		w.logf("stuck-job watchdog cannot run the unblock agent: %v", err)
		return
	}
	run := w.unblockRun
	if run == nil {
		runner, ok := w.runner().(CommandRunner)
		if !ok {
			w.logf("stuck-job watchdog cannot run the unblock agent: no coding agent is configured")
			return
		}
		run = runner.runUnblock
	}
	w.logf("stuck-job watchdog running one unblock agent call (%s) for %d job(s)", spec, len(stuck))
	callCtx, cancel := context.WithTimeout(ctx, unblockCallTimeout)
	defer cancel()
	output, err := run(callCtx, spec, unblockPrompt(w.Identity, w.SelfLogin, w.WebUIURL, stuck, interval))
	if ctx.Err() != nil {
		return
	}
	stamp := time.Now().Format("2006-01-02 15:04")
	actions := parseUnblockActions(output)
	if err != nil && len(actions) == 0 {
		// The exit code alone says nothing about why the call failed, so the
		// agent's own last words go into the log and the history, where the
		// next round's escalation reads them.
		tail := lastLines(output, unblockOutputLines)
		if tail == "" {
			w.logf("stuck-job watchdog unblock agent failed: %v, and the agent produced no output", err)
			for _, item := range stuck {
				history[item.Key] = append(history[item.Key], fmt.Sprintf("%s: the unblock agent failed (%v) with no output before acting", stamp, err))
			}
			return
		}
		w.logf("stuck-job watchdog unblock agent failed: %v; its last output:\n%s", err, indentLines(tail, "  "))
		reason := strings.Join(strings.Split(lastLines(output, unblockHistoryLines), "\n"), " / ")
		for _, item := range stuck {
			history[item.Key] = append(history[item.Key], fmt.Sprintf("%s: the unblock agent failed (%v) before acting, saying: %s", stamp, err, reason))
		}
		return
	}
	if err != nil {
		// The agent reported its outcome before exiting non-zero, such as
		// when it could not unblock a job, so the round reads as that report.
		w.logf("stuck-job watchdog unblock agent exited with %v after reporting its actions", err)
	}
	for _, item := range stuck {
		action, ok := actions[item.Key]
		if !ok {
			w.logf("stuck-job watchdog: the unblock agent reported no action for %s", item.Key)
			if err != nil {
				history[item.Key] = append(history[item.Key], fmt.Sprintf("%s: the unblock agent exited with %v and reported no action", stamp, err))
				continue
			}
			history[item.Key] = append(history[item.Key], fmt.Sprintf("%s: the unblock agent ran but reported no action", stamp))
			continue
		}
		label := map[string]string{"comment": "got a comment", "local": "got a local task", "none": "needed no action"}[action.Kind]
		w.logf("stuck-job watchdog: %s %s: %s", item.Key, label, action.Detail)
		history[item.Key] = append(history[item.Key], fmt.Sprintf("%s: %s: %s", stamp, action.Kind, action.Detail))
	}
}

// unblockActionPattern reads one closing report line, tolerating the list
// markers and code quotes a model may wrap it in.
var unblockActionPattern = regexp.MustCompile("(?m)^[\\s>*`-]*UNBLOCK\\s+(\\S+)\\s+(comment|local|none)\\b[ \\t]*:?[ \\t]*(.*?)[`\\s]*$")

// parseUnblockActions reads the unblock agent's closing report, keyed by job.
// The last line about a job wins.
func parseUnblockActions(output string) map[string]unblockAction {
	actions := map[string]unblockAction{}
	for _, match := range unblockActionPattern.FindAllStringSubmatch(output, -1) {
		detail := strings.TrimSpace(match[3])
		if detail == "" {
			detail = "(no detail given)"
		}
		actions[match[1]] = unblockAction{Key: match[1], Kind: match[2], Detail: detail}
	}
	return actions
}

// unblockPrompt asks the unblock agent to verify every stuck job's live state
// and unblock each one, planning them together because they can be related.
func unblockPrompt(identity Identity, selfLogin, webUIURL string, stuck []stuckJob, interval time.Duration) string {
	var b strings.Builder
	mention := "@/aviary:" + string(identity)
	fmt.Fprintf(&b, "You are the stuck-job watchdog of the aviary instance /aviary:%s. aviary dispatches GitHub issues to coding agents as jobs. The %d job(s) below look stuck: each is paused, or its agent has written no new output for at least %s. You are not a job on any of these issues: do not claim them, do not run /gh-fix for them, and do not resume their sessions. Unblock them so their own jobs pick the work back up.\n\n", identity, len(stuck), formatInterval(interval))
	b.WriteString("The jobs may be related, for example stacked pull requests parked behind one root pull request, so plan the steps for all of them together before acting.\n\n")
	b.WriteString("For each job:\n")
	b.WriteString("1. Verify its live state with gh before prescribing anything: the issue and pull request state, check results, mergeability, and what has merged or closed since the job stopped. A report the job's agent posted earlier may be stale, and that is often why the job is stuck.\n")
	b.WriteString("2. If nothing blocks it any more and it only needs to hear so, or its agent is legitimately busy, such as waiting on a long CI run, leave it alone and report none.\n")
	fmt.Fprintf(&b, "3. Otherwise post one comment on its pull request, or on its issue when it has none, that mentions the pull request's or issue's author with @LOGIN and this instance with %s, and gives concrete next steps grounded in the live state you verified. aviary's mention path delivers that comment to the owning job, which wakes or resumes it.", mention)
	if selfLogin != "" {
		fmt.Fprintf(&b, " aviary runs as @%s.", selfLogin)
	}
	b.WriteString("\n")
	b.WriteString("4. When a comment will not work for a job, perform a local task on this machine instead, such as fixing its checkout")
	if webUIURL != "" {
		fmt.Fprintf(&b, " or retrying, stopping, or pausing it through aviary's dashboard API: POST %s/api/jobs/action with the JSON body {\"action\":\"retry\",\"target\":\"TARGET\",\"number\":NUMBER}, where TARGET and NUMBER come from the job's key TARGET#NUMBER", strings.TrimRight(webUIURL, "/"))
	}
	b.WriteString(".\n")
	b.WriteString("Do every step you can do yourself instead of leaving it to the owner, and prefer steps that let the owning job continue its own work over doing that work for it. A job listed with earlier interventions is still stuck after them: never repeat an intervention that already failed, such as posting the same comment again; escalate to a different one or to a local task.\n\n")
	b.WriteString("When you are done, end your reply with exactly one line per job in one of these forms, using the job's key exactly as given:\n")
	b.WriteString("UNBLOCK KEY comment: COMMENT URL and a one-line summary\n")
	b.WriteString("UNBLOCK KEY local: a one-line summary of the local task\n")
	b.WriteString("UNBLOCK KEY none: why it needed nothing\n\n")
	b.WriteString("Jobs:\n")
	for _, item := range stuck {
		job := item.Job
		fmt.Fprintf(&b, "\n- key: %s\n", item.Key)
		if job.Title != "" {
			fmt.Fprintf(&b, "  title: %s\n", job.Title)
		}
		if url := job.IssueURL(); url != "" {
			fmt.Fprintf(&b, "  issue: %s\n", url)
		}
		if url := job.PullRequestURL(); url != "" {
			fmt.Fprintf(&b, "  pull request: %s\n", url)
		}
		fmt.Fprintf(&b, "  stuck because: %s\n", item.Why)
		if job.CheckoutDirectory != "" {
			fmt.Fprintf(&b, "  checkout: %s\n", job.CheckoutDirectory)
		}
		if len(item.Tried) > 0 {
			b.WriteString("  already tried in earlier rounds:\n")
			for _, tried := range item.Tried {
				fmt.Fprintf(&b, "    - %s\n", tried)
			}
		}
		if tail := lastLines(job.Log, unblockOutputLines); tail != "" {
			b.WriteString("  last agent output:\n")
			for _, line := range strings.Split(tail, "\n") {
				fmt.Fprintf(&b, "    %s\n", line)
			}
		}
	}
	return b.String()
}

// lastLines returns the last n non-empty lines of text.
func lastLines(text string, n int) string {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimRight(line, " \t\r"))
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// indentLines prefixes every line of text with indent.
func indentLines(text, indent string) string {
	return indent + strings.ReplaceAll(text, "\n", "\n"+indent)
}

// runUnblock runs the unblock call as a one-shot agent invocation outside any
// checkout, and returns the readable text it wrote.
func (r CommandRunner) runUnblock(ctx context.Context, spec agentSpec, prompt string) (string, error) {
	definition, ok := r.definition(spec.Name)
	if !ok {
		return "", llm.UnknownCLIProviderError(r.registry(), spec.Name)
	}
	if definition.InProcess {
		var text bytes.Buffer
		err := r.runInProcess(ctx, spec, "", prompt, "", os.TempDir(), nil, &text, nil)
		return text.String(), err
	}
	call, err := newOneShotPrompt(definition, clidefs.Values{Prompt: prompt, Model: spec.Model, Level: spec.Level, Approval: r.Approval}, os.TempDir())
	if err != nil {
		return "", err
	}
	defer call.cleanup()
	args := definition.Render(clidefs.ModeRun, call.Values)
	if len(args) == 0 {
		return "", fmt.Errorf("agent %s declares no run arguments", spec.Name)
	}
	cmd := newAgentCommand(ctx, r.binary(spec.Name), args...)
	if env := definition.EnvList(); len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.Dir = os.TempDir()
	if call.Stdin != nil {
		cmd.Stdin = call.Stdin
	}
	var text bytes.Buffer
	output := io.Writer(&text)
	decoder := newOutputDecoder(definition.Output, output)
	if decoder != nil {
		output = decoder
	}
	cmd.Stdout, cmd.Stderr = output, output
	if err = process.Start(cmd); err != nil {
		return "", spawnError(cmd, err)
	}
	err = process.Wait(cmd)
	if decoder != nil {
		if flushErr := decoder.Flush(); err == nil {
			err = flushErr
		}
	}
	return text.String(), err
}
