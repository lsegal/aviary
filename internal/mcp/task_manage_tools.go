package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/schedule"
	"github.com/lsegal/aviary/internal/scheduler"
)

// taskSessionArgs names the task whose session task_session returns.
type taskSessionArgs struct {
	Name  string `json:"name"`
	Agent string `json:"agent,omitempty"`
}

// taskSessionDTO is a task's session, as task_session returns it.
type taskSessionDTO struct {
	ID           string `json:"id"`
	AgentID      string `json:"agent_id"`
	Name         string `json:"name"`
	Title        string `json:"title"`
	IsProcessing bool   `json:"is_processing"`
}

// taskUpdateArgs changes fields of an existing task. A nil field is left
// unchanged; an empty string clears it.
type taskUpdateArgs struct {
	Name     string  `json:"name"`
	Agent    string  `json:"agent,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"`
	Content  *string `json:"content,omitempty"`
	Target   *string `json:"target,omitempty"`
	Schedule *string `json:"schedule,omitempty"`
	Trigger  *string `json:"trigger,omitempty" schema:"enum=once|interval|daily|weekly|cron|message|"`
	Channel  *string `json:"channel,omitempty"`
	Entry    *string `json:"entry,omitempty"`
	Match    *string `json:"match,omitempty"`
	Every    *string `json:"every,omitempty"`
	At       *string `json:"at,omitempty"`
	Days     *string `json:"days,omitempty"`
	Timezone *string `json:"timezone,omitempty"`
	StartAt  *string `json:"start_at,omitempty"`
	RunOnce  *bool   `json:"run_once,omitempty"`
	Overlap  *string `json:"overlap,omitempty" schema:"enum=queue|skip|"`
	Missed   *string `json:"missed,omitempty" schema:"enum=skip|run_once|"`
	Watch    *string `json:"watch,omitempty"`
	Project  *string `json:"project,omitempty"`
	Model    *string `json:"model,omitempty"`
	// Precompile turns a prompt or project task's precompiled script on or
	// off.
	Precompile *bool `json:"precompile,omitempty"`
}

type taskDeleteArgs struct {
	Name  string `json:"name"`
	Agent string `json:"agent,omitempty"`
}

type taskPreviewArgs struct {
	Name     string `json:"name,omitempty"`
	Agent    string `json:"agent,omitempty"`
	Schedule string `json:"schedule,omitempty"`
	Count    int    `json:"count,omitempty"`
	taskTriggerArgs
}

