# MCP Tool Reference

Aviary exposes all of its capabilities as MCP tools. Any MCP-compatible client — Claude Code, another LLM, or a custom integration — can connect to the running server and invoke these tools directly.

Connect at `https://localhost:16677/mcp` using the bearer token from `~/.config/aviary/token`.

## Tool Categories

| Category | Tools | Description |
| --- | --- | --- |
| [Agent Tools](./agents) | `agent_list`, `agent_run`, `agent_stop`, `agent_run_script`, `agent_get`, `agent_add`, `agent_update`, `agent_delete`, `agent_template_sync`, `agent_rules_get`, `agent_rules_set`, `agent_root_file_*` | Lifecycle, execution, and configuration of agents |
| [Session Tools](./sessions) | `session_list`, `session_create`, `session_messages`, `session_history`, `session_stop`, `session_remove`, `session_send`, `session_set_target` | Conversation history and channel delivery |
| [Task and Job Tools](./tasks-and-jobs) | `task_list`, `task_run`, `task_schedule`, `task_stop`, `task_compile_query`, `task_compile_get`, `job_list`, `job_query`, `job_logs`, `job_run_now` | Scheduled automation and execution history |
| [Browser and Channel Tools](./browser-and-channels) | `browser_open`, `browser_tabs`, `browser_navigate`, `browser_wait`, `browser_click`, `browser_keystroke`, `browser_fill`, `browser_text`, `browser_query`, `browser_screenshot`, `browser_resize`, `browser_eval`, `browser_close`, `channel_send_file` | Browser automation and file delivery to channels |
| [Files and Notes Tools](./files-and-notes) | `note_write`, `agent_file_list`, `agent_file_read`, `agent_root_file_list`, `agent_root_file_read`, `agent_root_file_write`, `agent_root_file_delete`, `file_read`, `file_write`, `file_append`, `file_truncate`, `file_delete`, `file_copy`, `file_move`, `exec` | Workspace files, filesystem access, and command execution |
| [Auth Tools](./auth) | `auth_set`, `auth_get`, `auth_list`, `auth_delete`, `auth_login_anthropic`, `auth_login_anthropic_complete`, `auth_login_gemini`, `auth_login_openai`, `auth_login_github_copilot`, `auth_login_github_copilot_complete` | Credential storage and OAuth login flows |
| [Server and Config Tools](./server-and-config) | `ping`, `server_status`, `server_version_check`, `server_upgrade`, `config_get`, `config_save`, `config_restore_latest_backup`, `config_validate` | Server health, upgrades, and configuration management |
| [Usage and Skills Tools](./usage-and-skills) | `usage_query`, `skills_list`, `web_search` | Token analytics, skill discovery, and web search |
| [Memory](./memory) | _(via file tools)_ | Long-term memory via `MEMORY.md` and note files |

## Permissions

Which tools are available to an agent depends on its [permissions preset](/reference/config#agentspermissions):

| Preset | Available groups |
| --- | --- |
| `standard` _(default)_ | session, task, job, browser, memory, search, skills, usage |
| `full` | all tools |
| `minimal` | session, memory, task, job, search, usage |

Tools in the `agent`, `auth`, `exec`, `file`, and `server` groups are blocked by the `standard` preset. Add them via `permissions.tools` or switch to `full` to use them.

The Settings → Providers panel and the Tools runner in the control panel expose all tools regardless of agent permissions.
