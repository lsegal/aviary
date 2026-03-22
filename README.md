# Aviary: the AI Agent Nest

<p align="center"><img src="web/public/logo.png" alt="Aviary logo" width="200" /></p>

Aviary is a full AI assistant platform. Connect your AI models to Slack, Signal, Discord, etc., have conversations, set up scheduled tasks, and let your agents work for you. All managed from a CLI or a web-based control panel.

## Table of Contents

- [Setup](#setup)
- [Configuring](#configuring)
- [Usage](#usage)
  - [CLI](#cli)
  - [Web Control Panel](#web-control-panel)
- [Contributing](CONTRIBUTING.md)

---

## Setup

### Install

macOS / Linux:

```shell
curl -fsSL https://avia.ry/install.sh | sh
```

Windows:

```powershell
iwr -useb https://avia.ry/install.ps1 | iex
```

### First Run

Start the Aviary server:

```shell
aviary start
```

On first start, Aviary generates an authentication token and prints it to your terminal. **Save this token**—you'll need it to access the web control panel and to authenticate remote CLI connections.

```
Aviary started on https://localhost:16677
Your access token: aviary_tok_xxxxxxxxxxxxxxxx
```

The server runs over HTTPS using a self-signed certificate by default. To use your own certificate, see [TLS Configuration](#tls-configuration) in the config reference below.

To stop the server:

```shell
aviary stop
```

### Access the Web Control Panel

Open `https://localhost:16677` in your browser. Enter your token on the login screen. Your session is persisted in a secure browser cookie—you won't need to log in again unless you clear it.

---

## Configuring

Aviary is configured with a YAML file at `~/.config/aviary/aviary.yaml`. **Changes are applied automatically**—no restart needed. Add a new agent, update a schedule, or swap out a model and it takes effect immediately.

### Minimal Configuration

```yaml
agents:
  - name: assistant
    model: anthropic/claude-sonnet-4.5
```

### Full Configuration Reference

```yaml
server:
  port: 16677               # Default: 16677
  external_access: false    # Bind to 0.0.0.0 instead of 127.0.0.1
  no_tls: false             # Disable TLS (plain HTTP)
  # tls:
  #   cert: /path/to/cert.pem
  #   key: /path/to/key.pem

agents:
  - name: assistant
    description: "General purpose assistant"
    model: anthropic/claude-sonnet-4.5  # Overrides models.defaults.model for this agent
    fallbacks:                          # Try these in order if the primary model fails
      - openai/gpt-4o
    memory: shared                      # Share memory across agents ("shared"), keep private ("private"),
                                        # or use a named pool (e.g. "team-memory")
    memory_tokens: 4096                 # Max tokens to include from memory in each prompt
    compact_keep: 20                    # Number of recent messages to retain when compacting history
    rules: |                            # Inline rules appended to the agent's RULES.md
      - Always respond in the user's language.

    permissions:
      preset: minimal                   # "minimal", "standard", or "full" — sets a base tool set
      tools:                            # Explicit list of allowed MCP tools (overrides preset)
        - task_run
      disabledTools:                    # Tools to remove from an otherwise-enabled set
        - browser_navigate
      filesystem:
        allowedPaths:                   # Glob patterns; relative paths are rooted at the agent directory
          - ./**                        # Agent's own directory (jobs, memory, sessions, notes)
          - ~/Documents/**
          - "!~/Documents/private/**"   # Prefix with "!" to deny
      exec:
        allowedCommands:               # Glob patterns matched against the full command string
          - "git *"
          - "npm *"
        shellInterpolate: false        # Pass command through a shell (sh/cmd); default false
        shell: sh                      # Shell to use when shellInterpolate is true

    channels:
      - type: slack
        token: auth:slack:workspace     # References a stored credential (see Auth below)
        id: "workspace-bot"
        allowFrom:
          - from: "@lsegal"             # Only respond to messages from this user
          - from: "@ops-team"           # Or this user group
            excludePrefixes:            # Silently drop messages that match any of these prefixes
              - "!"
              - "/"

      - type: discord
        token: auth:discord:bot-token
        id: "team-bot"
        allowFrom:
          - from: "*"                   # Allow messages from anyone in the channel

      - type: signal
        id: auth:signal:myphone        # Stable channel id; for Signal use the registered phone number
        allowFrom:
          - from: "+15551234567"        # Allowlist by phone number; omit to allow all

    tasks:
      - name: daily-briefing
        schedule: "0 9 * * *"           # Standard cron syntax
        prompt: "Give me a morning briefing based on recent activity."
        target: slack:workspace-bot:C123
                                        # <channel>:<configured-id>:<target> pins delivery to
                                        # a configured channel; use session:main (or another
                                        # session name/id) to send output into a session;
                                        # omit or set to "silent" for no delivery

      - name: organize-downloads
        watch: ~/Downloads/**           # Trigger on any file change, recursively
        prompt: "A file was added or changed in ~/Downloads. Rename it using a clear, descriptive name based on its contents."
        target: slack:workspace-bot:C123

      - name: onboarding
        run_once: true                  # Run exactly once then disable itself
        prompt: "Send a welcome message to #general."
        start_at: "2025-01-01T09:00:00"

models:
  providers:
    anthropic:
      auth: auth:anthropic:default
    openai:
      auth: auth:openai:default
    google:                             # Google Gemini via API key
      auth: auth:gemini:default
    google-gemini:                      # Google Gemini via OAuth (Code Assist free tier)
      auth: auth:gemini:oauth
  defaults:
    model: anthropic/claude-sonnet-4.5  # Used when an agent doesn't specify a model
    fallbacks:                          # Try these in order if the default fails
      - openai/gpt-4o

browser:
  binary: /usr/bin/chromium             # Path to your Chromium-compatible browser binary
  cdp_port: 9222                        # Chrome DevTools Protocol port
  headless: false                       # Run browser without a visible window
  profile_directory: ""                 # Custom Chrome profile directory

search:
  web:
    brave_api_key: auth:brave:default   # Brave Search API key for web search tool

scheduler:
  concurrency: auto                     # "auto" uses all available CPUs; or set a number

skills:                                 # Enable and configure installed skills
  my-skill:
    enabled: true
    settings:
      key: value
```

### Model Providers

| Prefix | Auth method | Notes |
|--------|-------------|-------|
| `anthropic/` | API key (`anthropic:default`) or OAuth | e.g. `anthropic/claude-sonnet-4.5` |
| `openai/` | API key (`openai:default`) | e.g. `openai/gpt-4o` |
| `openai-codex/` | OAuth (`openai:oauth`) | OpenAI Codex via OAuth |
| `google/` | API key (`gemini:default`) | Gemini via Google AI API |
| `google-gemini/` | OAuth (`gemini:oauth`) | Gemini via Code Assist (free tier) |
| `stdio/` | none | Subprocess via stdin/stdout; e.g. `stdio/claude` |

Run `aviary models list` to see all supported models.

### Authentication

Auth values use the format `auth:<provider>:<name>` to reference credentials stored securely—either in your system keychain or in a separate auth file. This keeps secrets out of your main config.

#### API Keys

Store credentials manually by name:

```shell
aviary auth set anthropic:default sk-ant-...
aviary auth set openai:default sk-...
aviary auth set gemini:default AIza...
aviary auth set slack:workspace xoxb-...
```

Then reference them anywhere in `aviary.yaml` as `auth:anthropic:default`, `auth:openai:default`, etc.

#### OAuth

For providers that support OAuth (Anthropic, OpenAI, Google, Slack, and others), Aviary can manage the full OAuth flow for you:

```shell
aviary auth login anthropic    # Opens a browser window to authorize with Anthropic
aviary auth login openai       # Opens a browser window to authorize with OpenAI
aviary auth login gemini       # Opens a browser window to authorize with Google (Code Assist)
aviary auth login slack        # Opens a browser window to authorize your Slack workspace
```

After completing authorization in the browser, the token is stored automatically and can be referenced in your config immediately.

---

## Usage

### CLI

The `aviary` CLI gives you full control over the server, agents, tasks, memory, and more. Every operation available in the web control panel is also available here.

#### Server

```shell
aviary start              # Start the Aviary server
aviary stop               # Stop the Aviary server
aviary status             # Show server status, uptime, and connected agents
aviary logs               # Tail Aviary server logs
aviary token              # Show or regenerate the server access token
aviary doctor             # Validate configuration and credentials
aviary upgrade            # Upgrade Aviary to the latest release
```

#### Interactive Setup

```shell
aviary configure              # Walk through full initial setup
aviary configure providers    # Authenticate with AI providers (OAuth or API key)
aviary configure agents       # Add or edit agents interactively
aviary configure skills       # Enable and configure installed skills
aviary configure general      # Configure shared runtime settings
aviary configure server       # Configure server port and TLS options
aviary configure browser      # Configure browser automation settings
aviary configure scheduler    # Configure concurrency and task defaults
```

`aviary configure` is the fastest way to get started. Running it with no arguments walks you through the full setup end-to-end. Each subcommand opens a targeted wizard for that section and writes the result directly to `aviary.yaml`—no manual YAML editing required.

#### Models

```shell
aviary models list                   # List all supported provider/model pairs
aviary models list --provider openai # Filter by provider
```

#### Agents

```shell
aviary agent list                            # List all configured agents and their current state
aviary agent run <name> "<message>"          # Send a message to an agent and stream the response
aviary agent run <name> --file prompt.txt    # Send a message from a file
aviary agent run <name> --bare               # Run without system prompt, rules, memory, or tools
aviary agent run <name> --history=false      # Run without prior session history
aviary agent stop <name>                     # Immediately stop all work in progress for an agent
aviary agent template-sync <name>            # Sync embedded template files into an agent directory
```

**Examples:**

```shell
aviary agent run assistant "What's on my calendar today?"
aviary agent run assistant "Summarize the last 10 Slack messages in #general"
aviary agent run researcher "Research competitors to Aviary and write a report" \
                --file context.txt
```

Responses stream to your terminal as the agent works. Multiple agents can run in parallel—kick off several tasks and they'll all run simultaneously.

#### Scheduled Tasks

Tasks can be triggered by a cron schedule (`schedule:`) or by file system changes (`watch:`). Both kinds are managed the same way:

```shell
aviary task list                      # List all tasks, their trigger type, and last run status
aviary task run <name>                # Manually trigger a task right now
aviary task stop                      # Stop all currently running scheduled task jobs

aviary job list                       # Show job history across all tasks
aviary job list --task <name>         # Show job history for a specific task
aviary job logs <job-id>              # Stream logs for a specific job run
```

#### Tools

```shell
aviary tool <name> [--field value ...]   # Run any MCP tool by name with the given arguments
```

#### Browser Control

Aviary can drive a Chromium-compatible browser for agentic web operations using the Chrome DevTools Protocol.

```shell
aviary browser open <url>                                # Navigate to a URL
aviary browser click --selector "<css-selector>"         # Click an element
aviary browser type --selector "<css-selector>" "<text>" # Type into an input
aviary browser screenshot                                # Capture a screenshot
aviary browser close                                     # Close the browser session
```

**Examples:**

```shell
aviary browser open https://github.com/myorg/myrepo
aviary browser click --selector "#new-issue-button"
aviary browser type --selector "#issue-title" "Bug: login fails on Safari"
```

You can also instruct agents to use the browser directly in conversation—they have full access to these same browser controls:

```shell
aviary agent run assistant "Go to our GitHub repo and list all open PRs older than 30 days"
```

#### Memory

Agents maintain persistent memory across conversations. You can search, inspect, and manage that memory directly.

```shell
aviary memory search <agent> "<query>"     # Search an agent's memory
aviary memory show <agent>                 # Display the full memory for an agent
aviary memory clear <agent>               # Wipe all memory for an agent
```

#### Authentication

```shell
aviary auth login <provider>       # Authorize via OAuth (opens browser)
aviary auth set <name> <value>     # Store a credential (API key or token) by name
aviary auth get <name>             # Show the credential name (value is masked)
aviary auth list                   # List all stored credential names
aviary auth delete <name>          # Remove a stored credential
```

---

### Web Control Panel

The web control panel is available at `https://localhost:16677`. Log in with your Aviary token.

#### Chat

The main view is a chat interface for conversing with your agents in real time. Select any agent from the sidebar to open its session.

- Messages stream in as the agent responds—no waiting for a full reply.
- Send text, images, and other media; agents can respond with media too.
- Type **stop** (or click the Stop button) to immediately halt everything the agent is doing and clear its queue.
- Multiple agent conversations can be open at once, each running in parallel.

#### Jobs

The **Jobs** panel shows all scheduled task runs across all agents. From here you can:

- See upcoming scheduled runs and the status of past runs (pending, running, completed, failed).
- Trigger any task immediately with **Run Now**.
- Watch live logs stream in for a running task.
- Cancel a task mid-run.

#### Settings

The **Settings** panel is the primary place to manage your Aviary configuration without editing YAML directly. Changes are saved back to `aviary.yaml` and take effect immediately.

**Agents & Tasks** — configure agents, their models, fallbacks, memory settings, permissions (tool presets, filesystem access, exec access), channel allowlists, and scheduled tasks. Each agent has tabbed sections for General, Permissions, Channels, Files, and Tasks.

**Providers** — authenticate with AI providers via API key or OAuth, and manage stored credentials.

**Models** — browse the full catalog of supported models with token limits and capability badges.

**System** — configure server port, TLS, browser binary, scheduler concurrency, and installed skills.

#### Sessions

Every agent has a main interactive session (visible in Chat) plus a separate session per scheduled task run. The **Sessions** panel gives you a full history of every conversation and task execution, with the ability to replay or review past outputs.
