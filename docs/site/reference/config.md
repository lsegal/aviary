# Configuration Reference

All configuration lives in `aviary.yaml`, located at `~/.config/aviary/aviary.yaml` by default. The path can be overridden with the `--config` flag or the `AVIARY_CONFIG_BASE_DIR` environment variable.

## Top-Level Structure

```yaml
server:    { ... }
agents:    [ ... ]
models:    { ... }
browser:   { ... }
search:    { ... }
scheduler: { ... }
skills:    { ... }
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
      - openai/gpt-4o
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
    model: anthropic/claude-haiku-4-5-20251001
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
| `show_typing` | bool | `true` | Show a typing indicator while processing on supported channels (currently Signal only; Slack and Discord do not support it) |
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
- `show_typing` is not supported on Slack because Slack apps using Events API and Socket Mode cannot send typing indicators, including in DMs.
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

---

## models

Provider credentials and default model settings.

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
  defaults:
    model: anthropic/claude-sonnet-4-6
    fallbacks:
      - openai/gpt-4o
```

| Field | Type | Description |
| --- | --- | --- |
| `providers.<name>.auth` | string | Credential reference in the form `auth:<key>` (see `aviary auth set`) |
| `defaults.model` | string | Default model used by agents that do not specify one |
| `defaults.fallbacks` | []string | Default fallback models used by agents that do not specify their own |

**Supported providers:** `anthropic`, `openai`, `gemini`, `github-copilot`

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

Installed disk skills are loaded from `AVIARY_CONFIG_BASE_DIR/skills` and `~/.agents/skill`. Search for published skills with `npx skills find` or on [skills.sh](https://skills.sh/), then install one globally with a command like `npx skills add --global -a universal owner/skill-name`.

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
