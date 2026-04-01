# Control Panel

The Aviary control panel is a web UI served at `https://localhost:16677`. It provides a browser-based interface for every major operation: chatting with agents, editing configuration, monitoring jobs, and managing credentials.

Log in with the bearer token from `~/.config/aviary/token`.

---

## Overview

The Overview page is the home screen. It shows:

- **Agent health** — count of configured agents and their current state (idle, running, error).
- **Job summary** — count of recent jobs, in-progress indicators, and failure flags.
- **Config validation** — any errors or warnings in `aviary.yaml`, refreshed on each load.
- **Setup wizard** — appears automatically when no agents have been configured yet. Walks through provider auth and first-agent creation.

---

## Chat

The Chat view (`/chat/:agent?/:sessionId?`) is the primary conversational interface.

- **Agent tabs** — switch between configured agents. Each agent's session list loads in the sidebar.
- **Session switching** — select a previous session to resume it, or create a new one with the **+** button.
- **Streaming responses** — the agent's reply streams token-by-token. Tool events appear inline as collapsible cards.
- **Media attachments** — images sent or received are rendered inline.
- **Stop** — click the stop button or send `/stop` to cancel an in-progress agent run immediately.
- **Session controls** — rename, delete, or create sessions from the session panel.

---

## Settings

Settings (`/settings/*`) provides a live editor for every configuration domain. Changes are validated before saving and take effect immediately upon write. A rotating backup is created on every save.

### General

Covers the `server`, `models`, `browser`, `scheduler`, and `search` config sections. Port and TLS changes require a server restart to take effect.

### Agents

Each configured agent has its own editor panel with tabs for:

- **General** — name, model, fallbacks, working directory, rules, memory, and verbose mode.
- **Permissions** — preset selector, tool allow/blocklist, filesystem paths, and exec rules.
- **Channels** - add, edit, or remove Slack/Discord/Signal channel connections and their `allow_from` rules. Slack channels can also validate the bot token and browse visible workspace channels directly from the UI.
- **Tasks** — add, edit, or remove scheduled and file-watch tasks.
- **Files** — browse and edit root-level markdown files in the agent's data directory (RULES.md, MEMORY.md, etc.).

### Providers

Manages API credentials and OAuth sessions:

- **Stored keys** — list, add, and delete named credentials.
- **OAuth login** — one-click flows for Anthropic, Gemini, OpenAI, and GitHub Copilot.
- Provider connectivity status is shown alongside each entry (cached for 30 seconds).

### Skills

Toggle individual skills on or off and edit per-skill settings. Skills are detected immediately when added or removed.

Aviary detects built-in skills plus disk-installed skills from `AVIARY_CONFIG_BASE_DIR/skills` and `~/.agents/skill`. You can search for installable skills with `npx skills find` or on [skills.sh](https://skills.sh/).

### Sessions

Session administration across all agents: list sessions, stop active work, and delete sessions. Useful for cleanup without opening each agent's Chat view.

---

## System

The System area groups operational dashboards.

### Usage (`/usage`)

Token analytics for the last 30 days (or a custom range):

- Input and output tokens per day, per agent, per model, and per provider.
- Total cost estimates when provider pricing is available.

### Jobs (`/jobs`)

The job queue and execution history:

- **Queue view** — all pending, running, completed, and failed jobs with status badges.
- **Job details** — click a job to see its logs, timing, and the session it ran in.
- **Compile attempts** — for precomputed prompt tasks, inspect the LLM compilation stages and the resulting Lua script.
- **Manual trigger** — run any pending job immediately with the Run Now button.
- **Stop** — cancel any pending or running job.

### Tools (`/system/tools`)

Browsable catalog of all registered MCP tools:

- Search by name or description.
- Click any tool to open a form-based runner: fill in arguments and invoke the tool directly without an agent.
- Useful for debugging, testing, and manual operations.

### Skills (`/system/skills`)

Marketplace-style view of installed skills with descriptions and enablement toggles. Links to per-skill settings in the Settings → Skills panel.

This is also the easiest place to confirm that a skill installed with a command like `npx skills add --global -a universal 4ier/notion-cli` has been detected by Aviary.

### Models (`/system/models`)

Searchable catalog of built-in model IDs grouped by provider. Shows availability status based on the most recent connectivity check.

### Logs (`/logs`)

Live streaming log output from the server process. Filter by log level and component. Useful for diagnosing errors in real time.

### Daemons (`/daemons`)

Process monitor for long-running background workers:

- Channel listeners (Slack, Discord, Signal)
- File watchers
- Scheduled task cron runner

Each daemon shows its PID, uptime, and health status. Restart a daemon without restarting the whole server.

---

## Keyboard Shortcuts

| Shortcut | Action |
| --- | --- |
| `Ctrl+Enter` | Send message (Chat) |
| `Esc` | Stop active agent run |

---

## Streaming Cancellation

If the browser tab is closed or navigated away while an agent is running, the run continues in the background. The response is stored in the session and visible when you return. Use the Stop button or `session_stop` MCP tool to actively cancel it.
