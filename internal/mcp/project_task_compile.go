package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
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
// compiled as: the prompt, plus where its script runs.
func projectTaskCompilePrompt(prompt, project string) string {
	return strings.TrimSpace(prompt) + "\n\n" +
		"This task runs on a timer against the GitHub repository " + strings.TrimSpace(project) +
		", inside a fresh clone of it. exec runs commands in that clone by default, " +
		"with gh authenticated, GH_REPO set to the repository, and AVIARY_CHECKOUT_DIRECTORY set to the clone's path."
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
	if compiled.Type == "script" && strings.TrimSpace(compiled.Script) != "" && !compiled.NeedsDiscovery {
		task.Compiled = strings.TrimSpace(compiled.Script)
		tracker.record.Status = domain.TaskCompileStatusSucceeded
		tracker.record.Script = task.Compiled
	} else {
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
