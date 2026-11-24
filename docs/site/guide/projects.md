# Code Loop

Code Loop watches GitHub repositories, GitHub Project boards and Discussions boards, and runs a CLI provider such as Codex or Claude Code on every issue that is ready for work. Each job runs the [`gh-fix`](/skills/gh-fix/) skill on an issue, or [`gh-discuss`](/skills/gh-discuss/) on a new Discussion thread. Both are ordinary skills you can also install and run without Aviary; see [Skills](/skills/). The skill does the work: it opens a branch and a draft pull request, implements and tests the fix, repairs CI, and merges. Code Loop decides which issue runs, on which CLI, and when.

[The code loop on GitHub](./code-loop) follows one issue through every stage, from the watch to the merged pull request.

Code Loop needs the [GitHub CLI](https://cli.github.com/) (`gh`) signed in with repository access, at least one CLI provider, and the `gh-fix` skill installed for every CLI the project runs on. CLIs you have installed but the project never uses don't need it.

## Watching a repository

```bash
aviary watch owner/repo
```

[`aviary watch`](./watch) needs no configuration and never changes your Aviary config. It watches the target in a one-off, isolated session until you press Ctrl+C. With no target, the current checkout's `origin` repository is watched.

A target is one of:

- `owner/repo` or a repository URL
- a GitHub Project board URL (user, organization or repository board)
- a Discussions URL, optionally narrowed to one category
- the `projects:` and `discussions:` shorthands, which default to the current checkout's repository

Every target of every agent shares one concurrency limit, and work is drawn from them in turn.

### Watch sessions

`aviary watch` runs its own Aviary server against a temporary config and data directory, `aviary-watch-<PID>-*` in the system temp directory. The session's config, state, sessions, jobs and clone bookkeeping all live there, and nothing is written to your Aviary config directory, even when an Aviary server is already running.

Only two things are shared with your own Aviary, and both are read in place rather than copied into the temporary directory:

- the **browser profile**, so the GitHub sign-in browser delivery uses is reused;
- **provider credentials**: CLI provider sign-ins, `auth:` references and stored tokens. No secret is copied into the session.

The session starts from your providers, browser, approval and Code Loop settings, and the command's flags override the Code Loop settings. Its targets run under `--agent`, else the agent of yours that owns them, else a `watch` agent that takes `models.defaults`. Only the agent's model, fallbacks and permissions come along, never its channels, tasks or other projects. `--model` and the repeatable `--fallback` set the session's [job model override](#job-model-override), which replaces that agent's model and fallbacks for the session's jobs without touching the agent in your config. `claude:opus` is shorthand for `claude/opus`:

```bash
aviary watch owner/repo --model claude:opus --fallback codex/gpt-5.6-terra:high
```

Without the flags, the `AVIARY_WATCH_MODEL` and `AVIARY_WATCH_FALLBACKS` (comma-separated) environment variables set the override, and without those, `projects.model` and `projects.fallbacks` in your `aviary.yaml`. Each model must be a model of a provider reached through a CLI whose CLI is installed; anything else stops the watch with an error naming the model. The session's minimal web UI has the same picker under Code Loop settings.

`--provider` is the old, deprecated name of `--model`. It still works with a warning, will be removed in a later release, and is an error together with `--model`.

**Minimal web UI.** The session's server listens on `localhost` only, on the first free port from your `server.port` (default 16677) up, and prints the URL of its Code Loop board. The web UI asks for no token and shows only **Code Loop** and **Scheduled Tasks**: there is no chat, agent or channel settings, System page, or way to add agents. The server refuses those pages' API calls too, not just the links, and Code Loop Settings can change only the Code Loop settings and scheduled tasks.

**TUI.** In a terminal the session shows a full-screen board: the Scheduled, Starting, Paused, Running, Done and Failed filters (`1`–`6`, `0` for all), scheduled tasks, board mode (`b`), job actions (`r` retry, `p` pause, `u` resume, `s` stop), the selected job's output (`Enter`), live logs (`l`) and the compact stats. `?` lists every key and `q` quits.

**Prompt chat in the TUI.** `c` opens the [prompt chat](#prompt-chat) as a bottom pane under the board, and `/` opens it with the input focused. It is the same session as the web board's prompt chat, so a conversation started in either shows up in the other. `Enter` sends, `Alt+Enter` adds a line (`Shift+Enter` or `Ctrl+J` too where the terminal sends them), `↑`/`↓` recall earlier inputs, `Tab` switches between the input and the transcript, and `Esc` returns to the board, which stays usable while the pane is open; `c` again closes it and restores the board's layout. Replies stream in live, with each tool call on one collapsed line that `e` expands. The transcript follows new output while it is at the bottom; scrolling up (`PgUp`, or `↑`/`k` in the transcript) stops following and shows a **more below** marker until `G` or `End`, or scrolling back down, follows again. Bold, code, lists, quotes and links render for the terminal and wrap to the pane. `#123` references are highlighted: in the transcript `n`/`N` select one and `Enter` focuses its job on the board. `z` makes the chat full screen and `+`/`-` resize the pane. The chat works with `--no-webui` too, and keeps its session in the watch session's temporary directory.

| Flag | Effect |
| --- | --- |
| `--no-webui` | serve no web UI; the TUI or log still runs |
| `--no-tui` | print the Code Loop log instead of the TUI, as happens anyway when output is not a terminal |

**From a source checkout.** `aviary watch` serves the web UI built into the binary. A binary built from a fresh checkout without `pnpm build` has none, and its web UI shows a "Web assets not built" page instead; a build older than the checkout's web sources logs a "web assets are stale" warning. To work on watch mode, run `pnpm dev:watch -- <watch arguments>` instead of `forgo run ./cmd/aviary watch`: it runs the session behind the Vite dev server, so the web UI hot-reloads. See [Developing watch mode](https://github.com/lsegal/aviary/blob/main/CONTRIBUTING.md#developing-watch-mode).

**Cleanup.** Ctrl+C, `SIGTERM`, `SIGHUP`, quitting the TUI, or a fatal error ends the session: running jobs are stopped and checkpointed, then the temporary directory is removed. A directory left behind by a session that was killed outright (`SIGKILL`) or crashed is removed by the next `aviary watch`, once the PID in its name is no longer running; a live session's directory is left alone.

To watch a project permanently, add it to an agent. A project belongs to exactly one Aviary agent: it is listed in that agent's `projects:` list in `aviary.yaml`, and managed on the agent's tab in **Code Loop Settings** (`/projects/settings?agent=<agent>`), where its projects are added and removed and their reports set. The **✕** on a saved project's row there deletes it: it stops watching the project and stops its in-flight jobs, after a confirmation; the repository and clones on disk are left alone. The agent's **Code Loop** page (`/agents/<agent>/projects`) is one board of every project it owns, each job card naming its project, with no settings on it. Each project's own board opens beneath it (`/agents/<agent>/projects/<owner%2Frepo>`) and shows the project's agent, with its Settings button opening the project in Code Loop Settings. A runtime watch no agent owns has its board under System → More (`/system/watches/<owner%2Frepo>`). Or run:

```bash
aviary project add owner/repo --agent coder
```

```yaml
agents:
  - name: coder
    model: cli/claude/opus
    projects:
      - target: owner/repo
```

The settings every project runs under, such as concurrency, the issue filter and merging, are not the agent's: they are one shared set in the top-level `projects:` section, edited on **Code Loop Settings** (`/projects/settings`). See [`projects`](/reference/config#projects) for every setting.

```yaml
projects:
  concurrency: 4   # jobs at once across every agent's projects
```

## Which issues run

A repository issue is picked up when it is open and matches the issue filter (`assignee:@me` by default); no label is needed. The board header shows the filter in use, such as "Issues matching `assignee:@me`", or "All open issues" when `all_issues` drops it. On a project board, the item's status must be the ready state, which is `Todo` or `Ready` unless you set one. Board items move to `In Progress` when work starts and `Done` when it finishes.

- Issues labeled `critical`, then `high`, run before everything else, across all projects.
- An issue with open sub-issues is a tracking issue and is skipped.
- An issue blocked by open dependencies waits. If it is blocked by exactly one issue that already has an open pull request, and the repository allows stacked pull requests, it runs stacked on that pull request.

## CLI providers

A project's jobs run on the agent that owns it. The agent's model and fallbacks are the provider and model defaults of the project's jobs, in fallback order: a job runs on the first one whose quota is not used up. A `<cli>/<model>` model such as `claude/opus` or `codex/gpt-5.6-terra:high` (or the same with a `cli/` prefix, `cli/claude/opus`) runs the job on that CLI. A model that is not a CLI, such as `anthropic/claude-sonnet-5-5`, runs the job on the agent itself, as `aviary/<agent>` below. An agent without a model takes `models.defaults`, and so does an `aviary watch` session whose targets no agent of yours owns, which then needs a CLI model there.

### Job model override

`model` and `fallbacks` in the shared `projects:` settings replace the owning agent's model and fallbacks for every project's jobs. They are models of providers reached through a CLI, such as `claude/opus` (or the shorthand `claude:opus`), tried in order; the CLI of each must be installed, and an unknown provider or a CLI that is not installed is rejected when the settings are saved, by `project_add`, and by `aviary watch`. With `model` empty, jobs run on the owning agent's models as described above, and `fallbacks` alone overrides nothing:

```yaml
projects:
  model: claude/opus
  fallbacks: [codex/gpt-5.6-terra]
```

Code Loop settings has a picker for both, offering the installed CLI providers' models. `project_status` reports each project's effective job model and its source, such as `claude/opus (project override)` or `claude/sonnet (from agent coder)`, and a job's detail shows the same source for a job running on its project's models. The `gh-fix` skills check and the quota display follow the override. The override changes only jobs' models: `chat_model`, `chat_fallbacks` and `unblock_model` keep their own meaning, and a scheduled task's own `model` still wins for its runs.

Built-in definitions ship for `codex`, `claude`, `gemini`, `muse`, `opencode`, `cline` and `agy`. `aviary/<agent>` runs a job on one of your Aviary agents instead, inside the Aviary server rather than as a child process; for the job's duration its file tools and `exec` may use the job's clone directory without a `permissions.filesystem.allowedPaths` entry (the agent still needs a permissions preset that enables file tools). Override a built-in field by field, or add a new CLI, under `models.cli_definitions`; [CLI provider definitions](/reference/cli-definitions) documents the schema: the CLI providers are core Aviary providers, shared with the rest of Aviary rather than owned by Code Loop. A `null` field drops the built-in's value. To run a CLI installed off `PATH`, set its `binary`, such as `models.cli_definitions.claude.binary: /opt/claude/bin/claude`; Settings → Providers & Authentication then reports it installed at that path, and jobs run the same executable.

Jobs run under the Code Loop [approval mode](/reference/config#approval): `projects.approval_mode`, or `approval.mode` when that is unset (`yolo` means `full`). Each CLI's definition translates the mode into the CLI's own flags at launch, and no CLI settings file is touched. Under the default `auto` mode, Codex runs in its `workspace-write` sandbox with network access, so `gh` and `git push` reach GitHub, and Claude Code runs in `auto` permission mode.

`agy` cannot run Code Loop jobs yet: it has no machine-readable output mode, so it cannot produce a structured job transcript. Dispatching a job to it fails with that reason, and `aviary doctor` lists the gate. A definition's `projectsUnavailable` field holds the reason.

Settings → Providers & Authentication shows the CLI providers in its one Providers list, tagged CLI beside the API, OAuth and endpoint providers, with each CLI's install, version, sign-in, quota and models, including CLIs that are not installed; `aviary models clis` (the `provider_cli_list` tool) reports the same, plus any Projects gate.

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

Jobs take instructions only from GitHub comments by allowed commenters on the issue or its pull request. By default the only allowed commenter is the signed-in `gh` user. New comments, edits to the issue description, and closures are relayed into the running session. The job's prompt also names the allowed commenters, so the CLI provider treats comments by anyone else in the thread as untrusted information rather than instructions.

Every Aviary server has an instance identity, an 8-character ID generated on first run and kept in Aviary's state, so a restarted server keeps the same ID and recognizes its own claims. The Code Loop board header and Code Loop Settings both show it. A comment that mentions `@/aviary:<ID>` starts a fresh `gh-fix` pass for that instance. Instances working the same repository coordinate through claim comments on the issue, so two instances never work one issue.

On a finished or paused job, a mention goes to the Code Loop agent instead, as `look at comment <link> on issue OWNER/REPO#N and determine next steps`. The job stays as it is unless the agent decides the comment needs more work and retries or resumes it.

## Queueing jobs on demand

The **Queue** menu in a project's header starts work that no poll picks up on its own:

- **Safe merge PR** opens a picker of the repository's open pull requests, with each one's CI status, review decision, mergeability, author, and the job that already owns it. Picked pull requests become `merge` jobs that run `gh-safe-merge`. They merge one at a time per repository, in the order you picked them, so each one rebases and rechecks on the latest default branch. A merge that's skipped or fails shows its reason on its card.
- **Discuss** opens a picker of open Discussion threads. Picked threads become `discuss` jobs that run `gh-discuss`.
- **Fix issue** takes issue numbers. It retries an issue's finished job, or checks GitHub now so that an issue matching the project's filter is dispatched.

The pull request and Discussion pickers list through the authenticated `gh` CLI whatever the project's delivery mode, and reuse a listing for 30 seconds. Both pickers also take numbers typed into their **Queue PR #…** or **Queue discussion #…** box. When nothing can be listed, for example because `gh` isn't signed in, the picker says so and suggests `gh auth login`, and typed numbers still queue.

Queued jobs show as board cards with logs, a transcript, and pause, resume, stop and retry. They wait for a free agent slot like any other job, and they're saved, so they survive a restart. A number that a job already owns isn't queued again: the picker names the owning job. Safe merge is queued only from this menu, `aviary watch` and the queue tools, not from board cards. While `no_merge` is set, **Safe merge PR** is disabled with an explanation. In `aviary watch`'s TUI, `m` queues the selected job's pull request for a safe merge, and `Q` opens a prompt such as `merge 1201 1207`, `discuss 4` or `fix 12`. Agents and scripts use the `project_job_queue` and `project_queue_candidates` tools.

## Prompt chat

**Prompt chat** on the board opens a side panel, or a bottom drawer on phones (in `aviary watch`'s TUI, a pane opened with `c`: see [Watch sessions](#watch-sessions)), for chatting with the built-in `projects` Aviary agent. Each prompt carries the board's project and a live snapshot of this instance (its watched projects and the agent each belongs to, issue and job counts, and a summary of every job), so a question like "status of issues?" is answered for the board it was opened from. It can retry, pause, resume or stop jobs, run an unblock round, and file issues through `gh-create`. Asked to update an issue or pull request, it posts one comment mentioning the author, which the job working it also hears, and replies with the comment's URL; it never edits an issue's body or title. It also manages the watched projects' scheduled tasks: it lists and reads them, creates one from a request such as "create a scheduled task to do SEO for aviary weekly" (replying with the schedule, its next runs, and the per-run token and cost estimate), and changes, pauses, resumes, runs, or deletes them, asking you to confirm a delete first. Tasks it creates are saved as the Scheduled Tasks page saves them, and each change is written to the Code Loop log. It is focused on the watched projects' Code Loop (jobs, issues, pull requests, commits, CI, releases, deploys and scheduled tasks) rather than restricted to its snapshot: it reads the watched repositories through `gh` with this instance's sign-in (a commit's files and diff, comparisons, files at a ref, pull requests with their reviews, comments and checks, an issue comment by URL or ID, and Actions runs and their logs) and fetches the projects' own sites, such as a repository's homepage or GitHub Pages site, so it can answer "what happened in this commit?" or "did #N deploy?" itself. Each read is written to the Code Loop log. Only the watched repositories and their sites can be read. Its permissions limit it, not a blanket refusal: the built-in agent gets the `project_*` tools, and an agent named `projects` in `aviary.yaml` that grants more tools can use them. It changes code through jobs, by filing an issue for `gh-fix`, and never manages scheduled tasks of projects it does not watch. It may steer clearly unrelated questions elsewhere. Images work in both directions. **Summarize** on a job card asks it to summarize that job's session log.

The agent keeps its conversations as ordinary Aviary sessions; mentions routed to it land in its `main` session. Its model comes from `chat_model` and `chat_fallbacks` in the shared `projects:` settings, or `models.defaults` when unset. An agent named `projects` in `agents:` replaces the built-in one.

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
agents:
  - name: coder
    projects:
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

Each job keeps a structured transcript built from the CLI's machine-readable event stream: messages, reasoning, tool calls and results, shell commands with their output and exit codes, file changes, plans, and token usage. Code Loop adds its own events: claims, pull requests opened, pauses, restarts, and relayed comments. `project_job_get` returns the transcript beside the CLI's raw output. Claude Code (`stream-json`), Codex (`exec --json`) and Gemini CLI (`gemini-json`) are mapped by their output format. opencode, Cline and Muse write generic `jsonl` streams, mapped by the `opencode-json`, `cline-json` and `muse-json` transcript mappers their definitions name in [`output.transcript`](/reference/cli-definitions#output). `agy` has no machine-readable output, so Code Loop does not dispatch jobs to it. A custom definition whose stream has no mapper records only its raw output and Projects' own events.

## Scheduled prompts

A project can also run prompts on a timer, such as a nightly triage or a weekly dependency update. They are Aviary scheduled tasks of type `project`, listed under the agent's **Scheduled Tasks** and on the project's board, where **Schedule** creates one. Each shows on the board as one Scheduled card that its runs update in place, and the card's gear opens the task's settings. See [Scheduled prompts](./scheduled-tasks#scheduled-prompts).

## Managing projects

Every Code Loop action is an MCP tool, and the `aviary project` command mirrors each one with the same arguments. See [Code Loop tools](/reference/mcp/projects).

```bash
aviary project status
aviary project job list --status active
aviary project job get 42
aviary project job explain 42
aviary project job stop 42
aviary project clone clear
```

Jobs reuse clones. A job's clone stays on disk after the job ends, and the next job on the same repository is handed a free one instead of cloning and installing dependencies again. Before the hand-off the clone is fetched and reset to the latest default branch: its other branches, local git config, stashes, hooks, and untracked and ignored files are removed, except `node_modules` and `.venv` dependency directories. A clone is free when no running, queued, waiting or paused job holds it, the same decision `project_clone_list` reports; jobs on the same repository that run at once each get their own clone. The job detail view shows the job's clone path with a copy button, or says when the job has no clone yet or its clone was removed or handed to another job. `aviary project clone clear` removes the clones no job holds.

Code Loop state, including handled issues, sessions and job transcripts, lives in the `projects/` directory of Aviary's data directory.
