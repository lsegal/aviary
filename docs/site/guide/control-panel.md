# Control Panel

The Aviary control panel is a web UI served at `https://localhost:16677`. It provides a browser-based interface for every major operation: chatting with agents, editing configuration, monitoring jobs, and managing credentials.

Log in with the bearer token from `~/.config/aviary/token`.

---

## Sidebar

The sidebar has two top-level sections:

- **Agents** — always expanded, listing every configured agent, with **+ Add Agent** at the end. Each agent's pages open under it: **Chats**, **Code Loop**, **Channels**, **Scheduled Tasks**, **Files** and **Agent Settings**. Moving from one agent to another keeps you on the same page.
- **System** — the instance's overview, and under it the instance's own pages: **Usage**, **Tools**, **Skills**, **Sessions**, **Models**, **Logs** and **Daemons**. The section starts collapsed; the overview lists every System page as a card.

The **gear** at the bottom left opens a menu of **Settings**, **Edit sidebar**, **Restart** (a safe restart that waits for running jobs) and **Logout**.

On narrow screens the sidebar becomes a row of buttons along the top: one per agent plus **System**, or, with a single agent, that agent's pages plus **System**. The current section's pages, projects, tasks or channels show in a second row below it. Both rows stay on one line and scroll sideways instead of wrapping.

### Edit sidebar

