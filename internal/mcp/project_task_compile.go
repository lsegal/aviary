package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/scheduler"
)

// Project tasks used to be hard-coded out of precompilation: they ran only as
// Code Loop jobs, whose coding agent works in an isolated clone with gh and
// git, while a compiled script ran in core's script runtime with no clone, no
// repository context and the tools of no particular agent. Precompiling one
// now compiles it against the tools of the agent the task belongs to (the
// agent its script runs as), with the clone described in the prompt, and runs
// the script inside a fresh clone of the project with that clone granted to
// its tools and AVIARY_CHECKOUT_DIRECTORY set (see scheduler.ProjectRun).
// Like a scheduled prompt task, a project task keeps its prompt, so a run
// whose script fails falls back to it. A prompt workflow precompiles the
// same way: its script runs on each matching
// message, with the message in the Lua global trigger, and a run whose script
// fails falls back to the prompt on the model.

// compileToolsFunc, when set, replaces compileTools in tests.
var compileToolsFunc func(ctx context.Context, agentName string) ([]agent.ToolInfo, error)

func resolveCompileTools() func(ctx context.Context, agentName string) ([]agent.ToolInfo, error) {
	if compileToolsFunc != nil {
		return compileToolsFunc
	}
	return compileTools
}

// projectTaskCompilePrompt is what a project task's prompt is analyzed and
// compiled as: the prompt, plus where its script runs. exec does not run its
// command through a shell, so the repository is spelled out for the script to
// write literally: "$GH_REPO" or gh's "{owner}/{repo}" would reach gh as is.
func projectTaskCompilePrompt(prompt, project string) string {
	project = strings.TrimSpace(project)
	return strings.TrimSpace(prompt) + "\n\n" +
		"This task runs on a timer against the GitHub repository " + project +
		", inside a fresh clone of it. exec runs commands in that clone by default, with gh authenticated. " +
		"exec runs a command directly, not through a shell: environment variables such as $GH_REPO, " +
		"pipes, redirections like 2>&1 and globs are not expanded. Write the repository literally as " + project +
		" in every command, never as a variable or as gh's {owner}/{repo} placeholders. For example, main's head SHA is " +
		"`gh api repos/" + project + "/commits/main --jq .sha` and the latest release is " +
		"`gh api repos/" + project + "/releases/latest --jq .tag_name`."
}

// workflowTaskCompilePrompt is what a prompt workflow is analyzed and compiled
// as: the prompt, plus the triggering message its script reads at run time.
func workflowTaskCompilePrompt(prompt string) string {
	return strings.TrimSpace(prompt) + "\n\n" +
		"This task is a workflow: it runs each time a matching message arrives on one of the agent's channels. " +
		"Its script reads the message that triggered the run from the Lua global table `trigger`: " +
		"trigger.message is the triggering message, trigger.before lists the messages just before it, oldest first, " +
		"and each message has text, sender (a display name, possibly empty), from_agent (true for the agent's own messages), " +
		"timestamp and attachments. trigger.channel, trigger.channel_type and trigger.entry name the channel and conversation. " +
		"What the script prints is the workflow's reply, delivered verbatim to its target. " +
		"When the workflow decides not to reply, the script calls tool.task_no_reply({reason = \"<short reason>\"}) and prints nothing, " +
		"so nothing is delivered and the reason is recorded in the run history."
}

// taskCompilePrompt is what task is analyzed and compiled as. A scheduled
// prompt task is compiled from its prompt alone, as task_schedule compiles
// one.
func taskCompilePrompt(task config.TaskConfig) string {
	switch {
	case task.IsWorkflow():
		return workflowTaskCompilePrompt(task.Prompt)
	case strings.EqualFold(task.Type, "project"):
		return projectTaskCompilePrompt(task.Prompt, task.Project)
	}
	return task.Prompt
}

// shellVariablePattern matches a shell variable reference such as $GH_REPO or
// ${HOME}. Lua patterns use "$" too, but only as an anchor, never before a
// name.
var shellVariablePattern = regexp.MustCompile(`\$\{?[A-Za-z_][A-Za-z0-9_]*`)

