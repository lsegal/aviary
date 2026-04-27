---
name: himalaya
description: "Manage email accounts and mailboxes through the local Himalaya CLI. Use for reading mail, searching folders, flagging messages, moving or deleting messages, downloading attachments, composing or sending messages/templates, and diagnosing Himalaya email account configuration once the skill runtime is enabled."
---

Use this skill to perform email operations through the local `himalaya` binary.

## Read-Only Commands

Inspect mailbox state before making changes:

- `himalaya account list` — show configured accounts
- `himalaya account doctor` — diagnose account configuration
- `himalaya folder list` — list mailbox folders
- `himalaya envelope list --folder INBOX` — list messages in a folder
- `himalaya envelope list --folder INBOX --query "subject:invoice"` — search by query
- `himalaya envelope thread <envelope-id>` — show a message thread
- `himalaya message read <envelope-id>` — read a message body
- `himalaya message export <envelope-id>` — export raw message (EML)

## State-Changing Commands

Treat these as explicit user-requested actions:

- **Flags**: `himalaya flag add <id> seen`, `flag set`, `flag remove`
- **Messages**: `himalaya message copy <id> --to Archive`, `message move`, `message delete`
- **Folders**: `himalaya folder add <name>`, `folder delete`, `folder purge`, `folder expunge`
- **Send**: `himalaya message send --from personal --to user@example.com --subject "Re: meeting"`, `template send`, `template save`

## Workflow

1. List envelopes: `himalaya envelope list --folder INBOX`
2. Read target message: `himalaya message read <envelope-id>`
3. Act on it (flag, move, reply, delete) only when the user confirms

Confirm before destructive operations (`delete`, `purge`, `expunge`).

## Rules

- Preserve envelope IDs, folder names, and account names exactly as returned
- Respect the configured `allowed_commands` list
- Default to INBOX unless another folder is specified
- Use `--output json` for machine-readable output (appended automatically)