**Edit sidebar** opens [Settings → Display](#settings-display) at its **Sidebar** section, which has two lists: **Agent pages** (**Pages** when there is only one agent) and **System pages**. Drag an item by its handle (on touch screens, long-press the row) and the other items slide aside to make room, or move it one place with its up and down buttons or the arrow keys on its handle. Each item's eye button hides or shows it. Changes save as you go and are stored on the server per user, so they follow you to other devices.

The **Pages** order is shared: every agent's pages in the sidebar, the collapsed rail and the top row on narrow screens all follow it. When pages are hidden, the sidebar offers **Show N hidden pages** to list them for the current visit. **Reset sidebar** restores the built-in order and shows every page again.

### Navigation

Every page shares one page header, the same on System pages, settings pages, Code Loop boards, channels, chats, files, scheduled tasks and job details. It has two rows:

- **Breadcrumb bar.** A **←** arrow back to the parent page, such as a task's settings back to the task or a task back to **Scheduled Tasks**, then a breadcrumb from the page's agent or System down to the current page, such as `Aviary › Scheduled Tasks › release-aviary`. Every crumb but the current one links to its page. On narrow screens the bar shortens to `← Parent › Page`.
- **Title row.** The section's icon in a muted tile, the page's title in bold, and, where the page has them, a status chip (a task's **Enabled**, a channel's **Enabled** or **Disabled**, a project's **live**) and a one-line subtitle (a task's type and schedule, a project's issue filter and delivery). The page's actions sit on the right: outlined buttons with an icon and a label, such as **Run now**, **Pause**, **Refresh** or **Save**, then icon-only buttons such as the chat or logs toggle and the gear for the page's settings. On narrow screens the actions wrap below the title instead of scrolling sideways.

Some pages add a row of stat chips below the header, such as a task's run counts and a Code Loop board's job counts. Leaving a page with unsaved changes through the back arrow or a crumb asks first.

With the sidebar collapsed to its icon rail, hovering or focusing an icon that has subpages opens a flyout listing them: another agent's icon lists its pages, **System** lists the System pages, and an agent page's icon lists its projects, tasks, channels or files.

On a project, task, channel or file page, the breadcrumb's current crumb has a caret that opens a switcher, like GitHub's repository switcher: a search box over that page's siblings, so you can jump straight to another project, task, channel or file. `Alt+S` (`⌥S` on a Mac) opens it from anywhere on the page.

### Reconnecting

When the control panel can't reach the server, for example during a restart, it shows **Server unavailable, reconnecting…** and keeps retrying instead of sending you to the login page. It continues where you were once the server answers. While you are signed in, the connection indicator shows **Disconnected**; click it to retry at once.

---

## Agents

Everything an agent owns lives under its entry (`/agents/<agent>/…`). Changes on the agent's Tasks, Files and Agent Settings pages are validated before saving and take effect immediately upon write. A rotating backup is created on every save.

### Chat (`/agents/<agent>/chat/:sessionId?`)

The primary conversational interface. Which agent you talk to is the agent you opened in the sidebar.

- **Session switching** — select a previous session to resume it, or create a new one with the **+** button.
- **Streaming responses** — the agent's reply streams token-by-token. Tool events appear inline as collapsible cards.
- **Media attachments** — images sent or received are rendered inline.
- **Stop** — click the stop button or send `/stop` to cancel an in-progress agent run immediately.
- **Session controls** — rename, delete, or create sessions from the session panel.

### Code Loop (`/agents/<agent>/projects`)

One board of the jobs of every [project](/guide/projects) the agent owns: the same search, state filters and job cards as a single project's board, with each card naming its project. The page holds no settings; its **Settings** gear opens the agent's projects in [Code Loop Settings](#projects-settings-projects-settings). Each project's own board opens beneath it (`/agents/<agent>/projects/<owner%2Frepo>`), and the sidebar always lists them, expanded under **Code Loop** on every page, with live job counts. **+ Add project** in the sidebar adds one to the agent.

The board's state chips (Starting, Paused, Running, Done, Failed) each toggle on and off, showing jobs in any selected state; with none selected every job and scheduled task shows, and **Clear** deselects them all. Each board remembers its own selection across pages and reloads, and the URL carries it as `?state=running,failed`, so a shared link opens the same filter.

### Code Loop Settings (`/projects/settings`)

Opened from the **Settings** gear on any Code Loop board. Every agent's projects, one tab per agent (`/projects/settings?agent=<agent>`): add and remove them, set their reports, and see the models their jobs run on. Below them, the one set of [Code Loop](/guide/projects) settings, shared by every project of every agent: how many jobs run at once across all of them, how GitHub is read, which issues are picked up, merging and changelogs, the approval mode, and where clones go. It also lists the job clones on disk, in a list that scrolls once it grows long.

### Scheduled Tasks (`/agents/<agent>/tasks`)

The one place for the agent's scheduled and file-watch tasks and their runs. **+ New task** adds one.

- **Summary** — stat cards for the chosen range (Today, 7d, 30d or custom dates): runs over time (succeeded and failed), success rate, average and median duration, runs coming up in the next 24 hours, running and pending runs, and recent failures.
- **More analytics** — expands to the runs by day, the job queue with a status filter, paging, Run Now and Stop, and below it the latest compile attempts of precomputed prompt tasks (with their LLM stages and resulting Lua script). Clicking a run opens its details: timing, attempts, and its live or saved output.
- **Tasks** — each task as a compact card with its enabled switch, its agent or target project, its schedule in words with its next run (or "paused"), its last run, a running-now dot, and quick actions: run now, pause or resume, view runs, and last the gear (**Task settings**), which opens the task's full settings. Search and the Enabled, Paused, Running and Failed chips filter them. The Cards / List toggle switches to a dense list with the same fields, and the choice is remembered.

Clicking a task opens its editor (`/agents/<agent>/tasks?task=<name>`), which has its own **Save Changes** and asks before leaving unsaved edits. The page refreshes itself, so it has no Reload or Save.

### Files (`/agents/<agent>/files`)

Browse and edit root-level markdown files in the agent's data directory (RULES.md, MEMORY.md, etc.).

### Channels (`/agents/<agent>/channels`)

The agent's Slack, Discord and Signal channels, listed in the sidebar below **Code Loop**. **+** adds a channel and creates its session. Clicking a channel opens its conversation (`/agents/<agent>/channels/<name>`) in the chat UI; the header's gear opens its settings (`/agents/<agent>/channels/<name>/settings`): connection, deliver-to conversation, model, tools and `allow_from` rules. Slack channels can also validate the bot token and browse visible workspace channels. A channel's **…** menu offers **Enable** or **Disable**, **Rename** (keeping its session), **Settings** and **Delete**, which deletes its sessions or keeps them as regular chats (see [Deleting data with an object](#deleting-data-with-an-object)). A new channel starts disabled until its connection settings are filled in. Disabled channels are greyed out with a disabled marker in the sidebar and the narrow-screen row, and listed after the enabled ones. The old `/agents/<agent>/settings/channels` address redirects here.

### Agent Settings (`/agents/<agent>/settings`)

The agent's own settings, as sections on one scrolling page. **Save Changes** saves every section at once, and **Reload** discards unsaved edits. A section's URL opens the page scrolled to it.

- **General** — name, model, fallbacks, working directory, and verbose mode.
- **Permissions** (`/agents/<agent>/settings/permissions`) — preset selector, tool allow/blocklist, filesystem paths, and exec rules.
- **Danger Zone** — **Remove Agent** deletes the agent, after a confirmation.

### Deleting data with an object

Deleting an agent, a scheduled task, a workflow, a channel or a Code Loop project asks for confirmation with a **Delete session data and files** checkbox, checked by default. The dialog lists what checking it deletes, such as "1 session, 34 messages, 12 runs, 3 files", counted by [`delete_preview`](/reference/mcp/tasks-and-jobs#delete-preview). Workflows have a Danger Zone too, on the Edit workflow form; see [Workflows](/guide/workflows#deleting-a-workflow).

- **Checked** deletes the object with its sessions and their messages, its run history, and its media. For an agent it deletes the whole data directory: `AGENTS.md`, `MEMORY.md`, `memory/`, `notes/`, sessions and other files. For a project it deletes the project's job records, job transcripts and the clones no job still uses, once its jobs have stopped.
- **Unchecked** only removes the object from the configuration and keeps its data on disk. A task's, workflow's or channel's sessions become regular chats titled `<name> (archived)`. An agent's data directory stays, so adding an agent with the same name brings it back.

Either way, no session of the deleted object stays in **Chats** as if it still existed. Sessions left behind by deletes from earlier versions are listed by `aviary doctor`, and `aviary doctor --fix` archives them.

---

## System

The System section groups the instance's settings and operational dashboards.

### Overview (`/system/overview`)

The home screen. It shows:

- **Agent health** — count of configured agents and their current state (idle, running, error).
- **Job summary** — count of recent jobs, in-progress indicators, and failure flags.
- **Config validation** — any errors or warnings in `aviary.yaml`, refreshed on each load.
- **Setup wizard** — while no agent has been configured, every page opens the wizard first, even when a provider such as an installed CLI is already detected. Walks through provider auth and first-agent creation; **Skip for now** opens the control panel without an agent for the rest of the browser session.

### Settings (`/system/settings`)

Opened from the sidebar's gear menu, with four tabs: **General**, **Providers & Authentication**, **Display** and **Notifications**. **General** covers the `server`, `models`, `browser`, `scheduler`, and `search` config sections. Port and TLS changes require a server restart to take effect.

### Settings → Providers & Authentication (`/system/providers`)

Manages API credentials and OAuth sessions:

- **Stored keys** — list, add, and delete named credentials.
- **OAuth login** — one-click flows for Anthropic, Gemini, OpenAI, and GitHub Copilot.
- Provider connectivity status is shown alongside each entry (cached for 30 seconds).

### Settings → Display (`/system/display`) {#settings-display}

Your own display preferences. Every change applies at once, with no Save button, and is stored on the server per user, so it follows you to other devices; the browser keeps a copy so pages open in your theme and size without a flash.

- **Text size** — Small, Default, Large or Extra large; scales the whole control panel.
- **Theme** — Light, Dark or System. System follows your device's light or dark setting and switches with it.
- **Sidebar** — the [Edit sidebar](#edit-sidebar) lists.
- **Reset to defaults** — restores the default text size, theme and sidebar.

An `aviary watch` session's gear menu has only **Display** and **Notifications**, and Settings shows only those tabs.

### Settings → Notifications (`/system/notifications`)

The master switch, delivery methods (in-app, system with the browser's permission state, and macOS Notification Center when no browser is open), each event type's on/off, style and sound, sound (off by default), quiet hours, and **Test notification**. Changes apply at once and are stored on the server per user. The header's bell holds the history. See [Notifications](/guide/notifications).

### Usage (`/system/usage`)

Token analytics for the last 30 days (or a custom range):

- Input and output tokens per day, per agent, per model, and per provider.
- Total cost estimates when provider pricing is available.
- A **Code Loop** summary for the same range: how many project issues jobs worked on, how many of those merged, how many ended failed (an issue that failed and was then retried to a merge counts as merged), the total wall time project job runs took, and the code those jobs changed: the lines added (+) and removed (−) by the pull requests that merged in the range. A table below breaks the same figures down per project repository. A job that opened no pull request adds no lines.

### Tools (`/system/tools`)

Browsable catalog of all registered MCP tools:

- Search by name or description.
- Click any tool to open a form-based runner: fill in arguments and invoke the tool directly without an agent.
- Useful for debugging, testing, and manual operations.

### Skills (`/system/skills`)

Every installed skill on one page: the version of each skill Aviary ships installed in each provider's CLI, and the skills Aviary agents use, which you can search, filter, enable or disable, and configure with their per-skill settings. Counts show how many agent skills are installed, enabled and showing under the current filters. Skills are detected immediately when added or removed.

The **Looking for more skills?** card at the top is the skills browser: Aviary detects built-in skills plus disk-installed skills from `~/.config/aviary/skills` and `~/.agents/skills`. You can search for installable skills with `npx skills find` or on [skills.sh](https://skills.sh/), and the card shows an example install. This is also the easiest place to confirm that a skill installed with a command like `npx skills add --global -a universal 4ier/notion-cli` has been detected by Aviary.

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

A runtime watch (`project_add` with `runtime`) that no agent owns has its board here, listed under **More**.

---

## Installing as an App

The control panel can be installed as a standalone app, with its own icon and window and no browser toolbar. Browsers only offer to install a site served over HTTPS with a **trusted** certificate, so first make sure the device you install on trusts Aviary's certificate. The default self-signed certificate is not enough: Chrome then shows "Not secure" and reports that the app cannot be installed. Pick one of these:

- **mkcert:** create a certificate with [mkcert](/guide/operations#trusted-certificates-with-mkcert) and install its root CA on each device, as described under [Trusting the certificate](/guide/operations#trusting-the-certificate).
- **Tailscale HTTPS:** turn on [HTTPS certificates](https://tailscale.com/kb/1153/enabling-https) for your tailnet, run `tailscale cert <machine>.<tailnet>.ts.net`, point `server.tls.cert` and `server.tls.key` at the files it writes, and open Aviary at `https://<machine>.<tailnet>.ts.net:16677`. Alternatively, `tailscale serve` can terminate TLS in front of an Aviary started with `server.no_tls`.
- **Reverse proxy:** put Aviary behind a proxy with a publicly trusted certificate (for example Caddy or nginx with Let's Encrypt) and set `server.no_tls: true` so the proxy terminates TLS.

Then open the control panel at that address and install it:

- **Android (Chrome):** open the menu and choose **Add to Home screen** → **Install**, or tap the install prompt when Chrome shows one.
- **Desktop Chrome and Edge:** click the install icon at the right end of the address bar, or open the menu and choose **Cast, save, and share → Install page as app** (Chrome) or **Apps → Install this site as an app** (Edge).
- **iOS and iPadOS (Safari):** tap **Share** → **Add to Home Screen**, keep the name **Aviary**, and tap **Add**.

The installed app's icon is the Aviary bird on a white rounded tile on every platform, including the Windows taskbar, Start menu and desktop shortcuts. The browser tab keeps the bird without a tile, which looks better on light and dark tab strips. The theme color matches the control panel header (light gray, or dark gray in dark mode), and the splash screen is white. All icon sizes are generated from `web/icons/icon.svg` by `pnpm icons`.

The installed app opens in its own window. Navigate with the sidebar (or the menu button on narrow screens); on Android the system back gesture also works. Links to other sites, such as GitHub issues or provider sign-in pages, open in your browser instead of replacing the app window.

The app always loads the current control panel from the server, so after an upgrade it shows the new version on the next launch without clearing any cache. Its service worker caches nothing but a "server unreachable" page, which it shows when the app is opened while the Aviary server is stopped or the device can't reach it; tap **Try again** once the server is back. The service worker is only registered by the production server (`aviary serve`), not by `pnpm dev`.

## Keyboard Shortcuts

| Shortcut | Action |
| --- | --- |
| `Ctrl+Enter` | Send message (Chat) |
| `Esc` | Stop active agent run |
| `Alt+S` (`⌥S` on a Mac) | Switch to another project, task, channel or file from its page's breadcrumb (see [Navigation](#navigation)) |

---

## Streaming Cancellation

If the browser tab is closed or navigated away while an agent is running, the run continues in the background. The response is stored in the session and visible when you return. Use the Stop button or `session_stop` MCP tool to actively cancel it.
