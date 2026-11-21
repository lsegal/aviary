# Projects Tools

The `project_*` tools manage [Projects](/guide/projects). Every tool is also an `aviary project` subcommand that takes the same arguments as `--<field> <value>` flags. A leading positional argument fills the subcommand's main field, such as the job number or the target. The tools need a running Aviary server.

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
| `project_refresh` | `aviary project refresh` | |
| `project_add` | `aviary project add` | |
| `project_remove` | `aviary project remove` | destructive |
| `project_clone_list` | `aviary project clone list` | read-only |
| `project_clone_clear` | `aviary project clone clear` | destructive |
| `project_unblock_run` | `aviary project unblock` | |
| `project_issue_comment` | `aviary project issue comment` | |
| `project_issue_create` | `aviary project issue create` | |
| `project_check` | `aviary project check` | read-only |

Settings have no tool of their own. A project lives in the `projects:` list of the agent that owns it in `aviary.yaml` ([`agents[].projects`](/reference/config#agents-projects)), and the settings every project shares live in the top-level `projects:` section ([`projects`](/reference/config#projects)). Both are read and written with `config_get` and `config_save`, and are applied to the running engine when the file changes.

---

## project_status

Instance identity, watched projects with issue counts, job counts, the jobs in each board state (`states`: `starting`, `paused`, `running`, `done`, `failed`), delivery mode, poll interval, ready label, last check, quota per CLI provider, the configured agent specs, and recent log lines.

**Arguments:** `log_offset` (optional): return only the lines logged after this offset. Each response's `log_offset` is the offset to pass next time. `aviary watch` tails the log this way. `targets` (optional): count `states` over only these projects' jobs, as `project_job_list` filters them; the board's stat cards pass the projects they show.

## project_job_list

Every job, running and errored jobs first, with the actions each accepts.

**Arguments:** `target`, `status` (such as `active`, `waiting`, `queued`, `paused`, `failed`, `complete`), `query` (text matched case-insensitively against every field: issue and pull request numbers, title, repository, status, skill, agent and model, session ID, schedule ID, wait reason, and output), `offset`, `limit` (default 50), all optional.

**Returns:** `jobs`, `total`, `offset`, `limit`. With a `query`, `total` counts the matches and `unfiltered_total` counts the jobs matching `target` and `status` alone.

## project_job_get

One job's state, agent session, checkout directory, structured transcript, raw CLI output, and scrollback.

**Arguments:** `number`, `target`, `lines` (scrollback lines, default 200, negative for all), `events` (transcript events, default 200, negative for all).

Transcript events have a `kind`: `message`, `reasoning`, `tool_call` (`name`, `input`, `call_id`), `tool_result` (`call_id`, `output`, `is_error`, `images`), `command` (`command`, `output`, `exit_code`, `status` of `running` or `done`), `file_change` (`path`, `diff`), `plan` (`items` with `text` and `status`), `usage` (tokens and cost), `comment_relay` (`author`, `text`), or `system` (claims, pull requests opened, pauses, restarts, failures).

## project_job_explain

Why a job is paused, blocked, waiting, queued, or looks stuck, what will wake it, and its last output.

**Arguments:** `number`, `target`.

## project_job_retry, project_job_pause, project_job_resume, project_job_stop

Retry requeues a job so `gh-fix` reruns it with its saved session. On a paused job it clears the pause first. Pause holds an active, waiting or queued job: a running agent is stopped and its work pushed. Resume clears a pause and requeues the job. Stop stops a job's agent and waits for it to exit, or clears a paused job and releases its claim. Each refuses what the job's status does not allow.

**Arguments:** `number`, `target`.

## project_refresh

Check GitHub for work now instead of waiting for the next poll.

**Arguments:** `target` (optional).

## project_add

Watch a project. A saved project is added to its agent's `projects:` list.

**Arguments:**

- `target`: `owner/repo`, a repository URL, a Project board, or a Discussions board.
- `agent` (required unless `runtime` is set): the Aviary agent that owns the project; its model and fallbacks are the jobs' provider and model defaults. A target another agent already owns is refused. For a runtime watch it names the agent the watch runs under; left out, a runtime watch uses the target's owning agent, or `models.defaults`.
- `reports` (optional, saved projects only): send this project's job events through an Aviary agent's channels, as `{agent, targets, events}`. See [Reports](/guide/projects#reports).
- `runtime` (optional): watch until the server stops without writing `aviary.yaml`, as `aviary watch` does.
- `settings` (optional, runtime only): projects settings that override the shared `projects:` settings while the watch runs.

## project_remove

Stop watching a project, removing it from the agent that owns it. Running jobs are checkpointed first.

**Arguments:** `target`, and `runtime` (optional) to stop only a runtime watch.

## project_clone_list, project_clone_clear

List every clone Projects made for a job, with its repository, issue, pull request and on-disk size, and whether a job still uses it, plus `totalSize` across the listing. Sizes are measured concurrently for at most a few seconds; a clone not measured in time has no `size` and is counted in `unmeasured`. Clear removes the unused ones and reports the space reclaimed.

## project_unblock_run

Run a stuck-job watchdog round now, over every stuck job or the one named.

**Arguments:** `number`, `target`, both optional.

## project_issue_comment

Post an update comment on an issue or pull request. The comment mentions the issue's author and this instance, so the job working it hears it. It never edits the body.

**Arguments:** `number`, `text`, and `repository` (optional; defaults to the repository of the job with that number, or the only watched repository).

## project_issue_create

File an issue through the `gh-create` skill with this instance's identity, and return the new issue's URL.

**Arguments:** `text`, and optional `repository` and `model`.

## project_check

Check whether a repository, Project board or Discussions board can be watched as a project. The add-project dialog runs it as you type. It is read-only and writes nothing.

It reports, as separate checks:

- **github**: `gh` is installed, signed in, and can read the repository (archived repositories fail).
- **fork**: a warning when you can't push to the repository, so pull requests will come from your fork.
- **ready_label**: the ready label exists, with its count of open issues. Repositories only.
- **clis**: which CLI providers (`claude`, `codex`, `gemini`, `opencode`, `cline`) are on `PATH`.
- **skills**: whether `gh-fix` is installed for each of those CLIs, with its version when the skill declares one.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `target` | string | ✓ | `owner/repo`, a GitHub URL, `users/<login>/projects/<n>`, `orgs/<org>/projects/<n>`, or `owner/repo/discussions`. |
| `ready_label` | string | | Ready label to look for. Defaults to `ready`. |

**Returns:** JSON object. `ok` is false when any check has `status: "fail"`.

```json
{
  "target": "lsegal/zvidlib",
  "repo": "lsegal/zvidlib",
  "kind": "repo",
  "ready_label": "ready",
  "fork": true,
  "ok": true,
  "checks": [
    { "name": "github", "status": "ok", "message": "gh is signed in and can read lsegal/zvidlib" },
    { "name": "fork", "status": "warn", "message": "You can't push to lsegal/zvidlib, so pull requests will come from your fork" },
    { "name": "ready_label", "status": "ok", "message": "\"ready\" exists, with 4 open issues" },
    { "name": "clis", "status": "ok", "message": "Found claude and codex" },
    { "name": "skills", "status": "ok", "message": "gh-fix 1.6.0 installed for all 2" }
  ],
  "clis": [
    { "name": "claude", "path": "/usr/local/bin/claude", "skills": { "gh-fix": "1.6.0" } },
    { "name": "codex", "path": "/usr/local/bin/codex", "skills": { "gh-fix": "1.6.0" } }
  ]
}
```
