# Aviary: the AI Agent Nest

<p align="center"><img src="web/public/logo.png" alt="Aviary logo" width="200" /></p>

<p align="center">
  <a href="https://github.com/lsegal/aviary/releases"><img src="https://img.shields.io/github/v/release/lsegal/aviary?display_name=tag&sort=semver" alt="Release" /></a>
  <a href="https://github.com/lsegal/aviary/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/lsegal/aviary/ci.yml?branch=main&label=tests" alt="Tests" /></a>
  <a href="https://github.com/lsegal/aviary/blob/main/LICENSE"><img src="https://img.shields.io/github/license/lsegal/aviary" alt="License" /></a>
</p>

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

Both scripts download the latest release binary to `~/.local/bin/` and add it to your `PATH`.

**Docker**

```shell
mkdir -p ~/.config/aviary
docker run --rm -it \
  -p 16677:16677 \
  -v ~/.config/aviary:/home/bot/.config/aviary \
  ghcr.io/lsegal/aviary:latest
```

The image runs `aviary serve` by default. With the bind mount above, Aviary stores its config, TLS certs, and login token in your host `~/.config/aviary/` directory.

**Binary release**

Download the latest release for your platform from the [Releases page](https://github.com/lsegal/aviary/releases), place the binary in your `$PATH`, and make it executable.

**Go install**

```shell
go install github.com/lsegal/aviary/cmd/aviary@latest
```

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

Using Docker, run one-off CLI commands against the same mounted config directory by overriding the container command:

```shell
docker run --rm -it \
  -v ~/.config/aviary:/home/bot/.config/aviary \
  ghcr.io/lsegal/aviary:latest \
  aviary configure
```

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
