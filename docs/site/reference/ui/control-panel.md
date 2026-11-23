# Control Panel Surface

This page summarizes the implemented browser UI as it exists today. The sidebar has two top-level sections: **Agents**, listing every agent with **+ Add Agent**, and **System**.

## Agent Pages

Each agent's pages, in sidebar order:

| Page | Route | Exposed Functionality |
| --- | --- | --- |
| Chat | `/agents/<agent>/chat/:sessionId?` | Real-time conversation and session management with that agent. |
| Code Loop | `/agents/<agent>/projects` | One board of the jobs of every project the agent owns, with no settings; each project's board at `/agents/<agent>/projects/<owner%2Frepo>` and its job runs under `/jobs/<number>`. |
| Code Loop Settings | `/projects/settings` | Opened from the boards' Settings gear: each agent's projects (add, remove, reports), the Code Loop settings every agent's GitHub projects share (concurrency, delivery, issue selection, merging), and the job clones on disk. |
| Channels | `/agents/<agent>/channels/:channel?` | The agent's Slack, Discord and Signal channels; a channel's conversation in the chat UI, and its settings at `/agents/<agent>/channels/<name>/settings`. `/agents/<agent>/settings/channels` redirects here. |
| Scheduled Tasks | `/agents/<agent>/tasks` | Run stats and analytics (job queue, compile attempts), and the agent's tasks as cards or a list with quick actions; `?task=<name>` opens a task's settings. A task's page, at `/agents/<agent>/tasks/<name>`, is a dashboard for that task: run stats, the current or last run's live logs, the run history (each run at `…/runs/<job id>`), Run now, Pause/Resume and Stop, a gear to its settings, and the session all its runs share in a resizable bottom chat panel the chat icon hides. The sidebar always lists the tasks; a row opens the task's page, and its **…** menu has **Settings** and **Delete**. |
| Files | `/agents/<agent>/files` | The agent's root markdown files. |
| Settings | `/agents/<agent>/settings` | General (name, model, fallbacks, working directory), and Permissions (`/settings/permissions`) sections. |

`/agents` opens the first agent's chat; `/agents?add=1` adds an agent.

## System Pages

| Page | Route | Exposed Functionality |
| --- | --- | --- |
| Overview | `/system/overview` | Health, counts, validation, initial setup, and a card for every System page. |
| Usage | `/system/usage` | Token and session analytics. |
| Tools | `/system/tools` | MCP tool catalog and direct invocation. |
| Skills | `/system/skills` | Every installed skill on one page: the installed version of each skill Aviary ships in each provider's CLI, and search, filtering, activation and per-skill settings for the skills Aviary agents use. |
| Sessions | `/system/sessions` | Session list, stop, create, and remove operations. |
| Models | `/system/models` | Built-in provider and model catalog. |
| Logs | `/system/logs` | Streaming log inspection and filtering. |
| Daemons | `/system/daemons` | Process monitoring and restart support. |
| Watches | `/system/watches/<owner%2Frepo>` | Boards of runtime watches no agent owns. |

The System sidebar item is the Overview; the section lists the other pages and starts collapsed.

## Settings

Settings opens from the sidebar's gear menu, which also holds **Restart** and **Logout**.

| Tab | Route | Exposed Functionality |
| --- | --- | --- |
| General | `/system/settings` | Server, models, browser, scheduler, and search settings. |
| Providers & Authentication | `/system/providers` | API keys, OAuth flows, and secret management. |

Skills detects built-in skills plus disk-installed skills from `~/.config/aviary/skills` and `~/.agents/skills`, and is where users can confirm installs from `npx skills find`, [skills.sh](https://skills.sh/), or commands like `npx skills add --global -a universal 4ier/notion-cli`.

Other routes: `/login` (token-based access to the local control plane) and `/onboarding` (the setup wizard, which every page redirects to while no agent is configured).
