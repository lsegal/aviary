# CLI Reference

## Global Flags

These flags apply to every `aviary` command.

| Flag | Default | Description |
| --- | --- | --- |
| `--config <path>` | `~/.config/aviary/aviary.yaml` | Path to the config file |
| `--data-dir <path>` | `~/.config/aviary` | Data directory |
| `--server <url>` | `https://localhost:16677` | Aviary server URL |
| `--token <token>` | _(stored token)_ | Authentication token (overrides stored token) |

---

## aviary serve

Start the Aviary server over HTTPS on the configured port (default: 16677).

```
aviary serve [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-d, --daemon` | `false` | Run in the background |
| `--trace-startup` | `false` | Print the per-phase startup trace once every subsystem has finished initializing |

The HTTP server and `/mcp` accept requests as soon as config and state are loaded, with a target of under 2 seconds on a warm restart. Channel connections, the skills Aviary ships, and Code Loop discovery initialize afterwards in the background. Until a subsystem is ready, MCP tools that need it answer with an "initializing, retry in Ns" error. `GET /healthz` answers while the process is serving, and `GET /readyz` reports each subsystem's readiness and the boot's phase timings, answering 503 until every subsystem is ready.

---

## aviary stop

Stop the running Aviary server.

```
aviary stop
```

No flags.

---

## aviary service status

Show whether the Aviary server is currently running.

```
aviary service status
```

No flags.

---

## aviary logs

Tail Aviary server logs from the filesystem.

