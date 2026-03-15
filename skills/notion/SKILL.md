---
name: notion
description: Manage Notion pages, databases, comments, and workspace search through the local notion-cli runtime. Use for Notion search, page inspection, page creation or editing, database queries and entry creation, comment workflows, and Notion CLI authentication once the skill runtime is enabled.
---

Use this skill to perform Notion operations through the local `notion-cli` binary.

Prefer structured commands over vague requests.

Use read-only commands first when inspecting workspace state:
- `search`
- `page list`
- `page view`
- `db list`
- `db query`
- `comment list`
- `auth status`
- `tools`

Treat state-changing commands as explicit actions:
- `page create`
- `page upload`
- `page sync`
- `page edit`
- `db create`
- `comment create`
- `auth login`
- `auth logout`

Use page URLs or IDs exactly as returned by Notion when possible.

Query a database before creating entries so property names and types are known.

Respect the configured `allowed_commands` list and do not attempt commands outside it.

Return machine-readable output when possible.
