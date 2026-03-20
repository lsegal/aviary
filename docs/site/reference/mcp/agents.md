# Agent Tools

Placeholder page for the tools that create, inspect, update, run, and remove agents.

| Tool | Current Purpose | Doc Status |
| --- | --- | --- |
| `agent_list` | List configured agents and state. | Placeholder |
| `agent_run` | Send a prompt to an agent and stream output. | Placeholder |
| `agent_stop` | Stop in-progress work for one agent. | Placeholder |
| `agent_run_script` | Execute embedded Lua against the tool sandbox. | Placeholder |
| `agent_get` | Return a single agent configuration. | Placeholder |
| `agent_add` | Add a new agent to config. | Placeholder |
| `agent_template_sync` | Sync template files into an agent directory. | Placeholder |
| `agent_update` | Update an existing agent config. | Placeholder |
| `agent_delete` | Remove an agent from config. | Placeholder |
| `agent_rules_get` | Read an agent rules file or rules payload. | Placeholder |
| `agent_rules_set` | Write an agent rules file or rules payload. | Placeholder |

## Follow-Up

- Separate runtime behavior from config mutation behavior.
- Document session targeting and history flags on `agent_run`.
