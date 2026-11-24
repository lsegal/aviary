# aviary watch

`aviary watch` runs a portable, no-configuration [Code Loop](./projects) instance for one or more repositories. It needs nothing but the `aviary` binary, your `gh` sign-in and the CLI providers you already use, and it never changes your Aviary config. It serves the Code Loop web UI like any Aviary server, and in a terminal it also shows a full-screen TUI board with a prompt chat, so you can follow and steer the work without leaving the terminal.

## Install

Install the `aviary` binary as [Getting Started](./getting-started#install) describes, for example with Homebrew:

```bash
brew tap lsegal/aviary https://github.com/lsegal/aviary
brew install aviary
```

`aviary watch` also needs:

- the [GitHub CLI](https://cli.github.com/) (`gh`) signed in with access to the repository;
- at least one CLI provider, such as Claude Code or Codex, signed in;
- the [`gh-fix`](/skills/gh-fix/) skill installed for each CLI the jobs run on. See [Skills](/skills/).

You do not need to run `aviary serve`, write an `aviary.yaml` or add an agent first.

## Watch a repository

```bash
aviary watch USER/REPO
```

The session watches `USER/REPO` until you press Ctrl+C or quit the TUI. With no target, the current checkout's `origin` repository is watched. A target can also be a repository URL, a GitHub Project board URL, or a Discussions URL; see [Watching a repository](./projects#watching-a-repository).

On start, the session prints the URL of its web UI's Code Loop board, on `localhost` on the first free port from 16677 up, and opens the TUI in the terminal. Both show the same jobs, and you can use either or both at once.

Common flags:

| Flag | Effect |
| --- | --- |
| `--model claude:opus` | the CLI provider and model jobs run on; `--fallback` adds fallbacks |
| `--concurrency N` | jobs at once |
| `--filter F` | the issue filter, `assignee:@me` by default |
| `--no-merge` | stop each job at a ready pull request instead of merging it |
| `--no-webui` | serve no web UI; the TUI still runs |
| `--no-tui` | print the Code Loop log instead of the TUI |

[`aviary watch`](/reference/cli#aviary-watch) in the CLI reference lists every flag.

## The TUI board

The TUI shows the watch's header (its targets and the web UI's URL), a stats line (busy slots, each state's count, pull requests merged in the last day, and how issues arrive, such as `poll every 1m`), the filters, and the jobs, either as a list or as a board with a column per state: Scheduled, Starting, Paused, Running, Done and Failed. Each job shows its issue, CLI and model, task progress, pull request and wait reason. The selected job's output pane shows its latest tool calls and output.

### Key bindings

| Key | Action |
| --- | --- |
| `0`–`6` | filter: all, scheduled, starting, paused, running, done, failed |
| `f` / `F` | next / previous filter |
| `b` | toggle board / list |
| `↑` `↓` / `j` `k` | select a job |
| `←` `→` / `Tab` | board column |
| `g` / `G` | first / last job |
| `Enter` | toggle the selected job's output |
| `l` | toggle the log pane; `PgUp` / `PgDn` scroll it |
| `r` / `p` / `u` | retry / pause / resume the selected job |
| `s` | stop it, after a confirmation |
| `d` | dismiss a failed scheduled run |
| `m` | queue the selected job's pull request for [`gh-safe-merge`](/skills/gh-safe-merge/) |
| `Q` | queue jobs, such as `merge 1201 1207`, `discuss 4` or `fix 12` |
| `c` | open / close the prompt chat |
| `/` | open the prompt chat and type |
| `?` | list every key |
| `q` | quit |

## Prompt chat

`c` opens the [prompt chat](./projects#prompt-chat) as a pane under the board, and `/` opens it with the input focused. Ask the Code Loop agent about a job, retry or stop jobs, or file a new issue. It can also read the watched repositories' commits, pull requests, comments and Actions runs, and fetch their deployed sites, so you can ask what a commit changed or whether a fix deployed. It is the same conversation as the web board's prompt chat, which the session's web UI shows too, so a conversation started in either shows up in the other.

| Key | Action |
| --- | --- |
| `Enter` | send |
| `Shift+Enter` | new line (`Alt+Enter` or `Ctrl+J` where the terminal can't report `Shift+Enter`) |
| `↑` / `↓` | earlier / later inputs |
| `Tab` | switch between the input and the transcript |
| `Esc` | back to the board, which stays usable while the pane is open |
| `n` / `N`, `Enter` | in the transcript, select a `#123` reference and focus its job on the board |
| `e` | expand / collapse tool calls |
| `z` | full screen chat |
| `+` / `-` | taller / shorter chat pane |

Replies stream in live, with each tool call on one collapsed line. You can keep sending while a reply streams, as in the web chat: the server queues or steers each message into the running turn, the transcript marks it **sending…** until the session saves it, and a message that could not be delivered shows its error under it. The chat works with `--no-webui` too.

## How it differs from the full server

`aviary watch` is a minimal, temporary Code Loop instance. `aviary serve` is your permanent Aviary.

| | `aviary watch` | `aviary serve` |
| --- | --- | --- |
| Configuration | none; flags set the Code Loop settings for the session | `aviary.yaml` |
| State | a temporary directory, removed when the session ends | your Aviary config directory |
| Web UI | Code Loop, with its prompt chat, and Scheduled Tasks only, on `localhost`, no token | the full control panel |
| Terminal | a TUI board with prompt chat | no TUI; use the web UI |
| Agents, channels, workflows | none; jobs run under `--agent`, the agent that owns the target, or a `watch` agent | everything you configure |
| Lifetime | until Ctrl+C or `q`; running jobs are stopped and checkpointed | always on |

Only your browser profile and provider credentials are shared with your own Aviary, both read in place; nothing is written to your config. [Watch sessions](./projects#watch-sessions) covers the details. To watch a project permanently, [add it to an agent](./projects#watching-a-repository) instead.