```
aviary logs [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `-f, --follow` | `true` | Follow log output as it is written |
| `-n, --lines <n>` | `200` | Number of trailing lines to show before following |

---

## aviary token

Show or regenerate the Aviary server authentication token. The token is used to authenticate with the Aviary web UI and API.

```
aviary token [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--new` | `false` | Generate and store a new token, replacing the existing one |

---

## aviary ui

Open the Code Loop board in the server's web UI. With a project argument such as `owner/repo`, open that project's board. Uses the server address from `--server`.

```
aviary ui [project]
```

---

## aviary doctor

Validate `aviary.yaml` and verify that required credentials exist in the auth store. Exits with status 1 if any errors are found.

The `System information` block starts with the Aviary version and an `Install type` line naming how the binary was built:

- `release build <version>`: a release binary, which upgrades from GitHub releases.
- `dev build from the source checkout at <dir>`: a dev build that found its checkout, which upgrades with `git pull`.
- `dev build without a source checkout`: a dev build with no checkout to pull, such as `go run` without `AVIARY_CHECKOUT`, or a dev build inside Docker.

When the server is running, a `Startup` line summarizes its latest boot: total boot time, when `/mcp` became ready, any subsystems still initializing, and the slowest phases.

The `Orphaned sessions` block lists task and channel sessions whose task, workflow or channel was deleted without releasing them, which would otherwise show under Chats as if their owner still existed. Each counts as a warning. `--fix` archives them as regular chats titled `<name> (archived)`.

The `Browser profile` block reports the lock on the browser `profile_directory`. When a running Aviary instance holds it, the block names that instance: its PID, host, start time, config directory, and DevTools endpoint. Stop that instance to free the profile. The lock is held by the operating system, so it never outlives the process that took it. A lock record left by a run that exited is cleared when the doctor runs.

```
aviary doctor [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--disable-version-check` | `false` | Skip the GitHub release version check and upgrade prompt |
| `--fix` | `false` | Archive orphaned sessions as regular chats |

---

## aviary upgrade

Upgrade Aviary to the latest release, or to `--version`. Does nothing when Aviary is already at that version. See [Upgrades](../guide/operations.md#upgrades) for how upgrades restart the server and resume jobs.

```
aviary upgrade [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--version <version>` | _(latest release)_ | Upgrade to a specific release instead of the latest release, with or without a leading `v` |
| `--when <now\|idle>` | `now` | When the running server restarts: `now`, or `idle` (once no jobs are running, holding new jobs until then) |
| `--release` | `false` | Upgrade from GitHub releases, even on a dev build |
| `--source` | `false` | Upgrade by pulling the source checkout, even on a release build |
| `--verbose` | `false` | Explain how the install type was detected |

`--release` and `--source` exclude each other, and `--source` rejects `--version`, which picks a release.

Without either flag, the mode follows the install type `aviary doctor` shows, never the working directory. The first line of output names it, such as `Installed release v0.7.3 at /usr/local/bin/aviary.` or `Running a dev build of 588e26e2abcd from the source checkout at ~/src/aviary.`; `--verbose` adds the reasons: the stamped version, the embedded VCS revision, and whether `AVIARY_CHECKOUT` or the executable's location found the checkout.

- **Release mode** (a release build, or `--release`) prints `Aviary v0.7.4 is up to date.` when there is nothing to install, or `Upgrading v0.7.3 → v0.7.5` and then `Aviary upgraded to v0.7.5.` once the release replaces the binary. `--release` on a dev build installs the latest release.
- **Source mode** (a dev build running from its checkout, or `--source`) fetches the checkout's upstream and prints what it compared, such as `Fetched origin/main: local 588e26e2 vs origin/main 9f1c0d3a (0 ahead, 2 behind).`, then runs `git pull --ff-only` and prints `Pulled N commit(s) into <dir> (<from> → <to>).`, or `The source checkout at <dir> is already up to date with origin/main (<sha>).`. On a dev build it then says whether the running binary was built from the checkout's HEAD; when it is stale it prints the `go build` command that rebuilds it, and it offers `aviary upgrade --release` to switch to the release binary. It never pulls over local changes or a diverged branch. The checkout is the one holding the binary or named by `AVIARY_CHECKOUT`; with no other checkout known, `--source` uses the checkout containing the working directory. See [Dev checkouts](../guide/operations.md#dev-checkouts).

A dev build with no source checkout cannot check for updates and reports that, unless `--version` or `--release` names a release.

When the server is running and is the same kind of install, the upgrade goes through it as a safe restart (`server_upgrade`): running jobs stop at a checkpoint and resume after the restart, and `--when idle` waits until no jobs are running. In source mode the server must also run from the same checkout. Otherwise the command upgrades on its own and leaves a server of the other kind alone, printing which command upgrades it (`aviary upgrade --release` or `aviary upgrade --source`).

---

## aviary version

Show the Aviary version.

```
aviary version
```

No flags. The output is one line:

```
aviary version v0.7.5
```

---

## aviary deps

Show or install the external programs voice transcription uses. See [Voice Messages](../guide/voice.md).

### aviary deps status

Show whether `whisper-cli`, the configured whisper model, and `ffmpeg` are installed, with their paths.

```
aviary deps status
```

### aviary deps install

Install what is missing: `whisper-cli` and `ffmpeg` through Homebrew (or winget on Windows), and the configured whisper model, downloaded to `deps/whisper/` under Aviary's data directory. Exits with an error naming anything it could not install.

```
aviary deps install
```

---

## aviary install-skills

Install the skills Aviary ships (`gh-create`, `gh-discuss`, `gh-fix` and `gh-safe-merge`) globally with `npx skills` (skills.sh): into `~/.agents/skills` and the skills directory of every CLI provider on the `PATH`. A release installs them from its own tag; a development build installs the copies bundled with it. Skills already installed are refreshed, so running it again is safe. Aviary runs the same install on its first launch. Requires Node.js.

```
aviary install-skills
```

No flags.

---

## aviary configure

Interactive configuration wizard for initial setup and ongoing configuration. Covers providers, agents, server settings, and more.

```
aviary configure [subcommand]
```

Running `aviary configure` with no subcommand runs the full onboarding wizard.

### aviary configure providers

Authenticate with AI providers via OAuth or API key. Alias: `auth`.

```
aviary configure providers
```

### aviary configure general

Configure shared runtime settings.

```
aviary configure general
```

### aviary configure agents

Add, view, or remove agents interactively.

```
aviary configure agents
```

### aviary configure skills

Enable and configure installed skills.

```
aviary configure skills
```

### aviary configure server

Configure server port and TLS options.

```
aviary configure server
```

### aviary configure browser

Configure browser automation settings.

```
aviary configure browser
```

### aviary configure scheduler

Configure task concurrency.

```
aviary configure scheduler
```

---

## aviary agent

Manage agents.

```
aviary agent <subcommand>
```

Agent-scoped commands take the agent's name, which may be omitted when only one agent exists (the built-in Code Loop agent does not count): the command then acts on that agent. With several agents, omitting it is an error that lists the agents to choose from.

### aviary agent list

List all configured agents and their current state.

```
aviary agent list
```

### aviary agent run

Send a message to an agent and stream the response.

```
aviary agent run [name] [message] [flags]
```

With a single agent, `aviary agent run "hello"` sends `hello` to it; with `--file`, the name alone may be omitted too.

| Flag | Default | Description |
| --- | --- | --- |
| `--file <path>` | | Read the prompt from a file instead of inline |
| `--bare` | `false` | Run without any system prompt, rules, memory, or tool preamble |
| `--history` | `true` | Include prior session history in the prompt |
| `--output-format <format>` | `text` | `text` prints the reply; `json` prints one JSON event per line as the run goes: `message`, `tool_call`, `tool_result` (paired by `id`), `file_change`, `media`, and `error` |
| `--workdir <dir>` | | Grant the run's file tools and `exec` this directory for the run only, without a `permissions.filesystem.allowedPaths` entry |

### aviary agent stop

Immediately stop all work in progress for an agent.

```
aviary agent stop [name]
```

### aviary agent template-sync

Sync embedded template files into an agent directory. Missing files are added; existing files are preserved.

```
aviary agent template-sync [name]
```

---

## aviary task

Manage scheduled tasks.

```
aviary task <subcommand>
```

### aviary task list

List all tasks, their trigger type, and last run status.

```
aviary task list
```

### aviary task run

Manually trigger a task immediately.

```
aviary task run <name>
```

### aviary task stop

Stop all currently running scheduled task jobs.

```
aviary task stop
```

### aviary task rm

Delete a scheduled task. Its session becomes a regular chat titled `<name> (archived)` and its run history stays on disk, unless `--delete-data` deletes both.

```
aviary task rm <name> [--delete-data]
```

---

## aviary watch

Watch GitHub projects and fix ready issues with a CLI provider, in a one-off session isolated from your Aviary config. The session runs its own server against a temporary config and data directory that is removed when it ends; only the browser profile and provider credentials are shared, read in place. See [aviary watch](/guide/watch) and [Watch sessions](/guide/projects#watch-sessions).

```
aviary watch [TARGET ...] [--agent NAME] [--model M] [--fallback M ...]
             [--concurrency N] [--interval D]
             [--delivery browser|poll|webhook] [--ready-state S]
             [--filter F ...] [--all-issues] [--manual] [--allowed-commenters LOGINS]
             [--yolo] [--no-merge] [--no-changelog]
             [--browser-vision] [--browser-visible] [--no-webui] [--no-tui]
```

With no `TARGET`, the current checkout's `origin` repository is watched. `--agent` runs the targets under an Aviary agent, whose model and fallbacks pick the jobs' CLI providers; without it a target runs under the agent that owns it, or `models.defaults`. `--model` sets the session's [job model override](/guide/projects#job-model-override), the shared `projects.model`, in place of that agent's model, and each `--fallback`, in the order given, sets `projects.fallbacks`. Without them, `AVIARY_WATCH_MODEL` and the comma-separated `AVIARY_WATCH_FALLBACKS` set them, and without those, `projects.model` and `projects.fallbacks` in your `aviary.yaml`. Models are `<provider>/<model>`, such as `claude/opus`, or the shorthand `claude:opus`, of a provider reached through a CLI that is installed; anything else is an error. `--provider` is a deprecated name for `--model`: it still works, prints `--provider is deprecated; use --model`, and is an error together with `--model`. Flags override the shared `projects:` settings for the session.

The session serves a minimal web UI, Code Loop and Scheduled Tasks only with no token prompt, on the first free `localhost` port from `server.port` up, and shows a TUI board in the terminal. The TUI's prompt chat (`c`, or `/` to type) talks to the Code Loop agent in the same session as the web board's prompt chat; `?` lists its keys. `--no-webui` serves no web UI; `--no-tui` prints the Code Loop log instead of the TUI. `--manual` starts the targets in [manual mode](/guide/projects#manual-mode): the session never searches them for issues, so jobs start only from queued work, scheduled prompts and agent tools, and the board's eye button switches a target back to watch mode. Ctrl+C, `SIGTERM` or `SIGHUP` stops and checkpoints running jobs, then removes the temporary directory; directories left by killed sessions are swept on the next start.

---

## aviary project

Manage Code Loop. Each subcommand calls the `project_*` tool of the same name with the same arguments, given as `--<field> <value>` flags; a leading positional argument fills the main field. See [Code Loop tools](/reference/mcp/projects).

```
aviary project status
aviary project add <target> [--agent NAME] [--runtime] [--mode watch|manual]
aviary project remove <target> [--runtime] [--delete_data]
aviary project update <target> --filter "label:bug -label:wip" | --reset_filter true | --mode watch|manual
aviary project filter check <target> --filter FILTER
aviary project refresh [target]
aviary project job list [--target T] [--status S] [--offset N] [--limit N]
aviary project job get|explain|retry|pause|resume|stop <number> [--target T]
aviary project job queue <gh-safe-merge|gh-discuss|gh-fix> --numbers N,N [--target T]
aviary project queue candidates <gh-safe-merge|gh-discuss> [--target T]
aviary project clone list|clear
aviary project unblock [number] [--target T]
aviary project issue comment <number> --text TEXT [--repository R]
aviary project issue create --text TEXT [--repository R] [--model M]
```

`project remove --delete_data` also deletes the project's job records, job transcripts and the clones no job still uses, once its jobs have stopped; without it they stay on disk.

---

## aviary job

View job history and logs.

```
aviary job <subcommand>
```

### aviary job list

Show job history across all tasks.

```
aviary job list [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--task <name>` | | Filter results to a specific task name |

### aviary job logs

Stream logs for a specific job run.

```
aviary job logs <job-id>
```

---

## aviary auth

Manage authentication credentials.

```
aviary auth <subcommand>
```

### aviary auth login

Authorize with an AI provider. Supported providers: `anthropic`, `openai`, `gemini`, `github-copilot`.

```
aviary auth login <provider> [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--code <code>` | | Authorization code (skips interactive browser prompt) |
| `--pat <token>` | | GitHub personal access token for `github-copilot` (skips device flow) |

### aviary auth set

Store a credential (API key or token) by name.

```
aviary auth set <name> <value>
```

### aviary auth get

Show whether a credential is stored (value is masked).

```
aviary auth get <name>
```

### aviary auth list

List all stored credential names.

```
aviary auth list
```

### aviary auth delete

Remove a stored credential.

```
aviary auth delete <name>
```

---

## aviary browser

Control a Chromium browser via the Chrome DevTools Protocol (CDP). All subcommands share the following persistent flags.

```
aviary browser <subcommand> [flags]
```

**Persistent flags:**

| Flag | Default | Description |
| --- | --- | --- |
| `--browser-binary <path>` | _(auto-detected)_ | Path to a Chromium or Chrome binary |
| `--cdp-port <port>` | `9222` | Chrome DevTools Protocol debugging port |
| `--profile-directory <name>` | | Chrome profile directory name (e.g. `Default`, `Work`) |
| `--tab <id>` | | CDP tab ID returned by `browser open` |

### aviary browser open

Open a URL in a new browser tab. Prints the tab ID for use with `--tab`.

```
aviary browser open <url>
```

### aviary browser tabs

List all open browser tabs.

```
aviary browser tabs
```

### aviary browser navigate

Navigate an existing tab to a new URL.

```
aviary browser navigate <url>
```

### aviary browser click

Click an element by CSS selector.

```
aviary browser click [flags]
```

| Flag | Description |
| --- | --- |
| `--selector <selector>` | CSS selector of the element to click |

### aviary browser type

Send keystrokes into an element.

```
aviary browser type [text] [flags]
```

| Flag | Description |
| --- | --- |
| `--selector <selector>` | CSS selector of the target element |
| `--text <text>` | Text to type |

### aviary browser fill

Fill text into an element (clears existing value first).

```
aviary browser fill [text] [flags]
```

| Flag | Description |
| --- | --- |
| `--selector <selector>` | CSS selector of the target element |
| `--text <text>` | Text to fill |

### aviary browser screenshot

Capture a screenshot of the current tab as a PNG.

```
aviary browser screenshot
```

### aviary browser wait

Wait for an element to become visible.

```
aviary browser wait [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--selector <selector>` | | CSS selector to wait for |
| `--timeout-ms <ms>` | `10000` | Wait timeout in milliseconds |

### aviary browser text

Extract normalized page text.

```
aviary browser text [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--selector <selector>` | | Optional CSS selector to scope extraction |
| `--max-length <n>` | `4000` | Maximum number of characters to return |

### aviary browser query

Extract structured element data by CSS selector.

```
aviary browser query [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--selector <selector>` | | CSS selector |
| `--count <n>` | `20` | Maximum number of elements to return |
| `--max-length <n>` | `500` | Maximum text length per element |
| `--include-html` | `false` | Include outer HTML for each element |

### aviary browser eval

Evaluate a JavaScript expression in the current tab.

```
aviary browser eval <expr>
```

---

## aviary models

Inspect supported provider and model pairs.

```
aviary models [flags]
aviary models list [flags]
aviary models clis
```

| Flag | Description |
| --- | --- |
| `--provider <name>` | Filter results to a specific provider name |
