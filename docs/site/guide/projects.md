# Projects

Projects watches GitHub repositories, GitHub Project boards and Discussions boards, and runs a coding CLI such as Codex or Claude Code on every issue that is ready for work. Each job runs the `gh-fix` skill on an issue, or `gh-discuss` on a new Discussion thread. The skill does the work: it opens a branch and a draft pull request, implements and tests the fix, repairs CI, and merges. Projects decides which issue runs, on which CLI, and when.

Projects needs the [GitHub CLI](https://cli.github.com/) (`gh`) signed in with repository access, at least one coding CLI, and the `gh-fix` skill installed for that CLI.

## Watching a repository

```bash
aviary watch owner/repo
```

`aviary watch` needs no configuration and writes nothing to `aviary.yaml`. It watches the target until you press Ctrl+C. When the Aviary server is running, the watch is added to it. Otherwise the server starts in the foreground for the watch. With no target, the current checkout's `origin` repository is watched.

A target is one of:

- `owner/repo` or a repository URL
- a GitHub Project board URL (user, organization or repository board)
- a Discussions URL, optionally narrowed to one category
- the `projects:` and `discussions:` shorthands, which default to the current checkout's repository

Several targets share one concurrency limit, and work is drawn from them in turn.

To watch a project permanently, add it to the `projects:` section of `aviary.yaml`, or run:

```bash
aviary project add owner/repo
```

## Which issues run

A repository issue is picked up when it is open, carries the ready label (`ready` by default), and matches the issue filter (`assignee:@me` by default). On a project board, the item's status must be the ready state, which is `Todo` or `Ready` unless you set one. Board items move to `In Progress` when work starts and `Done` when it finishes.

- Issues labeled `critical`, then `high`, run before everything else, across all projects.
- An issue with open sub-issues is a tracking issue and is skipped.
- An issue blocked by open dependencies waits. If it is blocked by exactly one issue that already has an open pull request, and the repository allows stacked pull requests, it runs stacked on that pull request.

## Coding CLIs

Jobs run on the coding CLIs listed in `projects.agents` (default `codex`). Each entry is an `AGENT/MODEL:LEVEL` spec, such as `codex`, `claude/opus` or `codex/gpt-5.6-terra:high`. Repeating an agent balances load across the entries. A project can list its own agents, which act as a fallback order: a job runs on the first one whose quota is not used up.

Built-in definitions ship for `codex`, `claude`, `gemini`, `muse`, `opencode`, `cline` and `agy`. `aviary/<agent>` runs a job on one of your Aviary agents instead; for the job's duration its file tools and `exec` may use the job's clone directory without a `permissions.filesystem.allowedPaths` entry (the agent still needs a permissions preset that enables file tools). Override a built-in field by field, or add a new CLI, under `models.cli_definitions`: the coding CLIs are core Aviary providers, shared with the rest of Aviary rather than owned by Projects. A `null` field drops the built-in's value.

Without `yolo`, Codex runs in its `workspace-write` sandbox with network access, so `gh` and `git push` reach GitHub, and Claude Code runs in `auto` permission mode.

`agy` cannot run Projects jobs yet: it has no machine-readable output mode, so it cannot produce a structured job transcript. Dispatching a job to it fails with that reason, and `aviary doctor` lists the gate. A definition's `projectsUnavailable` field holds the reason.

Settings → Providers lists every CLI with its install, version, sign-in, quota and models, including CLIs that are not installed; `aviary models clis` (the `provider_cli_list` tool) reports the same, plus any Projects gate.

### Deferred turns

A definition's `wakeup` field describes how its CLI ends a turn asking to be woken later, such as Claude Code's `ScheduleWakeup` tool. When the CLI exits with one pending, the job shows as waiting with its reason and resume time, then resumes the same session with the agent's own prompt. Stopping the job or closing its issue cancels the wakeup, and a run stops after 48 wakeups in a row. A CLI without a `wakeup` field just finishes its turn.

Every value is a field path in the CLI's JSON event stream: dot-separated keys, with `[]` stepping into each element of an array.

| Field | Read from | Description |
| --- | --- | --- |
| `type`, `event` | the event | Path to the event's type, and the type that carries the call; empty matches every event |
| `calls` | the event | Path to each call the event carries; empty reads the event itself |
| `name`, `tool` | each call | Path to the tool name, and the name that marks a wakeup (required) |
| `id` | each call | Path to the call's id, matched by `rejected` |
| `input` | each call | Path to the call's input; empty reads the call itself |
| `delay` | the input | Delay in seconds, clamped to 1 minute through 1 hour (required) |
| `prompt`, `reason` | the input | The prompt the session resumes with, and the reason shown while it waits |
| `stop` | the input | A boolean that cancels the pending wakeup instead of scheduling one |
| `rejected` | | `{event, results, id, error}`: where a failed call's result is reported, which cancels it; needs `id` |

```yaml
models:
  cli_definitions:
    acme:
      wakeup:
        type: kind
        event: defer
        name: kind
        tool: defer
        input: args
        delay: after
        prompt: then
        reason: why
```

## Steering jobs

Jobs take instructions only from GitHub comments by allowed commenters on the issue or its pull request. By default the only allowed commenter is the signed-in `gh` user. New comments, edits to the issue description, and closures are relayed into the running session. The job's prompt also names the allowed commenters, so the coding CLI treats comments by anyone else in the thread as untrusted information rather than instructions.

Every Aviary server has a random instance identity. A comment that mentions `@/aviary:<ID>` starts a fresh `gh-fix` pass for that instance. Instances working the same repository coordinate through claim comments on the issue, so two instances never work one issue.

On a finished or paused job, a mention goes to the Projects agent instead, as `look at comment <link> on issue OWNER/REPO#N and determine next steps`. The job stays as it is unless the agent decides the comment needs more work and retries or resumes it.

## Prompt chat

**Prompt chat** on the board opens a side panel, or a bottom drawer on phones, for chatting with the built-in `projects` Aviary agent. It answers from live state, and can retry, pause, resume or stop jobs, run an unblock round, and file issues through `gh-create`. It is limited to the `project_*` tools, cannot comment on issues or otherwise instruct a running job, and refuses anything outside this instance. Images work in both directions. **Summarize** on a job card asks it to summarize that job's session log.

The agent keeps its conversations as ordinary Aviary sessions; mentions routed to it land in its `main` session. Its model comes from `projects.chat_model` and `projects.chat_fallbacks`, or `models.defaults` when unset. An agent named `projects` in `agents:` replaces the built-in one.

## Keeping jobs moving

- A run that exits while its issue is still open is resumed in the same session, or restarted when the session is gone.
- Those resumes back off, doubling from 30 seconds up to 15 minutes. After four resumes in a row that leave no new commits, pull requests, or comments on GitHub, the job is paused instead, with the reason in the job list and the log. Like a job the owner paused, it stays paused until you retry it; the stuck-job watchdog leaves it alone.
- Work waiting on GitHub, such as an open dependency or a stacked blocker, is paused and gives up its slot until it is unblocked.
- A pull request closed without merging restarts the work.
- The stuck-job watchdog runs every 15 minutes (`unblock_interval`) and runs one agent call that unblocks paused and silent jobs.
- On shutdown, and before a safe restart or upgrade (`server_restart`, `server_upgrade`), every running job's work is committed and pushed before its agent stops; the jobs resume in their sessions afterwards and are told that Aviary restarted. An upgrade that waits for idle (`server_upgrade { when: "idle" }`) dispatches no new jobs while it waits. Every subprocess dies with Aviary, even on `SIGKILL`.

## Reports

A project can send a short report through an Aviary agent's channels when one of its jobs finishes (`merged`), fails (`failed`), or starts waiting on CI, a wakeup, or a blocker (`waiting`). Reports are off by default. Turn them on per project by naming the agent and where to send:

```yaml
projects:
  list:
    - target: owner/repo
      reports:
        agent: assistant
        targets: [slack:main:C0123456789, session:projects]
        events: [merged, failed]   # optional; all events when left out
```

Targets use the same form as a scheduled task's target ([delivery targets](/guide/channels#delivery-targets)), and the agent must have the channel configured. `project_add` takes the same `reports` object when it saves a project.

## Delivery

`poll` (the default) reads GitHub through `gh` every `interval` (30s). `webhook` starts an ngrok tunnel, creates repository webhooks that point at it and removes stale ones, and still syncs periodically as a fallback. Every GitHub read has a timeout, so a hung request never stops the watch.

## Job transcripts

Each job keeps a structured transcript built from the coding CLI's machine-readable event stream: messages, reasoning, tool calls and results, shell commands with their output and exit codes, file changes, plans, and token usage. Projects adds its own events: claims, pull requests opened, pauses, restarts, and relayed comments. `project_job_get` returns the transcript beside the CLI's raw output. Claude Code (`stream-json`) and Codex (`exec --json`) are mapped today. Other CLIs record their raw output and Projects' own events.

## Scheduled prompts

A project can also run prompts on a timer, such as a nightly triage or a weekly dependency update. They are Aviary scheduled tasks of type `project`, managed in the project's **Schedules** tab, and each run shows on the board as a Scheduled job. See [Scheduled prompts](./scheduled-tasks#scheduled-prompts).

## Managing projects

Every Projects action is an MCP tool, and the `aviary project` command mirrors each one with the same arguments. See [Projects tools](/reference/mcp/projects).

```bash
aviary project status
aviary project job list --status active
aviary project job get 42
aviary project job explain 42
aviary project job stop 42
aviary project clone clear
```

Projects state, including handled issues, sessions and job transcripts, lives in the `projects/` directory of Aviary's data directory.
