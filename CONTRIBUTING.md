# Contributing to Aviary

## Prerequisites

- **Go** 1.26+
- **Node.js** 20+ with **pnpm** 8+

## Development Setup

If you want a prebuilt environment with Go, Node, pnpm, Chrome, `codex`, `claude`, `gogcli`, and `himalaya`, use one of the container-based quick starts below.

### Quick Start With Devcontainers

[Dev Containers](https://containers.dev/) let you open the repository inside a preconfigured Docker-based development environment directly from VS Code, so you do not need to install the full toolchain on your host machine first.

Create the local Aviary config file used by the container:

```shell
cat > .devcontainer/aviary/aviary.yaml <<'EOF'
models:
  defaults:
    model: openai-codex/gpt-5.2
EOF
```

Open the repository in VS Code and run **Dev Containers: Reopen in Container**.

This starts:

- The Vite dev server at **https://localhost:5173**
- The docs dev server at **https://localhost:5174**
- The Aviary backend at **https://localhost:16677**

The container persists Aviary runtime state under `.devcontainer/aviary/`, including the generated token at `.devcontainer/aviary/token`.

### Quick Start With Docker Compose

Create the local Aviary config file used by the container:

```shell
cat > .devcontainer/aviary/aviary.yaml <<'EOF'
models:
  defaults:
    model: openai-codex/gpt-5.2
EOF
```

Start the containerized development environment:

```shell
docker compose up --build
```

This starts:

- The Vite dev server at **https://localhost:5173**
- The docs dev server at **https://localhost:5174**
- The Aviary backend at **https://localhost:16677**

The container persists Aviary runtime state under `.devcontainer/aviary/`, including the generated token at `.devcontainer/aviary/token`.

### Quick Start Without Containers

Install web dependencies:

```shell
pnpm install
```

Start the development server (runs Vite + Go backend concurrently):

```shell
pnpm dev
```

For watch mode (`aviary watch`), use `pnpm dev:watch` instead; see [Developing watch mode](#developing-watch-mode).

This starts:

- The Vite dev server at **https://localhost:5173** (hot-reload), also reachable from the LAN at `https://<lan-ip>:5173`
- The docs dev server at **https://localhost:5174** (hot-reload), also reachable from the LAN at `https://<lan-ip>:5174`
- The Aviary Go backend at **https://localhost:16677** (hot-reloads Go file changes via [`forgo run --watch`](https://github.com/lsegal/forgo))

The first `pnpm dev` installs the [forgo](https://github.com/lsegal/forgo) toolchain into `~/.forgo` if it is not already on `PATH`. It also updates a forgo in `~/.forgo` older than the release dev updates need (`MIN_FORGO_VERSION` in `scripts/dev-go.mjs`), because older releases crash the backend after some hot reloads; a forgo set with `FORGO_BIN` or found on `PATH` only gets a warning. Set `FORGO_INSTALL_DIR` to install elsewhere or `FORGO_BIN` to point at an existing binary. Platforms without a forgo release fall back to plain `go run` (no reload on change).

`pnpm dev` (or `pnpm dev:go` for the backend alone) is the supported way to run the dev backend: it runs it under forgo hot reload, so updating the checkout from Aviary's update notice is just a `git pull` that hot-reloads the backend without stopping running jobs. Aviary restarts the backend only when a pull can't be hot-reloaded, such as a `go.mod` change; see [Dev checkouts](docs/site/guide/operations.md#dev-checkouts).

Open https://localhost:5173 in your browser. The Vite dev server proxies MCP and API requests to the Go backend.

#### Developing watch mode

To work on `aviary watch` and its minimal web UI, run:

```shell
pnpm dev:watch -- owner/repo --no-tui
```

Everything after `--` is passed to `aviary watch` (see [Watch sessions](docs/site/guide/projects.md#watch-sessions)). This runs the watch session from source under forgo hot reload, as `pnpm dev:go` does for `serve`, and starts its own Vite dev server on the first free port from 5173 up, so it can run alongside `pnpm dev`. Vite proxies `/api` and `/mcp` to whichever port the session chose (it writes the port to `AVIARY_WATCH_PORT_FILE`), and the session prints the Vite URL (for example `https://localhost:5174/agents/watch/projects`) as its Code Loop link. Web changes hot-reload there. With the TUI on, Vite's output goes to a log file whose path is printed at startup. Ctrl+C or `SIGTERM` stops both processes, and the session removes its temporary directory.

Use `pnpm dev:watch` when you change watch mode or its web UI. A plain `forgo run ./cmd/aviary watch` (or `go run`) serves the web UI embedded in the binary, which a source checkout only has after `pnpm build`. Without a build the session's web UI shows a "Web assets not built" page, and a build older than the checkout's web sources gets a "web assets are stale" warning in the log. That is fine when you only work on the Go side or the TUI.

#### HTTPS in development

The Vite dev server serves HTTPS so the composer mic, which browsers only allow on HTTPS or `localhost`, also works from a phone on your LAN. It uses the backend's certificate (`aviary cert`): a self-signed certificate in `~/.config/aviary/certs/` covering `localhost`, `127.0.0.1`, `::1`, your hostname, and your current LAN IPv4 addresses. It is generated on first run, reused after that, and regenerated automatically when your LAN address changes.

- Accept the browser warning, or trust the certificate on desktop, iOS, and Android, as described in [Trusting the certificate](docs/site/guide/operations.md#trusting-the-certificate).
- For warning-free trust, use [mkcert](docs/site/guide/operations.md#trusted-certificates-with-mkcert) and point `server.tls.cert`/`server.tls.key` at its files; the dev server picks them up too.
- Run `pnpm dev:cert` to regenerate the self-signed certificate, then restart `pnpm dev`.
- Set `AVIARY_DEV_NO_TLS=1` to serve plain `http://localhost:5173` and `http://localhost:5174` instead.

The docs dev server serves HTTPS with the same certificate, so a device that trusts it reaches both servers.

#### LAN access

Both dev servers, the web UI on port 5173 and the docs on port 5174, listen on every interface and accept the same host names (`scripts/dev-allowed-hosts.mjs`). By default those are the names the certificate covers: `localhost`, the loopback addresses, your hostname, its `.local` name (for example `https://my-mac.local:5174`), and your current LAN IPv4 addresses. Over HTTPS, Vite skips its host check, so this list matters when they serve plain HTTP (`AVIARY_DEV_NO_TLS=1`, or when the certificate cannot be loaded). Then Vite rejects other names with "Blocked request. This host is not allowed".

To reach them over plain HTTP by another name, such as a DNS name or a Tailscale name pointing at your machine, list it in `AVIARY_DEV_HOSTS` before `pnpm dev`:

```shell
AVIARY_DEV_HOSTS=dev.example.com,.tailnet.ts.net pnpm dev
```

Names are comma-separated; a leading dot allows every subdomain. Vite's `__VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS` is honored the same way by both servers, even though the Vite version VitePress bundles ignores it on its own. The app preview server (`pnpm preview`) accepts the same names; the docs preview server (`pnpm docs:preview`) doesn't check the host. Add the extra names to `server.tls.sans` too, so the certificate covers them.

In the dev container or Docker Compose setup, the certificate is generated inside the container. Add the names you reach it by, such as your host's LAN address, under `server.tls.sans` in `.devcontainer/aviary/aviary.yaml`.

> **First run:** The backend generates an auth token on first start. Check the terminal output for `Your access token: aviary_tok_...` and paste it into the login screen.

## Project Structure

```plain
cmd/aviary/          CLI entrypoint (Cobra)
internal/
  agent/             Agent manager and runner
  auth/              Credential store and OAuth flows
  browser/           Browser automation (chromedp)
  channels/          Slack, Discord, Signal integrations
  config/            Config loading and file watching
  llm/               LLM provider adapters (Anthropic, OpenAI, Gemini, Stdio)
  mcp/               MCP server and tool dispatch
  memory/            Agent memory storage and search
  scheduler/         Cron runner, file watcher, job queue
  server/            HTTPS server, TLS, auth, web embed
  store/             Atomic JSON and append-only JSONL storage
docs/
  site/              VitePress documentation site
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

## Regenerating Docs Screenshots

The screenshots on the docs site and the aviary.bot landing page (`docs/site/public/screenshots/*-{light,dark}.png`) are rendered from the web UI against fixture data, not from a live instance. When the UI changes, update the fixtures in `scripts/generate-doc-screenshots.mjs` (for example `projectsFixtures` for the Code Loop board and run view) and regenerate:

```shell
pnpm exec vite                                   # web dev server, if it isn't already running
node scripts/generate-doc-screenshots.mjs        # every screenshot, light and dark
node scripts/generate-doc-screenshots.mjs projects  # only screenshots whose name starts with "projects"
```

The script mocks every MCP call and answers every other `/api` request itself (the dev server would otherwise proxy them to your local Aviary server), so no Go server or credentials are needed and nothing private ends up in an image. Set `DOCS_SCREENSHOT_BASE_URL` when the web dev server isn't at `https://127.0.0.1:5173`. Keep fixture data to public repositories with no tokens or personal details.

## MCP Connection

Aviary exposes an MCP endpoint you can connect to directly for testing:

```
Endpoint: https://localhost:16677/mcp
Token:    cat ~/.config/aviary/token
Config:   ~/.config/aviary/aviary.yaml
```

The server is usually already running during development (`pnpm dev` starts it). You can restart it by stopping `pnpm dev` and running `go run ./cmd/aviary serve`.

## Making Changes

- **Go changes:** `pnpm test:go` after every change; `pnpm lint:go` to check style
- **Web changes:** `pnpm lint:fix` to auto-format; `pnpm test:e2e` to verify UI behavior
- **Config schema changes:** update `internal/config/config.go` and any relevant stores in `web/src/stores/`

## Submitting a Pull Request

1. **Fork and branch** — fork the repo and create a feature branch from `main`.

2. **Sign your commits** — all commits must be GPG-signed. Configure Git signing before you start:

   ```shell
   git config --global commit.gpgsign true
   ```

   GitHub will show a "Verified" badge on signed commits. PRs with unsigned commits will not be merged.

3. **Pass all checks locally** before pushing:

   ```shell
   pnpm test      # lint + Go tests + e2e (mirrors CI exactly)
   ```

   CI runs Go tests on Linux, macOS, and Windows, plus Go lint, web lint, and Playwright e2e tests. Fix any failures before opening the PR.

4. **Write focused commits** — keep commits small and on-topic. Use a descriptive subject line (`feat:`, `fix:`, `refactor:`, etc.).

5. **Open the PR against `main`** — provide a short summary of what changed and why. If it fixes an issue, reference it (`Closes #123`).

6. **Keep CI green** — if CI fails after you open the PR, fix the failures before requesting review.
