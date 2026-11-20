package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects"
)

var (
	readOnlyTool    = &sdkmcp.ToolAnnotations{ReadOnlyHint: true}
	destructiveTrue = true
	destructiveTool = &sdkmcp.ToolAnnotations{DestructiveHint: &destructiveTrue}
	nonDestructive  = false
	mutatingTool    = &sdkmcp.ToolAnnotations{DestructiveHint: &nonDestructive}
)

type projectStatusArgs struct {
	LogOffset int `json:"log_offset,omitempty" jsonschema:"Return only the log lines logged after this offset, as returned in log_offset by an earlier call"`
}

type projectJobListArgs struct {
	Target string `json:"target,omitempty" jsonschema:"Only list jobs of this project"`
	Status string `json:"status,omitempty" jsonschema:"Only list jobs with this status, such as active, waiting, queued, paused, failed or complete"`
	Query  string `json:"query,omitempty" jsonschema:"Only list jobs with this text, case-insensitively, in any field: number, pull request, title, repository, status, skill, agent, session, schedule, or output"`
	Offset int    `json:"offset,omitempty" jsonschema:"Index of the first job to return"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Most jobs to return; defaults to 50"`
}

type projectJobArgs struct {
	Number int    `json:"number" jsonschema:"The job's issue or Discussion number"`
	Target string `json:"target,omitempty" jsonschema:"The job's project; needed only when several projects have a job with this number"`
}

type projectJobGetArgs struct {
	Number int    `json:"number" jsonschema:"The job's issue or Discussion number"`
	Target string `json:"target,omitempty" jsonschema:"The job's project; needed only when several projects have a job with this number"`
	Lines  int    `json:"lines,omitempty" jsonschema:"Scrollback lines to return; defaults to 200, negative returns all"`
	Events int    `json:"events,omitempty" jsonschema:"Transcript events to return; defaults to 200, negative returns all"`
}

type projectTargetArgs struct {
	Target string `json:"target,omitempty" jsonschema:"One project; leave out for all of them"`
}

type projectAddArgs struct {
	Target  string `json:"target" jsonschema:"owner/repo, a repository URL, a GitHub Project board, or a Discussions board"`
	Agent   string `json:"agent,omitempty" jsonschema:"The Aviary agent that owns the project; it lands in that agent's projects and its model and fallbacks are the provider and model defaults of the project's jobs. Required unless runtime is set"`
	Runtime bool   `json:"runtime,omitempty" jsonschema:"Watch until the server stops without writing aviary.yaml, as aviary watch does"`
	// Reports opts a saved project in to job reports.
	Reports *config.ProjectReportsConfig `json:"reports,omitempty" jsonschema:"Send this project's job events (merged, failed, waiting) through an Aviary agent's channels; leave out for no reports"`
	// Settings overrides the agents' projects settings for a runtime watch.
	Settings *config.ProjectsConfig `json:"settings,omitempty" jsonschema:"For a runtime watch, projects settings that override aviary.yaml while it runs"`
}

type projectRemoveArgs struct {
	Target  string `json:"target" jsonschema:"The project to stop watching"`
	Runtime bool   `json:"runtime,omitempty" jsonschema:"Only stop a runtime watch, leaving aviary.yaml alone"`
}

type projectUnblockArgs struct {
	Number int    `json:"number,omitempty" jsonschema:"One job's issue number; leave out to cover every stuck job"`
	Target string `json:"target,omitempty" jsonschema:"The job's project"`
}

type projectIssueCommentArgs struct {
	Number     int    `json:"number" jsonschema:"The issue or pull request number"`
	Repository string `json:"repository,omitempty" jsonschema:"OWNER/REPO hosting it; defaults to the repository of the job with this number, or the only watched repository"`
	Text       string `json:"text" jsonschema:"The update to post"`
}