// projectScriptPlaceholderProblem reports why a compiled project task script
// cannot work as written: a command that names the repository through gh's
// {owner}/{repo} placeholders or a shell variable, which exec passes to the
// command unexpanded. It returns "" for a script with neither.
func projectScriptPlaceholderProblem(script string) string {
	for _, placeholder := range []string{"{owner}", "{repo}"} {
		if strings.Contains(script, placeholder) {
			return "the script uses gh's " + placeholder + " placeholder instead of the project's owner/repo"
		}
	}
	if match := shellVariablePattern.FindString(script); match != "" {
		return "the script uses the shell variable " + match + ", which exec does not expand"
	}
	return ""
}

// taskCompilerVersion is part of every compile key. Bump it whenever the
// compiler, its determinism gate, the compile prompts or the script runtime
// change in a way that can change or fix a compiled script, so every existing
// compiled task, including one whose script was set aside after failing, is
// compiled again on its next run instead of keeping a script built by the old
// compiler. Version 3 changed the project prompt's latest-release example to
// the /releases/latest endpoint (#1189).
var taskCompilerVersion = 3

// projectTaskCompileKey identifies what a project task's script is compiled
// from: the compiler version, its prompt, its project, and the tools it may
// call, including the commands exec allows, so a change to any of them
// recompiles it.
func projectTaskCompileKey(prompt, project string, tools []agent.ToolInfo, agentCfg *config.AgentConfig) string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	var commands []string
	if agentCfg != nil && agentCfg.Permissions != nil && agentCfg.Permissions.Exec != nil {
		commands = slices.Clone(agentCfg.Permissions.Exec.AllowedCommands)
	}
	sum := sha256.New()
	for _, part := range []string{strconv.Itoa(taskCompilerVersion), strings.TrimSpace(prompt), strings.TrimSpace(project), strings.Join(names, ","), strings.Join(commands, "\n")} {
		sum.Write([]byte(part))
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))[:16]
}

// EffectiveTaskPrecompile reports whether a prompt or project task
// precompiles. A task that never chose stays off, so existing tasks do not
// start compiling; editors and task_schedule write the choice explicitly.
func EffectiveTaskPrecompile(task config.TaskConfig) bool {
	return task.PrecompilesBesidePrompt() && config.BoolOr(task.Precompile, false)
}

// backgroundCompiles tracks compiles started by compileTaskInBackground,
// so tests can wait for them before restoring the state they read.
var backgroundCompiles sync.WaitGroup

// compileTaskInBackground compiles a precompiled task without blocking
// the caller, logging a failure.
func compileTaskInBackground(agentName, taskName, trigger string) {
	backgroundCompiles.Go(func() {
		if _, err := EnsureTaskCompiled(context.Background(), agentName, taskName, trigger); err != nil {
			slog.Warn("task_compile: task compile failed", "component", "task_compile", "agent", agentName, "task", taskName, "error", err)
		}
	})
}

// RecompileTask discards a prompt or project task's compiled script, whatever
// state it is in, turns precompile on, and compiles it again in the
// background. It is the Recompile action: it starts over a script that is
// stale or keeps failing.
func RecompileTask(agentName, taskName string) error {
	_, agentCfg, task, err := findConfiguredTask(agentName, taskName)
	if err != nil {
		return err
	}
	if !task.PrecompilesBesidePrompt() {
		return fmt.Errorf("task %q is a %s task, which has no prompt to compile", task.Name, task.Type)
	}
	on := true
	task.Precompile = &on
	task.Compiled, task.CompiledKey = "", ""
	task.ScriptVerified, task.ScriptFailures, task.ScriptLastError, task.PrecompileNote = false, 0, "", ""
	if err := saveConfiguredTask(agentCfg, task); err != nil {
		return err
	}
	compileTaskInBackground(agentCfg.Name, task.Name, "manual")
	return nil
}

