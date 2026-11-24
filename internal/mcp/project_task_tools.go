package mcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/projects"
)

// The project_task_* tools let the Code Loop agent manage the scheduled
// tasks of the projects this instance watches. They save, validate, preview
// and run through the same code as task_schedule, task_update, task_delete,
// task_preview and task_run, so a task they create is the same as one the
// Scheduled Tasks UI creates; they only add the scoping to watched projects
// and the Code Loop log line of each change.

type projectTaskListArgs struct {
	Project string `json:"project,omitempty" jsonschema:"Only list this watched project's tasks (owner/repo); leave out for every watched project"`
}

type projectTaskNameArgs struct {
	Name    string `json:"name" jsonschema:"The task's name, or <agent>/<task> as project_task_list returns it in id"`
	Project string `json:"project,omitempty" jsonschema:"The task's watched project; needed only when tasks of several projects share the name"`
}

type projectTaskDeleteArgs struct {
	Name    string `json:"name" jsonschema:"The task's name, or <agent>/<task> as project_task_list returns it in id"`
	Project string `json:"project,omitempty" jsonschema:"The task's watched project; needed only when tasks of several projects share the name"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"Delete it. Leave out first: the call then only reports what deleting removes, so you can ask the user to confirm"`
}

// projectTaskTriggerArgs are a project task's schedule fields, as
// task_schedule takes them.
type projectTaskTriggerArgs struct {
	Schedule string `json:"schedule,omitempty" jsonschema:"A cron expression (5 fields, or 6 with leading seconds); use instead of trigger"`
	Trigger  string `json:"trigger,omitempty" jsonschema:"once, interval, daily, weekly or cron" schema:"enum=once|interval|daily|weekly|cron"`
	Every    string `json:"every,omitempty" jsonschema:"For trigger=interval, such as 30m, 2h or 1d"`
	At       string `json:"at,omitempty" jsonschema:"For trigger=daily or weekly, HH:MM; comma-separate several times"`
	Days     string `json:"days,omitempty" jsonschema:"For trigger=weekly, such as mon,wed"`
	Timezone string `json:"timezone,omitempty" jsonschema:"IANA zone the trigger is evaluated in; set it only when the user names one"`
	StartAt  string `json:"start_at,omitempty" jsonschema:"RFC 3339 time of the first fire; for trigger=once, its only fire"`
	RunOnce  bool   `json:"run_once,omitempty" jsonschema:"Fire once, at the first time the trigger matches"`
	Overlap  string `json:"overlap,omitempty" jsonschema:"queue (default) waits for a run in flight; skip drops the fire" schema:"enum=queue|skip"`
	Missed   string `json:"missed,omitempty" jsonschema:"skip (default) drops fires missed while Aviary was stopped; run_once runs one catch-up fire" schema:"enum=skip|run_once"`
}

func (a projectTaskTriggerArgs) taskTrigger() taskTriggerArgs {
	return taskTriggerArgs{
		Trigger: a.Trigger, Every: a.Every, At: a.At, Days: a.Days, Timezone: a.Timezone,
		StartAt: a.StartAt, RunOnce: a.RunOnce, Overlap: a.Overlap, Missed: a.Missed,
	}
}

type projectTaskCreateArgs struct {
	Project    string `json:"project" jsonschema:"The watched project (owner/repo) the task runs a Code Loop job on"`
	Name       string `json:"name,omitempty" jsonschema:"A short task name, such as weekly-seo; generated from the prompt when left out"`
	Content    string `json:"content" jsonschema:"The prompt each run gives the project's agent in a fresh clone of the project"`
	Model      string `json:"model,omitempty" jsonschema:"Model override for its runs, such as claude/opus; leave out for the project's agent's model"`
	SendTo     string `json:"send_to,omitempty" jsonschema:"Where each run's output goes: leave out for default routing, or silent, session:<name>, channel:<channel name>, or codeloop:<project>#<number>"`
	Precompile *bool  `json:"precompile,omitempty" jsonschema:"Compile a script beside the prompt that runs instead of the model while the prompt is deterministic; defaults to scheduler.precompute_tasks"`
	DryRun     bool   `json:"dry_run,omitempty" jsonschema:"Validate and preview the task without saving it"`
	projectTaskTriggerArgs
}

