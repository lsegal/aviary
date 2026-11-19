# Files and Notes Tools

These tools provide agents with access to the filesystem and their own workspace directory.

**File tools** (`file_*`) operate on the host filesystem, constrained to paths permitted by the agent's `permissions.filesystem.allowed_paths` rules. They require either the `full` permissions preset or explicit allowlist inclusion.

**Agent file tools** (`agent_file_*`) provide full read/write/delete access to the agent's own data directory. They operate on the calling agent's session context; an explicit `agent` argument is also accepted for callers without session context, such as the web UI. Available under the `standard` preset.

---

## agent_file_list

List all files in the current agent's data directory, including subdirectories and built-in files such as `AGENTS.md`, `RULES.md`, and `MEMORY.md`.

**Arguments:** none when called from an agent session; accepts optional `agent` for callers without session context.

**Returns:** JSON array of relative file paths.

---

## agent_file_read

Read a file from the current agent's data directory. Use `agent_file_list` first when you need extra context and are unsure which file is relevant.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `file` | string | yes | Path relative to the agent's data directory (e.g. `"MEMORY.md"`, `"notes/foo.json"`) |

**Returns:** Text content of the file.

---

## agent_file_write

Create or replace a file in the current agent's data directory. Supports both root-level files (e.g. `MEMORY.md`) and subdirectories (e.g. `notes/summary.json`). This is the primary way for agents to persist information.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `file` | string | yes | Path relative to the agent's data directory |
| `content` | string | yes | Text content to write |

**Returns:** Text confirmation.

---

## agent_file_delete

Delete a file from the current agent's data directory. Protected built-in files (`AGENTS.md`, `SYSTEM.md`, `MEMORY.md`, `RULES.md`) cannot be deleted.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `file` | string | yes | Path relative to the agent's data directory |

**Returns:** Text confirmation.

---

## file_read

Read a file within the current agent's filesystem allowlist. A file up to 256 KB is returned whole, as UTF-8 text or base64 for binary files. Pass `offset` and `limit` to read a range of lines; ranged reads, and whole reads of larger files, return numbered lines and a note saying how to read the rest.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `path` | string | yes | Absolute or relative file path |
| `offset` | int | | First line to read, 1-based |
| `limit` | int | | Number of lines to read (at most 2000) |

**Returns:** JSON object. A whole read:

```json
{
  "path": "/home/user/projects/app/README.md",
  "content": "# My App\n...",
  "encoding": "utf-8"
}
```

A ranged read prefixes each line with its number and a tab:

```json
{
  "path": "/home/user/projects/app/main.go",
  "content": "    10\tfunc main() {\n    11\t\trun()\n",
  "encoding": "utf-8",
  "start_line": 10,
  "end_line": 11,
  "total_lines": 3000,
  "truncated": true,
  "note": "showing lines 10-11 of 3000; call file_read with offset 12 to read more"
}
```

**Constraint:** The path must be within the agent's configured `allowed_paths`. Requires `full` preset or explicit allowlist entry.

---

## file_edit

Replace an exact string in a file without reading or rewriting the whole file. The edit fails when `old_string` is not found, or appears more than once without `replace_all`; add surrounding lines to make it unique. The file is written atomically, and keeps its line endings (CRLF files accept LF in `old_string` and `new_string`), byte order mark, and permissions.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `path` | string | yes | File path |
| `old_string` | string | yes | Exact text to replace |
| `new_string` | string | yes | Replacement text |
| `replace_all` | bool | | Replace every occurrence |

**Returns:** JSON object with a unified diff of the change.

```json
{
  "path": "/home/user/projects/app/main.go",
  "replacements": 1,
  "diff": "--- a/home/user/projects/app/main.go\n+++ b/home/user/projects/app/main.go\n@@ -9,3 +9,3 @@\n..."
}
```

---

## file_multi_edit

Apply several `file_edit` replacements to one file in order. Each edit sees the result of the previous one. Either every edit applies or the file is left unchanged, and the error names the edit that failed.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `path` | string | yes | File path |
| `edits` | array | yes | Objects with `old_string`, `new_string`, and optional `replace_all` |

**Returns:** JSON object like `file_edit`, with the total number of replacements.

---

## file_write

Create a file, or deliberately rewrite one whole. Creates parent directories as needed and writes atomically. During an agent run, an existing file must have been read with `file_read` in the same session, and not changed since, before `file_write` will overwrite it; use `file_edit` for changes.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `path` | string | yes | File path |
| `content` | string | yes | File content |
| `encoding` | string | | `"utf-8"` (default) or `"base64"` |

**Returns:** JSON object with `path`, `created`, `bytes`, and a unified `diff` for text files.

---

## file_glob

List files matching a glob pattern under a directory. `*` and `?` stay within one path segment, `**` spans directories, `{a,b}` matches either, and a pattern without a slash matches file names at any depth. `.gitignore` files are respected and `.git` is skipped.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `pattern` | string | yes | Glob pattern, such as `**/*.go` |
| `path` | string | | Directory to search (defaults to the working directory) |
| `include_ignored` | bool | | Include files matched by `.gitignore` |

**Returns:** JSON object with `root`, `files` (paths relative to `root`, at most 1000), and `truncated`.

---

## file_grep

Search file contents with a regular expression (Go RE2 syntax). `.gitignore` files are respected, and binary files are skipped.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `pattern` | string | yes | Regular expression |
| `path` | string | | File or directory to search (defaults to the working directory) |
| `glob` | string | | Only search files matching this glob, such as `*.ts` |
| `mode` | string | | `content` (default) returns matching lines, `files` returns file names, `count` returns match counts per file |
| `context` | int | | Lines of context before and after each match (`content` mode, at most 20) |
| `case_insensitive` | bool | | Ignore case |
| `include_ignored` | bool | | Include files matched by `.gitignore` |
| `limit` | int | | Maximum results (default and maximum 500) |

**Returns:** JSON object with `root`, `mode`, `truncated`, and `matches` (`file`, `line`, `text`, `before`, `after`), `files`, or `counts` (`file`, `count`).

**Constraint:** The search and file tools only touch paths within the agent's `allowed_paths`; files the allowlist denies are skipped, and a search rooted outside it is refused.

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
| `cwd` | string | | Working directory (defaults to the agent's data directory, or to the job's granted directory during a job) |

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
