# Guide

This section is the operator-facing documentation track for Aviary. The structure is based on the UI and runtime surfaces that already exist in the codebase so the guide can expand without reshuffling pages later.

## Planned Guide Sections

- [Getting Started](./getting-started) covers installation, server startup, token-based login, and the first control-panel session.
- [Configuration](./configuration) is the durable source for `aviary.yaml`, provider auth, agent definitions, permissions, and runtime defaults.
- [Channels](./channels) covers Slack, Discord, and Signal setup, sender rules, and channel delivery routing.
- [Security & Permissions](./security-permissions) covers host setup, server hardening, agent permission design, and external sandboxing recommendations.
- [Scheduled Tasks](./scheduled-tasks) covers cron and file-watch triggers, the precompiler, zero-token Lua script execution, and the sandboxed scripting API.
- [Control Panel](./control-panel) walks section by section through Overview, Chat, Settings, and the System area.
- [Operations](./operations) is where runtime workflows will live: sessions, jobs, usage, logs, daemons, and upgrade flows.

## Editorial Direction

- Keep the guide task-oriented.
- Let the reference pages hold exhaustive field and tool detail.
- Prefer documenting the current behavior in code over speculative features.