type projectTaskUpdateArgs struct {
	Name       string  `json:"name" jsonschema:"The task's name, or <agent>/<task> as project_task_list returns it in id"`
	Project    string  `json:"project,omitempty" jsonschema:"The task's watched project; needed only when tasks of several projects share the name"`
	NewName    *string `json:"new_name,omitempty" jsonschema:"Rename the task"`
	NewProject *string `json:"new_project,omitempty" jsonschema:"Move the task to another watched project"`
	Content    *string `json:"content,omitempty" jsonschema:"The new prompt"`
	Model      *string `json:"model,omitempty" jsonschema:"The new model override; empty uses the project's agent's model"`
	SendTo     *string `json:"send_to,omitempty" jsonschema:"Where each run's output goes; empty for default routing"`
	Precompile *bool   `json:"precompile,omitempty" jsonschema:"Turn the precompiled script on or off"`
	Enabled    *bool   `json:"enabled,omitempty" jsonschema:"false pauses the task, true resumes it"`
	Schedule   *string `json:"schedule,omitempty" jsonschema:"A cron expression; empty clears it"`
	Trigger    *string `json:"trigger,omitempty" jsonschema:"once, interval, daily, weekly or cron; empty clears it" schema:"enum=once|interval|daily|weekly|cron|"`
	Every      *string `json:"every,omitempty"`
	At         *string `json:"at,omitempty"`
	Days       *string `json:"days,omitempty"`
	Timezone   *string `json:"timezone,omitempty"`
	StartAt    *string `json:"start_at,omitempty"`
	RunOnce    *bool   `json:"run_once,omitempty"`
	Overlap    *string `json:"overlap,omitempty" schema:"enum=queue|skip|"`
	Missed     *string `json:"missed,omitempty" schema:"enum=skip|run_once|"`
}

// projectTask is a project scheduled task found for a project_task_* call.
type projectTask struct {
	agent config.AgentConfig
	task  config.TaskConfig
}

func (t projectTask) id() string { return t.agent.Name + "/" + t.task.Name }

// watchedProjects returns the projects the Code Loop agent may manage tasks
// of, each mapped to the agent its tasks belong to.
func watchedProjects() (*projects.Manager, map[string]string, error) {
	m, err := projectManager()
	if err != nil {
		return nil, nil, err
	}
	return m, m.WatchedProjects(), nil
}

// requireWatched refuses a project this instance does not watch.
func requireWatched(watched map[string]string, project string) error {
	if project == "" {
		return errors.New("project is required")
	}
	if _, ok := watched[project]; !ok {
		return fmt.Errorf("%s is not a project this instance watches, so its scheduled tasks are not managed here", project)
	}
	return nil
}

// projectTasks returns every scheduled task of a watched project, or only
// project's when it is set.
func projectTasks(cfg *config.Config, watched map[string]string, project string) []projectTask {
	var out []projectTask
	for _, ac := range cfg.Agents {
		for _, tc := range ac.Tasks {
			if !strings.EqualFold(tc.Type, "project") {
				continue
			}
			if _, ok := watched[tc.Project]; !ok || (project != "" && tc.Project != project) {
				continue
			}
			out = append(out, projectTask{agent: ac, task: tc})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].task.Project != out[j].task.Project {
			return out[i].task.Project < out[j].task.Project
		}
		return out[i].id() < out[j].id()
	})
	return out
}

