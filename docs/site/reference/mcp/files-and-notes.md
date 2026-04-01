# Files and Notes Tools

These tools provide agents with access to the filesystem and their own workspace directory.

**File tools** (`file_*`) operate on the host filesystem, constrained to paths permitted by the agent's `permissions.filesystem.allowed_paths` rules. They require either the `full` permissions preset or explicit allowlist inclusion.

**Agent file tools** (`agent_file_*`) provide full read/write/delete access to the agent's own data directory. They operate on the calling agent's session context — no `agent` argument is needed (or used). Available under the `standard` preset.

---

## agent_file_list

List all markdown files in the current agent's data directory, including subdirectories and built-in files such as `AGENTS.md`, `RULES.md`, and `MEMORY.md`.

**Arguments:** none (accepts `agent` for backwards compatibility but ignores it)

**Returns:** JSON array of relative file paths.

---

## agent_file_read

Read a markdown file from the current agent's data directory. Use `agent_file_list` first when you need extra context and are unsure which file is relevant.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `file` | string | yes | Path relative to the agent's data directory (e.g. `"MEMORY.md"`, `"notes/foo.md"`) |

**Returns:** Text content of the file.

---

## agent_file_write

Create or replace a markdown file in the current agent's data directory. Supports both root-level files (e.g. `MEMORY.md`) and subdirectories (e.g. `notes/summary.md`). This is the primary way for agents to persist information.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `file` | string | yes | Path relative to the agent's data directory |
| `content` | string | yes | Markdown content to write |

**Returns:** Text confirmation.

---

## agent_file_delete

Delete a markdown file from the current agent's data directory. Protected built-in files (`AGENTS.md`, `SYSTEM.md`, `MEMORY.md`, `RULES.md`) cannot be deleted.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `file` | string | yes | Path relative to the agent's data directory |

**Returns:** Text confirmation.

---

## file_read

Read a file within the current agent's filesystem allowlist. Returns UTF-8 text when possible, or base64 for binary files.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `path` | string | yes | Absolute or relative file path |

**Returns:** JSON object.

```json
{
  "path": "/home/user/projects/app/README.md",
  "content": "# My App\n...",
  "encoding": "utf-8"
}
```

**Constraint:** The path must be within the agent's configured `allowed_paths`. Requires `full` preset or explicit allowlist entry.

---

## file_write

Create or replace a file within the current agent's filesystem allowlist. Creates parent directories as needed.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `path` | string | yes | File path |
| `content` | string | yes | File content |
| `encoding` | string | | `"utf-8"` (default) or `"base64"` |

**Returns:** Text confirmation.

---

## file_append

Append data to a file. Creates the file if it does not exist.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `path` | string | yes | File path |
| `content` | string | yes | Content to append |
| `encoding` | string | | `"utf-8"` (default) or `"base64"` |

**Returns:** Text confirmation.

---

## file_truncate

Truncate or extend a file to a specific byte size.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `path` | string | yes | File path |
| `size` | int | yes | Target size in bytes (must be ≥ 0) |

**Returns:** Text confirmation with the resulting byte count.

---

## file_delete

Delete a file within the current agent's filesystem allowlist.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `path` | string | yes | File path |

**Returns:** Text confirmation.

---

## file_copy

Copy a file. Both paths must be within the agent's allowlist. Creates parent directories for the destination as needed.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `source` | string | yes | Source file path |
| `destination` | string | yes | Destination file path |

**Returns:** Text confirmation with both paths.

---

## file_move

Move or rename a file. Both paths must be within the agent's allowlist.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `source` | string | yes | Source file path |
| `destination` | string | yes | Destination file path |

**Returns:** Text confirmation with both paths.

---

## exec

Execute a host OS command. Only available when the agent's `permissions.exec.allowed_commands` is configured and the command matches an allow rule.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `command` | string | yes | Command string to execute |
| `cwd` | string | | Working directory (defaults to the agent's data directory) |

**Returns:** JSON execution result.

```json
{
  "command": "go test ./...",
  "cwd": "/home/user/projects/app",
  "stdout": "ok  github.com/...",
  "stderr": "",
  "exit_code": 0,
  "shell_interpolate": false
}
```

**Constraint:** The command must match an `allowed_commands` pattern. Shell interpolation is disabled unless `exec.shell_interpolate: true` is set in config.
