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
  disabledTools:
    - exec
  filesystem:
    allowedPaths:
      - "./workspace/**"
      - "!./workspace/private/**"
  exec:
    allowedCommands:
      - "git *"
      - "!git push *"
    shellInterpolate: false
    shell: /bin/bash
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `preset` | string | `"standard"` | Base tool surface: `"full"`, `"standard"`, or `"minimal"` |
| `tools` | []string | | Explicit tool allowlist. When non-empty, only the listed tools are offered. |
| `disabledTools` | []string | | Tools to remove from the available set regardless of preset |
| `filesystem.allowedPaths` | []string | | Ordered allow/deny glob rules for `file_*` tools. Rules use gitignore-style globbing; prefix with `!` to deny. Relative paths resolve to the agent's data directory. |
| `exec.allowedCommands` | []string | | Ordered glob rules matched against the raw command string. Prefix with `!` to deny. |
| `exec.shellInterpolate` | bool | `false` | Allow shell variable interpolation in commands |
| `exec.shell` | string | | Shell binary to use for command execution |

**Preset levels:**

| Preset | Description |
| --- | --- |
| `"standard"` _(default)_ | Blocks higher-risk local and server tools (filesystem writes, exec, config mutations) |
| `"full"` | All tools available |
| `"minimal"` | Only the smallest safe subset of tools |

**Path prefix shortcuts** for `allowedPaths`:

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
    token: xoxb-...
    id: T0123456789
    model: anthropic/claude-haiku-4-5-20251001
    show_typing: true
    react_to_emoji: true
    reply_to_replies: true
    send_read_receipts: true
    group_chat_history: 50
    disabledTools:
      - exec
    allowFrom:
      - from: U0123456789
        allowedGroups: "C0123456789,C9876543210"
        mentionPrefixes:
          - "hey bot*"
        respondToMentions: true
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `type` | string | _(required)_ | Channel type: `"slack"`, `"discord"`, or `"signal"` |
| `enabled` | bool | `true` | Whether this channel connection is active |
| `token` | string | | Bot token for the channel |
| `id` | string | | Workspace or server ID |
| `url` | string | | Webhook URL (Signal/signal-cli) |
| `model` | string | | Override model for all messages on this channel |
| `fallbacks` | []string | | Override fallbacks for all messages on this channel |
| `show_typing` | bool | `true` | Show a typing indicator while processing |
| `react_to_emoji` | bool | `true` | Treat emoji reactions on the agent's own messages as prompts |
| `reply_to_replies` | bool | `true` | Respond when someone replies to one of the agent's messages |
| `send_read_receipts` | bool | `true` | Send read receipts for messages the agent will act on |
| `group_chat_history` | int | `50` | Number of recent group chat messages retained as context. Set to `-1` to disable. |
| `disabledTools` | []string | | Tools disabled for messages arriving on this channel |
| `allowFrom` | []AllowFromEntry | | Sender and group filtering rules (see below) |

**allowFrom entries:**

Each entry controls which senders and groups can trigger the agent.

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `from` | string | _(required)_ | Sender ID (phone number, user ID) or `"*"` for any sender |
| `enabled` | bool | `true` | Whether this entry is active |
| `allowedGroups` | string | | Comma-separated group/channel IDs or `"*"` for any group. When empty, only direct messages match. |
| `mentionPrefixes` | []string | | Glob patterns matched against group message text. At least one must match (unless `respondToMentions` triggers). |
| `excludePrefixes` | []string | | Glob patterns; messages matching any pattern are silently dropped |
| `respondToMentions` | bool | `false` | Also forward group messages that directly @mention the bot |
| `mentionPrefixGroupOnly` | bool | `true` | When `true`, prefix/mention filtering applies only to group messages; direct messages from allowed senders are always forwarded. |
| `restrictTools` | []string | | Override the tool allowlist for messages matching this entry |
| `model` | string | | Override model for messages matching this entry |
| `fallbacks` | []string | | Override fallbacks for messages matching this entry |

A plain string in `allowFrom` is equivalent to `{ from: "<string>" }`.

### agents[].tasks

A list of scheduled or file-watch tasks for this agent.

```yaml
tasks:
  - name: daily-summary
    schedule: "0 9 * * *"
    prompt: "Summarize what happened yesterday and post to Slack."
    target: slack-channel

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
| `target` | string | | Target session or output destination |

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
| `providers.<name>.auth` | string | Credential reference in the form `auth:<provider>:<name>` (see `aviary auth set`) |
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
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `binary` | string | _(auto-detected)_ | Path to a Chrome or Chromium binary |
| `cdp_port` | int | `9222` | Chrome DevTools Protocol debugging port |
| `profile_directory` | string | `~/.config/aviary/browser` | Chrome user data directory |
| `headless` | bool | `false` | Run Chrome in headless mode |

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
        allowedPaths:
          - "./src/**"
          - "./tests/**"
      exec:
        allowedCommands:
          - "go test *"
          - "go build *"

  - name: lobby
    model: anthropic/claude-sonnet-4-6
    memory: shared
    channels:
      - type: slack
        token: xoxb-your-token
        id: T0123456789
        allowFrom:
          - from: "*"
            allowedGroups: "C0123456789"
            respondToMentions: true
    tasks:
      - name: morning-standup
        schedule: "0 9 * * 1-5"
        prompt: "Post a good morning message to the team."
```
