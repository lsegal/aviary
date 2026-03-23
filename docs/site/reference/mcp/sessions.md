# Session Tools

Session tools manage the conversation history between agents and their callers. Each agent can have multiple named sessions; sessions persist messages and can be targeted to deliver output to a channel.

---

## Sessions and Identifiers

A **session** is an ordered log of messages between one agent and one caller. Each session has:

- **`id`** — a stable UUID assigned at creation; use this when calling tools that accept `session_id`
- **`name`** — a human-readable label (defaults to `"main"` for the first session)

The `"main"` session is created automatically on first use. Additional sessions can be created explicitly with `session_create`.

Message roles follow the standard LLM convention: `user`, `assistant`, and `tool`.

---

## session_list

List all sessions for an agent. Creates the `"main"` session if it does not exist.

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
    "is_processing": false
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

## session_remove

Permanently delete a session and all of its messages.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |
| `session_id` | string | yes | Session ID to delete |

**Returns:** Text confirmation.

**Side effects:** Removes the session file from disk. This operation is irreversible.

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

## session_set_target

Attach a channel delivery target to a session. Subsequent messages sent or generated in the session will be delivered to the specified channel.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `agent` | string | yes | Agent name |
| `session_id` | string | yes | Session ID |
| `channel_type` | string | yes | Channel type: `"slack"`, `"discord"`, or `"signal"` |
| `channel_id` | string | yes | Channel or conversation ID |
| `target` | string | yes | Delivery target string (platform-specific) |

**Returns:** Text confirmation including the delivery route.

**Side effects:** Persists the channel target in the session sidecar file.
