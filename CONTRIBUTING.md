# Contributing to Aviary

## Prerequisites

- **Go** 1.22+
- **Node.js** 20+ with **pnpm** 8+

## Development Setup

Install web dependencies:

```shell
pnpm install
```

Start the development server (runs Vite + Go backend concurrently):

```shell
pnpm dev
```

This starts:
- The Vite dev server at **http://localhost:5173** (hot-reload, no HTTPS)
- The Aviary Go backend at **https://localhost:16677** (auto-restarts on Go file changes via `wgo`)

Open http://localhost:5173 in your browser. The Vite dev server proxies MCP and API requests to the Go backend.

> **First run:** The backend generates an auth token on first start. Check the terminal output for `Your access token: aviary_tok_...` and paste it into the login screen.

## Project Structure

```
cmd/aviary/          CLI entrypoint (Cobra)
internal/
  agent/             Agent manager and runner
  browser/           Browser automation (chromedp)
  channels/          Slack, Discord, Signal integrations
  config/            Config loading and file watching
  llm/               LLM provider adapters (Anthropic, OpenAI, Gemini, Stdio)
  mcp/               MCP server and tool dispatch
  memory/            Agent memory storage and search
  scheduler/         Cron runner, file watcher, job queue
  server/            HTTPS server, TLS, auth, web embed
web/
  src/               Vue 3 frontend (Pinia, Vue Router, Tailwind)
  e2e/               Playwright end-to-end tests
```

## Running Tests

```shell
pnpm test:go          # Go unit tests
pnpm test:e2e         # Playwright e2e tests (reuses running dev server)
pnpm test:e2e:ui      # Playwright with interactive UI
pnpm test             # lint + Go tests + e2e (full CI suite)
```

## Linting

```shell
pnpm lint             # Go (golangci-lint) + web (Biome)
pnpm lint:fix         # Auto-fix web lint/format issues
```

Run `pnpm lint` before submitting a PR — CI enforces it.

## Building a Release Binary

```shell
pnpm build            # Builds web assets and compiles the Go binary
```

Web assets are built to `web/dist/`, copied into `internal/server/webdist/`, and embedded in the Go binary via `go:embed`. The `webdist/` directory is gitignored.

## MCP Connection

Aviary exposes an MCP endpoint you can connect to directly for testing:

```
Endpoint: https://localhost:16677/mcp
Token:    cat ~/.config/aviary/token
Config:   ~/.config/aviary/aviary.yaml
```

The server is usually already running during development (`pnpm dev` starts it). You can restart it by stopping `pnpm dev` and running `go run ./cmd/aviary start`.

## Making Changes

- **Go changes:** `pnpm test:go` after every change; `pnpm lint:go` to check style
- **Web changes:** `pnpm lint:fix` to auto-format; `pnpm test:e2e` to verify UI behavior
- **Config schema changes:** update `internal/config/config.go` and any relevant stores in `web/src/stores/`
