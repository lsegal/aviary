# Code Loop Tools

The `project_*` tools manage [Code Loop](/guide/projects). Every tool is also an `aviary project` subcommand that takes the same arguments as `--<field> <value>` flags. A leading positional argument fills the subcommand's main field, such as the job number or the target. The tools need a running Aviary server.

A job is named by its issue `number`. `target` is needed only when several projects have a job with that number.

| Tool | CLI | Hint |
| --- | --- | --- |
| `project_status` | `aviary project status` | read-only |
| `project_job_list` | `aviary project job list` | read-only |
| `project_job_get` | `aviary project job get` | read-only |
| `project_job_explain` | `aviary project job explain` | read-only |
| `project_job_retry` | `aviary project job retry` | |
| `project_job_pause` | `aviary project job pause` | |
| `project_job_resume` | `aviary project job resume` | |
| `project_job_stop` | `aviary project job stop` | destructive |
| `project_job_dismiss` | `aviary project job dismiss` | |
| `project_job_queue` | `aviary project job queue` | |
| `project_queue_candidates` | `aviary project queue candidates` | read-only |
| `project_refresh` | `aviary project refresh` | |
| `project_add` | `aviary project add` | |
| `project_update` | `aviary project update` | |
| `project_filter_check` | `aviary project filter check` | read-only |
| `project_remove` | `aviary project remove` | destructive |
| `project_clone_list` | `aviary project clone list` | read-only |
| `project_clone_clear` | `aviary project clone clear` | destructive |
| `project_unblock_run` | `aviary project unblock` | |
| `project_issue_comment` | `aviary project issue comment` | |
| `project_issue_create` | `aviary project issue create` | |
| `project_issue_backfill_media` | `aviary project issue backfill-media` | |
| `project_github_read` | `aviary project github read` | read-only |
| `project_site_fetch` | `aviary project site fetch` | read-only |
| `project_task_list` | `aviary project task list` | read-only |
| `project_task_get` | `aviary project task get` | read-only |
| `project_task_create` | `aviary project task create` | |
| `project_task_update` | `aviary project task update` | |
| `project_task_pause` | `aviary project task pause` | |
| `project_task_resume` | `aviary project task resume` | |
| `project_task_run` | `aviary project task run` | |
| `project_task_delete` | `aviary project task delete` | destructive |
| `project_check` | `aviary project check` | read-only; reports the project's `mode`, or the `mode` passed |

