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
// Unlike a prompt task, which becomes a script task once compiled, a project
// task keeps its prompt, so a run whose script fails falls back to it.

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
		"`gh release view --repo " + project + " --json tagName --jq .tagName`."
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

// projectTaskCompileKey identifies what a project task's script is compiled
// from: its prompt, its project, and the tools it may call, including the
// commands exec allows, so a change to any of them recompiles it.
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
	for _, part := range []string{strings.TrimSpace(prompt), strings.TrimSpace(project), strings.Join(names, ","), strings.Join(commands, "\n")} {
		sum.Write([]byte(part))
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))[:16]
}

// EffectiveProjectTaskPrecompile reports whether a project task precompiles.
// A task that never chose stays off, so existing tasks do not start
// compiling; editors and task_schedule write the choice explicitly.
func EffectiveProjectTaskPrecompile(task config.TaskConfig) bool {
	return strings.EqualFold(task.Type, "project") && config.BoolOr(task.Precompile, false)
}

// backgroundCompiles tracks compiles started by compileProjectTaskInBackground,
// so tests can wait for them before restoring the state they read.
var backgroundCompiles sync.WaitGroup

// compileProjectTaskInBackground compiles a project task without blocking
// the caller, logging a failure.
func compileProjectTaskInBackground(agentName, taskName, trigger string) {
	backgroundCompiles.Go(func() {
		if _, err := EnsureProjectTaskCompiled(context.Background(), agentName, taskName, trigger); err != nil {
			slog.Warn("task_compile: project task compile failed", "component", "task_compile", "agent", agentName, "task", taskName, "error", err)
		}
	})
}

// EnsureProjectTaskCompiled returns the compiled script a precompiled project
// task runs, compiling it first when its prompt, project or tools changed
// since the last compile. It returns "" when the task does not precompile or
// its prompt did not pass the determinism gate, so the run uses the model.
func EnsureProjectTaskCompiled(ctx context.Context, agentName, taskName, trigger string) (string, error) {
	_, agentCfg, task, err := findConfiguredTask(agentName, taskName)
	if err != nil {
		return "", err
	}
	if !EffectiveProjectTaskPrecompile(task) {
		return "", nil
	}
	// The script is compiled against the tools it runs with.
	ctx = agent.WithProjectTaskScript(ctx)
	tools, err := resolveCompileTools()(ctx, agentCfg.Name)
	if err != nil {
		return "", fmt.Errorf("listing agent %q's tools: %w", agentCfg.Name, err)
	}
	key := projectTaskCompileKey(task.Prompt, task.Project, tools, &agentCfg)
	if key == task.CompiledKey {
		return strings.TrimSpace(task.Compiled), nil
	}
	tracker := newTaskCompileTracker(agentCfg.Name, task.Name, "project", task.Prompt, task.Target, trigger, false)
	compiled, compileErr := resolveTryCompileTaskPrompt()(withTaskCompileTracker(ctx, tracker), agentCfg.Name, projectTaskCompilePrompt(task.Prompt, task.Project), task.Target, false)
	if compileErr != nil {
		tracker.record.Status = domain.TaskCompileStatusFailed
		tracker.record.Reason = compileErr.Error()
		persistProjectCompileRecord(tracker)
		// The key is left as it was, so the next run tries again.
		return "", fmt.Errorf("compiling project task %q: %w", task.Name, compileErr)
	}
	tracker.record.ResultTaskType = compiled.Type
	tracker.record.NeedsDiscovery = compiled.NeedsDiscovery
	tracker.record.Validated = compiled.Validated
	tracker.record.Reason = strings.TrimSpace(compiled.Reason)
	tracker.record.Steps = toDomainCompileSteps(compiled.Steps)
	task.Compiled = ""
	task.ScriptVerified, task.ScriptFailures, task.PrecompileNote = false, 0, ""
	script := strings.TrimSpace(compiled.Script)
	problem := ""
	if compiled.Type == "script" && script != "" && !compiled.NeedsDiscovery {
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
		return task.Compiled, fmt.Errorf("saving project task %q's compiled script: %w", task.Name, err)
	}
	slog.Info("task_compile: compiled project task", "component", "task_compile", "agent", agentCfg.Name,
		"task", task.Name, "project", task.Project, "status", tracker.record.Status, "reason", tracker.record.Reason)
	return task.Compiled, nil
}

// projectScriptFailureLimit is how many runs in a row a compiled project task
// script that has completed before may fail before precompile is turned off,
// so a broken script does not cost a script run and a model run every fire.
const projectScriptFailureLimit = 3

// RecordProjectTaskScriptResult records how a precompiled project task's
// script run ended; runErr is nil when it completed. A script that fails
// before it has ever completed is set aside at once and the task runs on its
// prompt, since the compile gate could not catch the failure; one that has
// completed before is given projectScriptFailureLimit runs in a row before
// precompile is turned off. Either way PrecompileNote says what happened.
func RecordProjectTaskScriptResult(agentName, taskName string, runErr error) error {
	_, agentCfg, task, err := findConfiguredTask(agentName, taskName)
	if err != nil {
		return err
	}
	if strings.TrimSpace(task.Compiled) == "" {
		return nil
	}
	if runErr == nil {
		if task.ScriptVerified && task.ScriptFailures == 0 {
			return nil
		}
		task.ScriptVerified, task.ScriptFailures = true, 0
		return saveConfiguredTask(agentCfg, task)
	}
	cause := scheduler.ProjectScriptFailureCause(runErr)
	task.ScriptFailures++
	switch {
	case !task.ScriptVerified:
		// The key stays, so the same prompt is not compiled again.
		task.Compiled = ""
		task.PrecompileNote = "The compiled script failed on its first run (" + cause + "), so it was set aside and the task runs on the model. Turn precompile off and on to compile it again."
	case task.ScriptFailures >= projectScriptFailureLimit:
		off := false
		task.Precompile = &off
		task.Compiled, task.CompiledKey = "", ""
		task.PrecompileNote = fmt.Sprintf("Precompile was turned off after the compiled script failed %d runs in a row (last: %s), so the task runs on the model.", task.ScriptFailures, cause)
	}
	if task.PrecompileNote != "" {
		slog.Warn("task_compile: set aside a failing project task script", "component", "task_compile", "agent", agentCfg.Name,
			"task", task.Name, "failures", task.ScriptFailures, "note", task.PrecompileNote)
	}
	return saveConfiguredTask(agentCfg, task)
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
