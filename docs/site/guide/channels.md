# Channels

Channels are a core part of Aviary. They connect an agent to the communication systems where conversations already happen, so the same agent can answer in the control panel, over MCP, or inside a team chat.

Currently supported channel systems are:

- Slack
- Discord
- Signal

You can configure channels either in `aviary.yaml` or in the control panel at **Settings -> Agents -> Channels**.

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

- `id` is Aviary's configured integration name for that channel connection, not the remote platform's workspace or channel ID.
- `disabled_tools` can only restrict what the agent already has permission to use.
- `allow_from` controls which senders and group contexts are allowed to reach the agent.
- Channel-level `model` and `fallbacks` override the agent defaults for messages arriving through that channel.
- `show_typing` currently applies to Signal only. Slack and Discord do not support it.

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
- Event Subscriptions enabled, including both channel/direct message events and the `app_mention` event
- The app installed to the workspace
- The bot invited to any channels you want it to read or answer in

If the bot should respond when you type something like `@Aviary hi` in a channel, Slack must be configured to deliver `app_mention` events to the app.

If you want the bot to work in a channel like `#alerts`, invite it there in Slack the same way you would invite a teammate.

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
- Slack apps connected through Events API and Socket Mode cannot emit typing indicators, including in DMs. `show_typing` is not supported on Slack.
- `users:read` is required if you want Aviary to resolve Slack user names and support name-based routing instead of raw user IDs only.
- In the control panel, **Settings -> Agents -> Channels -> Slack** includes a **Browse Channels** action that validates the bot token and lists channels visible to the app.

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
4. Invite the bot to your server with permission to view channels, read message history, and send messages.
5. Turn on Developer Mode in Discord so you can copy user IDs, server IDs, and channel IDs.

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

Scheduled tasks and explicit session delivery can send output to configured channels. Channel targets use the form `<channel-type>:<configured-channel-id>:<delivery-id>`.

Examples:

- Slack: `slack:workspace-bot:#team-updates`
- Discord: `discord:ops-guild:234567890123456789`
- Signal: `signal:+15551234567:<group-or-recipient-id>`

For Slack, the final segment can be a human-friendly channel name like `#team-updates` or a raw Slack channel ID.