type taskUpcomingArgs struct {
	Project string `json:"project,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	PerTask int    `json:"per_task,omitempty"`
}

type taskUpcomingRun struct {
	TaskID  string `json:"task_id"`
	Name    string `json:"name"`
	Project string `json:"project,omitempty"`
	At      string `json:"at"`
}

type taskPreviewResult struct {
	Description string   `json:"description,omitempty"`
	Timezone    string   `json:"timezone,omitempty"`
	Next        []string `json:"next"`
	// RunsPer30Days is how often a recurring trigger fires in 30 days; it is
	// omitted for once and run-once triggers.
	RunsPer30Days int `json:"runs_per_30_days,omitempty"`
}

func registerTaskManageTools(s *sdkmcp.Server) {
	registerTaskUpcomingTool(s)
	addTool(s, &sdkmcp.Tool{
		Name:        "task_update",
		Description: "Change fields of an existing scheduled task. name may be <agent>/<task> or a task name (add agent to disambiguate). Only the fields you pass change; pass an empty string to clear a field. Trigger fields: schedule (cron), trigger (once|interval|daily|weekly|cron), every, at (HH:MM), days (mon,wed), timezone (IANA), start_at, run_once, overlap (queue|skip), missed (skip|run_once). Project tasks also take project (watched owner/repo) and model (model spec). Prompt and project tasks take precompile (run a compiled script, in the clone for a project task, when the prompt is deterministic, falling back to the model if it fails); script tasks do not. Use enabled=false to pause a task without deleting it. Workflows (trigger=message) take channel, entry and match instead of a schedule, model, target \"reply\", and, for prompt workflows, precompile (run a compiled script on each matching message when the prompt is deterministic, falling back to the model).",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args taskUpdateArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		_, agentCfg, task, err := findConfiguredTask(args.Agent, args.Name)
		if err != nil {
			return nil, struct{}{}, err
		}
		set := func(dst *string, v *string) {
			if v != nil {
				*dst = strings.TrimSpace(*v)
			}
		}
		if args.Enabled != nil {
			enabled := *args.Enabled
			task.Enabled = &enabled
		}
		set(&task.Prompt, args.Content)
		set(&task.Target, args.Target)
		if args.Schedule != nil {
			task.Schedule = normalizeTaskSchedule(*args.Schedule)
		}
		set(&task.Trigger, args.Trigger)
		set(&task.Every, args.Every)
		set(&task.At, args.At)
		set(&task.Days, args.Days)
		set(&task.Timezone, args.Timezone)
		set(&task.StartAt, args.StartAt)
		set(&task.Overlap, args.Overlap)
		set(&task.Missed, args.Missed)
		set(&task.Watch, args.Watch)
		set(&task.Channel, args.Channel)
		set(&task.Entry, args.Entry)
		if args.Match != nil {
			task.Match = strings.TrimSpace(*args.Match)
		}
		set(&task.Project, args.Project)
		set(&task.Model, args.Model)
		if args.RunOnce != nil {
			task.RunOnce = *args.RunOnce
		}
		if args.Precompile != nil {
			if !task.PrecompilesBesidePrompt() {
				return nil, struct{}{}, fmt.Errorf("precompile only applies to prompt and project tasks, which compile their prompt into a script; %s tasks have no prompt to compile", task.Type)
			}
			precompile := *args.Precompile
			task.Precompile = &precompile
			if !precompile || task.Compiled == "" {
				// Turning it back on compiles a script that was set aside again.
				task.Compiled, task.CompiledKey = "", ""
			}
			// Turning precompile off or back on starts the script over.
			task.ScriptVerified, task.ScriptFailures, task.PrecompileNote = false, 0, ""
		}
		task.Trigger = strings.ToLower(task.Trigger)
		task.Overlap = strings.ToLower(task.Overlap)
		task.Missed = strings.ToLower(task.Missed)

		if _, err := config.ParseTaskTarget(task.Target); err != nil {
			return nil, struct{}{}, err
		}
		if strings.EqualFold(task.Type, "project") && task.Project == "" {
			return nil, struct{}{}, fmt.Errorf("project tasks require a project")
		}
		workflow := task.IsWorkflow()
		if !strings.EqualFold(task.Type, "project") && (task.Project != "" || (task.Model != "" && !workflow)) {
			return nil, struct{}{}, fmt.Errorf("project and model are only supported for project tasks and workflows")
		}
		if config.IsReplyTarget(task.Target) && !workflow {
			return nil, struct{}{}, fmt.Errorf("only workflows can reply to the conversation that triggered them")
		}
		fields := scheduler.TaskFields(task)
		switch {
		case workflow:
			if strings.EqualFold(task.Type, "project") {
				return nil, struct{}{}, fmt.Errorf("workflows run prompt or script content")
			}
			if agentCfg.ChannelByName(task.Channel) < 0 {
				return nil, struct{}{}, fmt.Errorf("workflows need one of agent %q's channels; %q is not one", agentCfg.Name, task.Channel)
			}
			if task.Watch != "" || task.Schedule != "" || task.Every != "" || task.At != "" || task.Days != "" || task.StartAt != "" || task.RunOnce || task.Missed != "" {
				return nil, struct{}{}, fmt.Errorf("workflows are triggered by messages and cannot also have a schedule or watch")
			}
			task.Channel = agentCfg.Channels[agentCfg.ChannelByName(task.Channel)].EffectiveName()
		case task.Channel != "" || task.Entry != "" || task.Match != "":
			return nil, struct{}{}, fmt.Errorf("channel, entry and match are only supported for workflows (trigger=message)")
		case fields.HasTrigger() && task.Watch != "":
			return nil, struct{}{}, fmt.Errorf("a task cannot have both a schedule trigger and watch")
		case fields.HasTrigger():
			if _, err := schedule.Parse(fields); err != nil {
				return nil, struct{}{}, fmt.Errorf("invalid schedule: %w", err)
			}
		case task.Watch == "":
			return nil, struct{}{}, fmt.Errorf("task needs a schedule trigger or watch")
		}
		tasksDir := config.AgentTasksDir(agentCfg)
		oldPath := ""
		if task.FromFile {
			oldPath, _ = config.FindMarkdownTask(tasksDir, task.Name)
		}
		newPath, err := config.SaveMarkdownTask(tasksDir, task)
		if err != nil {
			return nil, struct{}{}, err
		}
		if oldPath != "" && oldPath != newPath {
			_ = os.Remove(oldPath)
		}
		if err := reloadTaskConfig(); err != nil {
			return nil, struct{}{}, err
		}
		if EffectiveTaskPrecompile(task) {
			// Compile ahead of the next run, which would otherwise compile
			// first; a task whose inputs did not change keeps its script.
			compileTaskInBackground(agentCfg.Name, task.Name, "update")
		}
		return text(fmt.Sprintf("Task %q updated for agent %q.", task.Name, agentCfg.Name))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "task_delete",
		Description: "Delete a scheduled task definition and its run history. name may be <agent>/<task> or a task name (add agent to disambiguate). Running jobs are not stopped; use task_stop for that.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args taskDeleteArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		_, agentCfg, task, err := findConfiguredTask(args.Agent, args.Name)
		if err != nil {
			return nil, struct{}{}, err
		}
		if !task.FromFile {
			return nil, struct{}{}, fmt.Errorf("task %q is defined in aviary.yaml; remove it from agents[%s].tasks there", task.Name, agentCfg.Name)
		}
		path, err := config.FindMarkdownTask(config.AgentTasksDir(agentCfg), task.Name)
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := os.Remove(path); err != nil {
			return nil, struct{}{}, fmt.Errorf("deleting task file: %w", err)
		}
		if err := reloadTaskConfig(); err != nil {
			return nil, struct{}{}, err
		}
		if d := GetDeps(); d != nil && d.Scheduler != nil {
			d.Scheduler.Forget(agentCfg.Name + "/" + task.Name)
		}
		return text(fmt.Sprintf("Task %q deleted from agent %q.", task.Name, agentCfg.Name))
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "task_session",
		Description: "Get a scheduled task's chat session, which every run of the task shares, creating it when the task has not run yet. name may be <agent>/<task> or a task name (add agent to disambiguate). The session is titled with the task's name.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args taskSessionArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		_, agentCfg, task, err := findConfiguredTask(args.Agent, args.Name)
		if err != nil {
			return nil, struct{}{}, err
		}
		sess, err := agent.EnsureTaskSession(agentCfg.Name, task.Name)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(taskSessionDTO{
			ID:           sess.ID,
			AgentID:      agentCfg.Name,
			Name:         sess.Name,
			Title:        sess.Title,
			IsProcessing: agent.IsSessionProcessing(agentCfg.Name, sess.ID),
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "task_preview",
		Description: "Preview the next fires of a schedule (default 3, at most 20). Pass name (<agent>/<task> or a task name) for a saved task, or trigger fields to preview an unsaved schedule: schedule (cron), trigger, every, at, days, timezone, start_at, run_once.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args taskPreviewArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		count := args.Count
		if count <= 0 {
			count = 3
		}
		count = min(count, 20)
		var fields schedule.Fields
		if strings.TrimSpace(args.Name) != "" {
			_, _, task, err := findConfiguredTask(args.Agent, args.Name)
			if err != nil {
				return nil, struct{}{}, err
			}
			fields = scheduler.TaskFields(task)
		} else {
			fields = args.fields(normalizeTaskSchedule(args.Schedule))
		}
		if !fields.HasTrigger() {
			return nil, struct{}{}, fmt.Errorf("no schedule trigger to preview")
		}
		spec, err := schedule.Parse(fields)
		if err != nil {
			return nil, struct{}{}, err
		}
		var next []time.Time
		if d := GetDeps(); strings.TrimSpace(args.Name) != "" && d != nil && d.Scheduler != nil {
			// The scheduler knows whether a run-once task already fired.
			if fires, err := d.Scheduler.Preview(previewKey(args.Agent, args.Name), count); err == nil {
				next = fires
			} else {
				next = spec.Preview(time.Now(), count)
			}
		} else {
			next = spec.Preview(time.Now(), count)
		}
		out := taskPreviewResult{
			Description:   spec.Describe(),
			Timezone:      spec.Location.String(),
			Next:          []string{},
			RunsPer30Days: spec.RunsIn(time.Now(), 30*24*time.Hour),
		}
		for _, t := range next {
			out.Next = append(out.Next, t.In(spec.Location).Format(time.RFC3339))
		}
		return jsonResult(out)
	})
}

func registerTaskUpcomingTool(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "task_upcoming",
		Description: "List the next fires across enabled scheduled tasks, soonest first (default 5, at most 3 per task). project=<owner/repo> limits it to that project's scheduled prompts.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args taskUpcomingArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		d := GetDeps()
		if d == nil || d.Scheduler == nil {
			return nil, struct{}{}, fmt.Errorf("scheduler not initialized")
		}
		limit := args.Limit
		if limit <= 0 {
			limit = 5
		}
		perTask := args.PerTask
		if perTask <= 0 {
			perTask = 3
		}
		tasks := map[string]domain.ScheduledTask{}
		for _, task := range d.Scheduler.ListTasks() {
			tasks[task.ID] = task
		}
		project := strings.TrimSpace(args.Project)
		out := []taskUpcomingRun{}
		for _, run := range d.Scheduler.Upcoming(len(tasks)*perTask, perTask) {
			task := tasks[run.TaskID]
			if project != "" && task.Project != project {
				continue
			}
			out = append(out, taskUpcomingRun{TaskID: run.TaskID, Name: task.Name, Project: task.Project, At: run.At.Format(time.RFC3339)})
			if len(out) == limit {
				break
			}
		}
		return jsonResult(out)
	})
}

func previewKey(agentName, name string) string {
	if agentName = strings.TrimSpace(agentName); agentName != "" && !strings.Contains(name, "/") {
		return agentName + "/" + strings.TrimSpace(name)
	}
	return strings.TrimSpace(name)
}

// findConfiguredTask locates a task by "<agent>/<task>" or by task name,
// optionally restricted to agentName.
func findConfiguredTask(agentName, name string) (*config.Config, config.AgentConfig, config.TaskConfig, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, config.AgentConfig{}, config.TaskConfig{}, fmt.Errorf("name is required")
	}
	agentName = strings.TrimSpace(agentName)
	if a, t, ok := strings.Cut(name, "/"); ok && agentName == "" {
		agentName, name = a, t
	}
	cfg, err := config.Load("")
	if err != nil {
		return nil, config.AgentConfig{}, config.TaskConfig{}, err
	}
	var matches []string
	var foundAgent config.AgentConfig
	var foundTask config.TaskConfig
	for _, ac := range cfg.Agents {
		if agentName != "" && ac.Name != agentName {
			continue
		}
		for _, tc := range ac.Tasks {
			if tc.Name == name {
				matches = append(matches, ac.Name+"/"+tc.Name)
				foundAgent, foundTask = ac, tc
			}
		}
	}
	switch len(matches) {
	case 0:
		return nil, config.AgentConfig{}, config.TaskConfig{}, fmt.Errorf("task %q not found; use task_list to see tasks", name)
	case 1:
		return cfg, foundAgent, foundTask, nil
	default:
		return nil, config.AgentConfig{}, config.TaskConfig{}, fmt.Errorf("task name %q is ambiguous (%s); pass agent or <agent>/<task>", name, strings.Join(matches, ", "))
	}
}

// reloadTaskConfig reloads the config, including task files, and reconciles
// agents and the scheduler with it.
func reloadTaskConfig() error {
	cfg, err := config.Load("")
	if err != nil {
		return err
	}
	if d := GetDeps(); d != nil {
		if d.Agents != nil {
			d.Agents.Reconcile(cfg)
		}
		if d.Scheduler != nil {
			d.Scheduler.Reconcile(cfg)
		}
	}
	return nil
}
