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
	Target  string   `json:"target" jsonschema:"owner/repo, a repository URL, a GitHub Project board, or a Discussions board"`
	Agents  []string `json:"agents,omitempty" jsonschema:"This project's coding-CLI specs (AGENT/MODEL:LEVEL) in fallback order"`
	Runtime bool     `json:"runtime,omitempty" jsonschema:"Watch until the server stops without writing aviary.yaml, as aviary watch does"`
	// Settings overrides the projects: section for a runtime watch.
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
	Model      string `json:"model,omitempty" jsonschema:"Model for the coding CLI that runs gh-create"`
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

func registerProjectTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "project_status",
		Description: "Projects status: instance identity, watched projects with issue counts, job counts, delivery mode and last check, quota per coding CLI, and recent log lines",
		Annotations: readOnlyTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectStatusArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_status", func(m *projects.Manager) (map[string]any, error) {
			return m.Status(ctx, args.LogOffset), nil
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_job_list",
		Description: "List every Projects job, running and errored jobs first, with the actions each accepts; filter by project and status, paged",
		Annotations: readOnlyTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectJobListArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_job_list", func(m *projects.Manager) (map[string]any, error) {
			return m.ListJobs(args.Target, args.Status, args.Offset, args.Limit), nil
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
		{"project_job_retry", "retry", "Retry a Projects job: requeue it so gh-fix reruns it with its saved session; on a paused job this clears the pause first", mutatingTool},
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
		Description: "Watch a project: a repository, GitHub Project board, or Discussions board. Saved to the projects: section of aviary.yaml unless runtime is set",
		Annotations: mutatingTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectAddArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_add", func(m *projects.Manager) (map[string]any, error) {
			target := strings.TrimSpace(args.Target)
			if target == "" {
				return nil, errors.New("target is required")
			}
			if args.Runtime {
				watch := projects.Watch{Targets: []string{target}, Agents: args.Agents}
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
			if slices.Contains(cfg.Projects.Targets(), target) {
				return nil, fmt.Errorf("%s is already watched", target)
			}
			cfg.Projects.List = append(cfg.Projects.List, config.ProjectConfig{Target: target, Agents: args.Agents})
			if err := config.Save("", cfg); err != nil {
				return nil, err
			}
			m.Reconcile(cfg.Projects)
			return map[string]any{"target": target, "saved": true}, nil
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_remove",
		Description: "Stop watching a project, removing it from aviary.yaml and from runtime watches; running jobs are checkpointed first",
		Annotations: destructiveTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectRemoveArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_remove", func(m *projects.Manager) (map[string]any, error) {
			target := strings.TrimSpace(args.Target)
			runtime := m.RemoveWatch(target)
			if args.Runtime {
				if !runtime {
					return nil, fmt.Errorf("%s has no runtime watch", target)
				}
				return map[string]any{"target": target, "removed": true}, nil
			}
			cfg, err := config.Load("")
			if err != nil {
				return nil, err
			}
			before := len(cfg.Projects.List)
			cfg.Projects.List = slices.DeleteFunc(cfg.Projects.List, func(p config.ProjectConfig) bool {
				return strings.TrimSpace(p.Target) == target
			})
			saved := len(cfg.Projects.List) != before
			if saved {
				if err := config.Save("", cfg); err != nil {
					return nil, err
				}
				m.Reconcile(cfg.Projects)
			}
			if !saved && !runtime {
				return nil, fmt.Errorf("%s is not watched", target)
			}
			return map[string]any{"target": target, "removed": true}, nil
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_cli_list",
		Description: "The coding-CLI doctor: for each coding-CLI definition, whether it is installed, its version, sign-in, quota, and the models it reports",
		Annotations: readOnlyTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult("project_cli_list", func(m *projects.Manager) (map[string]any, error) {
			return m.CLIList(ctx)
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
}
