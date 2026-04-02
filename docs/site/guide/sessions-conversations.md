# Sessions & Conversations

This guide explains how Aviary handles ongoing conversations, why sessions behave differently from many chat-first AI tools, and how memory fits into the picture.

If you want the raw MCP surface for session management, see the [Session Tools reference](/reference/mcp/sessions).

## The Mental Model

In Aviary, a **session** is the persisted conversation log for one agent and one conversation thread.

Sessions are where Aviary stores:

- user messages
- assistant replies
- tool messages and run history tied to that conversation
- delivery routing for channel-backed conversations

Sessions are **not** the same thing as long-term memory.

Think of it this way:

- **session history** is the transcript of what happened in one conversation
- **memory** is durable operator-authored or agent-authored context that can outlive any single conversation

If you delete a session, you remove that conversation thread. If you update memory, you are changing durable context the agent may use across future runs.

## How Sessions Work

Most users should not have to think about sessions directly.

In practice, Aviary creates and uses sessions as the conversation record behind a few different surfaces:

- the `main` session for web chat conversations
- channel-backed conversations such as Slack, Discord, or Signal
- scheduled tasks and other background work that need their own durable transcript

The important part is not "which session am I in?" so much as "what happened in this conversation?" Sessions are mainly the system's way of keeping who-said-what-when attached to the right thread.

This still matters because Aviary keeps conversation history scoped to the right thread. A restaurant reservation request from Signal, a calendar follow-up from a scheduled task, and a website research flow in the web UI should not all blend into one giant transcript.

That separation is the real benefit: each conversation keeps its own timeline, replies, and tool activity, so channel messages, web chats, and automated work do not trample each other's history.

## Memory vs Session History

This is the distinction that matters most when operating Aviary well.

### Session History

Session history is conversation-local:

- it records what happened in a specific thread
- it grows as the user and agent exchange messages
- it includes the immediate working context for that conversation

Session history is ideal for:

- short-term back-and-forth
- the timeline of a conversation
- tool activity tied to a specific interaction
- channel or task conversations that should stay attached to their own thread

### Memory

Memory is durable context outside any one thread:

- operator instructions
- persistent facts worth keeping
- stable team or project context
- agent-specific notes that should survive across sessions

Memory is ideal for:

- long-lived preferences
- project background
- operating rules
- facts you want available next week, not just in the current chat

In practice, think of sessions as the conversation log, not the place where you intentionally "store context." Use memory for durable context you want carried across future conversations.

## Unique Conversation Features

Many AI chat tools encourage a single linear conversation where each new prompt implicitly waits for the previous one to finish. Aviary is designed differently.

### 1. Sessions are parallel by design

In Aviary, each prompt starts a new request. Work is not treated as a single strictly sequential pipeline.

That means:

- you can trigger multiple runs in the same session
- you can have multiple sessions active for the same agent
- background work continues even if the UI focus changes

This makes Aviary feel closer to an operational assistant than a single blocking chat box. An agent might be checking a website, reviewing email, watching for a reply, or preparing a reservation request while other work is still in flight.

### 2. You can stop a conversation while work is in progress

Because work can happen in parallel, stopping work needs to be explicit and reliable.

That is why Aviary supports **stop** instructions in the UI and chat surfaces. When you stop a run, Aviary halts active work for that session instead of waiting for a long sequential exchange to unwind.

<ConversationStopDemo />

Under the hood this is wired to session-level cancellation through [`session_stop`](/reference/mcp/sessions#session_stop), but most operators only need to remember the higher-level behavior: if something is still running, you can stop it immediately.

This is one of the key differences from tools that assume a conversation is mostly just one message waiting on one response. In Aviary, a conversation can contain active work, overlapping requests, and resumable history.

### 3. Conversations are durable, but not monolithic

A lot of chat-first tools push users toward one ever-growing thread. Aviary does not require that.

Instead, you can:

- let web chat, channels, and task-driven work keep their own conversation history
- preserve each thread independently
- resume older activity without mixing it into whatever you are doing right now

This keeps conversation history cleaner and makes it easier to operate agents across everyday workflows like inbox triage, website research, reservation booking, reminders, and calendar follow-up.

## Operational Patterns

A few patterns tend to work well in practice:

- Let sessions stay mostly automatic unless you are explicitly managing chat threads.
- Treat sessions as conversation history, not as a manual context-storage tool.
- Use memory for stable instructions and facts, not for transient updates from one interaction.
- Let channels and scheduled tasks keep their own history instead of forcing everything into web chat.
- Stop runs when they are heading in the wrong direction; Aviary is built for that.

If you are coming from a more traditional chat tool, the biggest adjustment is this: sessions are part of Aviary's runtime record-keeping, not something you should usually treat as a hand-managed memory mechanism.

## Where To Manage Sessions

You can work with sessions from several places:

- **Chat** for everyday conversation flow, switching threads, and stopping active runs
- **Settings -> Sessions** for cross-agent session administration
- **MCP session tools** for programmatic control, listing, deletion, history reads, and explicit cancellation

For the exact MCP tool shapes, see the [Session Tools reference](/reference/mcp/sessions).
