# Channels

Channels are a core part of Aviary. They connect an agent to the communication systems where conversations already happen, so the same agent can answer in the control panel, over MCP, or inside a team chat.

Currently supported channel systems are:

- Slack
- Discord
- Signal

You can configure channels either in `aviary.yaml` or in the control panel's **Channels** sidebar section, below **Code Loop** under each agent.

## The Channels Section

Each agent's **Channels** section lists its channels:

- **+** on the **Channels** row adds a channel: give it a name and a type. It starts disabled, its settings open next, and its own chat session is created right away.
- Clicking a channel opens its conversation in the normal chat UI: the channel's own session first, plus any conversations the channel received from other chats. The gear in the header opens the channel's settings, the only place a channel is edited.
- A channel's **…** menu offers **Edit** (its settings), **Rename** and **Remove**. Renaming keeps its session and history, and tasks and reports that deliver to it follow the new name. Removing asks whether to **archive** its sessions, keeping them with their history as regular chats under **Chats**, or **delete** them.

Channel sessions live under their channel, not under **Chats**.

With no channel picked, the **Channels** page shows an overview:

- **Summary**: how many channels the agent has and how many are enabled, the messages received in the last 24 hours, the workflows and how many are running. **Add channel** and **Add workflow** sit in the header. **Add workflow** asks which channel the workflow watches, then opens that channel's Workflows tab.
- **Channels**: a card per channel with its type, whether it's enabled, its last activity, its messages in the last 24 hours and its workflow count. From a card you can open the chat, open its settings, or enable and disable it.
- **Workflows**: every workflow of the agent as a card, with its active or paused state, its last run's result and the running animation.
- **Recent messages**: each channel's latest messages, under a heading that opens that channel's chat. Each row shows the sender's name, how long ago it was sent, its attachments and the workflow that handled it. A long message fits one line: hover it to read it all, or click it to expand it. Search filters the messages across channels.

Senders are named by the latest name the channel has seen for them, so a message that only recorded a phone number, such as a Signal contact's first message before their profile name arrived, still shows their name. Phone numbers and IDs are never shown; a sender the channel only knows by ID is labelled **Unknown sender**.

