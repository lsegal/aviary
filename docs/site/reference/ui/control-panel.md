# Control Panel Surface

This page summarizes the implemented browser UI as it exists today.

## Route Groups

| Area | Primary Routes | User Value |
| --- | --- | --- |
| Login | `/login` | Token-based access to the local control plane. |
| Overview | `/overview` | Health, counts, validation, and initial setup. |
| Chat | `/chat/:agent?/:sessionId?` | Real-time conversation and session management. |
| Settings | `/settings/*` | Live configuration editing across runtime domains. |
| Usage | `/usage` | Token and session analytics. |
| Jobs | `/jobs` | Scheduled work, queue state, logs, and compile attempts. |
| Tools | `/system/tools` | MCP tool catalog and direct invocation. |
| Skills | `/system/skills` | Installed skill browsing and activation control. |
| Models | `/system/models` | Built-in provider and model catalog. |
| Logs | `/logs` | Streaming log inspection and filtering. |
| Daemons | `/daemons` | Process monitoring and restart support. |

## Settings Tabs

| Tab | Exposed Functionality |
| --- | --- |
| General | Server, models, browser, scheduler, and search settings. |
| Agents | Agent definitions, task definitions, permissions, channels, and agent root files. |
| Skills | Config-backed skill enablement and settings. |
| Sessions | Session list, stop, create, and remove operations. |
| Providers | API keys, OAuth flows, and secret management. |

Settings detects built-in skills plus disk-installed skills from `~/.config/aviary/skills` and `~/.agents/skill`, including skills installed from [skills.sh](https://skills.sh/).

## System Tabs

| Tab | Exposed Functionality |
| --- | --- |
| Usage | Token analytics and session cost-style breakdowns. |
| Jobs | Job history, schedules, compile attempts, and outputs. |
| Tools | Tool and skill catalog plus runner UI. |
| Skills | Marketplace-style installed skill management. |
| Models | Searchable model catalog. |
| Logs | Live runtime logs. |
| Daemons | Running process inspection and restart actions. |

The Skills view is where users can confirm installs from `npx skills find`, [skills.sh](https://skills.sh/), or commands like `npx skills add --global -a universal 4ier/notion-cli`.
