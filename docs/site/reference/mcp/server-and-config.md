# Server and Config Tools

These tools inspect and manage the running Aviary server and its configuration. Config mutation tools have file-system side effects and require the `full` permissions preset.

---

## ping

Check server connectivity.

**Arguments:** none

**Returns:** Text `"pong"`.

---

## server_status

Return basic server status and uptime.

**Arguments:** none

**Returns:** JSON status object.

```json
{ "status": "running" }
```

---

## server_version_check

Check the current Aviary version against the latest GitHub release.

**Arguments:** none

**Returns:** JSON object with version information and whether an update is available.

```json
{
  "current": "0.9.1",
  "latest": "0.9.2",
  "update_available": true,
  "release_url": "https://github.com/lsegal/aviary/releases/tag/v0.9.2"
}
```

**Side effects:** May trigger an asynchronous check against GitHub Releases.

---

## server_upgrade

Upgrade Aviary to the latest release and restart the server if needed.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `version` | string | | Target version to install; defaults to the latest |

**Returns:** JSON `{ started: true, emulated?: true }`. `emulated` is set when the upgrade was simulated (e.g. in a development build).

**Side effects:** Downloads and installs the new binary. May restart the server process.

---

## config_get

Return the current server configuration as a JSON object. This is the same structure as `aviary.yaml`, serialized as JSON.

**Arguments:** none

**Returns:** JSON `Config` object.

```json
{
  "server": { "port": 16677 },
  "agents": [
    { "name": "assistant", "model": "anthropic/claude-sonnet-4-6" }
  ],
  "models": {
    "providers": { "anthropic": { "auth": "ANTHROPIC_API_KEY" } },
    "defaults": { "model": "anthropic/claude-sonnet-4-6" }
  }
}
```

---

## config_save

Save an updated configuration. The full config object must be provided as a JSON string. Aviary validates the config and writes `aviary.yaml` before applying the new settings.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `config` | string | yes | Full JSON-encoded `Config` object |

**Returns:** Text confirmation.

**Side effects:**
- Validates the config; returns an error for invalid configurations.
- Rotates a backup copy of the previous `aviary.yaml`.
- Writes the new `aviary.yaml`.
- Creates agent data directories for any new agents.
- Updates channel metadata and session targets.

**Note:** Fields that change the server port or TLS settings take effect only after a server restart.

---

## config_restore_latest_backup

Restore `aviary.yaml` from the most recent rotating backup file (`backups/aviary.yml.bak.1`).

**Arguments:** none

**Returns:** Text confirmation.

**Side effects:** Overwrites `aviary.yaml` with the backup content. The live configuration reloads after the write.

---

## config_validate

Validate the current configuration and credentials. Returns all issues found. Provider connectivity is checked asynchronously; results improve on subsequent calls within the 30-second cache window.

**Arguments:** none

**Returns:** JSON array of validation issue objects.

```json
[
  {
    "level": "error",
    "field": "agents[0].model",
    "message": "model is required"
  },
  {
    "level": "warning",
    "field": "models.providers.anthropic",
    "message": "provider connectivity check pending"
  }
]
```

An empty array means no issues were found. `level` is `"error"` for issues that prevent normal operation and `"warning"` for non-blocking concerns.

---

## config_task_rename

Rename a task for an agent. This tool handles both inline tasks defined in `aviary.yaml` and tasks backed by markdown files.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | The agent that owns the task (agent name) |
| `task` | string | yes | The current task name to rename |
| `new_name` | string | yes | The new task name to apply |

**Behavior:**
- If the task is defined inline in `aviary.yaml`, the tool updates the `name` field in the config and saves the configuration.
- If the task is file-backed (a `.md` in the agent tasks directory), the tool updates the YAML frontmatter `name` and renames the file on disk to match the new task filename derived from the `name`.

**Returns:** Text confirmation on success or an error describing the failure (conflict, missing agent/task, or validation error).

**Side effects:**
- May write or remove files under the agent tasks directory.
- Saves `aviary.yaml` when inline tasks are modified.
- Reloads the live configuration and reconciles agent and scheduler state after changes.

**Permissions:** Requires the `full` permissions preset because it mutates server configuration and the filesystem.