// findProjectTask finds the watched project's task name names.
func findProjectTask(watched map[string]string, name, project string) (projectTask, error) {
	name, project = strings.TrimSpace(name), strings.TrimSpace(project)
	if name == "" {
		return projectTask{}, errors.New("name is required")
	}
	if project != "" {
		if err := requireWatched(watched, project); err != nil {
			return projectTask{}, err
		}
	}
	cfg, err := config.Load("")
	if err != nil {
		return projectTask{}, err
	}
	agentName, taskName, qualified := strings.Cut(name, "/")
	var matches []projectTask
	for _, t := range projectTasks(cfg, watched, project) {
		if t.task.Name == name || (qualified && t.agent.Name == agentName && t.task.Name == taskName) {
			matches = append(matches, t)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return projectTask{}, fmt.Errorf("no scheduled task %q on a watched project; project_task_list lists them", name)
	}
	ids := make([]string, len(matches))
	for i, t := range matches {
		ids[i] = t.id() + " (" + t.task.Project + ")"
	}
	return projectTask{}, fmt.Errorf("several watched projects have a task %q (%s); pass project or <agent>/<task>", name, strings.Join(ids, ", "))
}

// scheduledTask returns the scheduler's view of the task with id, which
// carries its trigger summary, next run and run history.
func scheduledTask(id string) (domain.ScheduledTask, bool) {
	d := GetDeps()
	if d == nil || d.Scheduler == nil {
		return domain.ScheduledTask{}, false
	}
	for _, task := range d.Scheduler.ListTasks() {
		if task.ID == id {
			return task, true
		}
	}
	return domain.ScheduledTask{}, false
}

// projectTaskSource says where a task is defined: a Markdown task file in
// its agent's tasks directory, or aviary.yaml.
func projectTaskSource(t projectTask) map[string]any {
	if t.task.FromFile {
		path, _ := config.FindMarkdownTask(config.AgentTasksDir(t.agent), t.task.Name)
		return map[string]any{"kind": "file", "path": path}
	}
	return map[string]any{"kind": "config", "path": config.DefaultPath()}
}

// projectTaskEntry is a task as project_task_list returns it; full adds
// project_task_get's whole definition and run history.
func projectTaskEntry(t projectTask, full bool) map[string]any {
	tc := t.task
	enabled := config.BoolOr(tc.Enabled, true)
	entry := map[string]any{
		"id":      t.id(),
		"name":    tc.Name,
		"agent":   t.agent.Name,
		"project": tc.Project,
		"enabled": enabled,
		"paused":  !enabled,
		"precompile": map[string]any{
			"enabled":  EffectiveTaskPrecompile(tc),
			"compiled": tc.Compiled != "",
			"verified": tc.ScriptVerified,
			"failures": tc.ScriptFailures,
			"note":     tc.PrecompileNote,
		},
		"source": projectTaskSource(t),
	}
	sched, ok := scheduledTask(t.id())
	if ok {
		entry["trigger"] = sched.Description
		if sched.NextRun != nil && enabled {
			entry["next_run"] = sched.NextRun.Format(time.RFC3339)
		}
		if n := len(sched.History); n > 0 {
			entry["last_run"] = sched.History[n-1]
		}
	}
	if !full {
		return entry
	}
	sendTo := tc.Target
	if sendTo == "" {
		sendTo = "default routing"
	}
	entry["type"] = "project"
	entry["prompt"] = tc.Prompt
	entry["model"] = tc.Model
	entry["send_to"] = sendTo
	entry["trigger_fields"] = map[string]any{
		"schedule": tc.Schedule, "trigger": tc.Trigger, "every": tc.Every, "at": tc.At,
		"days": tc.Days, "timezone": tc.Timezone, "start_at": tc.StartAt, "run_once": tc.RunOnce,
		"overlap": firstNonEmpty(tc.Overlap, "queue"), "missed": firstNonEmpty(tc.Missed, "skip"),
	}
	if enabled {
		if preview, err := previewTask(taskPreviewArgs{Name: t.id()}); err == nil {
			entry["next_runs"] = preview.Next
			entry["runs_per_30_days"] = preview.RunsPer30Days
			entry["timezone"] = preview.Timezone
		}
	}
	history := slices.Clone(sched.History)
	slices.Reverse(history)
	if history == nil {
		history = []domain.TaskRun{}
	}
	entry["recent_runs"] = history
	return entry
}

// resultText is the text of a tool result.
func resultText(result *sdkmcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	var parts []string
	for _, c := range result.Content {
		if t, ok := c.(*sdkmcp.TextContent); ok {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// projectTaskEstimate is the determinism verdict and per-run token and cost
// estimate task_analyze gives the Scheduled Tasks editor for a prompt, or
// the reason it is unavailable.
func projectTaskEstimate(ctx context.Context, agentName, prompt, project, model, sendTo string, precompile bool) map[string]any {
	analysis, err := analyzeTaskForEditor(ctx, taskAnalyzeArgs{
		Agent: agentName, Content: prompt, Type: "project", Project: project,
		Model: model, Target: sendTo, Precompile: precompile, Editor: "projects-chat",
	})
	if err != nil {
		return map[string]any{"available": false, "reason": err.Error()}
	}
	return map[string]any{
		"available":          analysis.Estimate.Available,
		"verdict":            analysis.Verdict,
		"will_precompile":    analysis.WillPrecompile,
		"reason":             analysis.Reason,
		"model":              analysis.Model,
		"per_run":            analysis.Estimate,
		"without_precompile": analysis.WithoutPrecompile,
	}
}

// projectTaskPreview is the plain-language schedule and next runs of a
// trigger, or the reason it does not parse.
func projectTaskPreview(args taskPreviewArgs) (map[string]any, error) {
	args.Count = 3
	preview, err := previewTask(args)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"schedule":         preview.Description,
		"timezone":         preview.Timezone,
		"next_runs":        preview.Next,
		"runs_per_30_days": preview.RunsPer30Days,
	}, nil
}

func createProjectTask(ctx context.Context, args projectTaskCreateArgs) (map[string]any, error) {
	m, watched, err := watchedProjects()
	if err != nil {
		return nil, err
	}
	project := strings.TrimSpace(args.Project)
	if err := requireWatched(watched, project); err != nil {
		return nil, err
	}
	agentName := watched[project]
	if agentName == "" {
		return nil, fmt.Errorf("%s is a runtime watch no agent owns, and a scheduled task belongs to an agent; save the project to an agent's projects first", project)
	}
	if strings.TrimSpace(args.Content) == "" {
		return nil, errors.New("content is required")
	}
	trigger := args.taskTrigger()
	schedule := normalizeTaskSchedule(args.Schedule)
	if !trigger.fields(schedule).HasTrigger() {
		return nil, errors.New("project tasks need a schedule trigger: schedule, or trigger with its fields")
	}
	preview, err := projectTaskPreview(taskPreviewArgs{Schedule: schedule, taskTriggerArgs: trigger})
	if err != nil {
		return nil, fmt.Errorf("invalid schedule: %w", err)
	}
	cfg, err := config.Load("")
	if err != nil {
		return nil, err
	}
	precompile := config.EffectivePrecomputeTasks(cfg.Scheduler)
	if args.Precompile != nil {
		precompile = *args.Precompile
	}
	sendTo := normalizeTaskTarget(args.SendTo)
	result := map[string]any{
		"project":  project,
		"agent":    agentName,
		"preview":  preview,
		"estimate": projectTaskEstimate(ctx, agentName, args.Content, project, args.Model, sendTo, precompile),
	}
	if args.DryRun {
		result["saved"] = false
		return result, nil
	}
	name := strings.TrimSpace(args.Name)
	if name != "" && slices.ContainsFunc(cfg.Agents, func(a config.AgentConfig) bool {
		return a.Name == agentName && slices.ContainsFunc(a.Tasks, func(t config.TaskConfig) bool { return t.Name == name })
	}) {
		return nil, fmt.Errorf("agent %q already has a task named %q; pick another name or update that task with project_task_update", agentName, name)
	}
	before := map[string]bool{}
	for _, t := range projectTasks(cfg, watched, project) {
		before[t.id()] = true
	}
	out, _, err := scheduleTask(ctx, nil, taskScheduleArgs{
		Agent: agentName, Name: name, Type: "project", Project: project, Model: args.Model,
		Content: args.Content, Schedule: args.Schedule, Target: args.SendTo, Precompile: &precompile,
		taskTriggerArgs: trigger,
	})
	if err != nil {
		return nil, err
	}
	created, err := config.Load("")
	if err != nil {
		return nil, err
	}
	var task projectTask
	for _, t := range projectTasks(created, watched, project) {
		if (name != "" && t.agent.Name == agentName && t.task.Name == name) || (name == "" && !before[t.id()]) {
			task = t
		}
	}
	if task.task.Name == "" {
		return nil, fmt.Errorf("the task was saved but cannot be found again: %s", resultText(out))
	}
	m.LogTaskChange(project, task.task.Name, "created")
	result["saved"] = true
	result["message"] = resultText(out)
	result["task"] = projectTaskEntry(task, false)
	return result, nil
}

func updateProjectTask(args projectTaskUpdateArgs, change string) (map[string]any, error) {
	m, watched, err := watchedProjects()
	if err != nil {
		return nil, err
	}
	t, err := findProjectTask(watched, args.Name, args.Project)
	if err != nil {
		return nil, err
	}
	if args.NewProject != nil {
		project := strings.TrimSpace(*args.NewProject)
		if err := requireWatched(watched, project); err != nil {
			return nil, err
		}
		if watched[project] != t.agent.Name {
			return nil, fmt.Errorf("%s belongs to agent %q, not %q, which owns this task; create the task on %s with project_task_create and delete this one instead", project, watched[project], t.agent.Name, project)
		}
	}
	source := projectTaskSource(t)
	update := taskUpdateArgs{
		Name: t.task.Name, Agent: t.agent.Name, Enabled: args.Enabled, Content: args.Content,
		Target: args.SendTo, Schedule: args.Schedule, Trigger: args.Trigger, Every: args.Every,
		At: args.At, Days: args.Days, Timezone: args.Timezone, StartAt: args.StartAt,
		RunOnce: args.RunOnce, Overlap: args.Overlap, Missed: args.Missed,
		Project: args.NewProject, Model: args.Model, Precompile: args.Precompile,
	}
	if _, _, err := updateTask(context.Background(), nil, update); err != nil {
		return nil, err
	}
	name := t.task.Name
	if args.NewName != nil && strings.TrimSpace(*args.NewName) != name {
		newName := strings.TrimSpace(*args.NewName)
		if _, _, err := renameTask(context.Background(), nil, configTaskRenameArgs{Agent: t.agent.Name, Task: name, NewName: newName}); err != nil {
			return nil, fmt.Errorf("the other changes were saved, but renaming failed: %w", err)
		}
		if err := reloadTaskConfig(); err != nil {
			return nil, err
		}
		name = newName
	}
	updated, err := findProjectTask(watched, t.agent.Name+"/"+name, "")
	if err != nil {
		return nil, err
	}
	m.LogTaskChange(updated.task.Project, name, change)
	edit := fmt.Sprintf("edited its task file %s directly", projectTaskSource(updated)["path"])
	if source["kind"] == "config" {
		edit = fmt.Sprintf("saved it to the task file %s, which now replaces its definition in %s", projectTaskSource(updated)["path"], source["path"])
	}
	return map[string]any{
		"ok":     true,
		"change": change,
		"edit":   edit,
		"task":   projectTaskEntry(updated, false),
	}, nil
}

func registerProjectTaskTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "project_task_list",
		Description: "List the scheduled tasks of the projects this instance watches, or of one: each with its id, name, agent, project, enabled and paused, trigger (plain-language schedule), next_run, last_run (fire time, status and summary), precompile state, and source (kind file, a Markdown task file, or config, aviary.yaml, with its path)",
		Annotations: readOnlyTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectTaskListArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			watched := m.WatchedProjects()
			project := strings.TrimSpace(args.Project)
			if project != "" {
				if err := requireWatched(watched, project); err != nil {
					return nil, err
				}
			}
			cfg, err := config.Load("")
			if err != nil {
				return nil, err
			}
			tasks := []map[string]any{}
			for _, t := range projectTasks(cfg, watched, project) {
				tasks = append(tasks, projectTaskEntry(t, false))
			}
			return map[string]any{"tasks": tasks, "total": len(tasks)}, nil
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_task_get",
		Description: "One scheduled task of a watched project: everything project_task_list gives, plus its prompt, model override, send_to, trigger_fields (schedule, trigger, every, at, days, timezone, start_at, run_once, overlap and missed policies), next_runs, and recent_runs, newest first",
		Annotations: readOnlyTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectTaskNameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			t, err := findProjectTask(m.WatchedProjects(), args.Name, args.Project)
			if err != nil {
				return nil, err
			}
			return projectTaskEntry(t, true), nil
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_task_create",
		Description: "Create a scheduled task on a watched project, saved as the Scheduled Tasks UI saves one, under the agent that owns the project: each run gives content to that agent as a Code Loop job in a fresh clone. Needs a schedule trigger: schedule (cron), or trigger=interval with every, daily with at, weekly with days and at, or once with start_at. The trigger is validated as task_schedule validates it, and the result previews it: schedule (plain language), next_runs and runs_per_30_days, plus estimate, the determinism verdict and per-run token and cost estimate. dry_run=true returns the preview without saving",
		Annotations: mutatingTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectTaskCreateArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		value, err := createProjectTask(ctx, args)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(value)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_task_update",
		Description: "Change fields of a watched project's scheduled task through task_update, validated the same way: only the fields passed change, and an empty string clears one. new_name renames it, new_project moves it to another watched project of the same agent, enabled pauses or resumes it. The result's edit says how the change was saved: a Markdown task file is edited directly; a task defined in aviary.yaml is saved to a task file that replaces that definition",
		Annotations: mutatingTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectTaskUpdateArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		value, err := updateProjectTask(args, "updated")
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(value)
	})

	for _, action := range []struct {
		name, change, description string
		enabled                   bool
	}{
		{"project_task_pause", "paused", "Pause a watched project's scheduled task, as the Scheduled Tasks UI's pause does: it stops firing until resumed. A run already in flight keeps going; pause it with project_job_pause", false},
		{"project_task_resume", "resumed", "Resume a paused scheduled task of a watched project, as the Scheduled Tasks UI's resume does", true},
	} {
		addTool(s, &sdkmcp.Tool{
			Name:        action.name,
			Description: action.description,
			Annotations: mutatingTool,
		}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectTaskNameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
			enabled := action.enabled
			value, err := updateProjectTask(projectTaskUpdateArgs{Name: args.Name, Project: args.Project, Enabled: &enabled}, action.change)
			if err != nil {
				return nil, struct{}{}, err
			}
			return jsonResult(value)
		})
	}

	addTool(s, &sdkmcp.Tool{
		Name:        "project_task_run",
		Description: "Run a watched project's scheduled task now, as task_run and the Scheduled Tasks UI's Run now do: the run lands on the board and in the task's history like any fire",
		Annotations: mutatingTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectTaskNameArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			t, err := findProjectTask(m.WatchedProjects(), args.Name, args.Project)
			if err != nil {
				return nil, err
			}
			d := GetDeps()
			if d.Scheduler == nil {
				return nil, errors.New("scheduler not initialized")
			}
			job, err := d.Scheduler.Trigger(t.id())
			if err != nil {
				return nil, err
			}
			m.LogTaskChange(t.task.Project, t.task.Name, "run now")
			return map[string]any{"ok": true, "task": t.id(), "project": t.task.Project, "task_job": job}, nil
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_task_delete",
		Description: "Delete a watched project's scheduled task through task_delete: its definition and its run history. Without confirm it deletes nothing and returns what would be removed, to show the user before asking them to confirm; call it again with confirm=true only after they do. A task defined in aviary.yaml is refused, as task_delete refuses it. A run already in flight is not stopped",
		Annotations: destructiveTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectTaskDeleteArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			t, err := findProjectTask(m.WatchedProjects(), args.Name, args.Project)
			if err != nil {
				return nil, err
			}
			source := projectTaskSource(t)
			runs := 0
			if sched, ok := scheduledTask(t.id()); ok {
				runs = len(sched.History)
			}
			removes := fmt.Sprintf("the scheduled task %s on %s (%s), its definition in %s, and its run history of %d runs", t.task.Name, t.task.Project, t.id(), source["path"], runs)
			if !args.Confirm {
				return map[string]any{
					"deleted":               false,
					"confirmation_required": true,
					"removes":               removes,
					"task":                  projectTaskEntry(t, false),
				}, nil
			}
			out, _, err := deleteTask(context.Background(), nil, taskDeleteArgs{Name: t.task.Name, Agent: t.agent.Name})
			if err != nil {
				return nil, err
			}
			m.LogTaskChange(t.task.Project, t.task.Name, "deleted")
			return map[string]any{"deleted": true, "removed": removes, "message": resultText(out)}, nil
		})
	})
}
