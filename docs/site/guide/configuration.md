# Configuration

Aviary is configured through a single YAML file, `aviary.yaml`, located at `~/.config/aviary/aviary.yaml` by default. The server reads this file at startup and watches it for changes — most settings take effect immediately without a restart.

For the full field-by-field schema, see the [Configuration Reference](/reference/config).

## File Location and Loading

The config file path is resolved in this order:

1. `$XDG_CONFIG_HOME/aviary/aviary.yaml` — if `XDG_CONFIG_HOME` is set
2. `~/.config/aviary/aviary.yaml` — the default
3. `--config` flag — overrides the path for a single server run

The directory containing `aviary.yaml` is the **config base directory**. Agent data directories, skill files, and relative filesystem paths in the config resolve under this root. You can override the base directory independently with `AVIARY_CONFIG_BASE_DIR`.

Every time the file is saved, Aviary rotates up to five timestamped backups in `~/.config/aviary/backups/`. The settings UI triggers the same rotation before writing changes.

## Live Reload

Most settings are applied as soon as the file changes on disk. The server uses fsnotify with a 300 ms debounce so rapid saves (e.g. editor auto-save) do not thrash the reload. Settings that bind a network port or change TLS require a server restart to take effect.

## Minimal Configuration

A minimal working config needs only a server port (which defaults to `16677`) and at least one agent with a model:

```yaml
models:
  providers:
    anthropic:
      auth: auth:anthropic:default
  defaults:
    model: anthropic/claude-sonnet-4-6

agents:
  - name: assistant
    memory: private
```

The `auth` value is a credential reference of the form `auth:<provider>:<name>`, where the provider and name match the key stored via `aviary auth set`. The model can be specified per-agent or inherited from `models.defaults`.

For browser automation, `browser.reuse_tabs` defaults to `true`, so `browser_open` reuses an existing page tab when the requested URL already matches exactly. Set it to `false` if you always want a fresh tab.

## Model Providers and Fallbacks

Aviary talks to LLM providers through a unified model string: `<provider>/<model-id>`. Configure API credentials once under `models.providers`:

```yaml
models:
  providers:
    anthropic:
      auth: auth:anthropic:default
    openai:
      auth: auth:openai:default
  defaults:
    model: anthropic/claude-sonnet-4-6
    fallbacks:
      - openai/gpt-4o
```

An agent's `fallbacks` list is tried in order when the primary model is unavailable. Individual agents can override both `model` and `fallbacks`; a channel within an agent can override them again; and a specific sender rule within a channel can override them a final time. Each level narrows the scope of the override.

## The Permission Model

Every agent has a **permissions preset** that determines its base tool surface, plus optional fine-grained overrides.

### Presets

| Preset | What it allows |
| --- | --- |
| `standard` _(default)_ | Memory, sessions, scheduler, browser, search, and skills. Blocks exec, filesystem writes, agent management, auth, and server config. |
| `full` | Every available tool |
| `minimal` | Only session and memory tools — no browser, exec, file, agent, auth, or skills |

Choose `standard` for most agents. Use `full` only for trusted automation agents running in a controlled environment. Use `minimal` for agents exposed to untrusted channel senders.

```yaml
permissions:
  preset: standard
```

### Allowlists and Blocklists

On top of a preset you can add individual tools with `tools` (an allowlist) or remove them with `disabledTools`:

```yaml
permissions:
  preset: standard
  tools:
    - file_read       # add file_read even though standard blocks file_*
  disabledTools:
    - browser_navigate  # remove browser_navigate from what standard allows
```

`tools` is additive relative to the preset. `disabledTools` is subtractive. Both are filtered to the tools actually accessible under the preset, so listing a tool blocked by the preset in `tools` has no effect.

### Filesystem Access

When an agent has access to `file_*` tools (either through `full` preset or via `tools`), you can restrict which paths it can read or write:

```yaml
permissions:
  preset: full
  filesystem:
    allowedPaths:
      - "~/projects/my-app/**"
      - "!~/projects/my-app/.env"
```

Rules are processed in order using gitignore-style glob matching. A leading `!` negates the match. Relative paths (starting with `./`) resolve under the agent's data directory (`~/.config/aviary/agents/<name>/`). Absolute paths and `~/` paths are used directly.

### Exec Access

Command execution is blocked by all presets except `full`. To enable it selectively:

```yaml
permissions:
  preset: full
  exec:
    allowedCommands:
      - "go test *"
      - "go build *"
      - "!go build -ldflags *"   # deny flag injection
    shellInterpolate: false
    shell: /bin/bash
```

`allowedCommands` patterns are matched against the raw command string. Deny rules (prefix `!`) are processed in order with allow rules. `shellInterpolate: false` prevents the agent from using shell variables or subshells in commands, even when a shell is configured.

### Channel and Sender Overrides

Permissions can be narrowed further at the channel level via `disabledTools`, and at the sender level via `restrictTools`. These only restrict — they cannot grant tools the agent-level preset does not allow.

```yaml
channels:
  - type: slack
    token: xoxb-...
    disabledTools:
      - memory_append   # disable memory writes from Slack
    allowFrom:
      - from: "U0123456789"
        restrictTools:
          - session_send    # this specific user can only send messages
```

