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

Or use **Settings → Sessions** in the control panel to delete sessions across all agents at once.

**Resume a session:** Pass `session_id` to `agent_run` or select the session in the Chat view.

---

## Scheduled Tasks and Jobs

Tasks trigger automatically on their configured schedule. Use the Jobs dashboard (`/jobs`) or the MCP tools to monitor and intervene.

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

- **Settings → Providers** — per-provider status badge
- **config_validate** — `"provider connectivity check pending"` warnings become pass/fail on subsequent calls

If a provider shows as unreachable:

1. Verify the credential is stored: `auth_get { name: "ANTHROPIC_API_KEY" }`.
2. Check network connectivity from the server host.
3. Re-run `config_validate` after ~30 seconds for a fresh ping result.

### Bedrock-specific checks

Bedrock does not require a credential in the auth store. If Bedrock shows as unreachable:

1. Verify the AWS region in your config is correct.
2. If using a named profile, confirm it exists in `~/.aws/config` and has valid credentials (check with `aws sts get-caller-identity --profile <profile>`).
3. Confirm the IAM principal has `bedrock:InvokeModelWithResponseStream` permission on the inference profile or model.
4. If using SSO credentials, ensure the session is active (`aws sso login --profile <profile>`).

---

## Upgrades

**Check for a newer release:**

```bash
server_version_check {}
```

**Apply the upgrade:**

```bash
server_upgrade {}
```

Or use the upgrade prompt that appears in the control panel header when a new version is available. The server restarts automatically after a successful upgrade.

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
3. Check the Jobs dashboard for `failed` status — the job may have run but errored.
4. If `precompute_tasks` is enabled, check for a pending or failed compile attempt for the task.

### Bedrock `AccessDeniedException`

A 403 error from Bedrock typically means the IAM principal lacks permission for the requested model:

1. Confirm the model ID is correct. Cross-region inference profiles (e.g. `us.anthropic.claude-sonnet-4-6`) require the `bedrock:InvokeModelWithResponseStream` action on the inference profile ARN, not just the foundation model ARN.
2. Verify the AWS profile and region in your provider config match the working configuration (compare with `aws bedrock-runtime invoke-model --region <region> --profile <profile> --model-id <arn>`).
3. If using instance roles or SSO, check that the session has not expired.
4. Bedrock model access must be explicitly enabled per-region in the AWS console under **Bedrock → Model access**.