type projectIssueCreateArgs struct {
	Repository string `json:"repository,omitempty" jsonschema:"OWNER/REPO to file the issue on; defaults to the only watched repository"`
	Text       string `json:"text" jsonschema:"What the issue is about, as you would describe it to gh-create"`
	Model      string `json:"model,omitempty" jsonschema:"Model for the CLI provider that runs gh-create"`
}

type projectGitHubImportArgs struct {
	Profile string `json:"profile" jsonschema:"Path of an Aviary browser profile directory signed in to GitHub on another machine"`
}

func projectManager() (*projects.Manager, error) {
	if m := GetDeps().Projects; m != nil {
		return m, nil
	}
	return nil, errors.New("projects are not available: start the Aviary server")
}

// projectResult runs fn against the manager and returns its value as JSON.
func projectResult[T any](tool string, fn func(*projects.Manager) (T, error)) (*sdkmcp.CallToolResult, struct{}, error) {
	slog.Info("mcp: tool call", "component", "projects", "tool", tool)
	m, err := projectManager()
	if err != nil {
		return nil, struct{}{}, err
	}
	value, err := fn(m)
	if err != nil {
		return nil, struct{}{}, err
	}
	return jsonResult(value)
}

// projectsAgentContext is the live context a request to the Projects agent
// carries: this instance, its targets, the board project the chat was opened
// from, and a snapshot of its jobs. Other agents get none.
func projectsAgentContext(ctx context.Context, agentName, project string) string {
	if agentName != config.ProjectsAgentName {
		return ""
	}
	m, err := projectManager()
	if err != nil {
		return ""
	}
	return m.ChatContext(ctx, project)
}

// retryScheduledJob fires a scheduled Projects job's task again through
// core's scheduler, so the rerun lands in the task's history like any fire.
func retryScheduledJob(taskID string) (map[string]any, error) {
	d := GetDeps()
	if d.Scheduler == nil {
		return nil, errors.New("scheduler not initialized")
	}
	job, err := d.Scheduler.Trigger(taskID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "action": "retry", "task": taskID, "task_job": job}, nil
}

func registerProjectTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "project_status",
		Description: "Projects status: instance identity, watched projects with issue counts, job counts, delivery mode and last check, allowed commenters, quota per CLI provider, and recent log lines",
		Annotations: readOnlyTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectStatusArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_status", func(m *projects.Manager) (map[string]any, error) {
			return m.Status(ctx, args.LogOffset), nil
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_job_list",
		Description: "List every Projects job, running and errored jobs first, with the actions each accepts; filter by project, status and search text, paged",
		Annotations: readOnlyTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectJobListArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_job_list", func(m *projects.Manager) (map[string]any, error) {
			return m.ListJobs(args.Target, args.Status, args.Query, args.Offset, args.Limit), nil
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_job_get",
		Description: "One Projects job: its state, agent session, structured transcript events, raw output, and scrollback",
		Annotations: readOnlyTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectJobGetArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_job_get", func(m *projects.Manager) (map[string]any, error) {
			return m.GetJob(args.Target, args.Number, args.Lines, args.Events)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_job_explain",
		Description: "Explain why a Projects job is paused, blocked, waiting, queued, or looks stuck, what will wake it, and its last output",
		Annotations: readOnlyTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectJobArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_job_explain", func(m *projects.Manager) (map[string]any, error) {
			return m.ExplainJob(args.Target, args.Number)
		})
	})

	for _, action := range []struct {
		name, verb, description string
		annotations             *sdkmcp.ToolAnnotations
	}{
		{"project_job_retry", "retry", "Retry a Projects job: requeue it so gh-fix reruns it with its saved session; on a paused job this clears the pause first. A scheduled job's task is fired again through the scheduler instead", mutatingTool},
		{"project_job_pause", "pause", "Pause an active, waiting or queued Projects job; a running agent is stopped and its work pushed, and only a retry or resume picks it back up", mutatingTool},
		{"project_job_resume", "resume", "Resume a paused Projects job: clear the pause and requeue it", mutatingTool},
		{"project_job_stop", "stop", "Stop a Projects job's agent and wait for it to exit, or clear a paused job and release its claim on the issue", destructiveTool},
	} {
		addTool(s, &sdkmcp.Tool{
			Name:        action.name,
			Description: action.description,
			Annotations: action.annotations,
		}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectJobArgs) (*sdkmcp.CallToolResult, struct{}, error) {
			return projectResult(action.name, func(m *projects.Manager) (map[string]any, error) {
				if action.verb == "retry" {
					if taskID, err := m.ScheduledRetry(args.Target, args.Number); err != nil {
						return nil, err
					} else if taskID != "" {
						return retryScheduledJob(taskID)
					}
				}
				return m.JobAction(ctx, action.verb, args.Target, args.Number)
			})
		})
	}

	addTool(s, &sdkmcp.Tool{
		Name:        "project_refresh",
		Description: "Check GitHub for Projects work now instead of waiting for the next poll",
		Annotations: mutatingTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectTargetArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_refresh", func(m *projects.Manager) (map[string]any, error) {
			return m.Refresh(args.Target)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_add",
		Description: "Watch a project: a repository, GitHub Project board, or Discussions board. Saved to the owning agent's projects in aviary.yaml unless runtime is set",
		Annotations: mutatingTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectAddArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_add", func(m *projects.Manager) (map[string]any, error) {
			target := strings.TrimSpace(args.Target)
			if target == "" {
				return nil, errors.New("target is required")
			}
			if args.Runtime {
				if args.Reports != nil {
					return nil, errors.New("reports apply only to a saved project")
				}
				watch := projects.Watch{Targets: []string{target}, Agent: strings.TrimSpace(args.Agent)}
				if args.Settings != nil {
					watch.Settings = *args.Settings
				}
				if err := m.AddWatch(watch); err != nil {
					return nil, err
				}
				return map[string]any{"target": target, "runtime": true}, nil
			}
			if args.Settings != nil {
				return nil, errors.New("settings apply only to a runtime watch; change saved settings with config_save")
			}
			cfg, err := config.Load("")
			if err != nil {
				return nil, err
			}
			if err := cfg.ValidateProjectReports(args.Reports); err != nil {
				return nil, err
			}
			if owner := cfg.ProjectOwner(target); owner != "" {
				return nil, fmt.Errorf("%s is already watched by agent %q", target, owner)
			}
			agent := strings.TrimSpace(args.Agent)
			if agent == "" {
				return nil, errors.New("agent is required: a saved project belongs to an agent")
			}
			i := slices.IndexFunc(cfg.Agents, func(a config.AgentConfig) bool { return a.Name == agent })
			if i < 0 {
				return nil, fmt.Errorf("agent %q is not configured", agent)
			}
			cfg.Agents[i].Projects.List = append(cfg.Agents[i].Projects.List, config.ProjectConfig{Target: target, Reports: args.Reports})
			if err := config.Save("", cfg); err != nil {
				return nil, err
			}
			m.Reconcile(cfg)
			return map[string]any{"target": target, "agent": agent, "saved": true}, nil
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_remove",
		Description: "Stop watching a project, removing it from aviary.yaml and from runtime watches. Its running, waiting, queued and paused jobs stop (running ones are checkpointed first) and are listed in stopped_jobs; the repository, its issues and pull requests, and clones on disk are left alone",
		Annotations: destructiveTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectRemoveArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_remove", func(m *projects.Manager) (map[string]any, error) {
			target := strings.TrimSpace(args.Target)
			stopped := m.InFlightJobs(target)
			runtime := m.RemoveWatch(target)
			if args.Runtime {
				if !runtime {
					return nil, fmt.Errorf("%s has no runtime watch", target)
				}
				return map[string]any{"target": target, "removed": true, "stopped_jobs": stopped}, nil
			}
			cfg, err := config.Load("")
			if err != nil {
				return nil, err
			}
			saved := false
			for i := range cfg.Agents {
				list := cfg.Agents[i].Projects.List
				before := len(list)
				cfg.Agents[i].Projects.List = slices.DeleteFunc(list, func(p config.ProjectConfig) bool {
					return strings.TrimSpace(p.Target) == target
				})
				saved = saved || len(cfg.Agents[i].Projects.List) != before
			}
			if saved {
				if err := config.Save("", cfg); err != nil {
					return nil, err
				}
				m.Reconcile(cfg)
			}
			if !saved && !runtime {
				return nil, fmt.Errorf("%s is not watched", target)
			}
			return map[string]any{"target": target, "removed": true, "stopped_jobs": stopped}, nil
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_clone_list",
		Description: "List every clone Projects made for a job, with its repository, issue, pull request and size, and whether it is still in use",
		Annotations: readOnlyTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_clone_list", func(m *projects.Manager) (any, error) {
			return m.CloneList(ctx)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_clone_clear",
		Description: "Remove every Projects clone no job still uses, reporting each one removed and the space reclaimed",
		Annotations: destructiveTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_clone_clear", func(m *projects.Manager) (any, error) {
			return m.CloneClear(ctx)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_unblock_run",
		Description: "Run a stuck-job watchdog round now, over every stuck Projects job or the one named; it returns once the round is requested",
		Annotations: mutatingTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectUnblockArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_unblock_run", func(m *projects.Manager) (map[string]any, error) {
			return m.UnblockRun(args.Target, args.Number)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_issue_comment",
		Description: "Post an update comment on an issue or pull request that mentions its author and this instance, so the job working it hears it; never edits the body",
		Annotations: mutatingTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectIssueCommentArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_issue_comment", func(m *projects.Manager) (map[string]any, error) {
			return m.IssueComment(ctx, args.Repository, args.Number, args.Text)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_issue_create",
		Description: "File an issue through the gh-create skill with this instance's identity, wait for it, and return the new issue's URL",
		Annotations: mutatingTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectIssueCreateArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_issue_create", func(m *projects.Manager) (map[string]any, error) {
			return m.IssueCreate(ctx, args.Repository, args.Text, args.Model)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_github_signin",
		Description: "Open GitHub's sign-in page in Aviary's browser so browser delivery reads GitHub signed in; returns once the window opens, and project_status reports the session. Fails where no window can appear, naming gh polling and profile import instead",
		Annotations: mutatingTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_github_signin", func(m *projects.Manager) (map[string]any, error) {
			return m.GitHubSignIn()
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_github_use_polling",
		Description: "Read GitHub through gh polling instead of Aviary's browser: saves delivery: poll to the projects settings of every agent that owns projects in aviary.yaml",
		Annotations: mutatingTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_github_use_polling", func(m *projects.Manager) (map[string]any, error) {
			cfg, err := config.Load("")
			if err != nil {
				return nil, err
			}
			changed := false
			for i := range cfg.Agents {
				if len(cfg.Agents[i].Projects.List) > 0 {
					cfg.Agents[i].Projects.Delivery = projects.DeliveryPoll
					changed = true
				}
			}
			if !changed {
				return nil, errors.New("no agent owns a project; add one to an agent first, or watch with --delivery poll")
			}
			if err := config.Save("", cfg); err != nil {
				return nil, err
			}
			m.Reconcile(cfg)
			return map[string]any{"delivery": projects.DeliveryPoll, "saved": true}, nil
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_github_import",
		Description: "Import the GitHub sign-in from an Aviary browser profile signed in on another machine, for servers and containers where no sign-in window can appear",
		Annotations: mutatingTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectGitHubImportArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_github_import", func(m *projects.Manager) (map[string]any, error) {
			return m.ImportBrowserProfile(strings.TrimSpace(args.Profile))
		})
	})
}
