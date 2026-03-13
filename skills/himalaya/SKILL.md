---
name: himalaya
description: Manage email accounts and mailboxes through the local Himalaya CLI. Use for reading mail, searching folders, flagging messages, moving or deleting messages, downloading attachments, composing or sending messages/templates, and diagnosing Himalaya email account configuration once the skill runtime is enabled.
---

Use this skill to perform email operations through the local `himalaya` binary.

Prefer structured commands over vague requests.

Use read-only commands first when inspecting mailbox state:
- `account list`
- `account doctor`
- `folder list`
- `envelope list`
- `envelope thread`
- `message read`
- `message export`

Treat state-changing commands as explicit actions:
- `flag add`, `flag set`, `flag remove`
- `message copy`, `message move`, `message delete`
- `folder add`, `folder delete`, `folder purge`, `folder expunge`
- `message send`, `template send`, `template save`

Preserve envelope IDs, folder names, and account names exactly as returned by Himalaya.

Respect the configured `allowed_commands` list and do not attempt commands outside it.

Return machine-readable output when possible.

Always read from INBOX by default unless another mailbox is specified.
