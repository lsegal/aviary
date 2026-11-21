# Session Tools

Session tools manage the conversation history between agents and their callers. Each agent can have multiple named sessions; sessions persist messages, and a channel's sessions deliver output to that channel.

---

## Sessions and Identifiers

A **session** is an ordered log of messages between one agent and one caller. Each session has:

- **`id`** — a stable UUID assigned at creation; use this when calling tools that accept `session_id`
- **`name`** — a human-readable label (defaults to `"main"` for the first session)
- **`title`** — the display name, set by `session_rename` or generated from a new chat's first prompt; absent until then. An untitled chat is named by its next prompt
- **`last_message_at`** — when the last user or assistant message was sent; absent for an empty session
- **`deletable`** — whether `session_remove` may delete the session. The `"main"` session is permanent: it can only be emptied with `session_clear`

The `"main"` session is created automatically on first use. Additional sessions can be created explicitly with `session_create`.

Message roles follow the standard LLM convention: `user`, `assistant`, and `tool`.

---

## session_list

List all sessions for an agent, leaving out hidden sessions and the sessions of its channels (see [Channel Sessions](#channel-sessions)). Creates the `"main"` session if it does not exist.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |

**Returns:** JSON array of session objects.

```json
[
  {
    "id": "01HZ...",
    "name": "main",
    "agent_id": "assistant",
    "created_at": "2026-03-01T09:00:00Z",
    "updated_at": "2026-03-22T14:30:00Z",
    "last_message_at": "2026-03-22T14:30:05Z",
    "is_processing": false,
    "deletable": false
  }
]
```

---

## session_create

Create a new session for an agent.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |

**Returns:** JSON session object (same shape as `session_list` entries).

The first prompt sent to a chat created this way names it: Aviary asks the agent's model for a short title from that prompt and stores it as the session's `title`, falling back to the prompt's opening words. A chat that is still untitled after its first prompt (for example, one sent with only an image) is named by its next prompt.

---

## session_messages

Return persisted messages for a session. Supports pagination and reverse ordering for efficient recent-history reads.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `session_id` | string | yes* | Session ID |
| `agent` | string | | Agent name (required when `session_id` is ambiguous) |
| `id` | string | | Alias for `session_id` |
| `limit` | int | | Maximum number of messages to return |
| `skip` | int | | Number of messages to skip (for pagination) |
| `order` | string | | `"asc"` (oldest first, default) or `"desc"` (newest first) |

**Returns:** JSON array of message objects.

```json
[
  {
    "role": "user",
    "content": "Hello",
    "timestamp": "2026-03-22T14:30:00Z"
  },
  {
    "role": "assistant",
    "content": "Hi there!",
    "timestamp": "2026-03-22T14:30:05Z",
    "model": "anthropic/claude-sonnet-4-6",
    "response_id": "msg_01..."
  }
]
```

Use `order=desc` with `limit=20` to quickly recover recent context in resumed sessions or group chats.

---

## session_history

Alias for `session_messages` with the same arguments and return shape. Prefer `order=desc` and `limit=20` for recent-context recovery.

---

## session_stop

Stop all in-progress work for a specific session.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `session_id` | string | | Session ID |
| `agent` | string | | Agent name |
| `session` | string | | Session name (alternative to `session_id`) |

**Returns:** Text message with the count of runs stopped (e.g. `stopped 1 run(s) in session "01HZ..."`).

---

## session_turn

Get what a session's running turn has streamed so far. The control panel uses it to restore a chat's typing indicator, tool calls, and partial reply when it opens mid-turn. The `/api/ws` stream sends the same state as `session_turn` events while the turn runs.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |
| `session_id` | string | yes | Session ID |

**Returns:** JSON object with `agent_id`, `session_id`, `processing` (true while a turn runs), and `items`, in streamed order. Each item has one of `text` (partial reply text), `media_url`, or `tool` (`id`, `name`, `args`, `result`, `error`, `done`). Items are empty once the turn ends.

---

## session_remove

Permanently delete a session and all of its messages.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |
| `session_id` | string | yes | Session ID to delete |

**Returns:** Text confirmation.

**Side effects:** Removes the session file and its sidecar files from disk. This operation is irreversible.

**Errors:** Refuses to delete a permanent session such as `"main"` (`deletable: false` in `session_list`); clear it with `session_clear` instead.

---

## session_clear

Remove every message from a session, stopping any in-progress work first. The session keeps its `id`, `name`, `title`, and channel bindings, so task delivery targets and channels that point at it keep working. The model's stored conversation is dropped, so the next prompt starts fresh.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |
| `session_id` | string | yes | Session ID to clear |

**Returns:** Text confirmation.

---

## session_rename

Set a session's display title. The session's `name` and `id` do not change, so channels and the `"main"` session still resolve to it.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |
| `session_id` | string | yes | Session ID |
| `name` | string | yes | New display title |

**Returns:** Text confirmation.

---

## session_send

Send a plain-text assistant message to a session. If the session has a configured channel target, the message is also delivered there.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `content` | string | yes | Message text |
| `agent` | string | | Agent name |
| `session_id` | string | | Session ID (defaults to current session when called from within an agent run) |

**Returns:** Text confirmation.

**Side effects:** Appends the message to the session log. Delivers to any connected channel target.

---

## Channel Sessions

Each of an agent's channels owns its sessions: its own session, created with the channel and keyed by its type and ID, and the conversations it receives. They live under the channel, not in `session_list`.

### channel_sessions

List a channel's sessions: its own session first (where messages delivered to the channel by name land), then the conversations it received, most recent first.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |
| `channel` | string | yes | Channel name |

**Returns:** JSON array of sessions in `session_list`'s shape, each with `own` set on the channel's own session.

### channel_create

Add a channel to an agent and create its session. The channel starts disabled until its connection settings are filled in; its ID is derived from its name.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |
| `name` | string | yes | Channel name, unique within the agent |
| `type` | string | yes | `"slack"`, `"discord"`, or `"signal"` |

**Returns:** JSON object with the new `channel` and its `session_id`.

### channel_rename

Rename a channel. Its session and history stay, and task and report targets that pick it by name follow the new name.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |
| `channel` | string | yes | Current channel name |
| `new_name` | string | yes | New channel name |

### channel_remove

Remove a channel. Task and report targets that pick it by name are cleared.

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |
| `channel` | string | yes | Channel name |
| `sessions` | string | yes | `"archive"` keeps its sessions, with their history, as regular chats; `"delete"` removes them |