Settings have no tool of their own. A project lives in the `projects:` list of the agent that owns it in `aviary.yaml` ([`agents[].projects`](/reference/config#agents-projects)), and the settings every project shares live in the top-level `projects:` section ([`projects`](/reference/config#projects)). Both are read and written with `config_get` and `config_save`, and are applied to the running engine when the file changes.

---

## project_status

Instance identity, watched projects with issue counts, job counts, the jobs in each board state (`states`: `starting`, `paused`, `running`, `done`, `failed`; a schedule counts once, under `starting` or `running` while a run is in flight and under `failed` while its last run failed and was not dismissed, and never under `done`), delivery mode, poll interval, the shared issue filter that selects work (`issue_filter`, empty when every open issue is picked up), each project's effective filter (`issue_filters`) and the projects with their own (`custom_filters`), each project's mode (`modes`: `watch` or `manual`; a manual project has no entry in `issue_counts`), last check, quota per CLI provider, the configured agent specs, each project's effective job model, and recent log lines. `models` maps each project to its job `model`, `fallbacks` and `source` (`project override`, `from agent <name>`, or `from models.defaults`), and `model_labels` renders them, such as `claude/opus (project override)`.

**Arguments:** `log_offset` (optional): return only the lines logged after this offset. Each response's `log_offset` is the offset to pass next time. `aviary watch --no-tui` tails the log this way. `targets` (optional): count `states` over only these projects' jobs, as `project_job_list` filters them; the board's stat cards pass the projects they show.

## project_job_list

Every job, running and errored jobs first, with the actions each accepts. A scheduled task is one entry, never a job per fire: the entry is its latest run, and its `state` is `starting` or `running` while that run is in flight, `failed` while its last run failed and was not dismissed, and `scheduled` otherwise.

**Arguments:** `target`, `status` (such as `active`, `waiting`, `queued`, `paused`, `failed`, `complete`), `states` (board states, any of `starting`, `paused`, `running`, `done`, `failed`, or `scheduled` for a schedule between runs; a job in any of them is listed), `kinds` (any of `issue`, `discussion`, `schedule`), `query` (text matched case-insensitively against every field: issue and pull request numbers, title, repository, status, skill, agent and model, session ID, schedule ID, wait reason, and output), `offset`, `limit` (default 50), all optional.

**Returns:** `jobs`, `total`, `offset`, `limit`. With a `query`, `total` counts the matches and `unfiltered_total` counts the jobs matching `target`, `status`, `states` and `kinds` alone.

A schedule's entry also has:

- `last_run`: its last finished run, with `number`, `status`, `result` (`succeeded`, `failed`, or `fallback` when its precompiled script failed and the model ran it), `summary` (one line of its output), `started`, `finished`, `duration_ms`, and `dismissed` when a failure was dismissed.
- `next_run`: `at`, its next fire; `paused` when the task is paused; `queued` when a fire is waiting on the overlap policy for the run in flight.
- `recent_runs`: up to 20 runs, newest first, in `last_run`'s shape. Each run's `number` is a job `project_job_get` reads, with its transcript.

Each run is still kept as its own job, for run history and usage.

## project_job_get

One job's state, agent session, checkout directory, structured transcript, raw CLI output, and scrollback. A job running on its project's models has `model_source`, `project override` or `from agent <name>`; a job on a scheduled task's own model has none.

**Arguments:** `number`, `target`, `lines` (scrollback lines, default 200, negative for all), `events` (transcript events, default 200, negative for all).

Transcript events have a `kind`: `message`, `reasoning`, `tool_call` (`name`, `input`, `call_id`), `tool_result` (`call_id`, `output`, `is_error`, `images`), `command` (`command`, `output`, `exit_code`, `status` of `running` or `done`), `file_change` (`path`, `diff`), `plan` (`items` with `text` and `status`), `usage` (tokens and cost), `comment_relay` (`author`, `text`), or `system` (claims, pull requests opened, pauses, restarts, failures).

## project_job_explain

Why a job is paused, blocked, waiting, queued, or looks stuck, what will wake it, and its last output.

**Arguments:** `number`, `target`.

## project_job_retry, project_job_pause, project_job_resume, project_job_stop, project_job_dismiss

Retry requeues a job so `gh-fix` reruns it with its saved session. On a paused job it clears the pause first. Pause holds an active, waiting or queued job: a running agent is stopped and its work pushed. Resume clears a pause and requeues the job. Stop stops a job's agent and waits for it to exit, or clears a paused job and releases its claim. Dismiss takes a schedule's failed runs off the `failed` state; its entry still reports the failure as its `last_run` until the next run. Each refuses what the job's status does not allow.

**Arguments:** `number`, `target`.

Merge and discuss jobs, which `project_job_queue` creates, accept the same actions: Pause holds the job in its place in the queue, stopping a running agent; Resume puts it back in its place; Stop takes a waiting job off the queue or stops a running one; Retry queues a finished job again at the end.

## project_job_queue

Queue jobs on demand. `skill: gh-safe-merge` queues pull requests for a safe merge as `merge` jobs, and `skill: gh-discuss` queues Discussion threads for an answer as `discuss` jobs. Each job is a board card with logs, a transcript, and pause, resume, stop and retry. Queued merges run one at a time per repository, in the order given, so each one rebases and rechecks on the latest default branch. Every job still waits for a free agent slot. Queued jobs are saved and survive a restart. A number that a job already owns isn't queued again: its result names the owning job in `owned_by`. While `no_merge` is set, safe merge is refused. `skill: gh-fix` retries an issue's finished job, or checks GitHub now so that a matching issue is dispatched.

**Arguments:** `skill`, `numbers`, `target`, `repository`, `titles`.

## project_queue_candidates

List what a project can queue: open pull requests for `gh-safe-merge`, with CI status, review decision, mergeability, author and the job that owns each, or open Discussion threads for `gh-discuss`. The result's `no_merge` says whether safe merge is turned off. The listing goes through `gh` whatever the project's delivery mode and is reused for 30 seconds. When it can't be fetched, for example because `gh` isn't signed in, the result has an `unavailable` notice instead of a list; `project_job_queue` still queues by number.

**Arguments:** `skill`, `target`, `repository`.

## project_refresh

Check GitHub for work now instead of waiting for the next poll.

**Arguments:** `target` (optional).

## project_add

Watch a project. A saved project is added to its agent's `projects:` list.

**Arguments:**

- `target`: `owner/repo`, a repository URL, a Project board, or a Discussions board.
- `agent` (required unless `runtime` is set): the Aviary agent that owns the project; its model and fallbacks are the jobs' provider and model defaults. A target another agent already owns is refused. For a runtime watch it names the agent the watch runs under; left out, a runtime watch uses the target's owning agent, or `models.defaults`.
- `reports` (optional, saved projects only): send this project's job events through an Aviary agent's channels, as `{agent, targets, events}`. See [Reports](/guide/projects#reports).
- `runtime` (optional): watch until the server stops without writing `aviary.yaml`.
- `mode` (optional): `watch`, the default, picks up open issues matching the filter; `manual` never searches the project for issues. See [Manual mode](/guide/projects#manual-mode). The result reports the `mode`.
- `settings` (optional, runtime only): projects settings that override the shared `projects:` settings while the watch runs. `model` and `fallbacks` set the [job model override](/guide/projects#job-model-override); an unknown provider or a CLI that is not installed fails the call.

The call returns as soon as the project is watched. `project_status` lists it right away, under `loading_targets` until its first poll, while the engine restarts onto it and reads GitHub in the background. Every open control panel shows the new project at once: the server sends a `project-added` event on the `/api/ws` stream, then `project-updated` when the project's issue count or jobs change.

## project_update

Change one watched project. `filter` sets the project's own [issue filter](/guide/projects#issue-filter), replacing the shared `projects.filter` (and `all_issues`) for that project only; `reset_filter` removes it. A new filter is checked first, as `project_filter_check` does, and an invalid one is refused with the reason and nothing saved. A saved project's filter is written to its `projects[].filter` in `aviary.yaml`; a runtime watch's lasts until the watch stops. The project is checked for work right away, and open control panels hear of the change through a `project-updated` event on `/api/ws`. `mode` switches the project between `watch` and `manual` ([Manual mode](/guide/projects#manual-mode)) without a restart: switching to manual stops its issue searches and keeps its jobs, and switching to watch checks it for issues right away. A saved project's mode is written to `projects[].mode`, left out for watch. The result has the effective `issue_filter`, `custom`, `mode`, and `matches` for a new filter.

**Arguments:** `target`, and at least one of `filter`, `reset_filter` or `mode`.

## project_filter_check

Check an issue filter for a project without saving it: its syntax (balanced quotes and parentheses, a value after every qualifier), then a dry-run GitHub search. The result's `matches` is how many open issues of the project it matches; an invalid filter fails with the reason.

**Arguments:** `target`, `filter`.

## project_remove

Stop watching a project, removing it from the agent that owns it. Running jobs are checkpointed first, in the background: the call returns once the project is no longer watched, and the server sends a `project-removed` event on the `/api/ws` stream.

**Arguments:** `target`, and `runtime` (optional) to stop only a runtime watch.

## project_clone_list, project_clone_clear

List every clone Code Loop made for a job, with its repository, issue, pull request and on-disk size, and whether a job still uses it, plus `totalSize` across the listing. Sizes are measured concurrently for at most a few seconds; a clone not measured in time has no `size` and is counted in `unmeasured`. Clear removes the unused ones and reports the space reclaimed.

## project_unblock_run

Run a stuck-job watchdog round now, over every stuck job or the one named.

**Arguments:** `number`, `target`, both optional.

## project_issue_comment

Post an update comment on an issue or pull request. The comment mentions the issue's author and this instance, so the job working it hears it. It never edits the body.

**Arguments:** `number`, `text`, and optional `repository` (defaults to the repository of the job with that number, or the only watched repository), `attachments` and `skip_turn_media`.

Images and videos are uploaded and embedded in the comment. See [Attachments](#attachments).

## project_issue_create

File an issue through the `gh-create` skill with this instance's identity, and return the new issue's URL.

**Arguments:** `text`, and optional `repository`, `model`, `attachments` and `skip_turn_media`.

Images and videos are uploaded and embedded in the issue. See [Attachments](#attachments).

### Attachments

`project_issue_create` and `project_issue_comment` attach screenshots and videos instead of describing them.

- By default they attach the images and videos from the user message that started the turn, such as a screenshot sent over Signal or pasted into web chat. Other files in that message, such as voice notes and documents, are left out and listed in `skipped_turn_media`.
- `attachments` adds local image or video files, as paths or `file://` URLs.
- `skip_turn_media: true` leaves the turn's media out.

GitHub takes PNG, JPEG, GIF, WebP and SVG images up to 10 MB, and MP4, MOV and WebM videos up to 100 MB. If any file is missing, has another type, or is too large, the call fails with an error that names the file and the reason, and nothing is posted. The result's `attachments` lists the files that were attached. Uploading needs push access to the repository.

## Project scheduled tasks

The `project_task_*` tools manage the [scheduled tasks](/guide/scheduled-tasks) of the projects this instance watches, and only theirs: a task of any other project, or one that is not a `project` task, is refused. They are how the [prompt chat](/guide/projects#prompt-chat) manages tasks. They validate, save, preview and run through the same code as `task_schedule`, `task_update`, `task_delete`, `task_preview` and `task_run`, so a task they create looks the same as one made on the Scheduled Tasks page. Every change they make is written to the Code Loop log.

A task is named by `name`, or `<agent>/<task>` as `id` gives it. `project` is needed only when tasks of several watched projects share the name.

- **`project_task_list`**: the tasks of every watched project, or of `project`. Each has `id`, `name`, `agent`, `project`, `enabled` and `paused`, `trigger` (the plain-language schedule), `next_run`, `last_run` (`fire_time`, `status`, `summary`), `precompile` (`enabled`, `compiled`, `verified`, `failures`, `note`), and `source`: `kind` `file` for a Markdown task file in its agent's `tasks/` directory, or `config` for `aviary.yaml`, with its `path`.
- **`project_task_get`**: one task, adding `prompt`, `model`, `send_to`, `trigger_fields` (`schedule`, `trigger`, `every`, `at`, `days`, `timezone`, `start_at`, `run_once`, `overlap`, `missed`), `next_runs`, `runs_per_30_days`, and `recent_runs`, newest first.
- **`project_task_create`**: `project`, `content` (the prompt), a schedule trigger (`schedule`, or `trigger` with `every`, `at`, `days` or `start_at`), and optional `name`, `timezone`, `overlap`, `missed`, `model`, `send_to` and `precompile`. The task belongs to the agent that owns the project; a runtime watch no agent owns is refused. The result's `preview` has `schedule`, `next_runs` and `runs_per_30_days`, and `estimate` has the determinism `verdict` and the `per_run` token and cost estimate, as `task_analyze` reports them. `dry_run: true` returns them without saving. An invalid trigger fails with `task_schedule`'s error.
- **`project_task_update`**: changes only the fields passed, as `task_update` does; an empty string clears one. `new_name` renames the task, `new_project` moves it to another watched project of the same agent, and `enabled` pauses or resumes it. The result's `edit` says how it was saved: a task file is edited directly; a task defined in `aviary.yaml` is saved to a task file that replaces that definition.
- **`project_task_pause`**, **`project_task_resume`**: stop or restart its fires. A run already in flight keeps going; pause it with `project_job_pause`.
- **`project_task_run`**: fires it now, as `task_run` does.
- **`project_task_delete`**: without `confirm`, deletes nothing and returns `removes`, a description of what goes, including its run history, to show before asking for confirmation. With `confirm: true` it deletes the task and its history through `task_delete`, which refuses a task defined in `aviary.yaml`.

## project_issue_backfill_media

Post the screenshots and videos sent with the request that filed each issue, for issues filed before `project_issue_create` attached them. It never edits issue bodies.

**Arguments:** `numbers`, and optional `repository` (defaults to the only watched repository) and `dry_run` (report what would be posted without posting).

Each issue is matched to the user message whose turn called `project_issue_create` and got that issue's URL back, searched across every agent's session history. That message's images and videos (Signal media under `media/incoming/`, or web chat images saved from their data URLs under `media/turn/`) are uploaded and posted as one comment, checked the way [Attachments](#attachments) describes. Unlike `project_issue_comment`, the comment mentions no one, so it does not wake the job or the Code Loop agent. The result lists each issue with a `status`:

- `posted`: the comment was posted; `url` links it and `attachments` lists the files.
- `would_post`: with `dry_run`, the comment would be posted with `attachments`.
- `already_posted`: the issue already has the comment, so running it again posts nothing new.
- `no_media`: nothing to post; `reason` says why, such as no filing call for the issue in session history, a request without images or videos, or media that can no longer be read.
- `failed`: the comment could not be checked or posted; `reason` has the error.

## project_github_read

One `GET` of the GitHub REST API under a watched repository, through `gh` with this instance's sign-in. The Code Loop agent uses it to read commits, comparisons, files, pull requests, comments and Actions runs. Only watched repositories, including the repositories of a watched Project board's jobs, can be read. Each read is written to the Code Loop log. A rate-limited read fails with a message to wait for the limit to reset.

**Arguments:** `path` or `url`, and optional `repository` (defaults to the url's repository or the only watched repository) and `media`.

- `path` is relative to `repos/OWNER/REPO`, such as `commits/SHA`, `compare/BASE...HEAD`, `contents/FILE?ref=REF`, `pulls/N/files`, `issues/comments/ID`, `commits/SHA/check-runs`, `actions/runs?branch=main` or `actions/jobs/ID/logs`. An empty path reads the repository itself.
- `url` is a github.com URL to read instead: a commit, comparison, file, pull request, issue, issue or review comment, review, Actions run or job, or release.
- `media` is `diff` or `patch` for a commit, comparison or pull request diff, or `raw` for a file's text, which is the default for `contents/` paths.

The result has the `repository` and `path` read, and `data` for a JSON answer or `text` for anything else. A body over 64 KB is cut and `truncated` is set. A log keeps its tail, where failures are.

## project_site_fetch

Fetch a page from a watched project's own site to check what is deployed. Allowed sites are each watched repository's homepage, its GitHub Pages site and custom domain, and its owner's `github.io` site. Redirects must stay on those sites. Each fetch is written to the Code Loop log.

**Arguments:** `url`.

The result has the final `url`, `status`, `content_type`, `last_modified`, `etag`, `age` and `date` when the site sends them, and `body`, cut at 64 KB with `truncated` set.



Check whether a repository, Project board or Discussions board can be watched as a project. The add-project dialog runs it as you type. It is read-only and writes nothing.

It reports, as separate checks:

- **github**: `gh` is installed, signed in, and can read the repository (archived repositories fail).
- **fork**: a warning when you can't push to the repository, so pull requests will come from your fork.
- **clis**: which CLI providers (`claude`, `codex`, `gemini`, `opencode`, `cline`) are on `PATH`.
- **skills**: whether `gh-fix` is installed for each of those CLIs the project uses, with its version when the skill declares one. A project uses the CLIs of its job models: `projects.model` and `projects.fallbacks` when `model` is set, else its owning agent's model and fallbacks (after `models.defaults`), the Code Loop chat's `chat_model` and `chat_fallbacks`, the `unblock_agent`, and the `model` of every scheduled task that targets it. A used CLI without the skill fails the check and is named.
- **skills_unused**: an `info` note listing installed CLIs the project does not use that lack `gh-fix`. It never fails the check.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `target` | string | ✓ | `owner/repo`, a GitHub URL, `users/<login>/projects/<n>`, `orgs/<org>/projects/<n>`, or `owner/repo/discussions`. |
| `agent` | string | | The agent that owns, or will own, the project. Empty uses the agent that owns `target`. |
| `models` | string[] | | The owning agent's model and fallbacks, in order, in place of its saved ones, so an unsaved settings form is checked as edited. A `projects.model` override takes their place. |

**Returns:** JSON object. `ok` is false when any check has `status: "fail"`.

```json
{
  "target": "lsegal/zvidlib",
  "repo": "lsegal/zvidlib",
  "kind": "repo",
  "fork": true,
  "ok": true,
  "checks": [
    { "name": "github", "status": "ok", "message": "gh is signed in and can read lsegal/zvidlib" },
    { "name": "fork", "status": "warn", "message": "You can't push to lsegal/zvidlib, so pull requests will come from your fork" },
    { "name": "clis", "status": "ok", "message": "Found claude and codex" },
    { "name": "skills", "status": "ok", "message": "gh-fix 1.6.0 installed for claude" },
    { "name": "skills_unused", "status": "info", "message": "gh-fix also missing for codex (not used)" }
  ],
  "clis": [
    { "name": "claude", "path": "/usr/local/bin/claude", "used": true, "skills": { "gh-fix": "1.6.0" } },
    { "name": "codex", "path": "/usr/local/bin/codex", "used": false, "skills": {} }
  ]
}
```
