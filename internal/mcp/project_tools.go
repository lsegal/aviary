package mcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/projects"
	projectscore "github.com/lsegal/aviary/internal/projects/core"
)

var (
	readOnlyTool    = &sdkmcp.ToolAnnotations{ReadOnlyHint: true}
	destructiveTrue = true
	destructiveTool = &sdkmcp.ToolAnnotations{DestructiveHint: &destructiveTrue}
	nonDestructive  = false
	mutatingTool    = &sdkmcp.ToolAnnotations{DestructiveHint: &nonDestructive}
)

type projectStatusArgs struct {
	LogOffset int      `json:"log_offset,omitempty" jsonschema:"Return only the log lines logged after this offset, as returned in log_offset by an earlier call"`
	Targets   []string `json:"targets,omitempty" jsonschema:"Count jobs in each board state over only these projects' jobs"`
}

type projectJobListArgs struct {
	Target  string   `json:"target,omitempty" jsonschema:"Only list jobs of this project"`
	Targets []string `json:"targets,omitempty" jsonschema:"Only list jobs of these projects, together with target"`
	Status  string   `json:"status,omitempty" jsonschema:"Only list jobs with this status, such as active, waiting, queued, paused, failed or complete"`
	States  []string `json:"states,omitempty" jsonschema:"Only list jobs the board shows in any of these states: starting, paused, running, done, failed, or scheduled for a schedule between runs"`
	Kinds   []string `json:"kinds,omitempty" jsonschema:"Only list jobs of these kinds: issue, discussion, schedule, merge or discuss"`
	Query   string   `json:"query,omitempty" jsonschema:"Only list jobs with this text, case-insensitively, in any field: number, pull request, title, repository, status, skill, agent, session, schedule, or output"`
	Offset  int      `json:"offset,omitempty" jsonschema:"Index of the first job to return"`
	Limit   int      `json:"limit,omitempty" jsonschema:"Most jobs to return; defaults to 50"`
}

