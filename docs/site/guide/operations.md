# Operations

This guide covers day-to-day operations: managing agents and sessions, working with the job queue, monitoring the server, and handling common failure modes.

---

## Starting and Stopping the Server

```bash
aviary serve    # start in the foreground (-d for background)
aviary serve stop     # send stop signal to the running server
aviary service status   # print running/stopped and the PID
```

The server writes its PID to `~/.config/aviary/aviary.pid`. If the process crashes without cleaning up, delete the PID file before restarting.

---

## TLS Certificates

The server serves HTTPS by default. Unless you configure your own certificate, it uses a self-signed one stored in `~/.config/aviary/certs/` (`cert.pem` and `key.pem`). The certificate covers:

- `localhost`, `127.0.0.1`, and `::1`
- the machine's hostname (plus `<hostname>.local` for a bare hostname)
- the machine's current LAN IPv4 addresses
- any extra names in `server.tls.sans`

The server generates the certificate on first start. On later starts it reuses it, and regenerates it only when it has expired or no longer covers those names, for example after your LAN address changes. A regenerated certificate has to be trusted again on each device.

```bash
aviary cert                 # print the cert and key paths, generating them if needed
aviary cert --regenerate    # replace the self-signed cert, then restart the server
```

The `pnpm dev` server uses the same certificate, so a device trusts one certificate for both. See [Development over HTTPS](#development-over-https).

### TLS settings

| `aviary.yaml` | Environment variable | Purpose |
| --- | --- | --- |
| `server.tls.cert`, `server.tls.key` | `AVIARY_CONFIG_SERVER_TLS_CERT`, `AVIARY_CONFIG_SERVER_TLS_KEY` | Serve your own certificate and key (PEM) instead of the self-signed one. |
| `server.tls.sans` | `AVIARY_CONFIG_SERVER_TLS_SANS` (comma-separated) | Extra hostnames and IP addresses for the self-signed certificate. |
| `server.tls.no_host_sans` | `AVIARY_CONFIG_SERVER_TLS_NO_HOST_SANS` | Leave the hostname and LAN addresses out of the self-signed certificate. |
| `server.no_tls` | `AVIARY_CONFIG_SERVER_NO_TLS` | Serve plain HTTP, for example behind a reverse proxy that terminates TLS. |

Environment variables override the config file. TLS changes take effect after a server restart.

### Trusting the certificate

Browsers warn about a self-signed certificate. You can accept the warning, or trust the certificate so the warning goes away.

Accepting the warning is enough to use the control panel, but [installing it as an app](/guide/control-panel#installing-as-an-app) needs a certificate the device trusts.

- **Desktop browsers:** choose **Advanced** and continue to the site. To trust it instead, import `cert.pem` into the system store: on macOS, open it in Keychain Access and set **Always Trust**; on Windows, import it into **Trusted Root Certification Authorities**; on Linux, add it to your browser's certificate settings.
- **iOS:** open the address in Safari, tap **Show Details**, and then **visit this website**. iOS only offers full trust for CA certificates, so to remove the warning, use [mkcert](#trusted-certificates-with-mkcert) and install its root CA.
- **Android:** in Chrome, tap **Advanced** and **Proceed**. To trust a certificate instead, install the mkcert root CA under **Settings > Security > Encryption & credentials > Install a certificate > CA certificate**.

### Trusted certificates with mkcert

[mkcert](https://github.com/FiloSottile/mkcert) creates a local certificate authority that your devices can trust, so no browser shows a warning:

```bash
mkcert -install
mkcert -cert-file ~/.config/aviary/certs/mkcert.pem \
  -key-file ~/.config/aviary/certs/mkcert-key.pem \
  localhost 127.0.0.1 ::1 "$(hostname)" 192.168.1.20   # your LAN address
```

Then point Aviary at the files in `aviary.yaml` and restart the server (and `pnpm dev`):

```yaml
server:
  tls:
    cert: ~/.config/aviary/certs/mkcert.pem
    key: ~/.config/aviary/certs/mkcert-key.pem
```

To trust it on a phone, copy `rootCA.pem` from `mkcert -CAROOT` to the device and install it. On iOS, install the profile under **Settings > General > VPN & Device Management**, then turn on full trust under **Settings > General > About > Certificate Trust Settings**. On Android, install it as a CA certificate as described above. Rerun `mkcert` when your LAN address changes.

### Development over HTTPS

`pnpm dev` serves the web UI at `https://<host>:5173` and listens on the LAN, so a phone can open it at `https://<lan-ip>:5173`. The composer mic only works on HTTPS or `localhost` pages, so this is what makes voice input work from other devices. Hot reload runs over `wss`, and the `/api` and `/mcp` proxies to the backend are unchanged.

The dev server gets its certificate from `aviary cert`: the configured `server.tls.cert`/`server.tls.key`, or the self-signed certificate described above. Run `pnpm dev:cert` to regenerate it, then restart `pnpm dev`.

Set `AVIARY_DEV_NO_TLS=1` to serve the dev server over plain `http://` instead:

```bash
AVIARY_DEV_NO_TLS=1 pnpm dev
```

---

## Running Agents

Agents start automatically when the server starts. They are idle until a message arrives (via chat, a channel, or a task trigger). You do not need to "start" an individual agent.

**Stop all work for an agent** (cancels any in-progress LLM calls and tool runs):

```bash
# via MCP
agent_stop { name: "assistant" }
```

Or click the stop button in the Chat view.

**Restart a crashed agent:** Agents recover automatically on the next request. If an agent is stuck, stop it (above) and send a new message — it will re-initialize.

---

## Working with Sessions

Sessions persist indefinitely. Clean them up when they are no longer needed.

**List sessions for an agent:**

```bash
session_list { agent: "assistant" }
```

**Delete a session:**

```bash
session_remove { agent: "assistant", session_id: "01HZ..." }
```

Or use **System → Sessions** in the control panel to delete sessions across all agents at once.

**Clear a session:** The `"main"` session cannot be deleted. Clear its messages instead; it keeps its ID, name, and bindings:

```bash
session_clear { agent: "assistant", session_id: "main" }
```

**Resume a session:** Pass `session_id` to `agent_run` or select the session in the Chat view.

---

## Scheduled Tasks and Jobs

Tasks trigger automatically on their configured schedule. Use an agent's Scheduled Tasks page (`/agents/<agent>/tasks`) or the MCP tools to monitor and intervene.

**Check pending jobs:**

```bash
job_query { status: "pending" }
```

**Force-run a pending job immediately** (bypasses schedule and concurrency limits):

```bash
job_run_now { id: "01HZ..." }
```

**Stop all running and pending jobs:**

```bash
task_stop {}
```

**Check compile attempts** (when `precompute_tasks` is enabled):

```bash
task_compile_query { status: "error" }
```

A compile error means the LLM could not translate the prompt into a Lua script. Inspect the error with `task_compile_get`, revise the task prompt, and re-trigger with `task_run`.

---

## Reading Logs

**Live logs** stream at `/logs` in the control panel. Filter by component (`chat`, `channel`, `scheduler`, `mcp`) and level.

**Job-specific output:**

```bash
job_logs { id: "01HZ..." }
```

Returns the captured stdout/stderr from the job run, or the session message log if no explicit output was captured.

---

## Config Validation

The Overview page runs `config_validate` on each load. It checks:

- Required fields (agent name, model)
- Provider credentials present and resolvable
- Provider connectivity (asynchronous, cached 30 s)
- Cron expression syntax for scheduled tasks
- TLS certificate and key paths (if configured)

**Fix errors before saving** — `config_save` rejects configs with error-level issues. Warning-level issues are shown but do not block the save.

**Restore a backup** if a bad config was saved:

```bash
config_restore_latest_backup {}
```

Backups rotate up to five copies in `~/.config/aviary/backups/`.

---

## Provider Health

Provider connectivity is checked asynchronously and cached for 30 seconds. Results appear in:

- **Settings → Providers & Authentication** — per-provider status badge
- **config_validate** — `"provider connectivity check pending"` warnings become pass/fail on subsequent calls

If a provider shows as unreachable:

1. Verify the credential is stored: `auth_get { name: "ANTHROPIC_API_KEY" }`.
2. Check network connectivity from the server host.
3. Re-run `config_validate` after ~30 seconds for a fresh ping result.

---

## Upgrades

Aviary checks for a new release every 5 minutes. When it finds one, every open control panel shows a notice that links the version to its release page.

**Check for a newer release, and see which running jobs an upgrade would stop:**

```bash
server_version_check {}
```

**Apply the upgrade:**

```bash
server_upgrade {}                  # now
server_upgrade { when: "idle" }    # once no jobs are running
```

From a terminal, `aviary upgrade` upgrades with its own binary's upgrade code, never the running server's, so an outdated or stuck server cannot break the upgrade. It checks first and prints `Aviary v0.7.4 is up to date` when Aviary is current, or `Upgrading v0.7.3 → v0.7.5` before it installs. It then downloads and verifies the release, installs it, and updates skills itself. A running release server is only restarted safely around the swap, the same way as `server_restart`: running jobs are checkpointed, and `aviary upgrade` waits for the server to come back and confirms it runs the new version. When the server runs from a different binary than `aviary`, that binary is upgraded too. With no server running, none is started. A server that does not respond, or cannot be restarted, is reported, and the command fails, after the binary was upgraded. `--version` accepts versions with or without a leading `v`, and `--when idle` swaps the binary right away and restarts the server once no jobs are running.

The upgrade mode comes from how the binary was built: a release build always upgrades from GitHub releases, even when run from inside an Aviary clone, and only a dev build running from its checkout pulls with git (see [Dev checkouts](#dev-checkouts)). `aviary version` and `aviary doctor` show the detected install type, and `aviary upgrade` prints it as its first line (`--verbose` explains how it was detected). In source mode, `aviary upgrade` reports the local and upstream commits it compared after fetching, says whether the running binary is older than the checkout's HEAD, and prints the command that rebuilds it. `--release` and `--source` force a mode. `aviary upgrade` restarts the running server only when the server is the same kind of install; a dev server from a checkout is left alone by a release binary's upgrade, and the reverse. For a dev server from the same checkout, `aviary upgrade` pulls itself, then waits for the server to hot-reload the pulled code, restarting it only when hot reload cannot apply it and `pnpm dev` rebuilds it on restart.

An upgrade is a safe restart. Aviary installs the release, updates the skills for every signed-in CLI provider, stops running jobs at a checkpoint and saves their sessions, then restarts. Each job then resumes in its own session and is told that Aviary restarted. Waiting jobs aren't touched. The upgrade dialog in the control panel names the jobs it will stop before you confirm. It offers **Now**, or **When no jobs are running**, which holds new jobs until the restart is done. A waiting upgrade can be canceled from the panel or with `server_restart { cancel: true }`.

Every restart, manual or automatic, first waits for in-flight MCP tool calls to finish, so a call such as `project_issue_create` is never cut off midway; the panel shows "pending: waiting on N MCP calls" meanwhile. New calls are still admitted for `server.restart_call_grace` (1 minute) after the restart was requested, then they wait for the restart and, if it takes longer than 30 seconds, fail with the retryable error "server restarting, try again". After `server.restart_call_max_wait` (5 minutes) the restart proceeds even if calls are still running. Calls that run agent turns (`agent_run`, `agent_run_script`) are not waited for: the restart checkpoints them like jobs. Pass `force: true` to `server_restart` or `server_upgrade` to skip the wait.

`server_restart {}` restarts the same way without installing anything.

**Automatic updates:** set `server.auto_update: true` to install new releases on their own. An available update is applied as soon as it is found, even while jobs are running: like a manual upgrade with **Now**, running jobs are checkpointed and resume after the restart.

### Dev checkouts

A dev build running from an Aviary git checkout (for example under `pnpm dev`, outside Docker) updates from git instead of releases. The check fetches the checkout's upstream branch and compares it with `HEAD`, and `git status` tells it whether the working tree has local changes. The notice appears when the upstream branch has new commits.

`pnpm dev` tells the backend where the checkout is through `AVIARY_CHECKOUT`, so the backend finds it even when it runs, or restarts, outside the checkout directory. To run a dev build any other way, set `AVIARY_CHECKOUT` to the checkout root yourself; without it, Aviary looks for a checkout around its own executable (for example `./aviary` built by `pnpm build:go`), never its working directory. `aviary upgrade --source` is the one exception: with no other checkout known, it pulls the checkout containing the working directory. A dev build that finds no checkout says so in the update notice instead of appearing up to date.

Updating, manually or automatically, runs `git pull --ff-only` in the checkout. `pnpm dev` runs the backend under forgo's hot reload (`forgo run --watch`), so pulled Go changes go live in the running backend without a restart: running jobs keep running, the Projects board keeps its jobs, and chat and MCP sessions stay connected (reloaded MCP tools are registered again). Aviary waits for forgo's verdict and logs `dev update applied via hot reload` when the new code is live. A pull that changes no Go code needs nothing more.

Aviary falls back to a safe restart only when hot reload can't run the pulled code, and logs `dev update needs a full restart` with the reason: `go.mod`, `go.sum` or a file a Go package embeds changed, forgo isn't active (for example on a platform without a forgo release), or forgo refuses or fails the reload (a struct's layout, a function's signature or a package's initialization changed, or the pulled code didn't compile). That restart works like a release upgrade (automatic updates restart now; a manual update can also wait until no jobs are running): running jobs are checkpointed and resumed, and `pnpm dev` runs the backend again from the pulled sources, so the backend never keeps running old code beside an updated frontend. A dev build run any other way can't rebuild itself, so the pull still happens and Aviary tells you to restart it. Aviary never pulls over uncommitted changes or a branch that has diverged from its upstream; the notice names the git state that blocks the update instead. Setting `AVIARY_EMULATE_VERSIONS` keeps the release updater in force.

Every process on a checkout (the server, `aviary watch` and `aviary upgrade`) runs its updater git commands one at a time, through a lock file in the checkout's `.git` directory, and update checks never take `.git/index.lock`. When another git command, such as an editor's git integration, holds a lock in the checkout, the updater retries for a few seconds and then shows "Update deferred: another git operation is running, retrying" with a **Retry** button; the automatic updater tries again within a minute. An `index.lock` at least 10 minutes old is reported as stale, with a **Remove stale lock and update** button (`server_upgrade { remove_stale_lock: true }`). The logs name the caller of each updater git command.

---

## Troubleshooting

### Agent returns no response

1. Check **Logs** for LLM errors or rate-limit responses.
2. Run `config_validate` — a missing or expired credential is the most common cause.
3. Check that the provider is reachable (see Provider Health above).
4. Verify the model ID in the agent config matches a supported model string.

### Browser automation fails

1. Confirm a Chrome or Chromium binary is installed and the path in `browser.binary` is correct (or auto-detection finds it).
2. Check that `browser.cdp_port` (default `9222`) is not in use by another process.
3. Try `browser.headless: false` to see the browser window and debug the issue visually.

### Channel messages not received

1. Confirm the channel daemon shows as running in **Daemons** (`/daemons`).
2. Check that the bot token and workspace/server ID are correct.
3. Verify the `allow_from` rules match the sender - an overly restrictive filter silently drops messages.
4. Check **Logs** for channel-component errors.

### Task not running on schedule

1. Confirm `enabled: true` on the task (or that `enabled` is absent, which defaults to true).
2. Check the cron expression with a cron validator — Aviary accepts 5-field and 6-field (leading seconds) expressions plus shortcuts like `@daily`.
3. Check the agent's Scheduled Tasks page for a failed last run — the job may have run but errored.
4. If `precompute_tasks` is enabled, check for a pending or failed compile attempt for the task.
