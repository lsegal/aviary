# Getting Started

This guide walks you through installing Aviary, starting the server, and creating your first agent.

## Prerequisites

- **macOS or Linux** — native support. **Windows** — supported via WSL2 or natively with Go 1.22+.
- A model provider API key (Anthropic, OpenAI, Gemini, or GitHub Copilot) _or_ an account eligible for OAuth login.

## Install

**macOS / Linux (Homebrew)**

```bash
brew install lsegal/tap/aviary
```

**Go install**

```bash
go install github.com/lsegal/aviary/cmd/aviary@latest
```

**Binary release**

Download the latest release for your platform from the [Releases page](https://github.com/lsegal/aviary/releases), place the binary in your `$PATH`, and make it executable.

## Start the Server

```bash
aviary start
```

The server starts on `https://localhost:16677` by default. It generates a self-signed TLS certificate on first run and stores it in `~/.config/aviary/`. The port and TLS settings can be changed in `aviary.yaml` — see the [Configuration guide](/guide/configuration).

Verify the server is running:

```bash
aviary status
```

## Open the Control Panel

Navigate to `https://localhost:16677` in your browser. Accept the self-signed certificate warning (it is local-only and generated fresh per installation).

The login screen asks for an **access token**. The token is stored at:

```
~/.config/aviary/token
```

Copy the token from that file and paste it into the login field.

## Store a Provider Credential

Before creating an agent you need at least one provider credential. The easiest path is OAuth login for Anthropic (no API key needed):

1. In the control panel, go to **Settings → Providers**.
2. Click **Log in with Anthropic**.
3. Complete the sign-in flow in the browser tab that opens.
4. Return to the control panel — the Anthropic provider should show as connected.

For a raw API key instead, use the CLI:

```bash
aviary auth set ANTHROPIC_API_KEY sk-ant-...
```

Then reference it in `aviary.yaml`:

```yaml
models:
  providers:
    anthropic:
      auth: ANTHROPIC_API_KEY
```

## Create Your First Agent

If no agents are configured, the control panel shows a setup wizard on the Overview page. Click **Create Agent**, fill in a name and model, and save.

Or add the agent directly to `aviary.yaml`:

```yaml
models:
  providers:
    anthropic:
      auth: ANTHROPIC_API_KEY
  defaults:
    model: anthropic/claude-sonnet-4-6

agents:
  - name: assistant
    memory: private
```

The server picks up config changes automatically — no restart needed.

## Start a Chat Session

1. Open **Chat** in the control panel.
2. Select your agent from the agent tabs.
3. Type a message and press Enter.

The agent responds in real time. Sessions are persisted automatically; they appear in the session list the next time you open the chat.

## Connect via MCP

Any MCP-compatible client can connect directly to the Aviary server:

- **Endpoint:** `https://localhost:16677/mcp`
- **Auth:** Bearer token from `~/.config/aviary/token`

In Claude Code, add a remote MCP server pointing to that endpoint. Once connected, `agent_run` and the full tool catalog are available to the LLM.

## Next Steps

- [Configuration](/guide/configuration) — full `aviary.yaml` reference walkthrough
- [Scheduled Tasks](/guide/scheduled-tasks) — automate recurring agent work
- [MCP Tool Reference](/reference/mcp/) — all available tools
