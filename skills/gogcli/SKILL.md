---
name: gogcli
description: Access Google Workspace capabilities through gogcli. Use for Gmail, Calendar, Drive, Contacts, Tasks, and related Google account workflows once the skill runtime is enabled.
---

Use this skill to perform Google Workspace operations through the local `gog` binary.

Use `--help` to identify commands and flags for each command.

Prefer structured commands over vague requests.

Respect the configured `allowed_commands` list and do not attempt commands outside it.

When returning results, preserve machine-readable output where possible.
