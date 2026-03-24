# Agent Tools

Agent tools manage the lifecycle and execution of configured agents — listing them, sending prompts, running scripts, and editing configuration.

Most of these tools require the `full` permissions preset (or explicit allowlist inclusion) because they expose agent management and config mutation capabilities.

---

## agent_list

List all configured agents and their current runtime state.

**Arguments:** none

**Returns:** JSON array of agent state objects.

```json
[
  {
    "id": "assistant",
    "name": "assistant",
    "model": "anthropic/claude-sonnet-4-6",
    "status": "idle"
  }
]
```

---

## agent_run

Send a message to an agent and stream the response. This is the primary tool for interacting with a configured agent.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | yes* | Agent name. Required unless `session_id` is provided. |
| `message` | string | yes | The prompt to send |
| `session` | string | | Session name; defaults to `"main"` |
| `session_id` | string | | Session ID to resume instead of looking up by name |
| `file` | string | | Local file path to attach to the message |
| `media_url` | string | | Image URL (data URL or remote URL) to attach |
| `bare` | bool | | Skip system prompt, rules, memory, and tool preamble |
| `history` | bool | | Include prior session messages (defaults to `true`, `false` when `bare=true`) |
| `include_tool_progress` | bool | | Emit tool-call events in MCP progress notifications (for streaming UIs) |

**Returns:** The agent's full text response. On error, returns an error result with `isError: true`.

**Streaming:** When the caller provides an MCP progress token, text chunks and (optionally) tool events are emitted as `ProgressNotification` messages before the final result is returned. Tool events are prefixed `[tool]`; media URLs are prefixed `[media]`.

**Stop shortcut:** Sending `/stop` as the `message` cancels any active work in the session instead of starting a new run.

---

## agent_stop

Immediately cancel all work in progress for an agent across all sessions.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | yes | Agent name |

**Returns:** Text confirmation (e.g. `agent "assistant" stopped`).

---

## agent_run_script

Run an embedded Lua script directly against the agent's tool sandbox without invoking the LLM. Useful for deterministic automation and compiled task scripts.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `script` | string | yes | Lua source code |
| `agent` | string | | Agent name; defaults to the current session's agent |
| `session` | string | | Session name; defaults to `"main"` |
| `session_id` | string | | Session ID to run in |

**Script environment:**

- `tool.<name>({ ... })` — call any tool available to the agent
- `environment.agent_id` — current agent ID
- `environment.session_id` — current session ID
- `environment.task_id` — task ID (when called from a task)
- `environment.job_id` — job ID (when called from a job)

**Returns:** Text output captured from the script.

---

## agent_get

Return the full configuration for a named agent.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | yes | Agent name |

**Returns:** JSON object containing the full `AgentConfig` for the agent.

---

## agent_add

Add a new agent to the configuration. Creates the agent's data directory and syncs the default template files.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | yes | New agent name (must be unique) |
| `model` | string | | Model ID (e.g. `anthropic/claude-sonnet-4-6`) |
| `fallbacks` | []string | | Ordered fallback model IDs |

**Returns:** Text confirmation.

**Side effects:** Creates `~/.config/aviary/agents/<name>/`, syncs template files, writes updated `aviary.yaml`.

---

## agent_update

Update fields of an existing agent's configuration.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | yes | Agent name to update |
| `model` | string | | New model ID |
| `fallbacks` | []string | | New fallback list |

**Returns:** Text confirmation.

**Side effects:** Writes updated `aviary.yaml`, reconciles running agent state.

---

## agent_delete

Remove an agent from the configuration. The agent's data directory is not deleted.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | yes | Agent name to remove |

**Returns:** Text confirmation.

**Side effects:** Removes agent entry from `aviary.yaml`.

---

## agent_template_sync

Sync the embedded agent template files into an agent's data directory. Missing files are created; existing files are preserved; markdown files are updated only within the `Synced by Aviary` section.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |

**Returns:** Text confirmation.

---

## agent_rules_get

Read the `RULES.md` file for an agent.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | yes | Agent name |

**Returns:** Text content of the agent's `RULES.md`, or empty string if none exists.

---

## agent_rules_set

Write (create or replace) the `RULES.md` file for an agent.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |
| `content` | string | yes | Markdown content |

**Returns:** Text confirmation.

**Side effects:** Creates or overwrites `~/.config/aviary/agents/<name>/RULES.md`.

---

> **Agent file management** (`agent_file_list`, `agent_file_read`, `agent_file_write`, `agent_file_delete`) has moved to [Files and Notes Tools](./files-and-notes).
