# Getting Started

This guide walks you through installing Aviary, starting the server, and creating your first agent.

## Prerequisites

- **macOS or Linux** — native support. **Windows** — supported via WSL2 or natively with Go 1.22+.
- A model provider API key (Anthropic, OpenAI, Gemini, or GitHub Copilot) _or_ an account eligible for OAuth login.

## Install

### macOS / Linux

#### Homebrew

```bash
brew tap lsegal/aviary https://github.com/lsegal/aviary
brew install aviary
```

This tap works with Homebrew on macOS and Linux.

#### Manual (cURL)

```bash
curl -fsSL https://aviary.bot/install.sh | sh
```

The install script downloads the latest release binary to `~/.local/bin/` and adds it to your `PATH`.

### Windows

#### Scoop

```powershell
scoop bucket add aviary https://github.com/lsegal/aviary
scoop install aviary/aviary
```

Scoop installs Aviary into Scoop's managed package directory and shims `aviary.exe` onto your `PATH`.

#### Manual (PowerShell)

```powershell
iwr https://aviary.bot/install.ps1 | iex
```

The install script downloads the latest release binary to `~/.local/bin/` and adds it to your `PATH`.

### Docker

```bash
mkdir -p ~/.config/aviary
docker run --rm -it \
  -p 16677:16677 \
  -p 1455:1455 \
  -p 45289:45289 \
  -v ~/.config/aviary:/home/bot/.config/aviary \
  ghcr.io/lsegal/aviary:latest
```

The image runs `aviary serve` by default. The bind mount keeps your config, TLS certs, and login token on the host in `~/.config/aviary/`.

The Docker image also bundles a working Chrome/Chromium browser, so Aviary can run browser tasks in headless mode out of the box.

::: tip
`-p 1455:1455` is only needed for OpenAI Codex OAuth, and `-p 45289:45289` is only needed for Gemini OAuth. If you are using API keys or other providers, you can omit those extra port mappings.
:::

### Binary Release

Download the latest release for your platform from the [Releases page](https://github.com/lsegal/aviary/releases), place the binary in your `$PATH`, and make it executable.

### Go Install

```bash
go install github.com/lsegal/aviary/cmd/aviary@latest
```

## Start the Server

```bash
aviary serve
```

The server starts on `https://localhost:16677` by default. It generates a self-signed TLS certificate on first run and stores it in `~/.config/aviary/`. The port and TLS settings can be changed in `aviary.yaml` — see the [Configuration guide](/guide/configuration).

If you are using Docker, the `docker run` command above already starts the server. To run CLI commands against the same config directory, override the container command:

```bash
docker run --rm -it \
  -v ~/.config/aviary:/home/bot/.config/aviary \
  ghcr.io/lsegal/aviary:latest \
  aviary configure
```

Verify the server is running:

```bash
aviary service status
```

## Open the Control Panel

Navigate to `https://localhost:16677` in your browser. Accept the self-signed certificate warning (it is local-only and generated fresh per installation).

The login screen asks for an **access token**. The token is stored at:

```
~/.config/aviary/token
```

Copy the token from that file and paste it into the login field.

## Store a Provider Credential

Before creating an agent you need at least one provider credential.

**Option 1 — Interactive wizard (recommended)**

```bash
aviary configure providers
```

Select a provider, then choose between API key entry or OAuth login. The wizard stores the credential and updates `aviary.yaml` automatically.

**Option 2 — Control panel**

1. In the control panel, go to **Settings → Providers**.
2. Click **Log in with Anthropic** (or the provider of your choice).
3. Complete the sign-in flow in the browser tab that opens.
4. Return to the control panel — the provider should show as connected.

**Option 3 — CLI directly**

```bash
aviary auth set anthropic:default sk-ant-...
```

Then reference the credential in `aviary.yaml` using the `auth:<key>` format:

```yaml
models:
  providers:
    anthropic:
      auth: auth:anthropic:default
```

Credentials stored via OAuth use the name `<provider>:oauth`; those set manually use whatever name you give them (e.g. `anthropic:default`). The `auth:` prefix in the YAML value tells Aviary to look up the credential by name rather than treating the string as a literal key.

## Create Your First Agent

**Option 1 — Interactive wizard**

```bash
aviary configure agents
```

Choose **Add agent**, enter a name and model, and save. The wizard writes the agent to `aviary.yaml` immediately.

**Option 2 — Control panel**

If no agents are configured, the control panel shows a setup wizard on the Overview page. Click **Create Agent**, fill in a name and model, and save.

**Option 3 — Edit `aviary.yaml` directly**

```yaml
models:
  providers:
    anthropic:
      auth: auth:anthropic:default
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

If you want to talk to the agent over Slack, Discord, or Signal instead of the built-in chat view, skip ahead to the [Channels guide](/guide/channels).

## Connect via MCP

Any MCP-compatible client can connect directly to the Aviary server:

- **Endpoint:** `https://localhost:16677/mcp`
- **Auth:** Bearer token from `~/.config/aviary/token`

In Claude Code, add a remote MCP server pointing to that endpoint. Once connected, `agent_run` and the full tool catalog are available to the LLM.

## Next Steps

- [Configuration](/guide/configuration) — full `aviary.yaml` reference walkthrough
- [Channels](/guide/channels) — skip here for Slack, Discord, and Signal setup
- [Security & Permissions](/guide/security-permissions) — hardening guidance for agents, the server, and the host machine
- [Scheduled Tasks](/guide/scheduled-tasks) — automate recurring agent work
- [MCP Tool Reference](/reference/mcp/) — all available tools
