# Aviary: the AI Agent Nest

<p align="center"><img src="web/public/logo.png" alt="Aviary logo" width="200" /></p>

Aviary is a full AI assistant platform. Connect your AI models to Slack, Signal, Discord, and more. Have conversations, set up scheduled tasks, and let your agents work for you — all managed from a CLI or a web-based control panel.

[Website](https://aviary.bot) | Docs: [Getting Started](https://aviary.bot/guide/getting-started) · [Configuration](https://aviary.bot/guide/configuration) · [CLI Reference](https://aviary.bot/reference/cli) · [MCP Tools](https://aviary.bot/reference/mcp/)

---

## Install

**macOS / Linux**

```shell
curl -fsSL https://aviary.bot/install.sh | sh
```

**Windows (PowerShell)**

```powershell
iwr https://aviary.bot/install.ps1 | iex
```

Both scripts download the latest release binary to `~/.config/aviary/bin/` and add it to your `PATH`.

**Go install**

```shell
go install github.com/lsegal/aviary/cmd/aviary@latest
```

**Binary release**

Download the latest release for your platform from the [Releases page](https://github.com/lsegal/aviary/releases), place the binary in your `$PATH`, and make it executable.

---

## Quick Start

```shell
# Start the server
aviary serve

# Set up a provider credential and create your first agent
aviary configure

# Chat with an agent
aviary agent run assistant "Hello!"
```

Open the web control panel at `https://localhost:16677` and log in with the token from `~/.config/aviary/token`.

For a full walkthrough see the [Getting Started guide](https://aviary.bot/guide/getting-started).

---

## Documentation

| | |
|---|---|
| [Getting Started](https://aviary.bot/guide/getting-started) | Install, first agent, first chat |
| [Configuration](https://aviary.bot/guide/configuration) | `aviary.yaml` walkthrough — providers, agents, channels, tasks |
| [Scheduled Tasks](https://aviary.bot/guide/scheduled-tasks) | Cron and file-watch tasks |
| [Control Panel](https://aviary.bot/guide/control-panel) | Web UI overview |
| [CLI Reference](https://aviary.bot/reference/cli) | All commands and flags |
| [Config Reference](https://aviary.bot/reference/config) | Full `aviary.yaml` schema |
| [MCP Tools](https://aviary.bot/reference/mcp/) | All available MCP tools |

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).