For the exact `agents[].channels` schema, see the [Configuration Reference](/reference/config#agents-channels).

## Channel Basics

Multiple channels can be attached to one agent. Each channel can override the agent's model, disable additional tools, and apply sender-specific rules.

```yaml
agents:
  - name: lobby
    model: anthropic/claude-sonnet-4-6
    channels:
      - type: slack
        id: workspace-bot
        url: xapp-your-app-level-token
        token: xoxb-your-bot-token
        disabled_tools:
          - exec
        allow_from:
          - from: "*"
            allowed_groups: "#alerts"
            respond_to_mentions: true
```

Shared channel behavior:

- `id` is Aviary's configured integration name for that channel connection, not the remote platform's workspace or channel ID. The channel's own session is keyed by its type and `id`.
- `name` is the channel's display name, and the name delivery targets pick it by. It defaults to the `id`.
- `deliver_to` is the conversation the channel's own session sends to: a Slack or Discord channel ID, or a Signal number or group ID. Without it, messages delivered to the channel stay in Aviary.
- `disabled_tools` can only restrict what the agent already has permission to use.
- `allow_from` controls which senders and group contexts are allowed to reach the agent.
- Channel-level `model` and `fallbacks` override the agent defaults for messages arriving through that channel.
- `show_typing` applies to Signal typing notifications and the Slack "is working" agent session indicator. Discord does not support it.

### Attachments

Slack, Discord, and Signal handle files sent with a message the same way. Every attachment is saved under Aviary's data directory in `media/incoming/<channel type>/`, up to 50 MB each.

- Images up to 8 MB go to the model as images.
- Every other file appears in the message as a placeholder with its name, type, size, and saved path, such as `[file attached: log.json (application/json, 1.2 MB) saved at /path/to/log.json]`. The agent can open it there with its file tools.
- Text files (`text/*`, JSON, XML, YAML, CSV, logs) are also given to the model inline, up to 32 KB. Longer files are truncated with a pointer to the saved path.
- PDFs go to the model as documents on providers that read them natively: Anthropic, Gemini, and OpenAI (`openai` and `openai-codex`) models that take file input. On OpenAI-compatible endpoints such as Ollama and vLLM, older OpenAI models, and CLI agents, the agent gets the saved path.
- Session history keeps the placeholder, not the file's contents.
- Voice notes and other audio are transcribed with a local whisper.cpp engine, and the transcript is added before the placeholder as `[voice message 0:12, transcribed]: …`. See [Voice Messages](./voice.md).

## Workflows

A workflow is a channel automation that works like a [scheduled task](./scheduled-tasks.md), but runs when a message arrives on the channel instead of on a schedule. For example, a workflow can triage every message containing "crash" in a Slack channel and reply in the same conversation.

Add, edit, pause, resume and delete a channel's workflows in its settings, on the **Workflows** tab. The form is the task form, with a trigger in place of the schedule:

- **Source entry** is the conversation to watch: a Slack or Discord channel, or a Signal contact or group. Empty watches every conversation on the channel.
- **Text match** is a case-insensitive wildcard over the whole message text: `*` matches anything (including line breaks) and `?` one character. `*crash*` matches every message containing "crash". Empty matches every message, like `*`.
- **Would match** tests the current text match against the entry's recent messages as you type.
- **Model override**, **Send Via** and **While a run is active** work as they do for tasks. Send Via can be **silent**, **reply to the source conversation**, a session, or another channel.
- The prompt, its live analysis, **Try to precompile** and the model override are the same section the task editors use. As you type, the prompt is analyzed (debounced, and a newer edit cancels the analysis in flight) and the form shows whether it can be precompiled, with the reason when it can't, and the estimated tokens and cost of each run. The estimate includes the triggering message and the messages before it that each run's prompt carries, so it is higher than for the same scheduled task. When the workflow will be precompiled, the estimate is 0 tokens per run, with the model's cost beside it for comparison.

### Precompiled workflows

A prompt workflow can be precompiled like a [project task](./scheduled-tasks.md#precompiled-project-tasks). **Try to precompile** follows `scheduler.precompute_tasks` for a new workflow (on by default), and `task_schedule` and `task_update` take `precompile`. Saving compiles the prompt in the background when it passes the determinism gate. The workflow keeps its prompt, and each matching message then:

1. Runs the compiled script, which reads the message from the Lua global `trigger` (`trigger.message.text`, `.sender`, `trigger.before`, `trigger.channel`, `trigger.entry`). What the script prints is the workflow's reply.
2. Falls back to a normal model run on the prompt if the script or the compile fails. The run shows **Succeeded via fallback**, the script's error is in the run's logs, and the workflow's card says its last run fell back to the model.

A script that fails on its first run is set aside, and one that fails 3 runs in a row turns precompile off, exactly as for project tasks; the workflow's page says why. Workflow cards, the Workflows tab and the workflow's page show a badge with the compiled state (**Compiled**, **Compiling…**, **Not precompiled** or **Runs on model**), and the page also counts compile attempts, fallback runs and script failures.

When a message matches one or more active workflows, **the workflows replace the agent's own reply**: every matching workflow runs, in name order, and the agent does not answer the message itself. Whether anything is said back is up to each workflow's Send Via. The message is still recorded in the channel's conversation, marked **Handled by workflow** with a link to each run. The agent replies as usual only when no active workflow matches. Paused workflows never match, so a message that matches only paused workflows gets the normal reply. A workflow with an empty text match takes over every message on its entry, and the form says so.

Workflows only see messages the channel accepts through its `allow_from` rules, and never run on the agent's own messages, so a workflow cannot loop on its replies.

Each run goes through the same job queue as scheduled tasks, with the same run history, logs, statuses and overlap handling. A prompt workflow's run gets the triggering message, and the few messages before it, below its prompt. Each run records its **trigger context**: the channel and entry, the sender's display name (never a raw ID or phone number), the message and its attachments, timestamps, and the messages just before and after it. Usage records attribute each run's tokens to the workflow and its channel.

In the channel's chat, its workflows show as cards above the conversation, active ones first. **Hide paused** filters out paused workflows, and the strip collapses to icons; both choices are remembered. While a workflow is triggered (a run queued) or running, a colored segment travels clockwise around its card, amber while triggered and blue while running. Under `prefers-reduced-motion` the card gets a static colored border instead. Clicking a card opens the workflow's page: live logs for the current run, previous runs with their results, each run's trigger context, and **Run now**, **Pause** and settings.

In YAML or a task file, a workflow is a task with `trigger: message`:

```yaml
agents:
  - name: lobby
    tasks:
      - name: triage-crashes
        trigger: message
        channel: Team Slack   # the agent's channel, by name or ID
        entry: C0123456789    # optional: one conversation on it
        match: "*crash*"      # optional: empty matches every message
        target: reply         # silent (empty), reply, session:<name>, or channel:<name>
        precompile: true      # optional: run a compiled script, falling back to the model
        prompt: Triage this crash report and reply with next steps.
```

Renaming a channel moves its workflows along, and removing a channel deletes them.

## Slack

Slack setup depends on five pieces:

- A Slack app is the container for the integration. It is where you configure permissions, events, and tokens.
- A bot is the automated Slack user created by that app. This is the thing that appears in channels and can reply to messages.
- A bot token is the secret Aviary uses to act as that bot. It starts with `xoxb-`.
- An app-level token is the secret Aviary uses to keep a live connection to Slack itself. It starts with `xapp-`.
- Socket Mode is Slack's name for that live connection. Instead of Slack calling a public web server on the internet, Aviary opens an outbound connection to Slack and listens there. That is why Aviary can work without you setting up a public webhook URL.

For Aviary, Slack uses two values:

- `token`: the Slack Bot token (`xoxb-...`) used for posting messages and reading channel context
- `url`: the Slack App-Level token (`xapp-...`) used for Socket Mode

The channel `id` is Aviary's own name for this Slack connection, not a Slack workspace ID or Slack channel ID. Use a stable name such as `workspace-bot`, `alerts-bot`, or `main-workspace`.

### Before You Start

You usually need to be a workspace admin, app manager, or someone allowed to install apps in the Slack workspace.

### Get The Slack Tokens

1. Go to [Slack app management](https://api.slack.com/apps).
2. Click **Create New App**.
3. Choose **From scratch**.
4. Give the app a name like `Aviary` and choose the Slack workspace where you want the bot to live.
5. Open **OAuth & Permissions**.
6. Under **Bot Token Scopes**, add the permissions Aviary needs. A practical starting set is `chat:write`, `channels:history`, `groups:history`, `channels:read`, `groups:read`, `users:read`, and `app_mentions:read`.
7. Still on **OAuth & Permissions**, click **Install to Workspace** or **Reinstall to Workspace**.
8. After installation, copy the **Bot User OAuth Token**. This is the value that starts with `xoxb-`. Put that into Aviary's `token` field.
9. Open **Socket Mode** in the Slack app settings and turn on **Enable Socket Mode**.
10. If Slack asks for an app-level token while enabling Socket Mode, create one. If not, open **Basic Information**, scroll to **App-Level Tokens**, and click **Generate Token and Scopes**.
11. Name the token something recognizable like `aviary-socket`.
12. Grant it the `connections:write` scope.
13. Copy the generated token that starts with `xapp-`. Put that into Aviary's `url` field.

At that point you have the two Slack secrets Aviary needs.

### Finish The Slack Setup

Your Slack app should also have:

- Socket Mode enabled
- Event Subscriptions enabled, including channel and direct message events, the `app_mention` event, and the `agent_session_stopped` event
- The app installed to the workspace
- The bot invited to any channels you want it to read or answer in

If the bot should respond when you type something like `@Aviary hi` in a channel, Slack must be configured to deliver `app_mention` events to the app.

If you want the bot to work in a channel like `#alerts`, invite it there in Slack the same way you would invite a teammate.

### Enable The Stop Control

While Aviary works on a reply in a Slack thread, it marks that thread's [agent session](https://docs.slack.dev/ai/agent-sessions/) as processing. Slack then shows "is working" with a **Stop** control. Pressing **Stop** cancels the run in that thread only. Runs in other threads, channels, and DMs keep going. Aviary keeps any partial reply, adds "Stopped." to it, and clears the working status. The next message in the thread starts a normal new run.

To turn this on:

1. In the Slack app settings, open the **Agent** tab and switch the app from the assistant view to the agent view (`agent_view`). Slack only offers Stop for agent sessions. This switch cannot be undone, and an existing app may need its manifest re-applied and admin re-approval.
2. Under **Event Subscriptions** → **Subscribe to bot events**, add `agent_session_stopped`.
3. Make sure the bot has the `chat:write` scope. Both `agents.sessions.setStatus` and `agent_session_stopped` use it.
4. Reinstall the app to the workspace if Slack asks you to.

Without the `agent_session_stopped` subscription, Slack still shows the working indicator, but hovering it says "Stop isn't available for this agent". Aviary logs Slack's `missing_agent_session_stopped_event_subscription` warning to the Slack channel log when this happens.

### What Goes Into Aviary

- `token`: your `xoxb-...` Bot User OAuth Token
- `url`: your `xapp-...` app-level token for Socket Mode
- `id`: any stable name you want Aviary to use for this Slack connection

Example:

```yaml
agents:
  - name: lobby
    model: anthropic/claude-sonnet-4-6
    channels:
      - type: slack
        id: workspace-bot
        url: xapp-your-app-level-token
        token: xoxb-your-bot-token
        allow_from:
          - from: "*"
            allowed_groups: "#alerts"
            respond_to_mentions: true
```

Slack-specific notes:

- `allow_from[].from` accepts raw Slack user IDs or human-friendly names such as `alice` or `@alice`.
- `allow_from[].allowed_groups` accepts raw Slack channel IDs or human-friendly names such as `alerts` or `#alerts`.
- Slack apps connected through Events API and Socket Mode cannot emit classic typing notifications. When `show_typing` is enabled, Aviary marks the thread's Slack agent session as processing instead, which shows "is working" with a Stop control. See [Enable The Stop Control](#enable-the-stop-control).
- `users:read` is required if you want Aviary to resolve Slack user names and support name-based routing instead of raw user IDs only.
- In the control panel, a Slack channel's settings include a **Browse Channels** action that validates the bot token and lists channels visible to the app.

### Common Confusions

- If you only have an `xapp-...` token, you are missing the bot token. Aviary needs both.
- If you only have an `xoxb-...` token, Aviary can identify the bot but cannot hold the live Slack connection. You still need Socket Mode and the `xapp-...` token.
- If the bot is installed but does not answer in a channel, it is often because the bot was never invited to that specific Slack channel.

## Discord

Discord uses a single bot token from the [Discord Developer Portal](https://discord.com/developers/applications).

To make Discord work end to end:

1. Create a new Discord application and add a bot user.
2. Copy the bot token and store it directly in `token` or via an auth reference.
3. In the bot settings, enable the **Message Content Intent** so Aviary can read guild-channel message text.
4. Invite the bot to your server:
  - Open **OAuth2 > URL Generator** in the Discord Developer Portal.
  - Under **Scopes**, enable `bot`.
  - Under **Bot Permissions**, enable `View Channels`, `Read Message History`, and `Send Messages`.
  - Copy the generated URL, open it, choose the target server, and authorize the bot.
  - If you prefer to build the URL yourself, use `https://discord.com/oauth2/authorize?client_id=<application-client-id>&scope=bot&permissions=68608`.
5. Turn on Developer Mode in Discord so you can copy user IDs, server IDs, and channel IDs.

To complete the invite successfully, your Discord user must be allowed to add bots to that server, which usually means having **Manage Server** or equivalent admin rights.

After the bot is installed, make sure it can access the specific channels you want Aviary to read or answer in. On private or restricted channels, you may need to open the channel's permissions and explicitly grant the bot `View Channel`, `Read Message History`, and `Send Messages` there too.

Example:

```yaml
agents:
  - name: discord-lobby
    model: anthropic/claude-sonnet-4-6
    verbose: true
    channels:
      - type: discord
        token: auth:discord:default
        id: ops-guild
        disabled_tools:
          - exec
        allow_from:
          - from: "*"
            allowed_groups: "234567890123456789"
            respond_to_mentions: true
```

Discord-specific notes:

- `token` is the bot token.
- `id` is Aviary's configured integration name for that Discord connection, not a Discord channel ID.
- `from` uses a Discord user ID.
- `allowed_groups` is a comma-separated list of Discord channel IDs, or `*` to allow any matched guild channel.
- Direct messages do not need `allowed_groups`; server channels do.

## Signal

Aviary connects to Signal via [signal-cli](https://github.com/AsamK/signal-cli). Install `signal-cli` and register or link your account before configuring this channel.

```bash
# Register a new number with Signal
signal-cli -a +15551234567 register
signal-cli -a +15551234567 verify <code>

# Or link to an existing Signal account as a secondary device
signal-cli link -n "Aviary"
```

Once registered, add the channel using the phone number as the `id`:

```yaml
agents:
  - name: assistant
    channels:
      - type: signal
        id: "+15551234567"
        allow_from:
          - from: "+19995550100"
```

Aviary launches and manages `signal-cli` automatically. The `signal-cli` binary must be on your `$PATH`.

If you are already running `signal-cli` as a daemon on a known TCP port, point Aviary at it with `url` to avoid a second process:

```yaml
      - type: signal
        id: "+15551234567"
        url: "127.0.0.1:7583"
```

Signal-specific notes:

- `from: "*"` matches any sender; a specific phone number matches only that sender.
- `allowed_groups` restricts to specific group IDs. Without it, only direct messages match.
- `mention_prefixes` filters group messages by text pattern.
- `exclude_prefixes` silently drops matching messages before any other rule applies.
- By default, group mention filtering applies only to group messages. Set `mention_prefix_group_only: false` if you also want direct messages to require a prefix match.

A plain string in `allow_from` is shorthand for `{ from: "<string>" }`:

```yaml
allow_from:
  - "+15551234567"
  - from: "U9876543210"
    allowed_groups: "*"
    respond_to_mentions: true
```

## Delivery Targets

Anything that sends to a channel picks it by name with `channel:<name>`: scheduled task delivery (`target`), project reports (`reports.targets`), and tasks an agent schedules from a channel conversation. The message lands in the channel's own session, which sends it on to the channel's `deliver_to` conversation.

```yaml
agents:
  - name: lobby
    channels:
      - type: slack
        id: workspace-bot
        name: Team Slack
        deliver_to: C0123456789
    tasks:
      - name: daily-summary
        schedule: "0 9 * * *"
        target: channel:Team Slack
        prompt: Summarize yesterday's alerts.
```

Older configs are migrated when they load: a `<channel-type>:<configured-id>:<delivery-id>` target, or a `session:` target naming one of a channel's sessions, becomes `channel:<name>`, and a channel with no `deliver_to` takes the target's conversation.
