# Security & Permissions

This guide collects the main security levers in Aviary and the surrounding host environment. Aviary already exposes useful controls for agent permissions, credential handling, TLS, and task isolation, but secure operation also depends on how you run the server itself.

The goal is straightforward: keep each agent limited to the smallest tool surface and smallest machine footprint that still lets it do useful work.

::: tip Recommended Baseline
For most deployments, run Aviary on a machine that is not used for general desktop work, keep the server bound to localhost or a trusted private network, leave TLS enabled, store provider keys through Aviary's auth store instead of plain YAML, and use the `standard` permissions preset unless you have a specific reason not to.
:::

## Environment Setup

Start with separation of duties between the host, the Aviary server, and the agents you configure.

### Recommended Setup

- Run Aviary under a dedicated operating-system user instead of your main login account.
- Keep `~/.config/aviary/` readable only by that account because it contains the login bearer token, TLS material, and config backups.
- Prefer a dedicated VM, small server, or container host over a laptop that also contains unrelated personal or production secrets.
- Give each high-trust agent its own `working_dir` instead of pointing multiple agents at a broad shared tree.
- Store model and channel credentials via `auth:<key>` references rather than embedding raw secrets directly in `aviary.yaml`.

::: info Note
Even when agents do not have `file_*` or `exec` tools, the Aviary process itself still runs with the privileges of its host user. Host-level separation matters.
:::

### Config And Secret Hygiene

- Treat `aviary.yaml`, `~/.config/aviary/token`, and the `backups/` directory as sensitive files.
- Avoid checking `aviary.yaml` into Git when it contains real agent names, private paths, channel identifiers, or credential references you do not want exposed.
- If you must manage config in version control, keep secrets in Aviary's auth store and keep the checked-in config free of raw tokens.
- Review backup retention on systems with stricter data-handling requirements. Aviary rotates up to five backups automatically.

## Server Hardening

The Aviary server is the control plane. If it is exposed too broadly, every downstream safeguard matters less.

### Network Exposure

- Keep TLS enabled. Do not set `server.no_tls: true` unless you are in a tightly controlled local-only environment.
- Prefer binding Aviary to localhost and reaching it through a secure tunnel, VPN, or trusted reverse proxy when remote access is required.
- If you expose Aviary on a LAN or WAN, use a real certificate and key via `server.tls.cert` and `server.tls.key` instead of relying on the generated local certificate.
- Limit inbound network access with host firewall rules, security groups, or reverse-proxy allowlists.

::: tip Recommendation
The safest default is still `https://localhost:16677` with the bearer token stored locally. Remote exposure should be an explicit deployment choice, not the default posture.
:::

### Service Controls

- Install Aviary as a service only under the least-privileged account that can still access the files and network paths it needs.
- Review generated service definitions before using them in more locked-down environments.
- Keep the service working directory and environment small; do not inherit a large shell profile full of unrelated credentials.
- Monitor the running PID, live logs, and daemon status so unexpected restarts or channel failures are noticed quickly.

### Authentication And Operator Access

- Treat the bearer token in `~/.config/aviary/token` like an admin credential.
- Rotate that token if the host is shared, copied, or suspected to be compromised.
- Limit who can log into the control panel or connect to the MCP endpoint.
- Avoid pasting the bearer token into shared terminals, screen recordings, or chat logs.

## Agent Access Permissions

Aviary's strongest built-in security controls are the per-agent permission settings.

### Permission Presets

Each agent starts from a preset:

- `minimal`: only session and memory tools
- `standard`: general-purpose tools such as memory, browser, scheduler, search, and skills, but no high-risk local mutation tools
- `full`: full tool access, including filesystem writes, exec, auth, and server configuration tools

Recommended usage:

- Use `minimal` for agents exposed to untrusted humans or public-ish channels.
- Use `standard` for most internal assistant workflows.
- Use `full` only for tightly controlled automation agents with a clear operational need.

::: warning Note
`full` should be treated like shell access with extra steps. If an agent does not clearly need it, do not grant it.
:::

### Tool-Level Restrictions

On top of presets, you can narrow the surface further:

- `disabled_tools` removes tools even if the preset would allow them.
- `tools` selectively adds tools when the preset and policy allow them.
- `channels[].disabled_tools` narrows tools for messages arriving from a specific integration.
- `allow_from[].restrict_tools` narrows tools even further for a specific sender rule.

Useful patterns:

- Remove `exec` unless the agent is explicitly meant to run commands.
- Remove memory-write tools for external chat channels when long-term memory is not essential.
- Separate agents by role instead of creating one agent with a very broad tool surface.

### Filesystem Controls

If an agent has file access, restrict it aggressively:

- Use `filesystem.allowed_paths` to define a short explicit allowlist.
- Add deny rules for secrets, env files, deployment keys, and credential folders.
- Prefer agent-specific working directories over large monorepo or home-directory access.
- Keep writable paths narrower than readable paths whenever possible.

Example:

```yaml
permissions:
  preset: full
  filesystem:
    allowed_paths:
      - "~/projects/support-bot/**"
      - "!~/projects/support-bot/.env"
      - "!~/projects/support-bot/secrets/**"
```

### Exec Controls

If command execution is enabled:

- Use `exec.allowed_commands` as an allowlist, not a wish list.
- Keep `shell_interpolate: false` unless you specifically need shell expansion.
- Deny dangerous flag patterns explicitly with `!` rules.
- Prefer wrapper scripts with narrow behavior over broad command wildcards.

Example:

```yaml
permissions:
  preset: full
  exec:
    allowed_commands:
      - "go test ./..."
      - "go test ./internal/..."
      - "!go test -run *"
    shell_interpolate: false
```

::: tip Recommendation
If an agent only needs to run one or two repeatable commands, encode exactly those commands instead of permitting a whole toolchain.
:::

### Channel-Level Safety

Channels widen the audience that can reach an agent, so use the message-routing controls deliberately:

- Keep `allow_from` narrow for production-facing bots.
- Use `allowed_groups` to limit which rooms or channels can trigger the agent.
- Use `respond_to_mentions` or explicit prefixes when you do not want every message forwarded.
- Give public-facing or team-facing channel agents a lower privilege profile than private operator agents.

## Sandboxing

Aviary includes important built-in limits, but host-level sandboxing is still useful and often necessary.

### What Aviary Already Sandboxes

- Prompt tasks can only use the tools granted to their agent.
- Script tasks run inside Aviary's Lua task sandbox rather than arbitrary local Lua execution.
- File I/O, `os.execute`, `loadfile`, and `dofile` are disabled inside script tasks.
- Channel and sender rules can further narrow what an agent can do depending on where the request came from.

That helps, but it does not replace host isolation.

### Host-Level Isolation Options

Depending on your environment, you may want one of these layers around Aviary itself:

- Docker or another container runtime to isolate filesystem view, network reachability, and runtime dependencies
- A VM for stronger workload separation when the host also runs other sensitive services
- `chroot`, jail-style isolation, or namespace-based containment on systems where containers are not the right fit
- systemd hardening directives such as restricted writable paths, private temp directories, dropped capabilities, and network controls

::: info Note
These controls are outside Aviary itself, but they materially change the blast radius if an agent, plugin, credential, or host account is compromised.
:::

### Practical Container Guidance

If you run Aviary in Docker or a similar container:

- Mount only the config and working directories the deployment actually needs.
- Prefer read-only mounts for source material that should not be modified.
- Do not mount the entire host home directory.
- Run as a non-root container user.
- Restrict outbound network access if agents only need specific APIs.
- Keep secrets in injected environment variables or mounted secret files only when that fits your platform's secret-management model better than Aviary's auth store.

### Practical Host Hardening Guidance

If you run Aviary directly on Linux or macOS:

- Use a dedicated service account.
- Restrict filesystem permissions on config, token, and key files.
- Limit which directories the service account can write to.
- Consider service-manager controls that block access to unrelated parts of the host.
- Treat agent `working_dir` choices as part of the security boundary, not just a convenience setting.

## Sensible Security Defaults

For a typical internal deployment, this is a strong starting posture:

- Keep the server local or behind a trusted private boundary.
- Keep TLS enabled.
- Store provider credentials in Aviary's auth store.
- Default agents to `standard`.
- Use separate low-privilege agents for Slack, Discord, or Signal.
- Grant `full` only to narrowly scoped automation agents.
- Use explicit filesystem allowlists and explicit exec allowlists.
- Run Aviary as a dedicated service account.
- Add container or VM isolation when the host contains anything else you would care about losing.

::: tip Quick Rule
If a permission feels convenient but not necessary, leave it out first. It is easier to add one missing capability than to clean up after an agent that had too much access.
:::
