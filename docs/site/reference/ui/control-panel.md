# Control Panel Surface

This page summarizes the implemented browser UI as it exists today. The sidebar has two top-level sections: **Agents**, listing every agent with **+ Add Agent**, and **System**.

## Agent Pages

Each agent's pages, in sidebar order:

| Page | Route | Exposed Functionality |
| --- | --- | --- |
| Chat | `/agents/<agent>/chat/:sessionId?` | Real-time conversation and session management with that agent. |
| Code Loop | `/agents/<agent>/projects` | One board of the jobs of every project the agent owns, with no settings; each project's board at `/agents/<agent>/projects/<owner%2Frepo>` and its job runs under `/jobs/<number>`. |
| Code Loop Settings | `/projects/settings` | Opened from the boards' Settings gear: each agent's projects (add, remove, reports), the Code Loop settings every agent's GitHub projects share (concurrency, delivery, issue selection, merging), and the job clones on disk. |
| Scheduled Tasks | `/agents/<agent>/tasks` | Run stats and analytics (job queue, compile attempts), and the agent's tasks as cards or a list with quick actions; `?task=<name>` opens a task's editor. |
| Files | `/agents/<agent>/files` | The agent's root markdown files. |
| Settings | `/agents/<agent>/settings` | General (name, model, fallbacks, working directory), Permissions (`/settings/permissions`) and Channels (`/settings/channels`) tabs. |

`/agents` opens the first agent's chat; `/agents?add=1` adds an agent.

## System Pages

| Page | Route | Exposed Functionality |
| --- | --- | --- |
| Overview | `/system/overview` | Health, counts, validation, and initial setup. |
| Settings | `/system/settings` | Server, models, browser, scheduler, and search settings. |
| Providers & Auth | `/system/providers` | API keys, OAuth flows, and secret management. |
| Usage | `/system/usage` | Token and session analytics. |
| Tools | `/system/tools` | MCP tool catalog and direct invocation. |
| Skills | `/system/skills` | Every installed skill on one page: the installed version of each skill Aviary ships in each provider's CLI, and search, filtering, activation and per-skill settings for the skills Aviary agents use. |
| Sessions | `/system/sessions` | Session list, stop, create, and remove operations. |
| Models | `/system/models` | Built-in provider and model catalog. |
| Logs | `/system/logs` | Streaming log inspection and filtering. |
| Daemons | `/system/daemons` | Process monitoring and restart support. |
| Watches | `/system/watches/<owner%2Frepo>` | Boards of runtime watches no agent owns. |

Overview, Settings, Projects, Providers & Auth and Usage are always shown; the rest sit behind **More**.

Skills detects built-in skills plus disk-installed skills from `~/.config/aviary/skills` and `~/.agents/skills`, and is where users can confirm installs from `npx skills find`, [skills.sh](https://skills.sh/), or commands like `npx skills add --global -a universal 4ier/notion-cli`.

Other routes: `/login` (token-based access to the local control plane) and `/onboarding` (the setup wizard).
