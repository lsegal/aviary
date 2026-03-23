# Files and Notes Tools

These tools provide agents with access to the filesystem and their workspace note storage.

**File tools** (`file_*`) operate on the host filesystem, constrained to paths permitted by the agent's `permissions.filesystem.allowedPaths` rules. They require either the `full` permissions preset or explicit allowlist inclusion.

**Agent context tools** (`agent_file_*`, `agent_root_file_*`) provide read and write access to the agent's own workspace directory and are available under the `standard` preset.

**Note tools** write structured markdown into the agent's workspace without requiring filesystem permissions.

---

## note_write

Write a workspace note to `notes/<file>.md` using markdown content. This is the primary way for agents to persist information within their session without needing filesystem permissions.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `file` | string | yes | Descriptive filename (e.g. `"research-summary"`) — `.md` is appended automatically |
| `content` | string | yes | Markdown content to write |

**Returns:** Text confirmation with the saved file path.

**Side effects:** Creates or overwrites `~/.config/aviary/agents/<name>/notes/<file>.md`.

---

## agent_file_list

List markdown context files available under an agent's workspace directory. Excludes `RULES.md`, which is already injected into the system prompt automatically.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |

**Returns:** JSON array of file metadata objects (name, path, size, modified time).

---

## agent_file_read

Read a markdown context file from an agent's workspace directory. Use `agent_file_list` first when you need extra context and are unsure which file is relevant.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |
| `file` | string | yes | Filename relative to the agent's workspace directory |

**Returns:** Text content of the file.

---

## agent_root_file_list

List root-level markdown files in an agent's data directory, including built-in files such as `AGENTS.md`, `RULES.md`, and `MEMORY.md`.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |

**Returns:** JSON array of file metadata objects.

---

## agent_root_file_read

Read a root-level markdown file from an agent's data directory.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |
| `file` | string | yes | Filename (e.g. `"MEMORY.md"`, `"AGENTS.md"`) |

**Returns:** Text content of the file.

---

## agent_root_file_write

Create or replace a root-level markdown file in an agent's data directory. This is the preferred way to update `MEMORY.md` and other long-lived context files.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |
| `file` | string | yes | Filename |
| `content` | string | yes | Markdown content |

**Returns:** Text confirmation.

---

## agent_root_file_delete

Delete a root-level markdown file from an agent's data directory. Protected files (`AGENTS.md`, `SYSTEM.md`, `MEMORY.md`, `RULES.md`) cannot be deleted.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |
| `file` | string | yes | Filename to delete |

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

**Constraint:** The path must be within the agent's configured `allowedPaths`. Requires `full` preset or explicit allowlist entry.

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

Execute a host OS command. Only available when the agent's `permissions.exec.allowedCommands` is configured and the command matches an allow rule.

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

**Constraint:** The command must match an `allowedCommands` pattern. Shell interpolation is disabled unless `exec.shellInterpolate: true` is set in config.