// EnsureTaskCompiled returns the compiled script a precompiled prompt or
// project task runs, compiling it first when its prompt, project or tools
// changed since the last compile. It returns "" when the task does not precompile or
// its prompt did not pass the determinism gate, so the run uses the model.
func EnsureTaskCompiled(ctx context.Context, agentName, taskName, trigger string) (string, error) {
	_, agentCfg, task, err := findConfiguredTask(agentName, taskName)
	if err != nil {
		return "", err
	}
	if !EffectiveTaskPrecompile(task) {
		return "", nil
	}
	project := strings.EqualFold(task.Type, "project")
	kind := "prompt"
	if task.IsWorkflow() {
		kind = "workflow"
	}
	if project {
		// The script is compiled against the tools it runs with.
		ctx = agent.WithProjectTaskScript(ctx)
		kind = "project"
	}
	tools, err := resolveCompileTools()(ctx, agentCfg.Name)
	if err != nil {
		return "", fmt.Errorf("listing agent %q's tools: %w", agentCfg.Name, err)
	}
	key := projectTaskCompileKey(task.Prompt, task.Project, tools, &agentCfg)
	if key == task.CompiledKey {
		return strings.TrimSpace(task.Compiled), nil
	}
	tracker := newTaskCompileTracker(agentCfg.Name, task.Name, kind, task.Prompt, task.Target, trigger, false)
	compiled, compileErr := resolveTryCompileTaskPrompt()(withTaskCompileTracker(ctx, tracker), agentCfg.Name, taskCompilePrompt(task), task.Target, false)
	if compileErr != nil {
		tracker.record.Status = domain.TaskCompileStatusFailed
		tracker.record.Reason = compileErr.Error()
		persistProjectCompileRecord(tracker)
		// The key is left as it was, so the next run tries again.
		return "", fmt.Errorf("compiling %s task %q: %w", kind, task.Name, compileErr)
	}
	tracker.record.ResultTaskType = compiled.Type
	tracker.record.NeedsDiscovery = compiled.NeedsDiscovery
	tracker.record.Validated = compiled.Validated
	tracker.record.Reason = strings.TrimSpace(compiled.Reason)
	tracker.record.Steps = toDomainCompileSteps(compiled.Steps)
	task.Compiled = ""
	task.ScriptVerified, task.ScriptFailures, task.ScriptLastError, task.PrecompileNote = false, 0, "", ""
	script := strings.TrimSpace(compiled.Script)
	problem := ""
	if project && compiled.Type == "script" && script != "" && !compiled.NeedsDiscovery {
		problem = projectScriptPlaceholderProblem(script)
	}
	switch {
	case problem != "":
		tracker.record.Status = domain.TaskCompileStatusSkipped
		tracker.record.Script = script
		tracker.record.Reason = "Rejected: " + problem + "; the task stays a prompt task."
	case compiled.Type == "script" && script != "" && !compiled.NeedsDiscovery:
		task.Compiled = script
		tracker.record.Status = domain.TaskCompileStatusSucceeded
		tracker.record.Script = task.Compiled
	default:
		tracker.record.Status = domain.TaskCompileStatusSkipped
	}
	// A prompt that is not deterministic records its key too, so it is not
	// analyzed again on every run until something it depends on changes.
	task.CompiledKey = key
	persistProjectCompileRecord(tracker)
	if err := saveConfiguredTask(agentCfg, task); err != nil {
		return task.Compiled, fmt.Errorf("saving %s task %q's compiled script: %w", kind, task.Name, err)
	}
	slog.Info("task_compile: compiled "+kind+" task", "component", "task_compile", "agent", agentCfg.Name,
		"task", task.Name, "project", task.Project, "status", tracker.record.Status, "reason", tracker.record.Reason)
	return task.Compiled, nil
}

// TaskScriptResult is what recording a precompiled script run changed.
type TaskScriptResult struct {
	// StreakStarted is set when this failure started a run of failures: the
	// script's previous counted run completed, or it had not failed yet. A
	// streak notifies once, so later failures in it leave this unset.
	StreakStarted bool
	// Failures is the script's consecutive failed runs, and Cause the last
	// failure's short cause.
	Failures int
	Cause    string
	// Workflow is set when the task is a workflow.
	Workflow bool
}

