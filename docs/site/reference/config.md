# Configuration Reference

All configuration lives in `aviary.yaml`, located at `~/.config/aviary/aviary.yaml` by default. The path can be overridden with the `--config` flag.

## Top-Level Structure

```yaml
server:    { ... }
agents:    [ ... ]
models:    { ... }
browser:   { ... }
search:    { ... }
scheduler: { ... }
skills:    { ... }
approval:  { ... }
projects:  { ... }
coding_skills: { ... }
```

---

## server

Controls the HTTP server.

```yaml
server:
  port: 16677
  external_access: false
  no_tls: false
  failed_task_timeout: "6h"
  tls:
    cert: /path/to/cert.pem
    key:  /path/to/key.pem
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `port` | int | `16677` | Port the server listens on |
| `external_access` | bool | `false` | Bind to `0.0.0.0` instead of `127.0.0.1` |
| `no_tls` | bool | `false` | Disable TLS and serve plain HTTP |
| `failed_task_timeout` | string | `"6h"` | Maximum age of a pending checkpoint before the agent gives up. Accepts Go duration strings (`"30m"`, `"6h"`, `"24h"`). |
| `auto_update` | bool | `false` | Install new releases automatically as soon as they are available. Running jobs are checkpointed and resume after the restart. Dev builds in a git checkout fast-forward the checkout instead (see [Upgrades](/guide/operations#dev-checkouts)). |
| `restart_call_grace` | string | `"1m"` | How long a pending restart keeps admitting new MCP tool calls while it waits for in-flight ones. Go duration string. |
| `restart_call_max_wait` | string | `"5m"` | How long a restart waits for in-flight MCP tool calls before it proceeds anyway. Go duration string. |
| `tls.cert` | string | | Path to TLS certificate file |
| `tls.key` | string | | Path to TLS private key file |

---

## agents

A list of agent definitions.

```yaml
agents:
  - name: assistant
    model: anthropic/claude-sonnet-4-6
    fallbacks:
      - openai/gpt-6.2-sol
    memory: shared
    memory_tokens: 4096
    compact_keep: 20
    working_dir: ~/workspace
    rules: |
      You are a helpful assistant.
    verbose: true
    permissions: { ... }
    channels: [ ... ]
    tasks: [ ... ]
    projects: { ... }
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `name` | string | _(required)_ | Unique agent name |
| `model` | string | | Model ID (e.g. `anthropic/claude-sonnet-4-6`). Falls back to `models.defaults.model`. |
| `fallbacks` | []string | | Ordered fallback model IDs if the primary model is unavailable |
| `memory` | string | | Memory pool: `"shared"`, `"private"`, or a named pool (e.g. `"team-memory"`) |
| `memory_tokens` | int | | Maximum tokens of memory content injected into each prompt |
| `compact_keep` | int | | Number of recent messages to retain during context compaction |
| `working_dir` | string | | Default working directory for file-path resolution. Supports `~` and environment variables. Defaults to the agent's data directory. |
| `rules` | string | | Inline markdown rules or a path to a file (e.g. `"./RULES.md"`) injected at the top of every system prompt. Paths are resolved relative to `working_dir`. |
| `verbose` | bool | `false` | Emit a brief status message before each tool call when responding via a channel |
| `projects` | object | | The GitHub [projects](#agents-projects) this agent owns |

### agents[].permissions

Restricts which tools an agent may use.

```yaml
permissions:
  preset: standard
  tools:
    - agent_run
    - file_read
  disabled_tools:
    - exec
  filesystem:
    allowed_paths:
      - "./workspace/**"
      - "!./workspace/private/**"
  exec:
    allowed_commands:
      - "git *"
      - "!git push *"
    shell_interpolate: false
    shell: /bin/bash
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `preset` | string | `"standard"` | Base tool surface: `"full"`, `"standard"`, or `"minimal"` |
| `tools` | []string | | Explicit tool allowlist. When non-empty, only the listed tools are offered. |
| `disabled_tools` | []string | | Tools to remove from the available set regardless of preset |
| `filesystem.allowed_paths` | []string | | Ordered allow/deny glob rules for `file_*` tools. Rules use gitignore-style globbing; prefix with `!` to deny. Relative paths resolve to the agent's data directory. |
| `exec.allowed_commands` | []string | | Ordered glob rules matched against the raw command string. Prefix with `!` to deny. |
| `exec.shell_interpolate` | bool | `false` | Allow shell variable interpolation in commands |
| `cli_builtin_tools` | bool | `false` | For `cli/...` models, let the Claude Code or Codex CLI use its own shell, file-edit and web tools. Requires `preset: full`. |
| `exec.shell` | string | | Shell binary to use for command execution |

**Preset levels:**

| Preset | Description |
| --- | --- |
| `"standard"` _(default)_ | Blocks higher-risk local and server tools (filesystem writes, exec, config mutations) |
| `"full"` | All tools available |
| `"minimal"` | Only the smallest safe subset of tools |

**Path prefix shortcuts** for `allowed_paths`:

| Prefix | Resolves to |
| --- | --- |
| `./` or relative | Agent's data directory (`~/.config/aviary/agents/<name>/`) |
| `~/` | User home directory |
| Absolute path | Used as-is |

### agents[].channels

A list of messaging channel connections for this agent.

```yaml
channels:
  - type: slack
    id: workspace-bot
    url: xapp-...
    token: xoxb-...
    model: anthropic/claude-haiku-5-5
    react_to_emoji: true
    reply_to_replies: true
    send_read_receipts: true
    group_chat_history: 50
    disabled_tools:
      - exec
    allow_from:
      - from: "@alice"
        allowed_groups: "#alerts,#engineering"
        mention_prefixes:
          - "hey bot*"
        respond_to_mentions: true
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `type` | string | _(required)_ | Channel type: `"slack"`, `"discord"`, or `"signal"` |
| `enabled` | bool | `true` | Whether this channel connection is active |
| `token` | string | | Bot token for the channel. For Slack this is the `xoxb-...` token. For Discord this is the bot token from the Discord Developer Portal. |
| `id` | string | | Aviary's configured channel/integration ID. Used when routing task output, e.g. `slack:workspace-bot:#alerts`. |
| `url` | string | | Channel transport address. For Slack this is the App-Level token (`xapp-...`) used by Socket Mode. For Signal this is the `signal-cli` daemon address. Discord does not use `url`. |
| `model` | string | | Override model for all messages on this channel |
| `fallbacks` | []string | | Override fallbacks for all messages on this channel |
| `show_typing` | bool | `true` | Show a typing or progress indicator while processing on supported channels (Signal typing notifications and Slack assistant status indicators) |
| `separate_top_level_sessions` | bool | `false` | For Slack, start a distinct session for each top-level message thread instead of sharing one channel session |
| `react_to_emoji` | bool | `true` | Treat emoji reactions on the agent's own messages as prompts |
| `reply_to_replies` | bool | `true` | Respond when someone replies to one of the agent's messages |
| `send_read_receipts` | bool | `true` | Send read receipts for messages the agent will act on |
| `group_chat_history` | int | `50` | Number of recent group chat messages retained as context. Set to `-1` to disable. |
| `disabled_tools` | []string | | Tools disabled for messages arriving on this channel |
| `allow_from` | []AllowFromEntry | | Sender and group filtering rules (see below) |

### Slack-specific Notes

- `id` is not a Slack workspace ID or channel ID. It is your Aviary integration name for that Slack connection.
- `url` must contain the Slack App-Level token (`xapp-...`) when `type: slack`.
- `token` must contain the Slack Bot token (`xoxb-...`) when `type: slack`.
- Slack apps using Events API and Socket Mode cannot send classic typing indicators, but `show_typing` enables Slack assistant thread status updates while Aviary is working.
- Slack replies inside an existing thread are treated as replies and do not require an `@bot` mention or configured mention prefix.
- Set `separate_top_level_sessions: true` to keep each new top-level Slack message and its thread replies in a separate session.
- `users:read` is required on the Slack bot token if you want Aviary to resolve Slack user names for name-based routing.
- Slack Event Subscriptions should include both message events and the `app_mention` event if you want the bot to answer `@bot` mentions in channels.
- Slack scheduled task delivery routes use the form `slack:<configured-id>:<slack-channel-id>`.
- For Slack, Aviary accepts either raw IDs or friendly names in many places:
  `@alice` for users, and `#alerts` for channels in the common case. Raw Slack IDs still work when needed.

### Discord-specific Notes

- Enable the **Message Content Intent** for the bot in the Discord Developer Portal.
- `token` must contain the Discord bot token when `type: discord`.
- `id` is Aviary's configured integration name for that Discord connection, not a Discord channel ID.
- `allow_from[].from` should contain Discord user IDs.
- `allow_from[].allowed_groups` should contain Discord channel IDs.
- Discord scheduled task delivery routes use the form `discord:<configured-id>:<discord-channel-id>`.

**allow_from entries:**

Each entry controls which senders and groups can trigger the agent.

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `from` | string | _(required)_ | Sender ID (phone number, user ID) or `"*"` for any sender |
| `enabled` | bool | `true` | Whether this entry is active |
| `allowed_groups` | string | | Comma-separated group/channel IDs or `"*"` for any group. When empty, only direct messages match. |
| `mention_prefixes` | []string | | Glob patterns matched against group message text. At least one must match (unless `respond_to_mentions` triggers). |
| `exclude_prefixes` | []string | | Glob patterns; messages matching any pattern are silently dropped |
| `respond_to_mentions` | bool | `false` | Also forward group messages that directly @mention the bot |
| `mention_prefix_group_only` | bool | `true` | When `true`, prefix/mention filtering applies only to group messages; direct messages from allowed senders are always forwarded. |
| `restrict_tools` | []string | | Override the tool allowlist for messages matching this entry |
| `model` | string | | Override model for messages matching this entry |
| `fallbacks` | []string | | Override fallbacks for messages matching this entry |

A plain string in `allow_from` is equivalent to `{ from: "<string>" }`.

### agents[].tasks

A list of scheduled or file-watch tasks for this agent.

```yaml
tasks:
  - name: daily-summary
    schedule: "0 9 * * *"
    prompt: "Summarize what happened yesterday and post to Slack."
    target: slack:workspace-bot:#alerts

  - name: process-new-files
    watch: "./inbox/*.csv"
    type: script
    script: |
      local files = aviary.changed_files()
      for _, f in ipairs(files) do
        aviary.run_agent("processor", "Process file: " .. f)
      end
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `name` | string | _(required)_ | Unique task name within this agent |
| `enabled` | bool | `true` | Whether this task is active |
| `type` | string | `"prompt"` | Task type: `"prompt"` or `"script"` |
| `schedule` | string | | Cron expression for time-based scheduling. Supports standard 5-field cron and shortcuts like `@hourly`, `@daily`, `@weekly`. |
| `start_at` | string | | ISO 8601 datetime for the first run (e.g. `"2026-01-01T09:00:00Z"`) |
| `run_once` | bool | `false` | Run the task once then disable it |
| `watch` | string | | File glob pattern; the task runs when matching files change |
| `prompt` | string | | Prompt text sent to the agent (for `type: prompt`) |
| `script` | string | | Lua script executed directly (for `type: script`) |
| `target` | string | | Target session or output destination. Use `session:<name>` for a session or `<channel-type>:<configured-channel-id>:<delivery-id>` for channel delivery. |

### agents[].projects

The [Projects](/guide/projects) this agent owns. A project belongs to exactly one agent: it is listed under that agent, its jobs run on that agent's model and fallbacks, and the list is managed on the agent's tab in **Projects Settings** (`/projects/settings?agent=<agent>`). Listing the same target under two agents is a validation error. The settings every project runs under are the shared top-level [`projects`](#projects) section, not the agent's.

```yaml
agents:
  - name: coder
    model: cli/claude/opus
    fallbacks: [cli/codex/gpt-5.6-terra:high]
    projects:
      - target: owner/repo
      - target: https://github.com/orgs/acme/projects/3
        reports:                   # off unless set
          agent: assistant
          targets: [slack:main:C0123456789]
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `[].target` | string | | `owner/repo`, a repository URL, a Project board, or a Discussions board. The agent's model and fallbacks are its jobs' provider and model defaults, in fallback order: a `<cli>/<model>` (or `cli/<cli>/<model>`) model runs jobs on that CLI provider; any other model runs them on the agent itself |
| `[].reports.agent` | string | | Aviary agent whose channels deliver this project's [reports](/guide/projects#reports); reports are off unless `agent` and `targets` are set |
| `[].reports.targets` | string[] | | Where reports go, in a scheduled task's target form: `<channel type>:<configured channel id>:<delivery id>` or `session:<name>` |
| `[].reports.events` | string[] | all | Events to report: `merged`, `failed`, `waiting` |

---

## models

Provider credentials, default model settings, and the providers reached through a CLI.

```yaml
models:
  providers:
    anthropic:
      auth: auth:anthropic:default
    openai:
      auth: auth:openai:default
    gemini:
      auth: auth:gemini:default
    github-copilot:
      auth: auth:github-copilot:default
    meta:
      auth: auth:meta:default
  defaults:
    model: anthropic/claude-sonnet-4-6
    fallbacks:
      - openai/gpt-6.2-sol
  cli_definitions:
    claude:
      binary: /opt/claude/bin/claude
```

| Field | Type | Description |
| --- | --- | --- |
| `providers.<name>.auth` | string | Credential reference in the form `auth:<key>` (see `aviary auth set`) |
| `defaults.model` | string | Default model used by agents that do not specify one |
| `defaults.fallbacks` | []string | Default fallback models used by agents that do not specify their own |
| `cli_definitions` | map | CLI definitions by agent name: field-by-field overrides of the built-ins (`codex`, `claude`, `gemini`, `muse`, `opencode`, `cline`, `agy`; `null` drops a field) and new CLIs. Settings → Providers & Authentication and Projects both read the CLIs from here; a `binary` override (a path, or a name looked up on `PATH`) is the executable the doctor reports and the one Projects jobs run. `wakeup` declares how a CLI signals a deferred turn; see [Deferred turns](/guide/projects#deferred-turns). The schema is in [CLI provider definitions](/reference/cli-definitions) |

**Supported providers:** `anthropic`, `openai`, `gemini`, `github-copilot`, `meta`

`meta` serves the Muse models (`meta/muse-spark-1.3` and the rest of the lineup) straight from Meta's Model API with an API key, so they work without the `muse` CLI installed. Store the key with `aviary auth set meta:default <your-api-key>`; `providers.meta.base_uri` overrides the default `https://api.meta.ai/v1`.

---

## browser

Browser automation settings.

```yaml
browser:
  binary: /usr/bin/chromium
  cdp_port: 9222
  profile_directory: Default
  headless: false
  reuse_tabs: true
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `binary` | string | _(auto-detected)_ | Path to a Chrome or Chromium binary |
| `cdp_port` | int | `9222` | Chrome DevTools Protocol debugging port |
| `profile_directory` | string | `~/.config/aviary/browser` | Chrome user data directory |
| `headless` | bool | `false` | Run Chrome in headless mode |
| `reuse_tabs` | bool | `true` | Reuse an existing page tab in `browser_open` when the requested URL exactly matches the current URL |

---

## search

Web search backend settings.

```yaml
search:
  web:
    brave_api_key: BSA...
```

| Field | Type | Description |
| --- | --- | --- |
| `search.web.brave_api_key` | string | Brave Search API key for web search |

---

## scheduler

Task execution settings.

```yaml
scheduler:
  concurrency: auto
  precompute_tasks: true
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `concurrency` | int or `"auto"` | `"auto"` | Maximum concurrent task jobs. `"auto"` uses `runtime.NumCPU()`. |
| `precompute_tasks` | bool | `true` | Pre-compile prompt tasks when they are scheduled, rather than at run time |

---

## skills

Enables and configures installed skill runtimes. Keys are skill names.

Installed disk skills are loaded from `~/.config/aviary/skills` and `~/.agents/skill`. Search for published skills with `npx skills find` or on [skills.sh](https://skills.sh/), then install one globally with a command like `npx skills add --global -a universal owner/skill-name`.

```yaml
skills:
  my-skill:
    enabled: true
    settings:
      api_url: https://example.com/api
      timeout: 30
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `<name>.enabled` | bool | `false` | Whether this skill is active |
| `<name>.settings` | map | | Skill-specific key/value settings passed to the skill runtime |

---

## coding_skills

Controls how Aviary keeps the [skills for providers](/guide/skills/) (`gh-fix`, `gh-discuss`, `gh-create`, `gh-safe-merge`) installed and current. Both settings default to `true` and are only written when turned off.

The skills Aviary ships are not listed under `skills` and are not available to Aviary's own agents.

```yaml
coding_skills:
  auto_update: false
  install_new_clis: false
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `auto_update` | bool | `true` | At startup, reinstall any skill whose installed copy differs from the one this release was built with |
| `install_new_clis` | bool | `true` | Install the skills for providers found on the `PATH` later (checked every 15 minutes) |

---

## projects

The one set of [Projects](/guide/projects) settings, shared by every project of every agent and edited on **Projects Settings** (`/projects/settings`). One engine runs every agent's projects under them: `concurrency`, for example, is how many jobs run at once in total, across every agent's projects. The projects themselves are listed under the agents that own them; see [`agents[].projects`](#agents-projects).

Every field is optional and holds only a value that differs from its default. Changes apply to the running engine when the file changes. Changing a live setting (concurrency, allowed commenters, no-merge, no-changelog, the watchdog, clone directories) updates it in place. Changing anything else restarts the engine, and running jobs are checkpointed and resume.

```yaml
projects:
  concurrency: 4
  no_merge: true
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `concurrency` | int | `3` | Jobs that run at once across every project |
| `interval` | duration | `30s` | Time between GitHub polls |
| `delivery` | string | `poll` | `poll` or `webhook` (ngrok tunnel and repository webhooks) |
| `ready_state` | string | `Todo` or `Ready` | Project board status that marks an item ready |
| `filter` | string[] | `assignee:@me` | Issue search filter |
| `all_issues` | bool | `false` | Drop the issue filter and pick up every open issue |
| `allowed_commenters` | string[] | the `gh` user | GitHub logins whose comments steer jobs |
| `approval_mode` | string | `approval.mode` | Projects' [approval-mode](#approval) override for the CLI runs it launches: `read-only`, `auto`, or `full` |
| `yolo` | bool | `false` | The `full` approval mode, used when `approval_mode` is unset |
| `no_merge` | bool | `false` | Leave finished pull requests ready for a human to merge |
| `no_changelog` | bool | `false` | Skip changelog entries in `gh-fix` pull requests |
| `unblock_interval` | duration | `15m` | How often the stuck-job watchdog runs; `0` turns it off |
| `unblock_agent`, `unblock_model` | string | the coding agent | Agent spec and model for the watchdog's unblock call |
| `chat_model`, `chat_fallbacks` | string, string[] | `models.defaults` | Model and fallbacks of the built-in `projects` agent behind the board's prompt chat |
| `temp_dirs` | string[] | system temp | Absolute directories job clones go in, tried in order |
| `webhook.listen` | string | `:0` | Webhook listener address |
| `webhook.path` | string | `/webhook` | Webhook path |
| `webhook.secret` | string | | Webhook signature secret, plain or a credential reference in the form `auth:<key>` (see `aviary auth set`) |
| `webhook.ngrok_binary` | string | `ngrok` | ngrok executable; fetched through `npx` when not installed |

---

## approval

How much CLI providers may do without asking. Aviary owns the mode: every CLI launch (chat turns, Projects jobs, one-shot prompts) works out its surface's mode and translates it into that CLI's own permission flags at launch. Aviary never creates or edits a CLI's settings files to set it, so the CLI behaves as configured when you use it yourself.

```yaml
approval:
  mode: auto        # the general setting
  chat: read-only   # override for chat turns
projects:
  approval_mode: full  # override for Projects jobs
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `mode` | string | `auto` | The general setting, used by every surface without its own override |
| `chat` | string | `mode` | Override for agent chat turns run through a CLI provider: the web UI chat, including the Projects board's prompt chat |

`projects.approval_mode` (or `projects.yolo`, which means `full`) is the Projects override; see [`projects`](#projects).

| Mode | Meaning |
| --- | --- |
| `read-only` | Read and inspect, but change nothing |
| `auto` | Work inside the workspace without asking; the CLI's own sandbox or safety checks refuse riskier actions |
| `full` | No sandbox or permission checks |

Each CLI definition's `approval` field maps the modes to flags. Where a CLI has nothing that matches a mode exactly, the mapping takes the closest mode that is not more permissive. The built-in mappings:

| CLI | `read-only` | `auto` | `full` |
| --- | --- | --- | --- |
| `claude` | `--permission-mode dontAsk` | `--permission-mode auto` | `--dangerously-skip-permissions` |
| `codex` | `-c sandbox_mode=read-only` | `-c sandbox_mode=workspace-write` with network access | `--dangerously-bypass-approvals-and-sandbox` |
| `gemini` | `--approval-mode plan` | `--approval-mode auto_edit` | `--yolo` |
| `muse` | `--approval-mode untrusted --disable-write` | `--approval-mode never` | `--yolo` |
| `agy` | `--mode plan` | `--mode auto` | `--dangerously-skip-permissions` |
| `cline` | `--plan --auto-approve true` | `--auto-approve true` | same as `auto` |
| `opencode` | `--agent plan` | `--auto` | same as `auto` |

Claude's `dontAsk` runs only pre-approved tools and refuses the rest, so reads work but edits and shell commands do not. Cline and opencode have no sandbox and no partial-approval mode that can run unattended: the only way to keep them from changing files is their plan mode, so `auto` is their default auto-approving mode, the same as `full`. Pick `read-only` to keep them from making changes.

In a chat turn, Aviary's own MCP tools are always pre-approved for the CLI, so a mode never blocks them behind a separate prompt. The agent's [permissions](#agents-permissions) still decide which Aviary tools it gets, and Aviary checks every call. The mode governs the CLI's own tools, which a chat turn only gets with the `full` preset and `cli_builtin_tools`.

A chat turn's CLI runs in the agent's `working_dir` and always also has the agent's own directory (its `MEMORY.md` and `memory/`) in scope, so the agent can keep its memory wherever it works. Each definition's `extraDir` field grants that directory per mode, with the same fallback as `approval`: Claude gets `--add-dir` and, outside `read-only`, an `Edit` allow rule for that directory; Codex gets the directory as a writable root of its workspace sandbox. The grant covers only that directory and never changes the mode, so `read-only` still refuses writes there. A `cli_definitions` entry names the directory as `{dir}`, as `{dirQuoted}` (a quoted TOML string), or as `{dirPosix}` (`C:\a` becomes `/c/a`).

When a chat turn's CLI has its own tools, the agent's `exec.allowed_commands` and `filesystem.allowed_paths` reach it too, as the CLI's own allow and deny rules, so a command or path the agent allows is not then refused by the CLI. Each definition's `permissions` field declares the translation per mode. Claude gets, outside `read-only`, an allow rule for the `Bash` and `PowerShell` tools per command rule (`*` allows every command) and `--add-dir` plus an `Edit` allow rule per path rule. In every mode, including `full`, a `!` rule becomes a `--disallowedTools` rule, which Claude enforces even with its permission checks off. Claude weighs a deny above every allow, so a deny followed by a narrower allow is more restrictive in the CLI than in Aviary, never less.

A refusal names the layer and rule behind it. Aviary's own tools answer, for example, `write refused by Aviary: the agent's Filesystem Allowed Paths deny it with rule "!@/token"`. A refusal from the CLI's own permission check gets a note saying it came from the CLI, under which chat approval mode, and that no rule from the agent's permissions allowed it.

A `cli_definitions` entry declares its own mapping under `approval` and renders it with an argument that is exactly `{approval}` in its templates:

```yaml
models:
  cli_definitions:
    mycli:
      binary: mycli
      approval:
        read-only: [--sandbox, read-only]
        auto: [--sandbox, workspace]
        full: [--no-sandbox]
      args:
        run:
          - args: ["{approval}"]
          - args: ["{prompt}"]
```

A mode a definition leaves out falls back to the closest less permissive mode it declares. If it declares none of those, the flag is left off. The `yolo` fragment condition still holds in the `full` mode.

---

## Full Example

```yaml
server:
  port: 16677
  external_access: false
  failed_task_timeout: "6h"

models:
  providers:
    anthropic:
      auth: auth:anthropic:default
  defaults:
    model: anthropic/claude-sonnet-4-6

agents:
  - name: assistant
    model: anthropic/claude-sonnet-4-6
    memory: private
    memory_tokens: 2048
    working_dir: ~/projects/my-app
    rules: |
      You are a helpful coding assistant with access to the project workspace.
    permissions:
      preset: standard
      filesystem:
        allowed_paths:
          - "./src/**"
          - "./tests/**"
      exec:
        allowed_commands:
          - "go test *"
          - "go build *"

  - name: lobby
    model: anthropic/claude-sonnet-4-6
    memory: shared
    channels:
      - type: slack
        id: workspace-bot
        url: xapp-your-app-level-token
        token: xoxb-your-bot-token
        allow_from:
          - from: "*"
            allowed_groups: "#alerts"
            respond_to_mentions: true
    tasks:
      - name: morning-standup
        schedule: "0 9 * * 1-5"
        prompt: "Post a good morning message to the team."
```
