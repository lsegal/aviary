# Aviary: the AI Agent Nest

<p align="center"><img src="web/public/logo.png" alt="Aviary logo" width="200" /></p>

Aviary is an autonomous AI agent orchestrator. Connect your AI models (OpenAI, Claude, Gemini, and more) to your messaging channels (Slack, Signal, Discord), set up scheduled tasks, and let your agents work for you—all managed from a CLI or a web-based control panel.

## Table of Contents

- [Setup](#setup)
- [Configuring](#configuring)
- [Usage](#usage)
  - [CLI](#cli)
  - [Web Control Panel](#web-control-panel)

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
  # tls:
  #   cert: /path/to/cert.pem
  #   key: /path/to/key.pem

agents:
  - name: assistant
    description: "General purpose assistant"
    model: anthropic/claude-sonnet-4.5  # Overrides models.defaults.model for this agent
    memory: shared                      # Share memory across agents ("shared"), keep private ("private"),
                                        # or use a named pool (e.g. "team-memory")
    channels:
      - type: slack
        token: auth:slack:workspace     # References a stored credential (see Auth below)
        id: "workspace-bot"
        allowFrom:
          - "@lsegal"                   # Only respond to messages from this user
          - "@ops-team"                 # Or this user group

      - type: discord
        token: auth:discord:bot-token
        id: "team-bot"
        allowFrom:
          - "*"                         # Allow messages from anyone in the channel

      - type: signal
        id: auth:signal:myphone        # Stable channel id; for Signal use the registered phone number
        allowFrom:
          - "+15551234567"              # Allowlist by phone number; omit to allow all

    tasks:
      - name: daily-briefing
        schedule: "0 9 * * *"           # Standard cron syntax
        prompt: "Give me a morning briefing based on recent activity."
        target: route:slack:workspace-bot:C123
                                        # route:<type>:<id>:<target> pins delivery to
                                        # a configured channel; omit for silent

      - name: organize-downloads
        watch: ~/Downloads/**           # Trigger on any file change, recursively
        prompt: "A file was added or changed in ~/Downloads. Rename it using a clear, descriptive name based on its contents."
        target: route:slack:workspace-bot:C123

models:
  providers:
    anthropic:
      auth: auth:anthropic:default
    openai:
      auth: auth:openai:default
    gemini:
      auth: auth:gemini:default
  defaults:
    model: anthropic/claude-sonnet-4.5  # Used when an agent doesn't specify a model
    fallbacks:                          # Try these in order if the default fails
      - openai/gpt-4o

browser:
  binary: /usr/bin/chromium             # Path to your Chromium-compatible browser binary
  cdp_port: 9222                        # Chrome DevTools Protocol port

scheduler:
  concurrency: auto                     # "auto" uses all available CPUs; or set a number
```

### Authentication

Auth values use the format `auth:<provider>:<name>` to reference credentials stored securely—either in your system keychain or in a separate auth file. This keeps secrets out of your main config.

#### API Keys

Store credentials manually by name:

```shell
aviary auth set anthropic:default sk-ant-...
aviary auth set openai:default sk-...
aviary auth set slack:workspace xoxb-...
```

Then reference them anywhere in `aviary.yaml` as `auth:anthropic:default`, `auth:openai:default`, etc.

#### OAuth

For providers that support OAuth (Anthropic, OpenAI, Google, Slack, and others), Aviary can manage the full OAuth flow for you:

```shell
aviary auth login anthropic    # Opens a browser window to authorize with Anthropic
aviary auth login openai       # Opens a browser window to authorize with OpenAI
aviary auth login slack        # Opens a browser window to authorize your Slack workspace
```

After completing authorization in the browser, the token is stored automatically under the provider's default name (e.g. `auth:anthropic:default`) and can be referenced in your config immediately.

---

## Usage

### CLI

The `aviary` CLI gives you full control over the server, agents, tasks, memory, and more. Every operation available in the web control panel is also available here.

#### Server

```shell
aviary start              # Start the Aviary server
aviary stop               # Stop the Aviary server
aviary status             # Show server status, uptime, and connected agents
```

#### Interactive Setup

```shell
aviary configure              # Walk through full initial setup
aviary configure auth         # Add or update credentials (API keys, OAuth)
aviary configure agents       # Add or edit agents interactively
aviary configure channels     # Configure channels for an agent
aviary configure models       # Set up model providers and defaults
aviary configure scheduler    # Configure concurrency and task defaults
```

`aviary configure` is the fastest way to get started. Running it with no arguments walks you through the full setup end-to-end. Each subcommand opens a targeted wizard for that section and writes the result directly to `aviary.yaml`—no manual YAML editing required.

#### Agents

```shell
aviary agent list                            # List all configured agents and their current state
aviary agent run <name> "<message>"          # Send a message to an agent and stream the response
aviary agent run <name> --file prompt.txt    # Send a message from a file
aviary agent stop <name>                     # Immediately stop all work in progress for an agent
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

#### Tasks

The **Tasks** panel shows all scheduled tasks across all agents. From here you can:

- See upcoming scheduled runs and the status of past runs (pending, running, completed, failed).
- Trigger any task immediately with **Run Now**.
- Watch live logs stream in for a running task.
- Cancel a task mid-run.

#### Agents

The **Agents** panel gives an overview of all configured agents. Click any agent to:

- View its active and recent sessions.
- See which channels it's currently subscribed to.
- Browse a summary of its memory.
- Stop all in-progress work.

#### Sessions

Every agent has a main interactive session (visible in Chat) plus a separate session per scheduled task run. The **Sessions** panel gives you a full history of every conversation and task execution, with the ability to replay or review past outputs.
