# Control Panel

The Aviary control panel is a web UI served at `https://localhost:16677`. It provides a browser-based interface for every major operation: chatting with agents, editing configuration, monitoring jobs, and managing credentials.

Log in with the bearer token from `~/.config/aviary/token`.

---

## Sidebar

The sidebar has two top-level sections:

- **Agents** — always expanded, listing every configured agent, with **+ Add Agent** at the end. Each agent's pages open under it: **Chat**, **GitHub Projects**, **Tasks**, **Files** and **Settings**. Moving from one agent to another keeps you on the same page.
- **System** — the instance's own pages: **Overview**, **Settings**, **Providers & Auth**, **Usage** and **Jobs**, then **More** for Tools, Skills, Sessions, Models, Logs and Daemons.

---

## Agents

Everything an agent owns lives under its entry (`/agents/<agent>/…`). Changes on the agent's Tasks, Files and Settings pages are validated before saving and take effect immediately upon write. A rotating backup is created on every save.

### Chat (`/agents/<agent>/chat/:sessionId?`)

The primary conversational interface. Which agent you talk to is the agent you opened in the sidebar.

- **Session switching** — select a previous session to resume it, or create a new one with the **+** button.
- **Streaming responses** — the agent's reply streams token-by-token. Tool events appear inline as collapsible cards.
- **Media attachments** — images sent or received are rendered inline.
- **Stop** — click the stop button or send `/stop` to cancel an in-progress agent run immediately.
- **Session controls** — rename, delete, or create sessions from the session panel.

### GitHub Projects (`/agents/<agent>/projects`)

The [projects](/guide/projects) the agent owns, configured and watched in one place. The page adds, configures and removes the agent's projects; each project's board opens beneath it (`/agents/<agent>/projects/<owner%2Frepo>`), and the sidebar lists them with live job counts. **+ Add project** in the sidebar adds one to the agent.

### Tasks (`/agents/<agent>/tasks`)

Add, edit, or remove the agent's scheduled and file-watch tasks.

### Files (`/agents/<agent>/files`)

Browse and edit root-level markdown files in the agent's data directory (RULES.md, MEMORY.md, etc.).

### Settings (`/agents/<agent>/settings`)

The agent's own settings, in three tabs:

- **General** — name, model, fallbacks, working directory, and verbose mode. **Remove Agent** deletes the agent.
- **Permissions** (`/agents/<agent>/settings/permissions`) — preset selector, tool allow/blocklist, filesystem paths, and exec rules.
- **Channels** (`/agents/<agent>/settings/channels`) — add, edit, or remove Slack/Discord/Signal channel connections and their `allow_from` rules. Slack channels can also validate the bot token and browse visible workspace channels directly from the UI.

---

## System

The System section groups the instance's settings and operational dashboards.

### Overview (`/system/overview`)

The home screen. It shows:

- **Agent health** — count of configured agents and their current state (idle, running, error).
- **Job summary** — count of recent jobs, in-progress indicators, and failure flags.
- **Config validation** — any errors or warnings in `aviary.yaml`, refreshed on each load.
- **Setup wizard** — appears automatically when no agents have been configured yet. Walks through provider auth and first-agent creation.

### Settings (`/system/settings`)

Covers the `server`, `models`, `browser`, `scheduler`, and `search` config sections. Port and TLS changes require a server restart to take effect.

### Providers & Auth (`/system/providers`)

Manages API credentials and OAuth sessions:

- **Stored keys** — list, add, and delete named credentials.
- **OAuth login** — one-click flows for Anthropic, Gemini, OpenAI, and GitHub Copilot.
- Provider connectivity status is shown alongside each entry (cached for 30 seconds).

### Usage (`/system/usage`)

Token analytics for the last 30 days (or a custom range):

- Input and output tokens per day, per agent, per model, and per provider.
- Total cost estimates when provider pricing is available.

### Jobs (`/system/jobs`)

The job queue and execution history:

- **Queue view** — all pending, running, completed, and failed jobs with status badges.
- **Job details** — click a job to see its logs, timing, and the session it ran in.
- **Compile attempts** — for precomputed prompt tasks, inspect the LLM compilation stages and the resulting Lua script.
- **Manual trigger** — run any configured task or pending job immediately with the Run Now button.
- **Stop** — cancel any pending or running job.

### Tools (`/system/tools`)

Browsable catalog of all registered MCP tools:

- Search by name or description.
- Click any tool to open a form-based runner: fill in arguments and invoke the tool directly without an agent.
- Useful for debugging, testing, and manual operations.

### Skills (`/system/skills`)

Every installed skill on one page: the version of each skill Aviary ships installed in each provider's CLI, and the skills Aviary agents use, which you can search, filter, enable or disable, and configure with their per-skill settings. Skills are detected immediately when added or removed.

Aviary detects built-in skills plus disk-installed skills from `~/.config/aviary/skills` and `~/.agents/skills`. You can search for installable skills with `npx skills find` or on [skills.sh](https://skills.sh/). This is also the easiest place to confirm that a skill installed with a command like `npx skills add --global -a universal 4ier/notion-cli` has been detected by Aviary.

### Sessions (`/system/sessions`)

Session administration across all agents: list sessions, stop active work, and delete sessions. Useful for cleanup without opening each agent's Chat.

### Models (`/system/models`)

Searchable catalog of built-in model IDs grouped by provider. Shows availability status based on the most recent connectivity check.

### Logs (`/system/logs`)

Live streaming log output from the server process. Filter by log level and component. Useful for diagnosing errors in real time.

### Daemons (`/system/daemons`)

Process monitor for long-running background workers:

- Channel listeners (Slack, Discord, Signal)
- File watchers
- Scheduled task cron runner

Each daemon shows its PID, uptime, and health status. Restart a daemon without restarting the whole server.

### Watches (`/system/watches/<owner%2Frepo>`)

A runtime watch started with `aviary watch` that no agent owns has its board here, listed under **More**.

---

## Keyboard Shortcuts

| Shortcut | Action |
| --- | --- |
| `Ctrl+Enter` | Send message (Chat) |
| `Esc` | Stop active agent run |

---

## Streaming Cancellation

If the browser tab is closed or navigated away while an agent is running, the run continues in the background. The response is stored in the session and visible when you return. Use the Stop button or `session_stop` MCP tool to actively cancel it.