// jobListTargets is the projects a job list covers: target plus targets.
func jobListTargets(args projectJobListArgs) []string {
	if args.Target == "" {
		return args.Targets
	}
	return append([]string{args.Target}, args.Targets...)
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

type projectJobQueueArgs struct {
	Skill      string `json:"skill" jsonschema:"gh-safe-merge to queue pull requests for a safe merge, gh-discuss to queue Discussion threads for an answer, or gh-fix to have issues fixed"`
	Numbers    []int  `json:"numbers" jsonschema:"Pull request, Discussion or issue numbers, in the order to start them; queued merges run concurrently up to the agent slots and start in this priority order"`
	Target     string `json:"target,omitempty" jsonschema:"The project to queue on; needed only when several projects are watched"`
	Repository string `json:"repository,omitempty" jsonschema:"OWNER/REPO the numbers live in; needed only when the project is a GitHub Project board"`
	// Titles names the numbers on their cards before their runs read them.
	Titles map[string]string `json:"titles,omitempty" jsonschema:"Optional card titles by number"`
}

type projectQueueCandidatesArgs struct {
	Skill      string `json:"skill" jsonschema:"gh-safe-merge to list open pull requests, or gh-discuss to list open Discussion threads"`
	Target     string `json:"target,omitempty" jsonschema:"The project to list for; needed only when several projects are watched"`
	Repository string `json:"repository,omitempty" jsonschema:"OWNER/REPO to list; needed only when the project is a GitHub Project board"`
}

type projectTargetArgs struct {
	Target string `json:"target,omitempty" jsonschema:"One project; leave out for all of them"`
}

type projectAddArgs struct {
	Target  string `json:"target" jsonschema:"owner/repo, a repository URL, a GitHub Project board, or a Discussions board"`
	Agent   string `json:"agent,omitempty" jsonschema:"The Aviary agent that owns the project; it lands in that agent's projects and its model and fallbacks are the provider and model defaults of the project's jobs. Required unless runtime is set"`
	Runtime bool   `json:"runtime,omitempty" jsonschema:"Watch until the server stops without writing aviary.yaml, as aviary watch does"`
	// Mode overrides the shared projects mode for this project.
	Mode string `json:"mode,omitempty" jsonschema:"This project's own mode, overriding the shared projects mode: watch picks up open issues matching the filter; manual never searches for issues, so only work queued on demand, scheduled project tasks and agent tools start jobs. Empty or inherit follows the shared projects mode"`
	// Reports opts a saved project in to job reports.
	Reports *config.ProjectReportsConfig `json:"reports,omitempty" jsonschema:"Send this project's job events (merged, failed, waiting) through an Aviary agent's channels; leave out for no reports"`
	// Settings overrides the shared projects settings for a runtime watch.
	Settings *config.ProjectsConfig `json:"settings,omitempty" jsonschema:"For a runtime watch, projects settings that override aviary.yaml while it runs. model and fallbacks (CLI provider models such as claude/opus) replace the owning agent's model and fallbacks for its jobs"`
}

type projectUpdateArgs struct {
	Target string `json:"target" jsonschema:"The watched project to change"`
	// Filter sets the project's own issue search filter.
	Filter string `json:"filter,omitempty" jsonschema:"This project's own issue search filter, in GitHub search syntax such as label:bug -label:wip assignee:@me; it replaces the shared projects filter for this project only. It is checked against GitHub first and an invalid filter is refused"`
	// ResetFilter removes the override.
	ResetFilter bool `json:"reset_filter,omitempty" jsonschema:"Remove this project's own filter so it uses the shared projects filter again"`
	// Mode sets or clears the project's own mode override.
	Mode string `json:"mode,omitempty" jsonschema:"This project's own mode, overriding the shared projects mode for this project only: watch to pick up open issues matching the filter, manual to stop searching for issues so only work queued on demand, scheduled project tasks and agent tools start jobs, or inherit to remove the override and follow the shared projects mode again. Picking the shared mode itself also removes the override. Jobs already in flight carry on either way, and switching to watch checks for issues right away"`
}

type projectFilterCheckArgs struct {
	Target string `json:"target" jsonschema:"The project to search"`
	Filter string `json:"filter" jsonschema:"The issue search filter to check, in GitHub search syntax"`
}

type projectRemoveArgs struct {
	Target     string `json:"target" jsonschema:"The project to stop watching"`
	Runtime    bool   `json:"runtime,omitempty" jsonschema:"Only stop a runtime watch, leaving aviary.yaml alone"`
	DeleteData bool   `json:"delete_data,omitempty" jsonschema:"Also delete the project's job records, job transcripts and the clones no job still uses, once its jobs have stopped"`
}

type projectUnblockArgs struct {
	Number int    `json:"number,omitempty" jsonschema:"One job's issue number; leave out to cover every stuck job"`
	Target string `json:"target,omitempty" jsonschema:"The job's project"`
}

type projectIssueCommentArgs struct {
	Number     int    `json:"number" jsonschema:"The issue or pull request number"`
	Repository string `json:"repository,omitempty" jsonschema:"OWNER/REPO hosting it; defaults to the repository of the job with this number, or the only watched repository"`
	Text       string `json:"text" jsonschema:"The update to post"`
	// Attachments are files to upload and embed besides the turn's media.
	Attachments []string `json:"attachments,omitempty" jsonschema:"Local image or video files (paths or file:// URLs) to upload and embed in the comment, in addition to the current turn's media"`
	// SkipTurnMedia leaves the current turn's media out.
	SkipTurnMedia bool `json:"skip_turn_media,omitempty" jsonschema:"Do not attach the images and videos from the user message that started this turn, which are attached by default"`
}

type projectIssueCreateArgs struct {
	Repository string `json:"repository,omitempty" jsonschema:"OWNER/REPO to file the issue on; defaults to the only watched repository"`
	Text       string `json:"text" jsonschema:"What the issue is about, as you would describe it to gh-create"`
	Model      string `json:"model,omitempty" jsonschema:"Model for the CLI provider that runs gh-create"`
	// Attachments are files to upload and embed besides the turn's media.
	Attachments []string `json:"attachments,omitempty" jsonschema:"Local image or video files (paths or file:// URLs) for gh-create to upload and embed in the issue, in addition to the current turn's media"`
	// SkipTurnMedia leaves the current turn's media out.
	SkipTurnMedia bool `json:"skip_turn_media,omitempty" jsonschema:"Do not attach the images and videos from the user message that started this turn, which are attached by default"`
	// Distinct files the issue even though this session just filed another.
	Distinct bool `json:"distinct,omitempty" jsonschema:"Set only when the user asked for a separate issue: files it even though this session is filing, or just filed, a different issue on the repository"`
}

type projectIssueBackfillMediaArgs struct {
	Repository string `json:"repository,omitempty" jsonschema:"OWNER/REPO the issues are on; defaults to the only watched repository"`
	Numbers    []int  `json:"numbers" jsonschema:"The issue numbers to backfill"`
	DryRun     bool   `json:"dry_run,omitempty" jsonschema:"Report what would be posted without posting"`
}

// issueAttachmentsDescription tells agents how project_issue_create and
// project_issue_comment attach media.
const issueAttachmentsDescription = "Screenshots and videos are uploaded and embedded, never just described: by default the images and videos attached to the user message that started this turn are attached (its other files are left out and listed in skipped_turn_media), and attachments adds local image or video files; set skip_turn_media to leave the turn's media out. GitHub takes PNG, JPEG, GIF, WebP and SVG images up to 10 MB and MP4, MOV and WebM videos up to 100 MB. A file that is missing, of another type, or too large fails the call with an error naming it and the reason, and nothing is posted; the result lists the files attached"

// issueAttachments is the files a filing attaches: the images and videos of
// the current turn's media, unless skipTurnMedia, then extra. skipped names
// the turn's other media, such as voice notes and documents, which GitHub
// does not take as attachments.
func issueAttachments(ctx context.Context, extra []string, skipTurnMedia bool) (files, skipped []string, err error) {
	var problems []string
	add := func(mediaURL string, turn bool) {
		file, err := agent.MediaFile(mediaURL)
		if err != nil {
			problems = append(problems, err.Error())
			return
		}
		if turn && !isVisualMedia(file) {
			skipped = append(skipped, file)
			return
		}
		files = append(files, file)
	}
	if !skipTurnMedia {
		for _, mediaURL := range agent.TurnMediaFromContext(ctx) {
			add(mediaURL, true)
		}
	}
	for _, file := range extra {
		add(file, false)
	}
	if len(problems) > 0 {
		return nil, nil, fmt.Errorf("cannot attach %s", strings.Join(problems, "; "))
	}
	return files, skipped, nil
}

// isVisualMedia reports whether file is named as an image or video.
func isVisualMedia(file string) bool {
	contentType := llm.ContentTypeFromName(file)
	return strings.HasPrefix(contentType, "image/") || strings.HasPrefix(contentType, "video/")
}

// withSkippedMedia adds the turn media a filing left out to its result.
func withSkippedMedia(result map[string]any, err error, skipped []string) (map[string]any, error) {
	if err == nil && len(skipped) > 0 {
		result["skipped_turn_media"] = skipped
	}
	return result, err
}

type projectGitHubReadArgs struct {
	Repository string `json:"repository,omitempty" jsonschema:"Watched OWNER/REPO to read; defaults to the url's repository or the only watched repository"`
	Path       string `json:"path,omitempty" jsonschema:"REST path under repos/OWNER/REPO, such as commits/SHA, compare/BASE...HEAD, contents/FILE?ref=REF, pulls/N, pulls/N/files, pulls/N/reviews, issues/N/comments, issues/comments/ID, commits/SHA/check-runs, actions/runs?branch=main, actions/runs/ID/jobs or actions/jobs/ID/logs; empty reads the repository itself"`
	URL        string `json:"url,omitempty" jsonschema:"A github.com URL in a watched repository to read instead of path: a commit, comparison, file, pull request, issue, issue or review comment, review, Actions run or job, or release"`
	Media      string `json:"media,omitempty" jsonschema:"diff or patch for a commit, comparison or pull request's diff; raw for a file's text, the default for contents paths"`
}

type projectSiteFetchArgs struct {
	URL string `json:"url" jsonschema:"http or https URL on a watched repository's homepage or GitHub Pages site, or its owner's github.io site"`
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
// removedProject is project_remove's result, starting the purge of the
// removed project's data when deleteData asks for it.
func removedProject(m *projects.Manager, target string, stopped []int, deleteData bool) map[string]any {
	if deleteData {
		m.PurgeTargetInBackground(target)
	}
	return map[string]any{"target": target, "removed": true, "stopped_jobs": stopped, "data_deleted": deleteData}
}

func projectResult[T any](fn func(*projects.Manager) (T, error)) (*sdkmcp.CallToolResult, struct{}, error) {
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

// projectsAgentContext is the live context a request to the Code Loop agent
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

// retryScheduledJob fires a scheduled Code Loop job's task again through
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
		Description: "Code Loop status: instance identity, watched projects with issue counts, job counts, jobs in each board state (starting, paused, running, done, failed), delivery mode and last check, allowed commenters, quota per CLI provider, and recent log lines",
		Annotations: readOnlyTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectStatusArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			return m.Status(ctx, args.LogOffset, args.Targets), nil
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_job_list",
		Description: "List every Code Loop job, running and errored jobs first, with the actions each accepts; filter by project, status, board states, kinds and search text, paged. Each schedule is one entry, its latest run, with last_run, next_run and recent_runs, instead of one job per fire",
		Annotations: readOnlyTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectJobListArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			return m.ListJobs(jobListTargets(args), args.Status, args.States, args.Kinds, args.Query, args.Offset, args.Limit), nil
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_job_get",
		Description: "One Code Loop job: its state, agent session, structured transcript events, raw output, and scrollback",
		Annotations: readOnlyTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectJobGetArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			return m.GetJob(args.Target, args.Number, args.Lines, args.Events)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_job_explain",
		Description: "Explain why a Code Loop job is paused, blocked, waiting, queued, or looks stuck, what will wake it, and its last output",
		Annotations: readOnlyTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectJobArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			return m.ExplainJob(args.Target, args.Number)
		})
	})

	for _, action := range []struct {
		name, verb, description string
		annotations             *sdkmcp.ToolAnnotations
	}{
		{"project_job_retry", "retry", "Retry a Code Loop job: requeue it so gh-fix reruns it with its saved session; on a paused job this clears the pause first. A scheduled job's task is fired again through the scheduler instead", mutatingTool},
		{"project_job_pause", "pause", "Pause an active, waiting or queued Code Loop job; a running agent is stopped and its work pushed, and only a retry or resume picks it back up", mutatingTool},
		{"project_job_resume", "resume", "Resume a paused Code Loop job: clear the pause and requeue it", mutatingTool},
		{"project_job_stop", "stop", "Stop a Code Loop job's agent and wait for it to exit, or clear a paused job and release its claim on the issue", destructiveTool},
		{"project_job_dismiss", "dismiss", "Dismiss a failed scheduled run: its schedule leaves the board's Failed state until its next run fails, while its card still shows the last run", mutatingTool},
	} {
		addTool(s, &sdkmcp.Tool{
			Name:        action.name,
			Description: action.description,
			Annotations: action.annotations,
		}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectJobArgs) (*sdkmcp.CallToolResult, struct{}, error) {
			return projectResult(func(m *projects.Manager) (map[string]any, error) {
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
		Name:        "project_job_queue",
		Description: "Queue Code Loop jobs on demand: pull requests for gh-safe-merge, which verify and merge concurrently up to the agent slots, starting in the order given and updating any pull request a sibling merge left behind, or Discussion threads for gh-discuss. Each becomes a board job with logs, transcript and pause, resume, stop and retry; queued jobs survive a restart. A number a job already owns is reported with that job and not queued twice. Safe merge is refused while no_merge is set. gh-fix retries an issue's finished job, or checks GitHub now so a matching issue is dispatched",
		Annotations: mutatingTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectJobQueueArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			titles := map[int]string{}
			for number, title := range args.Titles {
				n, err := strconv.Atoi(strings.TrimPrefix(number, "#"))
				if err != nil {
					return nil, fmt.Errorf("titles: %q is not a number", number)
				}
				titles[n] = title
			}
			return m.QueueJobs(ctx, args.Target, strings.TrimSpace(args.Repository), strings.TrimSpace(args.Skill), args.Numbers, titles)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_queue_candidates",
		Description: "List what a Code Loop project can queue: open pull requests for gh-safe-merge, with CI status, review decision, mergeability, author and the job that already owns each, or open Discussion threads for gh-discuss. Also reports whether no_merge turns safe merge off",
		Annotations: readOnlyTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectQueueCandidatesArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			return m.QueueCandidates(ctx, args.Target, strings.TrimSpace(args.Repository), strings.TrimSpace(args.Skill))
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_refresh",
		Description: "Check GitHub for Code Loop work now instead of waiting for the next poll",
		Annotations: mutatingTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectTargetArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			return m.Refresh(args.Target)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_add",
		Description: "Watch a project: a repository, GitHub Project board, or Discussions board. Saved to the owning agent's projects in aviary.yaml unless runtime is set",
		Annotations: mutatingTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectAddArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			target := strings.TrimSpace(args.Target)
			if target == "" {
				return nil, errors.New("target is required")
			}
			mode, err := config.NormalizeProjectModeOverride(args.Mode)
			if err != nil {
				return nil, err
			}
			if args.Runtime {
				if args.Reports != nil {
					return nil, errors.New("reports apply only to a saved project")
				}
				watch := projects.Watch{Targets: []string{target}, Agent: strings.TrimSpace(args.Agent)}
				if mode != "" {
					watch.Modes = map[string]string{target: mode}
				}
				if args.Settings != nil {
					cfg, err := config.Load("")
					if err != nil {
						return nil, err
					}
					if err := config.ValidateProjectModels(cfg.Models, *args.Settings); err != nil {
						return nil, fmt.Errorf("settings: %w", err)
					}
					watch.Settings = *args.Settings
				}
				// The project is watched once this returns; the engine
				// restarts onto it and polls it in the background.
				if err := m.AddWatchInBackground(watch); err != nil {
					return nil, err
				}
				effective, source := m.ProjectMode(target)
				return map[string]any{"target": target, "runtime": true, "mode": effective, "mode_source": source}, nil
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
			project := config.ProjectConfig{Target: target, Reports: args.Reports, Mode: mode}
			cfg.Agents[i].Projects = append(cfg.Agents[i].Projects, project)
			if err := config.Save("", cfg); err != nil {
				return nil, err
			}
			m.ReconcileInBackground(cfg)
			effective, source := cfg.Projects.ModeFor(mode)
			return map[string]any{"target": target, "agent": agent, "saved": true, "mode": effective, "mode_source": source}, nil
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_filter_check",
		Description: "Check an issue search filter for a Code Loop project without saving it: its syntax, then a dry-run GitHub search reporting how many open issues of the project it matches in matches. An invalid filter fails with the reason",
		Annotations: readOnlyTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectFilterCheckArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			target := strings.TrimSpace(args.Target)
			filter := config.JoinFilter([]string{args.Filter})
			matches, err := m.CheckIssueFilter(ctx, target, filter)
			if err != nil {
				return nil, fmt.Errorf("invalid filter: %w", err)
			}
			return map[string]any{"target": target, "filter": filter, "matches": matches}, nil
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_update",
		Description: "Change one watched Code Loop project. filter sets the project's own issue search filter, replacing the shared projects filter for this project only, after checking it against GitHub; reset_filter removes it. mode switches it between watch and manual (no issue searches; only queued work, scheduled project tasks and agent tools start jobs). A saved project's changes are written to its entry in aviary.yaml, a runtime watch's last until the watch stops. The project is checked for work right away",
		Annotations: mutatingTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectUpdateArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			return updateProject(ctx, m, args)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_remove",
		Description: "Stop watching a project, removing it from aviary.yaml and from runtime watches. Its running, waiting, queued and paused jobs stop (running ones are checkpointed first) and are listed in stopped_jobs; the repository, its issues and pull requests, are left alone. With delete_data, its job records, job transcripts and unused clones are deleted once its jobs have stopped; otherwise they are kept",
		Annotations: destructiveTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectRemoveArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			target := strings.TrimSpace(args.Target)
			stopped := m.InFlightJobs(target)
			runtime := m.RemoveWatchInBackground(target)
			if args.Runtime {
				if !runtime {
					return nil, fmt.Errorf("%s has no runtime watch", target)
				}
				return removedProject(m, target, stopped, args.DeleteData), nil
			}
			cfg, err := config.Load("")
			if err != nil {
				return nil, err
			}
			saved := false
			for i := range cfg.Agents {
				list := cfg.Agents[i].Projects
				before := len(list)
				cfg.Agents[i].Projects = slices.DeleteFunc(list, func(p config.ProjectConfig) bool {
					return strings.TrimSpace(p.Target) == target
				})
				saved = saved || len(cfg.Agents[i].Projects) != before
			}
			if saved {
				if err := config.Save("", cfg); err != nil {
					return nil, err
				}
				m.ReconcileInBackground(cfg)
			}
			if !saved && !runtime {
				return nil, fmt.Errorf("%s is not watched", target)
			}
			return removedProject(m, target, stopped, args.DeleteData), nil
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_clone_list",
		Description: "List every clone Code Loop made for a job, with its repository, issue, pull request and size, and whether it is still in use",
		Annotations: readOnlyTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (any, error) {
			return m.CloneList(ctx)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_clone_clear",
		Description: "Remove every Code Loop clone no job still uses, reporting each one removed and the space reclaimed",
		Annotations: destructiveTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (any, error) {
			return m.CloneClear(ctx)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_unblock_run",
		Description: "Run a stuck-job watchdog round now, over every stuck Code Loop job or the one named; it returns once the round is requested",
		Annotations: mutatingTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args projectUnblockArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			return m.UnblockRun(args.Target, args.Number)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_issue_comment",
		Description: "Post an update comment on an issue or pull request that mentions its author and this instance, so the job working it hears it; never edits the body. " + issueAttachmentsDescription,
		Annotations: mutatingTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectIssueCommentArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			attachments, skipped, err := issueAttachments(ctx, args.Attachments, args.SkipTurnMedia)
			if err != nil {
				return nil, err
			}
			result, err := m.IssueComment(ctx, args.Repository, args.Number, args.Text, attachments)
			return withSkippedMedia(result, err, skipped)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_issue_create",
		Description: "File an issue through the gh-create skill with this instance's identity, wait for it, and return the new issue's URL. Calling it again with the same repository, text and attachments while that issue is still filing, or after it filed, waits for or returns the same issue instead of filing a duplicate. Before filing, check what this session already filed or is still filing (earlier project_issue_create calls in this conversation, including ones still running for another message); project_job_list cannot show a new issue. A different request from this session on the same repository while one is filing, or within 10 minutes of one filing, is refused with that issue's URL, age and summary, unless distinct is set because the user asked for a separate issue. " + issueAttachmentsDescription,
		Annotations: mutatingTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectIssueCreateArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			attachments, skipped, err := issueAttachments(ctx, args.Attachments, args.SkipTurnMedia)
			if err != nil {
				return nil, err
			}
			session, _ := agent.SessionIDFromContext(ctx)
			result, err := m.IssueCreate(ctx, projectscore.IssueCreate{
				Repository: args.Repository, Text: args.Text, Model: args.Model, Attachments: attachments,
				Session: session, Distinct: args.Distinct,
			})
			return withSkippedMedia(result, err, skipped)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_issue_backfill_media",
		Description: "Post the screenshots and videos sent with the request that filed each issue, for issues filed before project_issue_create attached them: each issue is matched to the user message whose turn called project_issue_create and got its URL back, and that message's images and videos are posted as one comment per issue that mentions no one, so no job or agent is woken; never edits issue bodies. Each issue's result says whether its comment was posted, was already there (a rerun posts nothing new), or why no media was found",
		Annotations: mutatingTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectIssueBackfillMediaArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			return m.BackfillIssueMedia(ctx, args.Repository, args.Numbers, args.DryRun)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_github_read",
		Description: "Read a watched repository from GitHub through gh with this instance's auth: one GET of the REST API under repos/OWNER/REPO, named by path or by a github.com URL, such as a commit with its files and diff, a comparison, a file at a ref, a pull request with its files, reviews, comments and checks, an issue or review comment by ID or URL, or Actions runs, jobs and job logs. Only watched repositories can be read, and each read is logged in the Code Loop log. A JSON answer is returned as data, anything else as text; a body over 64 KB is cut, keeping a log's tail",
		Annotations: readOnlyTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectGitHubReadArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			return m.GitHubRead(ctx, args.Repository, args.Path, args.URL, args.Media)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_site_fetch",
		Description: "Fetch a page of a watched project's own site, its repository's homepage or GitHub Pages site or its owner's github.io site, to check what is deployed: returns the final URL, status, content type, caching headers and the body, cut at 64 KB. Redirects must stay on those sites, and each fetch is logged in the Code Loop log",
		Annotations: readOnlyTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectSiteFetchArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			return m.SiteFetch(ctx, args.URL)
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_github_signin",
		Description: "Open GitHub's sign-in page in Aviary's browser so browser delivery reads GitHub signed in; returns once the window opens, and project_status reports the session. Fails where no window can appear, naming gh polling and profile import instead",
		Annotations: mutatingTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			return m.GitHubSignIn()
		})
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "project_github_use_polling",
		Description: "Read GitHub through gh polling instead of Aviary's browser: saves delivery: poll to the shared projects settings in aviary.yaml",
		Annotations: mutatingTool,
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			cfg, err := config.Load("")
			if err != nil {
				return nil, err
			}
			cfg.Projects.Delivery = projects.DeliveryPoll
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
		return projectResult(func(m *projects.Manager) (map[string]any, error) {
			return m.ImportBrowserProfile(strings.TrimSpace(args.Profile))
		})
	})
}

// updateProject is project_update: it sets or resets a project's own issue
// filter and sets or resets its mode override, on its runtime watch when it has one and
// otherwise on its saved entry, then checks the project for work.
func updateProject(ctx context.Context, m *projects.Manager, args projectUpdateArgs) (map[string]any, error) {
	target := strings.TrimSpace(args.Target)
	if target == "" {
		return nil, errors.New("target is required")
	}
	filter := config.JoinFilter([]string{args.Filter})
	setMode := strings.TrimSpace(args.Mode) != ""
	setFilter := args.ResetFilter || filter != ""
	switch {
	case args.ResetFilter && filter != "":
		return nil, errors.New("pass filter or reset_filter, not both")
	case !setFilter && !setMode:
		return nil, errors.New("nothing to change: pass filter, reset_filter or mode")
	}
	mode := ""
	if setMode {
		var err error
		if mode, err = config.NormalizeProjectModeOverride(args.Mode); err != nil {
			return nil, err
		}
		// Picking the shared mode clears the override, so the project keeps
		// following the shared mode when it changes later (#1461).
		if mode == m.DefaultMode() {
			mode = ""
		}
	}
	result := map[string]any{"target": target}
	var override []string
	if filter != "" {
		matches, err := m.CheckIssueFilter(ctx, target, filter)
		if err != nil {
			return nil, fmt.Errorf("invalid filter: %w", err)
		}
		override = []string{filter}
		result["matches"] = matches
	}
	runtime := false
	if setFilter {
		var err error
		if runtime, err = m.SetWatchFilter(target, override); err != nil {
			return nil, err
		}
	}
	if setMode {
		var err error
		if runtime, err = m.SetWatchMode(target, mode); err != nil {
			return nil, err
		}
	}
	if runtime {
		result["runtime"] = true
	} else {
		cfg, err := config.Load("")
		if err != nil {
			return nil, err
		}
		found := false
		for i := range cfg.Agents {
			for j := range cfg.Agents[i].Projects {
				if strings.TrimSpace(cfg.Agents[i].Projects[j].Target) != target {
					continue
				}
				found = true
				if setFilter {
					cfg.Agents[i].Projects[j].Filter = override
				}
				if setMode {
					// Inherit removes the override.
					cfg.Agents[i].Projects[j].Mode = mode
				}
			}
		}
		if !found {
			return nil, fmt.Errorf("%s is not watched", target)
		}
		if err := config.Save("", cfg); err != nil {
			return nil, err
		}
		m.Reconcile(cfg)
		result["saved"] = true
	}
	// A stopped engine has nothing to refresh; the next start reads the
	// new settings.
	_, _ = m.Refresh(target)
	m.NotifyUpdated(target)
	result["custom"] = m.IssueFilterCustom(target)
	result["issue_filter"] = m.IssueFilter(target)
	result["mode"], result["mode_source"] = m.ProjectMode(target)
	return result, nil
}