// RecordTaskScriptResult records how a precompiled project task's, prompt
// task's or workflow's script run ended; runErr is nil when it completed.
func RecordTaskScriptResult(agentName, taskName string, runErr error) error {
	_, err := RecordTaskScriptOutcome(agentName, taskName, runErr)
	return err
}

// RecordTaskScriptOutcome records how a precompiled script run ended, for
// display only (#1568): a failed run counts toward the task's consecutive
// failures and records its cause, and a completed one resets them. It never
// turns precompile off or discards the compiled script; each failed run
// falls back to the model, and recompiling stays the user's Recompile. A
// transient failure, such as a rate limit or a network error, says nothing
// about the script and is not counted.
func RecordTaskScriptOutcome(agentName, taskName string, runErr error) (TaskScriptResult, error) {
	_, agentCfg, task, err := findConfiguredTask(agentName, taskName)
	if err != nil {
		return TaskScriptResult{}, err
	}
	result := TaskScriptResult{Failures: task.ScriptFailures, Cause: task.ScriptLastError, Workflow: task.IsWorkflow()}
	if strings.TrimSpace(task.Compiled) == "" {
		return result, nil
	}
	if runErr == nil {
		if task.ScriptVerified && task.ScriptFailures == 0 && task.ScriptLastError == "" {
			return result, nil
		}
		task.ScriptVerified, task.ScriptFailures, task.ScriptLastError = true, 0, ""
		result.Failures, result.Cause = 0, ""
		return result, saveConfiguredTask(agentCfg, task)
	}
	if scheduler.IsTransientScriptError(runErr) {
		slog.Info("task_compile: not counting a transient compiled script failure", "component", "task_compile", "agent", agentCfg.Name,
			"task", task.Name, "cause", scheduler.ProjectScriptFailureCause(runErr))
		return result, nil
	}
	task.ScriptFailures++
	task.ScriptLastError = scheduler.ProjectScriptFailureCause(runErr)
	result.Failures, result.Cause = task.ScriptFailures, task.ScriptLastError
	result.StreakStarted = task.ScriptFailures == 1
	slog.Warn("task_compile: compiled script failed; the run fell back to the model", "component", "task_compile", "agent", agentCfg.Name,
		"task", task.Name, "failures", task.ScriptFailures, "cause", task.ScriptLastError)
	return result, saveConfiguredTask(agentCfg, task)
}

func persistProjectCompileRecord(tracker *taskCompileTracker) {
	if err := tracker.persist(); err != nil {
		slog.Warn("task_compile: failed to persist compile record", "component", "task_compile", "agent", tracker.record.AgentID, "error", err)
	}
}

// saveConfiguredTask writes task back where it is defined, its markdown file
// or aviary.yaml, and reloads the running config.
func saveConfiguredTask(agentCfg config.AgentConfig, task config.TaskConfig) error {
	if task.FromFile {
		tasksDir := config.AgentTasksDir(agentCfg)
		oldPath, _ := config.FindMarkdownTask(tasksDir, task.Name)
		newPath, err := config.SaveMarkdownTask(tasksDir, task)
		if err != nil {
			return err
		}
		if oldPath != "" && oldPath != newPath {
			_ = os.Remove(oldPath)
		}
		return reloadTaskConfig()
	}
	saved, err := config.Load("")
	if err != nil {
		return err
	}
	found := false
	for i := range saved.Agents {
		// Tasks loaded from markdown files stay in their files.
		saved.Agents[i].Tasks = slices.DeleteFunc(saved.Agents[i].Tasks, func(t config.TaskConfig) bool { return t.FromFile })
		if saved.Agents[i].Name != agentCfg.Name {
			continue
		}
		for j := range saved.Agents[i].Tasks {
			if saved.Agents[i].Tasks[j].Name == task.Name {
				saved.Agents[i].Tasks[j] = task
				found = true
			}
		}
	}
	if !found {
		return fmt.Errorf("task %q is not in aviary.yaml", task.Name)
	}
	if err := config.Save("", saved); err != nil {
		return err
	}
	return reloadTaskConfig()
}