## Agents and Working Directories

Each agent has a data directory at `~/.config/aviary/agents/<name>/`. The `working_dir` field overrides the base directory for file operations and path resolution:

```yaml
agents:
  - name: coder
    model: anthropic/claude-sonnet-4-6
    working_dir: ~/projects/my-app
    rules: ./RULES.md    # resolved relative to working_dir
```

`rules` can be an inline markdown string or a path to a file. The file is read at prompt time, so updates to `RULES.md` take effect without restarting the server.

## Channels

Channels connect an agent to a messaging platform. Multiple channels can be attached to one agent; each channel can have its own model, disabled tools, and sender rules.

```yaml
agents:
  - name: lobby
    model: anthropic/claude-sonnet-4-6
    channels:
      - type: slack
        token: xoxb-your-token
        id: T0123456789         # workspace ID
        show_typing: true
        allowFrom:
          - from: "*"
            allowedGroups: "C0123456789"   # only this channel
            respondToMentions: true         # forward @bot mentions
```

### Signal

Aviary connects to Signal via [signal-cli](https://github.com/AsamK/signal-cli). Install signal-cli and register (or link) your account before configuring this channel.

```bash
# Register a new number with Signal
signal-cli -a +15551234567 register
signal-cli -a +15551234567 verify <code>

# Or link to an existing Signal account as a secondary device
signal-cli link -n "Aviary"
```

Once registered, add the channel using the phone number as the `id`:

```yaml
agents:
  - name: assistant
    channels:
      - type: signal
        id: "+15551234567"      # your registered Signal number
        allowFrom:
          - from: "+19995550100"  # only respond to this contact
```

Aviary launches and manages `signal-cli` automatically. The `signal-cli` binary must be on your `$PATH`.

If you are already running signal-cli as a daemon on a known TCP port, point Aviary at it with `url` to avoid a second process:

```yaml
      - type: signal
        id: "+15551234567"
        url: "127.0.0.1:7583"   # existing signal-cli --tcp daemon address
```

**Sender filtering** controls who the agent responds to:

- `from: "*"` matches any sender; a specific user/phone ID matches only that sender.
- `allowedGroups` restricts to specific group/channel IDs. Without it, only direct messages match.
- `mentionPrefixes` filters group messages by text pattern. At least one pattern must match (unless `respondToMentions` catches an @mention).
- `excludePrefixes` silently drops matching messages before any other rule applies.

By default, group-chat filtering (mention prefixes, respond-to-mentions) applies only to group messages. Direct messages from allowed senders are always forwarded. Set `mentionPrefixGroupOnly: false` on an entry to also require a prefix match in direct messages.

A plain string in `allowFrom` is shorthand for `{ from: "<string>" }`:

```yaml
allowFrom:
  - "+15551234567"          # shorthand
  - from: "U9876543210"     # explicit form
    allowedGroups: "*"
    respondToMentions: true
```

## Scheduled Tasks

Tasks run prompts or scripts automatically on a schedule or in response to file changes. See the [Scheduled Tasks](/guide/scheduled-tasks) guide for a full walkthrough.

```yaml
agents:
  - name: assistant
    tasks:
      - name: daily-summary
        schedule: "0 9 * * 1-5"   # weekdays at 9 AM
        prompt: "Summarize open issues and post to Slack."
        target: slack:#team-updates
```

## Skills

Skills extend the agent's tool surface with custom runtimes. Enable them by name and pass any skill-specific settings:

```yaml
skills:
  my-skill:
    enabled: true
    settings:
      endpoint: https://example.com/api
```

Disabled skills (or skills with no settings) are omitted from the saved file automatically.

## Advanced Example

```yaml
server:
  port: 16677
  failed_task_timeout: "6h"

models:
  providers:
    anthropic:
      auth: auth:anthropic:default
    openai:
      auth: auth:openai:default
  defaults:
    model: anthropic/claude-sonnet-4-6
    fallbacks:
      - openai/gpt-4o

agents:
  - name: coder
    model: anthropic/claude-sonnet-4-6
    memory: private
    memory_tokens: 4096
    working_dir: ~/projects/my-app
    rules: ./RULES.md
    permissions:
      preset: full
      filesystem:
        allowedPaths:
          - "~/projects/my-app/**"
          - "!~/projects/my-app/.env"
          - "!~/projects/my-app/secrets/**"
      exec:
        allowedCommands:
          - "go *"
          - "make *"
          - "!make deploy *"

  - name: lobby
    model: anthropic/claude-haiku-4-5-20251001
    memory: shared
    verbose: true
    channels:
      - type: slack
        token: xoxb-your-token
        id: T0123456789
        show_typing: true
        disabledTools:
          - exec
        allowFrom:
          - from: "*"
            allowedGroups: "C0123456789"
            respondToMentions: true
          - from: "U0123456789"           # admin: DMs always forwarded
    tasks:
      - name: morning-standup
        schedule: "0 9 * * 1-5"
        prompt: "Post a good morning message and list today's open tasks."

browser:
  headless: false
  cdp_port: 9222
  reuse_tabs: true

search:
  web:
    brave_api_key: BSA...

scheduler:
  concurrency: auto
  precompute_tasks: true
```
