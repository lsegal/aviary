---
name: gogcli
description: "Access Google Workspace through gogcli. Use for sending and reading Gmail, creating and listing Calendar events, uploading and searching Drive files, managing Contacts and Tasks, and working with Sheets, Docs, Slides, Forms, Chat, Keep, and other Google services once the skill runtime is enabled."
---

Use this skill to perform Google Workspace operations through the local `gog` binary.

## Common Commands

- `gog gmail list` — list recent emails
- `gog gmail send --to user@example.com --subject "Hello"` — send an email
- `gog calendar list MyCalendar` — list events (calendar name is positional, not a flag)
- `gog calendar create MyCalendar --title "Standup" --start "2025-01-15T09:00:00"` — create event
- `gog drive list` — list files in Drive
- `gog drive upload ./report.pdf` — upload a file
- `gog contacts list` — list contacts
- `gog tasks list` — list tasks

Use `gog <service> --help` to discover subcommands and flags for any service.

## Available Services

gmail, calendar, drive, contacts, tasks, sheets, docs, slides, forms, chat, classroom, appscript, people, groups, admin, keep, time

## Rules

- Calendar name is always a **positional argument**, not a flag
- Respect the configured `allowed_commands` list
- Use `--json` for machine-readable output (appended automatically)
